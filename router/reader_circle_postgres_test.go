package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresReaderCirclePrivacyAndActivity(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "localhost:8080"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config}
	now := time.Now()
	accounts := []model.Account{
		{Nickname: "Listed", Email: "listed@example.test", Bio: "Public bio", Role: model.RoleUser, EmailVerifiedAt: &now},
		{Nickname: "Opted out", Email: "opted-out@example.test", Role: model.RoleUser, EmailVerifiedAt: &now},
		{Nickname: "Unverified", Email: "unverified@example.test", Role: model.RoleUser},
		{Nickname: "Disabled", Email: "disabled@example.test", Role: model.RoleUser, EmailVerifiedAt: &now, DisabledAt: &now},
		{Nickname: "Default member", Email: "default@example.test", Role: model.RoleUser, EmailVerifiedAt: &now},
	}
	for i := range accounts {
		if err := db.Create(&accounts[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&accounts[1]).Update("directory_hidden", true).Error; err != nil {
		t.Fatal(err)
	}
	var unlisted model.Account
	if err := db.Take(&unlisted, "id = ?", accounts[1].ID).Error; err != nil || !unlisted.DirectoryHidden {
		t.Fatalf("member opt-out not saved: %+v err=%v", unlisted, err)
	}
	var columnDefault string
	if err := db.Raw(`SELECT column_default FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'accounts' AND column_name = 'directory_hidden'`).Scan(&columnDefault).Error; err != nil || columnDefault != "false" {
		t.Fatalf("new account column default=%q err=%v", columnDefault, err)
	}
	public := model.Article{AuthorID: &accounts[0].ID, Title: "Public", Slug: "circle-public", Content: "Visible", CreatedAt: now.Add(-time.Second)}
	private := model.Article{AuthorID: &accounts[1].ID, Title: "Private", Slug: "circle-private", Content: "Hidden", Visibility: model.VisibilityPrivate, CreatedAt: now}
	for _, article := range []*model.Article{&public, &private} {
		if err := db.Create(article).Error; err != nil {
			t.Fatal(err)
		}
	}
	good := model.Comment{AccountID: &accounts[0].ID, ArticleID: &public.ID, Content: "<img src=x onerror=alert(1)> Hello", CreatedAt: now}
	spam := model.Comment{AccountID: &accounts[0].ID, ArticleID: &public.ID, Content: "Spam", IsSpam: true, CreatedAt: now}
	for _, comment := range []*model.Comment{&good, &spam} {
		if err := db.Create(comment).Error; err != nil {
			t.Fatal(err)
		}
	}
	underSpam := model.Comment{AccountID: &accounts[0].ID, ArticleID: &public.ID, ReplyTo: &spam.ID, Content: "Hidden reply", CreatedAt: now}
	privateComment := model.Comment{AccountID: &accounts[0].ID, ArticleID: &private.ID, Content: "Hidden private reply", CreatedAt: now}
	for _, comment := range []*model.Comment{&underSpam, &privateComment} {
		if err := db.Create(comment).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, account := range accounts[1:4] {
		comment := model.Comment{AccountID: &account.ID, ArticleID: &public.ID, Content: "Do not expose", CreatedAt: now}
		if err := db.Create(&comment).Error; err != nil {
			t.Fatal(err)
		}
	}
	old := model.Comment{AccountID: &accounts[0].ID, ArticleID: &public.ID, Content: "Older than 30 days", CreatedAt: now.AddDate(0, 0, -31)}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	latest, more, err := latestReaders(1, 12)
	if err != nil || more || len(latest) != 2 || latest[0].ID != accounts[4].ID || latest[1].ID != accounts[0].ID {
		t.Fatalf("visible latest readers=%+v more=%t err=%v", latest, more, err)
	}
	if latest[1].Bio != "Public bio" || latest[1].Nickname != "Listed" {
		t.Fatalf("public identity lost: %+v", latest[1])
	}
	active, err := activeReaders()
	if err != nil || len(active) != 1 || active[0].ID != accounts[0].ID || active[0].ActivityCount != 4 {
		t.Fatalf("only public post and clean comment count: %+v err=%v", active, err)
	}
	activity, err := recentReaderActivity()
	if err != nil || len(activity) != 2 || activity[0].Kind != "comment" || activity[0].CommentID != good.ID ||
		activity[0].AccountID != accounts[0].ID || activity[0].ArticleSlug != public.Slug || activity[0].ArticleTitle != public.Title ||
		activity[0].Excerpt != good.Content || activity[1].Kind != "article" || activity[1].ArticleSlug != public.Slug {
		t.Fatalf("only public activity should reach feed: %+v err=%v", activity, err)
	}
	app := fiber.New()
	app.Get("/readers/", readerCircle)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/readers/?page=1001", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unbounded directory pagination status=%d", resp.StatusCode)
	}
}
