package router

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// Consent, issuance, revocation, disable and deletion take this lock first.
// A cached client/request cannot issue credentials after its revocation wins.
func lockOIDCClient(tx *gorm.DB, id string, activeOnly bool) (*model.OIDCClient, error) {
	var client model.OIDCClient
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id)
	if activeOnly {
		query = query.Where("disabled_at IS NULL")
	}
	if err := query.Take(&client).Error; err != nil {
		return nil, err
	}
	return &client, nil
}

func deleteOIDCCredentials(tx *gorm.DB, clientID, accountID string) error {
	for _, table := range []interface{}{&model.OIDCAuthRequest{}, &model.OIDCRefreshToken{}, &model.OIDCAccessToken{}} {
		query := tx.Where("client_id = ?", clientID)
		if accountID != "" {
			query = query.Where("account_id = ?", accountID)
		}
		if err := query.Delete(table).Error; err != nil {
			return err
		}
	}
	return nil
}

// Current access is derived from credentials, never a second consent ledger.
// Filter each branch by account before aggregating; existing account indexes
// bound the work to this user's credentials rather than all applications.
func activeOIDCCredentials(db *gorm.DB, accountID string, now time.Time) *gorm.DB {
	return db.Raw(`
SELECT client_id, scopes, expires_at FROM o_id_c_access_tokens WHERE account_id = ? AND expires_at > ?
UNION ALL
SELECT client_id, scopes, expires_at FROM o_id_c_refresh_tokens WHERE account_id = ? AND expires_at > ?
UNION ALL
SELECT client_id, convert_from(request_json, 'UTF8')::jsonb->>'scope' AS scopes, expires_at
FROM o_id_c_auth_requests WHERE account_id = ? AND approved = true AND code_used_at IS NULL AND expires_at > ?`,
		accountID, now, accountID, now, accountID, now)
}

type oidcAuthorization struct {
	ClientID    string
	Name        string
	Description string
	HomepageURL string
	OwnerID     string
	OwnerName   string
	Scopes      string
	ScopeList   []string `gorm:"-"`
	ExpiresAt   time.Time
}

func authorizedApplicationsQuery(db *gorm.DB, accountID string, now time.Time) *gorm.DB {
	summary := db.Table("(?) AS credentials", activeOIDCCredentials(db, accountID, now)).
		Select("client_id, string_agg(DISTINCT scopes, ' ') AS scopes, max(expires_at) AS expires_at").Group("client_id")
	return db.Table("(?) AS active", summary).
		Select("active.client_id, active.scopes, active.expires_at, c.name, c.description, c.homepage_url, c.owner_id, a.nickname AS owner_name").
		Joins("JOIN o_id_c_clients c ON c.id = active.client_id AND c.disabled_at IS NULL").
		Joins("LEFT JOIN accounts a ON a.id = c.owner_id").Order("c.name ASC, c.id ASC")
}

func authorizedApplicationsPage(c *fiber.Ctx) error {
	page, err := listPage(c.Query("page"))
	if err != nil {
		return err
	}
	var applications []oidcAuthorization
	err = authorizedApplicationsQuery(solitudes.System.DB.WithContext(c.UserContext()), currentAccount(c).ID, time.Now()).
		Limit(21).Offset((page - 1) * 20).Scan(&applications).Error
	if err != nil {
		return err
	}
	more := len(applications) > 20
	if more {
		applications = applications[:20]
	}
	for i := range applications {
		unique := make(map[string]bool)
		for _, scope := range strings.Fields(applications[i].Scopes) {
			if !unique[scope] {
				applications[i].ScopeList = append(applications[i].ScopeList, scope)
				unique[scope] = true
			}
		}
		sort.Strings(applications[i].ScopeList)
		if !validClientHomepage(applications[i].HomepageURL) {
			applications[i].HomepageURL = ""
		}
	}
	c.Set(fiber.HeaderCacheControl, "private, no-store")
	return c.Render("site/account_authorizations", injectSiteData(c, fiber.Map{
		"title": "Authorized applications", "noindex": true, "authorizations": applications,
		"revoked": c.Query("revoked") == "1", "navigation": pageNavigationFor(c, "page", page, more, "authorizations"),
	}))
}

func revokeApplicationGrant(c *fiber.Ctx) error {
	id := c.Params("id")
	account := currentAccount(c)
	if account == nil {
		return fiber.ErrUnauthorized
	}
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockOIDCClient(tx, id, false); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		// Include expired/consumed requests in this ownership check: a stale
		// page or an in-flight code exchange must still be safely revocable.
		var ownsCredentials bool
		if err := tx.Raw(`SELECT EXISTS (
SELECT 1 FROM o_id_c_access_tokens WHERE client_id = ? AND account_id = ?
UNION ALL SELECT 1 FROM o_id_c_refresh_tokens WHERE client_id = ? AND account_id = ?
UNION ALL SELECT 1 FROM o_id_c_auth_requests WHERE client_id = ? AND account_id = ?
)`, id, account.ID, id, account.ID, id, account.ID).Scan(&ownsCredentials).Error; err != nil {
			return err
		}
		if !ownsCredentials {
			return fiber.ErrNotFound
		}
		if err := deleteOIDCCredentials(tx, id, account.ID); err != nil {
			return err
		}
		return auditMutation(c, tx, "grant.revoke", id, id, "")
	})
	if err != nil {
		return err
	}
	c.Locals("audit_recorded", true)
	return c.Redirect("/account/oidc/authorizations?revoked=1", http.StatusSeeOther)
}

func deleteOIDCClient(c *fiber.Ctx) error {
	id := c.Params("id")
	account := currentAccount(c)
	if account == nil {
		return fiber.ErrUnauthorized
	}
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		client, err := lockOIDCClient(tx, id, false)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		if !account.Role.IsAdmin() && (client.OwnerID == nil || *client.OwnerID != account.ID) {
			return fiber.ErrNotFound
		}
		if err := deleteOIDCCredentials(tx, id, ""); err != nil {
			return err
		}
		if err := tx.Delete(client).Error; err != nil {
			return err
		}
		// Audit events and compacted login summaries intentionally survive deletion.
		return auditMutation(c, tx, "client.delete", id, id, "")
	})
	if err != nil {
		return err
	}
	c.Locals("audit_recorded", true)
	if strings.HasPrefix(c.Path(), "/account/") {
		return c.Redirect("/account/oidc/clients", http.StatusSeeOther)
	}
	return c.Redirect("/admin/oidc/clients", http.StatusSeeOther)
}

func lockOIDCTokenRequest(ctx context.Context, tx *gorm.DB, request interface{ GetSubject() string }, clientID string) error {
	if _, err := lockOIDCClient(tx.WithContext(ctx), clientID, true); err != nil {
		return err
	}
	if auth, ok := request.(*oidcAuthRequest); ok {
		var row model.OIDCAuthRequest
		return tx.Where("id = ? AND client_id = ? AND account_id = ? AND approved = true AND expires_at > ?", auth.ID, clientID, request.GetSubject(), time.Now()).Take(&row).Error
	}
	return nil
}
