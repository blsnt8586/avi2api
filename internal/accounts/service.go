package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/cryptox"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/store"
)

type Service struct {
	Store  *store.Store
	Redis  *redis.Client
	Cipher *cryptox.Cipher
	Config config.Config
}

var ErrStaleBalanceRefresh = errors.New("account balance refresh superseded by a newer request")
var ErrBrowserSessionRequired = errors.New("browser session refresh required")

func IsBrowserSessionRequired(err error) bool {
	return errors.Is(err, ErrBrowserSessionRequired)
}

type BrowserSession struct {
	Verified          bool   `json:"verified"`
	AccessToken       string `json:"access_token"`
	AccessTokenExpiry int64  `json:"access_token_expiry"`
	CookieHeader      string `json:"cookie_header,omitempty"`
	HasuraUserID      string `json:"hasura_user_id"`
	CognitoSub        string `json:"cognito_sub"`
	Email             string `json:"email"`
	UserAgent         string `json:"user_agent"`
}

type BalanceRefreshResult struct {
	Account   domain.Account
	Version   int64
	StartedAt time.Time
}

type LoginCredential struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Service) Create(ctx context.Context, name, email, password, cookie, proxy, browserWorkerGroup string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int, session *BrowserSession) (domain.Account, error) {
	cookieCipher, err := s.Cipher.Encrypt(cookie)
	if err != nil {
		return domain.Account{}, err
	}
	credentialCipher := ""
	hasLoginCredentials := strings.TrimSpace(password) != ""
	if hasLoginCredentials {
		credentialCipher, err = s.encryptLoginCredential(LoginCredential{Email: strings.TrimSpace(email), Password: password})
		if err != nil {
			return domain.Account{}, err
		}
	}
	userAgent := ""
	if session != nil {
		userAgent = session.UserAgent
	}
	a, err := s.Store.CreateAccount(ctx, name, email, cookieCipher, credentialCipher, hasLoginCredentials, proxy, userAgent, concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots)
	if err != nil {
		return a, err
	}
	if err := s.Store.SetAccountBrowserWorkerGroup(ctx, a.ID, browserWorkerGroup); err != nil {
		_ = s.Store.DeleteAccount(ctx, a.ID)
		return domain.Account{}, err
	}
	if session != nil && session.AccessToken != "" {
		_, err = s.ImportBrowserSession(ctx, a.ID, *session)
	} else {
		_, err = s.Refresh(ctx, a.ID, true)
	}
	if err != nil {
		if deleteErr := s.Store.DeleteAccount(ctx, a.ID); deleteErr != nil {
			return a, errors.Join(err, deleteErr)
		}
		return domain.Account{}, err
	}
	return s.Store.GetAccount(ctx, a.ID)
}

func (s *Service) encryptLoginCredential(credential LoginCredential) (string, error) {
	if strings.TrimSpace(credential.Email) == "" || credential.Password == "" {
		return "", errors.New("email and password are required for automatic browser login")
	}
	payload, err := json.Marshal(credential)
	if err != nil {
		return "", err
	}
	return s.Cipher.Encrypt(string(payload))
}

func (s *Service) LoginCredential(ctx context.Context, id uuid.UUID) (LoginCredential, error) {
	ciphertext, configured, err := s.Store.GetAccountLoginCredentialCiphertext(ctx, id)
	if err != nil {
		return LoginCredential{}, err
	}
	if !configured || ciphertext == "" {
		return LoginCredential{}, store.ErrNotFound
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext)
	if err != nil {
		return LoginCredential{}, err
	}
	var credential LoginCredential
	if err := json.Unmarshal([]byte(plaintext), &credential); err != nil {
		return LoginCredential{}, err
	}
	if strings.TrimSpace(credential.Email) == "" || credential.Password == "" {
		return LoginCredential{}, errors.New("stored automatic login credential is incomplete")
	}
	return credential, nil
}

