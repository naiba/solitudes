package router

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type testHTTPOptions struct {
	Views        viewRenderer
	ErrorHandler endpointErrorHandler
}

// Fixtures compose native chi handlers. Each route captures the middleware
// registered before it, allowing protocol and browser fixtures to share a mux.
type testHTTPRouter struct {
	mux         *chi.Mux
	handler     http.Handler
	middlewares []func(http.Handler) http.Handler
	prefix      string
}

func newTestRouter(options ...testHTTPOptions) *testHTTPRouter {
	var cfg testHTTPOptions
	if len(options) > 0 {
		cfg = options[0]
	}
	mux := chi.NewRouter()
	mux.Use(prepareRoutePath, middleware.GetHead)
	return &testHTTPRouter{mux: mux, handler: requestLifecycle(cfg.Views, cfg.ErrorHandler)(mux)}
}
func (app *testHTTPRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	app.handler.ServeHTTP(w, r)
}
func (app *testHTTPRouter) Use(handlers ...any) {
	if prefix, ok := handlers[0].(string); ok {
		endpoint := handlers[1].(endpointHandler)
		handler := chi.Chain(app.middlewares...).Handler(serveEndpoint(endpoint))
		app.mux.Handle(strings.TrimRight(prefix, "/"), handler)
		app.mux.Handle(strings.TrimRight(prefix, "/")+"/*", handler)
		return
	}
	for _, handler := range handlers {
		switch h := handler.(type) {
		case func(http.Handler) http.Handler:
			app.middlewares = append(app.middlewares, h)
		case func(http.ResponseWriter, *http.Request) error:
			app.middlewares = append(app.middlewares, before(h))
		case endpointHandler:
			app.middlewares = append(app.middlewares, before(h))
		default:
			panic("unsupported HTTP fixture middleware")
		}
	}
	app.mux.NotFound(chi.Chain(app.middlewares...).Handler(serveEndpoint(func(w http.ResponseWriter, r *http.Request) error { return newHTTPError(http.StatusNotFound) })).ServeHTTP)
}
func (app *testHTTPRouter) Group(prefix string, handlers ...endpointHandler) *testHTTPRouter {
	group := &testHTTPRouter{mux: app.mux, middlewares: append([]func(http.Handler) http.Handler{}, app.middlewares...), prefix: strings.TrimRight(app.prefix+prefix, "/")}
	for _, h := range handlers {
		group.middlewares = append(group.middlewares, before(h))
	}
	return group
}

// Optional segments in existing behavior fixtures expand into separate chi
// routes. The production router declares these alternatives explicitly.
func fixtureRoutePatterns(pattern string) []string {
	pattern = strings.TrimRight(pattern, "/")
	if pattern == "" {
		return []string{"/"}
	}
	if strings.HasSuffix(pattern, "?") {
		split := strings.LastIndex(pattern, "/")
		return append(fixtureRoutePatterns(pattern[:split]), fixtureRoutePatterns(strings.TrimSuffix(pattern, "?"))...)
	}
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return []string{strings.Join(parts, "/")}
}
func (app *testHTTPRouter) register(method, path string, handlers ...endpointHandler) {
	for _, pattern := range fixtureRoutePatterns(app.prefix + path) {
		app.mux.Method(method, pattern, chi.Chain(app.middlewares...).Handler(serveEndpoint(handlers...)))
	}
}
func (app *testHTTPRouter) Get(path string, h ...endpointHandler) {
	app.register(http.MethodGet, path, h...)
}
func (app *testHTTPRouter) Post(path string, h ...endpointHandler) {
	app.register(http.MethodPost, path, h...)
}
func (app *testHTTPRouter) Delete(path string, h ...endpointHandler) {
	app.register(http.MethodDelete, path, h...)
}
func (app *testHTTPRouter) Patch(path string, h ...endpointHandler) {
	app.register(http.MethodPatch, path, h...)
}
func (app *testHTTPRouter) Put(path string, h ...endpointHandler) {
	app.register(http.MethodPut, path, h...)
}
func testRequest(handler http.Handler, r *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, r)
	return recorder.Result(), nil
}

func (app *testHTTPRouter) Add(method, path string, handlers ...endpointHandler) {
	app.register(method, path, handlers...)
}
