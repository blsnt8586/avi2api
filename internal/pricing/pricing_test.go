package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/imageopts"
	"github.com/leonardo2api/leonardo2api/internal/store"
)

type fakeRules map[string]domain.ModelCostRule

func (rules fakeRules) FindModelCostRule(_ context.Context, kind, model, size, quality, resolution string, duration int) (domain.ModelCostRule, error) {
	rule, ok := rules[kind+"|"+model+"|"+size+"|"+quality+"|"+resolution+"|"+string(rune(duration))]
	if !ok {
		return domain.ModelCostRule{}, store.ErrNotFound
	}
	return rule, nil
}

func TestImageEstimateDefaultsAndQuantity(t *testing.T) {
	rules := fakeRules{"image|gpt-image-2|1024x1024|low||\x00": {
		ID: 7, UnitTokens: 8, PriceVersion: "2026-07-15-ui", Source: "leonardo-ui",
	}}
	estimate, err := Image(context.Background(), rules, domain.ImageRequest{Model: "gpt-image-2", N: 3})
	if err != nil || estimate.Tokens != 24 || estimate.UnitTokens != 8 || estimate.RuleID != 7 {
		t.Fatalf("estimate=%+v err=%v", estimate, err)
	}
	if estimate.RuleSize != "1024x1024" || estimate.PriceVersion != "2026-07-15-ui" || estimate.Source != "leonardo-ui" {
		t.Fatalf("estimate metadata=%+v", estimate)
	}
}

func TestAdobeGPTImageEstimateUsesBKSRule(t *testing.T) {
	rules := fakeRules{"image|gpt-image-2|1024x1024|low||\x00": {
		ID: 17, UnitTokens: 41, PriceVersion: "adobe-bks-live", Source: "adobe-bks",
	}}
	estimate, err := ImageForProvider(context.Background(), rules, "adobe", domain.ImageRequest{Model: imageopts.GPTImage2, Size: "1024x1024", Quality: "low", N: 1})
	if err != nil || estimate.Tokens != 41 || estimate.UnitTokens != 41 || estimate.RuleID != 17 || estimate.Source != "adobe-bks" {
		t.Fatalf("estimate=%+v err=%v", estimate, err)
	}
}

func TestAdobeGPTImageEstimateAllowsZeroFairUseRule(t *testing.T) {
	rules := fakeRules{"image|gpt-image-2|1024x1024|low||\x00": {
		ID: 18, UnitTokens: 0, PriceVersion: "adobe-bks-fair-use", Source: "adobe-bks",
	}}
	estimate, err := ImageForProvider(context.Background(), rules, "adobe", domain.ImageRequest{Model: imageopts.GPTImage2, Size: "1024x1024", Quality: "low", N: 1})
	if err != nil || estimate.Tokens != 0 || estimate.UnitTokens != 0 || estimate.RuleID != 18 {
		t.Fatalf("estimate=%+v err=%v", estimate, err)
	}
}

func TestVideoEstimateDefaults(t *testing.T) {
	rules := fakeRules{"video|seedance-2.0-mini|||720p|\b": {ID: 9, UnitTokens: 1280, PriceVersion: "schema-1.247.2", Source: "leonardo-schema"}}
	estimate, err := Video(context.Background(), rules, domain.VideoRequest{Model: "seedance-2.0-mini"})
	if err != nil || estimate.Tokens != 1280 || estimate.UnitTokens != 1280 || estimate.RuleID != 9 || estimate.PriceVersion != "schema-1.247.2" || estimate.Source != "leonardo-schema" {
		t.Fatalf("estimate=%+v err=%v", estimate, err)
	}
}

