package relational

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDashboardWebQuotaMustBeFreshAndPositive(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "web-quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err = database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := accountModel{IdentityKey: testIdentityKey("web-quota-dashboard"), Name: "web-quota", SourceKey: "web-quota", Provider: "grok_web", Enabled: true, AuthStatus: "active", CreatedAt: now, UpdatedAt: now}
	if err = database.db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		remaining     int
		synced, reset time.Time
		active        int64
	}{
		{"exhausted", 0, now, now.Add(time.Hour), 0},
		{"available", 1, now, now.Add(time.Hour), 1},
		{"stale", 1, now.Add(-16 * time.Minute), now.Add(time.Hour), 0},
		{"reset-unconfirmed", 1, now, now.Add(-time.Minute), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			q := quotaWindowModel{AccountID: account.ID, Mode: "weekly", Remaining: test.remaining, Total: 10000, Source: "upstream", SyncedAt: &test.synced, ResetAt: &test.reset, UpdatedAt: now}
			if err = database.db.Save(&q).Error; err != nil {
				t.Fatal(err)
			}
			snapshot, err := NewDashboardRepository(database).Snapshot(ctx, testDashboardWindow(testDashboardBoundaries(now.Add(-24*time.Hour), 2*time.Hour, 12)), now)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Resources.ActiveAccounts != test.active || snapshot.Resources.TotalAccounts != 1 {
				t.Fatal("Web quota does not match availability")
			}
		})
	}
}
