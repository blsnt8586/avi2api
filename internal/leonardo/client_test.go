package leonardo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGraphQLErrorIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":null,"errors":[{"message":"mutation outcome unknown","extensions":{"code":"INTERNAL"}}]}`)
	}))
	defer srv.Close()
	c, _ := New("", "ua", "1.0")
	c.GraphQLURL = srv.URL
	_, err := c.Generate(context.Background(), "at", "", GenerateRequest{Model: "auto", Prompt: "p", Width: 64, Height: 64, Quantity: 1})
	var gqlErr *GraphQLError
	if !errors.As(err, &gqlErr) || gqlErr.Operation != "Generate" || gqlErr.Extensions["code"] != "INTERNAL" {
		t.Fatalf("expected typed GraphQLError, got %T: %v", err, err)
	}
}

func TestGetUserDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req["operationName"] != "GetUserDetails" {
			t.Fatalf("unexpected user details request: %+v", req)
		}
		variables, ok := req["variables"].(map[string]any)
		if !ok || variables["userSub"] != "sub" {
			t.Fatalf("unexpected user details variables: %+v", req["variables"])
		}
		_, _ = io.WriteString(w, "{\"data\":{\"users\":[{\"id\":\"user-1\",\"blocked\":false,\"suspensionStatus\":null,\"user_details\":[{\"id\":\"details-1\"}]}]}}")
	}))
	defer srv.Close()
	client, _ := New("", "ua", "1.280.1")
	client.GraphQLURL = srv.URL
	details, err := client.GetUserDetails(context.Background(), "token", "team", "sub")
	if err != nil || details.ID != "user-1" || details.Blocked || details.SuspensionStatus != "" {
		t.Fatalf("details=%+v err=%v", details, err)
	}
}

func TestGetUserDetailsBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{\"data\":{\"users\":[{\"id\":\"user-1\",\"blocked\":true,\"suspensionStatus\":\"SUSPENDED\",\"user_details\":[{\"id\":\"details-1\"}]}]}}")
	}))
	defer srv.Close()
	client, _ := New("", "ua", "1.280.1")
	client.GraphQLURL = srv.URL
	details, err := client.GetUserDetails(context.Background(), "token", "team", "sub")
	if err != nil || !details.Blocked || details.SuspensionStatus != "SUSPENDED" {
		t.Fatalf("details=%+v err=%v", details, err)
	}
}

