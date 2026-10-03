package relational

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
)

func TestAccountUnlimitedConstraintMigration(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "legacy-account-limit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	testAccountLimitMigration(t, ctx, database)
}

func TestAccountUnlimitedPostgresMigration(t *testing.T) {
	dsn := os.Getenv("GROK_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires an isolated PostgreSQL database")
	}
	ctx := context.Background()
	database, err := OpenPostgres(ctx, dsn, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	testAccountLimitMigration(t, ctx, database)
}

func testAccountLimitMigration(t *testing.T, ctx context.Context, database *Database) {
	t.Helper()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewAccountRepository(database)
	credential, _, err := repo.UpsertByIdentity(ctx, account.Credential{
		Provider: account.ProviderBuild, SourceKey: "migration-unlimited", Name: "migration-unlimited",
		EncryptedAccessToken: testEncryptedToken, AuthStatus: account.AuthStatusActive, Enabled: true, MaxConcurrent: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	db := database.db.WithContext(ctx)
	if database.dialect == "postgres" {
		if err := db.Exec("ALTER TABLE provider_accounts DROP CONSTRAINT chk_accounts_max_concurrent, ADD CONSTRAINT chk_accounts_max_concurrent CHECK (max_concurrent BETWEEN 1 AND 256)").Error; err != nil {
			t.Fatal(err)
		}
	} else {
		err := database.withSQLiteForeignKeysDisabled(ctx, func() error {
			var definition string
			if err := db.Raw("SELECT sql FROM sqlite_master WHERE name = 'provider_accounts'").Scan(&definition).Error; err != nil {
				return err
			}
			definition = strings.Replace(definition, "max_concurrent BETWEEN 0 AND 256", "max_concurrent BETWEEN 1 AND 256", 1)
			definition = strings.Replace(definition, "provider_accounts", "provider_accounts_legacy", 1)
			for index, statement := range []string{definition, "INSERT INTO provider_accounts_legacy SELECT * FROM provider_accounts", "DROP TABLE provider_accounts", "ALTER TABLE provider_accounts_legacy RENAME TO provider_accounts"} {
				if err := db.Exec(statement).Error; err != nil {
					t.Logf("legacy step %d, DDL prefix %.100s", index, definition)
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("UPDATE provider_accounts SET max_concurrent = 0 WHERE id = ?", credential.ID).Error; err == nil {
		t.Fatal("legacy constraint accepted zero")
	}
	for range 2 {
		if err := database.InitializeSchema(ctx); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE provider_accounts SET max_concurrent = 0 WHERE id = ?", credential.ID).Error; err != nil {
			t.Fatal(err)
		}
		stored, err := repo.Get(ctx, credential.ID)
		if err != nil || stored.MaxConcurrent != 0 || stored.EncryptedAccessToken != credential.EncryptedAccessToken {
			t.Fatal("migration failed to preserve account and credential")
		}
	}
	for _, invalid := range []int{-1, 257} {
		if err := db.Exec("UPDATE provider_accounts SET max_concurrent = ? WHERE id = ?", invalid, credential.ID).Error; err == nil {
			t.Fatalf("accepted %d", invalid)
		}
	}
}
