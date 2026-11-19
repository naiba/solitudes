//go:build e2e

package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/patrickmn/go-cache"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/internal/theme"
)

// TestCaptureSiteThemeScreenshots regenerates resource/themes/site/*/screenshot.png
// from the real templates, so a theme preview cannot drift from the theme again.
// It only runs on request and writes nothing but those images:
//
//	SOLITUDES_CAPTURE_SITE_THEME=1 go test -tags 'e2e postgres_test' ./router \
//	  -run TestCaptureSiteThemeScreenshots -count=1
func TestCaptureSiteThemeScreenshots(t *testing.T) {
	if os.Getenv("SOLITUDES_CAPTURE_SITE_THEME") != "1" {
		t.Skip("set SOLITUDES_CAPTURE_SITE_THEME=1 to rewrite the site theme screenshots")
	}
	if _, err := os.Stat(filepath.Join("..", "e2e", "node_modules", ".bin", "playwright")); err != nil {
		t.Skip("install e2e dependencies first: bun install --cwd e2e")
	}
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.FeedVisit{},
		&model.Passkey{}, &model.PasskeyCeremony{}, &model.OAuthAttempt{}, &model.ExternalIdentity{},
		&model.OIDCClient{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{},
		&model.OIDCRefreshToken{}, &model.OIDCConsent{}, &model.DatabaseMigration{}, &model.OIDCSigningKey{}, &model.OIDCCryptoKey{}); err != nil {
		t.Fatal(err)
	}
	if err := model.MigrateDatabasePolicy(db); err != nil {
		t.Fatal(err)
	}
	if err := seedThemeShowcase(db); err != nil {
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
	config.Site.SpaceName = "Solitudes"
	config.Site.SpaceDesc = "Notes on software, reading and working remotely"
	config.ConfigFilePath = filepath.Join(t.TempDir(), "conf.yml")
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config, Search: search,
		Cache: cache.New(time.Minute, time.Minute), SafeCache: new(singleflight.Group)}
	themes, err := theme.LoadThemes(filepath.Join("..", "resource", "themes"))
	if err != nil {
		t.Fatal(err)
	}
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	// Templates, translations and theme assets are read relative to the repository root.
	t.Chdir(repoRoot)
	for _, site := range themes.Site {
		t.Run(site.ID, func(t *testing.T) {
			config.Site.Theme = site.ID
			config.Admin.Theme = "default"
			// The home intro slots hold owner-written content; keep the preview
			// about the theme itself.
			config.Site.ThemeConfig = map[string]interface{}{
				site.ID + ".hometopcontent":    "",
				site.ID + ".homebottomcontent": "",
				site.ID + ".customcode":        "",
			}
			model.SyncThemeConfig(config, themes)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			config.Site.Domain = fmt.Sprintf("localhost:%d", port)
			serverDone := make(chan error, 1)
			server := &http.Server{Handler: newApp(), ReadHeaderTimeout: 5 * time.Second}
			go func() { serverDone <- server.Serve(listener) }()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
				_ = listener.Close()
				<-serverDone
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(repoRoot, "e2e", "node_modules", ".bin", "playwright"),
				"test", "--config", "playwright.config.ts", "theme-screenshot.spec.ts")
			cmd.Dir = filepath.Join(repoRoot, "e2e")
			cmd.Env = append(os.Environ(),
				fmt.Sprintf("E2E_BASE_URL=http://localhost:%d", port),
				"E2E_SITE_THEME="+site.ID,
				"E2E_ADMIN_THEME=default",
			)
			var output bytes.Buffer
			cmd.Stdout = io.MultiWriter(os.Stdout, &output)
			cmd.Stderr = cmd.Stdout
			if err := cmd.Run(); err != nil {
				t.Fatalf("capture %s: %v\n%s", site.ID, err, output.String())
			}
			info, err := os.Stat(filepath.Join(repoRoot, "resource", "themes", "site", site.ID, "screenshot.png"))
			if err != nil || info.Size() == 0 {
				t.Fatalf("%s screenshot was not written: %v", site.ID, err)
			}
		})
	}
}

