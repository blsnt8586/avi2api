package creativefabrica

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The Studio APIs expose protobuf enum names over Connect JSON.  Keep the
// public model IDs separate from those enum names: callers and database rows
// use the stable catalog ID, while only this adapter knows the wire spelling.
var imageModelEnums = map[string]string{
	"flux-2-max":             "FLOW_MODEL_FLUX_2_MAX",
	"flux-2-pro":             "FLOW_MODEL_FLUX_2_PRO",
	"flux-2-klein":           "FLOW_MODEL_FLUX_2_KLEIN",
	"grok":                   "FLOW_MODEL_GROK_2_IMAGE_GEN_1212",
	"grok-image":             "FLOW_MODEL_GROK_2_IMAGE_GEN_1212",
	"ideogram-p-image":       "FLOW_MODEL_IDEOGRAM_P_IMAGE",
	"ideogram-v4":            "FLOW_MODEL_IDEOGRAM_V4",
	"kling-image-o1":         "FLOW_MODEL_KLING_IMAGE_O1",
	"kling-v2-1-image":       "FLOW_MODEL_KLING_V2_1_IMAGE",
	"kling-v2.1-image":       "FLOW_MODEL_KLING_V2_1_IMAGE",
	"kling-v3-image":         "FLOW_MODEL_KLING_V3_IMAGE",
	"kling-v3-omni-image":    "FLOW_MODEL_KLING_V3_OMNI_IMAGE",
	"kling-v3-omni":          "FLOW_MODEL_KLING_V3_OMNI_IMAGE",
	"nano-banana":            "FLOW_MODEL_NANO_BANANA",
	"nano-banana-2":          "FLOW_MODEL_NANO_BANANA_2",
	"nano-banana-flash-lite": "FLOW_MODEL_NANO_BANANA_FLASH_LITE",
	// Keep the historical typo as an input alias, but never expose it as the
	// catalog/public model identifier.
	"nano-banano-pro":       "FLOW_MODEL_NANO_PRO",
	"nano-banana-pro":       "FLOW_MODEL_NANO_PRO",
	"openai-gpt-image-2":    "FLOW_MODEL_OPENAI_GPT_IMAGE_2",
	"gpt-image-2":           "FLOW_MODEL_OPENAI_GPT_IMAGE_2",
	"qwen-image-2-0-pro":    "FLOW_MODEL_QWEN_IMAGE_2_0_PRO",
	"qwen-image-2.0-pro":    "FLOW_MODEL_QWEN_IMAGE_2_0_PRO",
	"qwen-image-3":          "FLOW_MODEL_QWEN_IMAGE_3",
	"qwen-image-3-pro":      "FLOW_MODEL_QWEN_IMAGE_3_PRO",
	"qwen-image-edit-spicy": "FLOW_MODEL_QWEN_IMAGE_EDIT_SPICY",
	"recraft-v4":            "FLOW_MODEL_RECRAFT_V4",
	"recraft-v4-svg":        "FLOW_MODEL_RECRAFT_V4_SVG",
	"recraft":               "FLOW_MODEL_RECRAFT_V4",
	"seedream-4-0":          "FLOW_MODEL_SEEDREAM_4_0",
	"seedream-4.0":          "FLOW_MODEL_SEEDREAM_4_0",
	"seedream-4-5":          "FLOW_MODEL_SEEDREAM_4_5",
	"seedream-4.5":          "FLOW_MODEL_SEEDREAM_4_5",
	"seedream-5-lite":       "FLOW_MODEL_SEEDREAM_5_LITE",
	"seedream-5.0-lite":     "FLOW_MODEL_SEEDREAM_5_LITE",
	"seedream-5-pro":        "FLOW_MODEL_SEEDREAM_5_PRO",
	"seedream-5.0-pro":      "FLOW_MODEL_SEEDREAM_5_PRO",
	"wan-2-7-pro-image":     "FLOW_MODEL_WAN_2_7_PRO_IMAGE",
	"wan-2.7-pro-image":     "FLOW_MODEL_WAN_2_7_PRO_IMAGE",
	"wan-2.7-pro":           "FLOW_MODEL_WAN_2_7_PRO_IMAGE",
	"z-image-spicy":         "FLOW_MODEL_Z_IMAGE_SPICY",
	"quiverai-arrow-1-1":    "FLOW_MODEL_QUIVERAI_ARROW_1_1",
}

