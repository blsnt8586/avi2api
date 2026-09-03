package creativefabrica

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGraphQLUsesOperationPathAndCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/query/UserToken" {
			t.Fatalf("unexpected GraphQL path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer st-fixture" {
			t.Fatalf("unexpected Authorization header %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "cfToken=st-fixture" {
			t.Fatalf("unexpected Cookie header %q", got)
		}
		if got := r.Header.Get("Origin"); got != DefaultOrigin {
			t.Fatalf("unexpected Origin %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("invalid GraphQL request: %v", err)
		}
		if request["query"] != "query UserToken { me { token } }" {
			t.Fatalf("unexpected query %v", request["query"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"me":{"token":"rpc-fixture"}}}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "fixture-agent")
	client.GraphQLURL = server.URL + "/query"
	data, err := client.GraphQL(context.Background(), "UserToken", `query UserToken { me { token } }`, nil, "st-fixture", "cfToken=st-fixture")
	if err != nil {
		t.Fatalf("GraphQL() error = %v", err)
	}
	me, ok := data["me"].(map[string]any)
	if !ok || me["token"] != "rpc-fixture" {
		t.Fatalf("unexpected GraphQL data %#v", data)
	}
}

func TestGraphQLRejectsMissingDataAndGraphQLErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{name: "missing data", body: `{"errors":[]}`},
		{name: "graphql error", body: `{"data":null,"errors":[{"message":"denied","extensions":{"code":"UNAUTHENTICATED"}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := NewWithHTTPClient(server.Client(), "fixture")
			client.GraphQLURL = server.URL + "/query"
			_, err := client.GraphQL(context.Background(), "Fixture", "query Fixture { me { id } }", nil, "", "")
			if err == nil {
				t.Fatal("expected GraphQL error")
			}
			if tc.name == "graphql error" {
				var gqlErr *GraphQLError
				if !errors.As(err, &gqlErr) {
					t.Fatalf("expected GraphQLError, got %T: %v", err, err)
				}
				if !strings.Contains(err.Error(), "denied") {
					t.Fatalf("error does not contain provider message: %v", err)
				}
			}
		})
	}
}

func TestLoginIncludesStudioRequiredDeviceFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/query/logIn" {
			t.Fatalf("unexpected login path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var request struct {
			Variables struct {
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("invalid login request: %v", err)
		}
		input := request.Variables.Input
		if input["email"] != "fixture@example.com" || input["password"] != "fixture-password" {
			t.Fatalf("unexpected login credentials %#v", input)
		}
		if input["deviceId"] != "fixture1234" {
			t.Fatalf("unexpected deviceId %#v", input["deviceId"])
		}
		if remember, ok := input["remember"].(bool); !ok || remember {
			t.Fatalf("remember should be false, got %#v", input["remember"])
		}
		_, _ = io.WriteString(w, `{"data":{"login":{"token":"st-fixture","user":{"id":"u1","email":"fixture@example.com"},"challenge":null,"errors":[]}}}`)
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "fixture")
	client.GraphQLURL = server.URL + "/query"
	client.DeviceID = "fixture1234"
	result, err := client.LoginWithOTP(context.Background(), "fixture@example.com", "fixture-password", "")
	if err != nil {
		t.Fatalf("LoginWithOTP() error = %v", err)
	}
	if result.SessionToken != "st-fixture" {
		t.Fatalf("unexpected session token %q", result.SessionToken)
	}
}

func TestExchangeSessionTokenAndRPCHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jwtauth":
			body, _ := io.ReadAll(r.Body)
			var request map[string]string
			if err := json.Unmarshal(body, &request); err != nil {
				t.Fatalf("invalid jwtauth body: %v", err)
			}
			if request["token"] != "st-fixture" || request["origin"] != DefaultOrigin {
				t.Fatalf("unexpected jwtauth request %#v", request)
			}
			_, _ = io.WriteString(w, `{"access_token":"rpc-fixture"}`)
		case "/creativefabrica.coins.v2.CoinsService/GetBalance":
			if got := r.Header.Get("Authorization"); got != "Bearer rpc-fixture" {
				t.Fatalf("unexpected RPC Authorization %q", got)
			}
			if got := r.Header.Get("connect-protocol-version"); got != "1" {
				t.Fatalf("unexpected Connect protocol version %q", got)
			}
			_, _ = io.WriteString(w, `{"balance":"1234","available":"1200","used":"34"}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "fixture")
	client.JWTAuthURL = server.URL + "/jwtauth"
	client.CoinsURL = server.URL
	client.CookieHeader = "cfToken=stored"
	set, err := client.ExchangeSessionToken(context.Background(), "st-fixture")
	if err != nil {
		t.Fatalf("ExchangeSessionToken() error = %v", err)
	}
	if set.SessionToken != "st-fixture" || set.RPCToken != "rpc-fixture" {
		t.Fatalf("unexpected token set %#v", set)
	}
	coins, err := client.Coins(context.Background(), set.RPCToken)
	if err != nil {
		t.Fatalf("Coins() error = %v", err)
	}
	if coins.Balance != 1234 || coins.Available != 1200 || coins.Used != 34 {
		t.Fatalf("unexpected coins %#v", coins)
	}
}

func TestExchangeSessionTokenWithCookieFallsBackToClientCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); got != "cfToken=stored" {
			t.Fatalf("cookie fallback = %q", got)
		}
		_, _ = io.WriteString(w, `{"access_token":"rpc-fixture"}`)
	}))
	defer server.Close()
	client := NewWithHTTPClient(server.Client(), "fixture")
	client.JWTAuthURL = server.URL + "/jwtauth"
	client.CookieHeader = "cfToken=stored"
	set, err := client.ExchangeSessionTokenWithCookie(context.Background(), "st-fixture", "")
	if err != nil {
		t.Fatalf("ExchangeSessionTokenWithCookie() error = %v", err)
	}
	if set.RPCToken != "rpc-fixture" {
		t.Fatalf("unexpected token set %#v", set)
	}
}

func TestRPCDecodesModelsAndPollOutputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/creativefabrica.flow.v2.FlowService/ListModels" {
			t.Fatalf("unexpected RPC path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("invalid ListModels request: %v", err)
		}
		if len(request) != 0 {
			t.Fatalf("image ListModels request should be empty, got %#v", request)
		}
		_, _ = io.WriteString(w, `{"models":[{"id":"gpt-image-2","displayName":"GPT Image 2","inputOptions":{"foo":"bar"},"pricingInputs":["size"]}]}`)
	}))
	defer server.Close()
	client := NewWithHTTPClient(server.Client(), "fixture")
	client.FlowURL = server.URL
	models, err := client.ListModels(context.Background(), "rpc", "image")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "gpt-image-2" || models[0].Kind != "image" {
		t.Fatalf("unexpected models %#v", models)
	}

	result := parsePollResult(map[string]any{
		"status":   "completed",
		"progress": 100,
		"media":    []any{map[string]any{"id": "asset-1", "url": "https://cdn.example/asset.png", "mediaType": "image/png", "width": 1024, "height": 768}},
	})
	if result.Status != "COMPLETE" || len(result.Outputs) != 1 || result.Outputs[0].URL == "" {
		t.Fatalf("unexpected poll result %#v", result)
	}
}

func TestVideoListModelsSendsServiceType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/creativefabrica.studiomediamatrix.v1.StudioMediaMatrixService/ListModels" {
			t.Fatalf("unexpected RPC path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("invalid video ListModels request: %v", err)
		}
		if request["serviceType"] != "SERVICE_TYPE_VIDEO_GENERATOR" {
			t.Fatalf("unexpected video service type %#v", request)
		}
		_, _ = io.WriteString(w, `{"models":[{"id":"veo_31_fast_generate_preview","displayName":"Veo 3.1 Fast"}]}`)
	}))
	defer server.Close()
	client := NewWithHTTPClient(server.Client(), "fixture")
	client.MediaMatrixURL = server.URL
	models, err := client.ListModels(context.Background(), "rpc", "video")
	if err != nil {
		t.Fatalf("ListModels(video) error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "veo_31_fast_generate_preview" || models[0].Kind != "video" {
		t.Fatalf("unexpected video models %#v", models)
	}
}

