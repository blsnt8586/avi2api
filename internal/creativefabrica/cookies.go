package creativefabrica

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	maxCookieJSONBytes = 512 << 10
	maxCookies         = 256
)

// CookieJSON is the browser-exported JSON document. It remains JSON rather
// than being reduced to a Cookie header so path, expiry, SameSite and future
// browser attributes survive a later session restore.
type CookieJSON = json.RawMessage

type cookieDocument struct {
	Cookies []map[string]any `json:"cookies"`
}

// NormalizeCookieJSON validates a Chrome/Patchright cookie export and returns
// a compact copy. Both a bare cookie array and the common {"cookies": [...]}
// wrapper are accepted; the persisted representation is always an array.
func NormalizeCookieJSON(raw json.RawMessage) (CookieJSON, error) {
	if len(raw) == 0 {
		return nil, errors.New("cookie_json is required")
	}
	if len(raw) > maxCookieJSONBytes {
		return nil, fmt.Errorf("cookie_json must not exceed %d bytes", maxCookieJSONBytes)
	}
	var cookies []map[string]any
	if err := json.Unmarshal(raw, &cookies); err != nil {
		var document cookieDocument
		if wrapperErr := json.Unmarshal(raw, &document); wrapperErr != nil {
			return nil, errors.New("cookie_json must be a Chrome or Patchright cookie array")
		}
		cookies = document.Cookies
	}
	if len(cookies) < 1 || len(cookies) > maxCookies {
		return nil, fmt.Errorf("cookie_json must contain between 1 and %d cookies", maxCookies)
	}
	for _, cookie := range cookies {
		name := strings.TrimSpace(stringValue(cookie["name"]))
		if name == "" {
			return nil, errors.New("every cookie_json entry must include a name")
		}
		if !cookieScope(stringValue(cookie["domain"]), stringValue(cookie["url"])) {
			return nil, errors.New("cookie_json may contain only Creative Fabrica cookies")
		}
		if value := stringValue(cookie["value"]); !validCookieValue(name, value) {
			return nil, errors.New("cookie_json contains an invalid cookie value")
		}
	}
	canonical, err := json.Marshal(cookies)
	if err != nil {
		return nil, errors.New("cookie_json is invalid")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, canonical); err != nil {
		return nil, errors.New("cookie_json is invalid")
	}
	return CookieJSON(compact.Bytes()), nil
}

// CookieHeaderFromJSON derives the short-lived transport header used by the
// GraphQL and token-exchange calls. Duplicate cookie names are collapsed in
// export order, matching browser Cookie header behavior.
func CookieHeaderFromJSON(raw CookieJSON) (string, error) {
	var cookies []map[string]any
	if err := json.Unmarshal(raw, &cookies); err != nil {
		return "", errors.New("stored cookie_json is invalid")
	}
	parts := make([]string, 0, len(cookies))
	seen := make(map[string]struct{}, len(cookies))
	for _, cookie := range cookies {
		name := strings.TrimSpace(stringValue(cookie["name"]))
		value := stringValue(cookie["value"])
		if name == "" || !cookieScope(stringValue(cookie["domain"]), stringValue(cookie["url"])) {
			return "", errors.New("stored cookie_json contains an invalid Creative Fabrica cookie")
		}
		// Creative Fabrica exports _user_agent as a metadata cookie whose
		// value is the literal browser user-agent string and therefore may
		// contain semicolons. It must remain in the complete JSON export, but
		// it is not a transport cookie and would make a Cookie header invalid.
		if isCookieMetadata(name) {
			continue
		}
		if !validCookieValue(name, value) {
			return "", errors.New("stored cookie_json contains an invalid Creative Fabrica cookie")
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		parts = append(parts, name+"="+value)
	}
	if len(parts) == 0 {
		return "", errors.New("stored cookie_json is empty")
	}
	return strings.Join(parts, "; "), nil
}

func isCookieMetadata(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "_user_agent")
}

func validCookieValue(name, value string) bool {
	if isCookieMetadata(name) {
		return !strings.ContainsAny(value, "\r\n")
	}
	return !strings.ContainsAny(value, ";\r\n")
}

func CookieFingerprint(raw CookieJSON) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// MergeCookieJSON combines browser-exported documents from the login and JWT
// exchange responses. New values replace an older cookie with the same
// name/domain/path tuple while unrelated attributes are retained.
func MergeCookieJSON(documents ...CookieJSON) (CookieJSON, error) {
	merged := make([]map[string]any, 0)
	indexes := make(map[string]int)
	for _, raw := range documents {
		if len(raw) == 0 {
			continue
		}
		var cookies []map[string]any
		if err := json.Unmarshal(raw, &cookies); err != nil {
			return nil, errors.New("cookie_json is invalid")
		}
		for _, cookie := range cookies {
			name := strings.TrimSpace(stringValue(cookie["name"]))
			if name == "" || !cookieScope(stringValue(cookie["domain"]), stringValue(cookie["url"])) {
				return nil, errors.New("cookie_json may contain only Creative Fabrica cookies")
			}
			key := strings.ToLower(name) + "\x00" + strings.ToLower(strings.TrimSpace(stringValue(cookie["domain"]))) + "\x00" + stringValue(cookie["path"])
			if index, ok := indexes[key]; ok {
				merged[index] = cookie
			} else {
				indexes[key] = len(merged)
				merged = append(merged, cookie)
			}
		}
	}
	if len(merged) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, errors.New("cookie_json is invalid")
	}
	return NormalizeCookieJSON(raw)
}

func cookieScope(domain, rawURL string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if creativeFabricaHost(domain) {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && creativeFabricaHost(strings.ToLower(parsed.Hostname()))
}

func creativeFabricaHost(host string) bool {
	return host == "creativefabrica.com" || strings.HasSuffix(host, ".creativefabrica.com")
}
