package gateway

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/infra/runtime/memory"
)

func TestUnlimitedAccountSelectionTracksAndPreservesZero(t *testing.T) {
	ctx := context.Background()
	db, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "selector.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	repo := relational.NewAccountRepository(db)
	credential, _, err := repo.UpsertByIdentity(ctx, account.Credential{
		Provider: account.ProviderBuild, Name: "unlimited", SourceKey: "unlimited", EncryptedAccessToken: "encrypted",
		Enabled: true, AuthStatus: account.AuthStatusActive, MaxConcurrent: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	credential.MaxConcurrent = 0
	credential, err = repo.Update(ctx, credential)
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err = repo.UpsertByIdentity(ctx, credential)
	if err != nil || credential.MaxConcurrent != 0 {
		t.Fatalf("refresh changed zero: %d %v", credential.MaxConcurrent, err)
	}
	limiter := memory.NewConcurrencyLimiter()
	selector := NewSelector(repo, limiter, memory.NewStickyStore(), nil, time.Hour, time.Second, time.Minute)
	for range 40 {
		lease, err := selector.Acquire(ctx, account.ProviderBuild, 0, "", "", "", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Release()
		if lease.Credential.ID != credential.ID {
			t.Fatal("wrong selected account")
		}
	}
}
