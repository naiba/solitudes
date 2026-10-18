package model

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

func consentMigrationFixture(t *testing.T) (*gorm.DB, string, time.Time) {
	t.Helper()
	db := databasePolicyTestDB(t)
	cutoff := time.Now().UTC().Truncate(time.Second)
	db = db.Session(&gorm.Session{NowFunc: func() time.Time { return cutoff }})
	account := Account{Email: "consent@example.test", Nickname: "Consent"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"reader", "writer", "expired", "boundary", "history"} {
		policySQL(t, db, `INSERT INTO o_id_c_clients(id,name,redirect_uris_json) VALUES (?,?,'[]')`, client, client)
	}
	return db, account.ID, cutoff
}

func assertConsentScopes(t *testing.T, db *gorm.DB, want map[string]string) {
	t.Helper()
	var rows []OIDCConsent
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string)
	for _, row := range rows {
		got[row.AccountID+"/"+row.ClientID] = row.Scopes
		if row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			t.Errorf("consent lacks timestamps: %+v", row)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("consent scopes = %v, want %v", got, want)
	}
}

func assertConsentMigrationCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&DatabaseMigration{}).Where("name = ?", "oidc_persistent_consent").Count(&count).Error; err != nil || count != want {
		t.Fatalf("migration markers = %d, want %d: %v", count, want, err)
	}
}