// seedThemeShowcase publishes a small, realistic publication: enough posts for
// a lead story and a list, short notes with replies, and a few readers.
func seedThemeShowcase(db *gorm.DB) error {
	joined := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)
	verified := joined
	owner := model.Account{Email: "ada@example.com", Nickname: "Ada", Role: model.RoleAdmin, EmailVerifiedAt: &verified, CreatedAt: joined,
		Bio: "Writes about software, reading and remote work."}
	editor := model.Account{Email: "lin@example.com", Nickname: "Lin", Role: model.RoleEditor, EmailVerifiedAt: &verified, CreatedAt: joined.Add(24 * time.Hour)}
	reader := model.Account{Email: "morgan@example.com", Nickname: "Morgan", Role: model.RoleUser, EmailVerifiedAt: &verified, CreatedAt: joined.Add(48 * time.Hour)}
	for _, account := range []*model.Account{&owner, &editor, &reader} {
		if err := db.Create(account).Error; err != nil {
			return err
		}
	}
	day := func(month time.Month, dayOfMonth int) time.Time {
		return time.Date(2026, month, dayOfMonth, 10, 0, 0, 0, time.UTC)
	}
	posts := []model.Article{
		{AuthorID: &owner.ID, Slug: "a-blog-you-still-enjoy-in-ten-years", Title: "Designing a blog you still enjoy in ten years", RawTags: "writing,design",
			Content:   "Most publishing tools are designed for the launch, not for the tenth year. A blog that lasts keeps its URLs, loads without a build step, and stays pleasant to write in long after the novelty fades. These are the constraints behind this site, and the handful of decisions that mattered more than any redesign.",
			CreatedAt: day(time.March, 1), ReadNum: 420, CommentNum: 12},
		{AuthorID: &owner.ID, Slug: "building-web-apis-with-fiber", Title: "Building Web APIs with Fiber", RawTags: "go,web",
			Content:   "Fiber is an Express-inspired web framework built on top of Fasthttp. It keeps routing and middleware familiar while avoiding allocations on the hot path, which makes it a comfortable choice for small services.",
			CreatedAt: day(time.February, 26), ReadNum: 256, CommentNum: 5},
		{AuthorID: &editor.ID, Slug: "postgresql-performance-tuning-notes", Title: "PostgreSQL performance tuning notes", RawTags: "database",
			Content:   "Start with the queries, not the configuration. An index that matches the sort order, a covering index for the list page and an honest look at EXPLAIN ANALYZE remove most problems before any setting is changed.",
			CreatedAt: day(time.February, 22), ReadNum: 312, CommentNum: 8},
		{AuthorID: &owner.ID, Slug: "remote-work-notes-for-my-future-self", Title: "Remote work notes for my future self", RawTags: "remote,life",
			Content:   "Six years into working remotely, these are the habits that survived: a fixed start and end to the day, writing things down before discussing them, and one uninterrupted half day every week.",
			CreatedAt: day(time.February, 18), ReadNum: 198, CommentNum: 6},
		{AuthorID: &owner.ID, Slug: "understanding-docker-containers", Title: "Understanding Docker containers", RawTags: "docker",
			Content:   "A container is a process with a view of the system narrowed by namespaces and limited by cgroups. Once that picture is clear, images, layers and volumes stop being magic and become ordinary files.",
			CreatedAt: day(time.February, 12), ReadNum: 89, CommentNum: 1},
		{AuthorID: &editor.ID, Slug: "modern-css-layout-techniques", Title: "Modern CSS layout techniques", RawTags: "css,design",
			Content:   "Grid for the page, flexbox for the row, and min-width: 0 for everything that refuses to shrink. A short tour of the layout tools that replaced a decade of clearfix and negative margins.",
			CreatedAt: day(time.February, 6), ReadNum: 175, CommentNum: 2},
		{AuthorID: &owner.ID, Slug: "getting-started-with-go", Title: "Getting started with Go", RawTags: "go",
			Content:   "Go is a statically typed, compiled language with a small surface and a large standard library. This guide covers the toolchain, modules and the handful of idioms worth learning first.",
			CreatedAt: day(time.January, 28), ReadNum: 128, CommentNum: 3},
		{AuthorID: &owner.ID, Slug: "welcome-to-solitudes", Title: "Welcome to Solitudes", RawTags: "meta",
			Content:   "Solitudes is a fast, simple and self-hosted blog engine with themes, series, members and a reader circle. This first post explains how the site is organised.",
			CreatedAt: day(time.January, 20), ReadNum: 96, CommentNum: 4},
	}
	topics := []model.Article{
		{AuthorID: &owner.ID, Slug: "note-reading-on-paper", Title: "Reading on paper again", RawTags: "Topic",
			Content: "Switched back to paper for long reads this month. I remember far more of what I read.", CreatedAt: day(time.February, 28), ReadNum: 41, CommentNum: 2},
		{AuthorID: &editor.ID, Slug: "note-small-tools", Title: "Small tools", RawTags: "Topic",
			Content: "What is the smallest tool you could not work without?", CreatedAt: day(time.February, 24), ReadNum: 37, CommentNum: 1},
	}
	series := model.Article{AuthorID: &owner.ID, Slug: "a-field-guide-to-thoughtful-writing", Title: "A field guide to thoughtful writing", IsBook: true,
		Content: "A short series on drafting, revising and publishing in public.", CreatedAt: day(time.January, 15), ReadNum: 64}
	for _, group := range [][]model.Article{posts, topics, {series}} {
		for i := range group {
			group[i].TemplateID = solitudes.ArticleTemplateID
			group[i].Version = 1
			group[i].UpdatedAt = group[i].CreatedAt
			if err := db.Create(&group[i]).Error; err != nil {
				return err
			}
		}
	}
	replies := []model.Comment{
		{ArticleID: &topics[0].ID, AccountID: &reader.ID, Content: "Same here. A pencil in the margin beats any highlight colour.", CreatedAt: day(time.February, 28).Add(2 * time.Hour)},
		{ArticleID: &topics[0].ID, AccountID: &editor.ID, Content: "Paper for books, a screen for references works well for me.", CreatedAt: day(time.February, 28).Add(3 * time.Hour)},
		{ArticleID: &topics[1].ID, AccountID: &owner.ID, Content: "A plain text file named todo.", CreatedAt: day(time.February, 24).Add(time.Hour)},
		{ArticleID: &posts[0].ID, AccountID: &reader.ID, Content: "Keeping every old URL alive is the part most redesigns forget.", CreatedAt: day(time.March, 1).Add(4 * time.Hour)},
		{ArticleID: &posts[2].ID, Nickname: "A visitor", Email: "visitor@example.com", Content: "The covering index tip saved our archive page. Thank you.", CreatedAt: day(time.February, 23)},
	}
	for i := range replies {
		if err := db.Create(&replies[i]).Error; err != nil {
			return err
		}
	}
	return nil
}
