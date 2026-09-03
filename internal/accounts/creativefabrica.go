package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/leonardo2api/leonardo2api/internal/creativefabrica"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/store"
)

// CreativeFabricaCredentialEnvelope is encrypted as one provider-specific
// credential bundle. The browser cookie JSON remains in the dedicated cookie
// column so it can be restored without reconstructing browser attributes.
// Tokens are never returned by an admin DTO.
type CreativeFabricaCredentialEnvelope struct {
	Email                 string                        `json:"email,omitempty"`
	Password              string                        `json:"password,omitempty"`
	SessionToken          string                        `json:"session_token,omitempty"`
	AccessToken           string                        `json:"access_token,omitempty"`
	RPCToken              string                        `json:"rpc_token,omitempty"`
	SessionTokenExpiresAt *time.Time                    `json:"session_token_expires_at,omitempty"`
	AccessTokenExpiresAt  *time.Time                    `json:"access_token_expires_at,omitempty"`
	RPCTokenExpiresAt     *time.Time                    `json:"rpc_token_expires_at,omitempty"`
	Challenge             *creativefabrica.OTPChallenge `json:"challenge,omitempty"`
}

// CreativeFabricaOTPRequiredError lets the admin API distinguish an email
// challenge from a rejected login without exposing a raw token.
type CreativeFabricaOTPRequiredError struct {
	Challenge creativefabrica.OTPChallenge
}

func (e *CreativeFabricaOTPRequiredError) Error() string {
	if strings.TrimSpace(e.Challenge.Message) != "" {
		return "Creative Fabrica requires an email OTP: " + e.Challenge.Message
	}
	return "Creative Fabrica requires an email OTP"
}

func IsCreativeFabricaOTPRequired(err error) bool {
	var challenge *CreativeFabricaOTPRequiredError
	return errors.As(err, &challenge)
}

func (s *Service) creativeFabricaClient(account domain.Account) (*creativefabrica.Client, error) {
	return creativefabrica.New(account.ProxyURL, account.UserAgent)
}

func (s *Service) CreateCreativeFabricaWithCookieJSON(ctx context.Context, name, email, password string, raw json.RawMessage, proxy string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int) (domain.Account, error) {
	cookieJSON, err := creativefabrica.NormalizeCookieJSON(raw)
	if err != nil {
		return domain.Account{}, err
	}
	cookieHeader, err := creativefabrica.CookieHeaderFromJSON(cookieJSON)
	if err != nil {
		return domain.Account{}, err
	}
	client, err := creativefabrica.New(proxy, "")
	if err != nil {
		return domain.Account{}, err
	}
	client.CookieHeader = cookieHeader
	tokens, err := s.authenticateCreativeFabricaCookie(ctx, client, cookieJSON, cookieHeader)
	if err != nil {
		return domain.Account{}, err
	}
	return s.createCreativeFabrica(ctx, name, email, password, proxy, concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots, cookieJSON, cookieHeader, tokens)
}

func (s *Service) CreateCreativeFabricaWithCredentials(ctx context.Context, name, email, password, otp, proxy string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int) (domain.Account, error) {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return domain.Account{}, errors.New("Creative Fabrica email and password are required")
	}
	client, err := creativefabrica.New(proxy, "")
	if err != nil {
		return domain.Account{}, err
	}
	login, err := client.LoginWithOTP(ctx, email, password, strings.TrimSpace(otp))
	if err != nil {
		return domain.Account{}, err
	}
	if login.Challenge != nil && strings.TrimSpace(login.SessionToken) == "" {
		challenge := *login.Challenge
		if challenge.Message == "" && len(login.Errors) > 0 {
			challenge.Message = login.Errors[0].Message
		}
		return domain.Account{}, &CreativeFabricaOTPRequiredError{Challenge: challenge}
	}
	if strings.TrimSpace(login.SessionToken) == "" {
		return domain.Account{}, errors.New("Creative Fabrica login returned no session token")
	}
	client.CookieHeader = login.CookieHeader
	tokens, err := client.ExchangeSessionTokenWithCookie(ctx, login.SessionToken, login.CookieHeader)
	if err != nil {
		return domain.Account{}, err
	}
	cookieJSON, cookieHeader, err := mergeCreativeFabricaCookies(login.CookieJSON, tokens.CookieJSON)
	if err != nil {
		return domain.Account{}, err
	}
	return s.createCreativeFabrica(ctx, name, email, password, proxy, concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots, cookieJSON, cookieHeader, tokens)
}

