package adobe

import "testing"

func TestPublicAdobeModelMappings(t *testing.T) {
	tests := []struct {
		publicID     string
		kind         string
		modelID      string
		modelVersion string
		featureID    string
	}{
		{AdobeGPTImage2, "image", "gpt-image", "2", "firefly_3p:external:gpt_image_2"},
		{AdobeNanoBanana2, "image", "gemini-flash", "nano-banana-3", "firefly_3p:external:nano_banana_3"},
		{AdobeKling30Omni, "video", "kling", "kling_v3_omni", "firefly_3p:external:kling_o3_tir2v"},
		{AdobeVeo31, "video", "veo", "3.1-generate", "firefly_3p:external:veo_3"},
		{AdobeVeo31Fast, "video", "veo", "3.1-fast-generate", "firefly_3p:external:veo_3_fast"},
		{AdobeSeedance20, "video", "seedance", "seedance_2.0", "firefly_3p:external:seedance_2_0"},
		{AdobeSeedance20Fast, "video", "seedance", "seedance_2.0_fast", "firefly_3p:external:seedance_2_0_fast"},
	}
	for _, test := range tests {
		spec, ok := Model(test.publicID)
		if !ok || spec.Kind != test.kind || spec.ModelID != test.modelID || spec.ModelVersion != test.modelVersion || spec.FeatureID != test.featureID {
			t.Fatalf("unexpected mapping for %s: %+v", test.publicID, spec)
		}
	}
	if len(Models("image")) != 2 || len(Models("video")) != 5 {
		t.Fatalf("unexpected Adobe model counts: image=%d video=%d", len(Models("image")), len(Models("video")))
	}
}

func TestAdobeCostMetadata(t *testing.T) {
	nano, _ := Model(AdobeNanoBanana2)
	imageCost, err := nano.ImageCostRequest("4K", "")
	if err != nil || imageCost.Features[nano.FeatureID] != 1 || imageCost.Metadata["imageResolution"] != "4K" || imageCost.Metadata["enableCreditType"] != true {
		t.Fatalf("unexpected Nano Banana cost request: %+v err=%v", imageCost, err)
	}
	kling, _ := Model(AdobeKling30Omni)
	videoCost, err := kling.VideoCostRequest(10, "1080p", AdobeKlingWorkflowRTV)
	if err != nil || videoCost.Metadata["videoDuration"] != "10.0" || videoCost.Metadata["videoResolution"] != "1080p" || videoCost.Metadata["workflow"] != "rtv" || videoCost.Metadata["generateAudio"] != false {
		t.Fatalf("unexpected Kling cost request: %+v err=%v", videoCost, err)
	}
}