func TestListModelsParsesConnectModelOneofAndKeepsCatalogID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/creativefabrica.studiomediamatrix.v1.StudioMediaMatrixService/ListModels" {
			t.Fatalf("unexpected RPC path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"models":[{"model":{"case":"videoGeneratorModel","value":"VIDEO_GENERATOR_MODEL_VEO_31_FAST_GENERATE_PREVIEW"},"catalog":{"publicModelName":"Veo 3.1 Fast","capabilities":{"model":"veo_31_fast_generate_preview","inputOptions":{"resolution":{"stringType":{"allowedValues":["720p"]}}}}}}]}`)
	}))
	defer server.Close()
	client := NewWithHTTPClient(server.Client(), "fixture")
	client.MediaMatrixURL = server.URL
	models, err := client.ListModels(context.Background(), "rpc", "video")
	if err != nil {
		t.Fatalf("ListModels(video) error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("unexpected model count %#v", models)
	}
	model := models[0]
	if model.ID != "veo_31_fast_generate_preview" || model.PricingModelID != model.ID {
		t.Fatalf("catalog ID was not retained %#v", model)
	}
	if got := GenerationModelID(model); got != "VIDEO_GENERATOR_MODEL_VEO_31_FAST_GENERATE_PREVIEW" {
		t.Fatalf("generation enum = %q", got)
	}
	if model.Raw["modelCase"] != "videoGeneratorModel" {
		t.Fatalf("model oneof case was not retained %#v", model.Raw)
	}
}

func TestModelEntryIdentifierSupportsDirectOneof(t *testing.T) {
	model, ok := parseCreativeFabricaDirectModel(map[string]any{
		"model":        map[string]any{"case": "imageGeneratorModel", "value": "FLOW_MODEL_OPENAI_GPT_IMAGE_2"},
		"displayName":  "GPT Image 2",
		"inputOptions": map[string]any{"prompt": map[string]any{}},
	}, "image")
	if !ok || model.ID != "FLOW_MODEL_OPENAI_GPT_IMAGE_2" || GenerationModelID(model) != model.ID {
		t.Fatalf("unexpected direct oneof model %#v", model)
	}
}

func TestCreateUploadURLUsesFilenamePayloadAndUpload(t *testing.T) {
	var uploaded []byte
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/creativefabrica.flow.v2.FlowService/CreateUploadUrl":
			body, _ := io.ReadAll(r.Body)
			var request map[string]any
			if err := json.Unmarshal(body, &request); err != nil {
				t.Fatalf("invalid CreateUploadUrl request: %v", err)
			}
			if len(request) != 1 || request["uploadFile"] != "reference.png" {
				t.Fatalf("unexpected CreateUploadUrl request %#v", request)
			}
			_, _ = io.WriteString(w, `{"uploadUrl":"`+server.URL+`/signed/upload?sig=fixture","method":"PUT","headers":{"x-fixture":"ok"}}`)
		case "/signed/upload":
			if r.Header.Get("x-fixture") != "ok" {
				t.Fatalf("signed upload header was not forwarded")
			}
			uploaded, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewWithHTTPClient(server.Client(), "fixture")
	client.FlowURL = server.URL
	upload, err := client.CreateUploadURL(context.Background(), "rpc", "reference.png", "image/png", 3)
	if err != nil {
		t.Fatalf("CreateUploadURL() error = %v", err)
	}
	if upload.URL == "" || upload.Method != "PUT" || upload.Headers["x-fixture"] != "ok" {
		t.Fatalf("unexpected upload descriptor %#v", upload)
	}
	if err := client.UploadToURL(context.Background(), upload, "image/png", []byte("abc")); err != nil {
		t.Fatalf("UploadToURL() error = %v", err)
	}
	if string(uploaded) != "abc" {
		t.Fatalf("uploaded bytes = %q", uploaded)
	}
}

