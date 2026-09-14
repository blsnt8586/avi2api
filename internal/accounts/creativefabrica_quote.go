package accounts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/leonardo2api/leonardo2api/internal/creativefabrica"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/providers"
)

// CreativeFabricaQuoteRequest is a read-only upstream estimate, not a generation
// request or a replacement for an immutable task pricing rule.
type CreativeFabricaQuoteRequest struct {
	Kind       string                      `json:"kind"`
	Model      string                      `json:"model"`
	Options    map[string]any              `json:"options"`
	InputMedia []CreativeFabricaQuoteMedia `json:"input_media"`
}

type CreativeFabricaQuoteMedia struct {
	Role     string             `json:"role"`
	Metadata map[string]float64 `json:"metadata"`
}

type CreativeFabricaQuote struct {
	ProviderID          string                      `json:"provider_id"`
	Model               string                      `json:"model"`
	UpstreamModel       string                      `json:"upstream_model"`
	CreditUnit          string                      `json:"credit_unit"`
	Coins               int64                       `json:"coins"`
	PricingType         string                      `json:"pricing_type"`
	ResolvedOptions     any                         `json:"resolved_options,omitempty"`
	Request             CreativeFabricaQuoteRequest `json:"request"`
	Source              string                      `json:"source"`
	CheckedAt           time.Time                   `json:"checked_at"`
	Installed           bool                        `json:"installed"`
	Notice              string                      `json:"notice"`
	DefaultFlowTotal    *int64                      `json:"default_flow_total,omitempty"`
	DefaultFlowQuantity int                         `json:"default_flow_quantity,omitempty"`
	DefaultFlowNotice   string                      `json:"default_flow_notice,omitempty"`
}

func (r *CreativeFabricaQuoteRequest) Validate() error {
	r.Kind = strings.ToLower(strings.TrimSpace(r.Kind))
	r.Model = strings.TrimPrefix(strings.TrimSpace(r.Model), "creativefabrica/")
	if (r.Kind != "image" && r.Kind != "video") || r.Model == "" || strings.ContainsAny(r.Model, "/:") {
		return errors.New("kind must be image/video and model must identify a Creative Fabrica model")
	}
	if len(r.Options) > 64 || len(r.InputMedia) > 64 {
		return errors.New("too many quote parameters")
	}
	for key, value := range r.Options {
		if len(key) == 0 || len(key) > 128 {
			return errors.New("invalid option name")
		}
		switch v := value.(type) {
		case string:
			if len(v) > 256 {
				return errors.New("quote options must not contain prompts or media content")
			}
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("invalid numeric option")
			}
		case bool:
		default:
			return errors.New("quote options must be scalar strings, numbers or booleans")
		}
	}
	for _, media := range r.InputMedia {
		if media.Role == "" || len(media.Role) > 64 {
			return errors.New("input_media role is required")
		}
		for key, value := range media.Metadata {
			switch key {
			case "durationSeconds", "widthPx", "heightPx", "fps":
			default:
				return errors.New("unsupported pricing media metadata")
			}
			if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 1000000 {
				return errors.New("media metadata must contain finite positive values")
			}
			// The inspected Studio helper applies Math.round to source/reference
			// video metadata. Do not extend that evidence to separate audio flows.
			if media.Role != "sourceVideo" && media.Role != "referenceVideo" && math.Trunc(value) != value {
				return errors.New("fractional metadata rounding is not verified for this media role; integer metadata is required")
			}
			rounded := math.Floor(value + 0.5)
			if rounded == 0 {
				return errors.New("media metadata rounds to zero and cannot produce a usable quote")
			}
			media.Metadata[key] = rounded
		}
	}
	return nil
}

