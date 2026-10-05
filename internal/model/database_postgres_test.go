package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func databasePolicyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("SOLITUDES_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SOLITUDES_TEST_POSTGRES_DSN for isolated PostgreSQL tests")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*config)
	t.Cleanup(func() { _ = admin.Close() })
	schema := "solitudes_policy_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pq.QuoteIdentifier(schema)
	if _, err := admin.Exec("CREATE SCHEMA " + quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + quoted + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	config.RuntimeParams["search_path"] = schema + ",public"
	conn := stdlib.OpenDB(*config)
	conn.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE FUNCTION uuid_generate_v4() RETURNS uuid LANGUAGE SQL AS 'SELECT gen_random_uuid()'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Account{}, &Article{}, &ArticleHistory{}, &Comment{}, &LoginSession{}, &EmailAction{}, &ExternalIdentity{}, &Passkey{}, &PasskeyCeremony{}, &OAuthAttempt{}, &OIDCClient{}, &OIDCAuthRequest{}, &OIDCAccessToken{}, &OIDCRefreshToken{}, &OIDCSigningKey{}, &OIDCCryptoKey{}, &AuditEvent{}, &LoginSummary{}, &FeedVisit{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func policySQL(t *testing.T, db *gorm.DB, sql string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("fixture SQL failed: %v", err)
	}
}

func TestPostgresDatabaseConstraintsAndIndexes(t *testing.T) {
	db := databasePolicyTestDB(t)
	// Model migrations do not discard data just to make a new check pass.
	policySQL(t, db, `INSERT INTO accounts(email,nickname,role) VALUES ('bad@test','Bad','root')`)
	if err := MigrateDatabasePolicy(db); err == nil {
		t.Fatal("migration accepted invalid existing role")
	}
	policySQL(t, db, `UPDATE accounts SET role='user' WHERE email='bad@test'`)
	for i := 0; i < 2; i++ {
		if err := MigrateDatabasePolicy(db); err != nil {
			t.Fatal(err)
		}
	}
	var indexes []struct{ Indexname, Indexdef string }
	if err := db.Raw(`SELECT indexname, indexdef FROM pg_indexes WHERE schemaname=current_schema()`).Scan(&indexes).Error; err != nil {
		t.Fatal(err)
	}
	definitions := map[string]string{}
	for _, idx := range indexes {
		definitions[idx.Indexname] = idx.Indexdef
	}
	for name, fragment := range map[string]string{
		"idx_articles_slug": "UNIQUE", "idx_article_history_version": "UNIQUE",
		"idx_articles_tags_gin": "USING gin", "idx_comments_thread_page": "WHERE (is_spam = false)",
		"idx_articles_author_page": "author_id, created_at DESC, id DESC", "idx_clients_owner_page": "owner_id, created_at DESC, id",
		"idx_audit_page": "created_at DESC, id DESC", "idx_email_actions_expires_at": "expires_at",
		"idx_passkey_ceremonies_expires_at": "expires_at", "idx_o_auth_attempts_expires_at": "expires_at",
	} {
		if !strings.Contains(definitions[name], fragment) {
			t.Errorf("index %s missing %q: %s", name, fragment, definitions[name])
		}
	}
	for _, name := range []string{"idx_articles_tags", "idx_article_histories_article_id", "idx_article_histories_version", "idx_nickname", "idx_articles_author_id", "idx_o_id_c_clients_owner_id"} {
		if definitions[name] != "" {
			t.Errorf("redundant index survived: %s", name)
		}
	}
	account := Account{Email: "owner@test", Nickname: "Owner"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	article := Article{Slug: "unique", Title: "Unique", AuthorID: &account.ID}
	if err := db.Create(&article).Error; err != nil {
		t.Fatal(err)
	}
	policySQL(t, db, `INSERT INTO article_histories(article_id,version) VALUES (?,1)`, article.ID)
	for _, sql := range []string{
		`INSERT INTO articles(slug) VALUES ('unique')`,
		`INSERT INTO articles(slug) VALUES (NULL)`,
		`UPDATE accounts SET role='superuser'`,
		`UPDATE articles SET visibility='everyone'`,
		`INSERT INTO article_histories(article_id,version) SELECT id,1 FROM articles WHERE slug='unique'`,
		`INSERT INTO login_sessions(account_id,token_hash,expires_at) VALUES ('00000000-0000-4000-8000-000000000000','orphan',now())`,
		`INSERT INTO o_id_c_clients(id,owner_id,name,redirect_uris_json) VALUES ('orphan','00000000-0000-4000-8000-000000000000','orphan','[]')`,
	} {
		if err := db.Exec(sql).Error; err == nil {
			t.Errorf("constraint accepted: %s", sql)
		}
	}
	client := OIDCClient{ID: "client", OwnerID: &account.ID, Name: "Client", RedirectURIsJSON: "[]"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	policySQL(t, db, `INSERT INTO o_id_c_access_tokens(client_id,account_id,expires_at) VALUES ('client',?,now())`, account.ID)
	if err := db.Delete(&account).Error; err == nil {
		t.Fatal("owner with content/apps was deleted")
	}
	// Every authentication ownership link is enforced, not just the indexes.
	orphan := "00000000-0000-4000-8000-000000000000"
	for _, record := range []interface{}{
		&Article{Slug: "orphan-last-editor", UpdatedByID: &orphan},
		&ArticleHistory{ArticleID: article.ID, EditorID: &orphan, Version: 2},
		&ArticleHistory{ArticleID: article.ID, UpdatedByID: &orphan, Version: 2},
		&EmailAction{AccountID: orphan, TokenHash: "orphan-email"},
		&ExternalIdentity{AccountID: orphan, Provider: "test", Subject: "orphan"},
		&Passkey{AccountID: orphan, CredentialID: []byte("orphan"), PublicKey: []byte("key")},
		&PasskeyCeremony{AccountID: orphan, CookieHash: "orphan", SessionJSON: []byte(`{}`)},
		&OAuthAttempt{AccountID: &orphan, StateHash: "orphan"},
		&OIDCAuthRequest{AccountID: &orphan, ClientID: client.ID, RequestJSON: []byte(`{}`)},
		&OIDCAuthRequest{AccountID: &account.ID, ClientID: "missing-client", RequestJSON: []byte(`{}`)},
		&OIDCAccessToken{AccountID: orphan, ClientID: client.ID},
		&OIDCAccessToken{AccountID: account.ID, ClientID: "missing-client"},
		&OIDCRefreshToken{AccountID: orphan, ClientID: client.ID, TokenHash: "orphan", AccessID: uuid.NewString()},
		&OIDCRefreshToken{AccountID: account.ID, ClientID: "missing-client", TokenHash: "orphan", AccessID: uuid.NewString()},
	} {
		err := db.Create(record).Error
		var state interface{ SQLState() string }
		if !errors.As(err, &state) || state.SQLState() != "23503" {
			t.Errorf("%T: expected FK violation, got %v", record, err)
		}
	}
	policySQL(t, db, `DELETE FROM o_id_c_clients WHERE id='client'`)
	var count int64
	db.Model(&OIDCAccessToken{}).Count(&count)
	if count != 0 {
		t.Fatal("client deletion left access tokens")
	}
	// Concurrent requests cannot bypass handler prechecks to claim one slug.
	var workers sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- db.Create(&Article{Slug: "raced", AuthorID: &account.ID}).Error
		}()
	}
	workers.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("concurrent slug creation succeeded %d times", created)
	}
}