func TestUploadVideoFrameToURLSendsOnlyContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "image/png" {
			t.Fatalf("content type = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Fatalf("unexpected cookie header %q", got)
		}
		if got := r.Header.Get("Origin"); got != "" || r.Header.Get("Referer") != "" {
			t.Fatalf("unexpected browser headers: origin=%q referer=%q", got, r.Header.Get("Referer"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "fixture" {
			t.Fatalf("body = %q", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := NewWithHTTPClient(server.Client(), "fixture")
	client.CookieHeader = "cfToken=secret"
	if err := client.UploadVideoFrameToURL(context.Background(), server.URL+"/signed?sig=fixture", "image/png", []byte("fixture")); err != nil {
		t.Fatalf("UploadVideoFrameToURL() error = %v", err)
	}
}

func TestCalculateGenerationCostUsesPresetServicePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/creativefabrica.preset.v1.AIProviderService/CalculateModelCost" {
			t.Fatalf("unexpected cost path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("invalid cost request: %v", err)
		}
		if request["model"] != "FLOW_MODEL_OPENAI_GPT_IMAGE_2" || request["options"] == nil {
			t.Fatalf("unexpected cost request %#v", request)
		}
		_, _ = io.WriteString(w, `{"coinAmount":"17","pricing":"fixture"}`)
	}))
	defer server.Close()
	client := NewWithHTTPClient(server.Client(), "fixture")
	client.ModalityURL = server.URL
	cost, raw, err := client.CalculateGenerationCost(context.Background(), "rpc", "FLOW_MODEL_OPENAI_GPT_IMAGE_2", map[string]any{"width": 1024}, nil)
	if err != nil {
		t.Fatalf("CalculateGenerationCost() error = %v", err)
	}
	if cost != 17 || raw["pricing"] != "fixture" {
		t.Fatalf("unexpected cost result %d %#v", cost, raw)
	}
}

func TestSessionIterationAndExactPollParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/creativefabrica.studiomediamatrix.v1.StudioMediaMatrixService/GetSessionIterations" {
			t.Fatalf("unexpected session path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("invalid session request: %v", err)
		}
		if request["serviceType"] != "SERVICE_TYPE_VIDEO_GENERATOR" || request["sessionId"] != "session-1" {
			t.Fatalf("unexpected session request %#v", request)
		}
		_, _ = io.WriteString(w, `{"session":{"status":"SESSION_STATUS_COMPLETED"},"media":{"items":[{"id":"video-1","videoContent":{"generatedVideoUrl":"https://cdn.example/video.mp4","generatedPreviewUrl":"https://cdn.example/preview.jpg"}}]}}`)
	}))
	defer server.Close()
	client := NewWithHTTPClient(server.Client(), "fixture")
	client.MediaMatrixURL = server.URL
	result, err := client.GetSessionIterations(context.Background(), "rpc", "session-1")
	if err != nil {
		t.Fatalf("GetSessionIterations() error = %v", err)
	}
	if result.Status != "COMPLETE" || len(result.Outputs) != 1 || result.Outputs[0].URL != "https://cdn.example/video.mp4" || result.Outputs[0].MediaType != "video/mp4" {
		t.Fatalf("unexpected session result %#v", result)
	}
}

func TestSessionIterationsMediaItemsAreParsedAndDeduplicated(t *testing.T) {
	result := parseSessionPollResult(map[string]any{
		"iterations": []any{
			map[string]any{"mediaItems": []any{
				map[string]any{"id": "video-1", "status": "MEDIA_STATUS_COMPLETED", "videoContent": map[string]any{"generatedVideoUrl": "https://cdn.example/video.mp4"}},
			}},
		},
		"media": map[string]any{"items": []any{
			map[string]any{"id": "video-1", "status": "MEDIA_STATUS_COMPLETED", "videoContent": map[string]any{"generatedVideoUrl": "https://cdn.example/video.mp4"}},
		}},
	})
	if result.Status != "COMPLETE" || len(result.Outputs) != 1 || result.Outputs[0].URL != "https://cdn.example/video.mp4" {
		t.Fatalf("unexpected iteration result %#v", result)
	}
}

func TestSessionIterationFailureWithoutMediaIsParsed(t *testing.T) {
	result := parseSessionPollResult(map[string]any{
		"session": map[string]any{"status": "SESSION_STATUS_PENDING"},
		"iterations": []any{
			map[string]any{
				"status":       "MEDIA_STATUS_FAILED",
				"errorMessage": "provider rejected the generation",
			},
		},
	})
	if result.Status != "FAILED" || result.Error != "provider rejected the generation" {
		t.Fatalf("unexpected iteration failure result %#v", result)
	}
}

