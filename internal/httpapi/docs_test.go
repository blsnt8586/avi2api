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
	if document.OpenAPI != "3.1.0" || document.Info.Version != "2.0.0" || document.Paths["/v1/images/generations"] == nil || document.Paths["/v1/images/{id}"] == nil || document.Paths["/v1/images/{id}/cancel"] == nil || document.Paths["/v1/images/estimate"] == nil || document.Paths["/v1/videos/estimate"] == nil || document.Paths["/v1/audio/generations"] == nil || document.Paths["/v1/chat/completions"] == nil {
		t.Fatalf("incomplete OpenAPI document: openapi=%q contract=%q paths=%d", document.OpenAPI, document.Info.Version, len(document.Paths))
	}
	for _, removed := range []string{"/v1/tasks/images", "/v1/images/edits", "/v1/tasks/{id}", "/v1/tasks/{id}/cancel"} {
		if document.Paths[removed] != nil {
			t.Fatalf("removed path %s is still documented", removed)
		}
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
	for model, limit := range wantPromptLimits {
		actual := promptLimits["leonardo/"+model]
		if actual == nil {
			actual = promptLimits["adobe/"+model]
		}
		if actual == nil {
			// Leonardo models retired from the public catalog remain in the
			// backend constraint table for historical requests, but are not
			// part of the public OpenAPI contract.
			continue
		}
		if actual != float64(limit) {
			t.Errorf("OpenAPI prompt limit for %s=%v, want %d", model, actual, limit)
		}
	}
	for route, rawLimit := range promptLimits {
		model := route
		if slash := strings.IndexByte(model, '/'); slash >= 0 {
			model = model[slash+1:]
		}
		limit, ok := wantPromptLimits[model]
		if !ok || rawLimit != float64(limit) {
			t.Errorf("OpenAPI prompt route %s=%v has no matching backend limit", route, rawLimit)
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
	for _, model := range []string{"leonardo/gemini-omni-flash", "leonardo/happy-horse-1.1", "leonardo/kling-3.0", "leonardo/kling-3.0-turbo", "leonardo/kling-o3-omni", "leonardo/hailuo-2.3", "leonardo/wan-2.7"} {
		if !containsOpenAPIValue(modes["none"]["models"].([]any), model) {
			t.Fatalf("current Leonardo video model %s is missing from no-reference docs: %+v", model, modes["none"])
		}
	}
	for _, model := range []string{"flux-3-video", "seedance-2.0", "seedance-2.0-fast", "seedance-2.0-mini", "seedance-2.5", "veo-3.1", "veo-3.1-fast", "minimax-h3", "grok-imagine-1.5"} {
		if containsOpenAPIValue(modes["none"]["models"].([]any), model) {
			t.Fatalf("retired Leonardo model %s must be hidden from no-reference docs: %+v", model, modes["none"])
		}
	}
	frameRequired := modes["frame"]["required_media"].([]any)
	frameOptional := modes["frame"]["optional_media"].([]any)
	if !containsOpenAPIValue(frameRequired, "start_frame") || containsOpenAPIValue(frameRequired, "end_frame") || !containsOpenAPIValue(frameOptional, "end_frame") {
		t.Fatalf("frame mode must require start_frame and keep end_frame optional: %+v", modes["frame"])
	}
	if containsOpenAPIValue(modes["frame"]["models"].([]any), "grok-imagine-1.5") {
		t.Fatalf("Grok Imagine 1.5 must be hidden from frame docs: %+v", modes["frame"])
	}
	for _, mode := range []string{"none", "image", "frame", "video", "audio"} {
		if !containsOpenAPIValue(modes[mode]["models"].([]any), "creativefabrica/seedance_v2_mini") {
			t.Fatalf("Creative Fabrica Seedance Mini is missing from %s mode docs: %+v", mode, modes[mode])
		}
	}
	videoModels := modes["video"]["models"].([]any)
	if len(videoModels) != 4 || !containsOpenAPIValue(videoModels, "leonardo/kling-o3-omni") || !containsOpenAPIValue(videoModels, "adobe/seedance-2.0") || !containsOpenAPIValue(videoModels, "adobe/seedance-2.0-fast") {
		t.Fatalf("video-reference mode models are incomplete: %+v", modes["video"])
	}
	if len(modes["audio"]["models"].([]any)) != 3 || !containsOpenAPIValue(modes["audio"]["models"].([]any), "adobe/seedance-2.0") || !containsOpenAPIValue(modes["audio"]["models"].([]any), "adobe/seedance-2.0-fast") {
		t.Fatalf("audio-reference models are unclear: %+v", modes["audio"])
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
	if combinationRules["leonardo/flux-3-video"] != nil || combinationRules["leonardo/seedance-2.0"] != nil || combinationRules["leonardo/grok-imagine-1.5"] != nil {
		t.Fatalf("retired Leonardo reference rules must be hidden: %+v", combinationRules)
	}
	o3Combinations := combinationRules["leonardo/kling-o3-omni"]["combinable_reference_fields"].([]any)
	if !containsOpenAPIValue(o3Combinations, "image") || !containsOpenAPIValue(o3Combinations, "video") || combinationRules["leonardo/kling-o3-omni"]["video_reference_max_images"] != float64(4) {
		t.Fatalf("Kling O3 Omni reference combinations are unclear: %+v", combinationRules["leonardo/kling-o3-omni"])
	}
	if combinationRules["leonardo/minimax-h3"] != nil || combinationRules["leonardo/grok-imagine-1.5"] != nil {
		t.Fatalf("retired MiniMax/Grok reference rules must be hidden: %+v", combinationRules)
	}
	adobeCombinations := combinationRules["adobe/veo-3.1-fast"]["combinable_reference_fields"].([]any)
	if !containsOpenAPIValue(adobeCombinations, "start_frame") || !containsOpenAPIValue(adobeCombinations, "end_frame") {
		t.Fatalf("Adobe Veo frame contract is unclear: %+v", combinationRules["adobe/veo-3.1-fast"])
	}
	adobeSeedanceCombinations := combinationRules["adobe/seedance-2.0"]["combinable_reference_fields"].([]any)
	if !containsOpenAPIValue(adobeSeedanceCombinations, "image") || !containsOpenAPIValue(adobeSeedanceCombinations, "video") || !containsOpenAPIValue(adobeSeedanceCombinations, "audio") || combinationRules["adobe/seedance-2.0"]["max_reference_media"] != float64(12) {
		t.Fatalf("Adobe Seedance reference contract is unclear: %+v", combinationRules["adobe/seedance-2.0"])
	}
	if combinationRules["adobe/kling-3.0-omni"] == nil || combinationRules["adobe/veo-3.1"] == nil {
		t.Fatalf("Adobe Kling or Veo reference contract is missing: kling=%+v veo=%+v", combinationRules["adobe/kling-3.0-omni"], combinationRules["adobe/veo-3.1"])
	}
	cfMiniRule := combinationRules["creativefabrica/seedance_v2_mini"]
	cfMiniCombinations := cfMiniRule["combinable_reference_fields"].([]any)
	if !containsOpenAPIValue(cfMiniCombinations, "image") || !containsOpenAPIValue(cfMiniCombinations, "video") || !containsOpenAPIValue(cfMiniCombinations, "audio") ||
		cfMiniRule["max_reference_images"] != float64(9) || cfMiniRule["max_reference_videos"] != float64(3) || cfMiniRule["max_reference_audios"] != float64(3) ||
		cfMiniRule["max_reference_media"] != float64(9) || cfMiniRule["max_reference_video_duration"] != float64(15) || cfMiniRule["max_reference_audio_duration"] != float64(15) {
		t.Fatalf("Creative Fabrica Seedance Mini reference contract is unclear: %+v", cfMiniRule)
	}
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	chatRequest := schemas["ChatCompletionRequest"].(map[string]any)
	chatProperties := chatRequest["properties"].(map[string]any)
	if chatProperties["provider"] != nil || chatProperties["model"] == nil {
		t.Fatal("ChatCompletionRequest must select the platform through model=platform/model")
	}
	imageReference := schemas["ImageReferenceRequest"].(map[string]any)
	if imageReference["allOf"] != nil {
		t.Fatal("ImageReferenceRequest must not combine additionalProperties=false through allOf")
	}
	properties := imageReference["properties"].(map[string]any)
	if properties["image"] == nil || properties["reference_strength"] == nil {
		t.Fatalf("ImageReferenceRequest is incomplete: %+v", properties)
	}
	if properties["output_format"] != nil || properties["output_compression"] != nil || properties["response_format"].(map[string]any)["const"] != "url" {
		t.Fatalf("async image reference schema exposes synchronous delivery parameters: %+v", properties)
	}
	asyncImage := schemas["AsyncImageRequest"].(map[string]any)["properties"].(map[string]any)
	if asyncImage["output_format"] != nil || asyncImage["output_compression"] != nil {
		t.Fatalf("async image schema exposes unsupported delivery parameters: %+v", asyncImage)
	}
	taskImagePath := paths["/v1/images/generations"].(map[string]any)
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
	if len(modelRules["leonardo/gpt-image-2"].(map[string]any)["standard_presets"].([]any)) != 30 ||
		len(modelRules["leonardo/nano-banana-2,leonardo/nano-banana-pro,adobe/nano-banana-2"].(map[string]any)["standard_presets"].([]any)) != 30 ||
		len(modelRules["leonardo/seedream-5.0-pro"].(map[string]any)["standard_presets"].([]any)) != 48 {
		t.Fatalf("image size presets are incomplete: %+v", modelRules)
	}
	gptSizeRule := modelRules["leonardo/gpt-image-2"].(map[string]any)
	nanoSizeRule := modelRules["leonardo/nano-banana-2,leonardo/nano-banana-pro,adobe/nano-banana-2"].(map[string]any)
	seedreamSizeRule := modelRules["leonardo/seedream-5.0-pro"].(map[string]any)
	if gptSizeRule["size_mode"] != "enumerated_edges" || gptSizeRule["custom_sizes"] != false ||
		nanoSizeRule["size_mode"] != "enumerated_edges" || nanoSizeRule["custom_sizes"] != false ||
		seedreamSizeRule["size_mode"] != "continuous_range" || seedreamSizeRule["custom_sizes"] != true {
		t.Fatalf("image custom-size capabilities are unclear: %+v", modelRules)
	}
	quantityRules := schemas["ImageQuantity"].(map[string]any)["x-model-rules"].(map[string]any)
	for modelID, maximum := range map[string]float64{
		"gpt-image-2": 1, "adobe:gpt-image-2": 1, "adobe:nano-banana-2": 1, "nano-banana-2": 4, "nano-banana-pro": 4, "seedream-5.0-pro": 4,
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
		"KlingO3OmniVideoRequest": 2500,
		"DialogueAudioRequest":    5000, "MusicAudioRequest": 9999, "SoundEffectsAudioRequest": 9999,
	} {
		properties := schemas[schemaName].(map[string]any)["properties"].(map[string]any)
		if properties["prompt"].(map[string]any)["maxLength"] != limit {
			t.Fatalf("%s prompt limit=%v, want %v", schemaName, properties["prompt"], limit)
		}
	}
	currentVideo := schemas["LeonardoCurrentVideoRequest"].(map[string]any)
	currentModels := currentVideo["properties"].(map[string]any)["model"].(map[string]any)["enum"].([]any)
	for _, model := range []string{"leonardo/gemini-omni-flash", "leonardo/happy-horse-1.1", "leonardo/kling-3.0", "leonardo/kling-3.0-turbo", "leonardo/kling-o3-omni", "leonardo/hailuo-2.3", "leonardo/wan-2.7"} {
		if !containsOpenAPIValue(currentModels, model) {
			t.Fatalf("current Leonardo video schema is missing %s: %+v", model, currentVideo)
		}
	}
	for _, retiredSchema := range []string{"Flux3VideoRequest", "SeedanceVideoRequest", "SeedanceFastVideoRequest", "Seedance25VideoRequest", "Veo31VideoRequest", "MiniMaxH3VideoRequest", "GrokImagine15VideoEstimateRequest"} {
		if containsOpenAPIRef(schemas["VideoRequest"], retiredSchema) || containsOpenAPIRef(schemas["VideoEstimateRequest"], retiredSchema) || containsOpenAPIRef(schemas["VideoReferenceRequest"], retiredSchema) {
			t.Fatalf("retired Leonardo schema %s remains referenced by the public video contract", retiredSchema)
		}
	}
	geminiReference := schemas["GeminiOmniFlashReferenceRequest"].(map[string]any)["properties"].(map[string]any)["image"].(map[string]any)
	if geminiReference["maxItems"] != float64(5) {
		t.Fatalf("Gemini Omni Flash reference image limit is incomplete: %+v", geminiReference)
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
	videoReference := schemas["VideoReferenceRequest"].(map[string]any)
	if !strings.Contains(videoReference["description"].(string), "end_frame is optional") {
		t.Fatalf("video reference requirement summary is unclear: %+v", videoReference)
	}
	for _, removed := range []string{"GeminiVideoRequest", "GeminiReferenceRequest", "Veo31LiteVideoRequest", "Veo31LiteReferenceRequest", "KlingVideoRequest", "KlingReferenceRequest"} {
		if schemas[removed] != nil {
			t.Fatalf("removed video model schema %s remains public", removed)
		}
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

func containsOpenAPIRef(value any, schemaName string) bool {
	needle := "#/components/schemas/" + schemaName
	switch v := value.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok && ref == needle {
			return true
		}
		for _, child := range v {
			if containsOpenAPIRef(child, schemaName) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsOpenAPIRef(child, schemaName) {
				return true
			}
		}
	}
	return false
}
