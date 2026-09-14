package jobs

import (
	"errors"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/leonardo2api/leonardo2api/internal/adobe"
	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
)

func TestSubmissionAccountRouteableRequiresFreshJWT(t *testing.T) {
	now := time.Now()
	freshBalance := now.Add(-time.Minute)
	futureJWT := now.Add(time.Hour)
	expiredJWT := now.Add(-time.Minute)

	base := domain.Account{
		Status:               "active",
		AccessTokenExpiresAt: &futureJWT,
		LastCheckedAt:        &freshBalance,
		SubscriptionTokens:   100,
	}
	if !submissionAccountRouteable(base) {
		t.Fatal("fresh account was not routeable")
	}
	staleBalance := now.Add(-24 * time.Hour)
	base.LastCheckedAt = &staleBalance
	if !submissionAccountRouteable(base) {
		t.Fatal("authenticated account with a locally tracked balance was not routeable")
	}
	base.AccessTokenExpiresAt = &expiredJWT
	if submissionAccountRouteable(base) {
		t.Fatal("expired JWT account was routeable")
	}
	base.AccessTokenExpiresAt = nil
	if submissionAccountRouteable(base) {
		t.Fatal("account without JWT expiry was routeable")
	}
}

func TestUploadingTaskWithGenerationIDIsAlreadySubmitted(t *testing.T) {
	accountID := uuid.New()
	task := domain.Task{Status: domain.TaskUploading, GenerationID: "upstream-session", AccountID: &accountID}
	if !isSubmittedGenerationTask(task) {
		t.Fatal("an upstream session must never be submitted again after frame upload recovery")
	}
	task.GenerationID = ""
	if isSubmittedGenerationTask(task) {
		t.Fatal("pre-submission upload remains eligible for its first mutation")
	}
}

func TestUpstreamTaskState(t *testing.T) {
	for _, status := range []string{"PENDING", " pending "} {
		if got := upstreamTaskState(status); got != domain.TaskSubmitted {
			t.Fatalf("upstreamTaskState(%q)=%q, want %q", status, got, domain.TaskSubmitted)
		}
	}
	for _, status := range []string{"QUEUED", "WAITING", "SUBMITTED", "PROCESSING", "unknown"} {
		if got := upstreamTaskState(status); got != domain.TaskPolling {
			t.Fatalf("upstreamTaskState(%q)=%q, want %q", status, got, domain.TaskPolling)
		}
	}
}

func TestParseSize(t *testing.T) {
	w, h, err := parseSize("1024x768")
	if err != nil || w != 1024 || h != 768 {
		t.Fatalf("%dx%d %v", w, h, err)
	}
	if _, _, err := parseSize("99999x1"); err == nil {
		t.Fatal("expected invalid size")
	}
}

func TestAdobeGPTImageModelSpecificOmitsSizeForReferences(t *testing.T) {
	withoutReferences := adobeGPTImageModelSpecific(2048, 2048, false)
	if withoutReferences["size"] != "2048x2048" {
		t.Fatalf("text-to-image model payload = %#v", withoutReferences)
	}
	withReferences := adobeGPTImageModelSpecific(2048, 2048, true)
	if len(withReferences) != 0 {
		t.Fatalf("reference-image model payload = %#v, want empty object", withReferences)
	}
}

func TestAdobeImageSubmitRequestsUseCurrentModels(t *testing.T) {
	nano, err := adobeImageSubmitRequest(domain.ImageRequest{Model: adobe.AdobeNanoBanana2, Prompt: "fixture"}, 5504, 3072, []adobe.Reference{{ID: "ref-1", Usage: "general"}})
	if err != nil || nano.ModelID != "gemini-flash" || nano.ModelVersion != "nano-banana-3" || nano.GroundSearch == nil || *nano.GroundSearch || nano.SkipCAI == nil || *nano.SkipCAI {
		t.Fatalf("unexpected Nano Banana request: %+v err=%v", nano, err)
	}
	if nano.ModelSpecific["aspectRatio"] != "16:9" || nano.GenerationMetadata["module"] != "image2image" {
		t.Fatalf("unexpected Nano Banana payload metadata: %+v", nano)
	}
}

func TestAdobeVideoSubmitRequestsUseCurrentModels(t *testing.T) {
	tests := []struct {
		model   string
		modelID string
		version string
	}{
		{adobe.AdobeVeo31, "veo", "3.1-generate"},
		{adobe.AdobeVeo31Fast, "veo", "3.1-fast-generate"},
		{adobe.AdobeSeedance20, "seedance", "seedance_2.0"},
		{adobe.AdobeSeedance20Fast, "seedance", "seedance_2.0_fast"},
		{adobe.AdobeKling30Omni, "kling", "kling_v3_omni"},
	}
	for _, test := range tests {
		request, err := adobeVideoSubmitRequest(domain.VideoRequest{Model: test.model, Prompt: "fixture", Duration: 8}, 1280, 720, nil)
		if err != nil || request.ModelID != test.modelID || request.ModelVersion != test.version || request.GenerateAudio == nil || *request.GenerateAudio {
			t.Fatalf("unexpected %s request: %+v err=%v", test.model, request, err)
		}
	}
	if ref := adobeVideoReference(adobe.AdobeVeo31, "image", "asset-1", 1); ref.Usage != "asset" {
		t.Fatalf("unexpected Veo reference: %+v", ref)
	}
	if ref := adobeVideoReference(adobe.AdobeSeedance20, "video", "video-1", 1); ref.Usage != "source" {
		t.Fatalf("unexpected Seedance reference: %+v", ref)
	}
}

