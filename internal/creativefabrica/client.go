package creativefabrica

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	DefaultGraphQLURL      = "https://graphql-gw.creativefabrica.com/query"
	DefaultJWTAuthURL      = "https://www.creativefabrica.com/cfsecure/jwtauth"
	DefaultFlowURL         = "https://flow-api.creativefabrica.com"
	DefaultMediaMatrixURL  = "https://studio-media-matrix.creativefabrica.com"
	DefaultCoinsURL        = "https://coins.creativefabrica.com"
	DefaultModalityURL     = "https://modality.creativefabrica.com"
	DefaultOrigin          = "https://studio.creativefabrica.com"
	flowServicePath        = "/creativefabrica.flow.v2.FlowService/"
	mediaMatrixServicePath = "/creativefabrica.studiomediamatrix.v1.StudioMediaMatrixService/"
	// Connect JSON uses the full protobuf enum name for ServiceType values.
	// The generated browser client exposes the short GoScript enum key
	// VIDEO_GENERATOR, but serializes it as SERVICE_TYPE_VIDEO_GENERATOR.
	videoServiceType = "SERVICE_TYPE_VIDEO_GENERATOR"
	coinsServicePath = "/creativefabrica.coins.v2.CoinsService/"
	// Preset owns the account-scoped AI model and pricing RPCs.  The host is
	// still the historical modality.creativefabrica.com origin, but the
	// Connect service package is creativefabrica.preset.v1.
	presetServicePath = "/creativefabrica.preset.v1.AIProviderService/"
)

type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("creativefabrica returned HTTP %d: %s", e.Status, e.Body)
}

type GraphQLErrorItem struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

type GraphQLError struct {
	Operation string
	Errors    []GraphQLErrorItem
}

func (e *GraphQLError) Error() string {
	if len(e.Errors) == 0 {
		return "creativefabrica graphql request failed"
	}
	message := strings.TrimSpace(e.Errors[0].Message)
	if message == "" {
		message = "request failed"
	}
	if len(e.Errors[0].Extensions) == 0 {
		return fmt.Sprintf("creativefabrica graphql %s: %s", e.Operation, message)
	}
	detail, _ := json.Marshal(e.Errors[0].Extensions)
	return fmt.Sprintf("creativefabrica graphql %s: %s: %s", e.Operation, message, detail)
}

type RPCError struct {
	Service string
	Method  string
	Status  int
	Code    string
	Message string
	Body    string
}

func (e *RPCError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "rpc request failed"
	}
	if e.Code != "" {
		message += " [" + e.Code + "]"
	}
	if e.Status > 0 {
		return fmt.Sprintf("creativefabrica %s.%s HTTP %d: %s", e.Service, e.Method, e.Status, message)
	}
	return fmt.Sprintf("creativefabrica %s.%s: %s", e.Service, e.Method, message)
}

type Client struct {
	HTTP      *http.Client
	UserAgent string
	// DeviceID mirrors the short browser identifier sent by Studio's
	// LoginInput.  Leave it empty to generate a fresh identifier per client.
	DeviceID string
	// CookieHeader is the short transport form derived from the complete
	// browser Cookie JSON. It is attached to first-party GraphQL/Connect calls
	// but never to signed object-storage uploads.
	CookieHeader   string
	GraphQLURL     string
	JWTAuthURL     string
	FlowURL        string
	MediaMatrixURL string
	CoinsURL       string
	ModalityURL    string
	RequestTimeout time.Duration
	PollInterval   time.Duration
}

type TokenInfo struct {
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
	Opaque    bool
}

type TokenSet struct {
	SessionToken string
	AccessToken  string
	RPCToken     string
	SessionInfo  TokenInfo
	AccessInfo   TokenInfo
	RPCInfo      TokenInfo
	CookieHeader string
	CookieJSON   CookieJSON
}

type OTPChallenge struct {
	Type    string `json:"type,omitempty"`
	OTP     string `json:"otp,omitempty"`
	Message string `json:"message,omitempty"`
}

type LoginResult struct {
	SessionToken string
	User         Profile
	Challenge    *OTPChallenge
	Errors       []LoginError
	Raw          map[string]any
	CookieJSON   CookieJSON
	CookieHeader string
}

type LoginError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type Profile struct {
	ID          string
	Email       string
	DisplayName string
	FirstName   string
	LastName    string
	Raw         map[string]any
}

type Coins struct {
	Balance   int64
	Available int64
	Used      int64
	Raw       map[string]any
}

// ErrBalanceUnavailable indicates that the upstream balance RPC completed
// successfully but returned an empty envelope. Creative Fabrica currently
// does this for some valid Studio sessions; callers may preserve the last
// locally calibrated balance while still accepting the refreshed session.
var ErrBalanceUnavailable = errors.New("Creative Fabrica balance is unavailable")

// PricingMedia describes media that a model's pricing calculator may charge
// for in addition to the generation itself. The upstream catalog has added
// fields to this object over time, so Raw is retained alongside stable fields.
type PricingMedia struct {
	Role             string         `json:"role,omitempty"`
	RequiredMetadata []string       `json:"required_metadata,omitempty"`
	Raw              map[string]any `json:"raw,omitempty"`
}

type Model struct {
	ID                   string         `json:"id"`
	PublicName           string         `json:"public_name,omitempty"`
	DisplayName          string         `json:"display_name,omitempty"`
	Kind                 string         `json:"kind,omitempty"`
	Provider             string         `json:"provider,omitempty"`
	InputOptions         map[string]any `json:"input_options,omitempty"`
	InputRoleConstraints map[string]any `json:"input_role_constraints,omitempty"`
	InputConstraintRules map[string]any `json:"input_constraint_rules,omitempty"`
	PricingInputs        []string       `json:"pricing_inputs,omitempty"`
	PricingInputConfig   map[string]any `json:"pricing_input_config,omitempty"`
	PricingInputMedia    []PricingMedia `json:"pricing_input_media,omitempty"`
	DefaultCoinConfig    map[string]any `json:"default_coin_config,omitempty"`
	PricingModelID       string         `json:"pricing_model_id,omitempty"`
	StaticCoinAmount     int64          `json:"static_coin_amount,omitempty"`
	HasStaticCoinAmount  bool           `json:"has_static_coin_amount,omitempty"`
	MaxPromptLength      int            `json:"max_prompt_length,omitempty"`
	Raw                  map[string]any `json:"raw,omitempty"`
}

type UploadURL struct {
	ID        string
	URL       string
	Method    string
	Headers   map[string]string
	ExpiresAt *time.Time
}

type Output struct {
	ID        string
	URL       string
	MediaType string
	Width     int
	Height    int
	Duration  int
}

type PollResult struct {
	Status   string
	Progress int
	Outputs  []Output
	Error    string
	Raw      map[string]any
}

type Job struct {
	ID        string
	FlowID    string
	SessionID string
	PollID    string
}

