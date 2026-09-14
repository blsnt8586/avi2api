package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"golang.org/x/crypto/argon2"
	"net/http"
	"strings"
	"time"
)

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct{ Username, Password string }
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	valid, err := s.verifyAdminPassword(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	if !valid {
		writeError(w, 401, "invalid_credentials", "invalid credentials")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "session_error", "failed to generate session token")
		return
	}
	hash := sha256.Sum256([]byte(token))
	if err := s.Redis.Set(r.Context(), "leo:admin:session:"+hex.EncodeToString(hash[:]), req.Username, 12*time.Hour).Err(); err != nil {
		writeError(w, 500, "session_error", err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "leo_admin_session", Value: token, Path: "/", HttpOnly: true, Secure: s.Config.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) verifyAdminPassword(ctx context.Context, username, password string) (bool, error) {
	if username != s.Config.AdminUsername {
		return false, nil
	}
	salt, hash, err := s.Store.GetAdminCredential(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		salt, hash = s.adminSalt, s.adminHash
	} else if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return constantEqual(candidate, hash), nil
}

func (s *Server) adminChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if len(req.NewPassword) < 8 {
		writeError(w, 400, "weak_password", "new password must be at least 8 characters")
		return
	}
	if req.CurrentPassword == req.NewPassword {
		writeError(w, 400, "password_unchanged", "new password must differ from current password")
		return
	}
	valid, err := s.verifyAdminPassword(r.Context(), s.Config.AdminUsername, req.CurrentPassword)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	if !valid {
		writeError(w, 401, "invalid_credentials", "current password is incorrect")
		return
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		writeError(w, 500, "password_update_failed", err.Error())
		return
	}
	hash := argon2.IDKey([]byte(req.NewPassword), salt, 1, 64*1024, 4, 32)
	if err := s.Store.SetAdminCredential(r.Context(), s.Config.AdminUsername, salt, hash); err != nil {
		writeError(w, 500, "password_update_failed", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "admin.password_change", s.Config.AdminUsername, nil)
	var cursor uint64
	for {
		keys, next, err := s.Redis.Scan(r.Context(), cursor, "leo:admin:session:*", 100).Result()
		if err != nil {
			s.Log.Warn("failed to clear admin sessions after password change", "error", err)
			break
		}
		if len(keys) > 0 {
			_ = s.Redis.Del(r.Context(), keys...).Err()
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "reauthenticate": true})
}

func (s *Server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("leo_admin_session")
		if err != nil {
			writeError(w, 401, "unauthorized", "admin login required")
			return
		}
		h := sha256.Sum256([]byte(c.Value))
		if _, err := s.Redis.Get(r.Context(), "leo:admin:session:"+hex.EncodeToString(h[:])).Result(); err != nil {
			writeError(w, 401, "unauthorized", "admin session expired")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		origin := strings.TrimRight(r.Header.Get("Origin"), "/")
		if origin != "" && origin != s.Config.PublicBaseURL && !strings.EqualFold(origin, requestOrigin(r)) {
			writeError(w, 403, "csrf_rejected", "request origin is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
