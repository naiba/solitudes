package router

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

type performanceSQL struct {
	logger.Interface
	count, nanos atomic.Int64
}

func (p *performanceSQL) Trace(_ context.Context, start time.Time, _ func() (string, int64), _ error) {
	p.count.Add(1)
	p.nanos.Add(time.Since(start).Nanoseconds())
}

// Run explicitly with -bench BenchmarkPostgresReading -benchmem. Fixtures live
// only in a random test schema; no benchmark data enters the preview/public DB.
func BenchmarkPostgresReading(b *testing.B) {
	db := newPostgresIdentityTestDB(b)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.ExternalIdentity{}, &model.Passkey{}, &model.PasskeyCeremony{}, &model.OAuthAttempt{}, &model.OIDCClient{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}, &model.OIDCSigningKey{}, &model.OIDCCryptoKey{}, &model.FeedVisit{}); err != nil {
		b.Fatal(err)
	}
	if err := model.MigrateDatabasePolicy(db); err != nil {
		b.Fatal(err)
	}
	stamp := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	owner := model.Account{Email: "perf@example.test", Nickname: "Performance author", Role: model.RoleAdmin, EmailVerifiedAt: &stamp}
	if err := db.Create(&owner).Error; err != nil {
		b.Fatal(err)
	}
	body := strings.Repeat("## Reading performance\n\nA representative paragraph about community and long-form writing with **emphasis**, [a link](https://example.test) and Unicode 阅读体验.\n\n", 50)
	for i := 0; i < 20; i++ {
		book := model.Article{AuthorID: &owner.ID, Slug: fmt.Sprintf("perf-book-%02d", i), Title: fmt.Sprintf("Book %02d", i), Content: body, IsBook: true, TemplateID: solitudes.ArticleTemplateID, CreatedAt: stamp, Version: 1}
		if err := db.Create(&book).Error; err != nil {
			b.Fatal(err)
		}
		for j := 0; j < 5; j++ {
			part := model.Article{AuthorID: &owner.ID, BookRefer: &book.ID, Slug: fmt.Sprintf("perf-part-%02d-%02d", i, j), Title: "Part", Content: body, IsBook: true, TemplateID: solitudes.ArticleTemplateID, CreatedAt: stamp, Version: 1}
			if err := db.Create(&part).Error; err != nil {
				b.Fatal(err)
			}
			var chapters []model.Article
			for k := 0; k < 10; k++ {
				chapters = append(chapters, model.Article{AuthorID: &owner.ID, BookRefer: &part.ID, Slug: fmt.Sprintf("perf-chapter-%02d-%02d-%02d", i, j, k), Title: "Chapter performance", Content: body, TemplateID: solitudes.ArticleTemplateID, CreatedAt: stamp, Version: 1, ReadNum: 10})
			}
			if err := db.Create(&chapters).Error; err != nil {
				b.Fatal(err)
			}
		}
	}
	searchIndex, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = searchIndex.Close() })
	var all []model.Article
	if err := db.Find(&all).Error; err != nil {
		b.Fatal(err)
	}
	for _, article := range all {
		if err := searchIndex.Index(article.GetIndexID(), map[string]interface{}{"Title": article.Title, "Content": article.Content, "IsPrivate": false}); err != nil {
			b.Fatal(err)
		}
	}
	all = nil
	if err := db.Exec("ANALYZE articles").Error; err != nil {
		b.Fatal(err)
	}
	b.Chdir("..")
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	b.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	for _, theme := range []string{"cactus", "folio"} {
		b.Run(theme, func(b *testing.B) {
			config := &model.Config{}
			config.Site.Theme, config.Admin.Theme, config.Site.Domain = theme, "default", "localhost"
			tracker := &performanceSQL{Interface: logger.Default.LogMode(logger.Silent)}
			queryDB := db.Session(&gorm.Session{Logger: tracker})
			solitudes.System = &solitudes.SysVariable{DB: queryDB, Config: config, Search: searchIndex}
			if err := LoadTemplates(); err != nil {
				b.Fatal(err)
			}
			app := fiber.New(fiber.Config{Views: globalDynamicEngine, DisableStartupMessage: true})
			app.Use(trans)
			app.Get("/", index)
			app.Get("/books/", book)
			app.Get("/search/", search)
			app.Get("/:slug", article)
			for _, route := range []struct{ name, path string }{{"Home", "/"}, {"Books", "/books/"}, {"Book", "/perf-book-00"}, {"Article", "/perf-chapter-00-00-00"}, {"Search", "/search/?w=performance"}} {
				b.Run(route.name, func(b *testing.B) {
					request := func() {
						response, err := app.Test(httptest.NewRequest("GET", route.path, nil), -1)
						if err != nil {
							b.Fatal(err)
						}
						if response.StatusCode != 200 {
							body, _ := io.ReadAll(response.Body)
							response.Body.Close()
							b.Fatalf("%s: %d %s", route.path, response.StatusCode, body)
						}
						_, _ = io.Copy(io.Discard, response.Body)
						response.Body.Close()
					}
					request()
					tracker.count.Store(0)
					tracker.nanos.Store(0)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						request()
					}
					b.StopTimer()
					b.ReportMetric(float64(tracker.count.Load())/float64(b.N), "sql/op")
					b.ReportMetric(float64(tracker.nanos.Load())/float64(b.N)/1e6, "sql-ms/op")
				})
			}
		})
	}
}