func New(proxyURL, userAgent string) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 30
	transport.IdleConnTimeout = 90 * time.Second
	if strings.TrimSpace(proxyURL) != "" {
		u, err := url.Parse(strings.TrimSpace(proxyURL))
		if err != nil {
			return nil, fmt.Errorf("parse Creative Fabrica proxy: %w", err)
		}
		switch u.Scheme {
		case "http", "https":
			transport.Proxy = http.ProxyURL(u)
		case "socks5", "socks5h":
			var auth *proxy.Auth
			if u.User != nil {
				password, _ := u.User.Password()
				auth = &proxy.Auth{User: u.User.Username(), Password: password}
			}
			dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
			if err != nil {
				return nil, fmt.Errorf("create Creative Fabrica socks proxy: %w", err)
			}
			transport.Proxy = nil
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialer.Dial(network, address)
			}
		default:
			return nil, fmt.Errorf("unsupported Creative Fabrica proxy scheme %q", u.Scheme)
		}
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "Mozilla/5.0"
	}
	return &Client{
		HTTP: transportClient(transport), UserAgent: userAgent,
		GraphQLURL: DefaultGraphQLURL, JWTAuthURL: DefaultJWTAuthURL,
		FlowURL: DefaultFlowURL, MediaMatrixURL: DefaultMediaMatrixURL,
		CoinsURL: DefaultCoinsURL, ModalityURL: DefaultModalityURL,
		RequestTimeout: 60 * time.Second, PollInterval: 3 * time.Second,
	}, nil
}

func transportClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: transport, Timeout: 60 * time.Second}
}

func NewWithHTTPClient(httpClient *http.Client, userAgent string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "Mozilla/5.0"
	}
	return &Client{
		HTTP: httpClient, UserAgent: userAgent,
		GraphQLURL: DefaultGraphQLURL, JWTAuthURL: DefaultJWTAuthURL,
		FlowURL: DefaultFlowURL, MediaMatrixURL: DefaultMediaMatrixURL,
		CoinsURL: DefaultCoinsURL, ModalityURL: DefaultModalityURL,
		RequestTimeout: 60 * time.Second, PollInterval: 3 * time.Second,
	}
}

func (c *Client) do(ctx context.Context, method, endpoint, bearer, cookie string, body []byte, contentType string, extra http.Header) ([]byte, http.Header, error) {
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
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", DefaultOrigin)
	req.Header.Set("Referer", DefaultOrigin+"/")
	req.Header.Set("User-Agent", c.UserAgent)
	if strings.TrimSpace(bearer) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
	}
	if strings.TrimSpace(cookie) != "" {
		req.Header.Set("Cookie", cookie)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
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
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if readErr != nil {
		return nil, resp.Header, readErr
	}
	if resp.StatusCode/100 != 2 {
		return raw, resp.Header, &HTTPError{Status: resp.StatusCode, Body: truncate(raw)}
	}
	return raw, resp.Header, nil
}

func (c *Client) graphqlEndpoint(operation string) string {
	base := strings.TrimRight(c.GraphQLURL, "/")
	if strings.TrimSpace(operation) == "" {
		return base
	}
	return base + "/" + url.PathEscape(operation)
}

func (c *Client) GraphQL(ctx context.Context, operation, query string, variables any, bearer, cookie string) (map[string]any, error) {
	if strings.TrimSpace(cookie) == "" {
		cookie = c.CookieHeader
	}
	data, _, err := c.graphqlRequest(ctx, operation, query, variables, bearer, cookie)
	return data, err
}

