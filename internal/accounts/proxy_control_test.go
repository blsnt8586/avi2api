package accounts

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/leonardo2api/leonardo2api/internal/config"
)

func TestProxyControlKeysDoNotExposeProxyURL(t *testing.T) {
	proxyURL := "http://user:secret@127.0.0.1:7890"
	key := proxyControlKey(proxyURL, "lock")
	if key == "" || key == proxyURL {
		t.Fatalf("unexpected key %q", key)
	}
	if len(key) < len("leo:proxy-control::lock") || key == "leo:proxy-control::lock" {
		t.Fatalf("unexpected key format %q", key)
	}
}

func TestProxyControlSerializesSameProxy(t *testing.T) {
	redisAddr := os.Getenv("LEO_TEST_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("LEO_TEST_REDIS_ADDR is not set")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		Redis: client,
		Config: config.Config{
			ProxyControlMinInterval: 2 * time.Second,
			ProxyControlLease:       5 * time.Second,
			Upstream429Cooldown:     10 * time.Second,
		},
	}
	proxyURL := "http://proxy-control-fixture-" + uuid.NewString()
	otherProxyURL := proxyURL + "-other"
	cleanup := func(proxy string) {
		for _, suffix := range []string{"lock", "pace", "cooldown"} {
			_ = client.Del(ctx, proxyControlKey(proxy, suffix)).Err()
		}
	}
	defer cleanup(proxyURL)
	defer cleanup(otherProxyURL)

	release, err := service.acquireProxyControl(ctx, proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.acquireProxyControl(ctx, proxyURL); !IsProxyControlUnavailable(err) {
		t.Fatalf("same proxy acquisition error = %v", err)
	}
	otherRelease, err := service.acquireProxyControl(ctx, otherProxyURL)
	if err != nil {
		t.Fatalf("independent proxy should remain available: %v", err)
	}
	otherRelease()
	release()
	if _, err := service.acquireProxyControl(ctx, proxyURL); !IsProxyControlUnavailable(err) {
		t.Fatalf("paced proxy acquisition error = %v", err)
	}

	service.MarkProxyRateLimited(ctx, proxyURL)
	if remaining := service.ProxyCooldownRemaining(ctx, proxyURL); remaining <= 0 {
		t.Fatal("expected proxy cooldown after rate limit")
	}
}

func TestProxyControlUnavailableError(t *testing.T) {
	err := &ProxyControlUnavailableError{RetryAfter: 42 * time.Second, Cooling: true}
	if !IsProxyControlUnavailable(err) {
		t.Fatal("expected proxy control error to be detectable")
	}
	if !errors.Is(err, ErrProxyControlUnavailable) {
		t.Fatal("expected wrapped sentinel error")
	}
	if got := ProxyControlRetryAfter(err); got != 42*time.Second {
		t.Fatalf("retry after = %s, want 42s", got)
	}
}