func (s *Service) SetLoginCredential(ctx context.Context, id uuid.UUID, email, password string) error {
	if password == "" {
		return s.Store.SetAccountLoginCredential(ctx, id, "", false)
	}
	ciphertext, err := s.encryptLoginCredential(LoginCredential{Email: strings.TrimSpace(email), Password: password})
	if err != nil {
		return err
	}
	return s.Store.SetAccountLoginCredential(ctx, id, ciphertext, true)
}

func (s *Service) ImportBrowserSession(ctx context.Context, id uuid.UUID, session BrowserSession) (domain.Account, error) {
	a, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return a, err
	}
	if session.CognitoSub == "" {
		session.CognitoSub = a.CognitoSub
	}
	if session.HasuraUserID == "" {
		session.HasuraUserID = a.HasuraUserID
	}
	if session.AccessToken == "" || session.AccessTokenExpiry <= time.Now().Unix() || session.CognitoSub == "" {
		return a, errors.New("browser session is incomplete or expired")
	}
	tokenCipher, err := s.Cipher.Encrypt(session.AccessToken)
	if err != nil {
		return a, err
	}
	cookieCipher := ""
	if session.CookieHeader != "" {
		cookieCipher, err = s.Cipher.Encrypt(session.CookieHeader)
		if err != nil {
			return a, err
		}
	}
	expiry := time.Unix(session.AccessTokenExpiry, 0)
	if err := s.Store.UpdateAccountSessionCredentials(ctx, id, tokenCipher, cookieCipher, expiry, session.HasuraUserID, session.CognitoSub, session.Email, session.UserAgent); err != nil {
		return a, err
	}
	a, err = s.Store.GetAccount(ctx, id)
	if err != nil {
		return a, err
	}

	// The browser job has already completed the expensive authentication step.
	// A busy or cooling shared exit delays only the balance refresh; it must not
	// discard the newly authenticated session.
	if s.ProxyControlRemaining(ctx, a.ProxyURL) > 0 {
		return a, nil
	}
	result, refreshErr := s.RefreshTokensAfter(ctx, a, session.AccessToken, time.Time{})
	if refreshErr != nil {
		var upstream *leonardo.HTTPError
		if errors.As(refreshErr, &upstream) && (upstream.Status == 401 || upstream.Status == 403) {
			status, message, cooldown := classifySessionError(refreshErr)
			_ = s.Store.SetAccountError(ctx, id, status, message, cooldown)
			return a, refreshErr
		}
		// The session is durable. The browser job completion path leaves a
		// Cookie-stage job pending when the balance snapshot is still stale.
		return a, nil
	}
	return result.Account, nil
}

// SessionCookie returns the decrypted browser cookie for a leased internal
// session worker. Callers must keep it in memory or short-lived private files.
func (s *Service) SessionCookie(ctx context.Context, id uuid.UUID) (string, error) {
	a, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return "", err
	}
	return s.Cipher.Decrypt(a.CookieCiphertext)
}

func (s *Service) RefreshTokens(ctx context.Context, a domain.Account, token string) (domain.Account, error) {
	result, err := s.RefreshTokensAfter(ctx, a, token, time.Time{})
	return result.Account, err
}