func TestSessionAndGenerate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=x" {
			t.Error("cookie missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session":{"accessToken":"at","accessTokenIssuedAt":100,"accessTokenExpiry":4102444800,"hasuraUserId":"u","cognitoSub":"s"},"user":{"email":"a@example.com"}}`))
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			t.Error("auth missing")
		}
		_, _ = w.Write([]byte(`{"data":{"generate":{"generationId":"g1","apiCreditCost":null}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New("", "ua", "1.0")
	c.SessionURL = srv.URL + "/session"
	c.GraphQLURL = srv.URL + "/graphql"
	s, email, err := c.GetSession(context.Background(), "session=x")
	if err != nil || s.AccessToken != "at" || email != "a@example.com" {
		t.Fatalf("%+v %s %v", s, email, err)
	}
	g, err := c.Generate(context.Background(), "at", "", GenerateRequest{Model: "auto", Prompt: "p", Width: 64, Height: 64, Quantity: 1})
	if err != nil || g.GenerationID != "g1" {
		t.Fatalf("%+v %v", g, err)
	}
}

func TestSubmitGenerationAcceptsStringCostAmount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"generate":{"generationId":"g-string-cost","apiCreditCost":8,"cost":{"amount":"8","unit":"TOKENS"}}}}`)
	}))
	defer srv.Close()

	client, _ := New("", "ua", "1.280.1")
	client.GraphQLURL = srv.URL
	response, err := client.SubmitGeneration(context.Background(), "token", "", CreateGenerationRequest{
		Model: "gpt-image-2", Public: false, Parameters: map[string]any{"prompt": "p"},
	})
	if err != nil || response.GenerationID != "g-string-cost" || response.Cost == nil || response.Cost.Amount != 8 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestGenerateNormalizesQuality(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				Request struct {
					Model      string         `json:"model"`
					Parameters map[string]any `json:"parameters"`
				} `json:"request"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Variables.Request.Model != "gpt-image-2" || req.Variables.Request.Parameters["quality"] != "LOW" {
			t.Fatalf("unexpected request: %+v", req.Variables.Request)
		}
		_, _ = io.WriteString(w, `{"data":{"generate":{"generationId":"g1"}}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New("", "ua", "1.0")
	c.GraphQLURL = srv.URL + "/graphql"
	if _, err := c.Generate(context.Background(), "at", "", GenerateRequest{Model: "gpt-image-2", Prompt: "p", Width: 1024, Height: 1024, Quantity: 1, Quality: "low"}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildImageGenerationRequestIncludesOfficialPromptEnhance(t *testing.T) {
	request := BuildImageGenerationRequest(GenerateRequest{
		Model: "gpt-image-2", Prompt: "p", Width: 1024, Height: 1024, Quantity: 1,
		Quality: "medium", PromptEnhance: "auto",
		StyleIDs: []string{"111dc692-d470-4eec-b791-3475abac4c46"},
	})
	if request.Public {
		t.Fatal("gateway generations must remain private")
	}
	if request.Parameters["quality"] != "MEDIUM" || request.Parameters["prompt_enhance"] != "AUTO" {
		t.Fatalf("unexpected normalized parameters: %+v", request.Parameters)
	}
	styles, ok := request.Parameters["style_ids"].([]string)
	if !ok || len(styles) != 1 || styles[0] != "111dc692-d470-4eec-b791-3475abac4c46" {
		t.Fatalf("unexpected style ids: %#v", request.Parameters["style_ids"])
	}
}

func TestSubmitGenerationUsesPreparedRequest(t *testing.T) {
	prepared := BuildImageGenerationRequest(GenerateRequest{
		Model: "gpt-image-2", Prompt: "prepared", Width: 1536, Height: 1024,
		Quantity: 1, Quality: "high", ImageReferences: []ImageReference{{ID: "init-1", Type: "UPLOADED", Strength: "LOW"}},
	})
	want, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Variables struct {
				Request json.RawMessage `json:"request"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if string(request.Variables.Request) != string(want) {
			t.Fatalf("submitted request=%s, want %s", request.Variables.Request, want)
		}
		_, _ = io.WriteString(w, `{"data":{"generate":{"generationId":"prepared-1"}}}`)
	}))
	defer srv.Close()
	client, _ := New("", "ua", "1.0")
	client.GraphQLURL = srv.URL
	response, err := client.SubmitGeneration(context.Background(), "token", "", prepared)
	if err != nil || response.GenerationID != "prepared-1" {
		t.Fatalf("SubmitGeneration response=%+v err=%v", response, err)
	}
}

func TestFailureDetails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string `json:"operationName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.OperationName != "GetGenerationFailure" {
			t.Fatalf("unexpected operation %s", req.OperationName)
		}
		_, _ = io.WriteString(w, `{"data":{"generations_by_pk":{"id":"g1","status":"FAILED","nsfw":false,"prompt_moderations":[{"moderationClassification":[]}],"notes":[{"noteType":"PROVIDER_FAILURE","notePayload":null,"failureReason":{"errorCode":"PROVIDER_MODERATION_ERROR"}}]}}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New("", "ua", "1.232.1")
	c.GraphQLURL = srv.URL + "/graphql"
	failure, err := c.FailureDetails(context.Background(), "at", "", "g1")
	if err != nil {
		t.Fatal(err)
	}
	if failure.ID != "g1" || failure.Status != "FAILED" || failure.ProviderErrorCode() != "PROVIDER_MODERATION_ERROR" || len(failure.Notes) != 1 {
		t.Fatalf("unexpected failure details: %+v", failure)
	}
}

func TestResultIncludesModerationMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string `json:"operationName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.OperationName != "GetGenerationResult" {
			t.Fatalf("unexpected operation %s", req.OperationName)
		}
		_, _ = io.WriteString(w, `{"data":{"generations_by_pk":{"id":"g1","status":"COMPLETE","nsfw":true,"prompt_moderations":[{"moderationClassification":["NSFW","EXTREME_VIOLENCE"]}],"generated_images":[{"id":"video-1","url":"","motionMP4URL":"https://cdn.example.test/video.mp4","motionGIFURL":"","image_width":1280,"image_height":720,"nsfw":true,"generated_image_moderation":{"generatedImageId":"video-1","moderationClassification":["NSFW","EXPLICIT"]}}]}}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New("", "ua", "1.232.1")
	c.GraphQLURL = srv.URL + "/graphql"
	result, err := c.Result(context.Background(), "at", "", "g1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.NSFW || len(result.Outputs) != 1 || !result.Outputs[0].NSFW {
		t.Fatalf("unexpected result: %+v", result)
	}
	want := []string{"NSFW", "EXTREME_VIOLENCE", "EXPLICIT"}
	if !reflect.DeepEqual(result.ModerationClassifications, want) || !reflect.DeepEqual(result.Outputs[0].ModerationClassifications, want) {
		t.Fatalf("moderation classifications result=%v output=%v want=%v", result.ModerationClassifications, result.Outputs[0].ModerationClassifications, want)
	}
}

func TestPromptModerationListAcceptsObjectArrayAndNull(t *testing.T) {
	tests := []struct {
		name string
		json string
		want int
	}{
		{name: "object", json: `{"moderationClassification":["NSFW"]}`, want: 1},
		{name: "array", json: `[{"moderationClassification":["NSFW"]}]`, want: 1},
		{name: "null", json: `null`, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var list PromptModerationList
			if err := json.Unmarshal([]byte(tc.json), &list); err != nil {
				t.Fatal(err)
			}
			if len(list) != tc.want {
				t.Fatalf("got %d moderation entries, want %d", len(list), tc.want)
			}
		})
	}
}