func TestParseVideoSize(t *testing.T) {
	tests := []struct {
		size       string
		resolution string
		width      int
		height     int
	}{
		{"1280x720", "480p", 864, 496},
		{"720x1280", "480p", 496, 864},
		{"1280x720", "720p", 1280, 720},
		{"720x1280", "1080p", 1080, 1920},
		{"1280x720", "2160p", 3840, 2160},
	}
	for _, test := range tests {
		width, height, err := parseVideoSize(test.size, test.resolution)
		if err != nil || width != test.width || height != test.height {
			t.Fatalf("parseVideoSize(%q, %q) = %dx%d, %v", test.size, test.resolution, width, height, err)
		}
	}
	if _, _, err := parseVideoSize("1x1", "720p"); err == nil {
		t.Fatal("expected invalid size")
	}
	if _, _, err := parseVideoSize("1280x720", "999p"); err == nil {
		t.Fatal("expected invalid resolution")
	}
}

func TestResolveMiniMaxH3Dimensions(t *testing.T) {
	spec, ok := videospec.Get("minimax-h3")
	if !ok {
		t.Fatal("missing MiniMax H3 spec")
	}
	width, height, err := resolveVideoDimensions(spec, "3360x1440", "1440p")
	if err != nil || width != 3360 || height != 1440 {
		t.Fatalf("resolveVideoDimensions = %dx%d, %v", width, height, err)
	}
	if _, _, err := resolveVideoDimensions(spec, "1280x720", "1440p"); err == nil {
		t.Fatal("MiniMax H3 accepted a non-schema size")
	}
}

func TestResolveGrokImagine15DimensionsAndTier(t *testing.T) {
	spec, ok := videospec.Get("grok-imagine-1.5")
	if !ok {
		t.Fatal("missing Grok Imagine 1.5 spec")
	}
	width, height, err := resolveVideoDimensions(spec, "1888x1072", "1080p")
	if err != nil || width != 1888 || height != 1072 {
		t.Fatalf("resolveVideoDimensions = %dx%d, %v", width, height, err)
	}
	if _, _, err := resolveVideoDimensions(spec, "1888x1072", "480p"); err == nil {
		t.Fatal("Grok Imagine 1.5 accepted a mismatched size price tier")
	}
}

func TestWorkerQueuesIncludeAudio(t *testing.T) {
	queues := WorkerQueues()
	if queues["images"] < 1 || queues["videos"] < 1 || queues["audio"] < 1 {
		t.Fatalf("unexpected worker queues: %#v", queues)
	}
}

func TestPollIntervalBacksOffAndJitters(t *testing.T) {
	worker := Worker{Config: config.Config{PollInterval: 3 * time.Second, TaskTimeout: 30 * time.Minute}}
	id := uuid.UUID{49}
	freshDeadline := time.Now().Add(30 * time.Minute)
	agedDeadline := time.Now().Add(24 * time.Minute)
	fresh := worker.pollInterval(id, &freshDeadline)
	aged := worker.pollInterval(id, &agedDeadline)
	if fresh <= 3*time.Second || fresh >= 5*time.Second {
		t.Fatalf("fresh interval=%v", fresh)
	}
	if aged < 10*time.Second || aged <= fresh {
		t.Fatalf("aged interval=%v fresh=%v", aged, fresh)
	}
}

func TestQueueForKind(t *testing.T) {
	tests := []struct {
		kind     string
		taskType string
		queue    string
	}{
		{kind: "image", taskType: TypeImage, queue: "images"},
		{kind: "video", taskType: TypeVideo, queue: "videos"},
		{kind: "audio", taskType: TypeAudio, queue: "audio"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			taskType, queue, err := queueForKind(tt.kind)
			if err != nil {
				t.Fatal(err)
			}
			if taskType != tt.taskType || queue != tt.queue {
				t.Fatalf("queueForKind(%q) = (%q, %q), want (%q, %q)", tt.kind, taskType, queue, tt.taskType, tt.queue)
			}
		})
	}

	if _, _, err := queueForKind("document"); err == nil {
		t.Fatal("queueForKind accepted an unsupported task kind")
	}
}

func TestSubmissionUncertain(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "explicit bad request", err: &leonardo.HTTPError{Status: http.StatusBadRequest}, want: false},
		{name: "rate limited", err: &leonardo.HTTPError{Status: http.StatusTooManyRequests}, want: false},
		{name: "request timeout", err: &leonardo.HTTPError{Status: http.StatusRequestTimeout}, want: true},
		{name: "upstream server error", err: &leonardo.HTTPError{Status: http.StatusBadGateway}, want: true},
		{name: "graphql mutation error", err: &leonardo.GraphQLError{Operation: "Generate", Message: "internal"}, want: true},
		{name: "graphql explicit capacity rejection", err: &leonardo.GraphQLError{Operation: "Generate", Message: "pending limit", Extensions: map[string]any{"code": "RATE_LIMIT_EXCEEDED", "statusCode": float64(429)}}, want: false},
		{name: "response decode error", err: errors.New("invalid response body"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := submissionUncertain(tt.err); got != tt.want {
				t.Fatalf("submissionUncertain(%v)=%v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestNormalizeUpstreamReportedCost(t *testing.T) {
	valid := 45.5
	if got := normalizeUpstreamReportedCost(&valid); got == nil || *got != valid {
		t.Fatalf("valid cost was not retained: %v", got)
	}
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		value := invalid
		if got := normalizeUpstreamReportedCost(&value); got != nil {
			t.Fatalf("invalid cost %v was retained", invalid)
		}
	}
	if normalizeUpstreamReportedCost(nil) != nil {
		t.Fatal("nil upstream cost must remain nil")
	}
}
