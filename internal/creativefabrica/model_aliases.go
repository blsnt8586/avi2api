package creativefabrica

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ModelAliasKeys returns stable lookup keys for a catalog model. Creative
// Fabrica exposes both a pricing/catalog identifier and a generation enum;
// account responses have used camelCase and snake_case spellings for the
// latter over time. The returned keys include the original lower-cased value
// and a separator-normalized value so callers can match either form without
// leaking provider-specific naming into the public model ID.
func ModelAliasKeys(model Model) []string {
	values := []string{model.ID, model.PricingModelID}
	for _, key := range []string{
		"generationModel", "generation_model",
		"videoGeneratorModel", "video_generator_model",
		"imageGeneratorModel", "image_generator_model",
		"pricingModelId", "pricing_model_id",
		"catalogModelId", "catalog_model_id",
		"model", "modelId", "model_id",
	} {
		if value := modelStringValue(model.Raw[key]); value != "" {
			values = append(values, value)
		}
	}
	return ModelAliasKeysFromValues(values...)
}

// ModelAliasKeysFromValues normalizes provider model identifiers for matching
// a catalog entry to a configured public model. It deliberately keeps both a
// lower-cased raw key and a separator-normalized key: slashes and dots can be
// meaningful in a public ID while upstream enum names commonly use underscores.
func ModelAliasKeysFromValues(values ...string) []string {
	seen := make(map[string]struct{}, len(values)*2)
	result := make([]string, 0, len(values)*2)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
		normalized := strings.NewReplacer("-", "_", ".", "_", "/", "_", " ", "_").Replace(value)
		if normalized != value {
			if _, exists := seen[normalized]; !exists {
				seen[normalized] = struct{}{}
				result = append(result, normalized)
			}
		}
	}
	for _, value := range values {
		add(value)
	}
	return result
}

// GenerationModelID returns the wire generation enum when the catalog entry
// carries one. It is used only by the adapter-facing catalog view; pricing
// continues to use PricingModelID, which is the identifier accepted by the
// CalculateModelCost RPC.
func GenerationModelID(model Model) string {
	for _, key := range []string{
		"generationModel", "generation_model",
		"videoGeneratorModel", "video_generator_model",
		"imageGeneratorModel", "image_generator_model",
	} {
		if value := modelStringValue(model.Raw[key]); value != "" {
			return value
		}
	}
	return ""
}

func modelStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strings.TrimSpace(fmt.Sprint(typed))
	default:
		return ""
	}
}
