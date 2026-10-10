package router

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"
	html "github.com/gofiber/template/html/v2"

	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestAdminEmailVerificationPresentation(t *testing.T) {
	t.Chdir("..")
	previous := translator.Trans
	t.Cleanup(func() { translator.Trans = previous })
	translator.Reload("cactus", "default")
	engine := html.New("resource/themes/admin/default/templates", ".html")
	setFuncMap(engine)
	if err := engine.Load(); err != nil {
		t.Fatal(err)
	}
	verifiedAt := time.Date(2026, time.January, 2, 3, 4, 0, 0, time.UTC)
	for _, locale := range []string{"en", "zh"} {
		tr, _ := translator.Trans.GetTranslator(locale)
		translated := &translator.Translator{Translator: tr, Trans: tr}
		for _, verified := range []bool{false, true} {
			for _, disabled := range []bool{false, true} {
				user := adminUserView{ID: "account-under-review", Nickname: "Reader", Email: "reader@example.test", Role: model.RoleUser}
				if verified {
					user.EmailVerifiedAt = &verifiedAt
				}
				if disabled {
					user.DisabledAt = &verifiedAt
				}
				for _, view := range []string{"admin/users", "admin/user_detail"} {
					t.Run(locale+"/"+view+"/"+map[bool]string{true: "verified", false: "unverified"}[verified]+"/"+map[bool]string{true: "disabled", false: "active"}[disabled], func(t *testing.T) {
						data := fiber.Map{"Tr": translated, "Conf": &model.Config{}, "Account": &model.Account{ID: "admin", Role: model.RoleAdmin}, "Path": "/admin/users", "Data": fiber.Map{"users": []adminUserView{user}, "user": user, "overview": identityTotals{}}}
						var output bytes.Buffer
						if err := engine.Render(&output, view, data); err != nil {
							t.Fatal(err)
						}
						doc, err := goquery.NewDocumentFromReader(strings.NewReader(output.String()))
						if err != nil {
							t.Fatal(err)
						}
						status := doc.Find(`[data-testid="admin-email-verification"]`)
						key := "identity_unverified"
						state := "unverified"
						if verified {
							key = "identity_email_verified"
							state = "verified"
						}
						if status.Length() != 1 || strings.TrimSpace(status.Text()) != translated.T(key) || status.AttrOr("data-verification-state", "") != state {
							t.Fatalf("missing explicit email verification status: %s", output.String())
						}
						stamp := doc.Find(`[data-testid="admin-email-verified-at"]`)
						if view == "admin/user_detail" && verified {
							if stamp.Length() != 1 || stamp.AttrOr("datetime", "") != verifiedAt.Format(time.RFC3339) || stamp.Text() != "2026-01-02 03:04" {
								t.Fatal("missing verification timestamp")
							}
						} else if stamp.Length() != 0 {
							t.Fatal("invented or misplaced verification timestamp")
						}
						if disabled && !strings.Contains(doc.Text(), translated.T("disabled")) {
							t.Fatal("email status obscured account disablement")
						}
					})
				}
			}
		}
	}
}
