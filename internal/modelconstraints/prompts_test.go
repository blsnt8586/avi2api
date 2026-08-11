package modelconstraints

import "testing"

func TestPublicPromptLimits(t *testing.T) {
	want := map[string]int{
		"gpt-image-2": 9999, "nano-banana-2": 9999, "nano-banana-pro": 9999, "seedream-5.0-pro": 9999,
		"flux-3-video": 5000, "seedance-2.0": 5000, "seedance-2.0-fast": 5000, "seedance-2.0-mini": 5000,
		"veo-3.1": 9999, "veo-3.1-fast": 9999, "kling-o3-omni": 2500, "minimax-h3": 2000, "grok-imagine-1.5": 5000,
		"dialogue-v3": 5000, "music-v1": 9999, "sound-effects-v2": 9999,
	}
	if len(promptLimits) != len(want) {
		t.Fatalf("prompt limits contain %d models, want %d: %v", len(promptLimits), len(want), promptLimits)
	}
	for model, limit := range want {
		if got, ok := PromptLimit(model); !ok || got != limit {
			t.Errorf("PromptLimit(%q)=(%d,%v), want (%d,true)", model, got, ok, limit)
		}
	}
}

func TestAllPromptLimitsReturnsCopy(t *testing.T) {
	limits := AllPromptLimits()
	limits["minimax-h3"] = 1
	if got, _ := PromptLimit("minimax-h3"); got != 2000 {
		t.Fatalf("caller mutated prompt limit: %d", got)
	}
}

func TestPromptCharactersCountsUnicodeCodePoints(t *testing.T) {
	if got := PromptCharacters("生A😀"); got != 3 {
		t.Fatalf("PromptCharacters()=%d, want 3", got)
	}
}
