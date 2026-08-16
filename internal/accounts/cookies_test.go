package accounts

import (
	"encoding/json"
	"testing"
)

func validCookieJSON(t *testing.T) json.RawMessage {
	t.Helper()
	return json.RawMessage(`[
  {"name":"__Secure-better-auth.session_token","value":"session","domain":".leonardo.ai","path":"/","httpOnly":true,"secure":true,"sameSite":"Lax","expires":1800000000},
  {"name":"__Secure-better-auth.session_data.0","value":"part-0","domain":".leonardo.ai","path":"/","httpOnly":true,"secure":true,"sameSite":"Lax"},
  {"name":"__Secure-better-auth.session_data.1","value":"part-1","domain":".leonardo.ai","path":"/","httpOnly":true,"secure":true,"sameSite":"Lax"}
]`)
}

func TestNormalizeBrowserCookieJSONRetainsBrowserAttributes(t *testing.T) {
	normalized, err := NormalizeBrowserCookieJSON(validCookieJSON(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(normalized) == "" || !contains(string(normalized), `"httpOnly":true`) || !contains(string(normalized), `"expires":1800000000`) {
		t.Fatalf("cookie attributes were not retained: %s", normalized)
	}
	header, err := CookieHeaderFromJSON(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(header, "__Secure-better-auth.session_token=session") || !contains(header, "session_data.1=part-1") {
		t.Fatalf("unexpected header %q", header)
	}
}

func TestNormalizeBrowserCookieJSONAllowsEmptyCookieValues(t *testing.T) {
	raw := json.RawMessage(`[
  {"name":"__Secure-better-auth.session_token","value":"session","domain":".leonardo.ai","path":"/"},
  {"name":"__Secure-better-auth.session_data.0","value":"","domain":".leonardo.ai","path":"/"}
]`)
	if _, err := NormalizeBrowserCookieJSON(raw); err != nil {
		t.Fatalf("empty cookie values are valid browser state: %v", err)
	}
}

func TestNormalizeBrowserCookieJSONRejectsIncompleteOrForeignCookies(t *testing.T) {
	tests := []json.RawMessage{
		json.RawMessage(`[{"name":"__Secure-better-auth.session_token","value":"x","domain":".leonardo.ai"}]`),
		json.RawMessage(`[{"name":"__Secure-better-auth.session_token","value":"x","domain":".example.com"},{"name":"session_data.0","value":"x","domain":".example.com"}]`),
	}
	for _, raw := range tests {
		if _, err := NormalizeBrowserCookieJSON(raw); err == nil {
			t.Fatalf("expected invalid cookie JSON to fail: %s", raw)
		}
	}
}

func contains(value, want string) bool {
	return len(want) == 0 || (len(value) >= len(want) && stringContains(value, want))
}

func stringContains(value, want string) bool {
	for index := 0; index+len(want) <= len(value); index++ {
		if value[index:index+len(want)] == want {
			return true
		}
	}
	return false
}
