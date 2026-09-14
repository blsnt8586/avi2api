package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/config"
)

func TestAdminCSRFAcceptsConfiguredAndCurrentOrigin(t *testing.T) {
	server := &Server{Config: config.Config{PublicBaseURL: "https://admin.example.com"}}
	handler := server.adminCSRF(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for name, test := range map[string][2]string{
		"configured origin": {"http://45.59.128.219:18080/admin/api/settings/password", "https://admin.example.com"},
		"current origin":    {"http://45.59.128.219:18080/admin/api/settings/password", "http://45.59.128.219:18080"},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, test[0], nil)
			req.Header.Set("Origin", test[1])
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestAdminCSRFRejectsForeignOrigin(t *testing.T) {
	server := &Server{Config: config.Config{PublicBaseURL: "https://admin.example.com"}}
	handler := server.adminCSRF(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPut, "http://45.59.128.219:18080/admin/api/settings/password", nil)
	req.Header.Set("Origin", "https://foreign.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