func TestRequestBuildersUseProviderEnumsAndStripSignedQueries(t *testing.T) {
	image, err := BuildImageFlowRequest("openai-gpt-image-2", "fixture", "1920x1080", 2, "https://upload/image?sig=1", []string{"https://upload/ref?sig=2"})
	if err != nil {
		t.Fatalf("BuildImageFlowRequest() error = %v", err)
	}
	settings := image["settings"].(map[string]any)
	models := settings["models"].([]any)
	if models[0].(map[string]any)["model"] != "FLOW_MODEL_OPENAI_GPT_IMAGE_2" || settings["imageUrl"] != "https://upload/image" || settings["referenceImages"].(map[string]string)["1"] != "https://upload/ref" {
		t.Fatalf("unexpected image request %#v", image)
	}
	video, err := BuildVideoSessionRequest("veo-3.1-fast", "fixture", "720x1280", "720p", 8, []VideoFrameRequest{{Type: "VIDEO_FRAME_TYPE_FIRST", FileSize: 123, FileName: "frame.png", ReferenceType: "VIDEO_FRAME_REFERENCE_TYPE_SUBJECT", Width: 720, Height: 1280}})
	if err != nil {
		t.Fatalf("BuildVideoSessionRequest() error = %v", err)
	}
	content := video["sessionRequestPromptToVideoGeneratorContent"].(map[string]any)
	if content["serviceType"] != "SERVICE_TYPE_VIDEO_GENERATOR" || content["model"] != "VIDEO_GENERATOR_MODEL_VEO_31_FAST_GENERATE_PREVIEW" || content["aspectRatio"] != "PROMPT_TO_VIDEO_GENERATOR_CONTENT_ASPECT_RATIO_9_16" {
		t.Fatalf("unexpected video content %#v", content)
	}
	frames := content["frames"].([]any)
	frame := frames[0].(map[string]any)
	if frame["fileSize"] != int64(123) || frame["fileName"] != "frame.png" || frame["width"] != 720 || frame["height"] != 1280 {
		t.Fatalf("unexpected request frame metadata: %#v", frame)
	}
	if _, present := frame["url"]; present {
		t.Fatalf("request frame unexpectedly contains response URL: %#v", frame)
	}
	if _, present := frame["inputMediaType"]; present {
		t.Fatalf("request frame unexpectedly contains response input media type: %#v", frame)
	}
}

