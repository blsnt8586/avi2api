package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/leonardo2api/leonardo2api/internal/adobe"
	"github.com/leonardo2api/leonardo2api/internal/creativefabrica"
	"github.com/leonardo2api/leonardo2api/internal/leonardo"
)

var ErrProxyControlUnavailable = errors.New("upstream proxy control window is unavailable")
var ErrGenerationPermissionBlocked = errors.New("generation permission is blocked")

// ProxyControlUnavailableError means a shared exit is pacing requests or is
// cooling down after an upstream 429. It is deliberately not an account error.
type ProxyControlUnavailableError struct {
	RetryAfter time.Duration
	Cooling    bool
}

func (e *ProxyControlUnavailableError) Error() string {
	if e.Cooling {
		return fmt.Sprintf("upstream proxy is cooling down; retry after %s", e.RetryAfter.Round(time.Second))
	}
	return fmt.Sprintf("upstream proxy request is paced; retry after %s", e.RetryAfter.Round(time.Second))
}

func (e *ProxyControlUnavailableError) Unwrap() error { return ErrProxyControlUnavailable }

func IsProxyControlUnavailable(err error) bool {
	return errors.Is(err, ErrProxyControlUnavailable)
}

func ProxyControlRetryAfter(err error) time.Duration {
	var unavailable *ProxyControlUnavailableError
	if errors.As(err, &unavailable) && unavailable.RetryAfter > 0 {
		return unavailable.RetryAfter
	}
	return 0
}

func IsUpstreamRateLimited(err error) bool {
	var upstream *leonardo.HTTPError
	if errors.As(err, &upstream) {
		return upstream.Status == 429
	}
	var gqlErr *leonardo.GraphQLError
	if errors.As(err, &gqlErr) {
		if status, ok := gqlErr.Extensions["statusCode"].(float64); ok && int(status) == 429 {
			return true
		}
		if code, _ := gqlErr.Extensions["code"].(string); strings.EqualFold(code, "RATE_LIMIT_EXCEEDED") {
			return true
		}
	}
	var adobeError *adobe.HTTPError
	if errors.As(err, &adobeError) && (adobeError.Status == 429 || adobeError.Status == 451) {
		return true
	}
	var cfHTTP *creativefabrica.HTTPError
	if errors.As(err, &cfHTTP) && (cfHTTP.Status == 429 || cfHTTP.Status == 451) {
		return true
	}
	var cfRPC *creativefabrica.RPCError
	if errors.As(err, &cfRPC) {
		if cfRPC.Status == 429 || cfRPC.Status == 451 {
			return true
		}
		code := strings.ToUpper(strings.TrimSpace(cfRPC.Code + " " + cfRPC.Message + " " + cfRPC.Body))
		return strings.Contains(code, "RESOURCE_EXHAUSTED") || strings.Contains(code, "RATE_LIMIT") || strings.Contains(code, "TOO MANY REQUEST")
	}
	var cfGraphQL *creativefabrica.GraphQLError
	if errors.As(err, &cfGraphQL) {
		for _, item := range cfGraphQL.Errors {
			code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(item.Extensions["code"])))
			if strings.Contains(code, "RESOURCE_EXHAUSTED") || strings.Contains(code, "RATE_LIMIT") || code == "TOO_MANY_REQUESTS" {
				return true
			}
			if status, ok := numericStatus(item.Extensions["statusCode"]); ok && (status == 429 || status == 451) {
				return true
			}
		}
	}
	return false
}

// IsGenerationPermissionBlocked recognizes account-level generation denials
// returned by Leonardo either as HTTP 403 or as a nested GraphQL HttpException.
// Authentication and balance queries can still succeed in this state.
func IsGenerationPermissionBlocked(err error) bool {
	if errors.Is(err, ErrGenerationPermissionBlocked) {
		return true
	}
	var upstream *leonardo.HTTPError
	if errors.As(err, &upstream) && upstream.Status == 403 {
		return true
	}
	var cfHTTP *creativefabrica.HTTPError
	if errors.As(err, &cfHTTP) && cfHTTP.Status == 403 {
		return true
	}
	var gqlErr *leonardo.GraphQLError
	if !errors.As(err, &gqlErr) {
		var cfGraphQL *creativefabrica.GraphQLError
		if !errors.As(err, &cfGraphQL) {
			var cfRPC *creativefabrica.RPCError
			if !errors.As(err, &cfRPC) {
				return false
			}
			text := strings.ToLower(cfRPC.Code + " " + cfRPC.Message + " " + cfRPC.Body)
			return strings.Contains(text, "permission_denied") || strings.Contains(text, "forbidden") || strings.Contains(text, "access denied")
		}
		for _, item := range cfGraphQL.Errors {
			if status, ok := numericStatus(item.Extensions["statusCode"]); ok && status == 403 {
				return true
			}
			text := strings.ToLower(item.Message + " " + fmt.Sprint(item.Extensions))
			if strings.Contains(text, "permission_denied") || strings.Contains(text, "forbidden") || strings.Contains(text, "access denied") {
				return true
			}
		}
		return false
	}
	if status, ok := gqlErr.Extensions["statusCode"].(float64); ok && int(status) == 403 {
		return true
	}
	if status, ok := gqlErr.Extensions["statusCode"].(int); ok && status == 403 {
		return true
	}
	text := strings.ToLower(gqlErr.Message + " " + fmt.Sprint(gqlErr.Extensions))
	for _, marker := range []string{"user is blocked", "access denied", "m004 suspended", "suspended"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func SanitizedUpstreamError(err error) string {
	var upstream *leonardo.HTTPError
	if errors.As(err, &upstream) {
		return fmt.Sprintf("Leonardo upstream HTTP %d", upstream.Status)
	}
	var adobeError *adobe.HTTPError
	if errors.As(err, &adobeError) {
		return fmt.Sprintf("Adobe upstream HTTP %d", adobeError.Status)
	}
	var cfHTTP *creativefabrica.HTTPError
	if errors.As(err, &cfHTTP) {
		return fmt.Sprintf("Creative Fabrica upstream HTTP %d", cfHTTP.Status)
	}
	var cfRPC *creativefabrica.RPCError
	if errors.As(err, &cfRPC) {
		if cfRPC.Code != "" {
			return fmt.Sprintf("Creative Fabrica RPC %s", cfRPC.Code)
		}
		return "Creative Fabrica RPC request failed"
	}
	var cfGraphQL *creativefabrica.GraphQLError
	if errors.As(err, &cfGraphQL) {
		return "Creative Fabrica GraphQL request failed"
	}
	if IsProxyControlUnavailable(err) {
		return err.Error()
	}
	return strings.TrimSpace(err.Error())
}

func numericStatus(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		n, err := typed.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		return n, err == nil
	default:
		return 0, false
	}
}

func proxyControlKey(proxyURL, suffix string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(proxyURL)))
	return "leo:proxy-control:" + fmt.Sprintf("%x", sum[:8]) + ":" + suffix
}

