package router

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestPostgresOwnedClientUsageAndThemes(t *testing.T) {
	db, otherOwner, owner := auditTestDB(t)
	now := time.Now().Truncate(time.Second)
	create := func(value interface{}) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	clients := []model.OIDCClient{
		{ID: "owned", OwnerID: &owner.ID, Name: "My app", RedirectURIsJSON: "[]", SecretHash: "never-render-secret"},
		{ID: "disabled", OwnerID: &owner.ID, Name: "Disabled app", RedirectURIsJSON: "[]", DisabledAt: &now},
		{ID: "foreign", OwnerID: &otherOwner.ID, Name: "Foreign app", RedirectURIsJSON: "[]"},
	}
	create(&clients)
	accounts := []model.Account{
		{Email: "expired@usage.test", Nickname: "Expired", EmailVerifiedAt: &now},
		{Email: "disabled@usage.test", Nickname: "Disabled", EmailVerifiedAt: &now, DisabledAt: &now},
		{Email: "unverified@usage.test", Nickname: "Unverified"},
		{Email: "pending@usage.test", Nickname: "Pending", EmailVerifiedAt: &now},
	}
	create(&accounts)
	accesses := []model.OIDCAccessToken{
		{ClientID: "owned", AccountID: owner.ID, ExpiresAt: now.Add(time.Hour)},
		{ClientID: "owned", AccountID: owner.ID, ExpiresAt: now.Add(time.Hour)},
		{ClientID: "owned", AccountID: accounts[0].ID, ExpiresAt: now},
		{ClientID: "owned", AccountID: accounts[1].ID, ExpiresAt: now.Add(time.Hour)},
		{ClientID: "owned", AccountID: accounts[2].ID, ExpiresAt: now.Add(time.Hour)},
		{ClientID: "disabled", AccountID: owner.ID, ExpiresAt: now.Add(time.Hour)},
		{ClientID: "foreign", AccountID: accounts[0].ID, ExpiresAt: now.Add(time.Hour)},
		{ClientID: "owned", AccountID: otherOwner.ID, ExpiresAt: now.Add(-time.Hour)},
	}
	create(&accesses)
	create(&[]model.OIDCRefreshToken{
		{ClientID: "owned", AccountID: owner.ID, AccessID: accesses[0].ID, TokenHash: "owner-refresh", ExpiresAt: now.Add(time.Hour)},
		{ClientID: "owned", AccountID: otherOwner.ID, AccessID: accesses[7].ID, TokenHash: "refresh-only", ExpiresAt: now.Add(time.Hour)},
		{ClientID: "owned", AccountID: accounts[0].ID, AccessID: accesses[2].ID, TokenHash: "expired", ExpiresAt: now.Add(-time.Second)},
	})
	create(&model.OIDCAuthRequest{ClientID: "owned", AccountID: &accounts[3].ID, Approved: true, RequestJSON: []byte(`{}`), ExpiresAt: now.Add(time.Hour)})
	for i := 0; i < 2; i++ {
		create(&model.AuditEvent{ID: fmt.Sprint("login-", i), Action: "oidc.login", Outcome: "success", ClientID: "owned", ActorID: owner.ID})
	}
	create(&model.AuditEvent{ID: "failed", Action: "oidc.login", Outcome: "failure", ClientID: "owned", ActorID: accounts[0].ID})
	create(&model.AuditEvent{ID: "site", Action: "session.login", Outcome: "success", ClientID: "owned", ActorID: accounts[0].ID})
	create(&[]model.LoginSummary{
		{Action: "oidc.login", ClientID: "owned", ActorID: owner.ID, LoginCount: 9, LastLoginAt: now},
		{Action: "oidc.login", ClientID: "owned", ActorID: otherOwner.ID, LoginCount: 5, LastLoginAt: now},
		{Action: "oidc.login", ClientID: "foreign", ActorID: accounts[0].ID, LoginCount: 3, LastLoginAt: now},
	})
	usage, err := ownedClientUsage(db, owner.ID, []string{"owned", "disabled", "foreign"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 2 || usage["owned"].LoginUsers != 2 || usage["owned"].ActiveUsers != 2 || usage["disabled"].ActiveUsers != 0 {
		t.Fatalf("incorrect deduplication, expiry/status filtering or owner boundary: %+v", usage)
	}
	if empty, err := ownedClientUsage(db, owner.ID, nil, now); err != nil || len(empty) != 0 {
		t.Fatal("empty page must need no statistics")
	}

	t.Chdir("..")
	previousEngine, previousTranslations := globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() { globalDynamicEngine.engine, translator.Trans = previousEngine, previousTranslations })
	for _, theme := range []string{"cactus", "folio"} {
		solitudes.System.Config.Site.Theme, solitudes.System.Config.Admin.Theme = theme, "default"
		if err := LoadTemplates(); err != nil {
			t.Fatal(err)
		}
		viewer := &owner
		app := fiber.New(fiber.Config{Views: globalDynamicEngine})
		app.Use(trans)
		app.Use(func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, viewer); return c.Next() })
		app.Get("/account/oidc/clients", requireAccount, oidcClientsPage)
		for _, language := range []string{"zh", "en"} {
			req := httptest.NewRequest("GET", "/account/oidc/clients?owner_id="+otherOwner.ID, nil)
			req.Header.Set("Accept-Language", language)
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 200 {
				t.Fatalf("%s render: %d %v", theme, resp.StatusCode, err)
			}
			if resp.Header.Get("Cache-Control") != "private, no-store" {
				t.Fatal("private app statistics must not be cached")
			}
			if strings.Contains(doc.Text(), "Foreign app") || strings.Contains(doc.Text(), "never-render-secret") {
				t.Fatal("page leaked another owner's app or credentials")
			}
			if doc.Find(`[data-testid="account-client-row"]`).Length() != 2 {
				t.Fatal("missing owned applications")
			}
			doc.Find(`[data-testid="account-client-row"]`).Each(func(_ int, row *goquery.Selection) {
				id := row.Find("code").First().Text()
				want := "0"
				if id == "owned" {
					want = "2"
				}
				if row.Find(`[data-testid="oidc-client-active-users"]`).Text() != want || row.Find(`[data-testid="oidc-client-login-users"]`).Text() != want {
					t.Errorf("%s %s: wrong statistics", theme, id)
				}
			})
		}
		viewer = nil
		resp, err := app.Test(httptest.NewRequest("GET", "/account/oidc/clients", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 302 {
			t.Fatal("anonymous user reached app statistics")
		}
	}
	// Revoking credentials changes only the current count, not lifetime users.
	if err := deleteOIDCCredentials(db, "owned", owner.ID); err != nil {
		t.Fatal(err)
	}
	usage, err = ownedClientUsage(db, owner.ID, []string{"owned"}, now)
	if err != nil || usage["owned"].ActiveUsers != 1 || usage["owned"].LoginUsers != 2 {
		t.Fatalf("revocation statistics: %+v %v", usage, err)
	}
}