func TestGenerateIncludesImageReferences(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				Request struct {
					Parameters struct {
						Guidances struct {
							ImageReference []struct {
								Image struct {
									ID   string `json:"id"`
									Type string `json:"type"`
								} `json:"image"`
								Strength string `json:"strength"`
							} `json:"image_reference"`
						} `json:"guidances"`
					} `json:"parameters"`
				} `json:"request"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		refs := req.Variables.Request.Parameters.Guidances.ImageReference
		if len(refs) != 2 || refs[0].Image.ID != "init-1" || refs[0].Image.Type != "UPLOADED" || refs[1].Strength != "HIGH" {
			t.Fatalf("unexpected references: %+v", refs)
		}
		_, _ = io.WriteString(w, `{"data":{"generate":{"generationId":"g1"}}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New("", "ua", "1.0")
	c.GraphQLURL = srv.URL + "/graphql"
	refs := []ImageReference{{ID: "init-1", Type: "UPLOADED", Strength: "HIGH"}, {ID: "init-2", Type: "UPLOADED", Strength: "HIGH"}}
	if _, err := c.Generate(context.Background(), "at", "", GenerateRequest{Model: "gpt-image-2", Prompt: "p", Width: 1024, Height: 1024, Quantity: 1, ImageReferences: refs}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateVideo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				Request struct {
					Model      string         `json:"model"`
					Parameters map[string]any `json:"parameters"`
				} `json:"request"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Variables.Request.Model != "seedance-2.0-fast" || req.Variables.Request.Parameters["mode"] != "RESOLUTION_720" || req.Variables.Request.Parameters["duration"] != float64(6) {
			t.Fatalf("unexpected video request: %+v", req.Variables.Request)
		}
		guidances, ok := req.Variables.Request.Parameters["guidances"].(map[string]any)
		if !ok {
			t.Fatalf("missing video reference guidances: %+v", req.Variables.Request.Parameters)
		}
		references, ok := guidances["image_reference"].([]any)
		if !ok || len(references) != 1 {
			t.Fatalf("unexpected video reference guidances: %+v", guidances)
		}
		if frames, ok := guidances["start_frame"].([]any); !ok || len(frames) != 1 {
			t.Fatalf("missing start frame: %+v", guidances)
		}
		if frames, ok := guidances["end_frame"].([]any); !ok || len(frames) != 1 {
			t.Fatalf("missing end frame: %+v", guidances)
		}
		if videos, ok := guidances["video_reference_base"].([]any); !ok || len(videos) != 1 {
			t.Fatalf("missing video reference: %+v", guidances)
		}
		if audio, ok := guidances["audio_reference"].([]any); !ok || len(audio) != 2 {
			t.Fatalf("missing audio reference: %+v", guidances)
		}
		if req.Variables.Request.Parameters["motion_has_audio"] != false {
			t.Fatalf("unexpected native audio flag: %+v", req.Variables.Request.Parameters)
		}
		_, _ = io.WriteString(w, `{"data":{"generate":{"generationId":"video-1","apiCreditCost":40}}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New("", "ua", "1.0")
	c.GraphQLURL = srv.URL + "/graphql"
	generateAudio := false
	got, err := c.GenerateVideo(context.Background(), "at", "", GenerateVideoRequest{
		Model: "seedance-2.0-fast", Prompt: "p", Width: 1280, Height: 720, Duration: 6,
		ResolutionMode: "RESOLUTION_720", Public: true,
		ImageReferences: []ImageReference{{ID: "init-video-1", Type: "UPLOADED", Strength: "HIGH"}},
		StartFrame:      &ImageReference{ID: "start-1", Type: "UPLOADED"},
		EndFrame:        &ImageReference{ID: "end-1", Type: "UPLOADED"},
		VideoReferences: []MediaReference{{ID: "video-ref-1", Type: "UPLOADED", Duration: 4.5, Width: 1280, Height: 720, MotionHasAudio: true}},
		AudioReference:  &MediaReference{ID: "audio-ref-1", Type: "UPLOADED", Duration: 3.2},
		AudioReferences: []MediaReference{{ID: "audio-ref-2", Type: "UPLOADED", Duration: 2.8}},
		GenerateAudio:   &generateAudio,
	})
	if err != nil || got.GenerationID != "video-1" {
		t.Fatalf("unexpected result: %+v %v", got, err)
	}
}

func TestBuildGrokImagine15VideoRequest(t *testing.T) {
	generateAudio := false
	request := BuildVideoGenerationRequest(GenerateVideoRequest{
		Model: "grok-imagine-1.5", Prompt: "animate the still", Width: 1888, Height: 1072,
		Duration: 6, StartFrame: &ImageReference{ID: "start-1", Type: "UPLOADED"},
		GenerateAudio: &generateAudio,
	})
	if request.Model != "grok-imagine-1.5" || request.Parameters["width"] != 1888 || request.Parameters["height"] != 1072 || request.Parameters["duration"] != 6 {
		t.Fatalf("unexpected Grok request: %+v", request)
	}
	if _, ok := request.Parameters["mode"]; ok {
		t.Fatalf("Grok upstream schema does not accept mode: %+v", request.Parameters)
	}
	guidances, ok := request.Parameters["guidances"].(map[string]any)
	if !ok || len(guidances) != 1 {
		t.Fatalf("unexpected Grok guidances: %+v", request.Parameters["guidances"])
	}
	if frames, ok := guidances["start_frame"].([]map[string]any); !ok || len(frames) != 1 {
		t.Fatalf("missing Grok start frame: %+v", guidances)
	}
	if request.Parameters["motion_has_audio"] != false {
		t.Fatalf("unexpected Grok audio flag: %+v", request.Parameters)
	}
}

func TestBuildFlux3VideoRequest(t *testing.T) {
	generateAudio := false
	request := BuildVideoGenerationRequest(GenerateVideoRequest{
		Model: "bfl/flux-3-video", Prompt: "continue the scene", Width: 2520, Height: 1080,
		Duration: 20, Resolution: "1080p",
		VideoReferences: []MediaReference{{ID: "video-1", Type: "UPLOADED", Duration: 15.05, Width: 1920, Height: 1080}},
		GenerateAudio:   &generateAudio,
	})
	if request.Model != "bfl/flux-3-video" || request.Parameters["width"] != 2520 || request.Parameters["height"] != 1080 || request.Parameters["duration"] != 20 || request.Parameters["resolution"] != "1080p" {
		t.Fatalf("unexpected FLUX 3 Video request: %+v", request)
	}
	if _, ok := request.Parameters["mode"]; ok {
		t.Fatalf("FLUX 3 Video upstream schema does not accept mode: %+v", request.Parameters)
	}
	guidances, ok := request.Parameters["guidances"].(map[string]any)
	if !ok || len(guidances) != 1 || len(guidances["video_reference_base"].([]map[string]any)) != 1 {
		t.Fatalf("unexpected FLUX 3 Video guidances: %+v", request.Parameters["guidances"])
	}
	if request.Parameters["motion_has_audio"] != false {
		t.Fatalf("unexpected FLUX 3 Video audio flag: %+v", request.Parameters)
	}
}

func TestBuildKlingO3OmniVideoReferenceRequest(t *testing.T) {
	generateAudio := true
	request := BuildVideoGenerationRequest(GenerateVideoRequest{
		Model: "kling-video-o-3", Prompt: "keep the subject consistent", Width: 0, Height: 0,
		ImageReferences: []ImageReference{{ID: "image-1", Type: "UPLOADED", Strength: "HIGH"}},
		VideoReferences: []MediaReference{{ID: "video-1", Type: "UPLOADED", Duration: 5, Width: 1920, Height: 1080, MotionHasAudio: true}},
		GenerateAudio:   &generateAudio,
	})
	if request.Model != "kling-video-o-3" || request.Parameters["width"] != 0 || request.Parameters["height"] != 0 {
		t.Fatalf("unexpected Kling O3 Omni request: %+v", request)
	}
	if _, ok := request.Parameters["duration"]; ok {
		t.Fatalf("Kling O3 Omni video references must omit output duration: %+v", request.Parameters)
	}
	if _, ok := request.Parameters["mode"]; ok {
		t.Fatalf("Kling O3 Omni uses canonical dimensions instead of mode: %+v", request.Parameters)
	}
	guidances, ok := request.Parameters["guidances"].(map[string]any)
	if !ok || len(guidances["image_reference"].([]map[string]any)) != 1 || len(guidances["video_reference_base"].([]map[string]any)) != 1 {
		t.Fatalf("unexpected Kling O3 Omni guidances: %+v", request.Parameters["guidances"])
	}
}

func TestStatusUsesUnfilteredPrimaryKeyLookup(t *testing.T) {
	for _, status := range []string{"PENDING", "COMPLETE", "FAILED"} {
		t.Run(status, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					OperationName string `json:"operationName"`
					Query         string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatal(err)
				}
				if req.OperationName != "GetAIGenerationStatus" || strings.Contains(req.Query, "_in") {
					t.Fatalf("unexpected status query: %+v", req)
				}
				io.WriteString(w, `{"data":{"generations_by_pk":{"id":"generation-1","status":"`+status+`"}}}`)
			}))
			defer api.Close()
			client, _ := New("", "ua", "1.232.1")
			client.GraphQLURL = api.URL
			got, err := client.Status(context.Background(), "token", "", "generation-1")
			if err != nil || got != status {
				t.Fatalf("Status=%q err=%v", got, err)
			}
		})
	}
}

