package httpapi

import (
	"strings"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
)

func TestNormalizeMiniMaxH3Options(t *testing.T) {
	spec, ok := videospec.Get("minimax-h3")
	if !ok {
		t.Fatal("missing MiniMax H3 spec")
	}
	request := domain.VideoRequest{Model: "minimax-h3"}
	if err := normalizeVideoOptions(&request, spec); err != nil {
		t.Fatal(err)
	}
	if request.Size != "2560x1440" || request.Resolution != "1440p" || request.Duration != 5 || request.GenerateAudio == nil || !*request.GenerateAudio {
		t.Fatalf("unexpected defaults: %+v", request)
	}
}

func TestNormalizeMiniMaxH3RejectsUnsupportedParameters(t *testing.T) {
	spec, _ := videospec.Get("minimax-h3")
	disabled := false
	tests := []domain.VideoRequest{
		{Model: "minimax-h3", Size: "1280x720"},
		{Model: "minimax-h3", Duration: 4},
		{Model: "minimax-h3", Resolution: "1080p"},
		{Model: "minimax-h3", GenerateAudio: &disabled},
	}
	for _, request := range tests {
		if err := normalizeVideoOptions(&request, spec); err == nil {
			t.Fatalf("accepted unsupported request: %+v", request)
		}
	}
	if err := normalizeVideoOptions(&domain.VideoRequest{Model: "minimax-h3", GenerateAudio: &disabled}, spec); err == nil || !strings.Contains(err.Error(), "cannot be false") {
		t.Fatalf("unexpected generate_audio error: %v", err)
	}
}

func TestNormalizeGrokImagine15Options(t *testing.T) {
	spec, ok := videospec.Get("grok-imagine-1.5")
	if !ok {
		t.Fatal("missing Grok Imagine 1.5 spec")
	}
	request := domain.VideoRequest{Model: "grok-imagine-1.5"}
	if err := normalizeVideoOptions(&request, spec); err != nil {
		t.Fatal(err)
	}
	if request.Size != "736x400" || request.Resolution != "480p" || request.Duration != 6 || request.GenerateAudio == nil || !*request.GenerateAudio {
		t.Fatalf("unexpected defaults: %+v", request)
	}

	high := domain.VideoRequest{Model: "grok-imagine-1.5", Size: "1424x1424"}
	if err := normalizeVideoOptions(&high, spec); err != nil || high.Resolution != "1080p" {
		t.Fatalf("size did not select its price tier: request=%+v err=%v", high, err)
	}
	portrait := domain.VideoRequest{Model: "grok-imagine-1.5", Resolution: "720p"}
	if err := normalizeVideoOptions(&portrait, spec); err != nil || portrait.Size != "1280x720" {
		t.Fatalf("resolution did not select its default size: request=%+v err=%v", portrait, err)
	}
	disabled := false
	withoutAudio := domain.VideoRequest{Model: "grok-imagine-1.5", GenerateAudio: &disabled}
	if err := normalizeVideoOptions(&withoutAudio, spec); err != nil {
		t.Fatalf("Grok Schema allows motion_has_audio=false: %v", err)
	}
}

func TestNormalizeGrokImagine15RejectsMismatchedSizeTier(t *testing.T) {
	spec, _ := videospec.Get("grok-imagine-1.5")
	request := domain.VideoRequest{Model: "grok-imagine-1.5", Size: "1888x1072", Resolution: "480p"}
	if err := normalizeVideoOptions(&request, spec); err == nil || !strings.Contains(err.Error(), "requires resolution 1080p") {
		t.Fatalf("unexpected size-tier validation error: %v", err)
	}
}

func TestValidateGrokImagine15FrameContract(t *testing.T) {
	spec, _ := videospec.Get("grok-imagine-1.5")
	if err := validateVideoFrameContract(domain.VideoRequest{Model: "grok-imagine-1.5"}, spec); err == nil || !strings.Contains(err.Error(), "requires start_frame") {
		t.Fatalf("missing start frame was accepted: %v", err)
	}
	start := &domain.SourceMedia{Filename: "start.png"}
	if err := validateVideoFrameContract(domain.VideoRequest{Model: "grok-imagine-1.5", StartFrame: start}, spec); err != nil {
		t.Fatalf("valid start-frame request was rejected: %v", err)
	}
	end := &domain.SourceMedia{Filename: "end.png"}
	if err := validateVideoFrameContract(domain.VideoRequest{Model: "grok-imagine-1.5", StartFrame: start, EndFrame: end}, spec); err == nil || !strings.Contains(err.Error(), "does not support end_frame") {
		t.Fatalf("unsupported end frame was accepted: %v", err)
	}
}

func TestNormalizeKlingO3OmniOptions(t *testing.T) {
	spec, ok := videospec.Get("kling-o3-omni")
	if !ok {
		t.Fatal("missing Kling O3 Omni spec")
	}
	request := domain.VideoRequest{Model: "kling-o3-omni", Size: "1440x1440"}
	if err := normalizeVideoOptions(&request, spec); err != nil {
		t.Fatal(err)
	}
	if request.Resolution != "1080p" || request.Duration != 5 || request.GenerateAudio == nil || !*request.GenerateAudio {
		t.Fatalf("unexpected Kling O3 Omni defaults: %+v", request)
	}

	videoReference := domain.VideoRequest{
		Model: "kling-o3-omni", Size: "3840x2160",
		ReferenceVideos: []domain.SourceMedia{{Filename: "reference.mp4"}},
	}
	if err := normalizeVideoOptions(&videoReference, spec); err == nil || !strings.Contains(err.Error(), "video references") {
		t.Fatalf("accepted Kling O3 Omni 4K video reference: %v", err)
	}
	tooLong := domain.VideoRequest{
		Model: "kling-o3-omni", Duration: 11,
		ReferenceVideos: []domain.SourceMedia{{Filename: "reference.mp4"}},
	}
	if err := normalizeVideoOptions(&tooLong, spec); err == nil || !strings.Contains(err.Error(), "must not exceed") {
		t.Fatalf("accepted Kling O3 Omni 11-second video reference: %v", err)
	}
}
