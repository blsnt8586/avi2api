package adobe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	maxCookieJSONBytes = 512 << 10
	maxCookies         = 128
)

type CookieJSON = json.RawMessage

type browserCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	URL    string `json:"url"`
}

func NormalizeCookieJSON(raw json.RawMessage) (CookieJSON, error) {
	if len(raw) == 0 {
		return nil, errors.New("cookie_json is required")
	}
	if len(raw) > maxCookieJSONBytes {
		return nil, fmt.Errorf("cookie_json must not exceed %d bytes", maxCookieJSONBytes)
	}
	var cookies []browserCookie
	if err := json.Unmarshal(raw, &cookies); err != nil {
		return nil, errors.New("cookie_json must be a Chrome or Patchright cookie array")
	}
	if len(cookies) < 1 || len(cookies) > maxCookies {
		return nil, fmt.Errorf("cookie_json must contain between 1 and %d cookies", maxCookies)
	}
	for _, cookie := range cookies {
		if strings.TrimSpace(cookie.Name) == "" {
			return nil, errors.New("every cookie_json entry must include a name")
		}
		if !adobeCookieScope(cookie.Domain, cookie.URL) {
			return nil, errors.New("cookie_json may contain only Adobe cookies")
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, errors.New("cookie_json is invalid")
	}
	return CookieJSON(compact.Bytes()), nil
}

func CookieHeaderFromJSON(raw CookieJSON) (string, error) {
	var cookies []browserCookie
	if err := json.Unmarshal(raw, &cookies); err != nil {
		return "", errors.New("stored cookie_json is invalid")
	}
	parts := make([]string, 0, len(cookies))
	seen := make(map[string]struct{}, len(cookies))
	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie.Name)
		if name == "" || !adobeCookieScope(cookie.Domain, cookie.URL) {
			return "", errors.New("stored cookie_json contains an invalid Adobe cookie")
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		parts = append(parts, name+"="+cookie.Value)
	}
	if len(parts) == 0 {
		return "", errors.New("stored cookie_json is empty")
	}
	return strings.Join(parts, "; "), nil
}

func adobeCookieScope(domain, rawURL string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if adobeHost(domain) {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && adobeHost(strings.ToLower(parsed.Hostname()))
}

func adobeHost(host string) bool {
	return host == "adobe.com" || strings.HasSuffix(host, ".adobe.com") ||
		host == "adobelogin.com" || strings.HasSuffix(host, ".adobelogin.com")
}
