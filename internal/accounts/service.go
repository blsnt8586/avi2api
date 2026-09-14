package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/cryptox"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
	"github.com/leonardo2api/leonardo2api/internal/providers"
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
var ErrCookieImportSuperseded = errors.New("complete cookie import was superseded by a newer upload")

func IsBrowserSessionRequired(err error) bool {
	return errors.Is(err, ErrBrowserSessionRequired)
}

type BrowserSession struct {
	Verified              bool            `json:"verified"`
	AccessToken           string          `json:"access_token"`
	AccessTokenExpiry     int64           `json:"access_token_expiry"`
	CookieHeader          string          `json:"cookie_header,omitempty"`
	CookieJSON            json.RawMessage `json:"cookie_json,omitempty"`
	CookieJSONSource      string          `json:"cookie_json_source,omitempty"`
	CookieJSONFingerprint string          `json:"cookie_json_fingerprint,omitempty"`
	HasuraUserID          string          `json:"hasura_user_id"`
	CognitoSub            string          `json:"cognito_sub"`
	Email                 string          `json:"email"`
	UserAgent             string          `json:"user_agent"`
}

type BalanceRefreshResult struct {
	Account   domain.Account
	Version   int64
	StartedAt time.Time
}

// GenerationPermissionCheckDue keeps the entitlement probe alongside the
// session-refresh lifecycle while deduplicating repeated JWT refreshes.
func GenerationPermissionCheckDue(account domain.Account, interval time.Duration) bool {
	if account.ProviderID != providers.Leonardo {
		return false
	}
	if account.GenerationPermissionCheckedAt == nil {
		return true
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if account.GenerationPermissionStatus == "rate_limited" {
		interval = minDuration(interval, 5*time.Minute)
	} else if account.GenerationPermissionStatus == "error" {
		interval = minDuration(interval, 30*time.Minute)
	}
	return time.Since(*account.GenerationPermissionCheckedAt) >= interval
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
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

// CreateWithCompleteCookieJSON creates an account whose first browser refresh
// validates the supplied JSON. It stores no synthesized JSON and retains every
// browser cookie attribute required for later restores.
func (s *Service) CreateWithCompleteCookieJSON(ctx context.Context, name, email, password string, raw json.RawMessage, proxy, browserWorkerGroup string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int) (domain.Account, error) {
	cookieJSON, err := NormalizeBrowserCookieJSON(raw)
	if err != nil {
		return domain.Account{}, err
	}
	cookieHeader, err := CookieHeaderFromJSON(cookieJSON)
	if err != nil {
		return domain.Account{}, err
	}
	cookieCipher, err := s.Cipher.Encrypt(cookieHeader)
	if err != nil {
		return domain.Account{}, err
	}
	cookieJSONCipher, err := s.Cipher.Encrypt(string(cookieJSON))
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
	a, err := s.Store.CreateAccount(ctx, name, email, cookieCipher, credentialCipher, hasLoginCredentials, proxy, "", concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots)
	if err != nil {
		return a, err
	}
	if err := s.Store.SetAccountBrowserWorkerGroup(ctx, a.ID, browserWorkerGroup); err != nil {
		_ = s.Store.DeleteAccount(ctx, a.ID)
		return domain.Account{}, err
	}
	if err := s.Store.SetPendingAccountCookieJSON(ctx, a.ID, cookieJSONCipher, cookieJSONFingerprint(cookieJSON)); err != nil {
		_ = s.Store.DeleteAccount(ctx, a.ID)
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
	cookieHeader := strings.TrimSpace(session.CookieHeader)
	cookieJSONCipher := ""
	if len(session.CookieJSON) > 0 {
		if session.CookieJSONSource == "" {
			session.CookieJSONSource = "browser"
		}
		cookieJSON, normalizeErr := NormalizeBrowserCookieJSON(session.CookieJSON)
		if normalizeErr != nil {
			return a, normalizeErr
		}
		cookieHeader, err = CookieHeaderFromJSON(cookieJSON)
		if err != nil {
			return a, err
		}
		cookieJSONCipher, err = s.Cipher.Encrypt(string(cookieJSON))
		if err != nil {
			return a, err
		}
	}
	cookieCipher := ""
	if cookieHeader != "" {
		cookieCipher, err = s.Cipher.Encrypt(cookieHeader)
		if err != nil {
			return a, err
		}
	}
	expiry := time.Unix(session.AccessTokenExpiry, 0)
	promoteCookieJSON := session.CookieJSONSource == "pending"
	persistCookieJSON := session.CookieJSONSource == "pending" || session.CookieJSONSource == "active" || session.CookieJSONSource == "browser"
	if promoteCookieJSON && session.CookieJSONFingerprint == "" {
		return a, errors.New("pending complete cookie validation is missing its fingerprint")
	}
	updated, err := s.Store.UpdateAccountSessionCredentials(ctx, id, tokenCipher, cookieCipher, cookieJSONCipher, persistCookieJSON, promoteCookieJSON, session.CookieJSONFingerprint, expiry, session.HasuraUserID, session.CognitoSub, session.Email, session.UserAgent)
	if err != nil {
		return a, err
	}
	if !updated {
		return a, ErrCookieImportSuperseded
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
	if _, permissionErr := s.CheckGenerationPermission(ctx, result.Account.ID, false); permissionErr != nil && IsGenerationPermissionBlocked(permissionErr) {
		updated, _ := s.Store.GetAccount(ctx, result.Account.ID)
		return updated, permissionErr
	}
	return result.Account, nil
}

// ImportCompleteCookieJSON stages a complete Chrome or Patchright Cookie JSON
// document. It intentionally does not replace the current live session until
// the browser worker has obtained a valid fresh access token from Leonardo.
func (s *Service) ImportCompleteCookieJSON(ctx context.Context, id uuid.UUID, raw json.RawMessage) (domain.Account, error) {
	cookieJSON, err := NormalizeBrowserCookieJSON(raw)
	if err != nil {
		return domain.Account{}, err
	}
	ciphertext, err := s.Cipher.Encrypt(string(cookieJSON))
	if err != nil {
		return domain.Account{}, err
	}
	fingerprint := cookieJSONFingerprint(cookieJSON)
	if err := s.Store.SetPendingAccountCookieJSON(ctx, id, ciphertext, fingerprint); err != nil {
		return domain.Account{}, err
	}
	return s.Store.GetAccount(ctx, id)
}

// SessionCookie returns the decrypted browser cookie for a leased internal
// session worker. Callers must keep it in memory or short-lived private files.
func (s *Service) SessionCookie(ctx context.Context, id uuid.UUID) (string, error) {
	a, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return "", err
	}
	if a.CookieJSONCiphertext != "" {
		cookieJSON, err := s.Cipher.Decrypt(a.CookieJSONCiphertext)
		if err != nil {
			return "", err
		}
		return CookieHeaderFromJSON(BrowserCookieJSON(cookieJSON))
	}
	return s.Cipher.Decrypt(a.CookieCiphertext)
}

// SessionBrowserCookieJSON returns a complete browser-importable Cookie JSON.
// A staged import wins over the active value so that a retry can verify it
// without replacing the known-good session first.
func (s *Service) SessionBrowserCookieJSON(ctx context.Context, id uuid.UUID) (BrowserCookieJSON, string, string, error) {
	a, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return nil, "", "", err
	}
	ciphertext := a.PendingCookieJSONCiphertext
	source := "pending"
	fingerprint := a.PendingCookieJSONFingerprint
	if ciphertext == "" {
		ciphertext = a.CookieJSONCiphertext
		source = "active"
		fingerprint = ""
	}
	if ciphertext == "" {
		return nil, "", "", nil
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext)
	if err != nil {
		return nil, "", "", err
	}
	normalized, err := NormalizeBrowserCookieJSON(json.RawMessage(plaintext))
	if err != nil {
		return nil, "", "", err
	}
	return normalized, source, fingerprint, nil
}

func cookieJSONFingerprint(value BrowserCookieJSON) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("%x", digest[:])
}

func (s *Service) RefreshTokens(ctx context.Context, a domain.Account, token string) (domain.Account, error) {
	result, err := s.RefreshTokensAfter(ctx, a, token, time.Time{})
	if err != nil {
		return result.Account, err
	}
	if _, permissionErr := s.CheckGenerationPermission(ctx, result.Account.ID, false); permissionErr != nil && IsGenerationPermissionBlocked(permissionErr) {
		updated, _ := s.Store.GetAccount(ctx, result.Account.ID)
		return updated, permissionErr
	}
	return result.Account, nil
}

// CheckGenerationPermission reads Leonardo's account entitlement flags with a
// no-generation GraphQL query. This is the same account-status boundary used
// by the web application, so the check does not create a generation or consume
// output credits.
func (s *Service) CheckGenerationPermission(ctx context.Context, id uuid.UUID, force bool) (domain.Account, error) {
	a, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return a, err
	}
	if a.ProviderID != providers.Leonardo {
		return a, nil
	}
	if !force && !GenerationPermissionCheckDue(a, s.Config.GenerationPermissionCheckInterval) {
		return a, nil
	}
	if a.AccessTokenCiphertext == "" || a.AccessTokenExpiresAt == nil || !a.AccessTokenExpiresAt.After(time.Now()) {
		return a, ErrBrowserSessionRequired
	}
	token, err := s.Cipher.Decrypt(a.AccessTokenCiphertext)
	if err != nil {
		return a, err
	}
	release, err := s.acquireGenerationProbeControl(ctx, a.ProxyURL)
	if err != nil {
		return a, err
	}
	defer release()
	client, err := leonardo.New(a.ProxyURL, a.UserAgent, s.Config.SchemaVersion)
	if err != nil {
		return a, err
	}
	details, err := client.GetUserDetails(ctx, token, a.TeamID, a.CognitoSub)
	if err == nil {
		suspension := strings.ToLower(strings.TrimSpace(details.SuspensionStatus))
		if details.Blocked || (suspension != "" && suspension != "none" && suspension != "active") {
			message := "Leonardo account generation permission was denied"
			if details.Blocked {
				message = "Leonardo account is blocked"
			} else {
				message = fmt.Sprintf("Leonardo account suspension status: %s", details.SuspensionStatus)
			}
			_ = s.Store.SetGenerationPermission(ctx, a.ID, "blocked", "account-status", "generation_permission_denied", message)
			updated, _ := s.Store.GetAccount(ctx, a.ID)
			return updated, fmt.Errorf("%w: %s", ErrGenerationPermissionBlocked, message)
		}
		if storeErr := s.Store.SetGenerationPermission(ctx, a.ID, "verified", "account-status", "", ""); storeErr != nil {
			return a, storeErr
		}
		return s.Store.GetAccount(ctx, a.ID)
	}
	if IsGenerationPermissionBlocked(err) {
		_ = s.Store.SetGenerationPermission(ctx, a.ID, "blocked", "account-status", "generation_permission_denied", SanitizedUpstreamError(err))
		updated, _ := s.Store.GetAccount(ctx, a.ID)
		return updated, fmt.Errorf("%w: %v", ErrGenerationPermissionBlocked, SanitizedUpstreamError(err))
	}
	if IsUpstreamRateLimited(err) {
		_ = s.Store.SetGenerationPermission(ctx, a.ID, "rate_limited", "account-status", "upstream_rate_limited", SanitizedUpstreamError(err))
		return a, err
	}
	_ = s.Store.SetGenerationPermission(ctx, a.ID, "error", "account-status", "probe_error", SanitizedUpstreamError(err))
	return a, err
}

