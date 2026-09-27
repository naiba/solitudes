package router

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-uuid"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func newPostgresIdentityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("SOLITUDES_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SOLITUDES_TEST_POSTGRES_DSN to a dedicated PostgreSQL test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	random, err := uuid.GenerateUUID()
	if err != nil {
		t.Fatal(err)
	}
	schema := "solitudes_test_" + strings.ReplaceAll(random, "-", "")
	quoted := pq.QuoteIdentifier(schema)
	if err := db.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA " + quoted + " CASCADE").Error; err != nil {
			t.Errorf("remove temporary test schema %s: %v", schema, err)
		}
	})
	if err := db.Exec("SET search_path TO " + quoted + ", public").Error; err != nil {
		t.Fatal(err)
	}
	// uuid-ossp is database-wide and may already belong to another test's
	// temporary schema. Keep this fixture self-contained instead.
	if err := db.Exec(`CREATE FUNCTION uuid_generate_v4() RETURNS uuid LANGUAGE SQL AS 'SELECT gen_random_uuid()'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Account{}, &model.LoginSession{}, &model.EmailAction{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// startMailCatcher implements enough of SMTP to exercise gomail's real TCP
// delivery. It never relays mail or logs the one-time verification URL.
func startMailCatcher(t *testing.T) (int, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	messages := make(chan string, 32)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				reader := bufio.NewReader(conn)
				_, _ = io.WriteString(conn, "220 localhost SMTP catcher\r\n")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						_, _ = io.WriteString(conn, "250-localhost\r\n250 8BITMIME\r\n")
					case strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
						_, _ = io.WriteString(conn, "250 OK\r\n")
					case strings.HasPrefix(line, "DATA"):
						_, _ = io.WriteString(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
						var body strings.Builder
						for {
							part, err := reader.ReadString('\n')
							if err != nil {
								return
							}
							if part == ".\r\n" {
								break
							}
							body.WriteString(part)
						}
						messages <- body.String()
						_, _ = io.WriteString(conn, "250 Queued\r\n")
					case strings.HasPrefix(line, "QUIT"):
						_, _ = io.WriteString(conn, "221 Bye\r\n")
						return
					default:
						_, _ = io.WriteString(conn, "500 Unsupported\r\n")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		workers.Wait()
	})
	return listener.Addr().(*net.TCPAddr).Port, messages
}

func TestPostgresRegistrationSMTPAndVerifiedLogin(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "localhost:8080"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config}
	port, messages := startMailCatcher(t)
	solitudes.System.Config.Email.Host = "127.0.0.1"
	solitudes.System.Config.Email.Port = port
	solitudes.System.Config.Email.User = "test@localhost"
	t.Setenv("SOLITUDES_E2E", "1")
	app := fiber.New()
	app.Use(auth, csrfGuard)
	app.Post("/admin/register", guestRequired, registerHandler)
	app.Post("/admin/login", guestRequired, loginHandler)
	app.Get("/admin/verify-email", verifyEmailHandler)
	post := func(path string, fields url.Values) *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(fields.Encode()))
		req.Host = "localhost:8080"
		req.Header.Set("Host", "localhost:8080")
		req.Header.Set("Origin", "http://localhost:8080")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	fields := url.Values{"email": {"reader@example.com"}, "nickname": {"Reader"},
		"password": {"some-long-password"}, "captcha": {"test"}, "captchaId": {"test"}}
	resp := post("/admin/register", fields)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("registration status: %d", resp.StatusCode)
	}
	login := url.Values{"email": {"reader@example.com"}, "password": {"some-long-password"},
		"captcha": {"test"}, "captchaId": {"test"}}
	resp = post("/admin/login", login)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unverified account logged in: %d", resp.StatusCode)
	}
	var message string
	select {
	case message = <-messages:
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP catcher did not receive verification message")
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(message)))
	if err != nil {
		t.Fatal(err)
	}
	linkPattern := regexp.MustCompile(`http://localhost:8080/admin/verify-email\?token=[a-f0-9]{64}`)
	link := linkPattern.FindString(string(decoded))
	if link == "" || !strings.Contains(message, "To: reader@example.com") {
		t.Fatal("SMTP message has no verification link or unexpected recipient")
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	var action model.EmailAction
	if err := db.Take(&action, "purpose = ?", "verify_email").Error; err != nil || action.TokenHash == parsed.Query().Get("token") {
		t.Fatalf("verification token persisted in plaintext: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil)
	resp, err = app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("verification status: %d", resp.StatusCode)
	}
	resp, err = app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reused verification link: %d", resp.StatusCode)
	}
	resp = post("/admin/login", login)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || len(resp.Cookies()) == 0 {
		t.Fatalf("verified login: %d (cookies: %d)", resp.StatusCode, len(resp.Cookies()))
	}
	var account model.Account
	if err := db.Take(&account, "email = ?", "reader@example.com").Error; err != nil || account.EmailVerifiedAt == nil || account.Role != model.RoleUser {
		t.Fatalf("registered account: %+v %v", account, err)
	}
}

func TestPostgresEditorCannotAlterAnotherAuthorsArticle(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	search, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = search.Close() })
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "localhost:8080"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config, Search: search}
	now := time.Now()
	accounts := []model.Account{
		{Email: "admin@example.com", Nickname: "Admin", Role: model.RoleAdmin, EmailVerifiedAt: &now},
		{Email: "editor@example.com", Nickname: "Editor", Role: model.RoleEditor, EmailVerifiedAt: &now},
		{Email: "reader@example.com", Nickname: "Reader", Role: model.RoleUser, EmailVerifiedAt: &now},
	}
	for i := range accounts {
		if err := db.Create(&accounts[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.LoginSession{AccountID: accounts[i].ID,
			TokenHash: secretHash(string(accounts[i].Role)), ExpiresAt: now.Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	article := model.Article{AuthorID: &accounts[0].ID, Slug: "admin-article", Title: "Old article",
		Content: "original content", TemplateID: solitudes.ArticleTemplateID, Version: 1}
	if err := db.Create(&article).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(auth, csrfGuard)
	app.Post("/admin/publish", loginRequired, publishHandler)
	app.Delete("/admin/articles", loginRequired, deleteArticle)
	call := func(role model.Role, method, path string, fields url.Values) int {
		t.Helper()
		var requestBody io.Reader
		if fields != nil {
			requestBody = strings.NewReader(fields.Encode())
		}
		req := httptest.NewRequest(method, path, requestBody)
		req.Host = "localhost:8080"
		req.Header.Set("Host", "localhost:8080")
		req.Header.Set("Origin", "http://localhost:8080")
		if fields != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: string(role)})
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	form := url.Values{"id": {article.ID}, "title": {"Changed title"}, "slug": {article.Slug},
		"content": {"changed content"}, "template": {"1"}}
	if status := call(model.RoleEditor, http.MethodPost, "/admin/publish", form); status != http.StatusForbidden {
		t.Fatalf("editor edited another author's article: %d", status)
	}
	if status := call(model.RoleEditor, http.MethodDelete, "/admin/articles?id="+article.ID, nil); status != http.StatusNotFound {
		t.Fatalf("editor deleted another author's article: %d", status)
	}
	if status := call(model.RoleUser, http.MethodPost, "/admin/publish", form); status != http.StatusForbidden {
		t.Fatalf("ordinary user published: %d", status)
	}
	var stored model.Article
	if err := db.Take(&stored, "id = ?", article.ID).Error; err != nil || stored.Content != "original content" {
		t.Fatalf("unauthorized write changed article: %+v %v", stored, err)
	}
	if status := call(model.RoleAdmin, http.MethodPost, "/admin/publish", form); status != http.StatusOK {
		t.Fatalf("administrator unable to edit article: %d", status)
	}
	if err := db.Take(&stored, "id = ?", article.ID).Error; err != nil || stored.AuthorID == nil || *stored.AuthorID != accounts[0].ID || stored.Content != "changed content" {
		t.Fatalf("administrator edit changed ownership or failed: %+v %v", stored, err)
	}
	owned := model.Article{AuthorID: &accounts[1].ID, Slug: "editor-article", Title: "Editor's article",
		Content: "editor's draft", TemplateID: solitudes.ArticleTemplateID, Version: 1}
	if err := db.Create(&owned).Error; err != nil {
		t.Fatal(err)
	}
	form.Set("id", owned.ID)
	form.Set("slug", owned.Slug)
	if status := call(model.RoleEditor, http.MethodPost, "/admin/publish", form); status != http.StatusOK {
		t.Fatalf("editor cannot directly publish their article: %d", status)
	}
	stored = model.Article{}
	if err := db.Take(&stored, "id = ?", owned.ID).Error; err != nil || stored.AuthorID == nil || *stored.AuthorID != accounts[1].ID || stored.Content != "changed content" {
		t.Fatalf("editor edit changed ownership or failed: %+v %v", stored, err)
	}
}

func TestPostgresOIDCProviderAuthorizationAndTokenRotation(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.OIDCClient{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{},
		&model.OIDCRefreshToken{}, &model.OIDCSigningKey{}, &model.OIDCCryptoKey{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "localhost:8080"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config}
	now := time.Now()
	account := model.Account{Email: "oidc-user@example.com", Nickname: "OIDC user", Role: model.RoleUser, EmailVerifiedAt: &now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	client := model.OIDCClient{ID: "downstream-app", Name: "App", Public: true,
		RedirectURIsJSON: `["http://localhost:9999/callback"]`}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	provider, err := newOIDCProvider()
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req := httptest.NewRequest(method, path, body)
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp := httptest.NewRecorder()
		provider.ServeHTTP(resp, req)
		return resp
	}
	if resp := request(http.MethodGet, "/.well-known/openid-configuration", nil); resp.Code != http.StatusOK ||
		!strings.Contains(resp.Body.String(), `"issuer":"http://localhost:8080"`) {
		t.Fatalf("discovery failed: %d %s", resp.Code, resp.Body.String())
	}
	verifier := strings.Repeat("v", 64)
	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{"client_id": {client.ID}, "redirect_uri": {"http://localhost:9999/callback"},
		"response_type": {"code"}, "scope": {"openid email offline_access"}, "state": {"request-state"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}}
	badRedirect := url.Values{}
	for key, values := range query {
		badRedirect[key] = append([]string(nil), values...)
	}
	badRedirect.Set("redirect_uri", "http://localhost:9998/attacker")
	if resp := request(http.MethodGet, "/authorize?"+badRedirect.Encode(), nil); resp.Code == http.StatusFound {
		t.Fatal("unregistered OIDC redirect accepted")
	}
	start := request(http.MethodGet, "/authorize?"+query.Encode(), nil)
	redirect, err := url.Parse(start.Header().Get("Location"))
	if err != nil || start.Code != http.StatusFound || redirect.Path != "/oidc/consent" {
		t.Fatalf("authorization: %d %v %v", start.Code, redirect, err)
	}
	authID := redirect.Query().Get("authRequestID")
	if authID == "" {
		t.Fatal("authorization request not persisted")
	}
	if err := db.Model(&model.OIDCAuthRequest{}).Where("id = ?", authID).
		Updates(map[string]interface{}{"approved": true, "account_id": account.ID, "auth_time": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	callback := request(http.MethodGet, "/authorize/callback?id="+url.QueryEscape(authID), nil)
	redirect, err = url.Parse(callback.Header().Get("Location"))
	if err != nil || callback.Code != http.StatusFound || redirect.Query().Get("state") != "request-state" {
		t.Fatalf("callback: %d %v %v", callback.Code, redirect, err)
	}
	code := redirect.Query().Get("code")
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID},
		"redirect_uri": {"http://localhost:9999/callback"}, "code_verifier": {verifier}, "code": {code}}
	result := request(http.MethodPost, "/oauth/token", form)
	if result.Code != http.StatusOK {
		t.Fatalf("authorization code exchange: %d %s", result.Code, result.Body.String())
	}
	var tokens struct {
		Access  string `json:"access_token"`
		ID      string `json:"id_token"`
		Refresh string `json:"refresh_token"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &tokens); err != nil || tokens.Access == "" || tokens.ID == "" || tokens.Refresh == "" {
		t.Fatalf("OIDC tokens missing: %v %s", err, result.Body.String())
	}
	if replay := request(http.MethodPost, "/oauth/token", form); replay.Code == http.StatusOK {
		t.Fatal("authorization code replay accepted")
	}
	userReq := httptest.NewRequest(http.MethodGet, "/userinfo", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokens.Access)
	userResp := httptest.NewRecorder()
	provider.ServeHTTP(userResp, userReq)
	if userResp.Code != http.StatusOK || !strings.Contains(userResp.Body.String(), account.Email) {
		t.Fatalf("userinfo: %d %s", userResp.Code, userResp.Body.String())
	}
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {tokens.Refresh}}
	rotated := request(http.MethodPost, "/oauth/token", refresh)
	if rotated.Code != http.StatusOK || !strings.Contains(rotated.Body.String(), `"refresh_token"`) {
		t.Fatalf("refresh rotation: %d %s", rotated.Code, rotated.Body.String())
	}
	if replay := request(http.MethodPost, "/oauth/token", refresh); replay.Code == http.StatusOK {
		t.Fatal("old refresh token accepted after rotation")
	}
}

func TestPostgresSiblingArticleQueriesDoNotShareConditions(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	solitudes.System = &solitudes.SysVariable{DB: db, Config: &model.Config{}}
	base := time.Now().Add(-time.Hour)
	var posts [3]model.Article
	for i := range posts {
		posts[i] = model.Article{Slug: fmt.Sprintf("neighbor-%d", i), Title: "Neighbor", Content: "body",
			TemplateID: solitudes.ArticleTemplateID, Version: 1, CreatedAt: base.Add(time.Duration(i) * time.Minute)}
		if err := db.Create(&posts[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	relatedSiblingArticle(&posts[1], nil)
	if posts[1].SibilingArticle == nil || posts[1].SibilingArticle.Prev.ID != posts[0].ID || posts[1].SibilingArticle.Next.ID != posts[2].ID {
		t.Fatalf("previous/next queries leaked conditions into each other: %+v", posts[1].SibilingArticle)
	}
}
