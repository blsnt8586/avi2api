package adobe

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeCookieJSONPreservesAdobeCookieAttributes(t *testing.T) {
	raw := json.RawMessage(`[{"name":"session","value":"abc","domain":".adobe.com","path":"/","httpOnly":true,"sameSite":"Lax"}]`)
	normalized, err := NormalizeCookieJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(normalized), `"httpOnly":true`) {
		t.Fatalf("normalized = %s", normalized)
	}
	header, err := CookieHeaderFromJSON(normalized)
	if err != nil || header != "session=abc" {
		t.Fatalf("header = %q, %v", header, err)
	}
}

func TestNormalizeCookieJSONRejectsNonAdobeCookie(t *testing.T) {
	_, err := NormalizeCookieJSON(json.RawMessage(`[{"name":"session","value":"abc","domain":"example.com"}]`))
	if err == nil {
		t.Fatal("expected domain validation error")
	}
}
