package router

import (
	"bytes"
	"net/url"
	"testing"

	"github.com/PuerkitoBio/goquery"
	html "github.com/gofiber/template/html/v2"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestAccountPasswordSetupThemes(t *testing.T) {
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
		for _, passwordHash := range []string{"", "existing-password-hash"} {
			t.Run(theme+"/"+passwordHash, func(t *testing.T) {
				var output bytes.Buffer
				data := viewData{"Tr": &translator.Translator{Translator: tr, Trans: tr}, "Conf": &model.Config{}, "Theme": viewData{}, "Data": viewData{"passkeys_navigation": viewData{"Previous": "", "Next": ""}},
					"Account": &model.Account{ID: "oauth-reader", Role: model.RoleUser, PasswordHash: passwordHash}}
				if err := engine.Render(&output, "site/account", data); err != nil {
					t.Fatal(err)
				}
				doc, err := goquery.NewDocumentFromReader(&output)
				if err != nil {
					t.Fatal(err)
				}
				form := doc.Find(`[data-testid="account-password-form"]`)
				if form.Length() != 1 || form.AttrOr("action", "") != "/account/password" || form.AttrOr("method", "") != "post" {
					t.Fatal("password setup/change form missing")
				}
				old := form.Find(`[name="old_password"]`)
				if passwordHash == "" && old.Length() != 0 || passwordHash != "" && old.Length() != 1 {
					t.Fatal("current password input does not match account credentials")
				}
				input := form.Find(`[name="new_password"]`)
				if input.Length() != 1 || input.AttrOr("type", "") != "password" || input.AttrOr("autocomplete", "") != "new-password" || input.AttrOr("minlength", "") != "12" || input.AttrOr("maxlength", "") != "72" {
					t.Fatal("new password input missing or incorrectly constrained")
				}
				want := "Set password"
				if passwordHash != "" {
					want = "Change password"
				}
				if form.Find(`[data-testid="account-password-submit"]`).Text() != want {
					t.Fatalf("password action does not describe account state; want %q", want)
				}
			})
		}
		t.Run(theme+"/OAuth registration", func(t *testing.T) {
			config := &model.Config{}
			config.Auth.GitHub.ClientID, config.Auth.GitHub.ClientSecret = "github-client", "github-secret"
			config.Auth.Google.ClientID, config.Auth.Google.ClientSecret = "google-client", "google-secret"
			config.Auth.OIDC.Issuer, config.Auth.OIDC.ClientID, config.Auth.OIDC.ClientSecret = "https://identity.example", "oidc-client", "oidc-secret"
			for _, enabled := range []bool{true, false} {
				if !enabled {
					config.Auth.GitHub.ClientSecret = ""
					config.Auth.Google.ClientSecret = ""
					config.Auth.OIDC.ClientSecret = ""
				}
				var output bytes.Buffer
				data := viewData{"Tr": &translator.Translator{Translator: tr, Trans: tr}, "Conf": config, "Theme": viewData{},
					"Data": viewData{"registration_available": false, "return_to": "/account?section=security"}}
				if err := engine.Render(&output, "site/register", data); err != nil {
					t.Fatal(err)
				}
				doc, err := goquery.NewDocumentFromReader(&output)
				if err != nil {
					t.Fatal(err)
				}
				for _, provider := range []string{"github", "google", "oidc"} {
					link := doc.Find(`a[href^="/auth/` + provider + `?"]`)
					if !enabled {
						if link.Length() != 0 {
							t.Fatalf("incomplete %s configuration exposed registration", provider)
						}
						continue
					}
					if link.Length() != 1 {
						t.Fatalf("%s registration unavailable without SMTP", provider)
					}
					target, err := url.Parse(link.AttrOr("href", ""))
					if err != nil || target.Query().Get("return_to") != "/account?section=security" {
						t.Fatalf("OAuth registration lost return destination: %v", err)
					}
				}
			}
		})
	}
}
