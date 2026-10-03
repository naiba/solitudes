package router

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-uuid"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

type auditContextKey struct{}
type auditRequest struct{ ID, IP, ClientID string }

func auditIdentifier(value string) string {
	if len(value) > 64 {
		return ""
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return ""
		}
	}
	return value
}

// Group authorization can reject a request before Fiber matches its leaf
// route. Recover only known route shapes, never the raw URL or query string.
func auditResource(path string) (route, target, client string) {
	for _, prefix := range []string{"/admin/users/", "/admin/oidc/clients/", "/account/oidc/clients/", "/account/passkeys/"} {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
		if len(parts) > 2 || parts[0] == "" {
			return "", "", ""
		}
		route = prefix + ":id"
		if len(parts) == 2 {
			switch parts[1] {
			case "role", "metadata", "disable":
				route += "/" + parts[1]
			default:
				return "", "", ""
			}
		}
		target = auditIdentifier(parts[0])
		if strings.Contains(prefix, "/oidc/clients/") {
			client = target
		}
		return route, target, client
	}
	switch path {
	case "/admin/users", "/admin/oidc/clients", "/admin/settings", "/admin/auth/providers", "/admin/publish", "/admin/articles", "/admin/audit":
		return path, "", ""
	}
	return "", "", ""
}

func saveAudit(ctx context.Context, db *gorm.DB, event model.AuditEvent) error {
	if db == nil {
		return errors.New("audit database unavailable")
	}
	id, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	event.ID = id
	event.CreatedAt = time.Now().UTC()
	if request, ok := ctx.Value(auditContextKey{}).(*auditRequest); ok {
		event.RequestID, event.IP = request.ID, request.IP
		if event.ClientID == "" {
			event.ClientID = request.ClientID
		}
	}
	event.ActorID, event.ClientID, event.TargetID = auditIdentifier(event.ActorID), auditIdentifier(event.ClientID), auditIdentifier(event.TargetID)
	return db.WithContext(ctx).Create(&event).Error
}

// Only server-defined action/reason strings and identifiers reach the ledger.
// Do not add raw URL/query, request bodies, Authorization, cookies, UA or errors.
func auditAction(method, path string) string {
	if method == http.MethodGet {
		if path == "/authorize" || path == "/authorize/callback" {
			return "oidc.authorize"
		}
		if path == "/end_session" {
			return "oidc.logout"
		}
		if path == "/admin/audit" {
			return "audit.view"
		}
		if strings.HasPrefix(path, "/auth/") && strings.HasSuffix(path, "/callback") {
			return "identity.callback"
		}
		if path == "/verify-email" {
			return "account.verify"
		}
		return ""
	}
	if method == http.MethodHead || method == http.MethodOptions {
		return ""
	}
	switch path {
	case "/login":
		return "session.login"
	case "/logout":
		return "session.logout"
	case "/register":
		return "account.register"
	case "/resend-verification":
		return "account.verification.send"
	case "/oidc/consent":
		return "oidc.consent"
	case "/admin/auth/providers":
		return "provider.update"
	case "/admin/settings":
		return "settings.update"
	case "/admin/oidc/keys/rotate":
		return "oidc.keys.rotate"
	case "/admin/publish":
		return "article.save"
	case "/admin/articles":
		return "article.delete"
	case "/api/comment":
		return "comment.create"
	case "/account/password":
		return "account.password.update"
	case "/account/profile":
		return "account.profile.update"
	case "/auth/passkey/login/finish":
		return "session.login"
	}
	if strings.Contains(path, "/oidc/clients") {
		if strings.HasSuffix(path, "/disable") {
			return "client.disable"
		}
		if strings.HasSuffix(path, "/metadata") {
			return "client.update"
		}
		return "client.create"
	}
	if strings.HasPrefix(path, "/admin/users/") && strings.HasSuffix(path, "/role") {
		return "user.role.update"
	}
	if strings.Contains(path, "/passkey") {
		return "passkey.operation"
	}
	if strings.HasPrefix(path, "/account/") || strings.HasPrefix(path, "/auth/") {
		return "account.operation"
	}
	if strings.HasPrefix(path, "/admin/") {
		return "admin.operation"
	}
	if strings.HasPrefix(path, "/oauth/") || path == "/revoke" || path == "/end_session" {
		return "oidc.request"
	}
	return ""
}

func auditStatus(c *fiber.Ctx, err error) int {
	if err == nil {
		return c.Response().StatusCode()
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound
	}
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return fe.Code
	}
	return http.StatusInternalServerError
}