func TestPostgresOIDCConsentMigrationImportsOnlyLiveCredentialUnion(t *testing.T) {
	db, accountID, cutoff := consentMigrationFixture(t)
	other := Account{Email: "other-consent@example.test", Nickname: "Other"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	for _, token := range []OIDCAccessToken{
		{AccountID: accountID, ClientID: "reader", Scopes: " profile\topenid  profile\n", ExpiresAt: cutoff.Add(time.Hour)},
		{AccountID: accountID, ClientID: "reader", Scopes: "forbidden", ExpiresAt: cutoff.Add(-time.Second)},
		{AccountID: accountID, ClientID: "writer", Scopes: "expired_access", ExpiresAt: cutoff.Add(-time.Hour)},
		{AccountID: accountID, ClientID: "expired", Scopes: "expired", ExpiresAt: cutoff.Add(-time.Second)},
		{AccountID: accountID, ClientID: "boundary", Scopes: "equal", ExpiresAt: cutoff},
		{AccountID: other.ID, ClientID: "reader", Scopes: "openid", ExpiresAt: cutoff.Add(time.Hour)},
	} {
		if err := db.Create(&token).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, token := range []OIDCRefreshToken{
		{AccountID: accountID, ClientID: "reader", Scopes: "email\nopenid\toffline_access", ExpiresAt: cutoff.Add(time.Hour)},
		{AccountID: accountID, ClientID: "writer", Scopes: "email", ExpiresAt: cutoff.Add(time.Hour)},
		{AccountID: accountID, ClientID: "expired", Scopes: "expired_refresh", ExpiresAt: cutoff.Add(-time.Second)},
		{AccountID: accountID, ClientID: "boundary", Scopes: "equal_refresh", ExpiresAt: cutoff},
	} {
		token.TokenHash, token.AccessID = uuid.NewString(), uuid.NewString()
		if err := db.Create(&token).Error; err != nil {
			t.Fatal(err)
		}
	}
	policySQL(t, db, `INSERT INTO o_id_c_auth_requests(client_id,account_id,request_json,approved,expires_at) VALUES ('history',?,'{"scope":"request_only"}',true,?)`, accountID, cutoff.Add(time.Hour))
	policySQL(t, db, `INSERT INTO audit_events(id,action,outcome,actor_id,client_id,created_at) VALUES ('legacy-login','oidc.login','success',?,'history',?)`, accountID, cutoff)
	policySQL(t, db, `INSERT INTO login_summaries(action,actor_id,client_id,login_count,last_login_at) VALUES ('oidc.login',?,'history',1,?)`, accountID, cutoff)
	var accessBefore, accessAfter []OIDCAccessToken
	var refreshBefore, refreshAfter []OIDCRefreshToken
	if err := db.Order("id").Find(&accessBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&refreshBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateOIDCConsent(db); err != nil {
		t.Fatal(err)
	}
	assertConsentScopes(t, db, map[string]string{
		accountID + "/reader": "email offline_access openid profile",
		accountID + "/writer": "email",
		other.ID + "/reader":  "openid",
	})
	assertConsentMigrationCount(t, db, 1)
	if err := db.Order("id").Find(&accessAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&refreshAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(accessBefore, accessAfter) || !reflect.DeepEqual(refreshBefore, refreshAfter) {
		t.Fatal("migration changed legacy credentials")
	}
	// Revocation must remain effective even while legacy credentials still exist.
	policySQL(t, db, `DELETE FROM o_id_c_consents`)
	policySQL(t, db, `DELETE FROM audit_events`)
	policySQL(t, db, `DELETE FROM login_summaries`)
	if err := MigrateOIDCConsent(db); err != nil {
		t.Fatal(err)
	}
	assertConsentScopes(t, db, map[string]string{})
	assertConsentMigrationCount(t, db, 1)
}

func TestPostgresOIDCConsentMigrationEmptyCompletionSurvivesMaintenance(t *testing.T) {
	db, accountID, cutoff := consentMigrationFixture(t)
	if err := MigrateOIDCConsent(db); err != nil {
		t.Fatal(err)
	}
	assertConsentMigrationCount(t, db, 1)
	policySQL(t, db, `INSERT INTO o_id_c_access_tokens(account_id,client_id,scopes,expires_at) VALUES (?,'reader','openid',?)`, accountID, cutoff.AddDate(2, 0, 0))
	if _, err := MaintainDatabase(t.Context(), db, cutoff.AddDate(1, 0, 0), 1); err != nil {
		t.Fatal(err)
	}
	if err := MigrateOIDCConsent(db); err != nil {
		t.Fatal(err)
	}
	assertConsentScopes(t, db, map[string]string{})
	assertConsentMigrationCount(t, db, 1)
}

func TestPostgresOIDCConsentMigrationRollsBackAndRetries(t *testing.T) {
	for _, failure := range []string{"consent", "marker"} {
		t.Run(failure, func(t *testing.T) {
			db, accountID, cutoff := consentMigrationFixture(t)
			policySQL(t, db, `INSERT INTO o_id_c_access_tokens(account_id,client_id,scopes,expires_at) VALUES (?,'reader','openid',?)`, accountID, cutoff.Add(time.Hour))
			table := "o_id_c_consents"
			if failure == "marker" {
				table = "database_migrations"
			}
			policySQL(t, db, "ALTER TABLE "+table+" ADD CONSTRAINT reject_consent_migration CHECK (false)")
			if err := MigrateOIDCConsent(db); err == nil {
				t.Fatal("migration accepted persistence failure")
			}
			assertConsentScopes(t, db, map[string]string{})
			assertConsentMigrationCount(t, db, 0)
			policySQL(t, db, "ALTER TABLE "+table+" DROP CONSTRAINT reject_consent_migration")
			if err := MigrateOIDCConsent(db); err != nil {
				t.Fatal(err)
			}
			assertConsentScopes(t, db, map[string]string{accountID + "/reader": "openid"})
			assertConsentMigrationCount(t, db, 1)
		})
	}
}

func TestPostgresOIDCConsentMigrationConcurrentStartup(t *testing.T) {
	db, accountID, cutoff := consentMigrationFixture(t)
	policySQL(t, db, `INSERT INTO o_id_c_access_tokens(account_id,client_id,scopes,expires_at) VALUES (?,'reader','openid',?)`, accountID, cutoff.Add(time.Hour))
	var workers sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 4)
	for range 4 {
		workers.Add(1)
		go func() { defer workers.Done(); <-start; results <- MigrateOIDCConsent(db) }()
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertConsentScopes(t, db, map[string]string{accountID + "/reader": "openid"})
	assertConsentMigrationCount(t, db, 1)
}

func TestPostgresOIDCConsentMigrationSchemaIsolation(t *testing.T) {
	db, accountID, cutoff := consentMigrationFixture(t)
	fallback, fallbackAccount, fallbackCutoff := consentMigrationFixture(t)
	policySQL(t, fallback, `INSERT INTO o_id_c_access_tokens(account_id,client_id,scopes,expires_at) VALUES (?,'reader','email',?)`, fallbackAccount, fallbackCutoff.Add(time.Hour))
	if err := MigrateOIDCConsent(fallback); err != nil {
		t.Fatal(err)
	}
	var localSchema, fallbackSchema string
	if err := db.Raw("SELECT current_schema()").Scan(&localSchema).Error; err != nil {
		t.Fatal(err)
	}
	if err := fallback.Raw("SELECT current_schema()").Scan(&fallbackSchema).Error; err != nil {
		t.Fatal(err)
	}
	policySQL(t, db, `INSERT INTO o_id_c_access_tokens(account_id,client_id,scopes,expires_at) VALUES (?,'reader','openid',?)`, accountID, cutoff.Add(time.Hour))
	withFallback := func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO " + pq.QuoteIdentifier(localSchema) + "," + pq.QuoteIdentifier(fallbackSchema) + ",public").Error; err != nil {
			return err
		}
		return MigrateOIDCConsent(tx)
	}
	// Missing local data or marker tables must not resolve into a fallback schema.
	for _, table := range []string{"o_id_c_access_tokens", "o_id_c_refresh_tokens", "o_id_c_consents", "database_migrations"} {
		t.Run(table, func(t *testing.T) {
			policySQL(t, db, "ALTER TABLE "+table+" RENAME TO held_consent_migration_table")
			err := db.Transaction(withFallback)
			// Restore before asserting so a red subtest cannot poison later cases.
			policySQL(t, db, "ALTER TABLE held_consent_migration_table RENAME TO "+table)
			if err == nil {
				t.Fatal("migration used a fallback table")
			}
			assertConsentMigrationCount(t, db, 0)
			assertConsentScopes(t, fallback, map[string]string{fallbackAccount + "/reader": "email"})
		})
	}
	// An unrelated schema's migration lock must not serialize this schema.
	locked := fallback.Begin()
	if locked.Error != nil {
		t.Fatal(locked.Error)
	}
	defer locked.Rollback()
	policySQL(t, locked, `LOCK TABLE database_migrations IN EXCLUSIVE MODE`)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Transaction(withFallback); err != nil {
		t.Fatal(err)
	}
	assertConsentScopes(t, db, map[string]string{accountID + "/reader": "openid"})
	assertConsentMigrationCount(t, db, 1)
	assertConsentScopes(t, fallback, map[string]string{fallbackAccount + "/reader": "email"})
}

func TestPostgresOIDCConsentConstraintsCascadeAndMaintenance(t *testing.T) {
	db, accountID, cutoff := consentMigrationFixture(t)
	if err := MigrateDatabasePolicy(db); err != nil {
		t.Fatal(err)
	}
	consent := OIDCConsent{AccountID: accountID, ClientID: "reader", Scopes: "openid"}
	if err := db.Create(&consent).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		row   OIDCConsent
		state string
	}{
		{consent, "23505"},
		{OIDCConsent{AccountID: uuid.NewString(), ClientID: "reader"}, "23503"},
		{OIDCConsent{AccountID: accountID, ClientID: "missing"}, "23503"},
	} {
		err := db.Create(&item.row).Error
		var state interface{ SQLState() string }
		if !errors.As(err, &state) || state.SQLState() != item.state {
			t.Fatalf("expected %s, got %v", item.state, err)
		}
	}
	var indexed bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND tablename='o_id_c_consents' AND indexdef LIKE '%(client_id)%')`).Scan(&indexed).Error; err != nil || !indexed {
		t.Fatalf("missing client index: %v", err)
	}
	if _, err := MaintainDatabase(t.Context(), db, cutoff.AddDate(1, 0, 0), 1); err != nil {
		t.Fatal(err)
	}
	assertConsentScopes(t, db, map[string]string{accountID + "/reader": "openid"})
	policySQL(t, db, `DELETE FROM o_id_c_clients WHERE id='reader'`)
	assertConsentScopes(t, db, map[string]string{})
	if err := db.Create(&OIDCConsent{AccountID: accountID, ClientID: "writer", Scopes: "email"}).Error; err != nil {
		t.Fatal(err)
	}
	policySQL(t, db, `DELETE FROM accounts WHERE id=?`, accountID)
	assertConsentScopes(t, db, map[string]string{})
}
