package adobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubmitImageUsesOverrideStatusLinkAndBuildsFireflyPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image" || r.Method != http.MethodPost {
			t.Fatalf("unexpected submit request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer TOKEN" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "API_KEY" {
			t.Fatalf("x-api-key = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["modelId"] != "gemini-flash" || payload["modelVersion"] != "nano-banana-2" {
			t.Fatalf("unexpected model payload: %#v", payload)
		}
		if payload["prompt"] != "a test image" {
			t.Fatalf("prompt missing: %#v", payload)
		}
		if payload["outputResolution"] != "2K" {
			t.Fatalf("outputResolution = %#v", payload["outputResolution"])
		}
		if refs, ok := payload["referenceBlobs"].([]any); !ok || len(refs) != 1 {
			t.Fatalf("referenceBlobs = %#v", payload["referenceBlobs"])
		}
		w.Header().Set("x-override-status-link", "https://jobs.example/poll/abc")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"links":{"result":{"href":"https://wrong.example/result"}}}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.ImageSubmitURL = server.URL + "/image"
	job, err := client.SubmitImage(context.Background(), "TOKEN", SubmitRequest{
		ModelID: "gemini-flash", ModelVersion: "nano-banana-2", Prompt: "a test image",
		Width: 2048, Height: 2048, OutputResolution: "2k", References: []Reference{{ID: "asset-1", Usage: "general"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.PollURL != "https://jobs.example/poll/abc" || job.ID != "abc" {
		t.Fatalf("job = %#v", job)
	}
}

func TestSubmitVideoNormalizesFireflyEPOStatusLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if refs, ok := payload["referenceBlobs"].([]any); !ok || len(refs) != 0 {
			t.Fatalf("referenceBlobs = %#v, want empty array", payload["referenceBlobs"])
		}
		w.Header().Set("x-override-status-link", "https://firefly-epo1234.adobe.io/v2/jobs/job-42")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.VideoSubmitURL = server.URL
	job, err := client.SubmitVideo(context.Background(), "TOKEN", SubmitRequest{ModelID: "veo", ModelVersion: "3.1-fast-generate", Prompt: "test", N: 1})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "job-42" || job.PollURL != "https://bks-epo1234.adobe.io/v2/jobs/result/job-42?host=firefly-epo1234.adobe.io/" {
		t.Fatalf("job = %#v", job)
	}
}

func TestPollExtractsOutputAndProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/poll/job" {
			t.Fatalf("unexpected poll path: %s", r.URL.Path)
		}
		w.Header().Set("x-task-progress", "72")
		_, _ = io.WriteString(w, `{"status":"PROCESSING","outputs":[{"image":{"id":"img-1","presignedUrl":"https://cdn.example/img.png","width":1024,"height":768}}]}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	result, err := client.Poll(context.Background(), "TOKEN", server.URL+"/poll/job")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "PENDING" || result.Progress != 72 || len(result.Outputs) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Outputs[0].URL != "https://cdn.example/img.png" || result.Outputs[0].MediaType != "image" {
		t.Fatalf("output = %#v", result.Outputs[0])
	}
}

func TestUploadSelectsMediaEndpointAndFindsID(t *testing.T) {
	var gotType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		if string(body) != "audio bytes" {
			t.Fatalf("body = %q", body)
		}
		_, _ = io.WriteString(w, `{"assets":[{"id":"audio-1"}]}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.AudioUploadURL = server.URL + "/audio"
	id, err := client.Upload(context.Background(), "TOKEN", "audio/mpeg", []byte("audio bytes"))
	if err != nil || id != "audio-1" {
		t.Fatalf("upload = %q, %v", id, err)
	}
	if gotType != "audio/mpeg" {
		t.Fatalf("content type = %q", gotType)
	}
}

func TestHTTPErrorPreservesStatusWithoutSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"expired"}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.ImageUploadURL = server.URL
	_, err := client.Upload(context.Background(), "TOKEN", "image/png", []byte("image"))
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusUnauthorized || !strings.Contains(httpErr.Error(), "401") {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestEstimateCostAllowsZeroFairUseCredits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"features":{"firefly_3p:external:gpt_image_2":{"cost":{"numGeneration":1,"credits":0.0,"creditType":"firefly_fair_use"},"type":"premium"}}}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.CreditsCostURL = server.URL
	result, err := client.EstimateCost(context.Background(), "TOKEN", CostRequest{Features: map[string]int{"firefly_3p:external:gpt_image_2": 1}})
	if err != nil || result.Credits != 0 {
		t.Fatalf("cost=%+v err=%v", result, err)
	}
}

func TestEstimateCostAllowsFairUseResponseCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message":"Feature under fair use","error_code":"chargeable_with_fair_use"}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.CreditsCostURL = server.URL
	result, err := client.EstimateCost(context.Background(), "TOKEN", CostRequest{Features: map[string]int{"firefly_3p:external:veo_3_fast": 1}})
	if err != nil || result.Credits != 0 || result.Raw["error_code"] != "chargeable_with_fair_use" {
		t.Fatalf("cost=%+v err=%v", result, err)
	}
}

func TestEstimateCostPreservesOtherUnprocessableResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message":"invalid feature","error_code":"invalid_feature"}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.CreditsCostURL = server.URL
	_, err := client.EstimateCost(context.Background(), "TOKEN", CostRequest{Features: map[string]int{"feature": 1}})
	var upstream *HTTPError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusUnprocessableEntity {
		t.Fatalf("error=%T %v", err, err)
	}
}

func TestEstimateCostRejectsMissingCreditValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"features":{"feature":{"cost":{"creditType":"firefly_fair_use"}}}}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "API_KEY", "CREDITS_KEY", "test-agent")
	client.CreditsCostURL = server.URL
	_, err := client.EstimateCost(context.Background(), "TOKEN", CostRequest{Features: map[string]int{"feature": 1}})
	if err == nil || !strings.Contains(err.Error(), "valid non-negative credit cost") {
		t.Fatalf("error=%v", err)
	}
}
