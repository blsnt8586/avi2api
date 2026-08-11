package pricing

import (
	"context"
	"os"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/migrate"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
)

func TestMigratedRulesCoverEveryPublishedCostCombination(t *testing.T) {
	databaseURL := os.Getenv("LEO_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LEO_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := migrate.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	rules, err := store.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer rules.Close()

	imageRequests := []domain.ImageRequest{
		{Model: "gpt-image-2", Size: "1024x1024", Quality: "low"},
		{Model: "gpt-image-2", Size: "1024x1024", Quality: "medium"},
		{Model: "gpt-image-2", Size: "1024x1024", Quality: "high"},
		{Model: "gpt-image-2", Size: "2048x2048", Quality: "low"},
		{Model: "gpt-image-2", Size: "2048x2048", Quality: "medium"},
		{Model: "gpt-image-2", Size: "2048x2048", Quality: "high"},
		{Model: "gpt-image-2", Size: "2880x2880", Quality: "low"},
		{Model: "gpt-image-2", Size: "2880x2880", Quality: "medium"},
		{Model: "gpt-image-2", Size: "2880x2880", Quality: "high"},
	}
	for _, model := range []string{"nano-banana-2", "nano-banana-pro"} {
		for _, size := range []string{"1024x1024", "2048x2048", "4096x4096"} {
			imageRequests = append(imageRequests, domain.ImageRequest{Model: model, Size: size})
		}
	}
	for _, size := range []string{"1024x1024", "2048x2048"} {
		imageRequests = append(imageRequests, domain.ImageRequest{Model: "seedream-5.0-pro", Size: size})
	}
	for _, request := range imageRequests {
		estimate, err := Image(ctx, rules, request)
		if err != nil || estimate.Tokens <= 0 {
			t.Fatalf("missing image price for %+v: estimate=%+v err=%v", request, estimate, err)
		}
	}

	videoModels := []string{
		"seedance-2.0", "seedance-2.0-fast", "seedance-2.0-mini", "seedance-2.5", "flux-3-video",
		"veo-3.1", "veo-3.1-fast", "kling-o3-omni", "minimax-h3", "grok-imagine-1.5",
	}
	for _, model := range videoModels {
		spec, ok := videospec.Get(model)
		if !ok {
			t.Fatalf("missing video spec for %s", model)
		}
		for _, duration := range spec.Durations {
			for _, resolution := range spec.Resolutions {
				requests := []domain.VideoRequest{{Model: model, Duration: duration, Resolution: resolution}}
				if spec.SupportsGenerateAudio && !spec.AlwaysGenerateAudio {
					disabled := false
					requests = append(requests, domain.VideoRequest{Model: model, Duration: duration, Resolution: resolution, GenerateAudio: &disabled})
				}
				if spec.MaxReferenceVideos > 0 {
					requests = append(requests, domain.VideoRequest{Model: model, Duration: duration, Resolution: resolution, ReferenceVideos: []domain.SourceMedia{{Path: "fixture.mp4"}}})
				}
				for _, request := range requests {
					estimate, err := Video(ctx, rules, request)
					if err != nil || estimate.Tokens <= 0 || estimate.UnitTokens <= 0 {
						t.Fatalf("missing video price for %+v: estimate=%+v err=%v", request, estimate, err)
					}
				}
			}
		}
	}

	audioRequests := []domain.AudioRequest{{Model: "dialogue-v3", Prompt: "fixture", N: 1}}
	for duration := 1; duration <= 10; duration++ {
		audioRequests = append(audioRequests, domain.AudioRequest{Model: "music-v1", DurationMinutes: duration, N: 1})
	}
	for duration := 1; duration <= 22; duration++ {
		audioRequests = append(audioRequests, domain.AudioRequest{Model: "sound-effects-v2", Duration: duration, N: 1})
	}
	for _, request := range audioRequests {
		estimate, err := Audio(ctx, rules, request)
		if err != nil || estimate.Tokens <= 0 {
			t.Fatalf("missing audio price for %+v: estimate=%+v err=%v", request, estimate, err)
		}
	}
}
