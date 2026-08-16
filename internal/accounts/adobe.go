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

	"github.com/leonardo2api/leonardo2api/internal/adobe"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/providers"
	"github.com/leonardo2api/leonardo2api/internal/store"
)

func (s *Service) adobeClient(account domain.Account) (*adobe.Client, error) {
	if strings.TrimSpace(s.Config.AdobeSubmitAPIKey) == "" || strings.TrimSpace(s.Config.AdobeCreditsAPIKey) == "" {
		return nil, errors.New("Adobe provider API keys are not configured")
	}
	return adobe.New(account.ProxyURL, s.Config.AdobeSubmitAPIKey, s.Config.AdobeCreditsAPIKey, account.UserAgent)
}

func (s *Service) CreateAdobeWithAccessToken(ctx context.Context, name, email, accessToken, proxy string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int) (domain.Account, error) {
	return s.createAdobe(ctx, name, email, accessToken, nil, proxy, concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots)
}

func (s *Service) CreateAdobeWithCookieJSON(ctx context.Context, name, email string, raw json.RawMessage, proxy string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int) (domain.Account, error) {
	cookieJSON, err := adobe.NormalizeCookieJSON(raw)
	if err != nil {
		return domain.Account{}, err
	}
	cookieHeader, err := adobe.CookieHeaderFromJSON(cookieJSON)
	if err != nil {
		return domain.Account{}, err
	}
	client, err := adobe.New(proxy, s.Config.AdobeSubmitAPIKey, s.Config.AdobeCreditsAPIKey, "")
	if err != nil {
		return domain.Account{}, err
	}
	token, _, err := client.RefreshAccessToken(ctx, cookieHeader)
	if err != nil {
		return domain.Account{}, err
	}
	return s.createAdobe(ctx, name, email, token, cookieJSON, proxy, concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots)
}

func (s *Service) createAdobe(ctx context.Context, name, email, accessToken string, cookieJSON adobe.CookieJSON, proxy string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int) (domain.Account, error) {
	info, err := adobe.ParseAccessToken(accessToken)
	if err != nil {
		return domain.Account{}, err
	}
	if !info.ExpiresAt.After(time.Now().Add(time.Minute)) {
		return domain.Account{}, errors.New("Adobe access token is expired or expires too soon")
	}
	if !containsString(info.Scopes, "firefly_api") {
		return domain.Account{}, errors.New("Adobe access token does not include firefly_api scope")
	}
	client, err := adobe.New(proxy, s.Config.AdobeSubmitAPIKey, s.Config.AdobeCreditsAPIKey, "")
	if err != nil {
		return domain.Account{}, err
	}
	profile, err := client.Profile(ctx, accessToken)
	if err != nil {
		return domain.Account{}, fmt.Errorf("validate Adobe profile: %w", err)
	}
	credits, err := client.Credits(ctx, accessToken)
	if err != nil {
		return domain.Account{}, fmt.Errorf("validate Adobe credits: %w", err)
	}
	if profile.Email != "" {
		email = profile.Email
	}
	if strings.TrimSpace(name) == "" {
		name = profile.DisplayName
		if strings.TrimSpace(name) == "" {
			name = email
		}
	}
	if strings.TrimSpace(name) == "" {
		return domain.Account{}, errors.New("Adobe account name is required")
	}
	tokenCipher, err := s.Cipher.Encrypt(accessToken)
	if err != nil {
		return domain.Account{}, err
	}
	cookieHeaderCipher := ""
	cookieJSONCipher := ""
	if len(cookieJSON) > 0 {
		cookieHeader, headerErr := adobe.CookieHeaderFromJSON(cookieJSON)
		if headerErr != nil {
			return domain.Account{}, headerErr
		}
		cookieHeaderCipher, err = s.Cipher.Encrypt(cookieHeader)
		if err != nil {
			return domain.Account{}, err
		}
		cookieJSONCipher, err = s.Cipher.Encrypt(string(cookieJSON))
		if err != nil {
			return domain.Account{}, err
		}
	}
	account, err := s.Store.CreateProviderAccount(ctx, providers.Adobe, name, email, cookieHeaderCipher, "", false, proxy, "", concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots)
	if err != nil {
		return account, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = s.Store.DeleteAccount(context.Background(), account.ID)
		}
	}()
	if err := s.Store.SetAccountSessionRefreshEnabled(ctx, account.ID, len(cookieJSON) > 0); err != nil {
		return domain.Account{}, err
	}
	if cookieJSONCipher != "" {
		if err := s.Store.SetAccountCookieJSON(ctx, account.ID, cookieJSONCipher); err != nil {
			return domain.Account{}, err
		}
	}
	version, err := s.Store.BeginAccountBalanceRefresh(ctx, account.ID)
	if err != nil {
		return domain.Account{}, err
	}
	startedAt := time.Now().UTC()
	updated, err := s.Store.UpdateAccountSession(ctx, account.ID, version, tokenCipher, cookieHeaderCipher, info.ExpiresAt, startedAt, "", info.AccountID, email, "Adobe Firefly", credits.Available, 0, 0, "")
	if err != nil {
		return domain.Account{}, err
	}
	if !updated {
		return domain.Account{}, errors.New("Adobe balance snapshot was superseded during account creation")
	}
	cleanup = false
	return s.Store.GetAccount(ctx, account.ID)
}

