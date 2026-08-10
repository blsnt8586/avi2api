package videospec

import "testing"

func TestPublicVideoSpecs(t *testing.T) {
	tests := []struct {
		model               string
		duration            int
		resolution          string
		referenceImages     int
		generateAudio       bool
		usesResolutionMode  bool
		promptMaxCharacters int
	}{
		{"seedance-2.0", 15, "2160p", 4, true, true, 5000},
		{"seedance-2.0-fast", 15, "720p", 4, true, true, 5000},
		{"seedance-2.0-mini", 15, "720p", 4, true, false, 5000},
		{"flux-3-video", 20, "1080p", 0, true, false, 5000},
		{"veo-3.1", 8, "2160p", 3, true, true, 9999},
		{"veo-3.1-fast", 6, "1080p", 0, true, true, 9999},
		{"kling-o3-omni", 15, "2160p", 7, true, false, 2500},
		{"minimax-h3", 15, "1440p", 5, true, false, 2000},
		{"grok-imagine-1.5", 15, "1080p", 0, true, false, 5000},
	}
	for _, test := range tests {
		spec, ok := Get(test.model)
		if !ok {
			t.Fatalf("missing spec for %s", test.model)
		}
		if !spec.SupportsDuration(test.duration) || !spec.SupportsResolution(test.resolution) || spec.MaxReferenceImages != test.referenceImages || spec.SupportsGenerateAudio != test.generateAudio || spec.UsesResolutionMode != test.usesResolutionMode || spec.PromptMaxCharacters != test.promptMaxCharacters {
			t.Fatalf("unexpected spec for %s: %+v", test.model, spec)
		}
	}
}

func TestSpecsRejectUnsupportedCombinations(t *testing.T) {
	veo, _ := Get("veo-3.1")
	if veo.SupportsDuration(5) || veo.SupportsResolution("480p") {
		t.Fatal("Veo accepted an unsupported duration or resolution")
	}
	for _, removed := range []string{"gemini-omni-flash", "veo-3.1-lite", "kling-3.0"} {
		if _, ok := Get(removed); ok {
			t.Fatalf("removed model %s still has a public video spec", removed)
		}
	}
	o3, ok := Get("kling-o3-omni")
	if !ok || !o3.SupportsSize("960x960") || !o3.SupportsSize("1440x1440") || !o3.SupportsSize("2880x2880") {
		t.Fatalf("unexpected Kling O3 Omni sizes: %+v", o3.Sizes)
	}
	if o3.MaxReferenceImagesWithVideo != 4 || o3.MaxReferenceVideos != 1 || o3.MinVideoDuration != 3 || o3.MaxVideoDuration != 10.05 || !o3.OmitDurationWithVideo || o3.SupportsVideoReferenceResolution("2160p") {
		t.Fatalf("unexpected Kling O3 Omni media contract: %+v", o3)
	}
	if o3.DefaultSize != "1920x1080" || o3.DefaultResolution != "1080p" || !o3.SupportsReferenceVideoDimensions(1920, 1080) || o3.SupportsReferenceVideoDimensions(640, 1080) || o3.SupportsReferenceVideoDimensions(2160, 3840) {
		t.Fatalf("unexpected Kling O3 Omni defaults or reference dimensions: %+v", o3)
	}
	h3, _ := Get("minimax-h3")
	if !h3.SupportsSize("3360x1440") || !h3.SupportsSize("1440x2560") || h3.SupportsSize("1280x720") {
		t.Fatalf("unexpected MiniMax H3 sizes: %+v", h3.Sizes)
	}
	if !h3.AlwaysGenerateAudio || h3.MaxReferenceAudios != 3 || h3.MaxAudioDuration != 15 || !h3.UsesExactDimensions {
		t.Fatalf("unexpected MiniMax H3 media contract: %+v", h3)
	}
	grok, _ := Get("grok-imagine-1.5")
	if !grok.SupportsSize("736x400") || !grok.SupportsSize("960x960") || !grok.SupportsSize("1072x1888") || grok.SupportsSize("0x0") {
		t.Fatalf("unexpected Grok Imagine 1.5 sizes: %+v", grok.Sizes)
	}
	if !grok.RequiresStartFrame || grok.SupportsEndFrame || !grok.UsesExactDimensions || grok.MaxReferenceImages != 0 || grok.MaxReferenceVideos != 0 || grok.MaxReferenceAudios != 0 {
		t.Fatalf("unexpected Grok Imagine 1.5 media contract: %+v", grok)
	}
	if resolution, ok := grok.ResolutionForSize("1424x1424"); !ok || resolution != "1080p" {
		t.Fatalf("unexpected Grok Imagine 1.5 size tier: resolution=%q ok=%v", resolution, ok)
	}
	if size, ok := grok.DefaultSizeForResolution("720p"); !ok || size != "1280x720" {
		t.Fatalf("unexpected Grok Imagine 1.5 default 720p size: size=%q ok=%v", size, ok)
	}
	flux3, _ := Get("flux-3-video")
	if !flux3.SupportsSize("1470x630") || !flux3.SupportsSize("2520x1080") || flux3.SupportsSize("1280x1280") {
		t.Fatalf("unexpected FLUX 3 Video sizes: %+v", flux3.Sizes)
	}
	if !flux3.UsesExactDimensions || flux3.MaxReferenceImages != 0 || flux3.MaxReferenceVideos != 1 || flux3.MaxReferenceAudios != 0 || flux3.MaxVideoDuration != 15.05 || flux3.MaxReferenceVideoBytes != 50_000_000 {
		t.Fatalf("unexpected FLUX 3 Video media contract: %+v", flux3)
	}
	if resolution, ok := flux3.ResolutionForSize("2520x1080"); !ok || resolution != "1080p" {
		t.Fatalf("unexpected FLUX 3 Video size tier: resolution=%q ok=%v", resolution, ok)
	}
	for _, model := range []string{"seedance-2.0", "seedance-2.0-fast", "seedance-2.0-mini"} {
		spec, _ := Get(model)
		if spec.MaxReferenceAudios != 1 || spec.MaxVideoDuration != 15 || spec.MaxAudioDuration != 15 {
			t.Fatalf("unexpected %s audio-reference contract: %+v", model, spec)
		}
	}
}
