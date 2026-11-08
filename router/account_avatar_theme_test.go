package router

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"testing"

	"github.com/PuerkitoBio/goquery"
	html "github.com/gofiber/template/html/v2"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestAccountAvatarThemeConsistency(t *testing.T) {
	t.Chdir("..")
	previous := translator.Trans
	t.Cleanup(func() { translator.Trans = previous })
	for _, theme := range []string{"cactus", "folio"} {
		translator.Reload(theme, "default")
		tr, _ := translator.Trans.GetTranslator("en")
		engine := html.New("resource/themes/site/"+theme+"/templates", ".html")
		setFuncMap(engine)
		if err := engine.Load(); err != nil {
			t.Fatal(err)
		}
		for _, nickname := range []string{"Reader", "改名后的读者"} {
			account := &model.Account{ID: "stable-reader", Nickname: nickname, Email: "private@example.test", Role: model.RoleUser}
			comment := &model.Comment{ID: "member-comment", AccountID: &account.ID, Account: account, Email: account.Email, Article: &model.Article{Slug: "article"}}
			guest := &model.Comment{ID: "guest-comment", Nickname: "Guest", Email: "guest@example.test", Article: comment.Article}
			missingAccount := &model.Comment{ID: "unloaded-account-comment", AccountID: &account.ID, Nickname: "Old nickname", Email: account.Email, Article: comment.Article}
			data := viewData{"Tr": &translator.Translator{Translator: tr, Trans: tr}, "tr": &translator.Translator{Translator: tr, Trans: tr}, "Conf": &model.Config{}, "Theme": viewData{}, "Account": account, "comments": []*model.Comment{comment}, "Data": viewData{"profile": account, "page": 1, "articles_page": 1, "comments_page": 1, "passkeys_navigation": viewData{"Previous": "", "Next": ""}}}
			for view, selector := range map[string]string{"site/account": ".account-summary img", "site/user_profile": ".public-profile-avatar", "site/comments_entry": "[data-testid=comment-entry] img"} {
				t.Run(theme+"/"+nickname+"/"+view, func(t *testing.T) {
					var output bytes.Buffer
					if err := engine.Render(&output, view, data); err != nil {
						t.Fatal(err)
					}
					doc, err := goquery.NewDocumentFromReader(&output)
					if err != nil {
						t.Fatal(err)
					}
					avatar := doc.Find(selector).First()
					if avatar.Length() != 1 || avatar.AttrOr("src", "") != "https://api.dicebear.com/9.x/lorelei/svg?seed=stable-reader" {
						t.Fatalf("avatar must use the stable account ID: %s", avatar.AttrOr("src", ""))
					}
				})
			}
			t.Run(theme+"/"+nickname+"/guest and unloaded account", func(t *testing.T) {
				data["comments"] = []*model.Comment{guest, missingAccount}
				var output bytes.Buffer
				if err := engine.Render(&output, "site/comments_entry", data); err != nil {
					t.Fatal(err)
				}
				doc, err := goquery.NewDocumentFromReader(&output)
				if err != nil {
					t.Fatal(err)
				}
				avatars := doc.Find("[data-testid=comment-entry] img")
				if avatars.Length() != 2 {
					t.Fatalf("expected two comment avatars, got %d", avatars.Length())
				}
				wantGuest := fmt.Sprintf("https://gravatar.loli.net/avatar/%x.jpg?s=100&d=mm&r=g", md5.Sum([]byte(guest.Email)))
				if avatars.Eq(0).AttrOr("src", "") != wantGuest {
					t.Fatal("guest Gravatar behavior changed")
				}
				if avatars.Eq(1).AttrOr("src", "") != "https://api.dicebear.com/9.x/lorelei/svg?seed=stable-reader" {
					t.Fatal("unloaded account must not use email or nickname as its avatar seed")
				}
			})
		}
	}
}
