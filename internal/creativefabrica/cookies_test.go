package creativefabrica

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeCookieJSONAcceptsWrapperAndPreservesAttributes(t *testing.T) {
	raw := json.RawMessage(`{"cookies":[{"name":"cfToken","value":"opaque","domain":".creativefabrica.com","path":"/","httpOnly":true,"secure":true,"sameSite":"Lax","expirationDate":1893456000}]}`)
	normalized, err := NormalizeCookieJSON(raw)
	if err != nil {
		t.Fatalf("NormalizeCookieJSON() error = %v", err)
	}
	if !strings.Contains(string(normalized), `"httpOnly":true`) || !strings.Contains(string(normalized), `"expirationDate":1893456000`) {
		t.Fatalf("normalized cookie attributes were lost: %s", normalized)
	}
	header, err := CookieHeaderFromJSON(normalized)
	if err != nil {
		t.Fatalf("CookieHeaderFromJSON() error = %v", err)
	}
	if header != "cfToken=opaque" {
		t.Fatalf("unexpected cookie header %q", header)
	}
}

func TestNormalizeCookieJSONRejectsOutsideDomain(t *testing.T) {
	raw := json.RawMessage(`[{"name":"session","value":"x","domain":"example.com"}]`)
	if _, err := NormalizeCookieJSON(raw); err == nil {
		t.Fatal("expected non-Creative-Fabrica cookie to be rejected")
	}
}

func TestCookieHeaderFromJSONCollapsesDuplicateNames(t *testing.T) {
	raw := json.RawMessage(`[{"name":"a","value":"first","domain":"studio.creativefabrica.com"},{"name":"a","value":"second","domain":"studio.creativefabrica.com"},{"name":"b","value":"two","url":"https://www.creativefabrica.com/"}]`)
	header, err := CookieHeaderFromJSON(raw)
	if err != nil {
		t.Fatalf("CookieHeaderFromJSON() error = %v", err)
	}
	if header != "a=first; b=two" {
		t.Fatalf("unexpected collapsed header %q", header)
	}
}
