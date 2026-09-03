package accounts

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leonardo2api/leonardo2api/internal/cryptox"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
)

func TestLoginCredentialEncryptionRoundTrip(t *testing.T) {
	cipher, err := cryptox.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Cipher: cipher}
	ciphertext, err := service.encryptLoginCredential(LoginCredential{Email: "fixture@example.com", Password: "secret-value"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, "fixture@example.com") || strings.Contains(ciphertext, "secret-value") {
		t.Fatal("encrypted credential contains plaintext")
	}
	plaintext, err := cipher.Decrypt(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plaintext, `"email":"fixture@example.com"`) || !strings.Contains(plaintext, `"password":"secret-value"`) {
		t.Fatalf("unexpected credential payload %q", plaintext)
	}
}

func TestLoginCredentialRequiresEmailAndPassword(t *testing.T) {
	cipher, err := cryptox.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Cipher: cipher}
	for _, credential := range []LoginCredential{{Email: "fixture@example.com"}, {Password: "secret-value"}} {
		if _, err := service.encryptLoginCredential(credential); err == nil {
			t.Fatalf("expected incomplete credential to fail: %+v", credential)
		}
	}
}

func TestClassifySessionError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status string
		cool   bool
	}{
		{name: "rate limited", err: &leonardo.HTTPError{Status: 429}, status: "rate_limited", cool: true},
		{name: "unauthorized", err: &leonardo.HTTPError{Status: 401}, status: "invalid"},
		{name: "forbidden", err: &leonardo.HTTPError{Status: 403}, status: "invalid"},
		{name: "network", err: errors.New("connection reset"), status: "cooldown", cool: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _, cooldown := classifySessionError(tt.err)
			if status != tt.status || (cooldown != nil) != tt.cool {
				t.Fatalf("status=%q cooldown=%v", status, cooldown)
			}
		})
	}
}

func TestGenerationPermissionCheckDue(t *testing.T) {
	checkedAt := time.Now().Add(-2 * time.Hour)
	account := domain.Account{ProviderID: "leonardo", GenerationPermissionStatus: "verified", GenerationPermissionCheckedAt: &checkedAt}
	if GenerationPermissionCheckDue(account, 24*time.Hour) {
		t.Fatal("verified account should remain within the configured interval")
	}
	if !GenerationPermissionCheckDue(account, time.Hour) {
		t.Fatal("verified account should be due after the interval")
	}
	rateLimited := account
	rateLimited.GenerationPermissionStatus = "rate_limited"
	if !GenerationPermissionCheckDue(rateLimited, 5*time.Minute) {
		t.Fatal("rate-limited probe should retry on the short interval")
	}
}

func TestGenerationPermissionBlockedClassification(t *testing.T) {
	if !IsGenerationPermissionBlocked(&leonardo.HTTPError{Status: 403}) {
		t.Fatal("HTTP 403 should mark generation permission as blocked")
	}
	gql := &leonardo.GraphQLError{Operation: "Generate", Message: "Access denied", Extensions: map[string]any{"code": "HttpException", "details": "statusCode=403"}}
	if !IsGenerationPermissionBlocked(gql) {
		t.Fatal("nested GraphQL 403 should mark generation permission as blocked")
	}
	if IsGenerationPermissionBlocked(errors.New("network timeout")) {
		t.Fatal("network errors should remain transient")
	}
}

func TestBrowserSessionRequiredClassification(t *testing.T) {
	if !IsBrowserSessionRequired(ErrBrowserSessionRequired) {
		t.Fatal("browser session requirement was not recognized")
	}
	if IsBrowserSessionRequired(errors.New("fixture")) {
		t.Fatal("unrelated error was classified as a browser session requirement")
	}
}