func (s *Service) authenticateCreativeFabricaCookie(ctx context.Context, client *creativefabrica.Client, cookieJSON creativefabrica.CookieJSON, cookieHeader string) (creativefabrica.TokenSet, error) {
	// A cookie export may contain the GraphQL token already. UserToken remains
	// the preferred path because it mirrors the Studio client and refreshes a
	// stale client-side token from the server session.
	sessionToken, err := client.UserToken(ctx, cookieHeader, "")
	if err != nil {
		if fallback := cookieToken(cookieJSON); fallback != "" {
			sessionToken = fallback
		} else {
			return creativefabrica.TokenSet{}, err
		}
	}
	return client.ExchangeSessionTokenWithCookie(ctx, sessionToken, cookieHeader)
}

func (s *Service) createCreativeFabrica(ctx context.Context, name, email, password, proxy string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int, cookieJSON creativefabrica.CookieJSON, cookieHeader string, tokens creativefabrica.TokenSet) (domain.Account, error) {
	if strings.TrimSpace(tokens.RPCToken) == "" {
		return domain.Account{}, errors.New("Creative Fabrica RPC token is empty")
	}
	client, err := creativefabrica.New(proxy, "")
	if err != nil {
		return domain.Account{}, err
	}
	client.CookieHeader = cookieHeader
	profile, profileErr := client.Profile(ctx, tokens.RPCToken)
	if profileErr != nil && tokens.SessionToken != "" {
		profile, profileErr = client.Profile(ctx, tokens.SessionToken)
	}
	if profileErr != nil {
		return domain.Account{}, fmt.Errorf("validate Creative Fabrica profile: %w", profileErr)
	}
	coins, err := client.Coins(ctx, tokens.RPCToken)
	if err != nil {
		return domain.Account{}, fmt.Errorf("validate Creative Fabrica coins: %w", err)
	}
	if profile.Email != "" {
		email = profile.Email
	}
	if strings.TrimSpace(name) == "" {
		name = profile.DisplayName
		if name == "" {
			name = email
		}
	}
	if strings.TrimSpace(name) == "" {
		return domain.Account{}, errors.New("Creative Fabrica account name is required")
	}

	if len(tokens.CookieJSON) > 0 {
		cookieJSON, cookieHeader, err = mergeCreativeFabricaCookies(cookieJSON, tokens.CookieJSON)
		if err != nil {
			return domain.Account{}, err
		}
	}
	if strings.TrimSpace(cookieHeader) == "" && strings.TrimSpace(tokens.CookieHeader) != "" {
		cookieHeader = tokens.CookieHeader
	}
	cookieCipher, cookieJSONCipher, err := s.encryptCreativeFabricaCookies(cookieHeader, cookieJSON)
	if err != nil {
		return domain.Account{}, err
	}
	envelope := creativeFabricaEnvelope(email, password, tokens)
	credentialCipher, err := s.encryptCreativeFabricaEnvelope(envelope)
	if err != nil {
		return domain.Account{}, err
	}
	account, err := s.Store.CreateProviderAccount(ctx, providers.CreativeFabrica, name, email, cookieCipher, credentialCipher, strings.TrimSpace(password) != "", proxy, "", concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots)
	if err != nil {
		return account, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = s.Store.DeleteAccount(context.Background(), account.ID)
		}
	}()
	if cookieJSONCipher != "" {
		if err := s.Store.SetAccountCookieJSON(ctx, account.ID, cookieJSONCipher); err != nil {
			return domain.Account{}, err
		}
	}
	if strings.TrimSpace(email) == "" {
		email = profile.Email
	}
	if err := s.Store.SetAccountSessionRefreshEnabled(ctx, account.ID, cookieJSONCipher != "" || strings.TrimSpace(password) != ""); err != nil {
		return domain.Account{}, err
	}
	version, err := s.Store.BeginAccountBalanceRefresh(ctx, account.ID)
	if err != nil {
		return domain.Account{}, err
	}
	startedAt := time.Now().UTC()
	expiry := tokenExpiry(tokens)
	tokenCipher, err := s.Cipher.Encrypt(tokens.RPCToken)
	if err != nil {
		return domain.Account{}, err
	}
	updated, err := s.Store.UpdateAccountSession(ctx, account.ID, version, tokenCipher, "", expiry, startedAt, profile.ID, "", email, "Creative Fabrica Studio", coins.Available, 0, 0, "")
	if err != nil {
		return domain.Account{}, err
	}
	if !updated {
		return domain.Account{}, errors.New("Creative Fabrica balance snapshot was superseded during account creation")
	}
	cleanup = false
	return s.Store.GetAccount(ctx, account.ID)
}

