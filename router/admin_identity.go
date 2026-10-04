package router

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/pagination"
)

const identityPageSize = 25

type adminUserView struct {
	ID, Nickname, Email                      string
	Role                                     model.Role
	CreatedAt                                time.Time
	EmailVerifiedAt, DisabledAt, LastLoginAt *time.Time
	AppCount, LoginCount                     int64
}

type adminClientView struct {
	model.OIDCClient
	OwnerName, OwnerEmail    string
	RedirectURIs, LogoutURIs string `gorm:"-"`
	LoginCount, LoginUsers   int64
	LastLoginAt              *time.Time
}

type identityTotals struct {
	Users, Clients, ActiveClients, Logins int64
	StartedAt                             *time.Time
}

func (c *adminClientView) prepare() {
	if !validClientHomepage(c.HomepageURL) {
		c.HomepageURL = ""
	}
	var redirects, logout []string
	_ = json.Unmarshal([]byte(c.RedirectURIsJSON), &redirects)
	_ = json.Unmarshal([]byte(c.PostLogoutURIsJSON), &logout)
	c.RedirectURIs, c.LogoutURIs = strings.Join(redirects, "\n"), strings.Join(logout, "\n")
}

func identityOverview(db *gorm.DB) (identityTotals, error) {
	var totals identityTotals
	var start model.AuditEvent
	if err := db.Where("id = ?", "audit-initialized").Limit(1).Find(&start).Error; err != nil {
		return totals, err
	}
	if !start.CreatedAt.IsZero() {
		totals.StartedAt = &start.CreatedAt
	}
	if err := db.Model(&model.Account{}).Count(&totals.Users).Error; err != nil {
		return totals, err
	}
	if err := db.Model(&model.OIDCClient{}).Count(&totals.Clients).Error; err != nil {
		return totals, err
	}
	if err := db.Model(&model.OIDCClient{}).Where("disabled_at IS NULL").Count(&totals.ActiveClients).Error; err != nil {
		return totals, err
	}
	err := db.Table(model.LoginFactsSQL+" AS logins").Select("COALESCE(SUM(login_count), 0)").Where("action = ?", "oidc.login").Scan(&totals.Logins).Error
	return totals, err
}

// Only dashboard totals are briefly cached. Account status, authorization,
// application details and audit records always come from the database.
func cachedIdentityOverview() (identityTotals, error) {
	system := solitudes.System
	if system.Cache == nil {
		return identityOverview(system.DB)
	}
	key := fmt.Sprintf("identity-overview:%p", system.DB)
	if value, ok := system.Cache.Get(key); ok {
		return value.(identityTotals), nil
	}
	load := func() (interface{}, error) {
		if value, ok := system.Cache.Get(key); ok {
			return value, nil
		}
		totals, err := identityOverview(system.DB)
		if err == nil {
			system.Cache.Set(key, totals, 30*time.Second)
		}
		return totals, err
	}
	var value interface{}
	var err error
	if system.SafeCache != nil {
		value, err, _ = system.SafeCache.Do(key, load)
	} else {
		value, err = load()
	}
	if err != nil {
		return identityTotals{}, err
	}
	return value.(identityTotals), nil
}

func adminUsersQuery(db *gorm.DB) *gorm.DB {
	// Used for individual lookups and projections over a bounded page subquery.
	facts := model.LoginFactsSQL + " AS logins WHERE action = 'session.login' AND actor_id = a.id::text"
	return db.Table("accounts AS a").Select(`a.id, a.nickname, a.email, a.role, a.created_at, a.email_verified_at, a.disabled_at,
	 (SELECT COUNT(*) FROM o_id_c_clients WHERE owner_id = a.id) AS app_count,
	 (SELECT COALESCE(SUM(login_count), 0) FROM ` + facts + `) AS login_count,
	 (SELECT MAX(last_login_at) FROM ` + facts + `) AS last_login_at`)
}

func adminClientsQuery(db *gorm.DB) *gorm.DB {
	facts := model.LoginFactsSQL + " AS logins WHERE action = 'oidc.login' AND client_id = c.id"
	return db.Table("o_id_c_clients AS c").Select(`c.*, COALESCE(a.nickname, '') AS owner_name, COALESCE(a.email, '') AS owner_email,
	 (SELECT COALESCE(SUM(login_count), 0) FROM ` + facts + `) AS login_count,
	 (SELECT COUNT(DISTINCT actor_id) FROM ` + facts + `) AS login_users,
	 (SELECT MAX(last_login_at) FROM ` + facts + `) AS last_login_at`).
		Joins("LEFT JOIN accounts AS a ON a.id = c.owner_id")
}

func adminUsersPageQuery(base *gorm.DB, page int) *gorm.DB {
	selected := base.Session(&gorm.Session{}).Select("a.*").Order("a.created_at DESC, a.id DESC").Limit(identityPageSize).Offset((page - 1) * identityPageSize)
	return adminUsersQuery(base.Session(&gorm.Session{NewDB: true})).Table("(?) AS a", selected).Order("a.created_at DESC, a.id DESC")
}

