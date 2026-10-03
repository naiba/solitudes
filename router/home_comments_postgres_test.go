package router

import (
	"strings"
	"testing"
	"time"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresHomeCommentsOnlyShowPublicConversations(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	solitudes.System = &solitudes.SysVariable{DB: db}
	now := time.Now()
	accounts := []model.Account{
		{Nickname: "Visible", Email: "visible@example.test", Role: model.RoleUser, EmailVerifiedAt: &now},
		{Nickname: "Exited", Email: "exited@example.test", Role: model.RoleUser, EmailVerifiedAt: &now, DirectoryHidden: true},
		{Nickname: "Unverified", Email: "unverified@example.test", Role: model.RoleUser},
		{Nickname: "Disabled", Email: "disabled@example.test", Role: model.RoleUser, EmailVerifiedAt: &now, DisabledAt: &now},
	}
	for i := range accounts {
		if err := db.Create(&accounts[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	public := model.Article{AuthorID: &accounts[0].ID, Slug: "conversation", Title: "A public story", Content: "Public"}
	private := model.Article{AuthorID: &accounts[0].ID, Slug: "private-conversation", Title: "Private", Content: "Private", Visibility: model.VisibilityPrivate}
	for _, article := range []*model.Article{&public, &private} {
		if err := db.Create(article).Error; err != nil {
			t.Fatal(err)
		}
	}
	guest := model.Comment{ArticleID: &public.ID, Nickname: "Guest", Email: "guest-private@example.test", Content: "A guest reply", CreatedAt: now.Add(-time.Minute)}
	member := model.Comment{ArticleID: &public.ID, AccountID: &accounts[0].ID, Nickname: "Outdated", Content: "<img src=x onerror=alert(1)> Member reply", CreatedAt: now}
	spam := model.Comment{ArticleID: &public.ID, Nickname: "Spam", Content: "Spam root", IsSpam: true, CreatedAt: now.Add(time.Minute)}
	for _, comment := range []*model.Comment{&guest, &member, &spam} {
		if err := db.Create(comment).Error; err != nil {
			t.Fatal(err)
		}
	}
	invalid := []model.Comment{
		{ArticleID: &private.ID, AccountID: &accounts[0].ID, Content: "Private reply"},
		{ArticleID: &public.ID, ReplyTo: &spam.ID, Content: "Reply under spam"},
	}
	for i := 1; i < len(accounts); i++ {
		invalid = append(invalid, model.Comment{ArticleID: &public.ID, AccountID: &accounts[i].ID, Content: "Hidden member reply"})
	}
	for i := range invalid {
		if err := db.Create(&invalid[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	comments, err := recentPublicComments(solitudes.System.DB, 5)
	if err != nil || len(comments) != 2 {
		t.Fatalf("public comments=%+v err=%v", comments, err)
	}
	if comments[0].ID != member.ID || comments[0].Nickname != "Visible" || comments[0].AccountID != accounts[0].ID ||
		comments[0].Role != "user" || comments[0].ArticleSlug != public.Slug || comments[0].ArticleTitle != public.Title ||
		!strings.Contains(comments[0].Content, "Member reply") {
		t.Fatalf("member comment mapping: %+v", comments[0])
	}
	if comments[1].ID != guest.ID || comments[1].Nickname != "Guest" || comments[1].AccountID != "" ||
		comments[1].Role != "guest" || comments[1].Content != guest.Content {
		t.Fatalf("guest comment mapping: %+v", comments[1])
	}
}
