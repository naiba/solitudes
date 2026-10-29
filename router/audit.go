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

// Group authorization can reject a request before chi matches its leaf
// route. Recover only known route shapes, never the raw URL or query string.
func auditResource(path string) (route, target, client string) {
	for _, prefix := range []string{"/admin/users/", "/admin/oidc/clients/", "/account/oidc/clients/", "/account/oidc/authorizations/", "/account/passkeys/"} {
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
			case "role", "metadata", "disable", "delete", "revoke":
				route += "/" + parts[1]
			default:
				return "", "", ""
			}
		}
		target = auditIdentifier(parts[0])
		if strings.Contains(prefix, "/oidc/") {
			client = target
		}
		return route, target, client
	}
	switch path {
	case "/admin/users", "/admin/oidc/clients", "/admin/settings", "/admin/auth/providers", "/admin/publish", "/admin/articles", "/admin/audit", "/admin/redirects":
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
		if path == "/userinfo" {
			return "oidc.request"
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
	case "/admin/redirects":
		if method == http.MethodDelete {
			return "redirect.delete"
		}
		return "redirect.save"
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
	case "/userinfo":
		return "oidc.request"
	}
	if strings.Contains(path, "/oidc/clients") {
		if strings.HasSuffix(path, "/delete") {
			return "client.delete"
		}
		if strings.HasSuffix(path, "/disable") {
			return "client.disable"
		}
		if strings.HasSuffix(path, "/metadata") {
			return "client.update"
		}
		return "client.create"
	}
	if strings.HasPrefix(path, "/account/oidc/authorizations/") && strings.HasSuffix(path, "/revoke") {
		return "grant.revoke"
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

// Successful protocol plumbing is not a security event. Token issuance has
// its own transactional oidc.login event; refresh/introspection and challenges
// are routine. Failures (including OAuth errors conveyed by redirects) must
// bypass this filter. Consent, revocation and completed mutations are not here.
func routineAuditSuccess(method, path string, status int) bool {
	if status < http.StatusOK || status >= http.StatusBadRequest {
		return false
	}
	if method == http.MethodGet {
		switch path {
		case "/admin/audit", "/authorize", "/authorize/callback":
			return true
		case "/userinfo":
			return status < http.StatusMultipleChoices
		}
	}
	// Completed sign-ins are recorded transactionally by issueLoginSession.
	// A successful response without that event is navigation, not a login attempt.
	if method == http.MethodPost && (path == "/login" || path == "/auth/passkey/login/finish") {
		return true
	}
	if method == http.MethodPost && status < http.StatusMultipleChoices {
		switch path {
		// Revocations audit actual deletions in storage; comments already have business records.
		case "/revoke", "/api/comment", "/oauth/token", "/oauth/introspect", "/userinfo", "/auth/passkey/login/begin", "/account/passkeys/begin":
			return true
		}
	}
	return false
}

func auditStatus(writer http.ResponseWriter, httpRequest *http.Request, err error) int {
	// A late socket write failure cannot change an already committed status;
	// auditing it as 500 would turn ordinary client disconnects into failures.
	if responseCommitted(httpRequest) || err == nil {
		return responseStatus(httpRequest)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound
	}
	var fe *httpError
	if errors.As(err, &fe) {
		return fe.Code
	}
	return http.StatusInternalServerError
}

func auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if err := auditRequestOutcome(writer, httpRequest, next); err != nil {
			respondError(writer, httpRequest, err)
		}
	})
}

