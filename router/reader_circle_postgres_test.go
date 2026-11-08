package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	if err != nil || len(active) != 1 || active[0].ID != accounts[0].ID || active[0].ActivityCount != 5 {
		t.Fatalf("only public post and clean comment count: %+v err=%v", active, err)
	}
	activity, err := recentReaderActivity()
	if err != nil || len(activity) != 3 || activity[0].Kind != "comment" || activity[0].CommentID != good.ID ||
		activity[0].AccountID != accounts[0].ID || activity[0].ArticleSlug != public.Slug || activity[0].ArticleTitle != public.Title ||
		activity[0].Excerpt != good.Content || activity[1].Kind != "article" || activity[1].ArticleSlug != public.Slug || activity[2].CommentID != old.ID {
		t.Fatalf("only public activity should reach feed: %+v err=%v", activity, err)
	}
	app := newTestRouter()
	app.Get("/readers/", readerCircle)
	resp, err := testRequest(app, httptest.NewRequest(http.MethodGet, "/readers/?page=1001", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unbounded directory pagination status=%d", resp.StatusCode)
	}
}

// Quiet communities must still show their latest contributions, even years later.
func TestPostgresReaderCircleKeepsOlderActivityInLatestOrder(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	solitudes.System = &solitudes.SysVariable{DB: db}
	verified := time.Now().AddDate(-2, 0, 0)
	author := model.Account{Nickname: "Earlier author", Email: "earlier-author@example.test", Role: model.RoleEditor, EmailVerifiedAt: &verified}
	reader := model.Account{Nickname: "Latest participant", Email: "latest-participant@example.test", Role: model.RoleUser, EmailVerifiedAt: &verified}
	for _, account := range []*model.Account{&author, &reader} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	var posts []model.Article
	for day := 0; day < 13; day++ {
		post := model.Article{AuthorID: &author.ID, Title: fmt.Sprintf("Archive story %d", day), Slug: fmt.Sprintf("archive-story-%d", day), Content: "Public archive", CreatedAt: verified.AddDate(0, 0, day)}
		if err := db.Create(&post).Error; err != nil {
			t.Fatal(err)
		}
		posts = append(posts, post)
	}
	comment := model.Comment{AccountID: &reader.ID, ArticleID: &posts[0].ID, Content: "Latest public conversation", CreatedAt: verified.AddDate(0, 0, 14)}
	if err := db.Create(&comment).Error; err != nil {
		t.Fatal(err)
	}
	activity, err := recentReaderActivity()
	if err != nil {
		t.Fatal(err)
	}
	if len(activity) != 12 {
		t.Fatalf("expected latest 12 archived contributions, got %d", len(activity))
	}
	if activity[0].CommentID != comment.ID || activity[0].AccountID != reader.ID {
		t.Fatalf("latest comment missing: %+v", activity[0])
	}
	for index, event := range activity[1:] {
		if event.Kind != "article" || event.ArticleSlug != posts[12-index].Slug {
			t.Fatalf("feed order at %d: %+v", index+1, event)
		}
	}
	active, err := activeReaders()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 || active[0].ID != reader.ID || active[0].ActivityCount != 1 || active[1].ID != author.ID || active[1].ActivityCount != 39 {
		t.Fatalf("members must follow latest participation, including old activity: %+v", active)
	}
}
