package accounts

import (
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/creativefabrica"
	"github.com/leonardo2api/leonardo2api/internal/domain"
)

func TestCreativeFabricaPricingEnumeratesDurationBeforeDefault(t *testing.T) {
	model := creativefabrica.Model{InputOptions: map[string]any{
		"duration_seconds": map[string]any{"integerType": map[string]any{
			"min": "4", "max": "30", "defaultValue": "5",
		}},
	}}
	values := creativeFabricaOptionValues(model, "duration_seconds", "video")
	if len(values) != 27 || scalarInt(values[0]) != 4 || scalarInt(values[26]) != 30 {
		t.Fatalf("duration domain truncated: %v", values)
	}
}

func TestCreativeFabricaPricingEnumeratesBothBooleanValues(t *testing.T) {
	model := creativefabrica.Model{InputOptions: map[string]any{
		"audio": map[string]any{"booleanType": map[string]any{"defaultValue": true}},
	}}
	values := creativeFabricaOptionValues(model, "audio", "video")
	if len(values) != 2 || values[0] != false || values[1] != true {
		t.Fatalf("boolean domain truncated: %v", values)
	}
}

func TestCreativeFabricaPricingRejectsMissingDomain(t *testing.T) {
	model := creativefabrica.Model{PricingInputs: []string{"unknown_pricing_option", "duration_seconds"}}
	if _, err := creativeFabricaPricingCandidates(model, "video", "seedance_v2_5"); err == nil {
		t.Fatal("missing domain must not shift the following option's values")
	}
}

func TestCreativeFabricaPricingRejectsTruncatedSnapshot(t *testing.T) {
	values := make([]any, 65)
	for i := range values {
		values[i] = i
	}
	model := creativefabrica.Model{
		PricingInputs: []string{"one", "two"},
		InputOptions: map[string]any{
			"one": map[string]any{"integerType": map[string]any{"allowedValues": values}},
			"two": map[string]any{"integerType": map[string]any{"allowedValues": values}},
		},
	}
	if _, err := creativeFabricaPricingCandidates(model, "image", "test"); err == nil {
		t.Fatal("over-limit combinations must fail explicitly, not truncate")
	}
}

func TestCreativeFabricaPricingKeepsDistinctUpstreamOptions(t *testing.T) {
	candidates := []creativeFabricaPricingCandidate{
		{Resolution: "720p", Duration: 5, Options: map[string]any{"audio": false}},
		{Resolution: "720p", Duration: 5, Options: map[string]any{"audio": true}},
		{Resolution: "720p", Duration: 5, Options: map[string]any{"audio": true}},
	}
	if got := dedupeCreativeFabricaCandidates(candidates, "video"); len(got) != 2 {
		t.Fatalf("distinct pricing options collapsed: %+v", got)
	}
}

func TestCreativeFabricaPricingRejectsConflictingQuotes(t *testing.T) {
	original := domain.ModelCostRule{ProviderID: "creativefabrica", Model: "test", UnitTokens: 100}
	incoming := original
	incoming.UnitTokens = 200
	if _, err := appendUniqueCreativeFabricaRule([]domain.ModelCostRule{original}, incoming); err == nil {
		t.Fatal("conflicting upstream prices were silently merged")
	}
	if got, err := appendUniqueCreativeFabricaRule([]domain.ModelCostRule{original}, original); err != nil || len(got) != 1 {
		t.Fatalf("identical price should deduplicate: %v %v", got, err)
	}
}
