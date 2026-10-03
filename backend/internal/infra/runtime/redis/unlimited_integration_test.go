package redis

import (
	"context"
	"os"
	"testing"
	"time"

	redisclient "github.com/redis/go-redis/v9"
)

func TestUnlimitedConcurrencyWithRedis(t *testing.T) {
	addr := os.Getenv("GROK_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires isolated Redis")
	}
	ctx := context.Background()
	client := redisclient.NewClient(&redisclient.Options{Addr: addr})
	defer client.Close()
	store := &Store{client: client, prefix: "test-unlimited:", concurrencyLease: time.Minute}
	releases := []func(){}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	for range 40 {
		release, ok, err := store.acquireConcurrency(ctx, "account", 0)
		if err != nil || !ok {
			t.Fatalf("acquire: %v %v", ok, err)
		}
		releases = append(releases, release)
	}
	if current, err := store.Current(ctx, "account"); err != nil || current != 40 {
		t.Fatalf("current: %d %v", current, err)
	}
	if _, ok, err := store.acquireConcurrency(ctx, "account", 8); err != nil || ok {
		t.Fatalf("lowered cap: %v %v", ok, err)
	}
	for _, release := range releases {
		release()
		release()
	}
	if current, err := store.Current(ctx, "account"); err != nil || current != 0 {
		t.Fatalf("released: %d %v", current, err)
	}
	release, ok, err := store.acquireConcurrency(ctx, "account", 1)
	if err != nil || !ok {
		t.Fatalf("reacquire: %v %v", ok, err)
	}
	release()
}