func TestPostgresMigrationDoesNotTouchFallbackSchema(t *testing.T) {
	db := databasePolicyTestDB(t)
	var schema string
	if err := db.Raw(`SELECT current_schema()`).Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	fallback := schema + "_fallback"
	quoted := pq.QuoteIdentifier(fallback)
	policySQL(t, db, "CREATE SCHEMA "+quoted)
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA " + quoted + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	policySQL(t, db, "CREATE TABLE "+quoted+".sentinel (id integer)")
	policySQL(t, db, "CREATE INDEX idx_articles_tags ON "+quoted+".sentinel(id)")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO " + pq.QuoteIdentifier(schema) + "," + quoted + ",public").Error; err != nil {
			return err
		}
		return MigrateDatabasePolicy(tx)
	}); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := db.Raw(`SELECT to_regclass(?) IS NOT NULL`, fallback+".idx_articles_tags").Scan(&exists).Error; err != nil || !exists {
		t.Fatalf("migration modified fallback schema: %t %v", exists, err)
	}
	// The periodic cleanup must also fail closed if a local table is missing,
	// never delete a similarly named table farther down search_path.
	policySQL(t, db, "CREATE TABLE "+quoted+".o_auth_attempts (id uuid PRIMARY KEY, expires_at timestamptz)")
	policySQL(t, db, "INSERT INTO "+quoted+".o_auth_attempts VALUES (gen_random_uuid(), now()-interval '1 day')")
	policySQL(t, db, "DROP TABLE "+pq.QuoteIdentifier(schema)+".o_auth_attempts")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO " + pq.QuoteIdentifier(schema) + "," + quoted + ",public").Error; err != nil {
			return err
		}
		_, err := MaintainDatabase(t.Context(), tx, time.Now(), 0)
		return err
	})
	if err == nil {
		t.Fatal("maintenance fell through to fallback table")
	}
	var count int64
	if err := db.Raw("SELECT count(*) FROM " + quoted + ".o_auth_attempts").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("maintenance changed fallback data: %d %v", count, err)
	}
}

