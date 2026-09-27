//go:build e2e

package router

import (
	"context"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/patrickmn/go-cache"
	"golang.org/x/crypto/bcrypt"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/internal/theme"
)

// TestBrowserThemeMatrix serves the real Fiber application on an ephemeral
// port, using an isolated PostgreSQL schema and the actual Playwright browser.
// No production config, database, or port 8080 is modified.
func TestBrowserThemeMatrix(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "e2e", "node_modules", ".bin", "playwright")); err != nil {
		t.Skip("install e2e dependencies first: bun install --cwd e2e")
	}
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.FeedVisit{},
		&model.Passkey{}, &model.PasskeyCeremony{}, &model.OAuthAttempt{}, &model.ExternalIdentity{},
		&model.OIDCClient{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{},
		&model.OIDCRefreshToken{}, &model.OIDCSigningKey{}, &model.OIDCCryptoKey{}); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("test-browser-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	admin := model.Account{Email: "browser-admin@example.com", Nickname: "Browser Admin", PasswordHash: string(hash),
		Role: model.RoleAdmin, EmailVerifiedAt: &now}
	reader := model.Account{Email: "browser-reader@example.com", Nickname: "Browser Reader", PasswordHash: string(hash),
		Role: model.RoleUser, EmailVerifiedAt: &now}
	for _, account := range []*model.Account{&admin, &reader} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	article := model.Article{AuthorID: &admin.ID, Slug: "e2e-theme-article", Title: "E2E Theme Article",
		Content: "A browser-visible article used to test search and comments.", TemplateID: solitudes.ArticleTemplateID, Version: 1}
	if err := db.Create(&article).Error; err != nil {
		t.Fatal(err)
	}
	search, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = search.Close() })
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.SpaceName = "Solitudes Browser Test"
	config.Site.SpaceDesc = "Browser E2E test site"
	config.User.Nickname = admin.Nickname
	config.User.Email = admin.Email
	smtpPort, mailMessages := startMailCatcher(t)
	config.Email.Host = "127.0.0.1"
	config.Email.Port = smtpPort
	config.Email.User = "browser-test@localhost"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config, Search: search,
		Cache: cache.New(time.Minute, time.Minute)}
	if err := solitudes.IndexArticle(&article); err != nil {
		t.Fatal(err)
	}
	themes, err := theme.LoadThemes(filepath.Join("..", "resource", "themes"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOLITUDES_E2E", "1")
	for _, site := range []string{"cactus", "folio"} {
		for _, adminTheme := range []string{"default", "glacie"} {
			t.Run(site+"-"+adminTheme, func(t *testing.T) {
				if err := db.Model(&reader).Updates(map[string]interface{}{
					"role": model.RoleUser, "password_hash": string(hash),
				}).Error; err != nil {
					t.Fatal(err)
				}
				config.Site.Theme = site
				config.Admin.Theme = adminTheme
				model.SyncThemeConfig(config, themes)
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				port := listener.Addr().(*net.TCPAddr).Port
				config.Site.Domain = fmt.Sprintf("localhost:%d", port)
				// Templates and translations are read relative to the repository root.
				t.Chdir("..")
				app := newAppWithRoutes(func(app *fiber.App) {
					// Only this in-process E2E server exposes captured test mail. The
					// production router has no mail inspection endpoint.
					app.Get("/__e2e/verification-mail", func(c *fiber.Ctx) error {
						select {
						case message := <-mailMessages:
							decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(message)))
							if err != nil {
								return err
							}
							link := regexp.MustCompile(`http://localhost:\d+/admin/verify-email\?token=[a-f0-9]{64}`).FindString(string(decoded))
							if link == "" {
								return fiber.ErrInternalServerError
							}
							return c.SendString(link)
						default:
							return c.SendStatus(http.StatusNoContent)
						}
					})
				})
				serverDone := make(chan error, 1)
				go func() { serverDone <- app.Listener(listener) }()
				t.Cleanup(func() {
					_ = app.ShutdownWithTimeout(5 * time.Second)
					_ = listener.Close()
					<-serverDone
				})
				baseURL := fmt.Sprintf("http://localhost:%d", port)
				ready := false
				for attempt := 0; attempt < 60; attempt++ {
					resp, err := http.Get(baseURL + "/admin/login")
					if err == nil {
						resp.Body.Close()
						ready = resp.StatusCode == http.StatusOK
					}
					if ready {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				if !ready {
					t.Fatal("isolated application did not start")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
				defer cancel()
				root, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"test", "--config", "playwright.config.ts", "theme-contract.spec.ts"}
				if focus := os.Getenv("SOLITUDES_E2E_GREP"); focus != "" {
					args = append(args, "--grep", focus)
				}
				cmd := exec.CommandContext(ctx, filepath.Join(root, "e2e", "node_modules", ".bin", "playwright"), args...)
				cmd.Dir = filepath.Join(root, "e2e")
				cmd.Env = append(os.Environ(),
					"E2E_BASE_URL="+baseURL,
					"E2E_SITE_THEME="+site,
					"E2E_ADMIN_THEME="+adminTheme,
					"E2E_ADMIN_EMAIL="+admin.Email,
					"E2E_ADMIN_PASSWORD=test-browser-password",
					"E2E_READER_EMAIL="+reader.Email,
					"E2E_READER_PASSWORD=test-browser-password",
					"E2E_ARTICLE_SLUG="+article.Slug,
					"E2E_ARTICLE_TITLE="+article.Title,
				)
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("Playwright failed for %s/%s: %v\n%s", site, adminTheme, err, output)
				}
				t.Logf("%s/%s: %s", site, adminTheme, output)
			})
		}
	}
}