func (s *Service) ImportCreativeFabricaCookieJSON(ctx context.Context, id uuid.UUID, raw json.RawMessage) (domain.Account, error) {
	account, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return account, err
	}
	if account.ProviderID != providers.CreativeFabrica {
		return account, errors.New("account is not a Creative Fabrica provider account")
	}
	newCookieJSON, err := creativefabrica.NormalizeCookieJSON(raw)
	if err != nil {
		return account, err
	}
	newCookieHeader, err := creativefabrica.CookieHeaderFromJSON(newCookieJSON)
	if err != nil {
		return account, err
	}
	client, err := s.creativeFabricaClient(account)
	if err != nil {
		return account, err
	}
	client.CookieHeader = newCookieHeader
	tokens, err := s.authenticateCreativeFabricaCookie(ctx, client, newCookieJSON, newCookieHeader)
	if err != nil {
		return account, err
	}
	profile, profileErr := client.Profile(ctx, tokens.RPCToken)
	if profileErr != nil {
		return account, profileErr
	}
	coins, err := client.Coins(ctx, tokens.RPCToken)
	if err != nil {
		return account, err
	}
	storedCookieJSON, storedCookieErr := s.creativeFabricaCookieJSON(ctx, id)
	if storedCookieErr != nil {
		return account, storedCookieErr
	}
	cookieJSON, cookieHeader, err := mergeCreativeFabricaCookies(storedCookieJSON, newCookieJSON, tokens.CookieJSON)
	if err != nil {
		return account, err
	}
	if strings.TrimSpace(cookieHeader) == "" {
		cookieHeader = newCookieHeader
	}
	cookieCipher, cookieJSONCipher, err := s.encryptCreativeFabricaCookies(cookieHeader, cookieJSON)
	if err != nil {
		return account, err
	}
	envelope, _, credentialErr := s.creativeFabricaCredentials(ctx, id)
	if credentialErr != nil {
		return account, credentialErr
	}
	envelope.SessionToken = tokens.SessionToken
	envelope.AccessToken = tokens.AccessToken
	envelope.RPCToken = tokens.RPCToken
	envelope.SessionTokenExpiresAt = timePointer(tokens.SessionInfo.ExpiresAt)
	envelope.AccessTokenExpiresAt = timePointer(tokens.AccessInfo.ExpiresAt)
	envelope.RPCTokenExpiresAt = timePointer(tokens.RPCInfo.ExpiresAt)
	if profile.Email != "" {
		envelope.Email = profile.Email
	}
	credentialCipher, err := s.encryptCreativeFabricaEnvelope(envelope)
	if err != nil {
		return account, err
	}
	if err := s.Store.SetAccountCookieJSON(ctx, id, cookieJSONCipher); err != nil {
		return account, err
	}
	if err := s.Store.SetAccountCookieCiphertext(ctx, id, cookieCipher); err != nil {
		return account, err
	}
	if err := s.Store.SetAccountCredentialCiphertext(ctx, id, credentialCipher, envelope.Email != "" && envelope.Password != ""); err != nil {
		return account, err
	}
	if err := s.Store.SetAccountSessionRefreshEnabled(ctx, id, true); err != nil {
		return account, err
	}
	version, err := s.Store.BeginAccountBalanceRefresh(ctx, id)
	if err != nil {
		return account, err
	}
	tokenCipher, err := s.Cipher.Encrypt(tokens.RPCToken)
	if err != nil {
		return account, err
	}
	updated, err := s.Store.UpdateAccountSession(ctx, id, version, tokenCipher, "", tokenExpiry(tokens), time.Now().UTC(), profile.ID, "", profile.Email, "Creative Fabrica Studio", coins.Available, 0, 0, "")
	if err != nil {
		return account, err
	}
	if !updated {
		return account, ErrStaleBalanceRefresh
	}
	return s.Store.GetAccount(ctx, id)
}

