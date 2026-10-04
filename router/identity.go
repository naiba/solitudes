package router

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

var errNoAccount = errors.New("account not found")

func newSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func secretHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func findLoginSession(token string) (*model.Account, error) {
	if solitudes.System == nil || solitudes.System.DB == nil || token == "" {
		return nil, errNoAccount
	}
	var session model.LoginSession
	if err := solitudes.System.DB.Where("token_hash = ? AND expires_at > ?", secretHash(token), time.Now()).Take(&session).Error; err != nil {
		return nil, err
	}
	var account model.Account
	if err := solitudes.System.DB.Where("id = ? AND email_verified_at IS NOT NULL AND disabled_at IS NULL", session.AccountID).Take(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// sessionLookup is replaceable in handler tests. Authentication requires a
// live database session and fails closed when storage is unavailable.
var sessionLookup = findLoginSession

func currentAccount(c *fiber.Ctx) *model.Account {
	account, _ := c.Locals(solitudes.CtxAccount).(*model.Account)
	return account
}

func issueLoginSession(c *fiber.Ctx, account *model.Account, remember bool) error {
	token, err := newSecret()
	if err != nil {
		return err
	}
	id, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	expires := time.Now().Add(4 * time.Hour)
	if remember {
		expires = time.Now().AddDate(0, 3, 0)
	}
	if err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model.LoginSession{ID: id, AccountID: account.ID, TokenHash: secretHash(token), ExpiresAt: expires}).Error; err != nil {
			return err
		}
		reason := "password"
		if strings.Contains(c.Path(), "/passkey/") {
			reason = "passkey"
		} else if strings.HasPrefix(c.Path(), "/auth/") {
			reason = "oauth"
		}
		return saveAudit(c.UserContext(), tx, model.AuditEvent{Action: "session.login", Outcome: "success", ActorID: account.ID,
			Method: c.Method(), Route: c.Route().Path, Status: http.StatusOK, Reason: reason})
	}); err != nil {
		return fmt.Errorf("create login session: %w", err)
	}
	c.Locals("audit_recorded", true)
	c.Cookie(&fiber.Cookie{
		Name: solitudes.AuthCookie, Value: token, Path: "/", Expires: expires,
		HTTPOnly: true, SameSite: fiber.CookieSameSiteLaxMode, Secure: c.Protocol() == "https",
	})
	return nil
}

func verifyAccountEmail(raw string) error {
	if raw == "" {
		return errNoAccount
	}
	return solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		var action model.EmailAction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?", secretHash(raw), "verify_email", time.Now()).
			Take(&action).Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&action).Update("used_at", now).Error; err != nil {
			return err
		}
		return tx.Model(&model.Account{}).Where("id = ? AND disabled_at IS NULL", action.AccountID).
			Update("email_verified_at", now).Error
	})
}

func safeReturnPath(raw string) string {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.ContainsAny(raw, "\\\r\n") {
		return raw
	}
	return ""
}

func publicBaseURL() string {
	host := solitudes.System.Config.Site.Domain
	if parsed, err := url.Parse("//" + host); err == nil {
		if parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" {
			return "http://" + host
		}
	}
	return "https://" + host
}

func validSiteDomain() bool {
	host := strings.TrimSpace(solitudes.System.Config.Site.Domain)
	if host == "" || strings.ContainsAny(host, "/\\?#@ \t\r\n") {
		return false
	}
	u, err := url.Parse("//" + host)
	return err == nil && u.Host == host && u.Hostname() != "" && u.User == nil
}

func requireAdmin(c *fiber.Ctx) error {
	if account := currentAccount(c); account == nil || !account.Role.IsAdmin() {
		return fiber.NewError(http.StatusForbidden, "administrator required")
	}
	return c.Next()
}

func requireAccount(c *fiber.Ctx) error {
	if currentAccount(c) == nil {
		if c.Method() == fiber.MethodGet {
			return c.Redirect("/login?return_to="+url.QueryEscape(c.OriginalURL()), http.StatusFound)
		}
		return c.Redirect("/login", http.StatusFound)
	}
	return c.Next()
}