// QuoteCreativeFabricaCost asks the official calculator with exact metadata.
// It never uploads media, submits a generation, reserves credits or writes prices.
func (s *Service) QuoteCreativeFabricaCost(ctx context.Context, account domain.Account, request CreativeFabricaQuoteRequest) (CreativeFabricaQuote, error) {
	if account.ProviderID != providers.CreativeFabrica {
		return CreativeFabricaQuote{}, errors.New("account is not a Creative Fabrica account")
	}
	if err := request.Validate(); err != nil {
		return CreativeFabricaQuote{}, err
	}
	config, err := s.Store.GetModelProviderConfig(ctx, providers.CreativeFabrica, request.Model)
	if err != nil {
		return CreativeFabricaQuote{}, errors.New("model is not enabled for Creative Fabrica")
	}
	account, token, err := s.CreativeFabricaToken(ctx, account)
	if err != nil {
		return CreativeFabricaQuote{}, errors.New("account session is unavailable")
	}
	client, err := s.creativeFabricaClient(account)
	if err != nil {
		return CreativeFabricaQuote{}, err
	}
	client.CookieHeader, err = s.CreativeFabricaCookieHeader(ctx, account.ID)
	if err != nil {
		return CreativeFabricaQuote{}, errors.New("account cookie is unavailable")
	}
	models, err := client.ListModels(ctx, token, request.Kind)
	if err != nil {
		return CreativeFabricaQuote{}, errors.New("upstream catalog request failed")
	}
	index := make(map[string]string)
	for _, key := range creativefabrica.ModelAliasKeysFromValues(config.UpstreamModel, config.ModelID) {
		index[key] = request.Model
	}
	for _, model := range models {
		if creativeFabricaPublicModel(model, index) != request.Model {
			continue
		}
		wireOptions, err := creativeFabricaQuoteOptions(model, request.Options)
		if err != nil {
			return CreativeFabricaQuote{}, err
		}
		request.Options = wireOptions
		media := make([]any, 0, len(request.InputMedia))
		for _, item := range request.InputMedia {
			known := false
			for _, role := range model.PricingInputMedia {
				known = known || role.Role == item.Role
			}
			if !known {
				return CreativeFabricaQuote{}, fmt.Errorf("role %s is not declared as a pricing input", item.Role)
			}
			media = append(media, map[string]any{"role": item.Role, "metadata": item.Metadata})
		}
		coins, raw, err := client.CalculateGenerationCost(ctx, token, pricingModelID(model), wireOptions, media)
		if err != nil {
			return CreativeFabricaQuote{}, errors.New("upstream rejected the cost estimate; check model options and required media metadata")
		}
		pricingType, _ := raw["pricingType"].(string)
		quote := CreativeFabricaQuote{
			ProviderID: providers.CreativeFabrica, Model: "creativefabrica/" + request.Model,
			UpstreamModel: pricingModelID(model), CreditUnit: "coins", Coins: coins,
			PricingType: pricingType, ResolvedOptions: raw["resolvedOptions"], Request: request,
			Source: creativeFabricaPricingSource, CheckedAt: time.Now().UTC(),
			Notice: "官方计算器的模型报价；源/参考视频元数据按官网 Math.round 四舍五入，响应列出实际计价参数。未生成内容、未预留积分、未替换本地价格规则；此数不自动表示多张输出的整单费用。",
		}
		if request.Kind == "image" && len(media) == 0 {
			quantity := scalarInt(request.Options["outputCount"])
			if quantity < 1 {
				quantity = 1
			}
			selections := []any{}
			enum := creativefabrica.GenerationModelID(model)
			if quantity > 1 {
				selections = append(selections, map[string]any{"model": enum, "count": quantity})
			}
			total, flowErr := client.CalculateFlowGenerationCost(ctx, token, map[string]any{"model": enum, "models": selections})
			if flowErr == nil {
				quote.DefaultFlowTotal = &total
				quote.DefaultFlowQuantity = quantity
				quote.DefaultFlowNotice = "官网 Flow 默认设置的整单报价；不包含本次额外质量/分辨率选项或参考素材，不自动替换本地价格规则。"
			} else {
				quote.DefaultFlowNotice = "模型报价已取得，Flow 整单报价暂未取得；不要自行用模型报价推断整单金额。"
			}
		}
		return quote, nil
	}
	return CreativeFabricaQuote{}, errors.New("model is not present in this account's upstream catalog")
}

func creativeFabricaQuoteOptions(model creativefabrica.Model, options map[string]any) (map[string]any, error) {
	filled := make(map[string]any, len(options))
	for key, value := range options {
		filled[key] = value
	}
	// CalculateModelCost requires some options even when the catalog gives a
	// default. Only use explicit upstream defaults, never invented dimensions.
	for key, raw := range model.InputOptions {
		if _, present := filled[key]; present {
			continue
		}
		definition, _ := raw.(map[string]any)
		for kind, rawSpec := range definition {
			spec, _ := rawSpec.(map[string]any)
			value, present := spec["defaultValue"]
			if !present {
				continue
			}
			if kind == "integerType" || kind == "numberType" {
				value = quoteNumericBound(value)
			}
			filled[key] = value
			break
		}
	}
	options = filled
	result := make(map[string]any, len(options))
	for key, value := range options {
		definition, ok := model.InputOptions[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("option %s is not declared by this model", key)
		}
		found := false
		for _, kind := range []string{"integerType", "numberType", "stringType", "booleanType"} {
			spec, ok := definition[kind].(map[string]any)
			if !ok {
				continue
			}
			found = true
			switch kind {
			case "integerType", "numberType":
				v, ok := value.(float64)
				if !ok || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 9007199254740991 {
					return nil, fmt.Errorf("option %s requires a finite JSON number", key)
				}
				if kind == "integerType" {
					if math.Trunc(v) != v {
						return nil, fmt.Errorf("option %s requires an integer", key)
					}
					value = int64(v)
				}
				if lower, exists := spec["min"]; exists && v < quoteNumericBound(lower) {
					return nil, fmt.Errorf("option %s is below its minimum", key)
				}
				if upper, exists := spec["max"]; exists && v > quoteNumericBound(upper) {
					return nil, fmt.Errorf("option %s exceeds its maximum", key)
				}
			case "stringType":
				if _, ok := value.(string); !ok {
					return nil, fmt.Errorf("option %s requires a string", key)
				}
			case "booleanType":
				if _, ok := value.(bool); !ok {
					return nil, fmt.Errorf("option %s requires a boolean", key)
				}
			}
			if allowed := scalarValues(spec["allowedValues"]); len(allowed) > 0 {
				matched := false
				for _, candidate := range allowed {
					matched = matched || fmt.Sprint(candidate) == fmt.Sprint(value)
				}
				if !matched {
					return nil, fmt.Errorf("option %s is not an allowed value", key)
				}
			}
			result[key] = value
			break
		}
		if !found {
			return nil, fmt.Errorf("option %s has no supported scalar type", key)
		}
	}
	return result, nil
}

func quoteNumericBound(value any) float64 {
	var result float64
	if _, err := fmt.Sscan(fmt.Sprint(value), &result); err != nil {
		return math.NaN()
	}
	return result
}