func (s *Service) refreshCreativeFabrica(ctx context.Context, account domain.Account, force bool) (domain.Account, error) {
	envelope, _, credentialErr := s.creativeFabricaCredentials(ctx, account.ID)
	if credentialErr != nil {
		return account, credentialErr
	}
	client, err := s.creativeFabricaClient(account)
	if err != nil {
		return account, err
	}
	var tokens creativefabrica.TokenSet
	if !force && account.AccessTokenCiphertext != "" && account.AccessTokenExpiresAt != nil && account.AccessTokenExpiresAt.After(time.Now().Add(s.Config.SessionRefreshAhead)) {
		token, decryptErr := s.Cipher.Decrypt(account.AccessTokenCiphertext)
		if decryptErr != nil {
			return account, decryptErr
		}
		tokens = tokenSetFromStored(envelope, token)
	} else {
		cookieJSON, cookieErr := s.creativeFabricaCookieJSON(ctx, account.ID)
		cookieHeader := ""
		if cookieErr == nil && len(cookieJSON) > 0 {
			cookieHeader, cookieErr = creativefabrica.CookieHeaderFromJSON(cookieJSON)
		}
		if cookieErr != nil {
			return account, cookieErr
		}
		if cookieHeader == "" && account.CookieCiphertext != "" {
			cookieHeader, cookieErr = s.Cipher.Decrypt(account.CookieCiphertext)
			if cookieErr != nil {
				return account, cookieErr
			}
		}
		client.CookieHeader = cookieHeader
		loginFallback := func() (creativefabrica.TokenSet, error) {
			if envelope.Email == "" || envelope.Password == "" {
				return creativefabrica.TokenSet{}, ErrBrowserSessionRequired
			}
			login, loginErr := client.Login(ctx, envelope.Email, envelope.Password)
			if loginErr != nil {
				return creativefabrica.TokenSet{}, loginErr
			}
			if login.Challenge != nil && login.SessionToken == "" {
				return creativefabrica.TokenSet{}, &CreativeFabricaOTPRequiredError{Challenge: *login.Challenge}
			}
			if strings.TrimSpace(login.SessionToken) == "" {
				return creativefabrica.TokenSet{}, errors.New("Creative Fabrica login returned no session token")
			}
			loginTokens, exchangeErr := client.ExchangeSessionTokenWithCookie(ctx, login.SessionToken, login.CookieHeader)
			if exchangeErr != nil {
				return creativefabrica.TokenSet{}, exchangeErr
			}
			if len(login.CookieJSON) > 0 || len(loginTokens.CookieJSON) > 0 {
				mergedJSON, mergedHeader, mergeErr := mergeCreativeFabricaCookies(login.CookieJSON, loginTokens.CookieJSON)
				if mergeErr != nil {
					return creativefabrica.TokenSet{}, mergeErr
				}
				loginTokens.CookieJSON, loginTokens.CookieHeader = mergedJSON, mergedHeader
			}
			return loginTokens, nil
		}
		if cookieHeader != "" {
			sessionToken, tokenErr := client.UserToken(ctx, cookieHeader, "")
			if tokenErr == nil {
				tokens, tokenErr = client.ExchangeSessionTokenWithCookie(ctx, sessionToken, cookieHeader)
				if tokenErr == nil {
					// The cookie path is authoritative when it succeeds.
					// A failed exchange falls through to the stored token or
					// password recovery below instead of stranding the account.
				} else if envelope.RPCToken != "" && envelope.RPCTokenExpiresAt != nil && envelope.RPCTokenExpiresAt.After(time.Now()) {
					tokens = tokenSetFromStored(envelope, envelope.RPCToken)
				} else {
					tokens, tokenErr = loginFallback()
				}
			} else if envelope.RPCToken != "" && envelope.RPCTokenExpiresAt != nil && envelope.RPCTokenExpiresAt.After(time.Now()) {
				tokens = tokenSetFromStored(envelope, envelope.RPCToken)
			} else {
				tokens, tokenErr = loginFallback()
			}
		} else if envelope.Email != "" && envelope.Password != "" {
			tokens, err = loginFallback()
			if err != nil {
				return account, err
			}
		} else if envelope.RPCToken != "" && envelope.RPCTokenExpiresAt != nil && envelope.RPCTokenExpiresAt.After(time.Now()) {
			tokens = tokenSetFromStored(envelope, envelope.RPCToken)
		} else {
			return account, ErrBrowserSessionRequired
		}
	}
	return s.refreshCreativeFabricaWithTokens(ctx, account, client, envelope, tokens)
}

