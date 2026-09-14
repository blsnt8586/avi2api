package modelconstraints

import "unicode/utf8"

// PromptLimits is the single source of truth for public model prompt limits.
// Limits count Unicode code points, matching Leonardo's schema semantics.
var promptLimits = map[string]int{
	"gpt-image-2":       9999,
	"nano-banana-2":     9999,
	"nano-banana-pro":   9999,
	"seedream-5.0-pro":  9999,
	"flux-3-video":      5000,
	"seedance-2.0":      5000,
	"seedance-2.0-fast": 5000,
	"seedance-2.0-mini": 5000,
	"seedance-2.5":      5000,
	"veo-3.1":           9999,
	"veo-3.1-fast":      9999,
	"kling-o3-omni":     2500,
	"kling-3.0-omni":    2500,
	"minimax-h3":        2000,
	"grok-imagine-1.5":  5000,
	"gemini-omni-flash": 5000,
	"happy-horse-1.1":   5000,
	"kling-3.0":         5000,
	"kling-3.0-turbo":   5000,
	"hailuo-2.3":        5000,
	"wan-2.7":           5000,
	"dialogue-v3":       5000,
	"music-v1":          9999,
	"sound-effects-v2":  9999,
	"seedance_v2_mini":  4096,
}

func PromptLimit(model string) (int, bool) {
	limit, ok := promptLimits[model]
	return limit, ok
}

func AllPromptLimits() map[string]int {
	limits := make(map[string]int, len(promptLimits))
	for model, limit := range promptLimits {
		limits[model] = limit
	}
	return limits
}

func PromptCharacters(prompt string) int {
	return utf8.RuneCountInString(prompt)
}