func TestAdobeKlingVideoEstimateSelectsWorkflowRule(t *testing.T) {
	rules := fakeRules{
		"video|kling-3.0-omni||t2v|720p|\x05": {ID: 70, UnitTokens: 100},
		"video|kling-3.0-omni||i2v|720p|\x05": {ID: 71, UnitTokens: 120},
		"video|kling-3.0-omni||rtv|720p|\x05": {ID: 72, UnitTokens: 140},
	}
	tests := []struct {
		name string
		req  domain.VideoRequest
		id   int64
	}{
		{name: "text", req: domain.VideoRequest{Model: "kling-3.0-omni"}, id: 70},
		{name: "frame", req: domain.VideoRequest{Model: "kling-3.0-omni", StartFrame: &domain.SourceMedia{Path: "start.png"}}, id: 71},
		{name: "reference images", req: domain.VideoRequest{Model: "kling-3.0-omni", ReferenceImages: []domain.SourceMedia{{Path: "ref.png"}}}, id: 72},
	}
	for _, test := range tests {
		estimate, err := VideoForProvider(context.Background(), rules, "adobe", test.req)
		if err != nil || estimate.RuleID != test.id {
			t.Fatalf("%s estimate=%+v err=%v", test.name, estimate, err)
		}
	}
}

func TestVideoEstimateReferenceModifier(t *testing.T) {
	rules := fakeRules{"video|seedance-2.0-fast|||720p|\b": {ID: 9, UnitTokens: 1935}}
	estimate, err := Video(context.Background(), rules, domain.VideoRequest{
		Model: "seedance-2.0-fast", Duration: 8, Resolution: "720p",
		ReferenceVideos: []domain.SourceMedia{{Path: "reference.mp4"}},
	})
	if err != nil || estimate.Tokens != 2032 || estimate.RuleID != 9 {
		t.Fatalf("estimate=%+v err=%v", estimate, err)
	}
}

func TestSeedance25VideoReferenceUsesSchemaRates(t *testing.T) {
	rules := fakeRules{
		"video|seedance-2.5|||480p|\x08": {ID: 40, UnitTokens: 1440},
		"video|seedance-2.5|||720p|\x08": {ID: 41, UnitTokens: 2336},
	}
	standard, err := Video(context.Background(), rules, domain.VideoRequest{Model: "seedance-2.5", Duration: 8, Size: "640x640", Resolution: "480p", ReferenceVideos: []domain.SourceMedia{{Path: "reference.mp4"}}})
	if err != nil || standard.Tokens != 2064 || standard.RuleID != 40 {
		t.Fatalf("480p estimate=%+v err=%v", standard, err)
	}
	hd, err := Video(context.Background(), rules, domain.VideoRequest{Model: "seedance-2.5", Duration: 8, Size: "1280x720", Resolution: "720p", ReferenceVideos: []domain.SourceMedia{{Path: "reference.mp4"}}})
	if err != nil || hd.Tokens != 3728 || hd.RuleID != 41 {
		t.Fatalf("720p estimate=%+v err=%v", hd, err)
	}
}

func TestFlux3VideoEstimateUsesResolutionAndReferenceRates(t *testing.T) {
	rules := fakeRules{
		"video|flux-3-video|||720p|\b":  {ID: 30, UnitTokens: 1720, PriceVersion: "schema-1.255.2-flux3"},
		"video|flux-3-video|||1080p|\b": {ID: 31, UnitTokens: 2928, PriceVersion: "schema-1.255.2-flux3"},
	}
	tests := []struct {
		name       string
		request    domain.VideoRequest
		wantTokens int64
		wantRuleID int64
	}{
		{name: "720p", request: domain.VideoRequest{Model: "flux-3-video"}, wantTokens: 1720, wantRuleID: 30},
		{name: "720p video reference", request: domain.VideoRequest{Model: "flux-3-video", ReferenceVideos: []domain.SourceMedia{{Path: "reference.mp4"}}}, wantTokens: 4344, wantRuleID: 30},
		{name: "1080p", request: domain.VideoRequest{Model: "flux-3-video", Duration: 8, Size: "2520x1080", Resolution: "1080p"}, wantTokens: 2928, wantRuleID: 31},
		{name: "1080p video reference", request: domain.VideoRequest{Model: "flux-3-video", Duration: 8, Size: "2520x1080", Resolution: "1080p", ReferenceVideos: []domain.SourceMedia{{Path: "reference.mp4"}}}, wantTokens: 5456, wantRuleID: 31},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			estimate, err := Video(context.Background(), rules, test.request)
			if err != nil || estimate.Tokens != test.wantTokens || estimate.RuleID != test.wantRuleID {
				t.Fatalf("estimate=%+v err=%v want_tokens=%d want_rule=%d", estimate, err, test.wantTokens, test.wantRuleID)
			}
		})
	}
}

