package solitudes

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
)

// SOLITUDES_TEST_POSTGRES_DSN must point to a dedicated test database. Each
// run creates and drops only its own uniquely named schema.
func TestPostgresLegacyArticlesMigrateToAdministrator(t *testing.T) {
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
	if err := db.Exec("SET search_path TO " + quoted + ", public").Error; err != nil {
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
	for _, ddl := range []string{
		`CREATE TABLE articles (id uuid PRIMARY KEY, slug text CONSTRAINT uni_articles_slug UNIQUE, title text, content text, template_id smallint, version bigint, created_at timestamptz, updated_at timestamptz)`,
		`CREATE TABLE article_histories (article_id uuid, version bigint, content text, created_at timestamptz)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	articleID := "38d33a80-3fda-4597-a357-ef1c67e34b0e"
	if err := db.Exec(`INSERT INTO articles (id, slug, title, content, template_id, version, created_at, updated_at)
		VALUES (?, 'legacy-post', 'Old article', 'Keep the original content', 1, 2, ?, ?)`, articleID, time.Now(), time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO article_histories (article_id, version, content, created_at)
		VALUES (?, 1, 'First version', ?)`, articleID, time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("legacy-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	config := &model.Config{}
	config.User.Email = "existing-admin@example.com"
	config.User.Nickname = "Existing Admin"
	config.User.Password = string(hash)
	previous := System
	System = &SysVariable{DB: db, Config: config}
	t.Cleanup(func() { System = previous })
	if err := migrate(); err != nil {
		t.Fatal(err)
	}
	var admin model.Account
	if err := db.Where("email = ?", config.User.Email).Take(&admin).Error; err != nil || admin.Role != model.RoleAdmin || admin.EmailVerifiedAt == nil {
		t.Fatalf("administrator bootstrap: %+v %v", admin, err)
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("legacy-password")) != nil {
		t.Fatal("previous administrator password was not preserved")
	}
	var article model.Article
	if err := db.Preload("Author").Take(&article, "id = ?", articleID).Error; err != nil {
		t.Fatal(err)
	}
	if article.AuthorID == nil || *article.AuthorID != admin.ID || article.Author.Nickname != "Existing Admin" ||
		article.Slug != "legacy-post" || article.Version != 2 || article.Content != "Keep the original content" {
		t.Fatalf("legacy article changed unexpectedly: %+v", article)
	}
	var history model.ArticleHistory
	if err := db.Take(&history, "article_id = ?", articleID).Error; err != nil || history.Content != "First version" || history.EditorID != nil {
		t.Fatalf("legacy revision changed unexpectedly: %+v %v", history, err)
	}
	if err := migrate(); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if !db.Migrator().HasColumn(&model.Account{}, "directory_hidden") {
		t.Fatal("reader circle opt-out column is missing")
	}
	var directoryColumns []string
	if err := db.Raw(`SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='accounts' AND column_name LIKE 'directory_%'`).Scan(&directoryColumns).Error; err != nil || len(directoryColumns) != 1 || directoryColumns[0] != "directory_hidden" {
		t.Fatalf("expected one reader circle preference: %v %v", directoryColumns, err)
	}
	if err := db.Take(&admin, "id = ?", admin.ID).Error; err != nil || admin.DirectoryHidden {
		t.Fatalf("existing verified member should be visible: %+v err=%v", admin, err)
	}
	var count int64
	if err := db.Model(&model.Account{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("repeated migration created extra accounts: %d %v", count, err)
	}
	if err := db.Raw(`SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='articles' AND indexdef LIKE '%UNIQUE%' AND indexdef LIKE '%(slug)%'`).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("migration retained duplicate/missing slug indexes: %d %v", count, err)
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
