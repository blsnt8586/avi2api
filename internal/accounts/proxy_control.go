package accounts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/leonardo2api/leonardo2api/internal/leonardo"
)

var ErrProxyControlUnavailable = errors.New("upstream proxy control window is unavailable")

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
	return errors.As(err, &upstream) && upstream.Status == 429
}

func SanitizedUpstreamError(err error) string {
	var upstream *leonardo.HTTPError
	if errors.As(err, &upstream) {
		return fmt.Sprintf("Leonardo upstream HTTP %d", upstream.Status)
	}
	if IsProxyControlUnavailable(err) {
		return err.Error()
	}
	return strings.TrimSpace(err.Error())
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
