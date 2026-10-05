package solitudes

import (
	"bytes"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
)

// SOLITUDES_TEST_POSTGRES_DSN must point to a dedicated test database. Each
// run creates and drops only its own uniquely named schema.
func administratorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("SOLITUDES_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SOLITUDES_TEST_POSTGRES_DSN to a dedicated PostgreSQL test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	random, err := uuid.GenerateUUID()
	if err != nil {
		t.Fatal(err)
	}
	schema := "solitudes_test_" + strings.ReplaceAll(random, "-", "")
	quoted := pq.QuoteIdentifier(schema)
	if err := db.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA " + quoted + " CASCADE").Error; err != nil {
			t.Errorf("remove temporary test schema %s: %v", schema, err)
		}
	})
	if err := db.Exec("SET search_path TO " + quoted).Error; err != nil {
		t.Fatal(err)
	}
	// migrate() creates uuid-ossp in this schema when it does not exist yet.
	// If another concurrently running test already installed it elsewhere,
	// give this isolated schema its own UUID default instead.
	var installed int64
	if err := db.Raw(`SELECT count(*) FROM pg_extension WHERE extname = 'uuid-ossp'`).Scan(&installed).Error; err != nil {
		t.Fatal(err)
	}
	if installed > 0 {
		if err := db.Exec(`CREATE FUNCTION uuid_generate_v4() RETURNS uuid LANGUAGE SQL AS 'SELECT gen_random_uuid()'`).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestPostgresAdministratorInitialization(t *testing.T) {
	db := administratorTestDB(t)
	if err := initializeDatabase(db); err != nil {
		t.Fatal(err)
	}
	if err := requireAdministrator(db); err == nil {
		t.Fatal("uninitialized server must not start")
	}
	password, err := createInitialAdministrator(db, " ADMIN@example.test ", "Administrator")
	if err != nil {
		t.Fatal(err)
	}
	var admin model.Account
	if err := db.Where("email = ?", "admin@example.test").Take(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if admin.Role != model.RoleAdmin || admin.EmailVerifiedAt == nil || len(password) < 32 ||
		bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)) != nil {
		t.Fatal("invalid initialized administrator")
	}
	if err := requireAdministrator(db); err != nil {
		t.Fatal(err)
	}
	var event model.AuditEvent
	if err := db.Where("action = ?", "account.initialized").Take(&event).Error; err != nil || event.ID == "" || event.ActorID != admin.ID || event.Details != "" {
		t.Fatal("missing or unsafe initialization audit", err)
	}
	if err := initializeDatabase(db); err != nil {
		t.Fatal(err)
	}
	if secret, err := createInitialAdministrator(db, "another@example.test", "Other"); !errors.Is(err, errAdministratorInitialized) || secret != "" {
		t.Fatal("repeat setup accepted", err)
	}
	var persisted model.Account
	if err := db.First(&persisted, "id = ?", admin.ID).Error; err != nil || persisted.PasswordHash != admin.PasswordHash {
		t.Fatal("setup changed existing credentials", err)
	}
	var count int64
	if err := db.Model(&model.Account{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("extra accounts", err)
	}
	if db.Migrator().HasColumn(&model.Comment{}, "is_admin") || db.Migrator().HasColumn(&model.Article{}, "is_private") {
		t.Fatal("obsolete permission columns created")
	}
	// There is exactly one persisted reader-circle preference.
	var columns []string
	if err := db.Raw("SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='accounts' AND column_name LIKE 'directory_%'").Scan(&columns).Error; err != nil || len(columns) != 1 || columns[0] != "directory_hidden" {
		t.Fatal("redundant directory state", columns, err)
	}
	now := time.Now()
	if err := db.Model(&admin).Update("disabled_at", &now).Error; err != nil {
		t.Fatal(err)
	}
	if err := requireAdministrator(db); err == nil {
		t.Fatal("disabled administrator accepted")
	}
}

func TestPostgresAdministratorSetupNeverPromotesRegisteredUsers(t *testing.T) {
	db := administratorTestDB(t)
	if err := initializeDatabase(db); err != nil {
		t.Fatal(err)
	}
	user := model.Account{Email: "reader@example.test", Nickname: "Reader", Role: model.RoleUser}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := createInitialAdministrator(db, user.Email, "Admin"); !errors.Is(err, errAdministratorInitialized) {
		t.Fatal("registered user could become administrator", err)
	}
	if err := db.First(&user, "id = ?", user.ID).Error; err != nil || user.Role != model.RoleUser {
		t.Fatal("changed registered user", err)
	}
}

func TestPostgresConcurrentAdministratorSetup(t *testing.T) {
	db := administratorTestDB(t)
	if err := initializeDatabase(db); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := createInitialAdministrator(db, "admin@example.test", "Admin")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes, rejections := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, errAdministratorInitialized) {
			rejections++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatal("non-atomic setup", successes, rejections)
	}
}

func TestPostgresAdministratorSetupRollsBackWhenAuditFails(t *testing.T) {
	db := administratorTestDB(t)
	if err := initializeDatabase(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE audit_events ADD CONSTRAINT reject_setup CHECK (action <> 'account.initialized')").Error; err != nil {
		t.Fatal(err)
	}
	if password, err := createInitialAdministrator(db, "admin@example.test", "Admin"); err == nil || password != "" {
		t.Fatal("setup accepted audit failure or returned a credential")
	}
	var count int64
	if err := db.Model(&model.Account{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed setup left an administrator", err)
	}
}

func TestPostgresAdministratorSetupUsesConfigWithoutSavingCredentials(t *testing.T) {
	db := administratorTestDB(t)
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	dsn := os.Getenv("SOLITUDES_TEST_POSTGRES_DSN")
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + schema
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("data", 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("data", "conf.yml")
	config, err := yaml.Marshal(&model.Config{Database: dsn})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	password, err := CreateInitialAdministrator("admin@example.test", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	var account model.Account
	if err := db.Where("email = ?", "admin@example.test").Take(&account).Error; err != nil || bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) != nil {
		t.Fatal("setup did not persist credentials in configured database", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(saved, config) || bytes.Contains(saved, []byte(password)) {
		t.Fatal("setup modified configuration or persisted its plaintext password", err)
	}
}

func TestPostgresDatabasePoolLimits(t *testing.T) {
	dsn := os.Getenv("SOLITUDES_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SOLITUDES_TEST_POSTGRES_DSN")
	}
	for _, limit := range []int{0, 2} {
		db, err := newDatabase(&model.Config{Database: dsn, DatabasePool: model.DatabasePoolConfig{MaxOpen: limit}})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		want := limit
		if want == 0 {
			want = 20
		}
		if got := pool.Stats().MaxOpenConnections; got != want {
			t.Errorf("pool limit=%d want %d", got, want)
		}
		if err := pool.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