func (s *Service) refreshCreativeFabricaWithTokens(ctx context.Context, account domain.Account, client *creativefabrica.Client, envelope CreativeFabricaCredentialEnvelope, tokens creativefabrica.TokenSet) (domain.Account, error) {
	if strings.TrimSpace(client.CookieHeader) == "" {
		if cookieHeader, cookieErr := s.CreativeFabricaCookieHeader(ctx, account.ID); cookieErr != nil {
			return account, cookieErr
		} else {
			client.CookieHeader = cookieHeader
		}
	}
	profile, err := client.Profile(ctx, tokens.RPCToken)
	if err != nil && tokens.SessionToken != "" {
		profile, err = client.Profile(ctx, tokens.SessionToken)
	}
	if err != nil {
		return account, err
	}
	coins, err := client.Coins(ctx, tokens.RPCToken)
	if err != nil {
		return account, err
	}
	envelope.SessionToken = tokens.SessionToken
	envelope.AccessToken = tokens.AccessToken
	envelope.RPCToken = tokens.RPCToken
	envelope.SessionTokenExpiresAt = timePointer(tokens.SessionInfo.ExpiresAt)
	envelope.AccessTokenExpiresAt = timePointer(tokens.AccessInfo.ExpiresAt)
	envelope.RPCTokenExpiresAt = timePointer(tokens.RPCInfo.ExpiresAt)
	if profile.Email != "" {
		envelope.Email = profile.Email
	}
	if len(tokens.CookieJSON) > 0 {
		storedCookieJSON, storedCookieErr := s.creativeFabricaCookieJSON(ctx, account.ID)
		if storedCookieErr != nil {
			return account, storedCookieErr
		}
		cookieJSON, cookieHeader, mergeErr := mergeCreativeFabricaCookies(storedCookieJSON, tokens.CookieJSON)
		if mergeErr != nil {
			return account, mergeErr
		}
		cookieCipher, cookieJSONCipher, encryptErr := s.encryptCreativeFabricaCookies(cookieHeader, cookieJSON)
		if encryptErr != nil {
			return account, encryptErr
		}
		if cookieJSONCipher != "" {
			if err := s.Store.SetAccountCookieJSON(ctx, account.ID, cookieJSONCipher); err != nil {
				return account, err
			}
		}
		if cookieCipher != "" {
			if err := s.Store.SetAccountCookieCiphertext(ctx, account.ID, cookieCipher); err != nil {
				return account, err
			}
		}
		client.CookieHeader = cookieHeader
	}
	credentialCipher, err := s.encryptCreativeFabricaEnvelope(envelope)
	if err != nil {
		return account, err
	}
	if err := s.Store.SetAccountCredentialCiphertext(ctx, account.ID, credentialCipher, envelope.Email != "" && envelope.Password != ""); err != nil {
		return account, err
	}
	version, err := s.Store.BeginAccountBalanceRefresh(ctx, account.ID)
	if err != nil {
		return account, err
	}
	tokenCipher, err := s.Cipher.Encrypt(tokens.RPCToken)
	if err != nil {
		return account, err
	}
	updated, err := s.Store.UpdateAccountSession(ctx, account.ID, version, tokenCipher, "", tokenExpiry(tokens), time.Now().UTC(), profile.ID, "", profile.Email, "Creative Fabrica Studio", coins.Available, 0, 0, "")
	if err != nil {
		return account, err
	}
	if !updated {
		return account, ErrStaleBalanceRefresh
	}
	return s.Store.GetAccount(ctx, account.ID)
}