func adminClientsPageQuery(base *gorm.DB, page int) *gorm.DB {
	selected := base.Session(&gorm.Session{}).Select("c.*").Order("c.created_at DESC, c.id").Limit(identityPageSize).Offset((page - 1) * identityPageSize)
	return adminClientsQuery(base.Session(&gorm.Session{NewDB: true})).Table("(?) AS c", selected).Order("c.created_at DESC, c.id")
}

func identityPage(c *fiber.Ctx) (int, error) {
	return listPage(c.Query("page"))
}

func identitySearch(c *fiber.Ctx) (string, error) {
	q := strings.TrimSpace(c.Query("q"))
	if len(q) > 100 || strings.ContainsRune(q, '\x00') {
		return "", fiber.ErrBadRequest
	}
	return q, nil
}

func paginationLinks(c *fiber.Ctx, page int, total int64) fiber.Map {
	link := func(p int) string {
		values, _ := url.ParseQuery(string(c.Request().URI().QueryString()))
		values.Set("page", strconv.Itoa(p))
		return c.Path() + "?" + values.Encode()
	}
	data := fiber.Map{"page": page, "total": total}
	if page > 1 {
		data["previous"] = link(page - 1)
	}
	if page < pagination.MaxPage && int64(page*identityPageSize) < total {
		data["next"] = link(page + 1)
	}
	return data
}

func adminIdentityRender(c *fiber.Ctx, template string, data fiber.Map) error {
	c.Set("Cache-Control", "private, no-store")
	data["noindex"] = true
	return c.Status(http.StatusOK).Render(template, injectSiteData(c, data))
}

func adminUsersPage(c *fiber.Ctx) error {
	page, err := identityPage(c)
	if err != nil {
		return err
	}
	q, err := identitySearch(c)
	if err != nil {
		return err
	}
	query := solitudes.System.DB.Table("accounts AS a")
	if q != "" {
		query = query.Where("LOWER(a.nickname) LIKE ? OR LOWER(a.email) LIKE ?", "%"+strings.ToLower(q)+"%", "%"+strings.ToLower(q)+"%")
	}
	role := c.Query("role")
	if role != "" {
		if role != "admin" && role != "editor" && role != "user" {
			return fiber.ErrBadRequest
		}
		query = query.Where("a.role = ?", role)
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return err
	}
	var users []adminUserView
	if err := adminUsersPageQuery(query, page).Scan(&users).Error; err != nil {
		return err
	}
	overview, err := cachedIdentityOverview()
	if err != nil {
		return err
	}
	data := paginationLinks(c, page, total)
	data["users"], data["overview"], data["q"], data["role"] = users, overview, q, role
	data["title"] = "Users"
	return adminIdentityRender(c, "admin/users", data)
}

func adminUserDetail(c *fiber.Ctx) error {
	var user adminUserView
	query := adminUsersQuery(solitudes.System.DB).Where("CAST(a.id AS TEXT) = ?", c.Params("id"))
	if err := query.Take(&user).Error; err != nil {
		return err
	}
	var clients []adminClientView
	if err := adminClientsQuery(solitudes.System.DB).Where("c.owner_id = ?", user.ID).Order("c.created_at DESC, c.id").Limit(identityPageSize).Scan(&clients).Error; err != nil {
		return err
	}
	return adminIdentityRender(c, "admin/user_detail", fiber.Map{"title": "User details", "user": user, "clients": clients})
}

func adminClientsPage(c *fiber.Ctx) error {
	page, err := identityPage(c)
	if err != nil {
		return err
	}
	q, err := identitySearch(c)
	if err != nil {
		return err
	}
	query := solitudes.System.DB.Table("o_id_c_clients AS c")
	owner := c.Query("owner_id")
	if len(owner) > 64 {
		return fiber.ErrBadRequest
	}
	if owner != "" {
		query = query.Where("CAST(c.owner_id AS TEXT) = ?", owner)
	}
	if q != "" {
		query = query.Where("LOWER(c.name) LIKE ? OR c.id = ?", "%"+strings.ToLower(q)+"%", q)
	}
	status := c.Query("status")
	switch status {
	case "":
	case "active":
		query = query.Where("c.disabled_at IS NULL")
	case "disabled":
		query = query.Where("c.disabled_at IS NOT NULL")
	default:
		return fiber.ErrBadRequest
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return err
	}
	var clients []adminClientView
	if err := adminClientsPageQuery(query, page).Scan(&clients).Error; err != nil {
		return err
	}
	overview, err := cachedIdentityOverview()
	if err != nil {
		return err
	}
	data := paginationLinks(c, page, total)
	data["clients"], data["overview"], data["q"], data["owner_id"], data["status"] = clients, overview, q, owner, status
	for i := range clients {
		clients[i].prepare()
	}
	data["title"], data["issuer"] = "OIDC clients", publicBaseURL()
	return adminIdentityRender(c, "admin/oidc_clients", data)
}