func (c *Client) graphqlRequest(ctx context.Context, operation, query string, variables any, bearer, cookie string) (map[string]any, http.Header, error) {
	if strings.TrimSpace(cookie) == "" {
		cookie = c.CookieHeader
	}
	body, err := json.Marshal(struct {
		Query     string `json:"query"`
		Variables any    `json:"variables,omitempty"`
	}{Query: query, Variables: variables})
	if err != nil {
		return nil, nil, err
	}
	raw, headers, err := c.do(ctx, http.MethodPost, c.graphqlEndpoint(operation), bearer, cookie, body, "application/json", http.Header{"Accept": []string{"application/json, multipart/mixed"}})
	if err != nil {
		return nil, nil, err
	}
	var envelope struct {
		Data   json.RawMessage    `json:"data"`
		Errors []GraphQLErrorItem `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, nil, fmt.Errorf("decode Creative Fabrica graphql response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return nil, nil, &GraphQLError{Operation: operation, Errors: envelope.Errors}
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, nil, fmt.Errorf("creativefabrica graphql %s returned no data", operation)
	}
	var data map[string]any
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, nil, fmt.Errorf("decode Creative Fabrica graphql data: %w", err)
	}
	return data, headers, nil
}

const loginQuery = `mutation logIn($input: LoginInput!) {
  login(input: $input) {
    ...LoginResponseFields
    challenge { type otp }
    errors { code message }
  }
}
fragment UserFields on User {
  id email firstName lastName
}
fragment LoginResponseFields on LoginResponse {
  token
  user { ...UserFields }
}`

const creativeFabricaDeviceAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func newCreativeFabricaDeviceID() string {
	const length = 10
	buf := make([]byte, length)
	if _, err := cryptorand.Read(buf); err != nil {
		// The identifier is not an authentication secret. Keep a deterministic
		// fallback for restricted entropy environments while preserving Studio's
		// short alphanumeric shape.
		fallback := fmt.Sprintf("%x", time.Now().UnixNano())
		if len(fallback) >= length {
			return fallback[:length]
		}
		return (fallback + "0000000000")[:length]
	}
	for i, value := range buf {
		buf[i] = creativeFabricaDeviceAlphabet[int(value)%len(creativeFabricaDeviceAlphabet)]
	}
	return string(buf)
}

func (c *Client) loginDeviceID() string {
	if id := strings.TrimSpace(c.DeviceID); id != "" {
		return id
	}
	return newCreativeFabricaDeviceID()
}

func (c *Client) Login(ctx context.Context, email, password string) (LoginResult, error) {
	return c.LoginWithOTP(ctx, email, password, "")
}

// LoginWithOTP uses the same LoginInput mutation as the Studio frontend. The
// otp field is only sent when supplied, keeping the initial password request
// compatible with accounts that do not require email verification.
func (c *Client) LoginWithOTP(ctx context.Context, email, password, otp string) (LoginResult, error) {
	if strings.TrimSpace(email) == "" || password == "" {
		return LoginResult{}, errors.New("Creative Fabrica email and password are required")
	}
	input := map[string]any{
		"email":    strings.TrimSpace(email),
		"deviceId": c.loginDeviceID(),
		"password": password,
		"remember": false,
	}
	if strings.TrimSpace(otp) != "" {
		input["otp"] = strings.TrimSpace(otp)
	}
	data, headers, err := c.graphqlRequest(ctx, "logIn", loginQuery, map[string]any{"input": input}, "", "")
	if err != nil {
		return LoginResult{}, err
	}
	login, _ := data["login"].(map[string]any)
	result := LoginResult{Raw: login}
	result.CookieJSON, result.CookieHeader = cookieJSONFromHeaders(headers, c.GraphQLURL)
	result.SessionToken = firstString(login, "token", "sessionToken", "session_token")
	if user, ok := login["user"].(map[string]any); ok {
		result.User = parseProfile(user)
	}
	if challenge, ok := login["challenge"].(map[string]any); ok && len(challenge) > 0 {
		result.Challenge = &OTPChallenge{Type: stringValue(challenge["type"]), OTP: stringValue(challenge["otp"])}
	}
	if rawErrors, ok := login["errors"].([]any); ok {
		for _, rawError := range rawErrors {
			if item, ok := rawError.(map[string]any); ok {
				result.Errors = append(result.Errors, LoginError{Code: stringValue(item["code"]), Message: stringValue(item["message"])})
			}
		}
	}
	if len(result.Errors) > 0 && result.SessionToken == "" {
		if result.Challenge == nil {
			return result, fmt.Errorf("creativefabrica login rejected: %s", result.Errors[0].Message)
		}
	}
	if result.SessionToken == "" {
		if result.Challenge != nil {
			return result, nil
		}
		return result, errors.New("Creative Fabrica login response has no session token")
	}
	return result, nil
}

func (c *Client) UserToken(ctx context.Context, cookieHeader, sessionToken string) (string, error) {
	data, err := c.GraphQL(ctx, "UserToken", `query UserToken { me { token } }`, nil, sessionToken, cookieHeader)
	if err != nil {
		return "", err
	}
	me, _ := data["me"].(map[string]any)
	token := firstString(me, "token", "sessionToken", "session_token")
	if token == "" {
		return "", errors.New("Creative Fabrica UserToken response has no token")
	}
	return token, nil
}

func (c *Client) ExchangeSessionToken(ctx context.Context, sessionToken string) (TokenSet, error) {
	return c.ExchangeSessionTokenWithCookie(ctx, sessionToken, "")
}

// ExchangeSessionTokenWithCookie mirrors the Studio jwtauth request while
// preserving the browser session cookies that established the session token.
// The response may only set one or two incremental cookies, so callers must
// merge TokenSet.CookieJSON with their stored complete cookie export.
func (c *Client) ExchangeSessionTokenWithCookie(ctx context.Context, sessionToken, cookieHeader string) (TokenSet, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return TokenSet{}, errors.New("Creative Fabrica session token is required")
	}
	if strings.TrimSpace(cookieHeader) == "" {
		cookieHeader = c.CookieHeader
	}
	body, _ := json.Marshal(map[string]string{"token": sessionToken, "origin": "https://studio.creativefabrica.com"})
	raw, headers, err := c.do(ctx, http.MethodPost, c.JWTAuthURL, "", cookieHeader, body, "application/json", http.Header{"Accept": []string{"application/json, multipart/mixed"}})
	if err != nil {
		return TokenSet{}, err
	}
	var response any
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &response); err != nil {
			return TokenSet{}, fmt.Errorf("decode Creative Fabrica jwtauth response: %w", err)
		}
	}
	set := TokenSet{SessionToken: sessionToken, SessionInfo: ParseTokenInfo(sessionToken)}
	set.AccessToken = firstToken(response, "access_token", "accessToken", "rpc_token", "rpcToken", "token", "jwt")
	if set.AccessToken == "" {
		set.AccessToken = tokenFromSetCookie(headers, "cfToken")
	}
	if set.AccessToken == "" {
		// The current Studio frontend treats jwtauth as a cookie-establishing
		// request and uses the UserToken value itself for Connect RPC calls.
		// Therefore an empty successful body is valid.
		set.AccessToken = sessionToken
	}
	set.RPCToken = set.AccessToken
	set.AccessInfo = ParseTokenInfo(set.AccessToken)
	set.RPCInfo = set.AccessInfo
	set.CookieJSON, set.CookieHeader = cookieJSONFromHeaders(headers, c.JWTAuthURL)
	return set, nil
}

func tokenFromSetCookie(headers http.Header, name string) string {
	if len(headers) == 0 || strings.TrimSpace(name) == "" {
		return ""
	}
	response := http.Response{Header: headers}
	for _, cookie := range response.Cookies() {
		if strings.EqualFold(cookie.Name, name) && strings.TrimSpace(cookie.Value) != "" {
			return strings.TrimSpace(cookie.Value)
		}
	}
	return ""
}

func cookieJSONFromHeaders(headers http.Header, endpoint string) (CookieJSON, string) {
	if len(headers.Values("Set-Cookie")) == 0 {
		return nil, ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, ""
	}
	response := http.Response{Header: headers, Request: &http.Request{URL: parsed}}
	items := make([]map[string]any, 0)
	for _, cookie := range response.Cookies() {
		domain := strings.TrimSpace(cookie.Domain)
		if domain == "" {
			domain = parsed.Hostname()
		}
		item := map[string]any{
			"name": cookie.Name, "value": cookie.Value, "domain": domain,
			"path": cookie.Path, "secure": cookie.Secure, "httpOnly": cookie.HttpOnly,
		}
		if cookie.Expires.Unix() > 0 {
			item["expirationDate"] = cookie.Expires.Unix()
		}
		switch cookie.SameSite {
		case http.SameSiteStrictMode:
			item["sameSite"] = "Strict"
		case http.SameSiteLaxMode:
			item["sameSite"] = "Lax"
		case http.SameSiteNoneMode:
			item["sameSite"] = "None"
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, ""
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, ""
	}
	normalized, err := NormalizeCookieJSON(raw)
	if err != nil {
		return nil, ""
	}
	header, err := CookieHeaderFromJSON(normalized)
	if err != nil {
		return nil, ""
	}
	return normalized, header
}

func (c *Client) Authenticate(ctx context.Context, cookieHeader, sessionToken, accessToken string) (TokenSet, error) {
	if strings.TrimSpace(accessToken) != "" {
		info := ParseTokenInfo(accessToken)
		return TokenSet{SessionToken: sessionToken, AccessToken: accessToken, RPCToken: accessToken, SessionInfo: ParseTokenInfo(sessionToken), AccessInfo: info, RPCInfo: info}, nil
	}
	if strings.TrimSpace(sessionToken) == "" {
		var err error
		sessionToken, err = c.UserToken(ctx, cookieHeader, "")
		if err != nil {
			return TokenSet{}, err
		}
	}
	return c.ExchangeSessionTokenWithCookie(ctx, sessionToken, cookieHeader)
}

func ParseTokenInfo(token string) TokenInfo {
	token = strings.TrimSpace(token)
	if token == "" {
		return TokenInfo{}
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return TokenInfo{Token: token, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Minute), Opaque: true}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return TokenInfo{Token: token, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Minute), Opaque: true}
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return TokenInfo{Token: token, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Minute), Opaque: true}
	}
	now := time.Now()
	created := unixClaim(claims, "iat", "created_at", "createdAt")
	expires := unixClaim(claims, "exp", "expires_at", "expiresAt")
	if expires == 0 {
		if seconds := numberClaim(claims, "expires_in", "expiresIn"); seconds > 0 {
			base := now
			if created > 0 {
				base = time.Unix(created, 0)
			}
			expires = base.Add(time.Duration(seconds) * time.Second).Unix()
		}
	}
	createdAt := now
	if created > 0 {
		createdAt = time.Unix(created, 0)
	}
	expiresAt := now.Add(30 * time.Minute)
	if expires > 0 {
		expiresAt = time.Unix(expires, 0)
	}
	return TokenInfo{Token: token, CreatedAt: createdAt, ExpiresAt: expiresAt}
}

func (c *Client) Profile(ctx context.Context, token string) (Profile, error) {
	query := `query user { me { token user { id email firstName lastName } } }`
	data, err := c.GraphQL(ctx, "user", query, nil, token, "")
	if err != nil {
		return Profile{}, err
	}
	me, _ := data["me"].(map[string]any)
	if user, ok := me["user"].(map[string]any); ok {
		return parseProfile(user), nil
	}
	return parseProfile(me), nil
}

func (c *Client) Coins(ctx context.Context, token string) (Coins, error) {
	data, err := c.RPC(ctx, "CoinsService", "GetBalance", c.CoinsURL, coinsServicePath, token, map[string]any{})
	if err != nil {
		return Coins{}, err
	}
	balance, balanceFound := firstPresentInt(data, "balance", "available", "coinBalance", "coin_balance")
	available, availableFound := firstPresentInt(data, "available", "balance", "coinBalance", "coin_balance")
	used, usedFound := firstPresentInt(data, "used", "spent")
	if !balanceFound && !availableFound && !usedFound {
		if found, value := findNumber(data, map[string]struct{}{"balance": {}, "available": {}, "coins": {}}); found {
			balance = value
			available = value
			balanceFound = true
			availableFound = true
		}
	}
	if !balanceFound && !availableFound {
		return Coins{Raw: data}, ErrBalanceUnavailable
	}
	if !balanceFound {
		balance = available
	}
	if !availableFound {
		available = balance
	}
	return Coins{Balance: balance, Available: available, Used: used, Raw: data}, nil
}

func (c *Client) ListModels(ctx context.Context, token, kind string) ([]Model, error) {
	var base, path string
	service := "FlowService"
	method := "ListModels"
	if strings.EqualFold(kind, "video") {
		base, path, service = c.MediaMatrixURL, mediaMatrixServicePath, "StudioMediaMatrixService"
		// ListModels is account/catalog scoped.  An empty request is accepted by
		// some older deployments but returns the wrong (non-video) catalog on
		// current Studio; always send the explicit service type.
		request := map[string]any{"serviceType": videoServiceType}
		data, err := c.RPC(ctx, service, method, base, path, token, request)
		if err != nil {
			return nil, err
		}
		models := parseModels(data, kind)
		if len(models) == 0 {
			return nil, errors.New("Creative Fabrica model catalog response is empty")
		}
		return models, nil
	} else {
		base, path = c.FlowURL, flowServicePath
	}
	data, err := c.RPC(ctx, service, method, base, path, token, map[string]any{})
	if err != nil {
		return nil, err
	}
	models := parseModels(data, kind)
	if len(models) == 0 {
		return nil, errors.New("Creative Fabrica model catalog response is empty")
	}
	return models, nil
}

func (c *Client) CalculateGenerationCost(ctx context.Context, token, model string, options map[string]any, inputMedia []any) (int64, map[string]any, error) {
	request := map[string]any{"model": model, "options": encodeOptions(options)}
	if len(inputMedia) > 0 {
		request["inputMedia"] = inputMedia
	}
	data, err := c.RPC(ctx, "AIProviderService", "CalculateModelCost", c.ModalityURL, presetServicePath, token, request)
	if err != nil {
		return 0, nil, err
	}
	coins, found := firstPresentInt(data, "coinAmount", "coin_amount", "coins", "cost", "price")
	if !found {
		found, coins = findNumber(data, map[string]struct{}{"coinAmount": {}, "coin_amount": {}, "coins": {}, "cost": {}, "price": {}})
	}
	if !found {
		return 0, data, errors.New("Creative Fabrica cost response has no coin amount")
	}
	if coins < 0 {
		return 0, data, errors.New("Creative Fabrica cost response contains a negative coin amount")
	}
	return coins, data, nil
}

func (c *Client) CreateFlow(ctx context.Context, token string, request map[string]any) (Job, map[string]any, error) {
	data, err := c.RPC(ctx, "FlowService", "CreateFlow", c.FlowURL, flowServicePath, token, request)
	if err != nil {
		return Job{}, nil, err
	}
	job := parseJob(data)
	if job.FlowID == "" {
		job.FlowID = job.ID
	}
	if job.ID == "" {
		job.ID = job.FlowID
	}
	if job.ID == "" {
		return Job{}, data, errors.New("Creative Fabrica CreateFlow response has no flow id")
	}
	return job, data, nil
}

func (c *Client) GetFlow(ctx context.Context, token, flowID string) (PollResult, error) {
	data, err := c.RPC(ctx, "FlowService", "GetFlow", c.FlowURL, flowServicePath, token, map[string]any{"id": flowID})
	if err != nil {
		return PollResult{}, err
	}
	return parseFlowPollResult(data), nil
}

func (c *Client) CreateUploadURL(ctx context.Context, token, filename, mediaType string, size int64) (UploadURL, error) {
	// FlowService.CreateUploadUrl's descriptor exposes upload_file as a string
	// (the browser sends the original filename).  The signed URL response
	// contains the upload URL and any required headers.
	request := map[string]any{"uploadFile": filename}
	data, err := c.RPC(ctx, "FlowService", "CreateUploadUrl", c.FlowURL, flowServicePath, token, request)
	if err != nil {
		return UploadURL{}, err
	}
	upload := parseUploadURL(data)
	if upload.URL == "" {
		return UploadURL{}, errors.New("Creative Fabrica CreateUploadUrl response has no URL")
	}
	return upload, nil
}

func (c *Client) UploadToURL(ctx context.Context, upload UploadURL, mediaType string, data []byte) error {
	method := strings.ToUpper(strings.TrimSpace(upload.Method))
	if method == "" {
		method = http.MethodPut
	}
	extra := make(http.Header)
	for key, value := range upload.Headers {
		extra.Set(key, value)
	}
	_, _, err := c.do(ctx, method, upload.URL, "", "", data, mediaType, extra)
	return err
}

// UploadVideoFrameToURL uploads a frame using the signed URL returned by
// StudioMediaMatrixService.InitiateSession. Object-storage uploads must not
// receive the Studio Cookie or RPC bearer; the browser only sends the media
// Content-Type header.
func (c *Client) UploadVideoFrameToURL(ctx context.Context, uploadURL, mediaType string, data []byte) error {
	if strings.TrimSpace(uploadURL) == "" {
		return errors.New("Creative Fabrica video frame upload URL is empty")
	}
	if strings.TrimSpace(mediaType) == "" {
		mediaType = "application/octet-stream"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mediaType)
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return &HTTPError{Status: resp.StatusCode, Body: truncate(raw)}
	}
	return nil
}

func (c *Client) InitiateSession(ctx context.Context, token string, request map[string]any) (Job, map[string]any, error) {
	data, err := c.RPC(ctx, "StudioMediaMatrixService", "InitiateSession", c.MediaMatrixURL, mediaMatrixServicePath, token, request)
	if err != nil {
		return Job{}, nil, err
	}
	job := parseJob(data)
	if job.SessionID == "" {
		job.SessionID = job.ID
	}
	if job.ID == "" {
		job.ID = job.SessionID
	}
	if job.ID == "" {
		return Job{}, data, errors.New("Creative Fabrica InitiateSession response has no session id")
	}
	return job, data, nil
}

// VideoFrameUploadURLs extracts the ordered signed object-storage URLs from
// an InitiateSession response. The response-side frames are distinct from the
// request-side VideoFrameRequest values and may be nested under either camel
// case or snake case Connect JSON fields.
func VideoFrameUploadURLs(data map[string]any) ([]string, error) {
	urls := make([]string, 0)
	seen := make(map[string]struct{})
	var walk func(any)
	walk = func(value any) {
		switch current := value.(type) {
		case map[string]any:
			for key, child := range current {
				if strings.EqualFold(key, "frames") {
					if items, ok := child.([]any); ok {
						for _, raw := range items {
							frame, ok := raw.(map[string]any)
							if !ok {
								continue
							}
							candidate := firstString(frame, "url", "uploadUrl", "upload_url", "signedUrl", "signed_url")
							if strings.HasPrefix(candidate, "http") {
								if _, exists := seen[candidate]; !exists {
									seen[candidate] = struct{}{}
									urls = append(urls, candidate)
								}
							}
						}
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range current {
				walk(child)
			}
		}
	}
	walk(data)
	if len(urls) == 0 {
		return nil, nil
	}
	return urls, nil
}

func (c *Client) IterateSession(ctx context.Context, token, sessionID string, request map[string]any) (PollResult, error) {
	payload := cloneMap(request)
	if _, ok := payload["sessionId"]; !ok {
		payload["sessionId"] = sessionID
	}
	data, err := c.RPC(ctx, "StudioMediaMatrixService", "IterateSession", c.MediaMatrixURL, mediaMatrixServicePath, token, payload)
	if err != nil {
		return PollResult{}, err
	}
	return parseSessionPollResult(data), nil
}

func (c *Client) GetSession(ctx context.Context, token, sessionID string) (PollResult, error) {
	data, err := c.RPC(ctx, "StudioMediaMatrixService", "GetSession", c.MediaMatrixURL, mediaMatrixServicePath, token, map[string]any{"serviceType": videoServiceType, "sessionId": sessionID})
	if err != nil {
		return PollResult{}, err
	}
	return parseSessionPollResult(data), nil
}

func (c *Client) GetSessionMedia(ctx context.Context, token, sessionID string) (PollResult, error) {
	data, err := c.RPC(ctx, "StudioMediaMatrixService", "GetSessionMedia", c.MediaMatrixURL, mediaMatrixServicePath, token, map[string]any{"serviceType": videoServiceType, "sessionId": sessionID})
	if err != nil {
		return PollResult{}, err
	}
	return parseSessionPollResult(data), nil
}

func (c *Client) GetSessionIterations(ctx context.Context, token, sessionID string) (PollResult, error) {
	data, err := c.RPC(ctx, "StudioMediaMatrixService", "GetSessionIterations", c.MediaMatrixURL, mediaMatrixServicePath, token, map[string]any{"serviceType": videoServiceType, "sessionId": sessionID})
	if err != nil {
		return PollResult{}, err
	}
	return parseSessionPollResult(data), nil
}

func (c *Client) GetSessionInputMedia(ctx context.Context, token, sessionID string) (map[string]any, error) {
	return c.RPC(ctx, "StudioMediaMatrixService", "GetSessionInputMedia", c.MediaMatrixURL, mediaMatrixServicePath, token, map[string]any{"serviceType": videoServiceType, "sessionId": sessionID})
}

func (c *Client) RPC(ctx context.Context, service, method, baseURL, servicePath, token string, request map[string]any) (map[string]any, error) {
	return c.RPCWithCookie(ctx, service, method, baseURL, servicePath, token, c.CookieHeader, request)
}

// RPCWithCookie is useful for signed upload/session calls where the browser
// sends both the short-lived RPC bearer and the complete Studio cookie jar.
func (c *Client) RPCWithCookie(ctx context.Context, service, method, baseURL, servicePath, token, cookie string, request map[string]any) (map[string]any, error) {
	endpoint := strings.TrimRight(baseURL, "/") + servicePath + url.PathEscape(method)
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	extra := make(http.Header)
	extra.Set("connect-protocol-version", "1")
	raw, _, err := c.do(ctx, http.MethodPost, endpoint, token, cookie, body, "application/json", extra)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("decode Creative Fabrica %s.%s response: %w", service, method, err)
	}
	if message := findStringRecursive(data, "error", "message", "errorMessage"); message != "" && looksLikeError(data) {
		return nil, &RPCError{Service: service, Method: method, Message: message, Body: truncate(raw)}
	}
	return data, nil
}

func parseProfile(data map[string]any) Profile {
	return Profile{ID: firstString(data, "id", "userId", "user_id"), Email: firstString(data, "email"), DisplayName: firstString(data, "displayName", "display_name", "name"), FirstName: firstString(data, "firstName", "first_name"), LastName: firstString(data, "lastName", "last_name"), Raw: data}
}

func parseModels(data map[string]any, kind string) []Model {
	var result []Model
	seen := make(map[string]struct{})
	add := func(model Model, ok bool) {
		if !ok {
			return
		}
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" {
			return
		}
		key := strings.ToLower(model.ID)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, model)
	}
	var walk func(any)
	walk = func(value any) {
		switch current := value.(type) {
		case map[string]any:
			if catalog := mapValue(current, "catalog"); catalog != nil {
				add(parseCreativeFabricaCatalogModel(catalog, current, kind))
			} else {
				add(parseCreativeFabricaDirectModel(current, kind))
			}
			for key, child := range current {
				// A CatalogModel is parsed together with its parent ModelEntry so
				// the generation enum and capability metadata remain associated.
				if key == "catalog" {
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range current {
				walk(child)
			}
		}
	}
	walk(data)
	return result
}

func parseCreativeFabricaCatalogModel(catalog, entry map[string]any, kind string) (Model, bool) {
	capabilities := mapValue(catalog, "capabilities")
	if capabilities == nil {
		capabilities = catalog
	}
	id := firstString(capabilities, "model", "modelId", "model_id", "id")
	if id == "" {
		id = firstString(catalog, "model", "modelId", "model_id", "id")
	}
	if id == "" {
		id = modelEntryIdentifier(entry, kind)
	}
	if id == "" {
		return Model{}, false
	}
	publicName := firstString(catalog, "publicModelName", "public_model_name", "publicName", "public_name")
	displayName := firstString(catalog, "displayName", "display_name", "name")
	if displayName == "" {
		displayName = publicName
	}
	if displayName == "" {
		displayName = id
	}
	raw := cloneMap(catalog)
	if generationModel := modelEntryGenerationIdentifier(entry, kind); generationModel != "" {
		raw["generationModel"] = generationModel
	}
	if modelCase, modelValue := modelEntryOneof(entry); modelCase != "" {
		raw["modelCase"] = modelCase
		if _, exists := raw["generationModel"]; !exists && modelValue != "" {
			raw["generationModel"] = modelValue
		}
	}
	model := Model{
		ID: id, PublicName: publicName, DisplayName: displayName, Kind: kind,
		Provider: firstString(catalog, "provider"), PricingModelID: id, Raw: raw,
		InputOptions:         mapValue(capabilities, "inputOptions", "input_options"),
		InputRoleConstraints: mapValue(capabilities, "inputRoleConstraints", "input_role_constraints"),
		InputConstraintRules: mapValue(capabilities, "inputConstraintRules", "input_constraint_rules"),
		DefaultCoinConfig:    mapValue(capabilities, "defaultCoinConfig", "default_coin_config"),
	}
	if model.Provider == "" {
		model.Provider = firstString(entry, "provider")
	}
	model.MaxPromptLength = firstPositiveInt(capabilities, "maxPromptLength", "max_prompt_length")
	applyCreativeFabricaPricingMetadata(&model, firstValue(capabilities, "pricingInputs", "pricing_inputs"))
	return model, true
}

func parseCreativeFabricaDirectModel(current map[string]any, kind string) (Model, bool) {
	capabilities := mapValue(current, "capabilities")
	if capabilities != nil && firstString(capabilities, "model", "modelId", "model_id") != "" {
		return parseCreativeFabricaCatalogModel(current, current, kind)
	}
	id := firstString(current, "id", "model", "modelId", "model_id")
	if id == "" {
		id = modelEntryIdentifier(current, kind)
	}
	name := firstString(current, "publicModelName", "public_model_name", "publicName", "public_name", "displayName", "display_name", "name")
	if id == "" || (name == "" && current["inputOptions"] == nil && current["input_options"] == nil && current["pricingInputs"] == nil && current["pricing_inputs"] == nil) {
		return Model{}, false
	}
	raw := cloneMap(current)
	if generationModel := modelEntryGenerationIdentifier(current, kind); generationModel != "" {
		raw["generationModel"] = generationModel
	}
	if modelCase, modelValue := modelEntryOneof(current); modelCase != "" {
		raw["modelCase"] = modelCase
		if _, exists := raw["generationModel"]; !exists && modelValue != "" {
			raw["generationModel"] = modelValue
		}
	}
	model := Model{
		ID: id, PublicName: firstString(current, "publicModelName", "public_model_name", "publicName", "public_name"),
		DisplayName: name, Kind: kind, Provider: firstString(current, "provider"), PricingModelID: id,
		InputOptions:         mapValue(current, "inputOptions", "input_options"),
		InputRoleConstraints: mapValue(current, "inputRoleConstraints", "input_role_constraints"),
		InputConstraintRules: mapValue(current, "inputConstraintRules", "input_constraint_rules"),
		DefaultCoinConfig:    mapValue(current, "defaultCoinConfig", "default_coin_config"),
		Raw:                  raw,
	}
	model.MaxPromptLength = firstPositiveInt(current, "maxPromptLength", "max_prompt_length")
	applyCreativeFabricaPricingMetadata(&model, firstValue(current, "pricingInputs", "pricing_inputs"))
	return model, true
}

// ModelEntry is a protobuf message with a JSON oneof. Connect JSON encodes
// the selected model as {"model":{"case":"videoGeneratorModel",
// "value":"VIDEO_GENERATOR_MODEL_..."}} rather than a scalar field. Keep
// this decoding tolerant of both the generated Connect shape and older
// releases that emitted the selected field directly.
func modelEntryOneof(entry map[string]any) (string, string) {
	if entry == nil {
		return "", ""
	}
	for _, key := range []string{"model", "model_oneof", "modelOneof"} {
		selected, ok := entry[key].(map[string]any)
		if !ok {
			continue
		}
		modelCase := firstString(selected, "case", "kind", "type")
		modelValue := firstString(selected, "value", "id", "name")
		if modelCase != "" || modelValue != "" {
			return modelCase, modelValue
		}
	}
	return "", ""
}

func modelEntryGenerationIdentifier(entry map[string]any, kind string) string {
	if entry == nil {
		return ""
	}
	keys := []string{"videoGeneratorModel", "video_generator_model"}
	if strings.EqualFold(kind, "image") {
		keys = []string{"imageGeneratorModel", "image_generator_model"}
	}
	if value := firstString(entry, keys...); value != "" {
		return value
	}
	modelCase, modelValue := modelEntryOneof(entry)
	if modelValue == "" {
		return ""
	}
	if strings.EqualFold(kind, "video") && strings.EqualFold(modelCase, "videoGeneratorModel") {
		return modelValue
	}
	if strings.EqualFold(kind, "image") && strings.EqualFold(modelCase, "imageGeneratorModel") {
		return modelValue
	}
	// Some Connect clients omit the case discriminator when serializing a
	// protobuf oneof. The requested media kind still gives us a useful fallback.
	if modelCase == "" {
		return modelValue
	}
	return ""
}

func modelEntryIdentifier(entry map[string]any, kind string) string {
	if value := modelEntryGenerationIdentifier(entry, kind); value != "" {
		return value
	}
	if _, value := modelEntryOneof(entry); value != "" {
		return value
	}
	return firstString(entry, "videoGeneratorModel", "video_generator_model", "imageGeneratorModel", "image_generator_model")
}

func applyCreativeFabricaPricingMetadata(model *Model, value any) {
	if model == nil {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		model.PricingInputConfig = cloneMap(typed)
		model.PricingInputs = stringSliceValue(firstValue(typed, "options"))
		if media, ok := firstValue(typed, "media").([]any); ok {
			for _, raw := range media {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				role := firstString(item, "role")
				if role == "" {
					continue
				}
				model.PricingInputMedia = append(model.PricingInputMedia, PricingMedia{
					Role: role, RequiredMetadata: stringSliceValue(firstValue(item, "requiredMetadata", "required_metadata")), Raw: cloneMap(item),
				})
			}
		}
	default:
		model.PricingInputs = stringSliceValue(typed)
	}
	if model.DefaultCoinConfig != nil {
		if amount, found := firstPresentInt(model.DefaultCoinConfig, "coinAmount", "coin_amount"); found {
			model.StaticCoinAmount = amount
			model.HasStaticCoinAmount = true
		}
	}
}

func firstPositiveInt(data map[string]any, keys ...string) int {
	for _, key := range keys {
		if value := intValue(data[key]); value > 0 {
			return value
		}
	}
	return 0
}

func firstValue(data map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			return value
		}
	}
	return nil
}

func stringSliceValue(value any) []string {
	switch values := value.(type) {
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text := stringValue(value); text != "" {
				result = append(result, text)
			}
		}
		return result
	case []string:
		return append([]string(nil), values...)
	}
	return nil
}

func parseJob(data map[string]any) Job {
	// Connect responses have changed envelope shapes across Studio releases.
	// Resolve the mutation's own identifiers first; a recursive scan can see an
	// unrelated nested media/user id and map it as the task id.
	flowID := firstString(data, "flowId", "flow_id")
	if flowID == "" {
		flowID = objectIdentifier(mapValue(data, "flow", "flowData", "flow_data"), "flowId", "flow_id", "id")
	}
	sessionID := firstString(data, "sessionId", "session_id")
	if sessionID == "" {
		sessionID = objectIdentifier(mapValue(data, "session", "sessionData", "session_data"), "sessionId", "session_id", "id")
	}
	topLevelID := firstString(data, "id")
	if flowID == "" {
		flowID = firstStringRecursive(data, "flowId", "flow_id")
	}
	if sessionID == "" {
		sessionID = firstStringRecursive(data, "sessionId", "session_id")
	}
	pollID := firstString(data, "iterationId", "iteration_id")
	if pollID == "" {
		pollID = objectIdentifier(mapValue(data, "iteration", "iterationData", "iteration_data"), "iterationId", "iteration_id", "id")
	}
	if pollID == "" {
		pollID = topLevelID
	}
	if pollID == "" {
		pollID = firstStringRecursive(data, "iterationId", "iteration_id")
	}
	id := flowID
	if id == "" {
		id = sessionID
	}
	if id == "" {
		id = topLevelID
	}
	if id == "" {
		id = firstStringRecursive(data, "flowId", "flow_id", "sessionId", "session_id", "id")
	}
	return Job{ID: id, FlowID: flowID, SessionID: sessionID, PollID: pollID}
}

func objectIdentifier(value map[string]any, keys ...string) string {
	if value == nil {
		return ""
	}
	return firstString(value, keys...)
}

func parsePollResult(data map[string]any) PollResult {
	status := normalizeStatus(firstStringRecursive(data, "status", "state", "flowStatus", "sessionStatus", "generationStatus"))
	progress := firstIntRecursive(data, "progress", "percent", "percentage")
	result := PollResult{Status: status, Progress: progress, Raw: data, Error: findStringRecursive(data, "errorMessage", "error_message", "message")}
	result.Outputs = parseOutputs(data)
	if result.Status == "" && len(result.Outputs) > 0 {
		result.Status = "COMPLETE"
	}
	return result
}

// parseFlowPollResult only accepts FlowImage fields.  A recursive URL scan is
// intentionally avoided here because Flow responses also contain input and
// thumbnail URLs which must never be returned as generated outputs.
func parseFlowPollResult(data map[string]any) PollResult {
	result := PollResult{Raw: data}
	flow := mapValue(data, "flow")
	if flow == nil {
		flow = data
	}
	result.Status = normalizeFlowStatus(firstStringRecursive(flow, "state", "status", "flowStatus"))
	result.Error = firstStringRecursive(flow, "errorMessage", "error_message")
	if iterations, ok := flow["iterations"].([]any); ok {
		for _, rawIteration := range iterations {
			iteration, ok := rawIteration.(map[string]any)
			if !ok {
				continue
			}
			images, _ := iteration["images"].([]any)
			for _, rawImage := range images {
				image, ok := rawImage.(map[string]any)
				if !ok {
					continue
				}
				state := normalizeFlowStatus(firstString(image, "state", "status"))
				if state == "FAILED" && result.Error == "" {
					result.Error = firstString(image, "errorMessage", "error_message")
				}
				if state == "FAILED" {
					result.Status = "FAILED"
				}
				urlValue := firstString(image, "imageUrl", "image_url", "previewUrl", "preview_url")
				if urlValue != "" && strings.HasPrefix(urlValue, "http") {
					result.Outputs = append(result.Outputs, Output{ID: firstString(image, "id", "externalId", "external_id"), URL: urlValue, MediaType: firstString(image, "contentType", "content_type"), Width: int(firstInt(image, "width")), Height: int(firstInt(image, "height"))})
				}
				if state == "GENERATED" {
					result.Status = "COMPLETE"
				}
			}
		}
	}
	if result.Status == "" && len(result.Outputs) > 0 {
		result.Status = "COMPLETE"
	}
	return result
}

// parseSessionPollResult understands the media-matrix Session/MediaItem
// envelopes and only returns generated video/image/audio URLs.
func parseSessionPollResult(data map[string]any) PollResult {
	result := PollResult{Raw: data}
	session := mapValue(data, "session")
	hasSession := session != nil
	if session == nil {
		session = data
	}
	setStatus := func(value string) {
		status := normalizeSessionStatus(value)
		switch status {
		case "FAILED":
			result.Status = "FAILED"
		case "COMPLETE":
			if result.Status != "FAILED" {
				result.Status = "COMPLETE"
			}
		case "PENDING":
			if result.Status == "" {
				result.Status = "PENDING"
			}
		}
	}
	setError := func(value map[string]any) {
		if result.Error == "" {
			result.Error = firstString(value, "errorMessage", "error_message", "error")
		}
	}
	setStatus(firstString(session, "status", "sessionStatus", "session_status"))
	setError(session)
	seen := make(map[string]struct{})
	appendItem := func(item map[string]any) {
		status := normalizeSessionStatus(firstString(item, "status", "mediaStatus", "media_status"))
		if status == "FAILED" {
			result.Status = "FAILED"
			setError(item)
			if result.Error == "" {
				result.Error = firstString(item, "message")
			}
			return
		}
		video := mapValue(item, "videoContent", "video_content")
		image := mapValue(item, "imageContent", "image_content")
		if video != nil {
			if urlValue := firstString(video, "generatedVideoUrl", "generated_video_url", "generatedPreviewUrl", "generated_preview_url"); strings.HasPrefix(urlValue, "http") {
				if _, exists := seen[urlValue]; !exists {
					seen[urlValue] = struct{}{}
					result.Outputs = append(result.Outputs, Output{ID: firstString(item, "id", "mediaId", "media_id"), URL: urlValue, MediaType: "video/mp4"})
				}
			}
		}
		if image != nil {
			if urlValue := firstString(image, "generatedImageUrl", "generated_image_url", "generatedPreviewUrl", "generated_preview_url"); strings.HasPrefix(urlValue, "http") {
				if _, exists := seen[urlValue]; !exists {
					seen[urlValue] = struct{}{}
					result.Outputs = append(result.Outputs, Output{ID: firstString(item, "id", "mediaId", "media_id"), URL: urlValue, MediaType: "image/png", Width: int(firstInt(image, "width")), Height: int(firstInt(image, "height"))})
				}
			}
		}
		setStatus(status)
	}
	collectItems := func(value any) {
		items, ok := value.([]any)
		if !ok {
			return
		}
		for _, rawItem := range items {
			if item, ok := rawItem.(map[string]any); ok {
				appendItem(item)
			}
		}
	}
	media := mapValue(data, "media")
	if media != nil {
		collectItems(firstValue(media, "items", "mediaItems", "media_items"))
	}
	collectItems(firstValue(data, "items", "mediaItems", "media_items"))
	if iterations, ok := data["iterations"].([]any); ok {
		for _, rawIteration := range iterations {
			iteration, ok := rawIteration.(map[string]any)
			if !ok {
				continue
			}
			collectItems(firstValue(iteration, "mediaItems", "media_items", "items"))
		}
	}
	// Some GetSession responses expose the terminal state only on an iteration
	// (with no media item at all).  Walk the iteration envelopes and preserve
	// that status/error instead of treating the task as indefinitely pending.
	var walkIterations func(any)
	walkIterations = func(value any) {
		switch current := value.(type) {
		case map[string]any:
			status := normalizeSessionStatus(firstString(current, "status", "state", "iterationStatus", "iteration_status"))
			setStatus(status)
			setError(current)
			if status == "FAILED" && result.Error == "" {
				result.Error = firstString(current, "message")
			}
			collectItems(firstValue(current, "mediaItems", "media_items", "items"))
			for _, key := range []string{"iterations", "iteration", "sessionIterations", "session_iterations"} {
				if child, ok := current[key]; ok {
					walkIterations(child)
				}
			}
		case []any:
			for _, child := range current {
				walkIterations(child)
			}
		}
	}
	walkIterations(data["iterations"])
	if hasSession {
		walkIterations(session["iterations"])
	}
	if result.Status == "" && len(result.Outputs) > 0 {
		result.Status = "COMPLETE"
	}
	return result
}

func normalizeFlowStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "IMAGE_STATE_GENERATING", "GENERATING", "QUEUED", "PROCESSING":
		return "PENDING"
	case "IMAGE_STATE_GENERATED", "GENERATED", "COMPLETE", "COMPLETED", "SUCCESS":
		return "COMPLETE"
	case "IMAGE_STATE_FAILED", "FAILED", "ERROR":
		return "FAILED"
	default:
		return normalizeStatus(value)
	}
}

func normalizeSessionStatus(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if strings.Contains(upper, "FAILED") || strings.Contains(upper, "ERROR") {
		return "FAILED"
	}
	if strings.Contains(upper, "COMPLETED") || strings.Contains(upper, "COMPLETE") || strings.Contains(upper, "SUCCEEDED") || strings.Contains(upper, "SUCCESS") {
		return "COMPLETE"
	}
	if strings.Contains(upper, "PENDING") || strings.Contains(upper, "PROCESS") || strings.Contains(upper, "QUEUE") || strings.Contains(upper, "RUNNING") {
		return "PENDING"
	}
	return normalizeStatus(value)
}

func parseOutputs(data map[string]any) []Output {
	result := make([]Output, 0)
	seen := make(map[string]struct{})
	var walk func(any)
	walk = func(value any) {
		switch current := value.(type) {
		case map[string]any:
			urlValue := firstString(current, "url", "downloadUrl", "download_url", "presignedUrl", "presigned_url", "mediaUrl", "media_url")
			if urlValue != "" && strings.HasPrefix(urlValue, "http") {
				if _, ok := seen[urlValue]; !ok {
					result = append(result, Output{ID: firstString(current, "id", "assetId", "asset_id", "mediaId", "media_id"), URL: urlValue, MediaType: firstString(current, "mediaType", "media_type", "mimeType", "mime_type", "type"), Width: int(firstInt(current, "width")), Height: int(firstInt(current, "height")), Duration: int(firstInt(current, "duration", "durationSeconds", "duration_seconds"))})
					seen[urlValue] = struct{}{}
				}
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
	walk(data)
	return result
}

func parseUploadURL(data map[string]any) UploadURL {
	var found UploadURL
	var walk func(any)
	walk = func(value any) {
		if found.URL != "" {
			return
		}
		switch current := value.(type) {
		case map[string]any:
			if candidate := firstString(current, "uploadUrl", "upload_url", "url", "signedUrl", "signed_url"); candidate != "" && strings.HasPrefix(candidate, "http") {
				found = UploadURL{ID: firstString(current, "id", "uploadId", "upload_id"), URL: candidate, Method: firstString(current, "method", "httpMethod", "http_method")}
				if rawHeaders, ok := current["headers"].(map[string]any); ok {
					found.Headers = make(map[string]string)
					for key, value := range rawHeaders {
						found.Headers[key] = stringValue(value)
					}
				}
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
	walk(data)
	return found
}

func encodeOptions(options map[string]any) map[string]any {
	result := make(map[string]any, len(options))
	for key, value := range options {
		switch typed := value.(type) {
		case int:
			result[key] = map[string]any{"integerValue": strconv.Itoa(typed)}
		case int64:
			result[key] = map[string]any{"integerValue": strconv.FormatInt(typed, 10)}
		case float64:
			result[key] = map[string]any{"floatValue": typed}
		case bool:
			result[key] = map[string]any{"booleanValue": typed}
		default:
			result[key] = map[string]any{"stringValue": fmt.Sprint(value)}
		}
	}
	return result
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func normalizeStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SUCCEEDED", "SUCCESS", "COMPLETED", "COMPLETE", "DONE":
		return "COMPLETE"
	case "FAILED", "FAILURE", "ERROR":
		return "FAILED"
	case "CANCELLED", "CANCELED":
		return "CANCELLED"
	case "PENDING", "QUEUED", "RUNNING", "PROCESSING", "COMPILING", "PERSISTING", "WAITING", "WAITING_EXTERNAL":
		return "PENDING"
	default:
		return strings.ToUpper(strings.TrimSpace(value))
	}
}

func firstString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(data[key]); value != "" {
			return value
		}
	}
	return ""
}
func firstStringRecursive(data map[string]any, keys ...string) string {
	var found string
	var walk func(any)
	walk = func(value any) {
		if found != "" {
			return
		}
		switch current := value.(type) {
		case map[string]any:
			if value := firstString(current, keys...); value != "" {
				found = value
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
	walk(data)
	return found
}
func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		if math.Trunc(typed) == typed {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}
func firstToken(value any, keys ...string) string {
	var found string
	var walk func(any)
	walk = func(current any) {
		if found != "" {
			return
		}
		switch typed := current.(type) {
		case map[string]any:
			for _, key := range keys {
				if token := stringValue(typed[key]); token != "" {
					found = token
					return
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return found
}
func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(typed))
		return n
	}
	return 0
}
func firstInt(data map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value := int64Value(data[key]); value != 0 {
			return value
		}
	}
	return 0
}
func firstPresentInt(data map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		value, exists := data[key]
		if !exists || value == nil {
			continue
		}
		switch typed := value.(type) {
		case int:
			return int64(typed), true
		case int64:
			return typed, true
		case float64:
			return int64(typed), true
		case json.Number:
			n, err := typed.Int64()
			if err == nil {
				return n, true
			}
		case string:
			n, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
			if err == nil {
				return n, true
			}
		}
	}
	return 0, false
}
func firstIntRecursive(data map[string]any, keys ...string) int {
	var found int
	var walk func(any)
	walk = func(value any) {
		if found != 0 {
			return
		}
		switch current := value.(type) {
		case map[string]any:
			for _, key := range keys {
				if n := intValue(current[key]); n != 0 {
					found = n
					return
				}
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
	walk(data)
	return found
}
func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		n, _ := typed.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return n
	}
	return 0
}
func findNumber(data map[string]any, keys map[string]struct{}) (bool, int64) {
	var found int64
	var ok bool
	var walk func(any)
	walk = func(value any) {
		if ok {
			return
		}
		switch current := value.(type) {
		case map[string]any:
			for key, child := range current {
				if _, wanted := keys[key]; wanted {
					if n := int64Value(child); n >= 0 {
						found = n
						ok = true
						return
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range current {
				walk(child)
			}
		}
	}
	walk(data)
	return ok, found
}

func mapValue(data map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := data[key].(map[string]any); ok {
			return value
		}
	}
	return nil
}
func stringSlice(data map[string]any, keys ...string) []string {
	for _, key := range keys {
		if values, ok := data[key].([]any); ok {
			result := make([]string, 0, len(values))
			for _, value := range values {
				if text := stringValue(value); text != "" {
					result = append(result, text)
				}
			}
			return result
		}
		if values, ok := data[key].([]string); ok {
			return append([]string(nil), values...)
		}
	}
	return nil
}
func looksLikeError(data map[string]any) bool {
	status := firstStringRecursive(data, "status", "code")
	return strings.EqualFold(status, "error") || strings.EqualFold(status, "failed") || data["error"] != nil || data["errors"] != nil
}
func findStringRecursive(data map[string]any, keys ...string) string {
	return firstStringRecursive(data, keys...)
}
func unixClaim(claims map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value := int64Value(claims[key]); value > 0 {
			if value > 1_000_000_000_000 {
				return value / 1000
			}
			return value
		}
	}
	return 0
}
func numberClaim(claims map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value := int64Value(claims[key]); value > 0 {
			return value
		}
	}
	return 0
}
func truncate(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 4096 {
		return text[:4096] + "…"
	}
	return text
}