func (s *Service) CreativeFabricaToken(ctx context.Context, account domain.Account) (domain.Account, string, error) {
	latest, err := s.Store.GetAccount(ctx, account.ID)
	if err != nil {
		return account, "", err
	}
	if latest.ProviderID != providers.CreativeFabrica {
		return latest, "", store.ErrNotFound
	}
	if latest.AccessTokenCiphertext != "" && latest.AccessTokenExpiresAt != nil && latest.AccessTokenExpiresAt.After(time.Now()) {
		token, decryptErr := s.Cipher.Decrypt(latest.AccessTokenCiphertext)
		return latest, token, decryptErr
	}
	refreshed, refreshErr := s.refreshCreativeFabrica(ctx, latest, true)
	if refreshErr != nil {
		return refreshed, "", refreshErr
	}
	token, decryptErr := s.Cipher.Decrypt(refreshed.AccessTokenCiphertext)
	return refreshed, token, decryptErr
}

func (s *Service) CreativeFabricaCredentials(ctx context.Context, id uuid.UUID) (CreativeFabricaCredentialEnvelope, error) {
	envelope, _, err := s.creativeFabricaCredentials(ctx, id)
	return envelope, err
}

// CreativeFabricaCookieHeader returns the decrypted first-party Cookie header
// for a task-scoped client. The complete JSON remains encrypted at rest; this
// method only exposes its derived header to the worker that is about to make a
// provider request.
func (s *Service) CreativeFabricaCookieHeader(ctx context.Context, id uuid.UUID) (string, error) {
	cookieJSON, err := s.creativeFabricaCookieJSON(ctx, id)
	if err != nil {
		return "", err
	}
	if len(cookieJSON) > 0 {
		return creativefabrica.CookieHeaderFromJSON(cookieJSON)
	}
	account, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(account.CookieCiphertext) == "" {
		return "", nil
	}
	return s.Cipher.Decrypt(account.CookieCiphertext)
}

// SetCreativeFabricaLoginCredential updates only the optional email/password
// fallback inside the provider envelope. Session, access, RPC tokens and the
// complete cookie export stay intact so an admin edit cannot invalidate a
// currently usable account by replacing the envelope with the Leonardo shape.
func (s *Service) SetCreativeFabricaLoginCredential(ctx context.Context, id uuid.UUID, email, password string) error {
	account, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return err
	}
	if account.ProviderID != providers.CreativeFabrica {
		return errors.New("account is not a Creative Fabrica provider account")
	}
	envelope, _, err := s.creativeFabricaCredentials(ctx, id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(password) == "" {
		envelope.Email = ""
		envelope.Password = ""
	} else {
		email = strings.TrimSpace(email)
		if email == "" {
			return errors.New("Creative Fabrica email is required when password is configured")
		}
		envelope.Email = email
		envelope.Password = password
	}
	ciphertext, err := s.encryptCreativeFabricaEnvelope(envelope)
	if err != nil {
		return err
	}
	return s.Store.SetAccountCredentialCiphertext(ctx, id, ciphertext, envelope.Email != "" && envelope.Password != "")
}

func (s *Service) creativeFabricaCredentials(ctx context.Context, id uuid.UUID) (CreativeFabricaCredentialEnvelope, bool, error) {
	ciphertext, configured, err := s.Store.GetAccountCredentialCiphertext(ctx, id)
	if err != nil {
		return CreativeFabricaCredentialEnvelope{}, false, err
	}
	if ciphertext == "" {
		return CreativeFabricaCredentialEnvelope{}, configured, nil
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext)
	if err != nil {
		return CreativeFabricaCredentialEnvelope{}, configured, err
	}
	var envelope CreativeFabricaCredentialEnvelope
	if err := json.Unmarshal([]byte(plaintext), &envelope); err != nil {
		return CreativeFabricaCredentialEnvelope{}, configured, err
	}
	return envelope, configured, nil
}

func (s *Service) encryptCreativeFabricaEnvelope(envelope CreativeFabricaCredentialEnvelope) (string, error) {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	return s.Cipher.Encrypt(string(raw))
}

func (s *Service) encryptCreativeFabricaCookies(cookieHeader string, cookieJSON creativefabrica.CookieJSON) (string, string, error) {
	cookieCipher := ""
	var err error
	if strings.TrimSpace(cookieHeader) != "" {
		cookieCipher, err = s.Cipher.Encrypt(cookieHeader)
		if err != nil {
			return "", "", err
		}
	}
	cookieJSONCipher := ""
	if len(cookieJSON) > 0 {
		cookieJSONCipher, err = s.Cipher.Encrypt(string(cookieJSON))
		if err != nil {
			return "", "", err
		}
	}
	return cookieCipher, cookieJSONCipher, nil
}

