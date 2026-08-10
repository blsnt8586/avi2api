package leonardo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	DefaultSessionURL = "https://app.leonardo.ai/api/auth/get-session"
	DefaultGraphQLURL = "https://api.leonardo.ai/v1/graphql"
)

type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("leonardo returned HTTP %d: %s", e.Status, e.Body)
}

type GraphQLError struct {
	Operation  string
	Message    string
	Extensions map[string]any
}

func (e *GraphQLError) Error() string {
	if len(e.Extensions) == 0 {
		return fmt.Sprintf("graphql %s: %s", e.Operation, e.Message)
	}
	detail, _ := json.Marshal(e.Extensions)
	return fmt.Sprintf("graphql %s: %s: %s", e.Operation, e.Message, detail)
}

type Client struct {
	HTTP               *http.Client
	SchemaVersion      string
	UserAgent          string
	SessionURL         string
	GraphQLURL         string
	UploadPollInterval time.Duration
	UploadTimeout      time.Duration
}

func New(proxyURL, userAgent, schemaVersion string) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 200
	transport.MaxIdleConnsPerHost = 50
	transport.IdleConnTimeout = 90 * time.Second
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		switch u.Scheme {
		case "http", "https":
			transport.Proxy = http.ProxyURL(u)
		case "socks5", "socks5h":
			var auth *proxy.Auth
			if u.User != nil {
				p, _ := u.User.Password()
				auth = &proxy.Auth{User: u.User.Username(), Password: p}
			}
			d, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
			if err != nil {
				return nil, err
			}
			transport.Proxy = nil
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) { return d.Dial(network, addr) }
		default:
			return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
		}
	}
	return &Client{HTTP: &http.Client{Transport: transport, Timeout: 45 * time.Second}, SchemaVersion: schemaVersion, UserAgent: userAgent, SessionURL: DefaultSessionURL, GraphQLURL: DefaultGraphQLURL, UploadPollInterval: 3 * time.Second, UploadTimeout: 5 * time.Minute}, nil
}

type Session struct {
	AccessToken       string `json:"accessToken"`
	AccessTokenIssued int64  `json:"accessTokenIssuedAt"`
	AccessTokenExpiry int64  `json:"accessTokenExpiry"`
	HasuraUserID      string `json:"hasuraUserId"`
	CognitoSub        string `json:"cognitoSub"`
}

type sessionEnvelope struct {
	Session Session `json:"session"`
	User    struct {
		Email string `json:"email"`
	} `json:"user"`
	NeedsRefresh bool `json:"needsRefresh"`
}

func (c *Client) GetSession(ctx context.Context, cookie string) (Session, string, error) {
	get, err := c.sessionRequest(ctx, http.MethodGet, cookie)
	if err != nil {
		return Session{}, "", err
	}
	if get.NeedsRefresh {
		get, err = c.sessionRequest(ctx, http.MethodPost, cookie)
		if err != nil {
			return Session{}, "", err
		}
	}
	if get.Session.AccessToken == "" || get.Session.AccessTokenExpiry == 0 {
		return Session{}, "", errors.New("session response has no access token")
	}
	return get.Session, get.User.Email, nil
}

