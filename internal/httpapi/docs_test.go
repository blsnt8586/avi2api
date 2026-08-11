package httpapi

import (
	"encoding/json"
	"github.com/leonardo2api/leonardo2api/internal/modelconstraints"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPISpec(t *testing.T) {
	recorder := httptest.NewRecorder()
	openAPISpec(recorder, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("content-type=%q", contentType)
	}
	var document struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.1.0" || document.Info.Version != "1.7.0" || document.Paths["/v1/images/generations"] == nil || document.Paths["/v1/images/estimate"] == nil || document.Paths["/v1/videos/estimate"] == nil || document.Paths["/v1/audio/generations"] == nil || document.Paths["/v1/chat/completions"] == nil || document.Paths["/v1/tasks/images"] == nil {
		t.Fatalf("incomplete OpenAPI document: openapi=%q contract=%q paths=%d", document.OpenAPI, document.Info.Version, len(document.Paths))
	}
}

func TestOpenAPIContractCoverage(t *testing.T) {
	recorder := httptest.NewRecorder()
	openAPISpec(recorder, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	var document map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	promptContract := document["x-prompt-limit-by-model"].(map[string]any)
	if promptContract["counting"] != "Unicode code points" {
		t.Fatalf("prompt counting semantics are missing: %+v", promptContract)
	}
	promptLimits := promptContract["limits"].(map[string]any)
	wantPromptLimits := modelconstraints.AllPromptLimits()
	if len(promptLimits) != len(wantPromptLimits) {
		t.Fatalf("OpenAPI prompt models=%d, backend=%d", len(promptLimits), len(wantPromptLimits))
	}
	for model, limit := range wantPromptLimits {
		if promptLimits[model] != float64(limit) {
			t.Errorf("OpenAPI prompt limit for %s=%v, want %d", model, promptLimits[model], limit)
		}
	}
	for path, rawItem := range paths {
		item := rawItem.(map[string]any)
		for _, method := range []string{"get", "post", "put", "patch", "delete"} {
			rawOperation, ok := item[method]
			if !ok {
				continue
			}
			operation := rawOperation.(map[string]any)
			if operation["operationId"] == nil || operation["operationId"] == "" {
				t.Fatalf("%s %s is missing operationId", method, path)
			}
		}
	}
	videoCreate := paths["/v1/videos/generations"].(map[string]any)["post"].(map[string]any)
	rawModes, ok := videoCreate["x-generation-modes"].([]any)
	if !ok || len(rawModes) != 5 {
		t.Fatalf("video generation modes are incomplete: %+v", videoCreate["x-generation-modes"])
	}
	modes := make(map[string]map[string]any, len(rawModes))
	for _, rawMode := range rawModes {
		mode := rawMode.(map[string]any)
		modes[mode["id"].(string)] = mode
	}
	for _, id := range []string{"none", "image", "frame", "video", "audio"} {
		if modes[id] == nil {
			t.Fatalf("video generation mode %q is missing: %+v", id, modes)
		}
	}
	if modes["none"]["content_type"] != "application/json" || len(modes["none"]["required_media"].([]any)) != 0 {
		t.Fatalf("no-reference mode is unclear: %+v", modes["none"])
	}
	if containsOpenAPIValue(modes["none"]["models"].([]any), "grok-imagine-1.5") {
		t.Fatalf("Grok Imagine 1.5 must not be documented as text-to-video: %+v", modes["none"])
	}
	frameRequired := modes["frame"]["required_media"].([]any)
	frameOptional := modes["frame"]["optional_media"].([]any)
	if !containsOpenAPIValue(frameRequired, "start_frame") || containsOpenAPIValue(frameRequired, "end_frame") || !containsOpenAPIValue(frameOptional, "end_frame") {
		t.Fatalf("frame mode must require start_frame and keep end_frame optional: %+v", modes["frame"])
	}
	if !containsOpenAPIValue(modes["frame"]["models"].([]any), "grok-imagine-1.5") || !strings.Contains(modes["frame"]["rule"].(string), "rejects end_frame") {
		t.Fatalf("Grok Imagine 1.5 frame exception is missing: %+v", modes["frame"])
	}
	videoModels := modes["video"]["models"].([]any)
	if len(videoModels) != 6 || !containsOpenAPIValue(videoModels, "flux-3-video") || !containsOpenAPIValue(videoModels, "seedance-2.5") || !containsOpenAPIValue(videoModels, "kling-o3-omni") || containsOpenAPIValue(videoModels, "minimax-h3") {
		t.Fatalf("video-reference mode models are incomplete: %+v", modes["video"])
	}
	if !strings.Contains(modes["audio"]["rule"].(string), "MiniMax H3 requires an ordinary image") {
		t.Fatalf("audio-reference dependencies are unclear: %+v", modes["audio"])
	}
	combinations, ok := videoCreate["x-reference-combinations"].(map[string]any)
	if !ok || combinations["mode_field"] != false || combinations["inferred_from"] != "multipart file fields" {
		t.Fatalf("video reference inference is unclear: %+v", videoCreate["x-reference-combinations"])
	}
	frameExclusions := combinations["frame_exclusive_with"].([]any)
	if !containsOpenAPIValue(frameExclusions, "image") || !containsOpenAPIValue(frameExclusions, "video") || !containsOpenAPIValue(frameExclusions, "audio") || combinations["end_frame_requires"] != "start_frame" {
		t.Fatalf("frame reference exclusions are incomplete: %+v", combinations)
	}
	rawCombinationRules, ok := combinations["rules"].([]any)
	if !ok || len(rawCombinationRules) != 6 {
		t.Fatalf("video reference combinations are incomplete: %+v", combinations["rules"])
	}
	combinationRules := make(map[string]map[string]any)
	for _, rawRule := range rawCombinationRules {
		rule := rawRule.(map[string]any)
		for _, rawModel := range rule["models"].([]any) {
			combinationRules[rawModel.(string)] = rule
		}
	}
	seedanceCombinations := combinationRules["seedance-2.0"]["combinable_reference_fields"].([]any)
	seedanceAudioDependencies := combinationRules["seedance-2.0"]["audio_requires_any"].([]any)
	if !containsOpenAPIValue(seedanceCombinations, "image") || !containsOpenAPIValue(seedanceCombinations, "video") || !containsOpenAPIValue(seedanceCombinations, "audio") || !containsOpenAPIValue(seedanceAudioDependencies, "image") || !containsOpenAPIValue(seedanceAudioDependencies, "video") {
		t.Fatalf("Seedance reference combinations are unclear: %+v", combinationRules["seedance-2.0"])
	}
	seedance25 := combinationRules["seedance-2.5"]
	if seedance25 == nil || seedance25["max_reference_images"] != float64(30) || seedance25["max_reference_videos"] != float64(10) || seedance25["max_reference_audios"] != float64(10) || seedance25["max_reference_media_duration_seconds"] != 30.2 {
		t.Fatalf("Seedance 2.5 reference combinations are incomplete: %+v", seedance25)
	}
	if combinationRules["flux-3-video"] == nil {
		t.Fatal("FLUX 3 Video reference combinations are missing")
	}
	fluxCombinations := combinationRules["flux-3-video"]["combinable_reference_fields"].([]any)
	if len(fluxCombinations) != 1 || !containsOpenAPIValue(fluxCombinations, "video") || !containsOpenAPIValue(combinationRules["flux-3-video"]["alternative_reference_fields"].([]any), "start_frame") {
		t.Fatalf("FLUX 3 Video reference combinations are unclear: %+v", combinationRules["flux-3-video"])
	}
	o3Combinations := combinationRules["kling-o3-omni"]["combinable_reference_fields"].([]any)
	if !containsOpenAPIValue(o3Combinations, "image") || !containsOpenAPIValue(o3Combinations, "video") || combinationRules["kling-o3-omni"]["video_reference_max_images"] != float64(4) {
		t.Fatalf("Kling O3 Omni reference combinations are unclear: %+v", combinationRules["kling-o3-omni"])
	}
	h3Combinations := combinationRules["minimax-h3"]["combinable_reference_fields"].([]any)
	h3AudioDependencies := combinationRules["minimax-h3"]["audio_requires_all"].([]any)
	if !containsOpenAPIValue(h3Combinations, "image") || !containsOpenAPIValue(h3Combinations, "audio") || !containsOpenAPIValue(h3AudioDependencies, "image") {
		t.Fatalf("MiniMax H3 reference combinations are unclear: %+v", combinationRules["minimax-h3"])
	}
	grokCombinations := combinationRules["grok-imagine-1.5"]["combinable_reference_fields"].([]any)
	grokRequired := combinationRules["grok-imagine-1.5"]["required_reference_fields"].([]any)
	grokUnsupported := combinationRules["grok-imagine-1.5"]["unsupported_reference_fields"].([]any)
	if len(grokCombinations) != 1 || !containsOpenAPIValue(grokCombinations, "start_frame") || !containsOpenAPIValue(grokRequired, "start_frame") || !containsOpenAPIValue(grokUnsupported, "end_frame") {
		t.Fatalf("Grok Imagine 1.5 reference contract is unclear: %+v", combinationRules["grok-imagine-1.5"])
	}
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	imageEdit := schemas["ImageEditRequest"].(map[string]any)
	if imageEdit["allOf"] != nil {
		t.Fatal("ImageEditRequest must not combine additionalProperties=false through allOf")
	}
	properties := imageEdit["properties"].(map[string]any)
	if properties["image"] == nil || properties["reference_strength"] == nil {
		t.Fatalf("ImageEditRequest is incomplete: %+v", properties)
	}
	if properties["output_format"] != nil || properties["output_compression"] != nil || properties["response_format"].(map[string]any)["const"] != "url" {
		t.Fatalf("async image edit schema exposes synchronous delivery parameters: %+v", properties)
	}
	asyncImage := schemas["AsyncImageRequest"].(map[string]any)["properties"].(map[string]any)
	if asyncImage["output_format"] != nil || asyncImage["output_compression"] != nil {
		t.Fatalf("async image schema exposes unsupported delivery parameters: %+v", asyncImage)
	}
	taskImagePath := document.Paths["/v1/tasks/images"].(map[string]any)
	taskImagePost := taskImagePath["post"].(map[string]any)
	taskImageRequestBody := taskImagePost["requestBody"].(map[string]any)
	taskImageContent := taskImageRequestBody["content"].(map[string]any)
	if taskImageContent["application/json"] == nil || taskImageContent["multipart/form-data"] == nil {
		t.Fatalf("async image task must document JSON and multipart requests: %+v", taskImageContent)
	}
	imageRequest := schemas["ImageRequest"].(map[string]any)["properties"].(map[string]any)
	if imageRequest["prompt"].(map[string]any)["maxLength"] != float64(9999) {
		t.Fatalf("image prompt limit must match Leonardo schema: %+v", imageRequest["prompt"])
	}
	imageSize := schemas["ImageSize"].(map[string]any)
	modelRules := imageSize["x-model-rules"].(map[string]any)
	if len(modelRules["gpt-image-2"].(map[string]any)["standard_presets"].([]any)) != 30 ||
		len(modelRules["nano-banana-2,nano-banana-pro"].(map[string]any)["standard_presets"].([]any)) != 30 ||
		len(modelRules["seedream-5.0-pro"].(map[string]any)["standard_presets"].([]any)) != 48 {
		t.Fatalf("image size presets are incomplete: %+v", modelRules)
	}
	gptSizeRule := modelRules["gpt-image-2"].(map[string]any)
	nanoSizeRule := modelRules["nano-banana-2,nano-banana-pro"].(map[string]any)
	seedreamSizeRule := modelRules["seedream-5.0-pro"].(map[string]any)
	if gptSizeRule["size_mode"] != "enumerated_edges" || gptSizeRule["custom_sizes"] != false ||
		nanoSizeRule["size_mode"] != "enumerated_edges" || nanoSizeRule["custom_sizes"] != false ||
		seedreamSizeRule["size_mode"] != "continuous_range" || seedreamSizeRule["custom_sizes"] != true {
		t.Fatalf("image custom-size capabilities are unclear: %+v", modelRules)
	}
	quantityRules := schemas["ImageQuantity"].(map[string]any)["x-model-rules"].(map[string]any)
	for modelID, maximum := range map[string]float64{
		"gpt-image-2": 1, "nano-banana-2": 4, "nano-banana-pro": 4, "seedream-5.0-pro": 4,
	} {
		if quantityRules[modelID].(map[string]any)["maximum"] != maximum {
			t.Fatalf("image quantity rule for %s is unclear: %+v", modelID, quantityRules[modelID])
		}
	}
	imageEstimate := schemas["ImageEstimate"].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"model", "size", "quantity", "unit_tokens", "estimated_tokens", "pricing_basis", "price_version"} {
		if imageEstimate[field] == nil {
			t.Fatalf("ImageEstimate is missing %s: %+v", field, imageEstimate)
		}
	}
	model := schemas["Model"].(map[string]any)["properties"].(map[string]any)
	if model["owned_by"].(map[string]any)["const"] != "aiv2api" {
		t.Fatalf("model owner must describe the gateway: %+v", model["owned_by"])
	}
	if model["created"] == nil {
		t.Fatal("model schema must include the created field returned by /v1/models")
	}
	for _, schemaName := range []string{"VideoRequest", "VideoReferenceRequest", "VideoEstimateRequest", "AudioRequest"} {
		options, ok := schemas[schemaName].(map[string]any)["oneOf"].([]any)
		if !ok || len(options) < 3 {
			t.Fatalf("%s must use model-specific oneOf schemas: %+v", schemaName, schemas[schemaName])
		}
	}
	for schemaName, limit := range map[string]float64{
		"Flux3VideoRequest": 5000, "SeedanceVideoRequest": 5000, "Seedance25VideoRequest": 5000,
		"Veo31VideoRequest": 9999, "KlingO3OmniVideoRequest": 2500, "MiniMaxH3VideoRequest": 2000,
		"GrokImagine15ReferenceRequest": 5000,
		"DialogueAudioRequest":          5000, "MusicAudioRequest": 9999, "SoundEffectsAudioRequest": 9999,
	} {
		properties := schemas[schemaName].(map[string]any)["properties"].(map[string]any)
		if properties["prompt"].(map[string]any)["maxLength"] != limit {
			t.Fatalf("%s prompt limit=%v, want %v", schemaName, properties["prompt"], limit)
		}
	}
	flux3Video := schemas["Flux3VideoRequest"].(map[string]any)["properties"].(map[string]any)
	flux3Size := schemas["Flux3VideoSize"].(map[string]any)
	if flux3Video["duration"].(map[string]any)["maximum"] != float64(20) || len(flux3Size["enum"].([]any)) != 14 || flux3Size["x-resolution-by-size"].(map[string]any)["2520x1080"] != "1080p" {
		t.Fatalf("FLUX 3 Video contract is incomplete: video=%+v size=%+v", flux3Video, flux3Size)
	}
	flux3Reference := schemas["Flux3VideoReferenceRequest"].(map[string]any)["properties"].(map[string]any)
	if flux3Reference["image"] != nil || flux3Reference["audio"] != nil || flux3Reference["video"].(map[string]any)["maxItems"] != float64(1) || !strings.Contains(flux3Reference["video"].(map[string]any)["description"].(string), "15.05 seconds") {
		t.Fatalf("FLUX 3 Video reference limits are incomplete: %+v", flux3Reference)
	}
	seedance25Video := schemas["Seedance25VideoRequest"].(map[string]any)["properties"].(map[string]any)
	seedance25Size := schemas["Seedance25Size"].(map[string]any)
	if seedance25Video["duration"].(map[string]any)["maximum"] != float64(30) || len(seedance25Size["enum"].([]any)) != 12 || seedance25Size["x-resolution-by-size"].(map[string]any)["640x640"] != "480p" || seedance25Size["x-resolution-by-size"].(map[string]any)["960x960"] != "720p" {
		t.Fatalf("Seedance 2.5 video contract is incomplete: video=%+v size=%+v", seedance25Video, seedance25Size)
	}
	seedance25Reference := schemas["Seedance25ReferenceRequest"].(map[string]any)["properties"].(map[string]any)
	if seedance25Reference["image"].(map[string]any)["maxItems"] != float64(30) || seedance25Reference["video"].(map[string]any)["maxItems"] != float64(10) || seedance25Reference["audio"].(map[string]any)["maxItems"] != float64(10) || !strings.Contains(seedance25Reference["audio"].(map[string]any)["description"].(string), "30.2 seconds") {
		t.Fatalf("Seedance 2.5 reference limits are incomplete: %+v", seedance25Reference)
	}
	o3Video := schemas["KlingO3OmniVideoRequest"].(map[string]any)["properties"].(map[string]any)
	o3Size := schemas["KlingO3OmniSize"].(map[string]any)
	if o3Video["resolution"].(map[string]any)["default"] != "1080p" || len(o3Size["enum"].([]any)) != 9 || o3Size["x-resolution-by-size"].(map[string]any)["2880x2880"] != "2160p" {
		t.Fatalf("Kling O3 Omni contract is incomplete: video=%+v size=%+v", o3Video, o3Size)
	}
	o3Reference := schemas["KlingO3OmniReferenceRequest"].(map[string]any)["properties"].(map[string]any)
	if o3Reference["image"].(map[string]any)["maxItems"] != float64(7) || o3Reference["video"].(map[string]any)["maxItems"] != float64(1) || o3Reference["audio"] != nil || !strings.Contains(o3Reference["video"].(map[string]any)["description"].(string), "3-10.05") {
		t.Fatalf("Kling O3 Omni reference limits are incomplete: %+v", o3Reference)
	}
	h3Video := schemas["MiniMaxH3VideoRequest"].(map[string]any)["properties"].(map[string]any)
	videoSizes := h3Video["size"].(map[string]any)["enum"].([]any)
	if !containsOpenAPIValue(videoSizes, "3360x1440") || !containsOpenAPIValue(videoSizes, "1440x2560") || h3Video["generate_audio"].(map[string]any)["const"] != true {
		t.Fatalf("MiniMax H3 video contract is incomplete: %+v", h3Video)
	}
	grokReference := schemas["GrokImagine15ReferenceRequest"].(map[string]any)
	grokProperties := grokReference["properties"].(map[string]any)
	grokRequiredFields := grokReference["required"].([]any)
	if !containsOpenAPIValue(grokRequiredFields, "start_frame") || grokProperties["end_frame"] != nil || grokProperties["image"] != nil || grokProperties["video"] != nil || grokProperties["audio"] != nil {
		t.Fatalf("Grok Imagine 1.5 must require only start-frame reference media: %+v", grokReference)
	}
	grokSize := schemas["GrokImagine15Size"].(map[string]any)
	if len(grokSize["enum"].([]any)) != 9 || grokSize["x-resolution-by-size"].(map[string]any)["1424x1424"] != "1080p" {
		t.Fatalf("Grok Imagine 1.5 size mapping is incomplete: %+v", grokSize)
	}
	veoReference := schemas["Veo31ReferenceRequest"].(map[string]any)
	if len(veoReference["oneOf"].([]any)) != 2 {
		t.Fatalf("Veo 3.1 references must separate ordinary images from frame guidance: %+v", veoReference)
	}
	videoReference := schemas["VideoReferenceRequest"].(map[string]any)
	if !strings.Contains(videoReference["description"].(string), "end_frame is optional") {
		t.Fatalf("video reference requirement summary is unclear: %+v", videoReference)
	}
	for _, schemaName := range []string{"Veo31FastReferenceRequest"} {
		required := schemas[schemaName].(map[string]any)["required"].([]any)
		if !containsOpenAPIValue(required, "start_frame") || containsOpenAPIValue(required, "end_frame") {
			t.Fatalf("%s must require start_frame and keep end_frame optional: %v", schemaName, required)
		}
	}
	for _, removed := range []string{"GeminiVideoRequest", "GeminiReferenceRequest", "Veo31LiteVideoRequest", "Veo31LiteReferenceRequest", "KlingVideoRequest", "KlingReferenceRequest"} {
		if schemas[removed] != nil {
			t.Fatalf("removed video model schema %s remains public", removed)
		}
	}
	seedanceReference := schemas["SeedanceReferenceRequest"].(map[string]any)["properties"].(map[string]any)
	if !strings.Contains(seedanceReference["audio"].(map[string]any)["description"].(string), "15 seconds") {
		t.Fatalf("Seedance audio reference duration limit is missing: %+v", seedanceReference["audio"])
	}
	voices := schemas["DialogueAudioRequest"].(map[string]any)["properties"].(map[string]any)["voice"].(map[string]any)["enum"].([]any)
	if len(voices) != 21 || !containsOpenAPIValue(voices, "george") || !containsOpenAPIValue(voices, "bill") {
		t.Fatalf("Dialogue voice enum is incomplete: %v", voices)
	}
	task := schemas["Task"].(map[string]any)["properties"].(map[string]any)
	if task["generation_id"] != nil {
		t.Fatal("public task schema exposes upstream generation_id")
	}
	statusEnum := task["status"].(map[string]any)["enum"].([]any)
	wantStatuses := []string{"queued", "processing", "succeeded", "failed", "cancelled"}
	if len(statusEnum) != len(wantStatuses) {
		t.Fatalf("public task status enum=%v", statusEnum)
	}
	for index, want := range wantStatuses {
		if statusEnum[index] != want {
			t.Fatalf("public task status enum=%v", statusEnum)
		}
	}
	chatMessage := schemas["ChatMessage"].(map[string]any)["properties"].(map[string]any)
	contentOptions := chatMessage["content"].(map[string]any)["oneOf"].([]any)
	parts := contentOptions[1].(map[string]any)
	if parts["maxContains"] != float64(1) {
		t.Fatalf("chat content must allow at most one image part: %+v", parts)
	}
}

func containsOpenAPIValue(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
