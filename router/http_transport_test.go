package router

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/siteurl"
)

func TestChiArticlePathPreservesStoredSlug(t *testing.T) {
	app := chi.NewRouter()
	app.Use(requestLifecycle(nil, nil), prepareRoutePath, middleware.GetHead)
	app.Get("/{slug}", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error {
		slug, err := siteurl.DecodeArticleSlug(chi.URLParam(r, "slug"))
		if err != nil {
			return newHTTPError(400)
		}
		w.Header().Set("X-Article-Slug", url.PathEscape(slug))
		return writeText(w, 200, slug)
	}))
	for _, slug := range []string{"中文文章", "C#? +% &", "%2F-literal", "Mixed-Case"} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+"/"+slug, func(t *testing.T) {
				result := httptest.NewRecorder()
				app.ServeHTTP(result, httptest.NewRequest(method, siteurl.ArticlePath(slug), nil))
				if result.Code != 200 || result.Header().Get("X-Article-Slug") != url.PathEscape(slug) {
					t.Fatalf("status=%d slug=%q body=%q", result.Code, result.Header().Get("X-Article-Slug"), result.Body.String())
				}
				want := slug
				if method == "HEAD" {
					want = ""
				}
				if result.Body.String() != want {
					t.Fatalf("body=%q want=%q", result.Body.String(), want)
				}
			})
		}
	}
}

type failingPageRenderer struct{}

func (failingPageRenderer) Render(w io.Writer, _ string, _ interface{}, _ ...string) error {
	_, _ = io.WriteString(w, "private partial page")
	return errors.New("private template failure")
}
func TestHTTPTemplateFailureDoesNotCommitPartialPage(t *testing.T) {
	app := chi.NewRouter()
	app.Use(requestLifecycle(failingPageRenderer{}, nil))
	app.Get("/", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error { return renderPage(w, r, 200, "article", nil) }))
	result := httptest.NewRecorder()
	app.ServeHTTP(result, httptest.NewRequest("GET", "/", nil))
	if result.Code != 500 || result.Body.String() != "Internal Server Error" || strings.Contains(result.Body.String(), "private") {
		t.Fatalf("status=%d body=%q", result.Code, result.Body.String())
	}
}
func TestHTTPHandlerPreservesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := chi.NewRouter()
	app.Use(requestLifecycle(nil, nil))
	app.Get("/", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error {
		if !errors.Is(r.Context().Err(), context.Canceled) {
			t.Fatal("request cancellation lost")
		}
		return writeStatus(w, 204)
	}))
	result := httptest.NewRecorder()
	app.ServeHTTP(result, httptest.NewRequest("GET", "/", nil).WithContext(ctx))
	if result.Code != 204 || result.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", result.Code, result.Body.String())
	}
}
func TestHTTPBodyLimitRejectsUnknownLengthBeforeHandler(t *testing.T) {
	app := chi.NewRouter()
	app.Use(requestLifecycle(nil, nil))
	called := false
	app.Post("/", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error { called = true; return writeStatus(w, 204) }))
	req := httptest.NewRequest("POST", "/", bytes.NewReader(bytes.Repeat([]byte("a"), (4<<20)+1)))
	req.ContentLength = -1
	result := httptest.NewRecorder()
	app.ServeHTTP(result, req)
	if result.Code != 413 || called {
		t.Fatalf("status=%d handler_called=%t; oversized chunked uploads must be rejected before mutation", result.Code, called)
	}
}

func TestPostgresAuditPersistsAfterClientCancellation(t *testing.T) {
	db, admin, _ := auditTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := newTestRouter()
	app.Use(auditMiddleware)
	app.Post("/admin/settings", func(w http.ResponseWriter, r *http.Request) error {
		requestValue(r, solitudes.CtxAccount, &admin)
		cancel()
		return newHTTPError(http.StatusForbidden)
	})
	result := httptest.NewRecorder()
	app.ServeHTTP(result, httptest.NewRequest("POST", "/admin/settings", nil).WithContext(ctx))
	if result.Code != 403 {
		t.Fatalf("status=%d", result.Code)
	}
	var events []model.AuditEvent
	if err := db.Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ActorID != admin.ID || events[0].Status != 403 || events[0].RequestID == "" || events[0].Outcome != "denied" {
		t.Fatalf("cancelled request lost its security audit: %+v", events)
	}
}

func TestHTTPJSONRejectsTrailingPayloadBeforeMutation(t *testing.T) {
	app := newTestRouter()
	mutated := false
	app.Post("/account/profile", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Name string `json:"name"`
		}
		if err := decodeRequestBody(r, &input); err != nil {
			return newHTTPError(http.StatusBadRequest)
		}
		mutated = true
		return writeStatus(w, 204)
	})
	request := httptest.NewRequest("POST", "/account/profile", strings.NewReader(`{"name":"reader"}{"name":"unexpected"}`))
	request.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	app.ServeHTTP(result, request)
	if result.Code != 400 || mutated {
		t.Fatalf("status=%d mutated=%t", result.Code, mutated)
	}
}

