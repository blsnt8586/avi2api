package httpapi

import (
	"strings"
	"testing"
)

func TestNormalizeIdempotencyKey(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		want     string
		wantCode string
	}{
		{name: "missing", wantCode: "idempotency_key_required"},
		{name: "whitespace", value: " \t\r\n ", wantCode: "idempotency_key_required"},
		{name: "normalizes whitespace", value: "  operation-123  ", want: "operation-123"},
		{name: "maximum ASCII length", value: strings.Repeat("a", 255), want: strings.Repeat("a", 255)},
		{name: "maximum Unicode length", value: strings.Repeat("任", 255), want: strings.Repeat("任", 255)},
		{name: "too long", value: strings.Repeat("a", 256), wantCode: "idempotency_key_too_long"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeIdempotencyKey(test.value)
			if test.wantCode == "" {
				if err != nil || got != test.want {
					t.Fatalf("normalizeIdempotencyKey() = %q, %v; want %q, nil", got, err, test.want)
				}
				return
			}
			if err == nil || err.Code != test.wantCode {
				t.Fatalf("normalizeIdempotencyKey() error = %v; want code %q", err, test.wantCode)
			}
		})
	}
}