func (s *Service) ImportAdobeCookieJSON(ctx context.Context, id uuid.UUID, raw json.RawMessage) (domain.Account, error) {
	account, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return account, err
	}
	if account.ProviderID != providers.Adobe {
		return account, errors.New("account is not an Adobe provider account")
	}
	cookieJSON, err := adobe.NormalizeCookieJSON(raw)
	if err != nil {
		return account, err
	}
	cookieHeader, err := adobe.CookieHeaderFromJSON(cookieJSON)
	if err != nil {
		return account, err
	}
	client, err := s.adobeClient(account)
	if err != nil {
		return account, err
	}
	token, _, err := client.RefreshAccessToken(ctx, cookieHeader)
	if err != nil {
		return account, err
	}
	cookieJSONCipher, err := s.Cipher.Encrypt(string(cookieJSON))
	if err != nil {
		return account, err
	}
	cookieHeaderCipher, err := s.Cipher.Encrypt(cookieHeader)
	if err != nil {
		return account, err
	}
	if err := s.Store.SetAccountCookieJSON(ctx, id, cookieJSONCipher); err != nil {
		return account, err
	}
	if err := s.Store.SetAccountCookieCiphertext(ctx, id, cookieHeaderCipher); err != nil {
		return account, err
	}
	if err := s.Store.SetAccountSessionRefreshEnabled(ctx, id, true); err != nil {
		return account, err
	}
	return s.refreshAdobeWithToken(ctx, account, token, client)
}

func (s *Service) refreshAdobe(ctx context.Context, account domain.Account, force bool) (domain.Account, error) {
	client, err := s.adobeClient(account)
	if err != nil {
		return account, err
	}
	if !force && account.AccessTokenCiphertext != "" && account.AccessTokenExpiresAt != nil && account.AccessTokenExpiresAt.After(time.Now().Add(s.Config.SessionRefreshAhead)) {
		token, decryptErr := s.Cipher.Decrypt(account.AccessTokenCiphertext)
		if decryptErr != nil {
			return account, decryptErr
		}
		return s.refreshAdobeBalance(ctx, account, token, client)
	}
	if account.CookieCiphertext == "" {
		if account.AccessTokenCiphertext == "" || account.AccessTokenExpiresAt == nil || !account.AccessTokenExpiresAt.After(time.Now()) {
			return account, ErrBrowserSessionRequired
		}
		token, decryptErr := s.Cipher.Decrypt(account.AccessTokenCiphertext)
		if decryptErr != nil {
			return account, decryptErr
		}
		return s.refreshAdobeBalance(ctx, account, token, client)
	}
	cookieHeader, err := s.Cipher.Decrypt(account.CookieCiphertext)
	if err != nil {
		return account, err
	}
	token, _, err := client.RefreshAccessToken(ctx, cookieHeader)
	if err != nil {
		return account, err
	}
	return s.refreshAdobeWithToken(ctx, account, token, client)
}

func (s *Service) refreshAdobeWithToken(ctx context.Context, account domain.Account, token string, client *adobe.Client) (domain.Account, error) {
	info, err := adobe.ParseAccessToken(token)
	if err != nil {
		return account, err
	}
	profile, err := client.Profile(ctx, token)
	if err != nil {
		return account, err
	}
	credits, err := client.Credits(ctx, token)
	if err != nil {
		return account, err
	}
	tokenCipher, err := s.Cipher.Encrypt(token)
	if err != nil {
		return account, err
	}
	version, err := s.Store.BeginAccountBalanceRefresh(ctx, account.ID)
	if err != nil {
		return account, err
	}
	startedAt := time.Now().UTC()
	updated, err := s.Store.UpdateAccountSession(ctx, account.ID, version, tokenCipher, "", info.ExpiresAt, startedAt, "", info.AccountID, profile.Email, "Adobe Firefly", credits.Available, 0, 0, "")
	if err != nil {
		return account, err
	}
	if !updated {
		return account, ErrStaleBalanceRefresh
	}
	return s.Store.GetAccount(ctx, account.ID)
}

func (s *Service) refreshAdobeBalance(ctx context.Context, account domain.Account, token string, client *adobe.Client) (domain.Account, error) {
	credits, err := client.Credits(ctx, token)
	if err != nil {
		return account, err
	}
	version, err := s.Store.BeginAccountBalanceRefresh(ctx, account.ID)
	if err != nil {
		return account, err
	}
	updated, err := s.Store.UpdateAccountTokens(ctx, account.ID, version, time.Now().UTC(), "Adobe Firefly", credits.Available, 0, 0)
	if err != nil {
		return account, err
	}
	if !updated {
		return account, ErrStaleBalanceRefresh
	}
	return s.Store.GetAccount(ctx, account.ID)
}

