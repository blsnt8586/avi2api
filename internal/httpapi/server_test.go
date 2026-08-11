package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
)

type fakeImageEstimateStore struct {
	models map[string]domain.ModelConfig
	rules  map[string]domain.ModelCostRule
}

func (fake fakeImageEstimateStore) GetModel(_ context.Context, id string) (domain.ModelConfig, error) {
	model, ok := fake.models[id]
	if !ok {
		return domain.ModelConfig{}, store.ErrNotFound
	}
	return model, nil
}

func (fake fakeImageEstimateStore) FindModelCostRule(_ context.Context, kind, model, size, quality, resolution string, duration int) (domain.ModelCostRule, error) {
	rule, ok := fake.rules[strings.Join([]string{kind, model, size, quality, resolution, string(rune(duration))}, "|")]
	if !ok {
		return domain.ModelCostRule{}, store.ErrNotFound
	}
	return rule, nil
}

func TestNormalizeAccountConcurrency(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    int
		wantErr bool
	}{
		{name: "Leonardo default", input: 0, want: 5},
		{name: "minimum", input: 1, want: 1},
		{name: "recommended", input: 3, want: 3},
		{name: "upstream maximum", input: 5, want: 5},
		{name: "negative", input: -1, wantErr: true},
		{name: "above maximum", input: 6, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeAccountConcurrency(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeAccountConcurrency(%d) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeAccountConcurrency(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeAccountQueueCapacity(t *testing.T) {
	tests := []struct {
		value   int
		want    int
		wantErr bool
	}{
		{value: 0, want: 40},
		{value: 1, want: 1},
		{value: 5, want: 5},
		{value: -1, wantErr: true},
		{value: 1001, wantErr: true},
	}
	for _, tt := range tests {
		got, err := normalizeAccountQueueCapacity(tt.value)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Fatalf("normalizeAccountQueueCapacity(%d)=(%d,%v), want (%d, err=%v)", tt.value, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestValidateSalePricingSettings(t *testing.T) {
	providers := map[string]struct{}{"leonardo": {}}
	valid := salePricingSettings{Profiles: []salePricingProfile{{
		ProviderID: " leonardo ", Currency: "cny", AccountCost: 100,
		IncludedCredits: 8500, UsableCreditRate: 0.9, OverheadRate: 0.05,
		PaymentFeeRate: 0.03, TargetMargin: 0.35, RoundingStep: 0.01,
	}}}
	if err := validateSalePricingSettings(&valid, providers); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	if valid.Profiles[0].ProviderID != "leonardo" || valid.Profiles[0].Currency != "CNY" {
		t.Fatalf("settings were not normalized: %+v", valid.Profiles[0])
	}

	tests := []salePricingSettings{
		{Profiles: []salePricingProfile{{ProviderID: "missing", Currency: "CNY", AccountCost: 100, IncludedCredits: 1, UsableCreditRate: 1, RoundingStep: 0.01}}},
		{Profiles: []salePricingProfile{{ProviderID: "leonardo", Currency: "CN", AccountCost: 100, IncludedCredits: 1, UsableCreditRate: 1, RoundingStep: 0.01}}},
		{Profiles: []salePricingProfile{{ProviderID: "leonardo", Currency: "CNY", AccountCost: 0, IncludedCredits: 1, UsableCreditRate: 1, RoundingStep: 0.01}}},
		{Profiles: []salePricingProfile{{ProviderID: "leonardo", Currency: "CNY", AccountCost: 100, IncludedCredits: 0, UsableCreditRate: 1, RoundingStep: 0.01}}},
		{Profiles: []salePricingProfile{{ProviderID: "leonardo", Currency: "CNY", AccountCost: 100, IncludedCredits: 1, UsableCreditRate: 1, PaymentFeeRate: 0.5, TargetMargin: 0.5, RoundingStep: 0.01}}},
	}
	for index := range tests {
		if err := validateSalePricingSettings(&tests[index], providers); err == nil {
			t.Fatalf("invalid settings %d were accepted", index)
		}
	}
}

func TestCalculateSalePricingQuotePreservesTargetMargin(t *testing.T) {
	profile := salePricingProfile{
		ProviderID: "leonardo", Currency: "CNY", AccountCost: 100,
		IncludedCredits: 8500, UsableCreditRate: 0.9, OverheadRate: 0.05,
		PaymentFeeRate: 0.03, TargetMargin: 0.35, RoundingStep: 0.01,
	}
	economics := calculateSalePricingEconomics(profile)
	if math.Abs(economics.UsableCredits-7650) > 1e-9 || math.Abs(economics.LoadedAccountCost-105) > 1e-9 {
		t.Fatalf("unexpected economics: %+v", economics)
	}
	quote := calculateSaleMonetaryQuote(profile, economics, 1000)
	if math.Abs(quote.Price-22.14) > 1e-9 {
		t.Fatalf("price=%f, want 22.14", quote.Price)
	}
	if quote.Profit <= 0 || quote.Margin < profile.TargetMargin || quote.Margin >= profile.TargetMargin+0.001 {
		t.Fatalf("unexpected quote margin: %+v", quote)
	}
	if math.Abs(quote.Price-quote.PaymentFee-quote.Cost-quote.Profit) > 1e-9 {
		t.Fatalf("quote does not reconcile: %+v", quote)
	}
	if rounded := roundSalePrice(22.14, 0.01); rounded != 22.14 {
		t.Fatalf("exact price was rounded again: %f", rounded)
	}
	if rounded := roundSalePrice(22.140001, 0.01); rounded != 22.15 {
		t.Fatalf("non-exact price was not rounded upward: %f", rounded)
	}
}

func TestCalculateSaleRateQuotePreservesTargetMargin(t *testing.T) {
	profile := salePricingProfile{
		ProviderID: "leonardo", Currency: "CNY", AccountCost: 100,
		IncludedCredits: 8500, UsableCreditRate: 0.9, OverheadRate: 0.05,
		PaymentFeeRate: 0.03, TargetMargin: 0.35, RoundingStep: 0.01,
	}
	economics := calculateSalePricingEconomics(profile)
	quote := calculateSaleRateQuote(profile, economics, 241.916667)
	if quote.Credits != 241.916667 || quote.Price <= 0 {
		t.Fatalf("unexpected per-second quote: %+v", quote)
	}
	if quote.Margin < profile.TargetMargin {
		t.Fatalf("per-second quote missed target margin: %+v", quote)
	}
	if math.Abs(quote.Price-quote.PaymentFee-quote.Cost-quote.Profit) > 1e-9 {
		t.Fatalf("per-second quote does not reconcile: %+v", quote)
	}
}

func TestNormalizeAccountRouting(t *testing.T) {
	role, protected, slots, err := normalizeAccountRouting("video_reserved", 6804, 1, 5)
	if err != nil || role != "video_reserved" || protected != 6804 || slots != 1 {
		t.Fatalf("routing=(%q,%d,%d,%v)", role, protected, slots, err)
	}
	for _, fixture := range []struct {
		role      string
		protected int64
		slots     int
	}{
		{"invalid", 0, 0},
		{"general", -1, 0},
		{"video_reserved", 6804, 6},
	} {
		if _, _, _, err := normalizeAccountRouting(fixture.role, fixture.protected, fixture.slots, 5); err == nil {
			t.Fatalf("expected invalid routing fixture: %+v", fixture)
		}
	}
}

func TestParsePage(t *testing.T) {
	page, pageSize, err := parsePage("2", "50", 20, 100)
	if err != nil || page != 2 || pageSize != 50 {
		t.Fatalf("parsePage=(%d,%d,%v)", page, pageSize, err)
	}
	for _, values := range [][2]string{{"0", "20"}, {"1", "0"}, {"1", "101"}, {"invalid", "20"}} {
		if _, _, err := parsePage(values[0], values[1], 20, 100); err == nil {
			t.Fatalf("expected invalid pagination for %+v", values)
		}
	}
}

func TestParseAdminTimeRange(t *testing.T) {
	from, to, err := parseAdminTimeRange("2026-08-01T00:00:00+08:00", "2026-08-02T00:00:00+08:00")
	if err != nil || from == nil || to == nil || !from.Before(*to) {
		t.Fatalf("parseAdminTimeRange=(%v,%v,%v)", from, to, err)
	}
	for _, fixture := range [][2]string{
		{"invalid", ""},
		{"", "invalid"},
		{"2026-08-02T00:00:00Z", "2026-08-01T00:00:00Z"},
	} {
		if _, _, err := parseAdminTimeRange(fixture[0], fixture[1]); err == nil {
			t.Fatalf("expected invalid time range for %+v", fixture)
		}
	}
}

func TestValidAdminTaskStatus(t *testing.T) {
	for _, status := range []string{"", "active", "queued", "polling", "succeeded", "submission_uncertain"} {
		if !validAdminTaskStatus(status) {
			t.Fatalf("expected status %q to be accepted", status)
		}
	}
	if validAdminTaskStatus("unknown") {
		t.Fatal("unexpected unknown task status acceptance")
	}
}

func TestResponseErrorCode(t *testing.T) {
	if got := responseErrorCode(422, []byte(`{"error":{"code":"cost_unavailable"}}`)); got != "cost_unavailable" {
		t.Fatalf("responseErrorCode=%q", got)
	}
	if got := responseErrorCode(200, []byte(`{"error":{"code":"ignored"}}`)); got != "" {
		t.Fatalf("successful response error code=%q", got)
	}
}

func TestPaidCreationRequestRequiresKnownPOSTPath(t *testing.T) {
	for _, path := range []string{
		"/v1/images/generations", "/v1/images/edits", "/v1/tasks/images",
		"/v1/videos/generations", "/v1/audio/generations", "/v1/chat/completions",
	} {
		if !paidCreationRequest(httptest.NewRequest(http.MethodPost, path, nil)) {
			t.Fatalf("paid creation path %s was not protected", path)
		}
	}
	if paidCreationRequest(httptest.NewRequest(http.MethodGet, "/v1/tasks/task-id", nil)) {
		t.Fatal("task status read was classified as paid creation")
	}
}

func TestGenerationBulkheadRejectsWithoutCallingHandler(t *testing.T) {
	server := &Server{generationSlots: make(chan struct{}, 1), multipartSlots: make(chan struct{}, 1), syncSlots: make(chan struct{}, 1)}
	server.generationSlots <- struct{}{}
	called := false
	handler := server.generationBulkhead(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/videos/generations", strings.NewReader(`{}`)))
	if called || recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "gateway_overloaded") {
		t.Fatalf("called=%v status=%d body=%s", called, recorder.Code, recorder.Body.String())
	}
}

func TestLastChatInputDataImage(t *testing.T) {
	messages := []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}{{Role: "user", Content: []any{
		map[string]any{"type": "text", "text": "restyle this"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png"))}},
	}}}
	prompt, image, err := lastChatInput(messages)
	if err != nil || prompt != "restyle this" || image == nil || image.MediaType != "image/png" {
		t.Fatalf("unexpected chat input: %q %+v %v", prompt, image, err)
	}
}

func TestValidateGPTImage2Options(t *testing.T) {
	compression := 80
	model := domain.ModelConfig{Capabilities: []string{"quality"}}
	req := domain.ImageRequest{Model: "gpt-image-2", Size: "1536x1024", Quality: "high", ResponseFormat: "b64_json", OutputFormat: "jpeg", OutputCompression: &compression, Background: "opaque", Moderation: "auto"}
	if err := validateImageOptions(req, model); err != nil {
		t.Fatal(err)
	}
	req.Background = "transparent"
	if err := validateImageOptions(req, model); err == nil {
		t.Fatal("expected transparent background to be rejected")
	}
	req.Background = "opaque"
	req.OutputFormat = "webp"
	if err := validateImageOptions(req, model); err == nil {
		t.Fatal("expected unsupported WebP encoding to be rejected before generation")
	}
}

func TestValidateImageOptionsUsesLeonardoPromptAndQualityLimits(t *testing.T) {
	model := domain.ModelConfig{Capabilities: []string{"quality"}}
	valid := domain.ImageRequest{Model: "gpt-image-2", Prompt: strings.Repeat("a", 9999), Size: "1024x1024", Quality: "low"}
	if err := validateModelPrompt(valid.Model, valid.Prompt); err != nil {
		t.Fatal(err)
	}
	if err := validateImageOptions(valid, model); err != nil {
		t.Fatal(err)
	}
	valid.Prompt += "a"
	if err := validateModelPrompt(valid.Model, valid.Prompt); err == nil {
		t.Fatal("expected a 10000-character image prompt to be rejected")
	}
	if err := validateImageOptions(domain.ImageRequest{Model: "nano-banana-2", Prompt: "test", Size: "1024x1024", Quality: "auto"}, domain.ModelConfig{}); err == nil {
		t.Fatal("expected quality to be rejected for nano-banana-2")
	}
}

func TestNormalizeImageEstimateRequest(t *testing.T) {
	gpt, err := normalizeImageEstimateRequest(imageEstimateRequest{Model: "gpt-image-2", Size: "auto", Quality: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if gpt.Model != "gpt-image-2" || gpt.Size != "1024x1024" || gpt.Quality != "low" || gpt.N != 1 {
		t.Fatalf("unexpected GPT estimate request: %+v", gpt)
	}
	nano, err := normalizeImageEstimateRequest(imageEstimateRequest{Model: "nano-banana-2", Size: "5504x3072", N: 4})
	if err != nil {
		t.Fatal(err)
	}
	if nano.Quality != "" || nano.N != 4 {
		t.Fatalf("unexpected Nano estimate request: %+v", nano)
	}
	if _, err := normalizeImageEstimateRequest(imageEstimateRequest{Model: "nano-banana-2", Size: "1024x1024", Quality: "high"}); err == nil {
		t.Fatal("expected quality to be rejected for Nano Banana")
	}
	if _, err := normalizeImageEstimateRequest(imageEstimateRequest{Model: "gpt-image-2", Size: "1024x1024", N: 2}); err == nil {
		t.Fatal("expected GPT quantity greater than one to be rejected")
	}
}

func TestEstimateImageCostReturnsExactBreakdown(t *testing.T) {
	models := map[string]domain.ModelConfig{
		"gpt-image-2":      {ID: "gpt-image-2", Capabilities: []string{"text-to-image", "quality"}},
		"nano-banana-2":    {ID: "nano-banana-2", Capabilities: []string{"text-to-image"}},
		"seedream-5.0-pro": {ID: "seedream-5.0-pro", Capabilities: []string{"text-to-image"}},
	}
	rules := map[string]domain.ModelCostRule{
		"image|gpt-image-2|2048x2048|high||\x00":  {ID: 1, UnitTokens: 1033, PriceVersion: "v1", Source: "schema"},
		"image|nano-banana-2|4096x4096|||\x00":    {ID: 2, UnitTokens: 160, PriceVersion: "v1", Source: "schema"},
		"image|seedream-5.0-pro|1024x1024|||\x00": {ID: 3, UnitTokens: 45, PriceVersion: "v1", Source: "schema"},
		"image|seedream-5.0-pro|2048x2048|||\x00": {ID: 4, UnitTokens: 90, PriceVersion: "v1", Source: "schema"},
	}
	fake := fakeImageEstimateStore{models: models, rules: rules}
	gpt, err := estimateImageCost(context.Background(), fake, imageEstimateRequest{Model: "gpt-image-2", Size: "1536x1024", Quality: "high"}, []string{"*"}, true)
	if err != nil || gpt.UnitTokens != 388 || gpt.EstimatedTokens != 388 || gpt.PricingBasis != "pixel_formula" || gpt.QualityMultiplier == nil || *gpt.QualityMultiplier != 35.167 {
		t.Fatalf("GPT estimate=%+v err=%v", gpt, err)
	}
	nano, err := estimateImageCost(context.Background(), fake, imageEstimateRequest{Model: "nano-banana-2", Size: "5504x3072", N: 4}, nil, false)
	if err != nil || nano.UnitTokens != 160 || nano.EstimatedTokens != 640 || nano.PricingTier != "large" {
		t.Fatalf("Nano estimate=%+v err=%v", nano, err)
	}
	seedream, err := estimateImageCost(context.Background(), fake, imageEstimateRequest{Model: "seedream-5.0-pro", Size: "2048x2048", N: 2}, nil, false)
	if err != nil || seedream.UnitTokens != 90 || seedream.EstimatedTokens != 180 || seedream.PricingBasis != "size_threshold" || seedream.PricingTier != "2k" {
		t.Fatalf("Seedream estimate=%+v err=%v", seedream, err)
	}
	if _, err := estimateImageCost(context.Background(), fake, imageEstimateRequest{Model: "gpt-image-2", Size: "1024x1024"}, []string{"nano-banana-2"}, true); err == nil {
		t.Fatal("expected API key model restriction to be enforced")
	}
}

func TestEstimateImageCostMatrixUsesAdmissionPricing(t *testing.T) {
	models := map[string]domain.ModelConfig{
		"gpt-image-2":   {ID: "gpt-image-2", Capabilities: []string{"text-to-image", "quality"}},
		"nano-banana-2": {ID: "nano-banana-2", Capabilities: []string{"text-to-image"}},
	}
	rules := map[string]domain.ModelCostRule{
		"image|gpt-image-2|1024x1024|low||\x00":    {ID: 1, UnitTokens: 8, PriceVersion: "schema-current", Source: "leonardo-schema"},
		"image|gpt-image-2|1024x1024|medium||\x00": {ID: 2, UnitTokens: 65, PriceVersion: "schema-current", Source: "leonardo-schema"},
		"image|gpt-image-2|1024x1024|high||\x00":   {ID: 3, UnitTokens: 259, PriceVersion: "schema-current", Source: "leonardo-schema"},
		"image|nano-banana-2|1024x1024|||\x00":     {ID: 4, UnitTokens: 80, PriceVersion: "schema-current", Source: "leonardo-schema"},
	}
	fake := fakeImageEstimateStore{models: models, rules: rules}

	gpt, err := estimateImageCostMatrix(context.Background(), fake, imageCostMatrixRequest{
		Model: "gpt-image-2", Sizes: []string{"1024x1024", "1024x1024"},
	})
	if err != nil || len(gpt.Rows) != 1 || len(gpt.Qualities) != 3 {
		t.Fatalf("GPT matrix=%+v err=%v", gpt, err)
	}
	if gpt.Rows[0].Costs["low"] != 8 || gpt.Rows[0].Costs["medium"] != 65 || gpt.Rows[0].Costs["high"] != 259 {
		t.Fatalf("unexpected GPT costs: %+v", gpt.Rows[0].Costs)
	}
	if len(gpt.PriceVersions) != 1 || gpt.PriceVersions[0] != "schema-current" || len(gpt.Sources) != 1 || gpt.Sources[0] != "leonardo-schema" {
		t.Fatalf("unexpected GPT provenance: %+v", gpt)
	}

	nano, err := estimateImageCostMatrix(context.Background(), fake, imageCostMatrixRequest{
		Model: "nano-banana-2", Sizes: []string{"1024x1024"},
	})
	if err != nil || len(nano.Rows) != 1 || nano.Rows[0].Costs["fixed"] != 80 {
		t.Fatalf("Nano matrix=%+v err=%v", nano, err)
	}
	if _, err := estimateImageCostMatrix(context.Background(), fake, imageCostMatrixRequest{Model: "nano-banana-2"}); err == nil {
		t.Fatal("expected an empty size matrix to be rejected")
	}
}

func TestEstimateVideoCostUsesAdmissionPricingPath(t *testing.T) {
	disabled := false
	models := map[string]domain.ModelConfig{
		"seedance-2.0-fast": {ID: "seedance-2.0-fast", Capabilities: []string{"text-to-video", "video-reference"}},
		"flux-3-video":      {ID: "flux-3-video", Capabilities: []string{"text-to-video", "video-reference"}},
		"veo-3.1":           {ID: "veo-3.1", Capabilities: []string{"text-to-video", "image-to-video"}},
		"grok-imagine-1.5":  {ID: "grok-imagine-1.5", Capabilities: []string{"image-to-video"}},
	}
	rules := map[string]domain.ModelCostRule{
		"video|seedance-2.0-fast|||720p|\b":   {ID: 10, UnitTokens: 1935, PriceVersion: "schema-1.247.2", Source: "leonardo-schema"},
		"video|flux-3-video|||1080p|\b":       {ID: 13, UnitTokens: 2928, PriceVersion: "schema-1.255.2-flux3", Source: "leonardo-schema"},
		"video|veo-3.1|||2160p|\x04":          {ID: 11, UnitTokens: 3200, PriceVersion: "schema-1.247.2", Source: "leonardo-schema"},
		"video|grok-imagine-1.5|||1080p|\x06": {ID: 12, UnitTokens: 1740, PriceVersion: "schema-1.247.2", Source: "leonardo-schema"},
	}
	fake := fakeImageEstimateStore{models: models, rules: rules}

	seedance, err := estimateVideoCost(context.Background(), fake, videoEstimateRequest{
		Model: "seedance-2.0-fast", Duration: 8, Size: "1280x720", Resolution: "720p", HasVideoReference: true,
	}, []string{"*"}, true)
	if err != nil || seedance.BaseTokens != 1935 || seedance.EstimatedTokens != 2032 || !seedance.HasVideoReference || seedance.PriceVersion != "schema-1.247.2" {
		t.Fatalf("Seedance estimate=%+v err=%v", seedance, err)
	}
	if seedance.GenerateAudio == nil || !*seedance.GenerateAudio || len(seedance.AppliedModifiers) != 1 {
		t.Fatalf("Seedance normalized modifiers=%+v", seedance)
	}
	flux3, err := estimateVideoCost(context.Background(), fake, videoEstimateRequest{
		Model: "flux-3-video", Duration: 8, Size: "2520x1080", HasVideoReference: true,
	}, []string{"*"}, true)
	if err != nil || flux3.Resolution != "1080p" || flux3.BaseTokens != 2928 || flux3.EstimatedTokens != 5456 || len(flux3.AppliedModifiers) != 1 || flux3.AppliedModifiers[0] != "flux_video_reference" || !slices.Contains(flux3.CostParameters, "size") || !slices.Contains(flux3.CostParameters, "has_video_reference") {
		t.Fatalf("FLUX 3 Video estimate=%+v err=%v", flux3, err)
	}

	veo, err := estimateVideoCost(context.Background(), fake, videoEstimateRequest{
		Model: "veo-3.1", Duration: 4, Size: "1280x720", Resolution: "2160p", GenerateAudio: &disabled,
	}, nil, false)
	if err != nil || veo.BaseTokens != 3200 || veo.EstimatedTokens != 1600 || len(veo.AppliedModifiers) != 1 {
		t.Fatalf("Veo estimate=%+v err=%v", veo, err)
	}

	grok, err := estimateVideoCost(context.Background(), fake, videoEstimateRequest{
		Model: "grok-imagine-1.5", Duration: 6, Size: "1424x1424", GenerateAudio: &disabled,
	}, nil, false)
	if err != nil || grok.Resolution != "1080p" || grok.BaseTokens != 1740 || grok.EstimatedTokens != 1740 || len(grok.AppliedModifiers) != 0 || !slices.Contains(grok.CostParameters, "size") {
		t.Fatalf("Grok estimate=%+v err=%v", grok, err)
	}
	if _, err := estimateVideoCost(context.Background(), fake, videoEstimateRequest{
		Model: "grok-imagine-1.5", Duration: 6, Size: "1424x1424", Resolution: "480p",
	}, nil, false); err == nil {
		t.Fatal("expected Grok size/resolution mismatch to be rejected")
	}

	if _, err := estimateVideoCost(context.Background(), fake, videoEstimateRequest{Model: "veo-3.1"}, []string{"seedance-2.0-fast"}, true); err == nil {
		t.Fatal("expected API key model restriction to be enforced for video estimates")
	}
}

func TestApplyAndValidateAudioDefaults(t *testing.T) {
	model := domain.ModelConfig{Capabilities: []string{"text-to-sound"}, Defaults: []byte(`{"quantity":1,"duration":2,"loop":false,"prompt_influence":0.7}`)}
	request := domain.AudioRequest{Model: "sound-effects-v2", Prompt: "rain"}
	if err := applyAndValidateAudioDefaults(&request, model); err != nil {
		t.Fatal(err)
	}
	if request.N != 1 || request.Duration != 2 || request.PromptInfluence == nil || *request.PromptInfluence != 0.7 {
		t.Fatalf("unexpected sound effect defaults: %+v", request)
	}

	dialogue := domain.AudioRequest{Model: "dialogue-v3", Prompt: "hello", Voice: "unknown"}
	dialogueModel := domain.ModelConfig{Capabilities: []string{"text-to-speech"}, Defaults: []byte(`{"quantity":1,"voice":"george","language":"en","prompt_influence":0.5}`)}
	if err := applyAndValidateAudioDefaults(&dialogue, dialogueModel); err == nil {
		t.Fatal("expected unknown voice to be rejected")
	}

	music := domain.AudioRequest{Model: "music-v1", Prompt: "piano", Duration: 4}
	musicModel := domain.ModelConfig{Capabilities: []string{"text-to-music"}, Defaults: []byte(`{"quantity":1,"duration_minutes":1}`)}
	if err := applyAndValidateAudioDefaults(&music, musicModel); err == nil {
		t.Fatal("expected music duration seconds to be rejected")
	}
}

func TestApplyAndValidateAudioPromptInfluenceRange(t *testing.T) {
	value := 1.1
	request := domain.AudioRequest{Model: "sound-effects-v2", Prompt: "rain", Duration: 2, PromptInfluence: &value}
	model := domain.ModelConfig{Capabilities: []string{"text-to-sound"}}
	if err := applyAndValidateAudioDefaults(&request, model); err == nil {
		t.Fatal("expected prompt influence above one to be rejected")
	}
}

func TestNormalizeVideoOptionsEnforcesVeoReferenceContract(t *testing.T) {
	spec, _ := videospec.Get("veo-3.1")
	request := domain.VideoRequest{
		Model: "veo-3.1", Duration: 8, Size: "1280x720", Resolution: "720p",
		ReferenceImages: []domain.SourceMedia{{Filename: "reference.png"}},
	}
	if err := normalizeVideoOptions(&request, spec); err != nil {
		t.Fatalf("valid Veo reference request was rejected: %v", err)
	}

	invalidDuration := request
	invalidDuration.Duration = 6
	if err := normalizeVideoOptions(&invalidDuration, spec); err == nil {
		t.Fatal("expected Veo ordinary references with duration != 8 to be rejected")
	}

	invalidSize := request
	invalidSize.Size = "720x1280"
	if err := normalizeVideoOptions(&invalidSize, spec); err == nil {
		t.Fatal("expected Veo ordinary references outside 16:9 to be rejected")
	}

	frameOnly := domain.VideoRequest{Model: "veo-3.1", Duration: 6, Size: "720x1280", Resolution: "720p", StartFrame: &domain.SourceMedia{Filename: "start.png"}}
	if err := normalizeVideoOptions(&frameOnly, spec); err != nil {
		t.Fatalf("Veo start-frame request should retain its normal duration and size options: %v", err)
	}
}

func TestApplyAndValidateAudioPromptLimitsMatchSchema(t *testing.T) {
	models := map[string]domain.ModelConfig{
		"dialogue-v3":      {Capabilities: []string{"text-to-speech"}, Defaults: []byte(`{"voice":"george","language":"en"}`)},
		"music-v1":         {Capabilities: []string{"text-to-music"}, Defaults: []byte(`{"duration_minutes":1}`)},
		"sound-effects-v2": {Capabilities: []string{"text-to-sound"}, Defaults: []byte(`{"duration":2}`)},
	}
	limits := map[string]int{"dialogue-v3": 5000, "music-v1": 9999, "sound-effects-v2": 9999}
	for model, limit := range limits {
		valid := domain.AudioRequest{Model: model, Prompt: strings.Repeat("生", limit)}
		if err := validateModelPrompt(model, valid.Prompt); err != nil {
			t.Fatalf("%s rejected %d-character prompt: %v", model, limit, err)
		}
		if err := applyAndValidateAudioDefaults(&valid, models[model]); err != nil {
			t.Fatalf("%s rejected %d-character prompt: %v", model, limit, err)
		}
		invalid := domain.AudioRequest{Model: model, Prompt: strings.Repeat("生", limit+1)}
		if err := validateModelPrompt(model, invalid.Prompt); err == nil {
			t.Fatalf("%s accepted %d-character prompt", model, limit+1)
		}
	}
}

func TestTranscodePNGToJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	quality := 80
	out, err := transcodeImage(source.Bytes(), "jpeg", &quality)
	if err != nil {
		t.Fatal(err)
	}
	if _, format, err := image.Decode(bytes.NewReader(out)); err != nil || format != "jpeg" {
		t.Fatalf("unexpected output format %q: %v", format, err)
	}
}

func TestLastChatInputRejectsRemoteImage(t *testing.T) {
	messages := []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}{{Role: "user", Content: []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "http://127.0.0.1/private"}}}}}
	if _, _, err := lastChatInput(messages); err == nil {
		t.Fatal("expected remote image URL to be rejected")
	}
}

func TestLastChatInputRejectsMultipleImages(t *testing.T) {
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png"))
	messages := []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}{{Role: "user", Content: []any{
		map[string]any{"type": "text", "text": "combine these"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
	}}}
	if _, _, err := lastChatInput(messages); err == nil {
		t.Fatal("expected multiple image_url parts to be rejected")
	}
}

func TestLastChatInputValidatesPartsAndJoinsText(t *testing.T) {
	messages := []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}{{Role: "user", Content: []any{
		map[string]any{"type": "text", "text": "first"},
		map[string]any{"type": "text", "text": "second"},
	}}}
	prompt, _, err := lastChatInput(messages)
	if err != nil || prompt != "first\nsecond" {
		t.Fatalf("unexpected chat input: prompt=%q err=%v", prompt, err)
	}
	messages[0].Content = []any{map[string]any{"type": "text", "text": "test", "unknown": true}}
	if _, _, err := lastChatInput(messages); err == nil {
		t.Fatal("expected unsupported chat content field to be rejected")
	}
	messages[0] = struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}{Role: "tool", Content: "test"}
	if _, _, err := lastChatInput(messages); err == nil {
		t.Fatal("expected unsupported chat role to be rejected")
	}
}

func TestNormalizeAsyncImageRequest(t *testing.T) {
	req, err := normalizeAsyncImageRequest(domain.ImageRequest{Model: "gpt-image-2", Prompt: "test"})
	if err != nil || req.ResponseFormat != "url" {
		t.Fatalf("unexpected async image defaults: %+v %v", req, err)
	}
	compression := 80
	for _, invalid := range []domain.ImageRequest{
		{Model: "gpt-image-2", Prompt: "test", ResponseFormat: "b64_json"},
		{Model: "gpt-image-2", Prompt: "test", OutputFormat: "jpeg"},
		{Model: "gpt-image-2", Prompt: "test", OutputCompression: &compression},
		{Model: "gpt-image-2", Prompt: "test", SourceImage: &domain.SourceImage{}},
	} {
		if _, err := normalizeAsyncImageRequest(invalid); err == nil {
			t.Fatalf("expected async image request to be rejected: %+v", invalid)
		}
	}
}

func TestModelListForKeyFiltersDisallowedModels(t *testing.T) {
	models := []domain.ModelConfig{{ID: "gpt-image-2"}, {ID: "seedance-2.0-fast"}}
	filtered := modelListForKey(models, []string{"gpt-image-2"})
	if len(filtered) != 1 || filtered[0]["id"] != "gpt-image-2" {
		t.Fatalf("unexpected filtered models: %+v", filtered)
	}
	if filtered[0]["owned_by"] != "aiv2api" {
		t.Fatalf("public model owner leaked provider identity: %+v", filtered[0])
	}
	if all := modelListForKey(models, []string{"*"}); len(all) != len(models) {
		t.Fatalf("wildcard should expose all models: %+v", all)
	}
}

func TestPublicTaskResponseHidesInternalAccounting(t *testing.T) {
	accountID := uuid.New()
	tokensBefore, tokensAfter := int64(100), int64(90)
	task := domain.Task{
		ID: uuid.New(), AccountID: &accountID, Kind: "image", Status: domain.TaskQueued,
		Model: "gpt-image-2", Prompt: "test", TokensBefore: &tokensBefore, TokensAfter: &tokensAfter,
		GenerationID:    "upstream-generation-id",
		UpstreamRequest: json.RawMessage(`{"model":"gpt-image-2","public":false,"parameters":{"prompt":"test"}}`),
		ErrorDetails:    json.RawMessage(`{"source":"leonardo","generation_id":"upstream-generation-id","detail_error":"internal","provider_error_code":"PROVIDER_ERROR"}`),
		CreatedAt:       time.Now(), UpdatedAt: time.Now(),
	}
	payload, err := json.Marshal(newPublicTaskResponse(task))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"account_id", "tokens_before", "tokens_after", "provider_id", "api_key_id", "request", "upstream_request", "generation_id"} {
		if strings.Contains(string(payload), `"`+field+`"`) {
			t.Fatalf("public task leaked %s: %s", field, payload)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	details := decoded["error_details"].(map[string]any)
	if details["provider_error_code"] != "PROVIDER_ERROR" || details["source"] != nil || details["detail_error"] != nil {
		t.Fatalf("unexpected public error details: %+v", details)
	}
}

func TestPublicTaskStatusCollapsesInternalPhases(t *testing.T) {
	tests := map[string]string{
		domain.TaskQueued:              "queued",
		domain.TaskReserving:           "processing",
		domain.TaskUploading:           "processing",
		domain.TaskSubmitted:           "processing",
		domain.TaskPolling:             "processing",
		domain.TaskSubmissionUncertain: "processing",
		domain.TaskSucceeded:           "succeeded",
		domain.TaskFailed:              "failed",
		domain.TaskCancelled:           "cancelled",
	}
	for internal, want := range tests {
		if got := publicTaskStatus(internal); got != want {
			t.Errorf("publicTaskStatus(%q)=%q, want %q", internal, got, want)
		}
	}
}

func TestPublicJSONRequestsRejectInternalFields(t *testing.T) {
	for name, body := range map[string]string{
		"video": `{"model":"seedance-2.0-fast","prompt":"test","reference_images":[{"path":"asset-secret"}]}`,
		"audio": `{"model":"sound-effects-v2","prompt":"test","public":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			var err error
			if name == "video" {
				var value publicVideoJSONRequest
				err = decodeJSON(req, &value)
			} else {
				var value publicAudioJSONRequest
				err = decodeJSON(req, &value)
			}
			if err == nil {
				t.Fatal("expected undocumented internal field to be rejected")
			}
		})
	}
}

func TestClientIPFromRequestTrustsOnlyLoopbackProxy(t *testing.T) {
	proxied := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	proxied.RemoteAddr = "127.0.0.1:12345"
	proxied.Header.Set("X-Real-IP", "203.0.113.9")
	if got := clientIPFromRequest(proxied); got != "203.0.113.9" {
		t.Fatalf("proxied client IP=%q", got)
	}
	direct := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	direct.RemoteAddr = "198.51.100.8:12345"
	direct.Header.Set("X-Real-IP", "203.0.113.9")
	if got := clientIPFromRequest(direct); got != "198.51.100.8" {
		t.Fatalf("direct request trusted spoofed proxy header: %q", got)
	}
}

func TestSecurityHeaders(t *testing.T) {
	server := &Server{}
	handler := server.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "https://example.com/docs", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	for _, header := range []string{"Content-Security-Policy", "Permissions-Policy", "Referrer-Policy", "Strict-Transport-Security", "X-Content-Type-Options", "X-Frame-Options"} {
		if recorder.Header().Get(header) == "" {
			t.Fatalf("missing security header %s", header)
		}
	}
}