var videoModelEnums = map[string]string{
	"veo_31_fast_generate_preview":         "VIDEO_GENERATOR_MODEL_VEO_31_FAST_GENERATE_PREVIEW",
	"veo-3.1-fast":                         "VIDEO_GENERATOR_MODEL_VEO_31_FAST_GENERATE_PREVIEW",
	"grok_imagine_video":                   "VIDEO_GENERATOR_MODEL_GROK_IMAGINE_VIDEO",
	"grok-imagine-video":                   "VIDEO_GENERATOR_MODEL_GROK_IMAGINE_VIDEO",
	"grok_imagine_video_1_5":               "VIDEO_GENERATOR_MODEL_GROK_IMAGINE_VIDEO_1_5",
	"grok-imagine-video-1.5":               "VIDEO_GENERATOR_MODEL_GROK_IMAGINE_VIDEO_1_5",
	"bytedance/seedance-2.0":               "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDDREAM_2",
	"seedance_two_point_zero":              "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDDREAM_2",
	"seedance-2.0":                         "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDDREAM_2",
	"seedance_v2_fast":                     "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_2_FAST",
	"seedance-2.0-fast":                    "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_2_FAST",
	"seedance_v2_mini":                     "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_2_MINI",
	"seedance-2.0-mini":                    "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_2_MINI",
	"seedance_one_five_pro":                "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_1_5_PRO",
	"seedance-1.5-pro":                     "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_1_5_PRO",
	"seedance_v2_5":                        "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_2_5",
	"seedance-2.5":                         "VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_2_5",
	"kling_ai_v3":                          "VIDEO_GENERATOR_MODEL_KLING_AI_V3",
	"kling-ai-v3":                          "VIDEO_GENERATOR_MODEL_KLING_AI_V3",
	"kling_ai_v3_motion_control":           "VIDEO_GENERATOR_MODEL_KLING_AI_V3_MOTION_CONTROL",
	"kling-motion-control":                 "VIDEO_GENERATOR_MODEL_KLING_AI_V3_MOTION_CONTROL",
	"kling_ai_v3_turbo":                    "VIDEO_GENERATOR_MODEL_KLING_AI_V3_TURBO",
	"kling-v3-turbo":                       "VIDEO_GENERATOR_MODEL_KLING_AI_V3_TURBO",
	"kling_v3_omni":                        "VIDEO_GENERATOR_MODEL_KLING_V3_OMNI",
	"kling-v3-omni":                        "VIDEO_GENERATOR_MODEL_KLING_V3_OMNI",
	"alibaba_happy_horse_v1":               "VIDEO_GENERATOR_MODEL_ALIBABA_HAPPY_HORSE_V1",
	"happy-horse-1.0":                      "VIDEO_GENERATOR_MODEL_ALIBABA_HAPPY_HORSE_V1",
	"alibaba_happy_horse_v1_1":             "VIDEO_GENERATOR_MODEL_ALIBABA_HAPPY_HORSE_V1_1",
	"happy-horse-1.1":                      "VIDEO_GENERATOR_MODEL_ALIBABA_HAPPY_HORSE_V1_1",
	"alibaba_wan_2_7":                      "VIDEO_GENERATOR_MODEL_ALIBABA_WAN_2_7",
	"wan-2.7":                              "VIDEO_GENERATOR_MODEL_ALIBABA_WAN_2_7",
	"mulerouter_wan_2_7_spicy":             "VIDEO_GENERATOR_MODEL_MULEROUTER_WAN_2_7_SPICY",
	"wan-2.7-spicy":                        "VIDEO_GENERATOR_MODEL_MULEROUTER_WAN_2_7_SPICY",
	"fal_ltx_2_3":                          "VIDEO_GENERATOR_MODEL_FAL_LTX_2_3",
	"ltx-2.3":                              "VIDEO_GENERATOR_MODEL_FAL_LTX_2_3",
	"fal_pixverse_v6":                      "VIDEO_GENERATOR_MODEL_FAL_PIXVERSE_V6",
	"pixverse-v6":                          "VIDEO_GENERATOR_MODEL_FAL_PIXVERSE_V6",
	"fal_pixverse_c1":                      "VIDEO_GENERATOR_MODEL_FAL_PIXVERSE_C1",
	"pixverse-c1":                          "VIDEO_GENERATOR_MODEL_FAL_PIXVERSE_C1",
	"heygen_avatar_5":                      "VIDEO_GENERATOR_MODEL_HEYGEN_AVATAR_5",
	"heygen-avatar-5":                      "VIDEO_GENERATOR_MODEL_HEYGEN_AVATAR_5",
	"gemini_omni_flash":                    "VIDEO_GENERATOR_MODEL_GEMINI_OMNI_FLASH",
	"gemini-omni-flash":                    "VIDEO_GENERATOR_MODEL_GEMINI_OMNI_FLASH",
	"gemini_omni_flash_1_1":                "VIDEO_GENERATOR_MODEL_GEMINI_OMNI_FLASH_1_1",
	"gemini-omni-flash-1.1":                "VIDEO_GENERATOR_MODEL_GEMINI_OMNI_FLASH_1_1",
	"gemini_omni_flash_1_1_video_extended": "VIDEO_GENERATOR_MODEL_GEMINI_OMNI_FLASH_1_1_VIDEO_EXTENDED",
	"gemini-omni-flash-1.1-extend":         "VIDEO_GENERATOR_MODEL_GEMINI_OMNI_FLASH_1_1_VIDEO_EXTENDED",
	"luma_ray_2":                           "VIDEO_GENERATOR_MODEL_LUMA_RAY_2",
	"luma-ray-2":                           "VIDEO_GENERATOR_MODEL_LUMA_RAY_2",
	"luma_ray_3_2":                         "VIDEO_GENERATOR_MODEL_LUMA_RAY_3_2",
	"luma-ray-3.2":                         "VIDEO_GENERATOR_MODEL_LUMA_RAY_3_2",
	"minimax_hailuo_v3":                    "VIDEO_GENERATOR_MODEL_MINIMAX_HAILUO_V3",
	"minimax-hailuo-v3":                    "VIDEO_GENERATOR_MODEL_MINIMAX_HAILUO_V3",
	"minimax_hailuo_v3_max":                "VIDEO_GENERATOR_MODEL_MINIMAX_HAILUO_V3_MAX",
	"minimax-h3-max":                       "VIDEO_GENERATOR_MODEL_MINIMAX_HAILUO_V3_MAX",
	"flux_3":                               "VIDEO_GENERATOR_MODEL_FLUX_3",
	"flux-3":                               "VIDEO_GENERATOR_MODEL_FLUX_3",
	"runway_gen_4_5":                       "VIDEO_GENERATOR_MODEL_RUNWAY_GEN_4_5",
	"runway-gen-4.5":                       "VIDEO_GENERATOR_MODEL_RUNWAY_GEN_4_5",
	"pika_v2_5":                            "VIDEO_GENERATOR_MODEL_PIKA_2_5",
	"pika-2.5":                             "VIDEO_GENERATOR_MODEL_PIKA_2_5",
	"alibaba_wan_3_0":                      "VIDEO_GENERATOR_MODEL_ALIBABA_WAN_3_0",
	"wan_3_0":                              "VIDEO_GENERATOR_MODEL_ALIBABA_WAN_3_0",
	"wan-3.0":                              "VIDEO_GENERATOR_MODEL_ALIBABA_WAN_3_0",
	"alibaba_wan_3_0_prime":                "VIDEO_GENERATOR_MODEL_ALIBABA_WAN_3_0_PRIME",
	"wan_3_0_prime":                        "VIDEO_GENERATOR_MODEL_ALIBABA_WAN_3_0_PRIME",
	"wan-3.0-prime":                        "VIDEO_GENERATOR_MODEL_ALIBABA_WAN_3_0_PRIME",
	"mulerouter_berry_1_0":                 "VIDEO_GENERATOR_MODEL_MULEROUTER_BERRY_1_0",
	"berry_1_0":                            "VIDEO_GENERATOR_MODEL_MULEROUTER_BERRY_1_0",
	"berry-1.0":                            "VIDEO_GENERATOR_MODEL_MULEROUTER_BERRY_1_0",
	"mulerouter_berry_1_0_pro":             "VIDEO_GENERATOR_MODEL_MULEROUTER_BERRY_1_0_PRO",
	"berry_1_0_pro":                        "VIDEO_GENERATOR_MODEL_MULEROUTER_BERRY_1_0_PRO",
	"berry-1.0-pro":                        "VIDEO_GENERATOR_MODEL_MULEROUTER_BERRY_1_0_PRO",
}

