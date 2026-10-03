package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestFillCommentEntryTrustsAuthenticatedAccount(t *testing.T) {
	for _, tt := range []struct {
		name                             string
		account                          *model.Account
		wantID                           bool
		wantName, wantEmail, wantWebsite string
		wantAdmin                        bool
	}{
		{"guest", nil, false, "Visitor", "visitor@example.test", "https://example.test", false},
		{"member", &model.Account{ID: "account-user", Role: model.RoleUser, Nickname: "Member", Email: "member@example.test"}, true, "Member", "member@example.test", "", false},
		{"editor", &model.Account{ID: "account-editor", Role: model.RoleEditor, Nickname: "Editor", Email: "editor@example.test"}, true, "Editor", "editor@example.test", "", false},
		{"admin", &model.Account{ID: "account-admin", Role: model.RoleAdmin, Nickname: "Admin", Email: "admin@example.test"}, true, "Admin", "admin@example.test", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Post("/comment", func(c *fiber.Ctx) error {
				if tt.account != nil {
					c.Locals(solitudes.CtxAccount, tt.account)
				}
				cm := &model.Comment{}
				form := &commentForm{Nickname: "Visitor", Email: "visitor@example.test", Website: "https://example.test", Content: "Hello", Version: 1}
				if err := fillCommentEntry(c, tt.wantAdmin, cm, form, &model.Article{ID: "article-id"}); err != nil {
					return err
				}
				if (cm.AccountID != nil) != tt.wantID || cm.Nickname != tt.wantName || cm.Email != tt.wantEmail || cm.Website != tt.wantWebsite || cm.IsAdmin != tt.wantAdmin {
					t.Errorf("stored comment identity: %+v", cm)
				}
				if cm.AccountID != nil && *cm.AccountID != tt.account.ID {
					t.Errorf("account id = %q, want %q", *cm.AccountID, tt.account.ID)
				}
				return c.SendStatus(http.StatusOK)
			})
			resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/comment", nil))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
		})
	}
}