func TestVideoFrameUploadURLsExtractsOrderedSignedURLs(t *testing.T) {
	urls, err := VideoFrameUploadURLs(map[string]any{
		"session": map[string]any{
			"promptToVideoGeneratorContent": map[string]any{
				"frames": []any{
					map[string]any{"type": "VIDEO_FRAME_TYPE_FIRST", "url": "https://upload/first?sig=1"},
					map[string]any{"type": "VIDEO_FRAME_TYPE_LAST", "url": "https://upload/last?sig=2"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("VideoFrameUploadURLs() error = %v", err)
	}
	if len(urls) != 2 || urls[0] != "https://upload/first?sig=1" || urls[1] != "https://upload/last?sig=2" {
		t.Fatalf("unexpected upload URLs %#v", urls)
	}
}

func TestVideoSessionFixtureInitiateUploadAndPoll(t *testing.T) {
	getSessionCalls := 0
	uploaded := make(map[string]string)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/creativefabrica.studiomediamatrix.v1.StudioMediaMatrixService/InitiateSession":
			_, _ = io.WriteString(w, `{"sessionId":"session-fixture","frames":[{"url":"`+server.URL+`/upload/first?sig=1"},{"url":"`+server.URL+`/upload/last?sig=2"}]}`)
		case "/upload/first", "/upload/last":
			if r.Method != http.MethodPut || r.Header.Get("Content-Type") != "image/png" {
				t.Fatalf("unexpected signed upload request method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(r.Body)
			uploaded[r.URL.Path] = string(body)
			w.WriteHeader(http.StatusOK)
		case "/creativefabrica.studiomediamatrix.v1.StudioMediaMatrixService/GetSession":
			getSessionCalls++
			body, _ := io.ReadAll(r.Body)
			var request map[string]any
			if err := json.Unmarshal(body, &request); err != nil {
				t.Fatalf("invalid GetSession request: %v", err)
			}
			if request["serviceType"] != "SERVICE_TYPE_VIDEO_GENERATOR" || request["sessionId"] != "session-fixture" {
				t.Fatalf("unexpected GetSession request %#v", request)
			}
			if getSessionCalls == 1 {
				_, _ = io.WriteString(w, `{"session":{"status":"SESSION_STATUS_PENDING"}}`)
			} else {
				_, _ = io.WriteString(w, `{"session":{"status":"SESSION_STATUS_COMPLETED"},"media":{"items":[{"id":"video-fixture","status":"MEDIA_STATUS_COMPLETED","videoContent":{"generatedVideoUrl":"https://cdn.example/video.mp4"}}]}}`)
			}
		default:
			t.Fatalf("unexpected fixture path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewWithHTTPClient(server.Client(), "fixture")
	client.MediaMatrixURL = server.URL
	job, response, err := client.InitiateSession(context.Background(), "rpc", map[string]any{"fixture": true})
	if err != nil || job.SessionID != "session-fixture" {
		t.Fatalf("InitiateSession() job=%#v err=%v", job, err)
	}
	urls, err := VideoFrameUploadURLs(response)
	if err != nil || len(urls) != 2 {
		t.Fatalf("VideoFrameUploadURLs() urls=%#v err=%v", urls, err)
	}
	if err := client.UploadVideoFrameToURL(context.Background(), urls[0], "image/png", []byte("first")); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	if err := client.UploadVideoFrameToURL(context.Background(), urls[1], "image/png", []byte("last")); err != nil {
		t.Fatalf("last upload: %v", err)
	}
	if uploaded["/upload/first"] != "first" || uploaded["/upload/last"] != "last" {
		t.Fatalf("uploaded bodies %#v", uploaded)
	}
	first, err := client.GetSession(context.Background(), "rpc", job.SessionID)
	if err != nil || first.Status != "PENDING" {
		t.Fatalf("first poll=%#v err=%v", first, err)
	}
	second, err := client.GetSession(context.Background(), "rpc", job.SessionID)
	if err != nil || second.Status != "COMPLETE" || len(second.Outputs) != 1 || second.Outputs[0].URL != "https://cdn.example/video.mp4" {
		t.Fatalf("second poll=%#v err=%v", second, err)
	}
}

func TestParseTokenInfoReadsJWTClaims(t *testing.T) {
	payload, err := json.Marshal(map[string]any{"iat": 1700000000, "exp": 1700003600})
	if err != nil {
		t.Fatal(err)
	}
	token := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	info := ParseTokenInfo(token)
	if info.Opaque || !info.CreatedAt.Equal(time.Unix(1700000000, 0)) || !info.ExpiresAt.Equal(time.Unix(1700003600, 0)) {
		t.Fatalf("unexpected token info %#v", info)
	}
}

func TestModelAliasesIncludeCatalogAndGenerationIdentifiers(t *testing.T) {
	model := Model{
		ID:             "gpt-image-2",
		PricingModelID: "gpt-image-2",
		Raw: map[string]any{
			"generationModel": "FLOW_MODEL_OPENAI_GPT_IMAGE_2",
		},
	}
	keys := ModelAliasKeys(model)
	joined := strings.Join(keys, "|")
	for _, want := range []string{"gpt-image-2", "gpt_image_2", "flow_model_openai_gpt_image_2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("aliases %q do not contain %q", joined, want)
		}
	}
}

func TestParseJobPrefersMutationIdentifiersOverNestedIDs(t *testing.T) {
	flow := parseJob(map[string]any{
		"flow": map[string]any{"id": "flow-1", "images": []any{map[string]any{"id": "image-1"}}},
		"id":   "response-1",
	})
	if flow.ID != "flow-1" || flow.FlowID != "flow-1" || flow.SessionID != "" {
		t.Fatalf("unexpected flow job %#v", flow)
	}
	session := parseJob(map[string]any{
		"session": map[string]any{"id": "session-1", "media": []any{map[string]any{"id": "media-1"}}},
		"id":      "response-2",
	})
	if session.ID != "session-1" || session.SessionID != "session-1" || session.FlowID != "" {
		t.Fatalf("unexpected session job %#v", session)
	}
	topLevel := parseJob(map[string]any{"id": "mutation-1", "nested": map[string]any{"id": "nested-1"}})
	if topLevel.ID != "mutation-1" || topLevel.PollID != "mutation-1" {
		t.Fatalf("unexpected top-level job %#v", topLevel)
	}
}
