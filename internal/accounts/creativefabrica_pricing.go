package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/leonardo2api/leonardo2api/internal/creativefabrica"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
)

const (
	creativeFabricaPricingSource     = "creativefabrica-preset"
	creativeFabricaFlowPricingSource = "creativefabrica-flow"
	creativeFabricaMaxPricingComb    = 4096
)

// SyncCreativeFabricaPricing refreshes the local admission rules from the
// account-scoped Creative Fabrica CalculateModelCost RPC. The operation is
// intentionally explicit (admin-triggered): public task admission never
// blocks on a remote pricing request.
func (s *Service) SyncCreativeFabricaPricing(ctx context.Context, account domain.Account, token string) (int, error) {
	if account.ProviderID != providers.CreativeFabrica {
		return 0, errors.New("account is not a Creative Fabrica provider account")
	}
	if strings.TrimSpace(token) == "" {
		return 0, errors.New("Creative Fabrica RPC token is required")
	}
	client, err := s.creativeFabricaClient(account)
	if err != nil {
		return 0, err
	}
	if cookieHeader, cookieErr := s.CreativeFabricaCookieHeader(ctx, account.ID); cookieErr != nil {
		return 0, cookieErr
	} else {
		client.CookieHeader = cookieHeader
	}
	configs, err := s.Store.ListProviderModelConfigs(ctx, providers.CreativeFabrica)
	if err != nil {
		return 0, err
	}
	publicByUpstream := make(map[string]string, len(configs)*2)
	for _, config := range configs {
		// Studio returns a catalog/pricing ID and a separate generation enum.
		// Accept either spelling (plus the public ID) so a schema revision does
		// not silently remove a model from dynamic pricing.
		for _, key := range creativefabrica.ModelAliasKeysFromValues(config.UpstreamModel, config.Model.ID) {
			if _, exists := publicByUpstream[key]; !exists {
				publicByUpstream[key] = config.Model.ID
			}
		}
	}

	imageModels, err := client.ListModels(ctx, token, "image")
	if err != nil {
		return 0, fmt.Errorf("list Creative Fabrica image models: %w", err)
	}
	videoModels, err := client.ListModels(ctx, token, "video")
	if err != nil {
		return 0, fmt.Errorf("list Creative Fabrica video models: %w", err)
	}

	version := creativeFabricaPricingSource + "-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	rules := make([]domain.ModelCostRule, 0, len(imageModels)*4+len(videoModels)*8)
	for _, model := range imageModels {
		publicModel := creativeFabricaPublicModel(model, publicByUpstream)
		if publicModel == "" {
			continue
		}
		candidates, err := creativeFabricaPricingCandidates(model, "image", publicModel)
		if err != nil {
			return 0, err
		}
		for _, candidate := range candidates {
			// Image tasks use CreateFlow, not the generic preset generation
			// path. Quote the same settings shape as the worker submits.
			flowRequest, buildErr := creativefabrica.BuildImageFlowRequest(publicModel, "Cost estimate", candidate.Size, 1, "", nil)
			if buildErr != nil {
				return 0, buildErr
			}
			settings, ok := flowRequest["settings"].(map[string]any)
			if !ok {
				return 0, errors.New("Creative Fabrica Flow settings are missing")
			}
			coins, costErr := client.CalculateFlowGenerationCost(ctx, token, settings)
			if costErr != nil {
				return 0, fmt.Errorf("calculate Creative Fabrica image price for %s: %w", publicModel, costErr)
			}
			rules, err = appendUniqueCreativeFabricaRule(rules, domain.ModelCostRule{
				ProviderID: providers.CreativeFabrica, Kind: "image", Model: publicModel,
				Size: candidate.Size, Quality: candidate.Quality, UnitTokens: coins,
				Enabled: true, PriceVersion: version, Source: creativeFabricaFlowPricingSource,
			})
			if err != nil {
				return 0, err
			}
		}
	}
	for _, model := range videoModels {
		publicModel := creativeFabricaPublicModel(model, publicByUpstream)
		if publicModel == "" {
			continue
		}
		candidates, err := creativeFabricaPricingCandidates(model, "video", publicModel)
		if err != nil {
			return 0, err
		}
		for _, candidate := range candidates {
			coins, _, costErr := client.CalculateGenerationCost(ctx, token, pricingModelID(model), candidate.Options, nil)
			if costErr != nil {
				return 0, fmt.Errorf("calculate Creative Fabrica video price for %s: %w", publicModel, costErr)
			}
			rules, err = appendUniqueCreativeFabricaRule(rules, domain.ModelCostRule{
				ProviderID: providers.CreativeFabrica, Kind: "video", Model: publicModel,
				Resolution: candidate.Resolution, Duration: candidate.Duration, UnitTokens: coins,
				Enabled: true, PriceVersion: version, Source: creativeFabricaPricingSource,
			})
			if err != nil {
				return 0, err
			}
		}
	}
	if len(rules) == 0 {
		return 0, errors.New("Creative Fabrica returned no configured model prices")
	}
	return s.Store.ReplaceModelCostRules(ctx, rules)
}