func TestHTTPGuardStopsAfterRedirectThroughStandardMiddleware(t *testing.T) {
	app := chi.NewRouter()
	app.Use(requestLifecycle(nil, nil))
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(middleware.NewWrapResponseWriter(w, 1), r)
		})
	})
	called := false
	app.Get("/private", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error { return redirectResponse(w, r, "/login") }, func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return writeText(w, 200, "private body")
	}))
	result := httptest.NewRecorder()
	app.ServeHTTP(result, httptest.NewRequest("GET", "/private", nil))
	if called || result.Code != 302 || result.Header().Get("Location") != "/login" || strings.Contains(result.Body.String(), "private") {
		t.Fatalf("guard did not stop: called=%t status=%d body=%q", called, result.Code, result.Body.String())
	}
}

func TestHTTPHeadRetainsGetRepresentationHeaders(t *testing.T) {
	app := chi.NewRouter()
	app.Use(requestLifecycle(nil, nil), middleware.GetHead)
	app.Get("/article", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error { return writeText(w, 200, "文章正文") }))
	server := httptest.NewServer(app)
	defer server.Close()
	get, err := server.Client().Get(server.URL + "/article")
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()
	head, err := server.Client().Head(server.URL + "/article")
	if err != nil {
		t.Fatal(err)
	}
	defer head.Body.Close()
	body, err := io.ReadAll(head.Body)
	if err != nil {
		t.Fatal(err)
	}
	if get.StatusCode != 200 || head.StatusCode != 200 || len(body) != 0 || get.ContentLength != int64(len("文章正文")) || head.ContentLength != get.ContentLength || head.Header.Get("Content-Type") != get.Header.Get("Content-Type") {
		t.Fatalf("GET status=%d length=%d type=%s; HEAD status=%d length=%d type=%s body=%q", get.StatusCode, get.ContentLength, get.Header.Get("Content-Type"), head.StatusCode, head.ContentLength, head.Header.Get("Content-Type"), body)
	}
}

func TestHTTPForwardedHeadersRequireTrustedProxy(t *testing.T) {
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	solitudes.System = &solitudes.SysVariable{Config: &model.Config{EnableTrustedProxyCheck: true, TrustedProxies: []string{"10.0.0.0/8"}, ProxyHeader: "X-Forwarded-For"}}
	for _, tc := range []struct{ remote, ip, host, scheme string }{
		{"192.0.2.42:1234", "192.0.2.42", "origin.test", "http"},
		{"10.0.0.42:1234", "198.51.100.7", "public.test", "https"},
	} {
		req := httptest.NewRequest("GET", "http://origin.test/", nil)
		req.RemoteAddr = tc.remote
		req.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")
		req.Header.Set("X-Forwarded-Host", "public.test")
		req.Header.Set("X-Forwarded-Proto", "https")
		if ip, host, scheme := requestIP(req), requestHost(req), requestScheme(req); ip != tc.ip || host != tc.host || scheme != tc.scheme {
			t.Fatalf("peer=%s got ip=%s host=%s scheme=%s", tc.remote, ip, host, scheme)
		}
	}
}

func TestHTTPAccessLogOmitsCredentialBearingTargets(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	app := chi.NewRouter()
	app.Use(requestLifecycle(nil, nil), requestAccessLog)
	app.Get("/{slug}", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error { return writeStatus(w, 204) }))
	result := httptest.NewRecorder()
	app.ServeHTTP(result, httptest.NewRequest("GET", "/private-token?code=private-code&token=private-token", nil))
	text := output.String()
	if result.Code != 204 || !strings.Contains(text, "route=/{slug}") || !strings.Contains(text, "status=204") || strings.Contains(text, "private-token") || strings.Contains(text, "private-code") {
		t.Fatalf("unsafe or missing access log: %q", text)
	}
}

func TestHTTPAuditUsesCommittedStatusAfterWriteFailure(t *testing.T) {
	app := chi.NewRouter()
	app.Use(requestLifecycle(nil, nil))
	app.Get("/article", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		if got := auditStatus(w, r, io.ErrClosedPipe); got != http.StatusOK {
			t.Fatalf("committed 200 response audited as %d after client disconnect", got)
		}
		return nil
	}))
	result := httptest.NewRecorder()
	app.ServeHTTP(result, httptest.NewRequest("GET", "/article", nil))
	if result.Code != 200 {
		t.Fatalf("status=%d", result.Code)
	}
}
