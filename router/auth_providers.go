package router

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

type loginProviderForm struct {
	GitHubID      string `form:"github_id"`
	GitHubSecret  string `form:"github_secret"`
	GitHubDisable bool   `form:"github_disable"`
	GoogleID      string `form:"google_id"`
	GoogleSecret  string `form:"google_secret"`
	GoogleDisable bool   `form:"google_disable"`
	OIDCIssuer    string `form:"oidc_issuer"`
	OIDCID        string `form:"oidc_id"`
	OIDCSecret    string `form:"oidc_secret"`
	OIDCDisable   bool   `form:"oidc_disable"`
}

func loginProvidersPage(writer http.ResponseWriter, httpRequest *http.Request) error {
	return renderPage(writer, httpRequest, http.StatusOK, "admin/login_providers", injectSiteData(writer, httpRequest, viewData{
		"title":         requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator).T("login_providers"),
		"callback_base": publicBaseURL(),
		"saved":         queryValue(httpRequest, "saved") == "1",
	}))
}

func updateLoginCredentials(id, secret *string, newID, newSecret string, disable bool) error {
	if disable {
		*id, *secret = "", ""
		return nil
	}
	// Keep persistent credentials independent of the parsed form strings.
	newID, newSecret = strings.Clone(strings.TrimSpace(newID)), strings.Clone(strings.TrimSpace(newSecret))
	if newID == "" {
		if newSecret != "" {
			return newHTTPError(http.StatusBadRequest, "client ID is required with a new secret")
		}
		*id, *secret = "", ""
		return nil
	}
	if newSecret == "" && (newID != *id || *secret == "") {
		return newHTTPError(http.StatusBadRequest, "new client ID requires a client secret")
	}
	if newSecret != "" {
		*secret = newSecret
	}
	*id = newID
	return nil
}

func applyLoginProviderForm(config *model.Config, form loginProviderForm) error {
	if err := updateLoginCredentials(&config.Auth.GitHub.ClientID, &config.Auth.GitHub.ClientSecret,
		form.GitHubID, form.GitHubSecret, form.GitHubDisable); err != nil {
		return newHTTPError(http.StatusBadRequest, "GitHub: "+err.Error())
	}
	if err := updateLoginCredentials(&config.Auth.Google.ClientID, &config.Auth.Google.ClientSecret,
		form.GoogleID, form.GoogleSecret, form.GoogleDisable); err != nil {
		return newHTTPError(http.StatusBadRequest, "Google: "+err.Error())
	}
	if err := updateLoginCredentials(&config.Auth.OIDC.ClientID, &config.Auth.OIDC.ClientSecret,
		form.OIDCID, form.OIDCSecret, form.OIDCDisable); err != nil {
		return newHTTPError(http.StatusBadRequest, "OIDC: "+err.Error())
	}
	if config.Auth.OIDC.ClientID == "" {
		config.Auth.OIDC.Issuer = ""
		return nil
	}
	issuer := strings.Clone(strings.TrimRight(strings.TrimSpace(form.OIDCIssuer), "/"))
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return newHTTPError(http.StatusBadRequest, "OIDC issuer must be a valid HTTPS URL")
	}
	config.Auth.OIDC.Issuer = issuer
	return nil
}

func saveLoginProviders(writer http.ResponseWriter, httpRequest *http.Request) error {
	var form loginProviderForm
	if err := decodeRequestBody(httpRequest, &form); err != nil {
		return err
	}
	candidate := *solitudes.System.Config
	if err := applyLoginProviderForm(&candidate, form); err != nil {
		return err
	}
	if err := candidate.Save(); err != nil {
		return fmt.Errorf("save login providers: %w", err)
	}
	*solitudes.System.Config = candidate
	return redirectResponse(writer, httpRequest, "/admin/auth/providers?saved=1", http.StatusSeeOther)
}