func TestVideoEstimateUsesModelDefaultDuration(t *testing.T) {
	rules := fakeRules{"video|kling-o3-omni|||1080p|\x05": {ID: 10, UnitTokens: 1400}}
	estimate, err := Video(context.Background(), rules, domain.VideoRequest{Model: "kling-o3-omni", Resolution: "1080p"})
	if err != nil || estimate.Tokens != 1400 || estimate.RuleID != 10 {
		t.Fatalf("estimate=%+v err=%v", estimate, err)
	}
}

func TestKlingO3OmniVideoReferenceEstimate(t *testing.T) {
	disabled := false
	rules := fakeRules{"video|kling-o3-omni|||720p|\x05": {ID: 12, UnitTokens: 1120}}
	request := domain.VideoRequest{
		Model: "kling-o3-omni", Duration: 5, Size: "1280x720", Resolution: "720p",
		ReferenceVideos: []domain.SourceMedia{{Path: "reference.mp4"}}, GenerateAudio: &disabled,
	}
	estimate, err := Video(context.Background(), rules, request)
	if err != nil || estimate.Tokens != 1260 || estimate.UnitTokens != 1120 || estimate.RuleID != 12 {
		t.Fatalf("estimate=%+v err=%v", estimate, err)
	}
}

func TestMiniMaxH3EstimateUsesSchemaDefaults(t *testing.T) {
	rules := fakeRules{"video|minimax-h3|||1440p|\x05": {ID: 11, UnitTokens: 700}}
	estimate, err := Video(context.Background(), rules, domain.VideoRequest{Model: "minimax-h3"})
	if err != nil || estimate.Tokens != 700 || estimate.RuleID != 11 {
		t.Fatalf("estimate=%+v err=%v", estimate, err)
	}
}

func TestGrokImagine15EstimateUsesExactSizeTier(t *testing.T) {
	rules := fakeRules{
		"video|grok-imagine-1.5|||480p|\x06":  {ID: 20, UnitTokens: 600},
		"video|grok-imagine-1.5|||1080p|\x06": {ID: 21, UnitTokens: 1740},
	}
	defaults, err := Video(context.Background(), rules, domain.VideoRequest{Model: "grok-imagine-1.5"})
	if err != nil || defaults.Tokens != 600 || defaults.RuleID != 20 {
		t.Fatalf("default estimate=%+v err=%v", defaults, err)
	}
	high, err := Video(context.Background(), rules, domain.VideoRequest{Model: "grok-imagine-1.5", Size: "1424x1424", Duration: 6})
	if err != nil || high.Tokens != 1740 || high.RuleID != 21 {
		t.Fatalf("high-tier estimate=%+v err=%v", high, err)
	}
	_, err = Video(context.Background(), rules, domain.VideoRequest{Model: "grok-imagine-1.5", Size: "1424x1424", Resolution: "480p", Duration: 6})
	if !errors.Is(err, ErrCostUnavailable) {
		t.Fatalf("mismatched size tier error=%v", err)
	}
}

func TestVideoEstimateWithoutGeneratedAudio(t *testing.T) {
	disabled := false
	tests := []struct {
		model      string
		resolution string
		duration   int
		withAudio  int64
		want       int64
	}{
		{"veo-3.1", "2160p", 4, 3200, 1600},
		{"veo-3.1-fast", "2160p", 4, 1400, 1200},
		{"kling-o3-omni", "720p", 3, 672, 504},
		{"kling-o3-omni", "1080p", 3, 840, 672},
		{"kling-o3-omni", "2160p", 3, 1260, 1260},
	}
	for index, test := range tests {
		rules := fakeRules{"video|" + test.model + "|||" + test.resolution + "|" + string(rune(test.duration)): {ID: int64(index + 1), UnitTokens: test.withAudio}}
		estimate, err := Video(context.Background(), rules, domain.VideoRequest{Model: test.model, Resolution: test.resolution, Duration: test.duration, GenerateAudio: &disabled})
		if err != nil || estimate.Tokens != test.want {
			t.Fatalf("%s/%s estimate=%+v err=%v want=%d", test.model, test.resolution, estimate, err, test.want)
		}
	}
}