func (s *Service) RefreshTokensAfter(ctx context.Context, a domain.Account, token string, notBefore time.Time) (BalanceRefreshResult, error) {
	baseline, err := s.Store.GetAccountBalanceSnapshot(ctx, a.ID)
	if err != nil {
		return BalanceRefreshResult{Account: a}, err
	}
	if s.Redis == nil {
		return s.refreshTokensLocked(ctx, a, token, notBefore)
	}
	lockKey := "leo:account:balance-refresh:" + a.ID.String()
	const lockTTL = 45 * time.Second
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		latest, snapshotErr := s.Store.GetAccountBalanceSnapshot(ctx, a.ID)
		if snapshotErr != nil {
			return BalanceRefreshResult{Account: a}, snapshotErr
		}
		if latest.Version > baseline.Version && latest.StartedAt != nil && !latest.StartedAt.Before(notBefore) {
			refreshed, getErr := s.Store.GetAccount(ctx, a.ID)
			if getErr != nil {
				return BalanceRefreshResult{Account: a}, getErr
			}
			return BalanceRefreshResult{Account: refreshed, Version: latest.Version, StartedAt: *latest.StartedAt}, nil
		}

		lockValue := uuid.NewString()
		ok, lockErr := s.Redis.SetNX(ctx, lockKey, lockValue, lockTTL).Result()
		if lockErr != nil {
			return BalanceRefreshResult{Account: a}, lockErr
		}
		if ok {
			result, refreshErr := s.refreshTokensLocked(ctx, a, token, notBefore)
			_ = s.Redis.Eval(context.Background(), `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) end return 0`, []string{lockKey}, lockValue).Err()
			return result, refreshErr
		}

		select {
		case <-ctx.Done():
			return BalanceRefreshResult{Account: a}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) refreshTokensLocked(ctx context.Context, a domain.Account, token string, notBefore time.Time) (BalanceRefreshResult, error) {
	if delay := time.Until(notBefore); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return BalanceRefreshResult{Account: a}, ctx.Err()
		case <-timer.C:
		}
	}
	version, err := s.Store.BeginAccountBalanceRefresh(ctx, a.ID)
	if err != nil {
		return BalanceRefreshResult{Account: a}, err
	}
	latest, err := s.Store.GetAccount(ctx, a.ID)
	if err != nil {
		return BalanceRefreshResult{Account: a}, err
	}
	a = latest
	release, err := s.acquireProxyControl(ctx, a.ProxyURL)
	if err != nil {
		return BalanceRefreshResult{Account: a}, err
	}
	defer release()
	client, err := leonardo.New(a.ProxyURL, a.UserAgent, s.Config.SchemaVersion)
	if err != nil {
		return BalanceRefreshResult{Account: a}, err
	}
	snapshotStartedAt := time.Now().UTC()
	tokens, err := client.GetTokens(ctx, token, a.TeamID, a.CognitoSub)
	if err != nil {
		s.markProxyRateLimited(ctx, a.ProxyURL, err)
		return BalanceRefreshResult{Account: a}, err
	}
	updated, err := s.Store.UpdateAccountTokens(ctx, a.ID, version, snapshotStartedAt, tokens.Plan, tokens.Subscription, tokens.Rollover, tokens.Paid)
	if err != nil {
		return BalanceRefreshResult{Account: a}, err
	}
	if !updated {
		return BalanceRefreshResult{Account: a}, ErrStaleBalanceRefresh
	}
	refreshed, err := s.Store.GetAccount(ctx, a.ID)
	return BalanceRefreshResult{Account: refreshed, Version: version, StartedAt: snapshotStartedAt}, err
}

func (s *Service) Refresh(ctx context.Context, id uuid.UUID, force bool) (domain.Account, error) {
	return s.refresh(ctx, id, force, true)
}

// RefreshForScheduler uses a fresh JWT to finish deferred balance work. JWT
// renewal is handed to the browser worker because Leonardo checkpoints direct
// HTTP calls to get-session even when the Better Auth cookies are valid.
func (s *Service) RefreshForScheduler(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	a, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return a, err
	}
	if a.AccessTokenCiphertext == "" || a.AccessTokenExpiresAt == nil || !a.AccessTokenExpiresAt.After(time.Now().Add(s.Config.SessionRefreshMinFresh)) {
		return a, ErrBrowserSessionRequired
	}
	token, err := s.Cipher.Decrypt(a.AccessTokenCiphertext)
	if err != nil {
		return a, err
	}
	result, err := s.RefreshTokensAfter(ctx, a, token, time.Time{})
	return result.Account, err
}