func creativeFabricaEnvelope(email, password string, tokens creativefabrica.TokenSet) CreativeFabricaCredentialEnvelope {
	return CreativeFabricaCredentialEnvelope{
		Email: email, Password: password, SessionToken: tokens.SessionToken, AccessToken: tokens.AccessToken, RPCToken: tokens.RPCToken,
		SessionTokenExpiresAt: timePointer(tokens.SessionInfo.ExpiresAt), AccessTokenExpiresAt: timePointer(tokens.AccessInfo.ExpiresAt), RPCTokenExpiresAt: timePointer(tokens.RPCInfo.ExpiresAt),
	}
}

func tokenSetFromStored(envelope CreativeFabricaCredentialEnvelope, token string) creativefabrica.TokenSet {
	return creativefabrica.TokenSet{
		SessionToken: envelope.SessionToken, AccessToken: envelope.AccessToken, RPCToken: token,
		SessionInfo: creativefabrica.ParseTokenInfo(envelope.SessionToken), AccessInfo: creativefabrica.ParseTokenInfo(envelope.AccessToken), RPCInfo: creativefabrica.ParseTokenInfo(token),
	}
}

func tokenExpiry(tokens creativefabrica.TokenSet) time.Time {
	expiry := tokens.RPCInfo.ExpiresAt
	if expiry.IsZero() {
		expiry = tokens.AccessInfo.ExpiresAt
	}
	if expiry.IsZero() {
		expiry = time.Now().Add(30 * time.Minute)
	}
	return expiry
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

func (s *Service) creativeFabricaCookieJSON(ctx context.Context, id uuid.UUID) (creativefabrica.CookieJSON, error) {
	account, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(account.CookieJSONCiphertext) == "" {
		return nil, nil
	}
	plaintext, err := s.Cipher.Decrypt(account.CookieJSONCiphertext)
	if err != nil {
		return nil, err
	}
	return creativefabrica.NormalizeCookieJSON(json.RawMessage(plaintext))
}

func mergeCreativeFabricaCookies(documents ...creativefabrica.CookieJSON) (creativefabrica.CookieJSON, string, error) {
	merged, err := creativefabrica.MergeCookieJSON(documents...)
	if err != nil {
		return nil, "", err
	}
	if len(merged) == 0 {
		return nil, "", nil
	}
	header, err := creativefabrica.CookieHeaderFromJSON(merged)
	if err != nil {
		return nil, "", err
	}
	return merged, header, nil
}

func cookieToken(raw creativefabrica.CookieJSON) string {
	var cookies []map[string]any
	if json.Unmarshal(raw, &cookies) != nil {
		return ""
	}
	for _, cookie := range cookies {
		name := strings.ToLower(strings.TrimSpace(fmt.Sprint(cookie["name"])))
		if name == "cftoken" || name == "session_token" || name == "sessiontoken" {
			return strings.TrimSpace(fmt.Sprint(cookie["value"]))
		}
	}
	return ""
}

func IsCreativeFabricaAuthenticationRejected(err error) bool {
	var upstream *creativefabrica.HTTPError
	if errors.As(err, &upstream) && (upstream.Status == http.StatusUnauthorized || upstream.Status == http.StatusForbidden) {
		return true
	}
	var rpc *creativefabrica.RPCError
	if errors.As(err, &rpc) {
		if rpc.Status == http.StatusUnauthorized || rpc.Status == http.StatusForbidden {
			return true
		}
		text := strings.ToLower(rpc.Code + " " + rpc.Message + " " + rpc.Body)
		for _, marker := range []string{"unauthenticated", "unauthorized", "permission_denied", "forbidden", "invalid token", "token expired", "session expired"} {
			if strings.Contains(text, marker) {
				return true
			}
		}
	}
	var gql *creativefabrica.GraphQLError
	if errors.As(err, &gql) {
		for _, item := range gql.Errors {
			code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(item.Extensions["code"])))
			if code == "UNAUTHENTICATED" || code == "FORBIDDEN" || code == "UNAUTHORIZED" || code == "PERMISSION_DENIED" {
				return true
			}
			text := strings.ToLower(item.Message + " " + fmt.Sprint(item.Extensions))
			for _, marker := range []string{"invalid token", "token expired", "session expired", "not authenticated"} {
				if strings.Contains(text, marker) {
					return true
				}
			}
		}
	}
	return false
}