func TestImageEstimateUsesModelSpecificCostRules(t *testing.T) {
	rules := fakeRules{
		"image|gpt-image-2|2048x2048|high||\x00":  {ID: 12, UnitTokens: 1033},
		"image|nano-banana-2|1024x1024|||\x00":    {ID: 13, UnitTokens: 80},
		"image|nano-banana-2|2048x2048|||\x00":    {ID: 14, UnitTokens: 120},
		"image|nano-banana-2|4096x4096|||\x00":    {ID: 15, UnitTokens: 160},
		"image|nano-banana-pro|2048x2048|||\x00":  {ID: 16, UnitTokens: 140},
		"image|nano-banana-pro|4096x4096|||\x00":  {ID: 17, UnitTokens: 250},
		"image|seedream-5.0-pro|1024x1024|||\x00": {ID: 18, UnitTokens: 45},
		"image|seedream-5.0-pro|2048x2048|||\x00": {ID: 19, UnitTokens: 90},
	}
	tests := []struct {
		request domain.ImageRequest
		want    int64
	}{
		{domain.ImageRequest{Model: "gpt-image-2", Size: "1536x1024", Quality: "high"}, 388},
		{domain.ImageRequest{Model: "nano-banana-2", Size: "768x1344"}, 80},
		{domain.ImageRequest{Model: "nano-banana-2", Size: "1536x2752"}, 120},
		{domain.ImageRequest{Model: "nano-banana-2", Size: "3168x1344"}, 120},
		{domain.ImageRequest{Model: "nano-banana-2", Size: "5504x3072"}, 160},
		{domain.ImageRequest{Model: "nano-banana-pro", Size: "2752x1536"}, 140},
		{domain.ImageRequest{Model: "nano-banana-pro", Size: "5504x3072"}, 250},
		{domain.ImageRequest{Model: "seedream-5.0-pro", Size: "1536x1536"}, 45},
		{domain.ImageRequest{Model: "seedream-5.0-pro", Size: "2016x1152"}, 45},
		{domain.ImageRequest{Model: "seedream-5.0-pro", Size: "2016x1153"}, 90},
		{domain.ImageRequest{Model: "seedream-5.0-pro", Size: "1153x2016"}, 90},
	}
	for _, test := range tests {
		estimate, err := Image(context.Background(), rules, test.request)
		if err != nil || estimate.Tokens != test.want {
			t.Fatalf("%s/%s estimate=%+v err=%v want=%d", test.request.Model, test.request.Size, estimate, err, test.want)
		}
	}
}

