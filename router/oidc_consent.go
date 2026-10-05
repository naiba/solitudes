package router

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

type oidcBrowserKey struct{}
type oidcBrowserIdentity struct{ sessionHash string }

func oidcBrowser(c *fiber.Ctx) oidcBrowserIdentity {
	if c.Cookies(solitudes.AuthCookie) == "" {
		return oidcBrowserIdentity{}
	}
	// Protocol routes precede the site's auth middleware. Resolve the cookie
	// against live sessions below, never trust request parameters or ID hints.
	return oidcBrowserIdentity{secretHash(c.Cookies(solitudes.AuthCookie))}
}

type oidcConsentDecision string

const (
	oidcConsentReady oidcConsentDecision = ""
	oidcNeedsLogin   oidcConsentDecision = "login_required"
	oidcNeedsConsent oidcConsentDecision = "consent_required"
)

func (d oidcConsentDecision) protocolError() error {
	if d == oidcNeedsLogin {
		return oidc.ErrLoginRequired()
	}
	return &oidc.Error{ErrorType: "consent_required"}
}

// Only live credentials prove reusable consent; login history and old approved
// requests cannot resurrect a revoked/expired grant. UNION bounds duplicate scopes.
func oidcScopesGranted(db *gorm.DB, clientID, accountID string, scopes []string, now time.Time) (bool, error) {
	var existing []string
	err := db.Raw(`SELECT scopes FROM o_id_c_access_tokens WHERE client_id = ? AND account_id = ? AND expires_at > ?
UNION SELECT scopes FROM o_id_c_refresh_tokens WHERE client_id = ? AND account_id = ? AND expires_at > ?`,
		clientID, accountID, now, clientID, accountID, now).Scan(&existing).Error
	if err != nil || len(existing) == 0 {
		return false, err
	}
	granted := map[string]bool{}
	for _, value := range existing {
		for _, scope := range strings.Fields(value) {
			granted[scope] = true
		}
	}
	for _, scope := range scopes {
		if !granted[scope] {
			return false, nil
		}
	}
	return true, nil
}

func oidcFreshAuthentication(request *oidcAuthRequest, authenticatedAt, now time.Time) bool {
	force := slices.Contains(request.req.Prompt, oidc.PromptLogin) || slices.Contains(request.req.Prompt, oidc.PromptSelectAccount)
	if force || (request.req.MaxAge != nil && *request.req.MaxAge == 0) {
		return !authenticatedAt.Before(request.CreatedAt)
	}
	return request.req.MaxAge == nil || now.Sub(authenticatedAt).Seconds() <= float64(*request.req.MaxAge)
}

// Call while holding the client lock, shared with revoke/disable/token issuance.
// An ID-token hint constrains the subject but is never authentication by itself.
func evaluateOIDCConsent(db *gorm.DB, request *oidcAuthRequest, browser oidcBrowserIdentity, explicit bool, now time.Time) (oidcConsentDecision, error) {
	if browser.sessionHash == "" {
		return oidcNeedsLogin, nil
	}
	var session model.LoginSession
	err := db.Table("login_sessions AS s").Select("s.*").Joins("JOIN accounts a ON a.id = s.account_id").
		Where("s.token_hash = ? AND s.expires_at > ? AND a.disabled_at IS NULL AND a.email_verified_at IS NOT NULL",
			browser.sessionHash, now).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return oidcNeedsLogin, nil
	}
	if err != nil {
		return oidcNeedsLogin, err
	}
	if request.AccountID != nil && *request.AccountID != session.AccountID {
		return oidcNeedsLogin, nil
	}
	if request.Approved {
		return oidcConsentReady, nil
	}
	if !oidcFreshAuthentication(request, session.CreatedAt, now) {
		return oidcNeedsLogin, nil
	}
	if !explicit {
		if slices.Contains(request.req.Prompt, oidc.PromptConsent) {
			return oidcNeedsConsent, nil
		}
		granted, err := oidcScopesGranted(db, request.ClientID, session.AccountID, request.GetScopes(), now)
		if err != nil || !granted {
			return oidcNeedsConsent, err
		}
	}
	request.AccountID, request.AuthTime, request.Approved = &session.AccountID, &session.CreatedAt, true
	return oidcConsentReady, nil
}

func completeOIDCConsent(c *fiber.Ctx, original *oidcAuthRequest, explicit bool) (*oidcAuthRequest, oidcConsentDecision, error) {
	var request *oidcAuthRequest
	var decision oidcConsentDecision
	err := solitudes.System.DB.WithContext(c.UserContext()).Transaction(func(tx *gorm.DB) error {
		if _, err := lockOIDCClient(tx, original.ClientID, true); err != nil {
			return err
		}
		var row model.OIDCAuthRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND client_id = ? AND expires_at > ?", original.ID, original.ClientID, time.Now()).Take(&row).Error; err != nil {
			return err
		}
		if explicit && row.Approved {
			return fiber.ErrBadRequest
		}
		var err error
		request, err = oidcRequestFromRow(row)
		if err != nil {
			return err
		}
		decision, err = evaluateOIDCConsent(tx, request, oidcBrowser(c), explicit, time.Now())
		if err != nil || decision != oidcConsentReady || row.Approved {
			return err
		}
		return tx.Model(&row).Updates(map[string]interface{}{"approved": true, "account_id": request.AccountID, "auth_time": request.AuthTime}).Error
	})
	return request, decision, err
}

func oidcConsentResponse(c *fiber.Ctx, request *oidcAuthRequest, decision oidcConsentDecision) error {
	if slices.Contains(request.req.Prompt, oidc.PromptNone) {
		return oidcHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			op.AuthRequestError(w, r, &request.req, decision.protocolError(), activeOIDCProvider)
		}))(c)
	}
	return c.Redirect("/login?return_to="+url.QueryEscape("/oidc/consent?authRequestID="+request.ID), http.StatusFound)
}

func oidcBrowserContext(c *fiber.Ctx) context.Context {
	return context.WithValue(c.UserContext(), oidcBrowserKey{}, oidcBrowser(c))
}
