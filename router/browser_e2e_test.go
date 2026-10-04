//go:build e2e

package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-uuid"
	"github.com/patrickmn/go-cache"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/singleflight"

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
	// Exercise the same ownership constraints and query indexes as production,
	// not merely AutoMigrate's table definitions.
	if err := model.MigrateDatabasePolicy(db); err != nil {
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
	editor := model.Account{Email: "browser-editor@example.com", Nickname: "Browser Editor", PasswordHash: string(hash),
		Role: model.RoleEditor, EmailVerifiedAt: &now}
	for _, account := range []*model.Account{&admin, &reader, &editor} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	externalApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("External app callback"))
	}))
	t.Cleanup(externalApp.Close)
	redirectURI := externalApp.URL + "/callback"
	client := model.OIDCClient{ID: "browser-public-client", OwnerID: &admin.ID, Name: "Browser external app",
		Description: "An external app using your blog identity", HomepageURL: "https://client.example.test/", Public: true,
		RedirectURIsJSON: fmt.Sprintf("[%q]", redirectURI)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	legacyClient := model.OIDCClient{ID: "browser-legacy-client", Name: "Legacy Browser App", Public: true,
		HomepageURL: "javascript:alert(1)", RedirectURIsJSON: fmt.Sprintf("[%q]", redirectURI)}
	if err := db.Create(&legacyClient).Error; err != nil {
		t.Fatal(err)
	}
	article := model.Article{AuthorID: &admin.ID, Slug: "e2e-theme-article", Title: "E2E Theme Article",
		Content: "A browser-visible article used to test search and comments.", TemplateID: solitudes.ArticleTemplateID, Version: 1}
	if err := db.Create(&article).Error; err != nil {
		t.Fatal(err)
	}
	editorArticle := model.Article{AuthorID: &editor.ID, Slug: "visual-editor-post",
		Title: "An editor's field notes", Content: "A short article published by an editor so the author view has real data.",
		CreatedAt:  now.Add(-72 * time.Hour),
		TemplateID: solitudes.ArticleTemplateID, Version: 1}
	if err := db.Create(&editorArticle).Error; err != nil {
		t.Fatal(err)
	}
	publicPage := model.Article{AuthorID: &admin.ID, Slug: "visual-page", Title: "About this publication",
		CreatedAt:  now.Add(-72 * time.Hour),
		Content:    "## A publication for curious readers\n\nThis page presents the site and its editorial approach.\n\n- Read thoughtfully\n- Write clearly\n- Share generously\n\n1. Choose a story\n2. Join the conversation",
		TemplateID: solitudes.PageTemplateID, Version: 1}
	if err := db.Create(&publicPage).Error; err != nil {
		t.Fatal(err)
	}
	privateArticle := model.Article{AuthorID: &admin.ID, Slug: "e2e-private-profile-draft",
		Title: "Private profile draft", Content: "This draft must never appear on public profiles.",
		TemplateID: solitudes.ArticleTemplateID, Version: 1, Visibility: model.VisibilityPrivate}
	if err := db.Create(&privateArticle).Error; err != nil {
		t.Fatal(err)
	}
	for _, visibility := range []model.ArticleVisibility{model.VisibilityPublic, model.VisibilityMembers, model.VisibilityEditors, model.VisibilityPrivate} {
		secured := model.Article{AuthorID: &admin.ID, Slug: "access-" + string(visibility), Title: "Access " + string(visibility),
			Visibility: visibility, RawTags: "Access", TemplateID: solitudes.ArticleTemplateID, Version: 2,
			Content: "## Public heading\n\npublic-access-token\n\n```access:members\n## Member secret heading\n\nmember-access-secret\n```\n\n```access:editors\n## Editor secret heading\n\neditor-access-secret\n```\n\n```access:private\n## Author secret heading\n\nauthor-access-secret\n![hidden](https://hidden-attachment.example/image.jpg)\n```"}
		if err := db.Create(&secured).Error; err != nil {
			t.Fatal(err)
		}
		history := model.ArticleHistory{ArticleID: secured.ID, Version: 1, Content: "## Historical public heading\n\nlegacy-unguarded-secret\n\n```access:members\n## Historical member heading\n\nhistory-member-secret\n```\n\n```access:editors\n## Historical editor heading\n\nhistory-editor-secret\n```\n\n```access:private\n## Historical author heading\n\nhistory-author-secret\n![hidden](https://history-hidden-attachment.example/image.jpg)\n```"}
		if err := db.Create(&history).Error; err != nil {
			t.Fatal(err)
		}
	}
	privateComment := model.Comment{AccountID: &admin.ID, ArticleID: &privateArticle.ID,
		Nickname: admin.Nickname, Content: "Private comment content"}
	if err := db.Create(&privateComment).Error; err != nil {
		t.Fatal(err)
	}
	spamRoot := model.Comment{AccountID: &admin.ID, ArticleID: &article.ID,
		Nickname: admin.Nickname, Content: "Spam comment content", IsSpam: true}
	if err := db.Create(&spamRoot).Error; err != nil {
		t.Fatal(err)
	}
	spamReply := model.Comment{AccountID: &admin.ID, ArticleID: &article.ID,
		ReplyTo: &spamRoot.ID, Nickname: admin.Nickname, Content: "Reply to spam root"}
	if err := db.Create(&spamReply).Error; err != nil {
		t.Fatal(err)
	}
	// Keep both topic states in every CI theme run, not only screenshot fixtures.
	quietTopic := model.Article{AuthorID: &admin.ID, Slug: "visual-topic", Title: "A quiet thought",
		Content: "Share your favorite reading experience and how you found this space.", RawTags: "Topic,design",
		TemplateID: solitudes.ArticleTemplateID, Version: 1}
	busyTopic := model.Article{AuthorID: &editor.ID, Slug: "visual-topic-discussion", Title: "A conversation about reading",
		Content: "What makes a small community worth returning to?\n\nA short thought, followed by a real conversation.", RawTags: "Topic,design",
		TemplateID: solitudes.ArticleTemplateID, Version: 1}
	for _, topic := range []*model.Article{&quietTopic, &busyTopic} {
		if err := db.Create(topic).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, comment := range []model.Comment{
		{ArticleID: &busyTopic.ID, Nickname: "A visitor with a longer name", Email: "topic-guest@example.test", Content: "Guest topic reply: I enjoy discovering new authors."},
		{ArticleID: &busyTopic.ID, AccountID: &reader.ID, Content: "Member topic reply: " + strings.Repeat("A thoughtful conversation. ", 18)},
		{ArticleID: &busyTopic.ID, AccountID: &editor.ID, Content: "Editor topic reply: Thanks for sharing what you read."},
		{ArticleID: &busyTopic.ID, AccountID: &admin.ID, Content: "Admin topic reply: Everyone is welcome to join in."},
		{ArticleID: &busyTopic.ID, Nickname: "Hidden spam", Content: "topic-spam-must-not-render", IsSpam: true},
	} {
		if err := db.Create(&comment).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&busyTopic).UpdateColumn("comment_num", 4).Error; err != nil {
		t.Fatal(err)
	}
	for _, templateID := range []byte{solitudes.ArticleTemplateID, solitudes.PageTemplateID} {
		closed := model.Article{AuthorID: &admin.ID, Slug: fmt.Sprintf("visual-closed-%d", templateID), Title: "Comments closed",
			Content: "A read-only article without a comment form.", TemplateID: templateID, Version: 1, DisableComment: true}
		if err := db.Create(&closed).Error; err != nil {
			t.Fatal(err)
		}
	}
	longArticle := model.Article{AuthorID: &admin.ID, Slug: "visual-long-article", Title: "A deliberately long article title for navigation and typography checks",
		Content: "# A long-form article\n\nThis story checks the reading column, headings, and comments.\n\n## In detail\n\n- A first idea\n- A second idea\n\n```go\nfunc main() { println(\"hello\") }\n```\n\n### Looking ahead\n\nA nested section to verify the collapsible table of contents.",
		RawTags: "design,engineering", TemplateID: solitudes.ArticleTemplateID, Version: 1}
	longArticle.Content = strings.ReplaceAll(longArticle.Content, "\n\n##", strings.Repeat("\n\nA reading layout should let you follow an argument without losing your place. Clear headings, comfortable line length and a stable contents panel help readers move between sections on different screens.", 8)+"\n\n##")
	longArticle.Content += "\n\n[Ordinary reading link](/visual-page). <u>Explicit underline</u>. <u><a href=\"/visual-page\">Underlined link wrapper</a></u>. <a href=\"/visual-page\"><u>Underlined link text</u></a>. ~~Deleted text~~."
	if err := db.Create(&longArticle).Error; err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SOLITUDES_PAGINATION_FIXTURE") != "1" {
		if err := seedBookFixture(db, admin); err != nil {
			t.Fatal(err)
		}
		if err := seedDeepBookFixture(db, admin, editor, reader); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("SOLITUDES_VISUAL_AUDIT") != "" {
		if err := db.Create(&model.Comment{ArticleID: &article.ID, Nickname: "A visitor",
			Email: "visitor@example.test", Content: "This layout is easy to read. What comes next?"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&article).Update("comment_num", 1).Error; err != nil {
			t.Fatal(err)
		}
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
	config.ConfigFilePath = filepath.Join(t.TempDir(), "conf.yml")
	smtpPort, mailMessages := startMailCatcher(t)
	config.Email.Host = "127.0.0.1"
	config.Email.Port = smtpPort
	config.Email.User = "browser-test@localhost"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config, Search: search,
		Cache: cache.New(time.Minute, time.Minute), SafeCache: new(singleflight.Group)}
	var pagingThread string
	if os.Getenv("SOLITUDES_PAGINATION_FIXTURE") == "1" {
		pagingThread, err = seedPaginationFixture(db, admin, reader)
		if err != nil {
			t.Fatal(err)
		}
	}
	var indexedArticles []model.Article
	if err := db.Find(&indexedArticles).Error; err != nil {
		t.Fatal(err)
	}
	for i := range indexedArticles {
		if err := solitudes.IndexArticle(&indexedArticles[i]); err != nil {
			t.Fatal(err)
		}
	}
	themes, err := theme.LoadThemes(filepath.Join("..", "resource", "themes"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOLITUDES_E2E", "1")
	for _, site := range []string{"cactus", "folio"} {
		for _, adminTheme := range []string{"default"} {
			t.Run(site+"-"+adminTheme, func(t *testing.T) {
				if err := db.Where("account_id = ?", reader.ID).Delete(&model.Passkey{}).Error; err != nil {
					t.Fatal(err)
				}
				id, err := uuid.GenerateUUID()
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Where("account_id = ? AND provider = ?", reader.ID, "github").Delete(&model.ExternalIdentity{}).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.ExternalIdentity{ID: id, AccountID: reader.ID, Provider: "github", Subject: "browser-reader"}).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&reader).Updates(map[string]interface{}{
					"role": model.RoleUser, "password_hash": string(hash),
				}).Error; err != nil {
					t.Fatal(err)
				}
				config.Site.Theme = site
				config.Admin.Theme = adminTheme
				config.Auth.GitHub.ClientID = "browser-github-client"
				config.Auth.GitHub.ClientSecret = "browser-github-secret"
				model.SyncThemeConfig(config, themes)
				if os.Getenv("SOLITUDES_HOMETOP_FIXTURE") == "1" {
					content, err := os.ReadFile(filepath.Join("..", "e2e", "fixtures", "lifelonglearn-hometop.html"))
					if err != nil {
						t.Fatal(err)
					}
					config.Site.ThemeConfig[site+".hometopcontent"] = string(content)
				}
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				port := listener.Addr().(*net.TCPAddr).Port
				config.Site.Domain = fmt.Sprintf("localhost:%d", port)
				config.Auth.WebAuthn.RPID = "localhost"
				config.Auth.WebAuthn.Origin = fmt.Sprintf("http://localhost:%d", port)
				// Templates and translations are read relative to the repository root.
				repoRoot, err := filepath.Abs("..")
				if err != nil {
					t.Fatal(err)
				}
				if pagingThread != "" {
					t.Chdir(t.TempDir())
					if err := os.Symlink(filepath.Join(repoRoot, "resource"), "resource"); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll("data/upload", 0o755); err != nil {
						t.Fatal(err)
					}
					for i := 0; i < 31; i++ {
						if err := os.WriteFile(fmt.Sprintf("data/upload/paging-%02d.txt", i), []byte("fixture"), 0o644); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					t.Chdir(repoRoot)
				}
				app := newAppWithRoutes(func(app *fiber.App) {
					// Only this in-process E2E server exposes captured test mail. The
					// production router has no mail inspection endpoint.
					app.Get("/__e2e/server-error", func(c *fiber.Ctx) error {
						return errors.New("private-database-token-must-not-leak")
					})
					app.Get("/__e2e/verification-mail", func(c *fiber.Ctx) error {
						select {
						case message := <-mailMessages:
							decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(message)))
							if err != nil {
								return err
							}
							link := regexp.MustCompile(`http://localhost:\d+/verify-email\?token=[a-f0-9]{64}(&return_to=[^\s"<>]*)?`).FindString(string(decoded))
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
					resp, err := http.Get(baseURL + "/login")
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
				// The full role/layout matrix now includes more than 70 browser
				// cases per theme. Leave room for slower CI hosts without retries.
				testTimeout := 8 * time.Minute
				ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
				defer cancel()
				root := repoRoot
				args := []string{"test", "--config", "playwright.config.ts", "theme-contract.spec.ts"}
				if os.Getenv("SOLITUDES_VISUAL_AUDIT") != "" {
					args = []string{"test", "--config", "visual-audit.config.ts", "visual-audit.spec.ts"}
				}
				if focus := os.Getenv("SOLITUDES_E2E_GREP"); focus != "" {
					args = append(args, "--grep", focus)
				}
				cmd := exec.CommandContext(ctx, filepath.Join(root, "e2e", "node_modules", ".bin", "playwright"), args...)
				cmd.Dir = filepath.Join(root, "e2e")
				cmd.Env = append(os.Environ(),
					"E2E_BASE_URL="+baseURL,
					"E2E_PAGING_THREAD="+pagingThread,
					"E2E_SITE_THEME="+site,
					"E2E_ADMIN_THEME="+adminTheme,
					"E2E_ADMIN_EMAIL="+admin.Email,
					"E2E_ADMIN_PASSWORD=test-browser-password",
					"E2E_READER_EMAIL="+reader.Email,
					"E2E_READER_PASSWORD=test-browser-password",
					"E2E_EDITOR_EMAIL="+editor.Email,
					"E2E_OIDC_CLIENT_ID="+client.ID,
					"E2E_LEGACY_OIDC_CLIENT_ID="+legacyClient.ID,
					"E2E_OIDC_REDIRECT_URI="+redirectURI,
					"E2E_ADMIN_ID="+admin.ID,
					"E2E_READER_ID="+reader.ID,
					"E2E_EDITOR_ID="+editor.ID,
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

// Included by the existing CI -run TestBrowserThemeMatrix pattern.
func TestBrowserThemeMatrixPagination(t *testing.T) {
	t.Setenv("SOLITUDES_PAGINATION_FIXTURE", "1")
	t.Setenv("SOLITUDES_E2E_GREP", "pagination contract")
	TestBrowserThemeMatrix(t)
}