func creativeFabricaPublicModel(model creativefabrica.Model, index map[string]string) string {
	for _, key := range creativefabrica.ModelAliasKeys(model) {
		if publicID := strings.TrimSpace(index[key]); publicID != "" {
			return publicID
		}
	}
	return ""
}

type creativeFabricaPricingCandidate struct {
	Options    map[string]any
	Size       string
	Quality    string
	Resolution string
	Duration   int
}

func pricingModelID(model creativefabrica.Model) string {
	if value := strings.TrimSpace(model.PricingModelID); value != "" {
		return value
	}
	if model.Raw != nil {
		if value := firstStringFromMap(model.Raw, "generationModel", "generation_model", "model"); value != "" {
			return value
		}
	}
	return model.ID
}

func creativeFabricaPricingCandidates(model creativefabrica.Model, kind, publicModel string) ([]creativeFabricaPricingCandidate, error) {
	names := append([]string(nil), model.PricingInputs...)
	if len(names) == 0 {
		if options, ok := model.PricingInputConfig["options"].([]any); ok {
			for _, option := range options {
				if name := strings.TrimSpace(fmt.Sprint(option)); name != "" {
					names = append(names, name)
				}
			}
		}
	}
	names = uniqueStrings(names)
	optionSets := make([][]any, 0, len(names))
	count := 1
	for _, name := range names {
		values := creativeFabricaOptionValues(model, name, kind)
		if len(values) == 0 {
			return nil, fmt.Errorf("Creative Fabrica %s: missing pricing domain for %s", publicModel, name)
		}
		if count > creativeFabricaMaxPricingComb/len(values) {
			return nil, fmt.Errorf("Creative Fabrica %s: pricing domain exceeds %d combinations; snapshot was not truncated", publicModel, creativeFabricaMaxPricingComb)
		}
		count *= len(values)
		optionSets = append(optionSets, values)
	}
	combinations := cartesianPricingOptions(names, optionSets, creativeFabricaMaxPricingComb)
	if len(combinations) == 0 {
		combinations = []map[string]any{{}}
	}

	result := make([]creativeFabricaPricingCandidate, 0, len(combinations))
	for _, options := range combinations {
		candidate := creativeFabricaPricingCandidate{Options: options}
		if kind == "image" {
			candidate.Size = imagePricingCandidateSize(options)
			candidate.Quality = scalarString(options["quality"])
			if candidate.Size == "" {
				candidate.Size = "1024x1024"
			}
		} else {
			candidate.Resolution = normalizePricingResolution(scalarString(options["resolution"]))
			if candidate.Resolution == "" {
				candidate.Resolution = normalizePricingResolution(scalarString(options["mode"]))
			}
			candidate.Duration = scalarInt(options["duration_seconds"])
			if candidate.Duration == 0 {
				candidate.Duration = 5
			}
			if candidate.Resolution == "" {
				candidate.Resolution = "720p"
			}
			// The gateway currently exposes only the exact Creative Fabrica
			// dimensions represented by videospec. Skip catalog options that
			// cannot be admitted safely instead of inventing a price tier.
			spec, ok := videospec.GetForProvider(providers.CreativeFabrica, publicModel)
			if !ok || !spec.SupportsResolution(candidate.Resolution) || !spec.SupportsDuration(candidate.Duration) {
				continue
			}
		}
		result = append(result, candidate)
	}
	return dedupeCreativeFabricaCandidates(result, kind), nil
}

func creativeFabricaOptionValues(model creativefabrica.Model, name, kind string) []any {
	name = strings.TrimSpace(name)
	config := model.InputOptions[name]
	if config == nil {
		config = model.InputOptions[snakeCase(name)]
	}
	if option, ok := config.(map[string]any); ok {
		for _, typeName := range []string{"stringType", "integerType", "numberType", "booleanType"} {
			if typed, ok := option[typeName].(map[string]any); ok {
				if values := scalarValues(typed["allowedValues"]); len(values) > 0 {
					return normalizePricingOptionValues(typeName, values)
				}
				if typeName == "integerType" {
					minimum, maximum := scalarInt(typed["min"]), scalarInt(typed["max"])
					_, hasMin := typed["min"]
					_, hasMax := typed["max"]
					if hasMin && hasMax && minimum >= 0 && maximum >= minimum && maximum-minimum < creativeFabricaMaxPricingComb {
						values := make([]any, 0, maximum-minimum+1)
						for current := minimum; current <= maximum; current++ {
							values = append(values, current)
						}
						return values
					}
				}
				if typeName == "booleanType" {
					return []any{false, true}
				}
				if defaultValue, ok := typed["defaultValue"]; ok && defaultValue != nil {
					return []any{normalizePricingOptionValue(typeName, defaultValue)}
				}
			}
		}
	}
	// Keep defaults narrow when a catalog omits input metadata. These values
	// are only used to ask the upstream calculator; they are never prices.
	switch strings.ToLower(name) {
	case "duration_seconds", "duration", "video_duration":
		return []any{5}
	case "resolution":
		return []any{"720p"}
	case "aspect_ratio":
		return []any{"16:9"}
	case "mode":
		return []any{"720p"}
	case "quality":
		return []any{"standard"}
	default:
		if kind == "image" && name == "size" {
			return []any{"1024x1024"}
		}
		return nil
	}
}

