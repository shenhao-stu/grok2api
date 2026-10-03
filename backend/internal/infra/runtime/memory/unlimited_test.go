package memory

import (
	"context"
	"testing"
)

func TestUnlimitedConcurrencyStillTracksOwners(t *testing.T) {
	ctx := context.Background()
	limiter := NewConcurrencyLimiter()
	releases := make([]func(), 0, 40)
	for range 40 {
		release, ok, err := limiter.Acquire(ctx, "account:unlimited", 0)
		if err != nil || !ok {
			t.Fatalf("acquire: %v %v", ok, err)
		}
		releases = append(releases, release)
	}
	values, err := limiter.CurrentMany(ctx, []string{"account:unlimited"})
	if err != nil || values["account:unlimited"] != 40 {
		t.Fatalf("active leases: %v %v", values, err)
	}
	if _, ok, _ := limiter.Acquire(ctx, "account:unlimited", 8); ok {
		t.Fatal("lowering the cap ignored active owners")
	}
	for _, release := range releases {
		release()
		release()
	}
	values, _ = limiter.CurrentMany(ctx, []string{"account:unlimited"})
	if values["account:unlimited"] != 0 {
		t.Fatalf("leases after release: %v", values)
	}
	release, ok, err := limiter.Acquire(ctx, "account:unlimited", 1)
	if err != nil || !ok {
		t.Fatalf("bounded acquire after release: %v %v", ok, err)
	}
	release()
}