func TestStatusRejectsMissingOrUnknownGeneration(t *testing.T) {
	for name, response := range map[string]string{
		"missing": `{"data":{"generations_by_pk":null}}`,
		"unknown": `{"data":{"generations_by_pk":{"id":"generation-1","status":"QUEUED"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				io.WriteString(w, response)
			}))
			defer api.Close()
			client, _ := New("", "ua", "1.232.1")
			client.GraphQLURL = api.URL
			if _, err := client.Status(context.Background(), "token", "", "generation-1"); err == nil {
				t.Fatal("expected status error")
			}
		})
	}
}

func TestUploadMedia(t *testing.T) {
	var api *httptest.Server
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		switch req.OperationName {
		case "UploadImage":
			input := req.Variables["uploadImageInput"].(map[string]any)
			if input["extension"] != "mp4" || input["originalFilename"] != "reference.mp4" {
				t.Fatalf("unexpected upload input: %+v", input)
			}
			io.WriteString(w, `{"data":{"uploadImage":{"uploadId":"media-1","url":"`+api.URL+`/s3","fields":"{\"key\":\"media/reference.mp4\"}"}}}`)
		case "GetUploadedMediaById":
			polls++
			status := "PROCESSING"
			if polls > 1 {
				status = "COMPLETE"
			}
			io.WriteString(w, `{"data":{"uploaded_media":[{"id":"media-1","url":"https://cdn.example/reference.mp4","width":1280,"height":720,"duration":4.5,"status":"`+status+`","statusReason":""}]}}`)
		default:
			t.Fatalf("unexpected operation %s", req.OperationName)
		}
	})
	mux.HandleFunc("/s3", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if header.Filename != "reference.mp4" || string(body) != "video-data" {
			t.Fatalf("unexpected upload %s %q", header.Filename, body)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	api = httptest.NewServer(mux)
	defer api.Close()
	c, _ := New("", "ua", "1.232.1")
	c.GraphQLURL = api.URL + "/graphql"
	c.UploadPollInterval = time.Millisecond
	c.UploadTimeout = time.Second
	got, err := c.UploadMedia(context.Background(), "at", "", "reference.mp4", ".mp4", strings.NewReader("video-data"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "media-1" || got.Duration != 4.5 || got.Width != 1280 || got.Height != 720 {
		t.Fatalf("unexpected media: %+v", got)
	}
}

func TestGenerateAudio(t *testing.T) {
	cases := []struct {
		name     string
		request  GenerateAudioRequest
		validate func(t *testing.T, params map[string]any)
	}{
		{
			name:    "dialogue",
			request: GenerateAudioRequest{Model: "dialogue-v3", Prompt: "hello", Quantity: 2, VoiceID: "voice-1", LanguageCode: "en", PromptInfluence: float64ptr(0.5)},
			validate: func(t *testing.T, params map[string]any) {
				t.Helper()
				if params["voice_id"] != "voice-1" || params["language_code"] != "en" || params["prompt_influence"] != float64(0.5) {
					t.Fatalf("unexpected dialogue parameters: %+v", params)
				}
			},
		},
		{
			name:    "music",
			request: GenerateAudioRequest{Model: "music-v1", Prompt: "piano", Quantity: 1, DurationMinutes: 3, ForceInstrumental: true},
			validate: func(t *testing.T, params map[string]any) {
				t.Helper()
				if params["duration_minutes"] != float64(3) || params["force_instrumental"] != true {
					t.Fatalf("unexpected music parameters: %+v", params)
				}
			},
		},
		{
			name:    "sound effect",
			request: GenerateAudioRequest{Model: "sound-effects-v2", Prompt: "rain", Quantity: 1, Duration: 6, Loop: true, PromptInfluence: float64ptr(0.7)},
			validate: func(t *testing.T, params map[string]any) {
				t.Helper()
				if params["duration"] != float64(6) || params["loop"] != true || params["prompt_influence"] != float64(0.7) {
					t.Fatalf("unexpected sound effect parameters: %+v", params)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Variables struct {
						Request struct {
							Model      string         `json:"model"`
							Parameters map[string]any `json:"parameters"`
						} `json:"request"`
					} `json:"variables"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request.Variables.Request.Model != tc.request.Model || request.Variables.Request.Parameters["prompt"] != tc.request.Prompt || request.Variables.Request.Parameters["quantity"] != float64(tc.request.Quantity) {
					t.Fatalf("unexpected audio request: %+v", request.Variables.Request)
				}
				tc.validate(t, request.Variables.Request.Parameters)
				_, _ = io.WriteString(w, `{"data":{"generate":{"generationId":"audio-1"}}}`)
			}))
			defer srv.Close()
			client, _ := New("", "ua", "1.0")
			client.GraphQLURL = srv.URL
			result, err := client.GenerateAudio(context.Background(), "token", "", tc.request)
			if err != nil || result.GenerationID != "audio-1" {
				t.Fatalf("unexpected result: %+v %v", result, err)
			}
		})
	}
}

func TestAudioResultExtractsAudioURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"generations_by_pk":{"id":"audio-1","status":"COMPLETE","generated_images":[{"id":"file-1","url":"","urls":{"asset":"https://cdn.example.test/output.mp3"}}],"generations_metadata":{"metadata":{"audio":{"url":"https://cdn.example.test/metadata.wav"},"preview":"https://cdn.example.test/not-audio.png"}}}}}`)
	}))
	defer srv.Close()
	client, _ := New("", "ua", "1.0")
	client.GraphQLURL = srv.URL
	outputs, err := client.AudioResult(context.Background(), "token", "", "audio-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 2 || outputs[0].ID != "file-1" || outputs[0].URL != "https://cdn.example.test/output.mp3" || outputs[1].URL != "https://cdn.example.test/metadata.wav" {
		t.Fatalf("unexpected audio outputs: %+v", outputs)
	}
}

func float64ptr(value float64) *float64 { return &value }

func TestListPlatformImageModels(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"release":{"schemaReferences":[{"schemaId":"https://leonardo.ai/platform/requests/generate/models/gpt_image_2_0","schemaData":{"properties":{"model":{"const":"gpt-image-2","ui:metadata":{"order":3,"description":"image model","badge":{"alt":"OpenAI logo"}},"leo:model_config":{"id":"m1","name":"GPT Image 2","type":"image","capabilities":{"generate":true,"production_api_availability":true}},"leo:cost_config":{"tokens":{"type":"per_megapixel","amount":7}}},"parameters":{"properties":{"quality":{"enum":["LOW","MEDIUM"],"default":"MEDIUM"},"quantity":{"default":1,"maximum":8},"resolution":{"enum":["720p"]}}}}}},{"schemaId":"https://leonardo.ai/platform/requests/generate/models/upscaler","schemaData":{"properties":{"model":{"const":"upscaler","leo:model_config":{"type":"image","capabilities":{"generate":false}}}}}}]}}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New("", "ua", "1.0")
	c.GraphQLURL = srv.URL + "/graphql"
	models, err := c.ListPlatformImageModels(context.Background(), "at", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-image-2" || models[0].Provider != "OpenAI" || models[0].BaseTokenCost != 7 || len(models[0].ResolutionModes) != 1 || models[0].ResolutionModes[0] != "720p" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestListPlatformVideoModelsUsesPublicSchemaRegistry(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body.Query, "publicJsonSchemaRegistry") {
			t.Fatalf("missing public schema registry query: %s", body.Query)
		}
		_, _ = io.WriteString(w, `{"data":{"publicJsonSchemaRegistry":{"release":{"schemaReferences":[{"schemaId":"https://leonardo.ai/platform/requests/generate/models/bytedance/seedance-2.5","schemaData":{"ui:properties":{"dimensions":{"ui:options":{"16:9":{"sizes":{"RESOLUTION_480":{},"RESOLUTION_720":{}}}}}},"properties":{"model":{"const":"bytedance/seedance-2.5","ui:metadata":{"order":839,"description":"video model","badge":{"alt":"Seedance logo"}},"leo:model_config":{"id":"m25","name":"Seedance 2.5","type":"video","capabilities":{"generate":true,"production_api_availability":true}},"leo:cost_config":{"tokens":{"type":"fixed","amount":180}}},"parameters":{"properties":{"duration":{"enum":[4,30],"default":8},"quantity":{"default":1,"maximum":1}}}}}}]}}}}`)
	}))
	defer srv.Close()
	c, _ := New("", "ua", "1.258.0")
	c.GraphQLURL = srv.URL
	models, err := c.ListPlatformVideoModels(context.Background(), "at", "")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(models) != 1 || models[0].ID != "bytedance/seedance-2.5" || !reflect.DeepEqual(models[0].ResolutionModes, []string{"RESOLUTION_480", "RESOLUTION_720"}) {
		t.Fatalf("requests=%d models=%+v", requests, models)
	}
}

func TestUploadInitImage(t *testing.T) {
	var api *httptest.Server
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		switch req.OperationName {
		case "UploadImage":
			io.WriteString(w, `{"data":{"uploadImage":{"uploadId":"upload-1","url":"`+api.URL+`/s3","fields":"{\"key\":\"init/upload-1.jpg\",\"policy\":\"p\"}"}}}`)
		case "GetInitImageModeration":
			polls++
			status := "Pending"
			initID := ""
			if polls > 1 {
				status = "Accepted"
				initID = "init-1"
			}
			io.WriteString(w, `{"data":{"init_image_moderation":[{"initImageId":"`+initID+`","checkStatus":"`+status+`","init_image":{"imageWidth":640,"imageHeight":480}}]}}`)
		default:
			t.Fatalf("unexpected operation %s", req.OperationName)
		}
	})
	mux.HandleFunc("/s3", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("key") != "init/upload-1.jpg" || r.FormValue("policy") != "p" {
			t.Fatalf("unexpected fields: %#v", r.Form)
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		if h.Filename != "source.jpg" || string(b) != "jpeg-data" {
			t.Fatalf("unexpected file %s %q", h.Filename, b)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	api = httptest.NewServer(mux)
	defer api.Close()
	c, _ := New("", "ua", "1.231.0")
	c.GraphQLURL = api.URL + "/graphql"
	c.UploadPollInterval = time.Millisecond
	c.UploadTimeout = time.Second
	got, err := c.UploadInitImage(context.Background(), "at", "", "source.jpg", "image/jpeg", []byte("jpeg-data"))
	if err != nil {
		t.Fatal(err)
	}
	if got.InitImageID != "init-1" || got.Width != 640 || got.Height != 480 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestGenerateIncludesInitImage(t *testing.T) {
	var requestBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		requestBody = string(b)
		io.WriteString(w, `{"data":{"generate":{"generationId":"g1"}}}`)
	}))
	defer srv.Close()
	c, _ := New("", "ua", "1")
	c.GraphQLURL = srv.URL
	strength := 0.65
	if _, err := c.Generate(context.Background(), "at", "", GenerateRequest{Model: "auto-preset", Prompt: "p", Width: 512, Height: 512, Quantity: 1, InitImageID: "init-1", InitStrength: &strength}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(requestBody, `"init_image_id":"init-1"`) || !strings.Contains(requestBody, `"init_strength":0.65`) {
		t.Fatalf("missing image-to-image parameters: %s", requestBody)
	}
}
