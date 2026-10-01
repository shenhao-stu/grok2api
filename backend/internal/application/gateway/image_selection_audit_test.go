package gateway

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	accountapp "github.com/chenyme/grok2api/backend/internal/application/account"
	clientkeyapp "github.com/chenyme/grok2api/backend/internal/application/clientkey"
	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/clientkey"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
	"github.com/chenyme/grok2api/backend/internal/infra/runtime/memory"
)

type imageAuditQuotaAdapter struct{ webImageStreamAdapter }

func (*imageAuditQuotaAdapter) QuotaMode(string) string { return "fast" }

func TestImageSelectionAuditMatchesClientStatusWithoutCallingProvider(t *testing.T) {
	for _, reason := range []SelectionUnavailableReason{SelectionCooling, SelectionQuotaExhausted} {
		t.Run(string(reason), func(t *testing.T) {
			ctx := context.Background()
			db, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "image-audit.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.InitializeSchema(ctx); err != nil {
				t.Fatal(err)
			}
			accounts := relational.NewAccountRepository(db)
			models := relational.NewModelRepository(db)
			audits := relational.NewAuditRepository(db)
			now, until := time.Now().UTC(), time.Now().UTC().Add(time.Hour)
			credential := account.Credential{Provider: account.ProviderWeb, AuthType: account.AuthTypeSSO,
				WebTier: account.WebTierSuper, Name: "fixture", SourceKey: "fixture", EncryptedAccessToken: "synthetic",
				Enabled: true, AuthStatus: account.AuthStatusActive, MaxConcurrent: 1}
			if reason == SelectionCooling {
				credential.CooldownUntil = &until
			}
			credential, _, err = accounts.UpsertByIdentity(ctx, credential)
			if err != nil {
				t.Fatal(err)
			}
			const model = "image-audit-fixture"
			if err := models.UpsertRoutes(ctx, []modeldomain.Route{{PublicID: model, UpstreamModel: model,
				Provider: account.ProviderWeb, Capability: modeldomain.CapabilityImage, Enabled: true}}); err != nil {
				t.Fatal(err)
			}
			if err := models.ReplaceAccountCapabilities(ctx, credential.ID, []string{model}, now); err != nil {
				t.Fatal(err)
			}
			if reason == SelectionQuotaExhausted {
				if err := accounts.SaveQuotaWindows(ctx, credential.ID, account.WebTierSuper, now, []account.QuotaWindow{{
					AccountID: credential.ID, Mode: "fast", Remaining: 0, Total: 10, WindowSeconds: 3600,
					ResetAt: &until, SyncedAt: &now, Source: account.QuotaSourceUpstream,
				}}); err != nil {
					t.Fatal(err)
				}
			}
			adapter := &imageAuditQuotaAdapter{}
			registry := provider.NewRegistry(adapter)
			sticky := memory.NewStickyStore()
			accountService := accountapp.NewService(accounts, audits, memory.NewDeviceSessionStore(), sticky, registry, testCipher(t), nil)
			selector := NewSelector(accounts, memory.NewConcurrencyLimiter(), sticky, registry, time.Hour, time.Second, time.Minute)
			service := NewService(models, audits, accountService, clientkeyapp.NewService(nil, nil, nil, 60, 4, nil), registry, selector, relational.NewResponseRepository(db), 1)
			_, err = service.GenerateImage(ctx, ImageGenerationInput{RequestID: "fixture-request", ClientKey: clientkey.Key{ID: 1}, PublicModel: model, Prompt: "synthetic", Count: 1})
			var failure *SelectionUnavailableError
			if !errors.As(err, &failure) || failure.Reason != reason {
				t.Fatalf("selection error: %v", err)
			}
			rows, total, err := audits.List(ctx, 0, 10)
			if err != nil {
				t.Fatal(err)
			}
			if total != 1 || len(rows) != 1 || rows[0].StatusCode != http.StatusTooManyRequests || rows[0].StatusCode != failure.HTTPStatus() || rows[0].ErrorCode != failure.Code() {
				t.Fatalf("audit disagrees with client refusal: %#v", rows)
			}
			if rows[0].EstimatedCostInUSDTicks != 0 || rows[0].CostInUSDTicks != 0 || len(adapter.Attempts()) != 0 {
				t.Fatal("routing refusal charged or called upstream")
			}
		})
	}
}