func (c *Client) sessionRequest(ctx context.Context, method, cookie string) (sessionEnvelope, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.SessionURL, nil)
	if err != nil {
		return sessionEnvelope{}, err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Origin", "https://app.leonardo.ai")
	req.Header.Set("Referer", "https://app.leonardo.ai/")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return sessionEnvelope{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode/100 != 2 {
		return sessionEnvelope{}, &HTTPError{Status: resp.StatusCode, Body: truncate(body)}
	}
	var out sessionEnvelope
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

type gqlRequest struct {
	OperationName string `json:"operationName"`
	Query         string `json:"query"`
	Variables     any    `json:"variables"`
}
type gqlEnvelope[T any] struct {
	Data   T `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

func (c *Client) graphql(ctx context.Context, token, teamID, operation, query string, variables any, out any) error {
	body, _ := json.Marshal(gqlRequest{OperationName: operation, Query: query, Variables: variables})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GraphQLURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://app.leonardo.ai")
	req.Header.Set("Referer", "https://app.leonardo.ai/")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("x-leo-schema-version", c.SchemaVersion)
	if teamID != "" {
		req.Header.Set("x-leonardo-team-id", teamID)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return &HTTPError{Status: resp.StatusCode, Body: truncate(raw)}
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string         `json:"message"`
			Extensions map[string]any `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return &GraphQLError{Operation: operation, Message: envelope.Errors[0].Message, Extensions: envelope.Errors[0].Extensions}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

type Tokens struct {
	Plan                         string
	Subscription, Rollover, Paid int64
}

type PlatformImageModel struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	ModelID         string          `json:"model_id"`
	Name            string          `json:"name"`
	Provider        string          `json:"provider"`
	Description     string          `json:"description"`
	ThumbnailURL    string          `json:"thumbnail_url,omitempty"`
	Order           int             `json:"order"`
	ProductionAPI   bool            `json:"production_api"`
	Capabilities    map[string]bool `json:"capabilities"`
	CostType        string          `json:"cost_type,omitempty"`
	BaseTokenCost   float64         `json:"base_token_cost,omitempty"`
	QualityOptions  []string        `json:"quality_options,omitempty"`
	DefaultQuality  string          `json:"default_quality,omitempty"`
	DefaultQuantity int             `json:"default_quantity,omitempty"`
	MaximumQuantity int             `json:"maximum_quantity,omitempty"`
	DurationOptions []int           `json:"duration_options,omitempty"`
	DefaultDuration int             `json:"default_duration,omitempty"`
	ResolutionModes []string        `json:"resolution_modes,omitempty"`
}

func (c *Client) ListPlatformImageModels(ctx context.Context, token, teamID string) ([]PlatformImageModel, error) {
	return c.listPlatformModels(ctx, token, teamID, "image")
}

func (c *Client) ListPlatformVideoModels(ctx context.Context, token, teamID string) ([]PlatformImageModel, error) {
	return c.listPlatformModels(ctx, token, teamID, "video")
}

func (c *Client) ListPlatformAudioModels(ctx context.Context, token, teamID string) ([]PlatformImageModel, error) {
	return c.listPlatformModels(ctx, token, teamID, "audio")
}

func (c *Client) listPlatformModels(ctx context.Context, token, teamID, mediaType string) ([]PlatformImageModel, error) {
	const q = `query GetRelease($version: String!, $schemaIds: [String!]!) @cached(ttl: 300) { release(id:$version){id schemaReferences(schemaIds:$schemaIds,recursive:true){schemaId schemaData}} }`
	var data struct {
		Release struct {
			SchemaReferences []struct {
				SchemaID   string          `json:"schemaId"`
				SchemaData json.RawMessage `json:"schemaData"`
			} `json:"schemaReferences"`
		} `json:"release"`
	}
	vars := map[string]any{"version": c.SchemaVersion, "schemaIds": []string{"https://leonardo.ai/platform/requests/generate/meta"}}
	if err := c.graphql(ctx, token, teamID, "GetRelease", q, vars, &data); err != nil {
		return nil, err
	}
	var models []PlatformImageModel
	for _, ref := range data.Release.SchemaReferences {
		if !strings.Contains(ref.SchemaID, "/requests/generate/models/") {
			continue
		}
		var schema struct {
			Properties struct {
				Model struct {
					Const      string `json:"const"`
					UIMetadata struct {
						Badge struct {
							Alt string `json:"alt"`
						} `json:"badge"`
						Order        int    `json:"order"`
						Description  string `json:"description"`
						ThumbnailURL string `json:"thumbnail_url"`
					} `json:"ui:metadata"`
					ModelConfig struct {
						ID           string          `json:"id"`
						Name         string          `json:"name"`
						Type         string          `json:"type"`
						Capabilities map[string]bool `json:"capabilities"`
					} `json:"leo:model_config"`
					CostConfig struct {
						Tokens struct {
							Type   string  `json:"type"`
							Amount float64 `json:"amount"`
						} `json:"tokens"`
					} `json:"leo:cost_config"`
				} `json:"model"`
				Parameters struct {
					Properties struct {
						Quality struct {
							Enum    []string `json:"enum"`
							Default string   `json:"default"`
						} `json:"quality"`
						Quantity struct {
							Default int `json:"default"`
							Maximum int `json:"maximum"`
						} `json:"quantity"`
						Duration struct {
							Enum    []int `json:"enum"`
							Default int   `json:"default"`
						} `json:"duration"`
						Mode struct {
							Enum []string `json:"enum"`
						} `json:"mode"`
						Resolution struct {
							Enum []string `json:"enum"`
						} `json:"resolution"`
					} `json:"properties"`
				} `json:"parameters"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(ref.SchemaData, &schema); err != nil {
			continue
		}
		m := schema.Properties.Model
		if m.Const == "" || m.ModelConfig.Type != mediaType || !m.ModelConfig.Capabilities["generate"] {
			continue
		}
		provider := strings.TrimSpace(strings.TrimSuffix(m.UIMetadata.Badge.Alt, " logo"))
		resolutionModes := schema.Properties.Parameters.Properties.Mode.Enum
		if len(resolutionModes) == 0 {
			resolutionModes = schema.Properties.Parameters.Properties.Resolution.Enum
		}
		models = append(models, PlatformImageModel{
			ID: m.Const, Type: m.ModelConfig.Type, ModelID: m.ModelConfig.ID, Name: m.ModelConfig.Name, Provider: provider,
			Description: m.UIMetadata.Description, ThumbnailURL: m.UIMetadata.ThumbnailURL, Order: m.UIMetadata.Order,
			ProductionAPI: m.ModelConfig.Capabilities["production_api_availability"], Capabilities: m.ModelConfig.Capabilities,
			CostType: m.CostConfig.Tokens.Type, BaseTokenCost: m.CostConfig.Tokens.Amount,
			QualityOptions:  schema.Properties.Parameters.Properties.Quality.Enum,
			DefaultQuality:  schema.Properties.Parameters.Properties.Quality.Default,
			DefaultQuantity: schema.Properties.Parameters.Properties.Quantity.Default,
			MaximumQuantity: schema.Properties.Parameters.Properties.Quantity.Maximum,
			DurationOptions: schema.Properties.Parameters.Properties.Duration.Enum,
			DefaultDuration: schema.Properties.Parameters.Properties.Duration.Default,
			ResolutionModes: resolutionModes,
		})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Order == models[j].Order {
			return models[i].Name < models[j].Name
		}
		return models[i].Order < models[j].Order
	})
	return models, nil
}

func (c *Client) GetTokens(ctx context.Context, token, teamID, sub string) (Tokens, error) {
	const q = `query GetUserTokensFromSub($sub: String) { user_details(where:{cognitoId:{_eq:$sub}}){plan subscriptionTokens rolloverTokens paidTokens} teams{id akUUID subscriptionTokens rolloverTokens paidTokens} }`
	var data struct {
		UserDetails []struct {
			Plan         string `json:"plan"`
			Subscription int64  `json:"subscriptionTokens"`
			Rollover     int64  `json:"rolloverTokens"`
			Paid         int64  `json:"paidTokens"`
		} `json:"user_details"`
		Teams []struct {
			AKUUID       string `json:"akUUID"`
			Subscription int64  `json:"subscriptionTokens"`
			Rollover     int64  `json:"rolloverTokens"`
			Paid         int64  `json:"paidTokens"`
		} `json:"teams"`
	}
	if err := c.graphql(ctx, token, teamID, "GetUserTokensFromSub", q, map[string]any{"sub": sub}, &data); err != nil {
		return Tokens{}, err
	}
	if teamID != "" {
		for _, t := range data.Teams {
			if t.AKUUID == teamID {
				return Tokens{Subscription: t.Subscription, Rollover: t.Rollover, Paid: t.Paid}, nil
			}
		}
	}
	if len(data.UserDetails) == 0 {
		return Tokens{}, errors.New("token query returned no user details")
	}
	u := data.UserDetails[0]
	return Tokens{Plan: u.Plan, Subscription: u.Subscription, Rollover: u.Rollover, Paid: u.Paid}, nil
}

type GenerateRequest struct {
	Model                   string
	Prompt                  string
	Width, Height, Quantity int
	Public                  bool
	StyleIDs, ReferenceIDs  []string
	InitImageID             string
	InitStrength            *float64
	Quality                 string
	ImageReferences         []ImageReference
}

type ImageReference struct {
	ID       string
	Type     string
	Strength string
}

type MediaReference struct {
	ID             string
	Type           string
	Duration       float64
	Width          int
	Height         int
	MotionHasAudio bool
}
type GenerateResponse struct {
	GenerationID  string
	APICreditCost *float64
}

type GenerateVideoRequest struct {
	Model           string
	Prompt          string
	Width, Height   int
	Duration        int
	ResolutionMode  string
	Resolution      string
	Public          bool
	ImageReferences []ImageReference
	StartFrame      *ImageReference
	EndFrame        *ImageReference
	VideoReferences []MediaReference
	AudioReference  *MediaReference
	AudioReferences []MediaReference
	GenerateAudio   *bool
}

type GenerateAudioRequest struct {
	Model             string
	Prompt            string
	Quantity          int
	Duration          int
	DurationMinutes   int
	ForceInstrumental bool
	Loop              bool
	VoiceID           string
	LanguageCode      string
	PromptInfluence   *float64
	Public            bool
}

// CreateGenerationRequest is the exact GraphQL variables.request payload sent
// to Leonardo. Authentication and transport headers are intentionally separate.
type CreateGenerationRequest struct {
	Model      string         `json:"model"`
	Public     bool           `json:"public"`
	Parameters map[string]any `json:"parameters"`
}

func BuildImageGenerationRequest(in GenerateRequest) CreateGenerationRequest {
	params := map[string]any{"height": in.Height, "width": in.Width, "quantity": in.Quantity, "prompt": in.Prompt}
	if len(in.StyleIDs) > 0 {
		params["style_ids"] = in.StyleIDs
	}
	if len(in.ReferenceIDs) > 0 {
		params["reference_ids"] = in.ReferenceIDs
	}
	if in.InitImageID != "" {
		params["init_image_id"] = in.InitImageID
		if in.InitStrength != nil {
			params["init_strength"] = *in.InitStrength
		}
	}
	if in.Quality != "" && in.Quality != "auto" {
		params["quality"] = strings.ToUpper(in.Quality)
	}
	if len(in.ImageReferences) > 0 {
		references := make([]map[string]any, 0, len(in.ImageReferences))
		for _, ref := range in.ImageReferences {
			strength := ref.Strength
			if strength == "" {
				strength = "MID"
			}
			references = append(references, map[string]any{
				"image":    map[string]any{"id": ref.ID, "type": ref.Type},
				"strength": strings.ToUpper(strength),
			})
		}
		params["guidances"] = map[string]any{"image_reference": references}
	}
	return CreateGenerationRequest{Model: in.Model, Public: in.Public, Parameters: params}
}

func BuildVideoGenerationRequest(in GenerateVideoRequest) CreateGenerationRequest {
	params := map[string]any{
		"width": in.Width, "height": in.Height, "quantity": 1,
		"prompt": in.Prompt,
	}
	if in.Duration > 0 {
		params["duration"] = in.Duration
	}
	if in.ResolutionMode != "" {
		params["mode"] = in.ResolutionMode
	}
	if in.Resolution != "" {
		params["resolution"] = in.Resolution
	}
	if len(in.ImageReferences) > 0 {
		references := make([]map[string]any, 0, len(in.ImageReferences))
		for _, ref := range in.ImageReferences {
			strength := ref.Strength
			if strength == "" {
				strength = "MID"
			}
			references = append(references, map[string]any{
				"image":    map[string]any{"id": ref.ID, "type": ref.Type},
				"strength": strings.ToUpper(strength),
			})
		}
		params["guidances"] = map[string]any{"image_reference": references}
	}
	guidances, _ := params["guidances"].(map[string]any)
	if guidances == nil {
		guidances = map[string]any{}
	}
	if in.StartFrame != nil {
		guidances["start_frame"] = []map[string]any{{"image": map[string]any{"id": in.StartFrame.ID, "type": in.StartFrame.Type}}}
	}
	if in.EndFrame != nil {
		guidances["end_frame"] = []map[string]any{{"image": map[string]any{"id": in.EndFrame.ID, "type": in.EndFrame.Type}}}
	}
	if len(in.VideoReferences) > 0 {
		references := make([]map[string]any, 0, len(in.VideoReferences))
		for _, ref := range in.VideoReferences {
			references = append(references, map[string]any{"video": map[string]any{
				"id": ref.ID, "type": ref.Type, "duration": ref.Duration,
				"width": ref.Width, "height": ref.Height, "motion_has_audio": ref.MotionHasAudio,
			}})
		}
		guidances["video_reference_base"] = references
	}
	audioReferences := append([]MediaReference(nil), in.AudioReferences...)
	if in.AudioReference != nil {
		audioReferences = append([]MediaReference{*in.AudioReference}, audioReferences...)
	}
	if len(audioReferences) > 0 {
		references := make([]map[string]any, 0, len(audioReferences))
		for _, ref := range audioReferences {
			references = append(references, map[string]any{"audio": map[string]any{
				"id": ref.ID, "type": ref.Type, "duration": ref.Duration,
			}})
		}
		guidances["audio_reference"] = references
	}
	if len(guidances) > 0 {
		params["guidances"] = guidances
	}
	if in.GenerateAudio != nil {
		params["motion_has_audio"] = *in.GenerateAudio
	}
	return CreateGenerationRequest{Model: in.Model, Public: in.Public, Parameters: params}
}

func BuildAudioGenerationRequest(in GenerateAudioRequest) CreateGenerationRequest {
	params := map[string]any{"prompt": in.Prompt, "quantity": in.Quantity}
	switch in.Model {
	case "dialogue-v3":
		params["voice_id"] = in.VoiceID
		params["language_code"] = in.LanguageCode
		if in.PromptInfluence != nil {
			params["prompt_influence"] = *in.PromptInfluence
		}
	case "music-v1":
		params["duration_minutes"] = in.DurationMinutes
		params["force_instrumental"] = in.ForceInstrumental
	case "sound-effects-v2":
		params["duration"] = in.Duration
		params["loop"] = in.Loop
		if in.PromptInfluence != nil {
			params["prompt_influence"] = *in.PromptInfluence
		}
	}
	return CreateGenerationRequest{Model: in.Model, Public: in.Public, Parameters: params}
}

func (c *Client) SubmitGeneration(ctx context.Context, token, teamID string, request CreateGenerationRequest) (GenerateResponse, error) {
	const q = `mutation Generate($request: CreateGenerationRequest!) { generate(request:$request){apiCreditCost generationId} }`
	vars := map[string]any{"request": request}
	var data struct {
		Generate struct {
			APICreditCost *float64 `json:"apiCreditCost"`
			GenerationID  string   `json:"generationId"`
		} `json:"generate"`
	}
	if err := c.graphql(ctx, token, teamID, "Generate", q, vars, &data); err != nil {
		return GenerateResponse{}, err
	}
	if data.Generate.GenerationID == "" {
		return GenerateResponse{}, errors.New("generate response has no generationId")
	}
	return GenerateResponse{GenerationID: data.Generate.GenerationID, APICreditCost: data.Generate.APICreditCost}, nil
}

func (c *Client) Generate(ctx context.Context, token, teamID string, in GenerateRequest) (GenerateResponse, error) {
	return c.SubmitGeneration(ctx, token, teamID, BuildImageGenerationRequest(in))
}

func (c *Client) GenerateVideo(ctx context.Context, token, teamID string, in GenerateVideoRequest) (GenerateResponse, error) {
	return c.SubmitGeneration(ctx, token, teamID, BuildVideoGenerationRequest(in))
}

func (c *Client) GenerateAudio(ctx context.Context, token, teamID string, in GenerateAudioRequest) (GenerateResponse, error) {
	return c.SubmitGeneration(ctx, token, teamID, BuildAudioGenerationRequest(in))
}

type UploadResult struct {
	InitImageID   string
	Width, Height int
}

type UploadedMedia struct {
	ID       string
	URL      string
	Duration float64
	Width    int
	Height   int
}

func (c *Client) UploadMedia(ctx context.Context, token, teamID, filename, extension string, source io.Reader) (UploadedMedia, error) {
	extension = strings.ToLower(strings.TrimPrefix(extension, "."))
	if extension == "" {
		return UploadedMedia{}, errors.New("media extension is required")
	}
	const q = `mutation UploadImage($uploadImageInput: UploadImageInput!) { uploadImage(arg1:$uploadImageInput){uploadId url fields} }`
	input := map[string]any{"uploadType": "INIT", "extension": extension, "originalFilename": filename}
	if teamID != "" {
		input["teamId"] = teamID
	}
	var prepared struct {
		UploadImage struct {
			UploadID string `json:"uploadId"`
			URL      string `json:"url"`
			Fields   string `json:"fields"`
		} `json:"uploadImage"`
	}
	if err := c.graphql(ctx, token, teamID, "UploadImage", q, map[string]any{"uploadImageInput": input}, &prepared); err != nil {
		return UploadedMedia{}, err
	}
	if prepared.UploadImage.UploadID == "" || prepared.UploadImage.URL == "" || prepared.UploadImage.Fields == "" {
		return UploadedMedia{}, errors.New("upload media response is incomplete")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(prepared.UploadImage.Fields), &fields); err != nil {
		return UploadedMedia{}, fmt.Errorf("decode upload fields: %w", err)
	}
	pipeReader, pipeWriter := io.Pipe()
	mw := multipart.NewWriter(pipeWriter)
	writeDone := make(chan error, 1)
	go func() {
		var writeErr error
		defer func() {
			if closeErr := mw.Close(); writeErr == nil {
				writeErr = closeErr
			}
			_ = pipeWriter.CloseWithError(writeErr)
			writeDone <- writeErr
		}()
		for key, value := range fields {
			if value == nil {
				continue
			}
			if writeErr = mw.WriteField(key, fmt.Sprint(value)); writeErr != nil {
				return
			}
		}
		part, err := mw.CreateFormFile("file", filename)
		if err != nil {
			writeErr = err
			return
		}
		_, writeErr = io.Copy(part, source)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, prepared.UploadImage.URL, pipeReader)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		return UploadedMedia{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		<-writeDone
		return UploadedMedia{}, err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	writeErr := <-writeDone
	if writeErr != nil {
		return UploadedMedia{}, writeErr
	}
	if resp.StatusCode/100 != 2 {
		_ = c.DeleteUploadedMedia(context.Background(), token, teamID, []string{prepared.UploadImage.UploadID})
		return UploadedMedia{}, &HTTPError{Status: resp.StatusCode, Body: truncate(raw)}
	}
	result, err := c.waitUploadedMedia(ctx, token, teamID, prepared.UploadImage.UploadID)
	if err != nil {
		_ = c.DeleteUploadedMedia(context.Background(), token, teamID, []string{prepared.UploadImage.UploadID})
		return UploadedMedia{}, err
	}
	return result, nil
}

func (c *Client) waitUploadedMedia(ctx context.Context, token, teamID, uploadID string) (UploadedMedia, error) {
	const q = `query GetUploadedMediaById($uploadId: uuid!) { uploaded_media(where:{id:{_eq:$uploadId}},limit:1){id url thumbnailUrl width height duration createdAt fileSize status statusReason video_fps videoCodec} }`
	poll := c.UploadPollInterval
	if poll <= 0 {
		poll = 3 * time.Second
	}
	timeout := c.UploadTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		var data struct {
			Media []struct {
				ID           string  `json:"id"`
				URL          string  `json:"url"`
				Width        int     `json:"width"`
				Height       int     `json:"height"`
				Duration     float64 `json:"duration"`
				Status       string  `json:"status"`
				StatusReason string  `json:"statusReason"`
			} `json:"uploaded_media"`
		}
		if err := c.graphql(ctx, token, teamID, "GetUploadedMediaById", q, map[string]any{"uploadId": uploadID}, &data); err != nil {
			return UploadedMedia{}, err
		}
		if len(data.Media) > 0 {
			media := data.Media[0]
			switch strings.ToUpper(media.Status) {
			case "COMPLETE":
				if media.ID == "" {
					return UploadedMedia{}, errors.New("completed media upload has no id")
				}
				return UploadedMedia{ID: media.ID, URL: media.URL, Duration: media.Duration, Width: media.Width, Height: media.Height}, nil
			case "FAILED":
				return UploadedMedia{}, fmt.Errorf("media processing failed: %s", media.StatusReason)
			}
		}
		select {
		case <-ctx.Done():
			return UploadedMedia{}, ctx.Err()
		case <-deadline.C:
			return UploadedMedia{}, errors.New("media processing timed out")
		case <-ticker.C:
		}
	}
}

func (c *Client) DeleteUploadedMedia(ctx context.Context, token, teamID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	const q = `mutation DeleteUploadedMedia($ids: [uuid!]!) { delete_uploaded_media(where:{id:{_in:$ids}}){affected_rows returning{id}} }`
	return c.graphql(ctx, token, teamID, "DeleteUploadedMedia", q, map[string]any{"ids": ids}, nil)
}

func (c *Client) UploadInitImage(ctx context.Context, token, teamID, filename, mediaType string, data []byte) (UploadResult, error) {
	ext := strings.ToLower(strings.TrimPrefix(pathExtension(filename), "."))
	if ext == "jpeg" {
		ext = "jpg"
	}
	if ext == "" {
		switch mediaType {
		case "image/png":
			ext = "png"
		case "image/jpeg":
			ext = "jpg"
		case "image/webp":
			ext = "webp"
		}
	}
	if ext != "png" && ext != "jpg" && ext != "webp" {
		return UploadResult{}, fmt.Errorf("unsupported image extension %q", ext)
	}
	const q = `mutation UploadImage($uploadImageInput: UploadImageInput!) { uploadImage(arg1:$uploadImageInput){uploadId url fields} }`
	input := map[string]any{"uploadType": "INIT", "extension": ext}
	if teamID != "" {
		input["teamId"] = teamID
	}
	var prepared struct {
		UploadImage struct {
			UploadID string `json:"uploadId"`
			URL      string `json:"url"`
			Fields   string `json:"fields"`
		} `json:"uploadImage"`
	}
	if err := c.graphql(ctx, token, teamID, "UploadImage", q, map[string]any{"uploadImageInput": input}, &prepared); err != nil {
		return UploadResult{}, err
	}
	if prepared.UploadImage.UploadID == "" || prepared.UploadImage.URL == "" || prepared.UploadImage.Fields == "" {
		return UploadResult{}, errors.New("upload image response is incomplete")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(prepared.UploadImage.Fields), &fields); err != nil {
		return UploadResult{}, fmt.Errorf("decode upload fields: %w", err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		if v == nil {
			continue
		}
		if err := mw.WriteField(k, fmt.Sprint(v)); err != nil {
			return UploadResult{}, err
		}
	}
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return UploadResult{}, err
	}
	if _, err = part.Write(data); err != nil {
		return UploadResult{}, err
	}
	if err = mw.Close(); err != nil {
		return UploadResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, prepared.UploadImage.URL, &body)
	if err != nil {
		return UploadResult{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return UploadResult{}, err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_ = c.DeleteInitImage(context.Background(), token, teamID, prepared.UploadImage.UploadID)
		return UploadResult{}, &HTTPError{Status: resp.StatusCode, Body: truncate(raw)}
	}
	result, err := c.waitInitImageModeration(ctx, token, teamID, prepared.UploadImage.UploadID)
	if err != nil {
		_ = c.DeleteInitImage(context.Background(), token, teamID, prepared.UploadImage.UploadID)
		return UploadResult{}, err
	}
	return result, nil
}

func (c *Client) waitInitImageModeration(ctx context.Context, token, teamID, uploadID string) (UploadResult, error) {
	const q = `query GetInitImageModeration($akUUID: uuid!) { init_image_moderation(where:{akUUID:{_eq:$akUUID}}){akUUID initImageId checkStatus init_image{imageWidth:image_width imageHeight:image_height}} }`
	poll := c.UploadPollInterval
	if poll <= 0 {
		poll = 3 * time.Second
	}
	timeout := c.UploadTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		var data struct {
			Moderation []struct {
				InitImageID string `json:"initImageId"`
				CheckStatus string `json:"checkStatus"`
				Image       *struct {
					Width  int `json:"imageWidth"`
					Height int `json:"imageHeight"`
				} `json:"init_image"`
			} `json:"init_image_moderation"`
		}
		if err := c.graphql(ctx, token, teamID, "GetInitImageModeration", q, map[string]any{"akUUID": uploadID}, &data); err != nil {
			return UploadResult{}, err
		}
		if len(data.Moderation) > 0 {
			m := data.Moderation[0]
			switch m.CheckStatus {
			case "Accepted":
				if m.InitImageID == "" {
					return UploadResult{}, errors.New("accepted upload has no initImageId")
				}
				out := UploadResult{InitImageID: m.InitImageID}
				if m.Image != nil {
					out.Width, out.Height = m.Image.Width, m.Image.Height
				}
				return out, nil
			case "Blocked":
				return UploadResult{}, errors.New("image was blocked by Leonardo moderation")
			case "Failed", "TimeOut":
				return UploadResult{}, fmt.Errorf("image moderation failed: %s", m.CheckStatus)
			}
		}
		select {
		case <-ctx.Done():
			return UploadResult{}, ctx.Err()
		case <-deadline.C:
			return UploadResult{}, errors.New("image moderation timed out")
		case <-ticker.C:
		}
	}
}

func (c *Client) DeleteInitImage(ctx context.Context, token, teamID, id string) error {
	const q = `mutation DeleteInitImage($id: uuid!) { delete_init_images_by_pk(id:$id){id} }`
	return c.graphql(ctx, token, teamID, "DeleteInitImage", q, map[string]any{"id": id}, nil)
}

func pathExtension(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

func (c *Client) Status(ctx context.Context, token, teamID, generationID string) (string, error) {
	const q = `query GetAIGenerationStatus($id: uuid!) { generations_by_pk(id:$id){id status} }`
	var data struct {
		Generation *struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"generations_by_pk"`
	}
	if err := c.graphql(ctx, token, teamID, "GetAIGenerationStatus", q, map[string]any{"id": generationID}, &data); err != nil {
		return "", err
	}
	if data.Generation == nil {
		return "", errors.New("generation was not found")
	}
	status := strings.ToUpper(strings.TrimSpace(data.Generation.Status))
	switch status {
	case "PENDING", "COMPLETE", "FAILED":
		return status, nil
	default:
		return "", fmt.Errorf("unsupported generation status %q", data.Generation.Status)
	}
}

type GenerationNote struct {
	NoteType      string          `json:"noteType"`
	NotePayload   json.RawMessage `json:"notePayload,omitempty"`
	FailureReason json.RawMessage `json:"failureReason,omitempty"`
}

type PromptModeration struct {
	ModerationClassification json.RawMessage `json:"moderationClassification,omitempty"`
}

// PromptModerationList normalizes Leonardo's moderation relation, which has
// appeared as both a single object and an array across generation responses.
type PromptModerationList []PromptModeration

func (list *PromptModerationList) UnmarshalJSON(data []byte) error {
	raw := bytes.TrimSpace(data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		*list = nil
		return nil
	}
	if raw[0] == '[' {
		var items []PromptModeration
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		*list = items
		return nil
	}
	var item PromptModeration
	if err := json.Unmarshal(raw, &item); err != nil {
		return fmt.Errorf("prompt moderation must be an object or array: %w", err)
	}
	*list = []PromptModeration{item}
	return nil
}

type GenerationFailure struct {
	ID                string               `json:"id"`
	Status            string               `json:"status"`
	NSFW              bool                 `json:"nsfw"`
	Notes             []GenerationNote     `json:"notes"`
	PromptModerations PromptModerationList `json:"prompt_moderations"`
}

func (f GenerationFailure) ProviderErrorCode() string {
	for _, note := range f.Notes {
		var reason struct {
			ErrorCode string `json:"errorCode"`
		}
		if json.Unmarshal(note.FailureReason, &reason) == nil && reason.ErrorCode != "" {
			return reason.ErrorCode
		}
	}
	return ""
}

func (c *Client) FailureDetails(ctx context.Context, token, teamID, generationID string) (GenerationFailure, error) {
	const q = `query GetGenerationFailure($id: uuid!) { generations_by_pk(id:$id){id status nsfw prompt_moderations{moderationClassification} notes{noteType notePayload failureReason}} }`
	var data struct {
		Generation *GenerationFailure `json:"generations_by_pk"`
	}
	if err := c.graphql(ctx, token, teamID, "GetGenerationFailure", q, map[string]any{"id": generationID}, &data); err != nil {
		return GenerationFailure{}, err
	}
	if data.Generation == nil {
		return GenerationFailure{}, errors.New("generation failure details not found")
	}
	return *data.Generation, nil
}

type Output struct {
	ID, URL                    string
	MotionMP4URL, MotionGIFURL string
	Width, Height              int
	NSFW                       bool
	ModerationClassifications  []string
}

type GenerationResult struct {
	Outputs                   []Output
	NSFW                      bool
	ModerationClassifications []string
}

func (c *Client) Result(ctx context.Context, token, teamID, generationID string) (GenerationResult, error) {
	const q = `query GetGenerationResult($id: uuid!) { generations_by_pk(id:$id){id status prompt nsfw prompt_moderations{moderationClassification} generated_images(order_by:[{url:desc}]){id url motionMP4URL motionGIFURL image_width image_height nsfw generated_image_moderation{moderationClassification}}} }`
	var data struct {
		Generation *struct {
			ID                string               `json:"id"`
			Status            string               `json:"status"`
			NSFW              bool                 `json:"nsfw"`
			PromptModerations PromptModerationList `json:"prompt_moderations"`
			Images            []struct {
				ID           string               `json:"id"`
				URL          string               `json:"url"`
				MotionMP4URL string               `json:"motionMP4URL"`
				MotionGIFURL string               `json:"motionGIFURL"`
				Width        int                  `json:"image_width"`
				Height       int                  `json:"image_height"`
				NSFW         bool                 `json:"nsfw"`
				Moderations  PromptModerationList `json:"generated_image_moderation"`
			} `json:"generated_images"`
		} `json:"generations_by_pk"`
	}
	if err := c.graphql(ctx, token, teamID, "GetGenerationResult", q, map[string]any{"id": generationID}, &data); err != nil {
		return GenerationResult{}, err
	}
	if data.Generation == nil {
		return GenerationResult{}, errors.New("generation result not found")
	}
	promptLabels := moderationClassifications(data.Generation.PromptModerations)
	result := GenerationResult{
		NSFW:                      data.Generation.NSFW,
		ModerationClassifications: append([]string(nil), promptLabels...),
		Outputs:                   make([]Output, 0, len(data.Generation.Images)),
	}
	for _, image := range data.Generation.Images {
		labels := mergeClassifications(promptLabels, moderationClassifications(image.Moderations))
		result.NSFW = result.NSFW || image.NSFW
		result.ModerationClassifications = mergeClassifications(result.ModerationClassifications, labels)
		result.Outputs = append(result.Outputs, Output{
			ID: image.ID, URL: image.URL, MotionMP4URL: image.MotionMP4URL, MotionGIFURL: image.MotionGIFURL,
			Width: image.Width, Height: image.Height, NSFW: data.Generation.NSFW || image.NSFW,
			ModerationClassifications: labels,
		})
	}
	return result, nil
}

func moderationClassifications(items PromptModerationList) []string {
	labels := make([]string, 0)
	for _, item := range items {
		var values []string
		if json.Unmarshal(item.ModerationClassification, &values) == nil {
			labels = mergeClassifications(labels, values)
			continue
		}
		var value string
		if json.Unmarshal(item.ModerationClassification, &value) == nil && value != "" {
			labels = mergeClassifications(labels, []string{value})
		}
	}
	return labels
}

func mergeClassifications(groups ...[]string) []string {
	seen := make(map[string]struct{})
	merged := make([]string, 0)
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, value)
		}
	}
	return merged
}

func (c *Client) AudioResult(ctx context.Context, token, teamID, generationID string) ([]Output, error) {
	const q = `query GetAudioGenerationResult($id: uuid!) { generations_by_pk(id:$id){id status generated_images(order_by:[{url:desc}]){id url urls{asset}} generations_metadata{metadata}} }`
	var data struct {
		Generation *struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Images []struct {
				ID   string `json:"id"`
				URL  string `json:"url"`
				URLs *struct {
					Asset string `json:"asset"`
				} `json:"urls"`
			} `json:"generated_images"`
			Metadata *struct {
				Value json.RawMessage `json:"metadata"`
			} `json:"generations_metadata"`
		} `json:"generations_by_pk"`
	}
	if err := c.graphql(ctx, token, teamID, "GetAudioGenerationResult", q, map[string]any{"id": generationID}, &data); err != nil {
		return nil, err
	}
	if data.Generation == nil {
		return nil, errors.New("audio generation result not found")
	}
	seen := map[string]bool{}
	outputs := make([]Output, 0, len(data.Generation.Images))
	add := func(id, rawURL string, fromAudioMetadata bool) {
		if rawURL == "" || seen[rawURL] || (!fromAudioMetadata && !isAudioURL(rawURL)) {
			return
		}
		seen[rawURL] = true
		outputs = append(outputs, Output{ID: id, URL: rawURL})
	}
	for _, image := range data.Generation.Images {
		rawURL := image.URL
		if rawURL == "" && image.URLs != nil {
			rawURL = image.URLs.Asset
		}
		add(image.ID, rawURL, false)
	}
	if data.Generation.Metadata != nil {
		for _, rawURL := range audioURLsFromMetadata(data.Generation.Metadata.Value) {
			add("", rawURL, true)
		}
	}
	return outputs, nil
}

func audioURLsFromMetadata(raw json.RawMessage) []string {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	seen := map[string]bool{}
	urls := make([]string, 0)
	var walk func(any, bool)
	walk = func(current any, audioContext bool) {
		switch v := current.(type) {
		case map[string]any:
			for key, child := range v {
				lower := strings.ToLower(key)
				walk(child, audioContext || strings.Contains(lower, "audio") || strings.Contains(lower, "sound") || strings.Contains(lower, "music"))
			}
		case []any:
			for _, child := range v {
				walk(child, audioContext)
			}
		case string:
			if (audioContext || isAudioURL(v)) && isHTTPURL(v) && !seen[v] {
				seen[v] = true
				urls = append(urls, v)
			}
		}
	}
	walk(value, false)
	return urls
}

func isAudioURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	path := strings.ToLower(u.Path)
	return strings.HasSuffix(path, ".mp3") || strings.HasSuffix(path, ".wav") || strings.HasSuffix(path, ".ogg") || strings.HasSuffix(path, ".m4a") || strings.HasSuffix(path, ".flac") || strings.HasSuffix(path, ".aac")
}

func isHTTPURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func truncate(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 2048 {
		return s[:2048]
	}
	return s
}