func redisTTL(ctx context.Context, client *redis.Client, key string) time.Duration {
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		return 0
	}
	return ttl
}

func maxDuration(values ...time.Duration) time.Duration {
	var maximum time.Duration
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

// acquireProxyControl serializes low-volume session and balance requests per
// configured egress. Separate proxy URLs remain fully concurrent.
func (s *Service) acquireProxyControl(ctx context.Context, proxyURL string) (func(), error) {
	if s.Redis == nil || strings.TrimSpace(proxyURL) == "" {
		return func() {}, nil
	}
	cooldownKey := proxyControlKey(proxyURL, "cooldown")
	if remaining := redisTTL(ctx, s.Redis, cooldownKey); remaining > 0 {
		return nil, &ProxyControlUnavailableError{RetryAfter: remaining, Cooling: true}
	}
	lockKey := proxyControlKey(proxyURL, "lock")
	paceKey := proxyControlKey(proxyURL, "pace")
	owner := uuid.NewString()
	ok, err := s.Redis.SetNX(ctx, lockKey, owner, s.Config.ProxyControlLease).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		retry := maxDuration(redisTTL(ctx, s.Redis, lockKey), redisTTL(ctx, s.Redis, paceKey), time.Second)
		return nil, &ProxyControlUnavailableError{RetryAfter: retry}
	}
	if remaining := redisTTL(ctx, s.Redis, paceKey); remaining > 0 {
		_, _ = s.Redis.Eval(ctx, `if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) end return 0`, []string{lockKey}, owner).Result()
		return nil, &ProxyControlUnavailableError{RetryAfter: remaining}
	}
	return func() {
		_, _ = s.Redis.Eval(ctx, `
if redis.call('get', KEYS[1]) == ARGV[1] then
  redis.call('del', KEYS[1])
  redis.call('psetex', KEYS[2], ARGV[2], 'paced')
  return 1
end
return 0`, []string{lockKey, paceKey}, owner, s.Config.ProxyControlMinInterval.Milliseconds()).Result()
	}, nil
}

func (s *Service) MarkProxyRateLimited(ctx context.Context, proxyURL string) {
	if s.Redis == nil || strings.TrimSpace(proxyURL) == "" || s.Config.Upstream429Cooldown <= 0 {
		return
	}
	_ = s.Redis.Set(ctx, proxyControlKey(proxyURL, "cooldown"), "429", s.Config.Upstream429Cooldown).Err()
}

func (s *Service) ProxyCooldownRemaining(ctx context.Context, proxyURL string) time.Duration {
	if s.Redis == nil || strings.TrimSpace(proxyURL) == "" {
		return 0
	}
	return redisTTL(ctx, s.Redis, proxyControlKey(proxyURL, "cooldown"))
}

// ProxyControlRemaining reports any active shared control-plane wait window.
// Browser jobs use it before opening Chrome so pacing does not become a false
// browser-login failure.
func (s *Service) ProxyControlRemaining(ctx context.Context, proxyURL string) time.Duration {
	if s.Redis == nil || strings.TrimSpace(proxyURL) == "" {
		return 0
	}
	return maxDuration(
		redisTTL(ctx, s.Redis, proxyControlKey(proxyURL, "cooldown")),
		redisTTL(ctx, s.Redis, proxyControlKey(proxyURL, "lock")),
		redisTTL(ctx, s.Redis, proxyControlKey(proxyURL, "pace")),
	)
}

func (s *Service) markProxyRateLimited(ctx context.Context, proxyURL string, err error) {
	if IsUpstreamRateLimited(err) {
		s.MarkProxyRateLimited(ctx, proxyURL)
	}
}