func auditRequestOutcome(writer http.ResponseWriter, httpRequest *http.Request, next http.Handler) error {
	id, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	ip := ""
	if parsed := net.ParseIP(requestIP(httpRequest)); parsed != nil {
		ip = parsed.String()
	}
	writer.Header().Set("X-Request-ID", id) // Never trust a client-supplied correlation ID.
	setRequestContext(httpRequest, context.WithValue(httpRequest.Context(), auditContextKey{}, &auditRequest{ID: id, IP: ip}))
	next.ServeHTTP(writer, httpRequest)
	err = stateFor(httpRequest).err
	status := auditStatus(writer, httpRequest, err)
	reason, _ := requestValue(httpRequest, "audit_reason").(string)
	// Only the protocol adapter may identify expected interaction responses.
	// Never let that marker hide middleware errors or explicit denials.
	if requestValue(httpRequest, "audit_interaction_required") == true && err == nil && reason == "" &&
		requestValue(httpRequest, "audit_outcome") != "failure" && requestValue(httpRequest, "audit_outcome") != "denied" &&
		(status == http.StatusBadRequest || (status >= 300 && status < 400)) {
		return nil
	}
	failed := err != nil || status >= 400 || reason != "" || requestValue(httpRequest, "audit_outcome") == "failure" || requestValue(httpRequest, "audit_outcome") == "denied"
	if !failed && (requestValue(httpRequest, "audit_no_change") == true || routineAuditSuccess(httpRequest.Method, requestPath(httpRequest), status)) {
		return nil
	}
	location := writer.Header().Get("Location")
	protectedPage := requestPath(httpRequest) == "/admin" || strings.HasPrefix(requestPath(httpRequest), "/admin/") || requestPath(httpRequest) == "/account" || strings.HasPrefix(requestPath(httpRequest), "/account/")
	authenticationRedirect := currentAccount(writer, httpRequest) == nil && status >= 300 && status < 400 &&
		(location == "/login" || strings.HasPrefix(location, "/login?")) && protectedPage
	// Browsing with no usable session (including expired cookies) is not a
	// login attempt. Keep auditing mutations, permission failures and errors.
	if !failed && authenticationRedirect && (httpRequest.Method == http.MethodGet || httpRequest.Method == http.MethodHead) {
		return nil
	}
	action := auditAction(httpRequest.Method, requestPath(httpRequest))
	if status >= 500 {
		action = "request.error"
	}
	// Missing pages are not permission denials. Explicit operations still retain
	// their failure event when a target is missing (for example client deletion).
	if action == "" && status >= 400 && status != http.StatusNotFound &&
		(protectedPage || requestPath(httpRequest) == "/authorize" || strings.HasPrefix(requestPath(httpRequest), "/authorize/")) {
		action = "access.denied"
	}
	if action == "" && authenticationRedirect {
		action = "access.denied"
	}
	if action == "" || (!failed && requestValue(httpRequest, "audit_recorded") == true) {
		return err
	}
	event := model.AuditEvent{Action: action, Outcome: "success", Method: httpRequest.Method, Status: status}
	event.TargetID = auditIdentifier(routeParam(httpRequest, "id"))
	if strings.HasPrefix(action, "client.") {
		event.ClientID = event.TargetID
	}
	if len(event.Method) > 16 {
		event.Method = "OTHER"
	}
	if route := requestRoute(httpRequest); route != nil {
		event.Route = route.Path
	}
	if route, target, client := auditResource(requestPath(httpRequest)); route != "" {
		event.Route, event.TargetID = route, target
		if client != "" {
			event.ClientID = client
		}
	}
	switch requestPath(httpRequest) {
	case "/oauth/token", "/oauth/introspect", "/revoke", "/userinfo", "/authorize", "/authorize/callback", "/end_session":
		event.Route = requestPath(httpRequest)
	}
	// Protocol routes are mounted as prefixes, not as their query-bearing URLs.
	if len(event.Route) > 128 {
		event.Route = ""
	}
	if account := currentAccount(writer, httpRequest); account != nil {
		event.ActorID = account.ID
	}
	if target, ok := requestValue(httpRequest, "audit_target").(string); ok {
		event.TargetID = target
	}
	if client, ok := requestValue(httpRequest, "audit_client").(string); ok {
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
	if reason, ok := requestValue(httpRequest, "audit_reason").(string); ok {
		event.Reason = reason
	}
	if outcome, ok := requestValue(httpRequest, "audit_outcome").(string); ok {
		event.Outcome = outcome
	}
	if authenticationRedirect {
		event.Outcome, event.Reason = "denied", "authentication_required"
	}
	if event.Reason == "consent_declined" {
		event.Outcome = "denied"
	}
	// A client disconnect cancels request work, but must not erase the final
	// security event. Preserve request identity and bound the independent write.
	auditContext, cancelAudit := context.WithTimeout(context.WithoutCancel(httpRequest.Context()), 5*time.Second)
	defer cancelAudit()
	if saveErr := saveAudit(auditContext, solitudes.System.DB, event); saveErr != nil {
		// The database may be down: preserve a secret-free fallback for diagnosis.
		log.Printf("audit_write_failed request_id=%s action=%s status=%d", id, action, status)
	}
	if status >= 500 {
		log.Printf("request_error request_id=%s route=%s status=%d", id, event.Route, status)
	}
	return err
}

func auditMutation(writer http.ResponseWriter, httpRequest *http.Request, tx *gorm.DB, action, targetID, clientID, details string) error {
	event := model.AuditEvent{Action: action, Outcome: "success", TargetID: targetID, ClientID: clientID,
		Method: httpRequest.Method, Route: requestRoute(httpRequest).Path, Details: details, Status: http.StatusOK}
	if account := currentAccount(writer, httpRequest); account != nil {
		event.ActorID = account.ID
	}
	return saveAudit(httpRequest.Context(), tx, event)
}
