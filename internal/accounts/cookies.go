package accounts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const maxBrowserCookieJSONBytes = 512 << 10
const maxBrowserCookies = 64

// BrowserCookieJSON is deliberately kept as raw JSON after validation. Chrome
// and Patchright add cookie attributes over time; retaining those attributes is
// necessary to restore the same browser session on a later refresh. The alias
// keeps HTTP responses as a JSON array instead of encoding the bytes as base64.
type BrowserCookieJSON = json.RawMessage

type browserCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	URL    string `json:"url"`
}

func NormalizeBrowserCookieJSON(raw json.RawMessage) (BrowserCookieJSON, error) {
	if len(raw) == 0 {
		return nil, errors.New("cookie_json is required")
	}
	if len(raw) > maxBrowserCookieJSONBytes {
		return nil, fmt.Errorf("cookie_json must not exceed %d bytes", maxBrowserCookieJSONBytes)
	}
	var cookies []browserCookie
	if err := json.Unmarshal(raw, &cookies); err != nil {
		return nil, errors.New("cookie_json must be a Chrome or Patchright cookie array")
	}
	if len(cookies) < 2 || len(cookies) > maxBrowserCookies {
		return nil, fmt.Errorf("cookie_json must contain between 2 and %d cookies", maxBrowserCookies)
	}
	hasSessionToken := false
	hasSessionData := false
	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie.Name)
		if name == "" {
			return nil, errors.New("every cookie_json entry must include a name")
		}
		if !leonardoCookieScope(cookie.Domain, cookie.URL) {
			return nil, errors.New("cookie_json may contain only leonardo.ai cookies")
		}
		lowerName := strings.ToLower(name)
		hasSessionToken = hasSessionToken || strings.Contains(lowerName, "session_token")
		hasSessionData = hasSessionData || strings.Contains(lowerName, "session_data")
	}
	if !hasSessionToken || !hasSessionData {
		return nil, errors.New("cookie_json must include the Better Auth session_token and session_data cookies")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, errors.New("cookie_json is invalid")
	}
	return BrowserCookieJSON(compact.Bytes()), nil
}

func CookieHeaderFromJSON(raw BrowserCookieJSON) (string, error) {
	var cookies []browserCookie
	if err := json.Unmarshal(raw, &cookies); err != nil {
		return "", errors.New("stored cookie_json is invalid")
	}
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if !leonardoCookieScope(cookie.Domain, cookie.URL) || cookie.Name == "" {
			return "", errors.New("stored cookie_json contains an invalid Leonardo cookie")
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	if len(parts) == 0 {
		return "", errors.New("stored cookie_json is empty")
	}
	return strings.Join(parts, "; "), nil
}

func leonardoCookieScope(domain, rawURL string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "leonardo.ai" || strings.HasSuffix(domain, ".leonardo.ai") {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "leonardo.ai" || strings.HasSuffix(host, ".leonardo.ai")
}