func ImageModelEnum(model string) (string, bool) {
	value, ok := imageModelEnums[strings.ToLower(strings.TrimSpace(model))]
	return value, ok
}

func VideoModelEnum(model string) (string, bool) {
	value, ok := videoModelEnums[strings.ToLower(strings.TrimSpace(model))]
	return value, ok
}

func imageAspectRatio(size string) string {
	width, height, ok := parseDimensions(size)
	if !ok || width <= 0 || height <= 0 {
		return "ASPECT_RATIO_SQUARE"
	}
	ratio := float64(width) / float64(height)
	switch {
	case ratio > 1.6:
		return "ASPECT_RATIO_LANDSCAPE_16_9"
	case ratio > 1.15:
		return "ASPECT_RATIO_LANDSCAPE_4_3"
	case ratio < 0.625:
		return "ASPECT_RATIO_PORTRAIT_9_16"
	case ratio < 0.88:
		return "ASPECT_RATIO_PORTRAIT_3_4"
	default:
		return "ASPECT_RATIO_SQUARE"
	}
}

func videoAspectRatio(size string) string {
	width, height, ok := parseDimensions(size)
	if !ok || width <= 0 || height <= 0 {
		return "PROMPT_TO_VIDEO_GENERATOR_CONTENT_ASPECT_RATIO_16_9"
	}
	ratio := float64(width) / float64(height)
	known := []struct {
		value float64
		name  string
	}{
		{21.0 / 9.0, "21_9"}, {16.0 / 9.0, "16_9"}, {3.0 / 2.0, "3_2"},
		{4.0 / 3.0, "4_3"}, {5.0 / 4.0, "5_4"}, {1, "1_1"},
		{4.0 / 5.0, "4_5"}, {3.0 / 4.0, "3_4"}, {2.0 / 3.0, "2_3"}, {9.0 / 16.0, "9_16"},
	}
	best := known[0]
	bestDistance := 1e9
	for _, candidate := range known {
		if distance := absFloat(ratio - candidate.value); distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return "PROMPT_TO_VIDEO_GENERATOR_CONTENT_ASPECT_RATIO_" + best.name
}

func resolutionEnum(resolution string) string {
	value := strings.ToLower(strings.TrimSpace(resolution))
	if value == "2k" {
		value = "2K"
	} else {
		value = strings.ToUpper(value)
	}
	return "PROMPT_TO_VIDEO_GENERATOR_CONTENT_RESOLUTION_" + value
}

// VideoFrame is the response-side representation returned by the media
// matrix session. The request and response messages intentionally differ:
// requests contain file metadata while responses contain signed upload URLs.
type VideoFrame struct {
	Type          string
	URL           string
	InputMedia    string
	ReferenceType string
}

// VideoFrameRequest is the metadata-only message accepted by
// SessionRequestPromptToVideoGeneratorContent.frames. The signed upload URL
// is returned by InitiateSession and must not be sent in this request.
type VideoFrameRequest struct {
	Type            string
	FileSize        int64
	FileName        string
	Ref             string
	DurationSeconds int
	ReferenceType   string
	Width           int
	Height          int
}

func BuildImageFlowRequest(model, prompt, size string, quantity int, sourceURL string, referenceURLs []string) (map[string]any, error) {
	modelEnum, ok := ImageModelEnum(model)
	if !ok {
		return nil, fmt.Errorf("Creative Fabrica image model %q is not in the catalog", model)
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("Creative Fabrica image prompt is required")
	}
	if quantity < 1 {
		quantity = 1
	}
	settings := map[string]any{
		"prompt":           prompt,
		"style":            "FLOW_STYLE_NONE",
		"type":             "FLOW_TYPE_FULL",
		"visibility":       "FLOW_VISIBILITY_PRIVATE",
		"ratio":            imageAspectRatio(size),
		"source":           "SOURCE_UNSPECIFIED",
		"surface":          "SURFACE_STUDIO",
		"models":           []any{map[string]any{"model": modelEnum, "count": quantity}},
		"outputImageCount": quantity,
		"assets":           []any{},
	}
	if strings.TrimSpace(sourceURL) != "" {
		settings["imageId"] = "uploaded"
		settings["imageUrl"] = stripQuery(sourceURL)
		settings["imageSource"] = "IMAGE_SOURCE_UPLOADED"
	}
	if len(referenceURLs) > 0 {
		references := make(map[string]string, len(referenceURLs))
		for index, value := range referenceURLs {
			if value = stripQuery(value); value != "" {
				references[strconv.Itoa(index+1)] = value
			}
		}
		if len(references) > 0 {
			settings["referenceImages"] = references
		}
	}
	return map[string]any{"settings": settings}, nil
}

func BuildVideoSessionRequest(model, prompt, size, resolution string, duration int, frames []VideoFrameRequest) (map[string]any, error) {
	modelEnum, ok := VideoModelEnum(model)
	if !ok {
		return nil, fmt.Errorf("Creative Fabrica video model %q is not in the catalog", model)
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("Creative Fabrica video prompt is required")
	}
	if duration < 1 {
		duration = 5
	}
	wireFrames := make([]any, 0, len(frames))
	for _, frame := range frames {
		item := make(map[string]any, 8)
		if value := strings.TrimSpace(frame.Type); value != "" {
			item["type"] = value
		}
		if frame.FileSize > 0 {
			item["fileSize"] = frame.FileSize
		}
		if value := strings.TrimSpace(frame.FileName); value != "" {
			item["fileName"] = value
		}
		if value := strings.TrimSpace(frame.Ref); value != "" {
			item["ref"] = value
		}
		if frame.DurationSeconds > 0 {
			item["durationSeconds"] = frame.DurationSeconds
		}
		if frame.ReferenceType != "" {
			item["referenceType"] = frame.ReferenceType
		}
		if frame.Width > 0 {
			item["width"] = frame.Width
		}
		if frame.Height > 0 {
			item["height"] = frame.Height
		}
		wireFrames = append(wireFrames, item)
	}
	content := map[string]any{
		"serviceType":          videoServiceType,
		"promptContent":        map[string]any{"prompt": prompt},
		"videoDuration":        map[string]any{"inSeconds": duration},
		"resolution":           resolutionEnum(resolution),
		"model":                modelEnum,
		"aspectRatio":          videoAspectRatio(size),
		"frames":               wireFrames,
		"studioAssetUsernames": []string{},
	}
	return map[string]any{
		"sessionRequestPromptToVideoGeneratorContent": content,
		"visibility": "SESSION_VISIBILITY_PRIVATE",
		"source":     "SOURCE_LUMO",
	}, nil
}

func parseDimensions(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	return width, height, widthErr == nil && heightErr == nil
}

func stripQuery(value string) string {
	if index := strings.IndexByte(value, '?'); index >= 0 {
		return value[:index]
	}
	return value
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
