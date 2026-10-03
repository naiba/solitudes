package router

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func settingsAccountFixture(t *testing.T) (*fiber.App, *model.Config, *model.Account) {
	t.Helper()
	t.Chdir(t.TempDir())
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	for _, theme := range []struct{ kind, id string }{{"site", "cactus"}, {"admin", "default"}} {
		dir := filepath.Join("resource", "themes", theme.kind, theme.id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"id":"`+theme.id+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("current-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &model.Config{ConfigFilePath: "settings.yml"}
	cfg.Site.Theme, cfg.Admin.Theme = "cactus", "default"
	cfg.User = model.User{Email: "bootstrap@example.test", Nickname: "Bootstrap administrator", Password: string(hash)}
	account := &model.Account{Email: "actual-admin@example.test", Nickname: "Actual administrator", PasswordHash: string(hash), Role: model.RoleAdmin}
	solitudes.System = &solitudes.SysVariable{Config: cfg}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(solitudes.CtxAccount, account)
		return c.Next()
	})
	app.Post("/admin/settings", requireAdmin, settingsHandler)
	return app, cfg, account
}

func submitSettingsWithAccountFields(t *testing.T, app *fiber.App, encoding string) {
	t.Helper()
	fields := map[string]string{"site_title": "Saved site settings"}
	if encoding != "without-account-fields" {
		// Even a correct current password must not turn site settings into a
		// second account mutation endpoint.
		fields["nickname"] = "Injected nickname"
		fields["email"] = "injected@example.test"
		fields["old_password"] = "current-test-password"
		fields["new_password"] = "injected-new-password"
	}
	var body bytes.Buffer
	var contentType string
	switch encoding {
	case "form":
		values := url.Values{}
		for key, value := range fields {
			values.Set(key, value)
		}
		body.WriteString(values.Encode())
		contentType = fiber.MIMEApplicationForm
	case "multipart":
		writer := multipart.NewWriter(&body)
		for key, value := range fields {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		contentType = writer.FormDataContentType()
	default:
		if err := json.NewEncoder(&body).Encode(fields); err != nil {
			t.Fatal(err)
		}
		contentType = fiber.MIMEApplicationJSON
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/settings", &body)
	req.Header.Set(fiber.HeaderContentType, contentType)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save settings: status %d", resp.StatusCode)
	}
}

func TestSettingsCannotModifyBootstrapOrCurrentAccount(t *testing.T) {
	app, cfg, account := settingsAccountFixture(t)
	bootstrap, originalAccount := cfg.User, *account
	for _, encoding := range []string{"json", "form", "multipart", "without-account-fields"} {
		t.Run(encoding, func(t *testing.T) {
			submitSettingsWithAccountFields(t, app, encoding)
			if cfg.User != bootstrap || account.Email != originalAccount.Email || account.Nickname != originalAccount.Nickname || account.PasswordHash != originalAccount.PasswordHash {
				t.Fatal("site settings modified account data")
			}
			data, err := os.ReadFile(cfg.ConfigFilePath)
			if err != nil {
				t.Fatal(err)
			}
			var saved model.Config
			if err := yaml.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.User != bootstrap || saved.Site.SpaceName != "Saved site settings" {
				t.Fatal("saved configuration did not preserve bootstrap data and apply site settings")
			}
		})
	}
}

func TestPostgresSettingsCannotModifyAccountOrRevokeSessions(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	app, _, account := settingsAccountFixture(t)
	solitudes.System.DB = db
	if err := db.AutoMigrate(&model.OIDCAccessToken{}, &model.OIDCRefreshToken{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(account).Error; err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)
	session := model.LoginSession{AccountID: account.ID, TokenHash: strings.Repeat("a", 64), ExpiresAt: expires}
	access := model.OIDCAccessToken{AccountID: account.ID, ClientID: "settings-test-client", ExpiresAt: expires}
	if err := db.Create(&access).Error; err != nil {
		t.Fatal(err)
	}
	refresh := model.OIDCRefreshToken{AccountID: account.ID, ClientID: "settings-test-client", AccessID: access.ID, TokenHash: strings.Repeat("b", 64), ExpiresAt: expires}
	for _, record := range []interface{}{&session, &refresh} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, encoding := range []string{"json", "form", "multipart", "without-account-fields"} {
		t.Run(encoding, func(t *testing.T) {
			submitSettingsWithAccountFields(t, app, encoding)
			var persisted model.Account
			if err := db.First(&persisted, "id = ?", account.ID).Error; err != nil {
				t.Fatal(err)
			}
			if persisted.Email != account.Email || persisted.Nickname != account.Nickname || persisted.PasswordHash != account.PasswordHash || persisted.Role != account.Role {
				t.Fatal("site settings modified the persisted account")
			}
			for _, table := range []interface{}{&model.LoginSession{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
				var count int64
				if err := db.Model(table).Where("account_id = ?", account.ID).Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("site settings revoked %T: count=%d err=%v", table, count, err)
				}
			}
		})
	}
}
