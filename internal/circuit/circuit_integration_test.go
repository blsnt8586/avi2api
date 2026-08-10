package circuit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestSharedCircuitOpensProviderAndProxy(t *testing.T) {
	address := os.Getenv("LEO_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("LEO_TEST_REDIS_ADDR is not set")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	breaker := Breaker{Redis: client, Window: time.Minute, Cooldown: time.Minute, ProviderFailures: 2, ProxyFailures: 2}
	if _, open, err := breaker.Check(ctx, "leonardo", "http://proxy:7890"); err != nil || open {
		t.Fatalf("initial check open=%v err=%v", open, err)
	}
	if _, open, err := breaker.RecordFailure(ctx, "leonardo", "http://proxy:7890"); err != nil || open {
		t.Fatalf("first failure open=%v err=%v", open, err)
	}
	if until, open, err := breaker.RecordFailure(ctx, "leonardo", "http://proxy:7890"); err != nil || !open || !until.After(time.Now()) {
		t.Fatalf("second failure until=%v open=%v err=%v", until, open, err)
	}
	if _, open, err := breaker.CheckProvider(ctx, "leonardo"); err != nil || !open {
		t.Fatalf("provider check open=%v err=%v", open, err)
	}
	if _, open, err := breaker.Check(ctx, "leonardo", "http://proxy:7890"); err != nil || !open {
		t.Fatalf("proxy check open=%v err=%v", open, err)
	}
}