func normalizePricingOptionValues(typeName string, values []any) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, normalizePricingOptionValue(typeName, value))
	}
	return result
}

func normalizePricingOptionValue(typeName string, value any) any {
	value = normalizeOptionValue(value)
	switch typeName {
	case "integerType":
		return scalarInt(value)
	case "numberType":
		if typed, ok := value.(float64); ok {
			return typed
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
		if err == nil {
			return parsed
		}
	case "booleanType":
		if typed, ok := value.(bool); ok {
			return typed
		}
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true")
	}
	return value
}

func cartesianPricingOptions(names []string, sets [][]any, limit int) []map[string]any {
	if len(sets) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, minInt(limit, 16))
	var walk func(int, map[string]any)
	walk = func(index int, current map[string]any) {
		if len(result) >= limit {
			return
		}
		if index == len(sets) {
			result = append(result, cloneAnyMap(current))
			return
		}
		name := names[index]
		for _, value := range sets[index] {
			current[name] = normalizeOptionValue(value)
			walk(index+1, current)
		}
		delete(current, name)
	}
	walk(0, make(map[string]any, len(sets)))
	return result
}

func imagePricingCandidateSize(options map[string]any) string {
	width, height := scalarInt(options["width"]), scalarInt(options["height"])
	if width > 0 && height > 0 {
		return fmt.Sprintf("%dx%d", width, height)
	}
	for _, key := range []string{"size", "dimensions", "resolution"} {
		if value := scalarString(options[key]); strings.Contains(value, "x") {
			return strings.ToLower(value)
		}
	}
	aspect := strings.ReplaceAll(strings.TrimSpace(scalarString(options["aspect_ratio"])), " ", "")
	switch aspect {
	case "16:9", "3:2", "4:3", "21:9":
		return "1536x1024"
	case "9:16", "2:3", "3:4":
		return "1024x1536"
	default:
		return "1024x1024"
	}
}

func dedupeCreativeFabricaCandidates(candidates []creativeFabricaPricingCandidate, kind string) []creativeFabricaPricingCandidate {
	seen := make(map[string]struct{}, len(candidates))
	result := make([]creativeFabricaPricingCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		// Distinct upstream options must all be quoted. Local price dimensions
		// are not proof that two upstream requests have the same cost.
		encoded, err := json.Marshal(candidate.Options)
		if err != nil {
			// Retain unencodable candidates so the caller reports the request
			// error instead of silently dropping a pricing combination.
			result = append(result, candidate)
			continue
		}
		key := string(encoded)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if kind == "image" {
			if result[i].Size == result[j].Size {
				return result[i].Quality < result[j].Quality
			}
			return result[i].Size < result[j].Size
		}
		if result[i].Resolution == result[j].Resolution {
			return result[i].Duration < result[j].Duration
		}
		return result[i].Resolution < result[j].Resolution
	})
	return result
}

func appendUniqueCreativeFabricaRule(rules []domain.ModelCostRule, rule domain.ModelCostRule) ([]domain.ModelCostRule, error) {
	for index := range rules {
		current := &rules[index]
		if current.ProviderID == rule.ProviderID && current.Kind == rule.Kind && current.Model == rule.Model && current.Size == rule.Size && current.Quality == rule.Quality && current.Resolution == rule.Resolution && current.Duration == rule.Duration {
			if current.UnitTokens != rule.UnitTokens {
				return nil, fmt.Errorf("Creative Fabrica %s: different upstream quotes map to the same local price dimensions (%d and %d Coins); snapshot not installed", rule.Model, current.UnitTokens, rule.UnitTokens)
			}
			return rules, nil
		}
	}
	return append(rules, rule), nil
}

func scalarValues(value any) []any {
	switch values := value.(type) {
	case []any:
		result := make([]any, 0, len(values))
		for _, item := range values {
			result = append(result, normalizeOptionValue(item))
		}
		return result
	case []string:
		result := make([]any, len(values))
		for index, item := range values {
			result[index] = item
		}
		return result
	}
	return nil
}

func normalizeOptionValue(value any) any {
	if object, ok := value.(map[string]any); ok {
		if nested, exists := object["value"]; exists {
			return normalizeOptionValue(nested)
		}
		if scalar, exists := object["stringValue"]; exists {
			return scalar
		}
		if scalar, exists := object["integerValue"]; exists {
			return scalarInt(scalar)
		}
	}
	return value
}

func scalarString(value any) string {
	value = normalizeOptionValue(value)
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func scalarInt(value any) int {
	value = normalizeOptionValue(value)
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		value, _ := strconv.Atoi(strings.TrimSpace(typed))
		return value
	default:
		return 0
	}
}

func normalizePricingResolution(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "prompt_to_video_generator_content_resolution_")
	return value
}

func firstStringFromMap(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := scalarString(data[key]); value != "" {
			return value
		}
	}
	return ""
}

func cloneAnyMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func snakeCase(value string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
