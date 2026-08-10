package circuit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Breaker struct {
	Redis            *redis.Client
	Window           time.Duration
	Cooldown         time.Duration
	ProviderFailures int
	ProxyFailures    int
}

var recordFailureScript = redis.NewScript(`
local now=tonumber(ARGV[1])
local window=tonumber(ARGV[2])
local cooldown=tonumber(ARGV[3])
local provider_limit=tonumber(ARGV[4])
local proxy_limit=tonumber(ARGV[5])
local provider_count=redis.call('incr',KEYS[1])
if provider_count == 1 then redis.call('pexpire',KEYS[1],window) end
local proxy_count=redis.call('incr',KEYS[3])
if proxy_count == 1 then redis.call('pexpire',KEYS[3],window) end
local until_at=0
if provider_count >= provider_limit then
  until_at=now+cooldown
  redis.call('set',KEYS[2],until_at,'px',cooldown)
end
if proxy_count >= proxy_limit then
  local proxy_until=now+cooldown
  redis.call('set',KEYS[4],proxy_until,'px',cooldown)
  if proxy_until > until_at then until_at=proxy_until end
end
return tostring(until_at)
`)

func (b Breaker) Check(ctx context.Context, providerID, proxyURL string) (time.Time, bool, error) {
	values, err := b.Redis.MGet(ctx, providerOpenKey(providerID), proxyOpenKey(providerID, proxyURL)).Result()
	if err != nil {
		return time.Time{}, false, err
	}
	until, open := latestOpen(values)
	return until, open, nil
}

func (b Breaker) CheckProvider(ctx context.Context, providerID string) (time.Time, bool, error) {
	value, err := b.Redis.Get(ctx, providerOpenKey(providerID)).Result()
	if err == redis.Nil {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	until, open := latestOpen([]any{value})
	return until, open, nil
}

func (b Breaker) RecordFailure(ctx context.Context, providerID, proxyURL string) (time.Time, bool, error) {
	now := time.Now()
	result, err := recordFailureScript.Run(ctx, b.Redis, []string{
		providerFailureKey(providerID), providerOpenKey(providerID),
		proxyFailureKey(providerID, proxyURL), proxyOpenKey(providerID, proxyURL),
	}, now.UnixMilli(), b.Window.Milliseconds(), b.Cooldown.Milliseconds(), b.ProviderFailures, b.ProxyFailures).Text()
	if err != nil {
		return time.Time{}, false, err
	}
	untilMS, _ := strconv.ParseInt(result, 10, 64)
	if untilMS <= now.UnixMilli() {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(untilMS), true, nil
}

func latestOpen(values []any) (time.Time, bool) {
	var latest int64
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err == nil && parsed > latest {
			latest = parsed
		}
	}
	if latest <= time.Now().UnixMilli() {
		return time.Time{}, false
	}
	return time.UnixMilli(latest), true
}

func providerFailureKey(providerID string) string {
	return "aiv2api:circuit:provider:" + normalizedProvider(providerID) + ":failures"
}
func providerOpenKey(providerID string) string {
	return "aiv2api:circuit:provider:" + normalizedProvider(providerID) + ":open-until"
}
func proxyFailureKey(providerID, proxyURL string) string {
	return "aiv2api:circuit:proxy:" + normalizedProvider(providerID) + ":" + proxyID(proxyURL) + ":failures"
}
func proxyOpenKey(providerID, proxyURL string) string {
	return "aiv2api:circuit:proxy:" + normalizedProvider(providerID) + ":" + proxyID(proxyURL) + ":open-until"
}
func normalizedProvider(providerID string) string {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if providerID == "" {
		return "unknown"
	}
	return providerID
}
func proxyID(proxyURL string) string {
	proxyURL = strings.TrimSpace(strings.ToLower(proxyURL))
	if proxyURL == "" {
		proxyURL = "direct"
	}
	hash := sha256.Sum256([]byte(proxyURL))
	return hex.EncodeToString(hash[:8])
}