func (s *Service) RefreshAccount(ctx context.Context, id uuid.UUID, force bool) (domain.Account, error) {
	account, err := s.Store.GetAccount(ctx, id)
	if err != nil {
		return account, err
	}
	if account.ProviderID == providers.Adobe {
		return s.refreshAdobe(ctx, account, force)
	}
	return s.Refresh(ctx, id, force)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func IsAdobeAuthenticationRejected(err error) bool {
	var upstream *adobe.HTTPError
	return errors.As(err, &upstream) && (upstream.Status == http.StatusUnauthorized || upstream.Status == http.StatusForbidden)
}

func (s *Service) SyncAdobePricing(ctx context.Context, account domain.Account, token string) (int, error) {
	if account.ProviderID != providers.Adobe {
		return 0, errors.New("account is not an Adobe provider account")
	}
	client, err := s.adobeClient(account)
	if err != nil {
		return 0, err
	}
	version := "adobe-bks-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	pending := make([]domain.ModelCostRule, 0, 128)
	nextRequestAt := time.Time{}
	estimateCost := func(request adobe.CostRequest) (adobe.CostResult, error) {
		return estimateAdobePricingCost(ctx, client, token, request, &nextRequestAt, 2*time.Second, 15*time.Second)
	}
	for _, spec := range adobe.Models("image") {
		qualities := spec.Qualities
		if len(qualities) == 0 {
			qualities = []string{""}
		}
		for _, tier := range spec.ImageTiers {
			for _, quality := range qualities {
				request, requestErr := spec.ImageCostRequest(tier.Resolution, quality)
				if requestErr != nil {
					return 0, requestErr
				}
				cost, costErr := estimateCost(request)
				if costErr != nil {
					return 0, fmt.Errorf("read Adobe BKS price for %s %s %s: %w", spec.PublicID, tier.Resolution, quality, costErr)
				}
				pending = append(pending, domain.ModelCostRule{
					ProviderID: providers.Adobe, Kind: "image", Model: spec.PublicID, Size: tier.Size, Quality: quality,
					UnitTokens: cost.Credits, Enabled: true, PriceVersion: version, Source: "adobe-bks",
				})
			}
		}
	}
	for _, spec := range adobe.Models("video") {
		workflows := spec.Workflows
		if len(workflows) == 0 {
			workflows = []string{""}
		}
		costCache := make(map[string]int64)
		for _, duration := range spec.Durations {
			for _, resolution := range spec.Resolutions {
				for _, workflow := range workflows {
					cacheResolution := resolution
					if !spec.CostUsesResolution {
						cacheResolution = ""
					}
					cacheWorkflow := workflow
					if !spec.CostUsesWorkflow {
						cacheWorkflow = ""
					}
					cacheKey := fmt.Sprintf("%d|%s|%s", duration, cacheResolution, cacheWorkflow)
					credits, ok := costCache[cacheKey]
					if !ok {
						request, requestErr := spec.VideoCostRequest(duration, resolution, workflow)
						if requestErr != nil {
							return 0, requestErr
						}
						cost, costErr := estimateCost(request)
						if costErr != nil {
							return 0, fmt.Errorf("read Adobe BKS price for %s %ds %s %s: %w", spec.PublicID, duration, resolution, workflow, costErr)
						}
						credits = cost.Credits
						costCache[cacheKey] = credits
					}
					pending = append(pending, domain.ModelCostRule{
						ProviderID: providers.Adobe, Kind: "video", Model: spec.PublicID, Quality: workflow,
						Resolution: resolution, Duration: duration, UnitTokens: credits, Enabled: true, PriceVersion: version, Source: "adobe-bks",
					})
				}
			}
		}
	}
	for index, rule := range pending {
		if _, createErr := s.Store.CreateModelCostRule(ctx, rule); createErr != nil {
			return index, createErr
		}
	}
	return len(pending), nil
}

func estimateAdobePricingCost(ctx context.Context, client *adobe.Client, token string, request adobe.CostRequest, nextRequestAt *time.Time, interval, initialBackoff time.Duration) (adobe.CostResult, error) {
	wait := func(duration time.Duration) error {
		if duration <= 0 {
			return nil
		}
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if nextRequestAt != nil {
			if err := wait(time.Until(*nextRequestAt)); err != nil {
				return adobe.CostResult{}, err
			}
		}
		result, err := client.EstimateCost(ctx, token, request)
		if nextRequestAt != nil {
			*nextRequestAt = time.Now().Add(interval)
		}
		if err == nil {
			return result, nil
		}
		var upstream *adobe.HTTPError
		if !errors.As(err, &upstream) || upstream.Status != http.StatusTooManyRequests || attempt == 2 {
			return adobe.CostResult{}, err
		}
		backoff := initialBackoff * time.Duration(1<<attempt)
		if err := wait(backoff); err != nil {
			return adobe.CostResult{}, err
		}
	}
	return adobe.CostResult{}, errors.New("Adobe pricing retry budget exhausted")
}

func (s *Service) AdobeToken(ctx context.Context, account domain.Account) (domain.Account, string, error) {
	account, token, err := s.Token(ctx, account)
	if err != nil {
		return account, "", err
	}
	if account.ProviderID != providers.Adobe {
		return account, "", store.ErrNotFound
	}
	return account, token, nil
}
