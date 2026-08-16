package adobe

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultImageSubmitURL = "https://firefly-3p.ff.adobe.io/v2/3p-images/generate-async"
	DefaultVideoSubmitURL = "https://firefly-3p.ff.adobe.io/v2/3p-videos/generate-async"
	DefaultImageUploadURL = "https://firefly-3p.ff.adobe.io/v2/storage/image"
	DefaultVideoUploadURL = "https://firefly-3p.ff.adobe.io/v2/storage/video"
	DefaultAudioUploadURL = "https://firefly-3p.ff.adobe.io/v2/storage/audio"
	DefaultProfileURL     = "https://ims-na1.adobelogin.com/ims/profile/v1"
	DefaultCreditsURL     = "https://firefly.adobe.io/v1/credits/balance"
	DefaultCreditsCostURL = "https://bks.adobe.io/v2/credits/cost"
	DefaultRefreshURL     = "https://adobeid-na1.services.adobe.com/ims/check/v6/token?jslVersion=v2-v0.54.0-3-g58cfcb7"
	DefaultRefreshScope   = "AdobeID,firefly_api,openid,pps.read,pps.write,additional_info.projectedProductContext,additional_info.ownerOrg,uds_read,uds_write,ab.manage,read_organizations,additional_info.roles,account_cluster.read,creative_production,profile"
)

type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("adobe returned HTTP %d: %s", e.Status, e.Body)
}

type Client struct {
	HTTP           *http.Client
	SubmitAPIKey   string
	CreditsAPIKey  string
	UserAgent      string
	ImageSubmitURL string
	VideoSubmitURL string
	ImageUploadURL string
	VideoUploadURL string
	AudioUploadURL string
	ProfileURL     string
	CreditsURL     string
	CreditsCostURL string
	RefreshURL     string
	RequestTimeout time.Duration
}

type AccessTokenInfo struct {
	AccountID string
	ClientID  string
	CreatedAt time.Time
	ExpiresAt time.Time
	Scopes    []string
}

type Profile struct {
	DisplayName string
	Email       string
	UserID      string
	Raw         map[string]any
}

type Credits struct {
	Total          int64
	Used           int64
	Available      int64
	AvailableUntil string
	AccountID      string
}

type Reference struct {
	ID              string `json:"id,omitempty"`
	Usage           string `json:"usage,omitempty"`
	PromptReference int    `json:"promptReference,omitempty"`
	Order           int    `json:"order,omitempty"`
}

type SubmitRequest struct {
	ModelID            string
	ModelVersion       string
	Prompt             string
	Width              int
	Height             int
	OutputResolution   string
	N                  int
	Duration           int
	AspectRatio        string
	GenerateAudio      *bool
	NegativePrompt     string
	References         []Reference
	ReferenceImages    []string
	GenerationMetadata map[string]any
	ModelSpecific      map[string]any
	GenerationSettings map[string]any
	Output             map[string]any
	GroundSearch       *bool
	SkipCAI            *bool
}

type Job struct {
	ID      string
	PollURL string
}

type Output struct {
	ID        string
	URL       string
	MediaType string
	Width     int
	Height    int
}

type PollResult struct {
	Status   string
	Progress int
	Outputs  []Output
	Error    string
	Raw      map[string]any
}

