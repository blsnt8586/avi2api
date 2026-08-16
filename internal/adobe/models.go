package adobe

import (
	"fmt"
	"strings"
)

const (
	AdobeGPTImage2        = "gpt-image-2"
	AdobeNanoBanana2      = "nano-banana-2"
	AdobeKling30Omni      = "kling-3.0-omni"
	AdobeVeo31            = "veo-3.1"
	AdobeVeo31Fast        = "veo-3.1-fast"
	AdobeSeedance20       = "seedance-2.0"
	AdobeSeedance20Fast   = "seedance-2.0-fast"
	AdobeKlingWorkflowT2V = "t2v"
	AdobeKlingWorkflowI2V = "i2v"
	AdobeKlingWorkflowRTV = "rtv"
)

type ImagePricingTier struct {
	Size       string
	Resolution string
}

type ModelSpec struct {
	PublicID           string
	DisplayName        string
	Kind               string
	ModelID            string
	ModelVersion       string
	FeatureID          string
	ImageTiers         []ImagePricingTier
	Qualities          []string
	Durations          []int
	Resolutions        []string
	Workflows          []string
	CostUsesResolution bool
	CostUsesWorkflow   bool
}

var modelSpecs = []ModelSpec{
	{
		PublicID: AdobeGPTImage2, DisplayName: "GPT Image 2", Kind: "image", ModelID: "gpt-image", ModelVersion: "2",
		FeatureID:  "firefly_3p:external:gpt_image_2",
		ImageTiers: []ImagePricingTier{{Size: "1024x1024", Resolution: "1K"}, {Size: "2048x2048", Resolution: "2K"}, {Size: "2880x2880", Resolution: "4K"}},
		Qualities:  []string{"low", "medium", "high"},
	},
	{
		PublicID: AdobeNanoBanana2, DisplayName: "Nano Banana 2", Kind: "image", ModelID: "gemini-flash", ModelVersion: "nano-banana-3",
		FeatureID:  "firefly_3p:external:nano_banana_3",
		ImageTiers: []ImagePricingTier{{Size: "1024x1024", Resolution: "1K"}, {Size: "2048x2048", Resolution: "2K"}, {Size: "4096x4096", Resolution: "4K"}},
	},
	{
		PublicID: AdobeVeo31, DisplayName: "Veo 3.1", Kind: "video", ModelID: "veo", ModelVersion: "3.1-generate",
		FeatureID: "firefly_3p:external:veo_3", Durations: []int{4, 6, 8}, Resolutions: []string{"720p", "1080p"},
	},
	{
		PublicID: AdobeVeo31Fast, DisplayName: "Veo 3.1 Fast", Kind: "video", ModelID: "veo", ModelVersion: "3.1-fast-generate",
		FeatureID: "firefly_3p:external:veo_3_fast", Durations: []int{4, 6, 8}, Resolutions: []string{"720p", "1080p"},
	},
	{
		PublicID: AdobeSeedance20, DisplayName: "Seedance 2.0", Kind: "video", ModelID: "seedance", ModelVersion: "seedance_2.0",
		FeatureID: "firefly_3p:external:seedance_2_0", Durations: integerRange(4, 15), Resolutions: []string{"480p", "720p", "1080p"}, CostUsesResolution: true,
	},
	{
		PublicID: AdobeSeedance20Fast, DisplayName: "Seedance 2.0 Fast", Kind: "video", ModelID: "seedance", ModelVersion: "seedance_2.0_fast",
		FeatureID: "firefly_3p:external:seedance_2_0_fast", Durations: integerRange(4, 15), Resolutions: []string{"480p", "720p"}, CostUsesResolution: true,
	},
	{
		PublicID: AdobeKling30Omni, DisplayName: "Kling 3.0 Omni", Kind: "video", ModelID: "kling", ModelVersion: "kling_v3_omni",
		FeatureID: "firefly_3p:external:kling_o3_tir2v", Durations: []int{5, 10, 15}, Resolutions: []string{"720p", "1080p"},
		Workflows: []string{AdobeKlingWorkflowT2V, AdobeKlingWorkflowI2V, AdobeKlingWorkflowRTV}, CostUsesResolution: true, CostUsesWorkflow: true,
	},
}

func Model(publicID string) (ModelSpec, bool) {
	for _, spec := range modelSpecs {
		if spec.PublicID == publicID {
			return spec, true
		}
	}
	return ModelSpec{}, false
}

func Models(kind string) []ModelSpec {
	result := make([]ModelSpec, 0, len(modelSpecs))
	for _, spec := range modelSpecs {
		if kind == "" || spec.Kind == kind {
			result = append(result, spec)
		}
	}
	return result
}

func (m ModelSpec) ImageCostRequest(resolution, quality string) (CostRequest, error) {
	if m.Kind != "image" {
		return CostRequest{}, fmt.Errorf("%s is not an Adobe image model", m.PublicID)
	}
	metadata := map[string]any{"imageResolution": strings.ToUpper(strings.TrimSpace(resolution)), "enableCreditType": true}
	if len(m.Qualities) > 0 {
		metadata["generationQuality"] = strings.ToLower(strings.TrimSpace(quality))
	}
	return CostRequest{Features: map[string]int{m.FeatureID: 1}, Metadata: metadata}, nil
}

func (m ModelSpec) VideoCostRequest(duration int, resolution, workflow string) (CostRequest, error) {
	if m.Kind != "video" {
		return CostRequest{}, fmt.Errorf("%s is not an Adobe video model", m.PublicID)
	}
	metadata := map[string]any{"videoDuration": fmt.Sprintf("%d.0", duration), "generateAudio": false}
	if m.CostUsesResolution {
		metadata["videoResolution"] = strings.ToLower(strings.TrimSpace(resolution))
	}
	if m.CostUsesWorkflow {
		metadata["workflow"] = strings.ToLower(strings.TrimSpace(workflow))
	}
	return CostRequest{Features: map[string]int{m.FeatureID: 1}, Metadata: metadata}, nil
}

func integerRange(minimum, maximum int) []int {
	values := make([]int, 0, maximum-minimum+1)
	for value := minimum; value <= maximum; value++ {
		values = append(values, value)
	}
	return values
}