func adminClientDetail(c *fiber.Ctx) error {
	page, err := listPage(c.Query("page"))
	if err != nil {
		return err
	}
	var client adminClientView
	if err := adminClientsQuery(solitudes.System.DB).Where("c.id = ?", c.Params("id")).Take(&client).Error; err != nil {
		return err
	}
	client.prepare()
	type loginUser struct {
		ID, Nickname string
		LoginCount   int64
		LastLoginAt  time.Time
	}
	var users []loginUser
	if err := solitudes.System.DB.Table(model.LoginFactsSQL+" AS e").
		Select("e.actor_id AS id, COALESCE(a.nickname, '') AS nickname, SUM(e.login_count) AS login_count, MAX(e.last_login_at) AS last_login_at").
		Joins("LEFT JOIN accounts AS a ON CAST(a.id AS TEXT) = e.actor_id").
		Where("e.action = ? AND e.client_id = ?", "oidc.login", client.ID).
		Group("e.actor_id, a.nickname").Order("MAX(e.last_login_at) DESC, e.actor_id").Limit(identityPageSize + 1).Offset((page - 1) * identityPageSize).Scan(&users).Error; err != nil {
		return err
	}
	more := len(users) > identityPageSize
	if more {
		users = users[:identityPageSize]
	}
	return adminIdentityRender(c, "admin/client_detail", fiber.Map{"title": "Application details", "client": client, "login_users": users, "navigation": pageNavigationFor(c, "page", page, more, "login-users")})
}

// Shared with the translation contract: audit actions are translated dynamically.
var auditActionNames = []string{"oidc.login", "session.login", "session.attempt", "session.logout", "account.register", "account.verify", "account.verification.send", "identity.callback", "oidc.authorize", "oidc.consent", "oidc.logout", "oidc.request", "client.create", "client.update", "client.disable", "client.delete", "grant.revoke", "user.role.update", "account.password.update", "account.profile.update", "account.operation", "passkey.operation", "provider.update", "settings.update", "oidc.keys.rotate", "article.save", "article.delete", "comment.create", "admin.operation", "access.denied", "request.error", "audit.view", "audit.enabled", "account.initialized"}

func auditPage(c *fiber.Ctx) error {
	page, err := identityPage(c)
	if err != nil {
		return err
	}
	query := solitudes.System.DB.Model(&model.AuditEvent{})
	data := fiber.Map{"title": "Security audit", "retention_days": solitudes.System.Config.AuditRetentionDays}
	data["actions"] = auditActionNames
	ip := c.Query("ip")
	if ip != "" {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			return fiber.ErrBadRequest
		}
		ip = parsed.String()
		query = query.Where("ip = ?", ip)
	}
	data["ip"] = ip
	for key, column := range map[string]string{"action": "action", "outcome": "outcome", "client_id": "client_id", "request_id": "request_id"} {
		value := strings.TrimSpace(c.Query(key))
		if len(value) > 64 {
			return fiber.ErrBadRequest
		}
		if value != "" {
			query = query.Where(column+" = ?", value)
		}
		data[key] = value
	}
	userID := c.Query("user_id")
	if len(userID) > 64 {
		return fiber.ErrBadRequest
	}
	if userID != "" {
		query = query.Where("actor_id = ? OR target_id = ?", userID, userID)
	}
	data["user_id"] = userID
	for _, key := range []string{"since", "until"} {
		value := c.Query(key)
		if value != "" {
			date, err := time.Parse("2006-01-02", value)
			if err != nil {
				return fiber.ErrBadRequest
			}
			if key == "since" {
				query = query.Where("created_at >= ?", date)
			} else {
				query = query.Where("created_at < ?", date.AddDate(0, 0, 1))
			}
		}
		data[key] = value
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return err
	}
	var events []model.AuditEvent
	if err := query.Order("created_at DESC, id DESC").Limit(identityPageSize).Offset((page - 1) * identityPageSize).Find(&events).Error; err != nil {
		return err
	}
	for k, v := range paginationLinks(c, page, total) {
		data[k] = v
	}
	data["events"] = events
	var actorIDs []string
	for _, event := range events {
		if event.ActorID != "" {
			actorIDs = append(actorIDs, event.ActorID)
		}
	}
	names := map[string]string{}
	if len(actorIDs) > 0 {
		var accounts []struct{ ID, Nickname string }
		if err := solitudes.System.DB.Model(&model.Account{}).Select("id, nickname").Where("CAST(id AS TEXT) IN ?", actorIDs).Scan(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			names[account.ID] = account.Nickname
		}
	}
	data["actor_names"] = names
	return adminIdentityRender(c, "admin/audit", data)
}