func TestEveryAcceptedEnumeratedImageSizeHasCost(t *testing.T) {
	rules := fakeRules{
		"image|gpt-image-2|1024x1024|low||\x00":    {ID: 1, UnitTokens: 8},
		"image|gpt-image-2|1024x1024|medium||\x00": {ID: 2, UnitTokens: 65},
		"image|gpt-image-2|1024x1024|high||\x00":   {ID: 3, UnitTokens: 259},
		"image|gpt-image-2|2048x2048|low||\x00":    {ID: 4, UnitTokens: 30},
		"image|gpt-image-2|2048x2048|medium||\x00": {ID: 5, UnitTokens: 260},
		"image|gpt-image-2|2048x2048|high||\x00":   {ID: 6, UnitTokens: 1033},
		"image|gpt-image-2|2880x2880|low||\x00":    {ID: 7, UnitTokens: 59},
		"image|gpt-image-2|2880x2880|medium||\x00": {ID: 8, UnitTokens: 513},
		"image|gpt-image-2|2880x2880|high||\x00":   {ID: 9, UnitTokens: 2042},
	}
	for _, model := range []string{imageopts.NanoBanana2, imageopts.NanoBananaPro} {
		rules["image|"+model+"|1024x1024|||\x00"] = domain.ModelCostRule{ID: 10, UnitTokens: 80}
		rules["image|"+model+"|2048x2048|||\x00"] = domain.ModelCostRule{ID: 11, UnitTokens: 120}
		rules["image|"+model+"|4096x4096|||\x00"] = domain.ModelCostRule{ID: 12, UnitTokens: 160}
	}
	tests := []struct {
		model               string
		maxWidth, maxHeight int
		qualities           []string
	}{
		{imageopts.GPTImage2, 3840, 3840, []string{"low", "medium", "high"}},
		{imageopts.NanoBanana2, 6336, 5504, []string{""}},
		{imageopts.NanoBananaPro, 6336, 5504, []string{""}},
	}
	for _, test := range tests {
		accepted := 0
		for width := 16; width <= test.maxWidth; width += 16 {
			for height := 16; height <= test.maxHeight; height += 16 {
				size := fmt.Sprintf("%dx%d", width, height)
				if _, _, err := imageopts.ParseSize(test.model, size); err != nil {
					continue
				}
				accepted++
				for _, quality := range test.qualities {
					estimate, err := Image(context.Background(), rules, domain.ImageRequest{Model: test.model, Size: size, Quality: quality})
					if err != nil || estimate.Tokens <= 0 {
						t.Fatalf("%s size=%s quality=%s estimate=%+v err=%v", test.model, size, quality, estimate, err)
					}
				}
			}
		}
		if accepted == 0 {
			t.Fatalf("%s accepted no sizes", test.model)
		}
	}
	seedreamRules := fakeRules{
		"image|seedream-5.0-pro|1024x1024|||\x00": {ID: 13, UnitTokens: 45},
		"image|seedream-5.0-pro|2048x2048|||\x00": {ID: 14, UnitTokens: 90},
	}
	for _, test := range []struct {
		size string
		want int64
	}{
		{"768x768", 180},
		{"1536x1536", 180},
		{"2016x1152", 180},
		{"2016x1153", 360},
		{"1153x2016", 360},
		{"2048x2048", 360},
	} {
		estimate, err := Image(context.Background(), seedreamRules, domain.ImageRequest{Model: imageopts.Seedream50Pro, Size: test.size, N: 4})
		if err != nil || estimate.Tokens != test.want {
			t.Fatalf("Seedream size=%s estimate=%+v err=%v want=%d", test.size, estimate, err, test.want)
		}
	}
}

func TestUnknownCostIsBlocked(t *testing.T) {
	_, err := Image(context.Background(), fakeRules{}, domain.ImageRequest{Model: "gpt-image-2", Size: "1536x1024", Quality: "high"})
	if err != ErrCostUnavailable {
		t.Fatalf("expected ErrCostUnavailable, got %v", err)
	}
}

func TestAudioEstimate(t *testing.T) {
	rules := fakeRules{
		"audio|music-v1||||\x03":         {ID: 11, UnitTokens: 2100},
		"audio|sound-effects-v2||||\x05": {ID: 12, UnitTokens: 10},
		"audio|dialogue-v3||||\x00":      {ID: 13, UnitTokens: 90},
	}
	music, err := Audio(context.Background(), rules, domain.AudioRequest{Model: "music-v1", DurationMinutes: 3, N: 2})
	if err != nil || music.Tokens != 4200 || music.RuleID != 11 {
		t.Fatalf("music estimate=%+v err=%v", music, err)
	}
	sfx, err := Audio(context.Background(), rules, domain.AudioRequest{Model: "sound-effects-v2", Duration: 5, N: 3})
	if err != nil || sfx.Tokens != 30 || sfx.RuleID != 12 {
		t.Fatalf("sound effect estimate=%+v err=%v", sfx, err)
	}
	dialogue, err := Audio(context.Background(), rules, domain.AudioRequest{Model: "dialogue-v3", Prompt: strings.Repeat("a", 1001), N: 2})
	if err != nil || dialogue.Tokens != 360 || dialogue.RuleID != 13 {
		t.Fatalf("dialogue estimate=%+v err=%v", dialogue, err)
	}
}
