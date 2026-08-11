package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/imageopts"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
)

var ErrCostUnavailable = errors.New("cost unavailable for model parameters")

type RuleStore interface {
	FindModelCostRule(context.Context, string, string, string, string, string, int) (domain.ModelCostRule, error)
}

type Estimate struct {
	Tokens       int64
	UnitTokens   int64
	RuleID       int64
	RuleSize     string
	PriceVersion string
	Source       string
}

func Image(ctx context.Context, rules RuleStore, request domain.ImageRequest) (Estimate, error) {
	size, err := imagePricingSize(request.Model, request.Size)
	if err != nil {
		return Estimate{}, ErrCostUnavailable
	}
	quality := ""
	if request.Model == "gpt-image-2" {
		quality = strings.ToLower(strings.TrimSpace(request.Quality))
		if quality == "" || quality == "auto" {
			quality = "low"
		}
	}
	rule, err := rules.FindModelCostRule(ctx, "image", request.Model, size, quality, "", 0)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Estimate{}, ErrCostUnavailable
		}
		return Estimate{}, err
	}
	quantity := request.N
	if quantity < 1 {
		quantity = 1
	}
	unitTokens := rule.UnitTokens
	if request.Model == imageopts.GPTImage2 {
		width, height, parseErr := imageopts.ParseSize(request.Model, request.Size)
		if parseErr != nil {
			return Estimate{}, ErrCostUnavailable
		}
		unitTokens, err = gptImage2Tokens(width, height, quality)
		if err != nil {
			return Estimate{}, err
		}
	}
	return Estimate{
		Tokens: unitTokens * int64(quantity), UnitTokens: unitTokens,
		RuleID: rule.ID, RuleSize: size, PriceVersion: rule.PriceVersion, Source: rule.Source,
	}, nil
}

func imagePricingSize(model, requested string) (string, error) {
	size := strings.ToLower(strings.TrimSpace(requested))
	if size == "" || size == "auto" {
		size = "1024x1024"
	}
	width, height, err := imageopts.ParseSize(model, size)
	if err != nil {
		return "", err
	}
	switch model {
	case imageopts.GPTImage2:
		pixels := int64(width) * int64(height)
		for _, edge := range []int{1024, 2048, 2880} {
			if pixels <= int64(edge)*int64(edge) {
				return fmt.Sprintf("%dx%d", edge, edge), nil
			}
		}
		return "", ErrCostUnavailable
	case imageopts.NanoBanana2, imageopts.NanoBananaPro:
		if width >= 3072 && width != 3168 {
			return "4096x4096", nil
		}
		if width >= 1536 && width != 1584 {
			return "2048x2048", nil
		}
		return "1024x1024", nil
	case imageopts.Seedream50Pro:
		if (width >= 2016 && height >= 1153) || (width >= 1153 && height >= 2016) {
			return "2048x2048", nil
		}
		return "1024x1024", nil
	default:
		return size, nil
	}
}

func gptImage2Tokens(width, height int, quality string) (int64, error) {
	factors := map[string]int64{"low": 1000, "medium": 8833, "high": 35167}
	factor, ok := factors[quality]
	if !ok {
		return 0, ErrCostUnavailable
	}
	numerator := int64(width) * int64(height) * 7 * factor
	const denominator int64 = 1_000_000 * 1000
	return (numerator + denominator - 1) / denominator, nil
}

func Video(ctx context.Context, rules RuleStore, request domain.VideoRequest) (Estimate, error) {
	spec, ok := videospec.Get(request.Model)
	if !ok {
		return Estimate{}, ErrCostUnavailable
	}
	resolution := strings.ToLower(strings.TrimSpace(request.Resolution))
	if resolution == "" {
		if mapped, mappedOK := spec.ResolutionForSize(strings.ToLower(strings.TrimSpace(request.Size))); mappedOK {
			resolution = mapped
		} else {
			resolution = spec.DefaultResolution
		}
	}
	duration := request.Duration
	if duration == 0 {
		duration = spec.DefaultDuration
	}
	if !spec.SupportsResolution(resolution) || !spec.SupportsDuration(duration) {
		return Estimate{}, ErrCostUnavailable
	}
	if len(spec.ResolutionBySize) > 0 {
		size := strings.ToLower(strings.TrimSpace(request.Size))
		if size == "" {
			if resolved, resolvedOK := spec.DefaultSizeForResolution(resolution); resolvedOK {
				size = resolved
			} else {
				size = spec.DefaultSize
			}
		}
		expected, mapped := spec.ResolutionForSize(size)
		if !mapped || expected != resolution {
			return Estimate{}, ErrCostUnavailable
		}
	}
	rule, err := rules.FindModelCostRule(ctx, "video", request.Model, "", "", resolution, duration)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Estimate{}, ErrCostUnavailable
		}
		return Estimate{}, err
	}
	tokens := rule.UnitTokens
	if len(request.ReferenceVideos) > 0 {
		if request.Model == "seedance-2.5" {
			perSecond := int64(258)
			if resolution == "720p" {
				perSecond = 466
			}
			tokens = perSecond * int64(duration)
		} else if request.Model == "kling-o3-omni" {
			// Kling O3 Omni bills a video reference at 1.5 * 168 credits
			// per input second. Admission requires duration to match the
			// uploaded reference before the upstream request is submitted.
			tokens = int64(252 * duration)
		} else if request.Model == "flux-3-video" {
			perSecond := int64(543)
			if resolution == "1080p" {
				perSecond = 682
			}
			tokens = perSecond * int64(duration)
		} else {
			// Seedance 2.0's video-reference modifier is:
			// base * ceil(duration * 5/3) / duration * 3/5.
			// Round the reservation upward because the stored base rule is already
			// an integer projection of the upstream formula.
			steps := int64((duration*5 + 2) / 3)
			numerator := tokens * steps * 3
			denominator := int64(duration * 5)
			tokens = (numerator + denominator - 1) / denominator
		}
	}
	if request.GenerateAudio != nil && !*request.GenerateAudio {
		// Stored rules represent each model's default motion_has_audio=true cost.
		// These inverse modifiers come from Leonardo schema 1.232.1.
		switch request.Model {
		case "veo-3.1":
			tokens /= 2
		case "veo-3.1-fast":
			tokens -= int64(50 * duration)
		case "kling-o3-omni":
			if len(request.ReferenceVideos) == 0 {
				switch resolution {
				case "720p":
					tokens = tokens * 3 / 4
				case "1080p":
					tokens = tokens * 4 / 5
				}
			}
		}
	}
	return Estimate{
		Tokens: tokens, UnitTokens: rule.UnitTokens, RuleID: rule.ID,
		PriceVersion: rule.PriceVersion, Source: rule.Source,
	}, nil
}

func Audio(ctx context.Context, rules RuleStore, request domain.AudioRequest) (Estimate, error) {
	duration := request.Duration
	if request.Model == "music-v1" {
		duration = request.DurationMinutes
	}
	rule, err := rules.FindModelCostRule(ctx, "audio", request.Model, "", "", "", duration)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Estimate{}, ErrCostUnavailable
		}
		return Estimate{}, err
	}
	quantity := request.N
	if quantity < 1 {
		quantity = 1
	}
	multiplier := int64(quantity)
	if request.Model == "dialogue-v3" {
		multiplier *= int64((utf8.RuneCountInString(request.Prompt) + 999) / 1000)
	}
	return Estimate{Tokens: rule.UnitTokens * multiplier, RuleID: rule.ID}, nil
}