type CostRequest struct {
	Features map[string]int `json:"features"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type CostResult struct {
	Credits int64
	Raw     map[string]any
}

func New(proxyURL, submitAPIKey, creditsAPIKey, userAgent string) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(proxyURL) != "" {
		proxy, err := url.Parse(strings.TrimSpace(proxyURL))
		if err != nil {
			return nil, fmt.Errorf("parse adobe proxy: %w", err)
		}
		if proxy.Scheme != "http" && proxy.Scheme != "https" {
			return nil, errors.New("adobe proxy must use http or https")
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	return NewWithHTTPClient(&http.Client{Transport: transport, Timeout: 60 * time.Second}, submitAPIKey, creditsAPIKey, userAgent), nil
}

func NewWithHTTPClient(httpClient *http.Client, submitAPIKey, creditsAPIKey, userAgent string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "Mozilla/5.0"
	}
	return &Client{
		HTTP: httpClient, SubmitAPIKey: strings.TrimSpace(submitAPIKey), CreditsAPIKey: strings.TrimSpace(creditsAPIKey), UserAgent: userAgent,
		ImageSubmitURL: DefaultImageSubmitURL, VideoSubmitURL: DefaultVideoSubmitURL,
		ImageUploadURL: DefaultImageUploadURL, VideoUploadURL: DefaultVideoUploadURL,
		AudioUploadURL: DefaultAudioUploadURL, ProfileURL: DefaultProfileURL,
		CreditsURL: DefaultCreditsURL, CreditsCostURL: DefaultCreditsCostURL,
		RefreshURL: DefaultRefreshURL, RequestTimeout: 60 * time.Second,
	}
}

func (c *Client) headers(token, apiKey, contentType string) http.Header {
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	h.Set("User-Agent", c.UserAgent)
	h.Set("Origin", "https://firefly.adobe.com")
	h.Set("Referer", "https://firefly.adobe.com/")
	h.Set("Accept", "application/json")
	if apiKey != "" {
		h.Set("x-api-key", apiKey)
	}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return h
}

func (c *Client) do(ctx context.Context, method, endpoint, token, apiKey string, body []byte, contentType string, extra http.Header) ([]byte, http.Header, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil, errors.New("adobe access token is required")
	}
	requestCtx := ctx
	if c.RequestTimeout > 0 {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, c.RequestTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(requestCtx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header = c.headers(token, apiKey, contentType)
	for key, values := range extra {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if readErr != nil {
		return nil, resp.Header, readErr
	}
	if resp.StatusCode/100 != 2 {
		return raw, resp.Header, &HTTPError{Status: resp.StatusCode, Body: truncate(raw)}
	}
	return raw, resp.Header, nil
}

func (c *Client) SubmitImage(ctx context.Context, token string, in SubmitRequest) (Job, error) {
	return c.submit(ctx, token, c.ImageSubmitURL, in, false)
}

func (c *Client) SubmitVideo(ctx context.Context, token string, in SubmitRequest) (Job, error) {
	return c.submit(ctx, token, c.VideoSubmitURL, in, true)
}

func (c *Client) submit(ctx context.Context, token, endpoint string, in SubmitRequest, video bool) (Job, error) {
	if strings.TrimSpace(in.Prompt) == "" {
		return Job{}, errors.New("adobe prompt is required")
	}
	if in.N < 1 {
		in.N = 1
	}
	if in.References == nil {
		in.References = []Reference{}
	}
	module := "text2image"
	submodule := "ff-image-generate"
	if video {
		module = "text2video"
		submodule = "ff-video-generate"
	}
	payload := map[string]any{
		"modelId": in.ModelID, "modelVersion": in.ModelVersion, "n": in.N,
		"prompt": in.Prompt, "seeds": []int{int(time.Now().UnixNano() % 999999)},
		"referenceBlobs": in.References, "output": nonNilMap(in.Output, map[string]any{"storeInputs": true}),
		"generationMetadata": nonNilMap(in.GenerationMetadata, map[string]any{"module": module, "submodule": submodule}),
	}
	if in.Width > 0 && in.Height > 0 {
		payload["size"] = map[string]int{"width": in.Width, "height": in.Height}
	}
	if in.OutputResolution != "" {
		payload["outputResolution"] = strings.ToUpper(strings.TrimSpace(in.OutputResolution))
	}
	if in.Duration > 0 {
		payload["duration"] = in.Duration
	}
	if in.AspectRatio != "" {
		payload["generationSettings"] = map[string]any{"aspectRatio": in.AspectRatio}
	}
	if in.GenerationSettings != nil {
		payload["generationSettings"] = in.GenerationSettings
	}
	if in.GenerateAudio != nil {
		payload["generateAudio"] = *in.GenerateAudio
	}
	if in.NegativePrompt != "" {
		payload["negativePrompt"] = in.NegativePrompt
	}
	if len(in.ReferenceImages) > 0 {
		payload["referenceImages"] = slicesToMap(in.ReferenceImages)
	}
	if in.ModelSpecific != nil {
		payload["modelSpecificPayload"] = in.ModelSpecific
	}
	if in.GroundSearch != nil {
		payload["groundSearch"] = *in.GroundSearch
	}
	if in.SkipCAI != nil {
		payload["skipCai"] = *in.SkipCAI
	}
	extra := make(http.Header)
	if nonce := submitNonce(token, in.Prompt); nonce != "" {
		extra.Set("x-nonce", nonce)
	}
	extra.Set("x-arp-session-id", arpSessionID())
	body, headers, err := c.do(ctx, http.MethodPost, endpoint, token, c.SubmitAPIKey, mustJSON(payload), "application/json", extra)
	if err != nil {
		return Job{}, err
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return Job{}, fmt.Errorf("decode adobe submit response: %w", err)
	}
	pollURL := strings.TrimSpace(headers.Get("x-override-status-link"))
	if pollURL == "" {
		pollURL = strings.TrimSpace(stringValue(response["statusUrl"]))
	}
	if pollURL == "" {
		if link, ok := response["links"].(map[string]any); ok {
			if result, ok := link["result"].(map[string]any); ok {
				pollURL = strings.TrimSpace(stringValue(result["href"]))
			} else {
				pollURL = strings.TrimSpace(stringValue(link["result"]))
			}
		}
	}
	if pollURL == "" {
		return Job{}, errors.New("adobe submit response has no result link")
	}
	if video {
		pollURL = normalizeVideoPollURL(pollURL)
	}
	return Job{ID: jobID(pollURL), PollURL: pollURL}, nil
}

func (c *Client) Poll(ctx context.Context, token, pollURL string) (PollResult, error) {
	raw, headers, err := c.do(ctx, http.MethodGet, pollURL, token, "", nil, "", nil)
	if err != nil {
		return PollResult{}, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return PollResult{}, fmt.Errorf("decode adobe poll response: %w", err)
	}
	status := firstStatus(data)
	if status == "" {
		status = headers.Get("x-task-status")
	}
	result := PollResult{Status: normalizeStatus(status), Progress: progressValue(data), Raw: data}
	if result.Progress == 0 {
		result.Progress = progressHeader(headers)
	}
	result.Outputs = parseOutputs(data)
	result.Error = findString(data, "message", "error_message", "errorMessage")
	if len(result.Outputs) > 0 && result.Status == "" {
		result.Status = "COMPLETE"
	}
	return result, nil
}

func (c *Client) Upload(ctx context.Context, token, mediaType string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("adobe media is empty")
	}
	endpoint := c.ImageUploadURL
	switch {
	case strings.HasPrefix(mediaType, "video/"):
		endpoint = c.VideoUploadURL
	case strings.HasPrefix(mediaType, "audio/"):
		endpoint = c.AudioUploadURL
	}
	raw, _, err := c.do(ctx, http.MethodPost, endpoint, token, c.SubmitAPIKey, data, mediaType, nil)
	if err != nil {
		return "", err
	}
	var response any
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode adobe upload response: %w", err)
	}
	if id := findID(response); id != "" {
		return id, nil
	}
	return "", errors.New("adobe upload response has no media id")
}

func (c *Client) Profile(ctx context.Context, token string) (Profile, error) {
	raw, _, err := c.do(ctx, http.MethodGet, c.ProfileURL, token, "", nil, "", nil)
	if err != nil {
		return Profile{}, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return Profile{}, fmt.Errorf("decode adobe profile response: %w", err)
	}
	return Profile{
		DisplayName: findString(data, "displayName", "display_name", "name"),
		Email:       findString(data, "email"),
		UserID:      findString(data, "userId", "user_id", "authId", "id"),
		Raw:         data,
	}, nil
}

func (c *Client) Credits(ctx context.Context, token string) (Credits, error) {
	info, err := ParseAccessToken(token)
	if err != nil {
		return Credits{}, err
	}
	extra := make(http.Header)
	extra.Set("x-account-id", info.AccountID)
	raw, _, err := c.do(ctx, http.MethodGet, c.CreditsURL, token, c.CreditsAPIKey, nil, "application/json", extra)
	if err != nil {
		return Credits{}, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return Credits{}, fmt.Errorf("decode adobe credits response: %w", err)
	}
	total, _ := data["total"].(map[string]any)
	quota, _ := total["quota"].(map[string]any)
	return Credits{
		Total: int64Value(quota["total"]), Used: int64Value(quota["used"]), Available: int64Value(quota["available"]),
		AvailableUntil: stringValue(total["availableUntil"]), AccountID: info.AccountID,
	}, nil
}

func (c *Client) EstimateCost(ctx context.Context, token string, request CostRequest) (CostResult, error) {
	if len(request.Features) == 0 {
		return CostResult{}, errors.New("adobe cost features are required")
	}
	extra := make(http.Header)
	extra.Set("X-Request-Id", uuid.NewString())
	raw, _, err := c.do(ctx, http.MethodPost, c.CreditsCostURL, token, c.SubmitAPIKey, mustJSON(request), "application/json", extra)
	if err != nil {
		if fairUse, ok := fairUseCostResult(err); ok {
			return fairUse, nil
		}
		return CostResult{}, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return CostResult{}, fmt.Errorf("decode adobe cost response: %w", err)
	}
	credits, ok := sumCredits(data)
	if !ok {
		return CostResult{}, fmt.Errorf("adobe cost response did not include a valid non-negative credit cost: %s", truncate(raw))
	}
	return CostResult{Credits: credits, Raw: data}, nil
}

func fairUseCostResult(err error) (CostResult, bool) {
	var upstream *HTTPError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusUnprocessableEntity {
		return CostResult{}, false
	}
	var data map[string]any
	if json.Unmarshal([]byte(upstream.Body), &data) != nil || stringValue(data["error_code"]) != "chargeable_with_fair_use" {
		return CostResult{}, false
	}
	return CostResult{Credits: 0, Raw: data}, true
}

func (c *Client) RefreshAccessToken(ctx context.Context, cookieHeader string) (string, AccessTokenInfo, error) {
	cookieHeader = strings.TrimSpace(cookieHeader)
	if cookieHeader == "" {
		return "", AccessTokenInfo{}, errors.New("adobe cookie is required")
	}
	form := url.Values{
		"client_id":     {c.SubmitAPIKey},
		"guest_allowed": {"true"},
		"scope":         {DefaultRefreshScope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.RefreshURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", AccessTokenInfo{}, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Origin", "https://firefly.adobe.com")
	req.Header.Set("Referer", "https://firefly.adobe.com/")
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", AccessTokenInfo{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", AccessTokenInfo{}, err
	}
	if resp.StatusCode/100 != 2 {
		return "", AccessTokenInfo{}, &HTTPError{Status: resp.StatusCode, Body: truncate(raw)}
	}
	var response map[string]any
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", AccessTokenInfo{}, fmt.Errorf("decode adobe refresh response: %w", err)
	}
	token := stringValue(response["access_token"])
	if token == "" {
		return "", AccessTokenInfo{}, errors.New("adobe refresh response has no access_token")
	}
	info, err := ParseAccessToken(token)
	return token, info, err
}

func ParseAccessToken(token string) (AccessTokenInfo, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return AccessTokenInfo{}, errors.New("adobe access token must be a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessTokenInfo{}, errors.New("adobe access token payload is invalid")
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return AccessTokenInfo{}, errors.New("adobe access token claims are invalid")
	}
	accountID := firstString(claims, "user_id", "aa_id", "sub")
	if accountID == "" {
		return AccessTokenInfo{}, errors.New("adobe access token has no account id")
	}
	createdMS := int64Value(claims["created_at"])
	expiresMS := int64Value(claims["expires_in"])
	createdAt := time.UnixMilli(createdMS)
	expiresAt := createdAt.Add(time.Duration(expiresMS) * time.Millisecond)
	if exp := int64Value(claims["exp"]); exp > 0 {
		expiresAt = time.Unix(exp, 0)
	}
	if createdMS <= 0 || expiresAt.IsZero() || !expiresAt.After(createdAt) {
		return AccessTokenInfo{}, errors.New("adobe access token has no valid expiry")
	}
	scopes := strings.Split(firstString(claims, "scope"), ",")
	return AccessTokenInfo{AccountID: accountID, ClientID: firstString(claims, "client_id"), CreatedAt: createdAt, ExpiresAt: expiresAt, Scopes: scopes}, nil
}

func submitNonce(token, prompt string) string {
	info, err := ParseAccessToken(token)
	if err != nil || info.AccountID == "" || prompt == "" {
		return ""
	}
	runes := []rune(prompt)
	if len(runes) > 256 {
		runes = runes[:256]
	}
	digest := sha256.Sum256([]byte(info.AccountID + "-" + string(runes)))
	return hex.EncodeToString(digest[:])
}

func arpSessionID() string {
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	raw, _ := json.Marshal(map[string]string{
		"sid": uuid.NewString(),
		"ftr": fmt.Sprintf("%s_%d_%d_dUAL43-mnts-ants-d4_31ck__tt", hex.EncodeToString(random), time.Now().UnixMilli(), os.Getpid()),
	})
	return base64.StdEncoding.EncodeToString(raw)
}

func normalizeStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "COMPLETED", "COMPLETE", "SUCCEEDED", "SUCCESS", "DONE":
		return "COMPLETE"
	case "FAILED", "FAILURE", "ERROR", "CANCELLED", "CANCELED":
		return "FAILED"
	case "IN_PROGRESS", "RUNNING", "PROCESSING", "PENDING", "QUEUED", "STARTED":
		return "PENDING"
	default:
		return strings.ToUpper(strings.TrimSpace(status))
	}
}

func firstStatus(data map[string]any) string {
	if value := stringValue(data["status"]); value != "" {
		return value
	}
	if task, ok := data["task"].(map[string]any); ok {
		return stringValue(task["status"])
	}
	return ""
}

func parseOutputs(data map[string]any) []Output {
	var result []Output
	var walk func(any)
	walk = func(value any) {
		switch current := value.(type) {
		case map[string]any:
			for _, kind := range []string{"image", "video", "audio"} {
				if nested, ok := current[kind].(map[string]any); ok {
					url := firstString(nested, "presignedUrl", "url")
					if url != "" {
						result = append(result, Output{ID: firstString(nested, "id", "creativeCloudFileId"), URL: url, MediaType: kind, Width: intValue(nested["width"]), Height: intValue(nested["height"])})
						return
					}
				}
			}
			if url := firstString(current, "presignedUrl", "url"); url != "" {
				result = append(result, Output{ID: firstString(current, "id"), URL: url, MediaType: firstString(current, "mediaType")})
				return
			}
			for _, child := range current {
				walk(child)
			}
		case []any:
			for _, child := range current {
				walk(child)
			}
		}
	}
	if outputs, ok := data["outputs"]; ok {
		walk(outputs)
	}
	return result
}

func sumCredits(data map[string]any) (int64, bool) {
	features, ok := data["features"].(map[string]any)
	if !ok || len(features) == 0 {
		return 0, false
	}
	var total int64
	found := false
	for _, feature := range features {
		item, itemOK := feature.(map[string]any)
		cost, costOK := item["cost"].(map[string]any)
		rawCredits, creditsOK := cost["credits"]
		if !itemOK || !costOK || !creditsOK {
			continue
		}
		credits, valid := nonNegativeInteger(rawCredits)
		if !valid || credits > int64(^uint64(0)>>1)-total {
			return 0, false
		}
		total += credits
		found = true
	}
	return total, found
}

func nonNegativeInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed < 0 || math.Trunc(typed) != typed || typed >= float64(1<<63) {
			return 0, false
		}
		return int64(typed), true
	case int:
		if typed < 0 {
			return 0, false
		}
		return int64(typed), true
	case int64:
		return typed, typed >= 0
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil && parsed >= 0
	default:
		return 0, false
	}
}

func jobID(pollURL string) string {
	parsed, err := url.Parse(pollURL)
	if err != nil {
		return pollURL
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return pollURL
	}
	return parts[len(parts)-1]
}

func normalizeVideoPollURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.HasPrefix(parsed.Host, "firefly-epo") {
		return rawURL
	}
	hostSuffix := strings.TrimPrefix(parsed.Host, "firefly-epo")
	shard := strings.SplitN(hostSuffix, ".", 2)[0]
	if len(shard) < 4 {
		return rawURL
	}
	shard = shard[:4]
	if _, err := strconv.Atoi(shard); err != nil {
		return rawURL
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return rawURL
	}
	return "https://bks-epo" + shard + ".adobe.io/v2/jobs/result/" + parts[len(parts)-1] + "?host=" + url.QueryEscape(parsed.Host) + "/"
}

func nonNilMap(value, fallback map[string]any) map[string]any {
	if value == nil {
		return fallback
	}
	return value
}

func slicesToMap(values []string) []map[string]string {
	out := make([]map[string]string, 0, len(values))
	for _, value := range values {
		out = append(out, map[string]string{"id": value})
	}
	return out
}

func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if found := stringValue(value[key]); found != "" && found != "<nil>" {
			return found
		}
	}
	return ""
}

func intValue(value any) int { return int(int64Value(value)) }

func int64Value(value any) int64 {
	switch raw := value.(type) {
	case float64:
		return int64(raw)
	case json.Number:
		result, _ := raw.Int64()
		return result
	case string:
		result, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		return result
	case int64:
		return raw
	case int:
		return int64(raw)
	}
	return 0
}

func progressValue(data map[string]any) int {
	for _, key := range []string{"progress", "percentage", "percent", "task_progress"} {
		if value := intValue(data[key]); value > 0 {
			if value <= 1 {
				return value * 100
			}
			return value
		}
	}
	if task, ok := data["task"].(map[string]any); ok {
		return progressValue(task)
	}
	return 0
}

func progressHeader(headers http.Header) int {
	for _, key := range []string{"x-task-progress", "x-progress", "progress"} {
		if value := intValue(headers.Get(key)); value > 0 {
			return value
		}
	}
	return 0
}

func findID(value any) string {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range []string{"id", "assetId", "asset_id", "imageId", "videoId", "audioId"} {
			if id := stringValue(current[key]); id != "" && id != "<nil>" {
				return id
			}
		}
		for _, child := range current {
			if id := findID(child); id != "" {
				return id
			}
		}
	case []any:
		for _, child := range current {
			if id := findID(child); id != "" {
				return id
			}
		}
	}
	return ""
}

func findString(value any, keys ...string) string {
	if obj, ok := value.(map[string]any); ok {
		for _, key := range keys {
			if found := stringValue(obj[key]); found != "" && found != "<nil>" {
				return found
			}
		}
		for _, child := range obj {
			if found := findString(child, keys...); found != "" {
				return found
			}
		}
	}
	if list, ok := value.([]any); ok {
		for _, child := range list {
			if found := findString(child, keys...); found != "" {
				return found
			}
		}
	}
	return ""
}

func truncate(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 2048 {
		return text[:2048]
	}
	return text
}