func (s *Service) refresh(ctx context.Context, id uuid.UUID, force, recordError bool) (domain.Account, error) {
	a, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return a, err
	}
	if !force && a.AccessTokenExpiresAt != nil && a.AccessTokenExpiresAt.After(time.Now().Add(s.Config.SessionRefreshAhead)) && a.AccessTokenCiphertext != "" {
		return a, nil
	}
	lockKey := "leo:account:refresh:" + id.String()
	lockValue := uuid.NewString()
	ok, err := s.Redis.SetNX(ctx, lockKey, lockValue, 30*time.Second).Result()
	if err != nil {
		return a, err
	}
	if !ok {
		for range 20 {
			time.Sleep(150 * time.Millisecond)
			a, err = s.Store.GetAccount(ctx, id)
			if err == nil && a.AccessTokenExpiresAt != nil && a.AccessTokenExpiresAt.After(time.Now().Add(time.Minute)) {
				return a, nil
			}
		}
		return a, errors.New("account refresh lock timeout")
	}
	defer s.Redis.Eval(ctx, `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) end return 0`, []string{lockKey}, lockValue)
	cookie, err := s.Cipher.Decrypt(a.CookieCiphertext)
	if err != nil {
		return a, err
	}
	client, err := leonardo.New(a.ProxyURL, a.UserAgent, s.Config.SchemaVersion)
	if err != nil {
		return a, err
	}
	release, err := s.acquireProxyControl(ctx, a.ProxyURL)
	if err != nil {
		return a, err
	}
	defer release()
	session, email, err := client.GetSession(ctx, cookie)
	if err != nil {
		s.markProxyRateLimited(ctx, a.ProxyURL, err)
		if recordError {
			status, message, cooldown := classifySessionError(err)
			// A checkpoint on the session endpoint belongs to the shared
			// authentication exit. A still-valid JWT remains usable for
			// generation and must not be removed from routing.
			if status != "rate_limited" || a.AccessTokenExpiresAt == nil || !a.AccessTokenExpiresAt.After(time.Now()) {
				_ = s.Store.SetAccountError(ctx, id, status, message, cooldown)
			}
		}
		return a, err
	}
	version, err := s.Store.BeginAccountBalanceRefresh(ctx, id)
	if err != nil {
		return a, err
	}
	snapshotStartedAt := time.Now().UTC()
	tokens, err := client.GetTokens(ctx, session.AccessToken, a.TeamID, session.CognitoSub)
	if err != nil {
		s.markProxyRateLimited(ctx, a.ProxyURL, err)
		return a, err
	}
	tokenCipher, err := s.Cipher.Encrypt(session.AccessToken)
	if err != nil {
		return a, err
	}
	expiry := time.Unix(session.AccessTokenExpiry, 0)
	if _, err := s.Store.UpdateAccountSession(ctx, id, version, tokenCipher, "", expiry, snapshotStartedAt, session.HasuraUserID, session.CognitoSub, email, tokens.Plan, tokens.Subscription, tokens.Rollover, tokens.Paid, ""); err != nil {
		return a, err
	}
	return s.Store.GetAccount(ctx, id)
}

func classifySessionError(err error) (string, string, *time.Time) {
	var upstream *leonardo.HTTPError
	if errors.As(err, &upstream) {
		switch upstream.Status {
		case 401, 403:
			return "invalid", "Leonardo authentication expired or was rejected", nil
		case 429:
			until := time.Now().Add(2 * time.Minute)
			return "rate_limited", "Leonardo HTTP 429: temporary Vercel security checkpoint", &until
		}
	}
	until := time.Now().Add(5 * time.Minute)
	return "cooldown", err.Error(), &until
}

func (s *Service) Token(ctx context.Context, a domain.Account) (domain.Account, string, error) {
	latest, err := s.Store.GetAccount(ctx, a.ID)
	if err != nil {
		return a, "", err
	}
	if latest.AccessTokenCiphertext == "" || latest.AccessTokenExpiresAt == nil || !latest.AccessTokenExpiresAt.After(time.Now()) {
		return latest, "", ErrBrowserSessionRequired
	}
	token, err := s.Cipher.Decrypt(latest.AccessTokenCiphertext)
	return latest, token, err
}
