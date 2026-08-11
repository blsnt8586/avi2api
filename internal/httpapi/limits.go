package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type requestError struct {
	Status     int
	Code       string
	Message    string
	Details    any
	RetryAfter time.Duration
}

func (e *requestError) Error() string { return e.Message }

var admissionScript = redis.NewScript(`
for i=1,#KEYS do
  local limit=tonumber(ARGV[(i-1)*3+1])
  local cost=tonumber(ARGV[(i-1)*3+2])
  local current=tonumber(redis.call('get',KEYS[i]) or '0')
  if limit > 0 and current + cost > limit then
    local ttl=redis.call('pttl',KEYS[i])
    if ttl < 0 then ttl=tonumber(ARGV[(i-1)*3+3]) end
    return 'limit:' .. i .. ':' .. ttl
  end
end
for i=1,#KEYS do
  local cost=tonumber(ARGV[(i-1)*3+2])
  local ttl=tonumber(ARGV[(i-1)*3+3])
  local value=redis.call('incrby',KEYS[i],cost)
  if value == cost then redis.call('pexpire',KEYS[i],ttl) end
end
return 'ok'
`)

func (s *Server) admitIP(ctx context.Context, remoteAddr string) error {
	now := time.Now().UTC()
	minuteEnd := now.Truncate(time.Minute).Add(time.Minute)
	ip := clientIP(remoteAddr)
	h := sha256.Sum256([]byte(ip))
	keys := []string{"leo:limit:ip:minute:" + hex.EncodeToString(h[:8]) + ":" + now.Format("200601021504")}
	args := []any{s.Config.IPRateLimitPerMinute, 1, time.Until(minuteEnd).Milliseconds() + 1000}
	return s.runAdmission(ctx, keys, args, "rate_limit_exceeded", "IP request rate limit exceeded")
}

func (s *Server) admitAPIKeyRequest(ctx context.Context, keyID uuid.UUID) error {
	now := time.Now().UTC()
	minuteEnd := now.Truncate(time.Minute).Add(time.Minute)
	hourEnd := now.Truncate(time.Hour).Add(time.Hour)
	keys := []string{
		"leo:limit:key:minute:" + keyID.String() + ":" + now.Format("200601021504"),
		"leo:limit:key:hour:" + keyID.String() + ":" + now.Format("2006010215"),
	}
	args := []any{
		s.Config.RateLimitPerMinute, 1, time.Until(minuteEnd).Milliseconds() + 1000,
		s.Config.RateLimitPerHour, 1, time.Until(hourEnd).Milliseconds() + 1000,
	}
	return s.runAdmission(ctx, keys, args, "rate_limit_exceeded", "API key request rate limit exceeded")
}

func (s *Server) admitDailyQuota(ctx context.Context, keyID uuid.UUID, images int) error {
	now := time.Now().UTC()
	dayEnd := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	keys := []string{"leo:quota:key:day:" + keyID.String() + ":" + now.Format("20060102")}
	args := []any{s.Config.DailyImageLimit, images, time.Until(dayEnd).Milliseconds() + 1000}
	return s.runAdmission(ctx, keys, args, "daily_quota_exceeded", "daily generation quota exceeded")
}

func (s *Server) runAdmission(ctx context.Context, keys []string, args []any, code, message string) error {
	result, err := admissionScript.Run(ctx, s.Redis, keys, args...).Text()
	if err != nil {
		return err
	}
	if result == "ok" {
		return nil
	}
	parts := strings.Split(result, ":")
	if len(parts) != 3 {
		return fmt.Errorf("unexpected rate limiter response %q", result)
	}
	_, _ = strconv.Atoi(parts[1])
	ttlMS, _ := strconv.ParseInt(parts[2], 10, 64)
	return &requestError{Status: http.StatusTooManyRequests, Code: code, Message: message, RetryAfter: time.Duration(ttlMS) * time.Millisecond}
}

func (s *Server) rollbackDailyQuota(ctx context.Context, keyID uuid.UUID, images int) {
	if images <= 0 {
		return
	}
	key := "leo:quota:key:day:" + keyID.String() + ":" + time.Now().UTC().Format("20060102")
	_ = s.Redis.Eval(ctx, `local v=tonumber(redis.call('get',KEYS[1]) or '0');local n=tonumber(ARGV[1]);if v<=n then return redis.call('del',KEYS[1]) end return redis.call('decrby',KEYS[1],n)`, []string{key}, images).Err()
}

func clientIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

func clientIPFromRequest(r *http.Request) string {
	peer := clientIP(r.RemoteAddr)
	peerIP := net.ParseIP(peer)
	if peerIP == nil || !peerIP.IsLoopback() {
		return peer
	}
	for _, candidate := range []string{r.Header.Get("X-Real-IP"), strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]} {
		candidate = strings.TrimSpace(candidate)
		if parsed := net.ParseIP(candidate); parsed != nil {
			return parsed.String()
		}
	}
	return peer
}