func TestPostgresDatabaseMaintenanceExpiryAndRetention(t *testing.T) {
	db := databasePolicyTestDB(t)
	if err := MigrateDatabasePolicy(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := Account{Email: "maintenance@test", Nickname: "Keeper"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	policySQL(t, db, `INSERT INTO o_id_c_clients(id,owner_id,name,redirect_uris_json) VALUES ('client',?,'Client','[]')`, account.ID)
	for i, expires := range []time.Time{now.Add(-time.Hour), now, now.Add(time.Hour)} {
		fixtures := []interface{}{
			&LoginSession{AccountID: account.ID, TokenHash: fmt.Sprint(i), ExpiresAt: expires},
			&EmailAction{AccountID: account.ID, TokenHash: fmt.Sprint(i), Purpose: "verify", ExpiresAt: expires},
			&OAuthAttempt{StateHash: fmt.Sprint(i), AccountID: &account.ID, ExpiresAt: expires},
			&PasskeyCeremony{AccountID: account.ID, CookieHash: fmt.Sprint(i), SessionJSON: []byte(`{}`), ExpiresAt: expires},
			&OIDCAuthRequest{ClientID: "client", AccountID: &account.ID, RequestJSON: []byte(`{}`), ExpiresAt: expires},
			&OIDCAccessToken{ClientID: "client", AccountID: account.ID, ExpiresAt: expires},
			&OIDCRefreshToken{ClientID: "client", AccountID: account.ID, TokenHash: fmt.Sprint(i), AccessID: uuid.NewString(), ExpiresAt: expires},
			&FeedVisit{IP: "127.0.0.1", CreatedAt: expires.AddDate(0, 0, -7)},
		}
		for _, record := range fixtures {
			if err := db.Create(record).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	old := now.AddDate(0, 0, -100)
	policySQL(t, db, `INSERT INTO audit_events(id,created_at,action,outcome) VALUES ('old',?,'request.error','failure'),('audit-initialized',?,'audit.enabled','success')`, old, old)
	n, err := MaintainDatabase(t.Context(), db, now, 0)
	if err != nil || n != 16 {
		t.Fatalf("expiry count %d: %v", n, err)
	}
	for _, table := range []string{"login_sessions", "email_actions", "o_auth_attempts", "passkey_ceremonies", "o_id_c_auth_requests", "o_id_c_access_tokens", "o_id_c_refresh_tokens", "feed_visits", "accounts", "o_id_c_clients"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 1 {
			t.Errorf("%s: %d %v", table, count, err)
		}
	}
	var count int64
	db.Model(&AuditEvent{}).Count(&count)
	if count != 2 {
		t.Fatal("default policy removed audit details")
	}
	n, err = MaintainDatabase(t.Context(), db, now, 90)
	if err != nil || n != 1 {
		t.Fatalf("retention %d %v", n, err)
	}
	db.Model(&AuditEvent{}).Where("id='audit-initialized'").Count(&count)
	if count != 1 {
		t.Fatal("audit start marker removed")
	}
	n, err = MaintainDatabase(t.Context(), db, now, 90)
	if err != nil || n != 0 {
		t.Fatalf("repeat cleanup: %d %v", n, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := MaintainDatabase(ctx, db, now, 90); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestPostgresAuditCompactionAtomicConcurrentAndBounded(t *testing.T) {
	db := databasePolicyTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	policySQL(t, db, `INSERT INTO audit_events(id,created_at,action,outcome,client_id,actor_id)
	 SELECT 'old-'||n, ?, 'oidc.login','success','client','user' FROM generate_series(1,2501) n`, now.AddDate(0, 0, -100))
	policySQL(t, db, `INSERT INTO audit_events(id,created_at,action,outcome,client_id,actor_id) VALUES ('recent',?,'oidc.login','success','client','user')`, now)
	// The same SQL statement rolls back deleted details if summary persistence fails.
	policySQL(t, db, `ALTER TABLE login_summaries ADD CONSTRAINT test_failure CHECK (login_count < 0)`)
	if _, err := CompactAuditBatch(db, now.AddDate(0, 0, -90)); err == nil {
		t.Fatal("expected compaction failure")
	}
	var count int64
	db.Model(&AuditEvent{}).Count(&count)
	if count != 2502 {
		t.Fatal("failed compaction lost events")
	}
	policySQL(t, db, `ALTER TABLE login_summaries DROP CONSTRAINT test_failure`)
	n, err := CompactAuditBatch(db, now.AddDate(0, 0, -90))
	if err != nil || n != maintenanceBatchSize {
		t.Fatalf("unbounded batch %d %v", n, err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := CompactAuditBatch(db, now.AddDate(0, 0, -90)); errors <- err }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var summary LoginSummary
	if err := db.Take(&summary).Error; err != nil || summary.LoginCount != 2501 {
		t.Fatalf("double/lost count: %+v %v", summary, err)
	}
	if err := db.Table(LoginFactsSQL + " AS facts").Select("SUM(login_count)").Scan(&count).Error; err != nil || count != 2502 {
		t.Fatalf("lifetime count %d %v", count, err)
	}
	db.Model(&AuditEvent{}).Count(&count)
	if count != 1 {
		t.Fatal("recent event removed")
	}
}
