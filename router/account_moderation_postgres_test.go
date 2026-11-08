package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"gorm.io/gorm"
)

func TestPostgresDisabledAccountRejectsStaleRequests(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	solitudes.System = &solitudes.SysVariable{DB: db, Config: &model.Config{}}
	now := time.Now()
	account := model.Account{Email: "disabled@example.com", Nickname: "已封禁用户", Role: model.RoleUser, EmailVerifiedAt: &now, DisabledAt: &now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.OIDCClient{}); err != nil {
		t.Fatal(err)
	}
	client := model.OIDCClient{ID: "moderated-client", Name: "Moderated client", RedirectURIsJSON: "[]"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	refreshRequest := &oidcRefreshRequest{token: model.OIDCRefreshToken{AccountID: account.ID, ClientID: client.ID}}
	if err := db.Transaction(func(tx *gorm.DB) error { return lockOIDCTokenRequest(t.Context(), tx, refreshRequest, client.ID) }); err == nil {
		t.Error("disabled account authorized stale token issuance")
	}
	stale := account
	stale.DisabledAt = nil
	app := newTestRouter()
	app.Use(func(w http.ResponseWriter, r *http.Request) error {
		requestValue(r, solitudes.CtxAccount, &stale)
		return nil
	})
	app.Post("/profile", updateAccountProfile)
	app.Post("/session", func(w http.ResponseWriter, r *http.Request) error { return issueLoginSession(w, r, &stale, false) })
	for _, path := range []string{"/profile", "/session"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(url.Values{"nickname": {"forbidden rename"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		app.ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s status=%d, want 403", path, response.Code)
		}
		if len(response.Result().Cookies()) != 0 {
			t.Error("disabled user received cookie")
		}
	}
	var stored model.Account
	if err := db.Take(&stored, "id = ?", account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Nickname != "已封禁用户" {
		t.Error("stale profile changed disabled nickname")
	}
	var count int64
	if err := db.Model(&model.LoginSession{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("sessions=%d err=%v", count, err)
	}
}

func TestPostgresAdminAccountModeration(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}, &model.ExternalIdentity{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "localhost:8080"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config}
	t.Setenv("SOLITUDES_E2E", "1")
	now := time.Now()
	admin := model.Account{Email: "admin@example.com", Nickname: "Admin", Role: model.RoleAdmin, EmailVerifiedAt: &now}
	reader := model.Account{Email: "reader@example.com", Nickname: "Original", Role: model.RoleUser, EmailVerifiedAt: &now}
	for _, a := range []*model.Account{&admin, &reader} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	for token, id := range map[string]string{"admin-session": admin.ID, "reader-session": reader.ID, "reader-other-session": reader.ID} {
		if err := db.Create(&model.LoginSession{AccountID: id, TokenHash: secretHash(token), ExpiresAt: now.Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	access := model.OIDCAccessToken{AccountID: reader.ID, ClientID: "client", ExpiresAt: now.Add(time.Hour)}
	if err := db.Create(&access).Error; err != nil {
		t.Fatal(err)
	}
	refresh := model.OIDCRefreshToken{AccessID: access.ID, AccountID: reader.ID, ClientID: "client", TokenHash: secretHash("refresh"), ExpiresAt: now.Add(time.Hour)}
	authorization := model.OIDCAuthRequest{AccountID: &reader.ID, ClientID: "client", RequestJSON: []byte("{}"), ExpiresAt: now.Add(time.Hour)}
	for _, entity := range []interface{}{&refresh, &authorization} {
		if err := db.Create(entity).Error; err != nil {
			t.Fatal(err)
		}
	}
	app := newTestRouter()
	app.Use(auth, csrfGuard)
	app.Post("/admin/users/{id}/nickname", requireAdmin, updateUserNickname)
	app.Post("/admin/users/{id}/disable", requireAdmin, disableUser)
	post := func(id, action, token, origin string, fields url.Values) int {
		t.Helper()
		req := httptest.NewRequest("POST", "http://localhost:8080/admin/users/"+id+"/"+action, strings.NewReader(fields.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: token})
		}
		result := httptest.NewRecorder()
		app.ServeHTTP(result, req)
		return result.Code
	}
	origin := "http://localhost:8080"
	for _, trial := range []struct {
		action, token, origin, value string
		status                       int
	}{
		{"nickname", "reader-session", origin, "Bad", 403},
		{"nickname", "", origin, "Bad", 403},
		{"disable", "reader-session", origin, "", 403},
		{"nickname", "admin-session", "https://other.example", "Bad", 403},
		{"nickname", "admin-session", origin, "", 400},
		{"nickname", "admin-session", origin, strings.Repeat("界", 81), 400},
		{"nickname", "admin-session", origin, "Bad\nname", 400},
		{"disable", "admin-session", origin, "", 400},
	} {
		if status := post(reader.ID, trial.action, trial.token, trial.origin, url.Values{"nickname": {trial.value}}); status != trial.status {
			t.Errorf("%+v status=%d", trial, status)
		}
	}
	var unchanged model.Account
	if err := db.Take(&unchanged, "id = ?", reader.ID).Error; err != nil || unchanged.Nickname != "Original" || unchanged.DisabledAt != nil {
		t.Fatalf("rejected operations changed account: %+v %v", unchanged, err)
	}
	if status := post(reader.ID, "nickname", "admin-session", origin, url.Values{"nickname": {" Moderated "}}); status != 303 {
		t.Fatalf("rename status=%d", status)
	}
	if err := db.Take(&unchanged, "id = ?", reader.ID).Error; err != nil || unchanged.Nickname != "Moderated" {
		t.Fatalf("rename not persisted: %v", err)
	}
	if name := (&model.Comment{AccountID: &reader.ID, Account: &unchanged}).PublicName(); name != "Moderated" {
		t.Fatalf("public name=%s", name)
	}
	if status := post(admin.ID, "disable", "admin-session", origin, url.Values{"confirm": {"disable"}}); status != 403 {
		t.Fatalf("self ban=%d", status)
	}
	for i := 0; i < 2; i++ {
		if status := post(reader.ID, "disable", "admin-session", origin, url.Values{"confirm": {"disable"}}); status != 303 {
			t.Fatalf("ban status=%d", status)
		}
	}
	for _, action := range []string{"user.nickname.update", "user.disable"} {
		var events []model.AuditEvent
		if err := db.Where("action = ? AND target_id = ?", action, reader.ID).Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].ActorID != admin.ID || events[0].Outcome != "success" {
			t.Fatalf("incorrect audit for %s: %+v", action, events)
		}
	}
	var banned model.Account
	if err := db.Take(&banned, "id = ?", reader.ID).Error; err != nil || banned.DisabledAt == nil || banned.Nickname != disabledAccountNickname {
		t.Fatalf("ban not persisted: %+v %v", banned, err)
	}
	if name := (&model.Comment{AccountID: &reader.ID, Account: &banned}).PublicName(); name != disabledAccountNickname {
		t.Fatalf("public banned name=%s", name)
	}
	for _, entity := range []interface{}{&model.LoginSession{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}, &model.OIDCAuthRequest{}} {
		var count int64
		if err := db.Model(entity).Where("account_id = ?", reader.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("credential %T remains: %d %v", entity, count, err)
		}
	}
	if _, err := findLoginSession("reader-session"); err == nil {
		t.Error("banned session authenticated")
	}
	if _, err := findLoginSession("admin-session"); err != nil {
		t.Fatalf("admin session affected: %v", err)
	}
	if status := post(reader.ID, "nickname", "admin-session", origin, url.Values{"nickname": {"Restored"}}); status != 409 {
		t.Fatalf("banned rename=%d", status)
	}
}
