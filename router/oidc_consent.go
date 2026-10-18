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

// Token expiry or single-token revocation is not withdrawal of consent. Only
// the durable, explicitly approved grant permits reuse; credentials and old
// authorization requests must never resurrect an application-level revocation.
func oidcScopesGranted(db *gorm.DB, clientID, accountID string, scopes []string) (bool, error) {
	var consent model.OIDCConsent
	err := db.Where("client_id = ? AND account_id = ?", clientID, accountID).Take(&consent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	granted := map[string]bool{}
	for _, scope := range strings.Fields(consent.Scopes) {
		granted[scope] = true
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
	// Bind an authenticated, fresh pending request before asking for consent.
	// Otherwise account-scoped revocation misses its NULL owner and an old
	// consent POST can recreate the grant after revocation wins the client lock.
	// Do not bind before freshness checks: login/select_account may choose a
	// different account, while an existing subject hint still constrains it.
	request.AccountID = &session.AccountID
	if !explicit {
		if slices.Contains(request.req.Prompt, oidc.PromptConsent) {
			return oidcNeedsConsent, nil
		}
		granted, err := oidcScopesGranted(db, request.ClientID, session.AccountID, request.GetScopes())
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
		if err != nil {
			return err
		}
		if decision == oidcNeedsConsent && row.AccountID == nil && request.AccountID != nil {
			// Requests created as guests acquire ownership on their authenticated
			// return to the consent page, under the same revocation/client lock.
			return tx.Model(&row).Update("account_id", request.AccountID).Error
		}
		if decision != oidcConsentReady || row.Approved {
			return nil
		}
		if explicit {
			// The client lock also serializes scope merging with application
			// revocation. Never write consent from a silent authorization, and keep
			// request scopes unchanged so tokens cannot inherit historical scopes.
			var consent model.OIDCConsent
			err := tx.Where("client_id = ? AND account_id = ?", request.ClientID, *request.AccountID).Take(&consent).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			scopes := append(strings.Fields(consent.Scopes), request.GetScopes()...)
			slices.Sort(scopes)
			consent.AccountID, consent.ClientID = *request.AccountID, request.ClientID
			consent.Scopes = strings.Join(slices.Compact(scopes), " ")
			consent.UpdatedAt = time.Now()
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "account_id"}, {Name: "client_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"scopes", "updated_at"}),
			}).Create(&consent).Error; err != nil {
				return err
			}
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