func (s *Service) acquireGenerationProbeControl(ctx context.Context, proxyURL string) (func(), error) {
	release, err := s.acquireProxyControl(ctx, proxyURL)
	if err == nil || s.Redis == nil {
		return release, err
	}
	var unavailable *ProxyControlUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Cooling || unavailable.RetryAfter <= 0 || unavailable.RetryAfter > 30*time.Second {
		return nil, err
	}
	timer := time.NewTimer(unavailable.RetryAfter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	return s.acquireProxyControl(ctx, proxyURL)
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
	a, err := s.refresh(ctx, id, force, true)
	if err != nil {
		return a, err
	}
	checked, permissionErr := s.CheckGenerationPermission(ctx, id, force)
	if permissionErr != nil && IsGenerationPermissionBlocked(permissionErr) {
		return checked, permissionErr
	}
	return checked, nil
}

// RefreshForScheduler uses a fresh JWT to finish deferred balance work. JWT
// renewal is handed to the browser worker because Leonardo checkpoints direct
// HTTP calls to get-session even when the Better Auth cookies are valid.
func (s *Service) RefreshForScheduler(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	a, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return a, err
	}
	if a.ProviderID == providers.Adobe {
		refreshed, refreshErr := s.refreshAdobe(ctx, a, false)
		if IsAdobeAuthenticationRejected(refreshErr) {
			_ = s.Store.SetAccountError(ctx, a.ID, "invalid", "Adobe Cookie or access token was rejected", nil)
		}
		return refreshed, refreshErr
	}
	if a.ProviderID == providers.CreativeFabrica {
		refreshed, refreshErr := s.refreshCreativeFabrica(ctx, a, false)
		if IsCreativeFabricaAuthenticationRejected(refreshErr) {
			_ = s.Store.SetAccountError(ctx, a.ID, "invalid", "Creative Fabrica session or token was rejected", nil)
		}
		return refreshed, refreshErr
	}
	if a.AccessTokenCiphertext == "" || a.AccessTokenExpiresAt == nil || !a.AccessTokenExpiresAt.After(time.Now().Add(s.Config.SessionRefreshMinFresh)) {
		return a, ErrBrowserSessionRequired
	}
	token, err := s.Cipher.Decrypt(a.AccessTokenCiphertext)
	if err != nil {
		return a, err
	}
	result, err := s.RefreshTokensAfter(ctx, a, token, time.Time{})
	if err != nil {
		return result.Account, err
	}
	if _, permissionErr := s.CheckGenerationPermission(ctx, result.Account.ID, false); permissionErr != nil && IsGenerationPermissionBlocked(permissionErr) {
		updated, _ := s.Store.GetAccount(ctx, result.Account.ID)
		return updated, permissionErr
	}
	return result.Account, nil
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
	if errors.Is(err, ErrCreativeFabricaBalanceUnverified) {
		return "invalid", ErrCreativeFabricaBalanceUnverified.Error(), nil
	}
	if IsCreativeFabricaAuthenticationRejected(err) {
		return "invalid", "Creative Fabrica session or token was rejected", nil
	}
	if IsAdobeAuthenticationRejected(err) {
		return "invalid", "Adobe session or token was rejected", nil
	}
	if IsUpstreamRateLimited(err) {
		until := time.Now().Add(2 * time.Minute)
		return "rate_limited", "upstream rate limit or security checkpoint", &until
	}
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
	if a.ProviderID == providers.CreativeFabrica {
		return s.CreativeFabricaToken(ctx, a)
	}
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