func auditMiddleware(c *fiber.Ctx) error {
	id, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	ip := ""
	if parsed := net.ParseIP(c.IP()); parsed != nil {
		ip = parsed.String()
	}
	c.Set("X-Request-ID", id) // Never trust a client-supplied correlation ID.
	c.SetUserContext(context.WithValue(c.UserContext(), auditContextKey{}, &auditRequest{ID: id, IP: ip}))
	err = c.Next()
	status := auditStatus(c, err)
	action := auditAction(c.Method(), c.Path())
	if status >= 500 {
		action = "request.error"
	}
	if action == "" && status >= 400 && (strings.HasPrefix(c.Path(), "/admin") || strings.HasPrefix(c.Path(), "/account") || strings.HasPrefix(c.Path(), "/authorize")) {
		action = "access.denied"
	}
	if action == "" && currentAccount(c) == nil && status >= 300 && status < 400 && strings.HasPrefix(c.GetRespHeader("Location"), "/login") && (strings.HasPrefix(c.Path(), "/admin") || strings.HasPrefix(c.Path(), "/account")) {
		action = "access.denied"
	}
	if action == "" || (err == nil && c.Locals("audit_recorded") == true) {
		return err
	}
	event := model.AuditEvent{Action: action, Outcome: "success", Method: c.Method(), Status: status}
	event.TargetID = auditIdentifier(c.Params("id"))
	if strings.HasPrefix(action, "client.") {
		event.ClientID = event.TargetID
	}
	if action == "session.login" && status < 400 {
		event.Action = "session.attempt"
	}
	if len(event.Method) > 16 {
		event.Method = "OTHER"
	}
	if route := c.Route(); route != nil {
		event.Route = route.Path
	}
	if route, target, client := auditResource(c.Path()); route != "" {
		event.Route, event.TargetID = route, target
		if client != "" {
			event.ClientID = client
		}
	}
	switch c.Path() {
	case "/oauth/token", "/oauth/introspect", "/revoke", "/userinfo", "/authorize", "/authorize/callback", "/end_session":
		event.Route = c.Path()
	}
	// Protocol routes are mounted as prefixes, not as their query-bearing URLs.
	if len(event.Route) > 128 {
		event.Route = ""
	}
	if account := currentAccount(c); account != nil {
		event.ActorID = account.ID
	}
	if target, ok := c.Locals("audit_target").(string); ok {
		event.TargetID = target
	}
	if client, ok := c.Locals("audit_client").(string); ok {
		event.ClientID = client
	}
	if status >= 400 {
		event.Outcome, event.Reason = "failure", "request_rejected"
		if status == 401 || status == 403 {
			event.Outcome, event.Reason = "denied", "authentication_or_permission"
		}
		if status >= 500 {
			event.Reason = "internal_error"
			if err != nil {
				event.Details = fmt.Sprintf("%T", err)
			}
			var state interface{ SQLState() string }
			if errors.As(err, &state) && len(state.SQLState()) == 5 && auditIdentifier(state.SQLState()) != "" {
				event.Reason = "database_" + state.SQLState()
			}
		}
	}
	if reason, ok := c.Locals("audit_reason").(string); ok {
		event.Reason = reason
	}
	if outcome, ok := c.Locals("audit_outcome").(string); ok {
		event.Outcome = outcome
	}
	if currentAccount(c) == nil && status >= 300 && status < 400 && (strings.HasPrefix(c.Path(), "/admin") || strings.HasPrefix(c.Path(), "/account")) {
		event.Outcome, event.Reason = "denied", "authentication_required"
	}
	if event.Reason == "consent_declined" {
		event.Outcome = "denied"
	}
	if saveErr := saveAudit(c.UserContext(), solitudes.System.DB, event); saveErr != nil {
		// The database may be down: preserve a secret-free fallback for diagnosis.
		log.Printf("audit_write_failed request_id=%s action=%s status=%d", id, action, status)
	}
	if status >= 500 {
		log.Printf("request_error request_id=%s route=%s status=%d", id, event.Route, status)
	}
	return err
}

func auditMutation(c *fiber.Ctx, tx *gorm.DB, action, targetID, clientID, details string) error {
	event := model.AuditEvent{Action: action, Outcome: "success", TargetID: targetID, ClientID: clientID,
		Method: c.Method(), Route: c.Route().Path, Details: details, Status: http.StatusOK}
	if account := currentAccount(c); account != nil {
		event.ActorID = account.ID
	}
	return saveAudit(c.UserContext(), tx, event)
}
