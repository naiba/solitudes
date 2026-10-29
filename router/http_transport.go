package router

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/naiba/solitudes"
	"github.com/zitadel/schema"
)

type viewData = map[string]any

type viewRenderer interface {
	Render(io.Writer, string, interface{}, ...string) error
}
type endpointHandler func(http.ResponseWriter, *http.Request) error
type endpointErrorHandler func(http.ResponseWriter, *http.Request, error) error

type httpError struct {
	Code    int
	Message string
}

func (e *httpError) Error() string { return e.Message }
func newHTTPError(code int, message ...string) *httpError {
	text := http.StatusText(code)
	if len(message) > 0 {
		text = message[0]
	}
	return &httpError{Code: code, Message: text}
}

type requestStateKey struct{}
type requestState struct {
	values       map[any]any
	body         []byte
	views        viewRenderer
	errorHandler endpointErrorHandler
	err          error
	status       int
	beforeCommit func(int)
}

func stateFor(r *http.Request) *requestState {
	if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok {
		return state
	}
	state := &requestState{values: make(map[any]any)}
	*r = *r.WithContext(context.WithValue(r.Context(), requestStateKey{}, state))
	return state
}
func requestValue(r *http.Request, key any, value ...any) any {
	state := stateFor(r)
	if len(value) > 0 {
		state.values[key] = value[0]
	}
	return state.values[key]
}
func setRequestContext(r *http.Request, ctx context.Context) { *r = *r.WithContext(ctx) }

// The writer observes the actual commit, so audit and cache policy see the
// final status without buffering uploads, static files or streaming responses.
type responseWriter struct {
	http.ResponseWriter
	request *http.Request
	state   *requestState
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) WriteHeader(status int) {
	if w.state.status != 0 {
		return
	}
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.state.status = status
	if w.state.beforeCommit != nil {
		w.state.beforeCommit(status)
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(body []byte) (int, error) {
	if w.state.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	// Real HTTP servers suppress HEAD bodies; do the same with httptest writers.
	if w.request.Method == http.MethodHead {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}
func (w *responseWriter) Flush() {
	if w.state.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Track the commit in request state, not a concrete writer assertion: standard
// logging and tracing middleware wrap ResponseWriter and must not bypass guards.
func responseStatus(r *http.Request) int {
	if status := stateFor(r).status; status != 0 {
		return status
	}
	return http.StatusOK
}
func responseCommitted(r *http.Request) bool { return stateFor(r).status != 0 }

func requestLifecycle(views viewRenderer, errorHandler endpointErrorHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := &requestState{values: make(map[any]any), views: views, errorHandler: errorHandler}
			r = r.WithContext(context.WithValue(r.Context(), requestStateKey{}, state))
			writer := &responseWriter{ResponseWriter: w, request: r, state: state}
			// Match the previous 4 MiB request limit, including chunked request bodies.
			if r.ContentLength > 4<<20 {
				respondError(writer, r, newHTTPError(http.StatusRequestEntityTooLarge))
				return
			}
			if r.Body == nil {
				r.Body = http.NoBody
			}
			// A lazy MaxBytesReader alone permits mutations before an oversized
			// chunked body is read. Validate the complete bounded body first,
			// preserving the previous server's limit and replayable payload.
			limited := http.MaxBytesReader(writer, r.Body, 4<<20)
			body, err := io.ReadAll(limited)
			_ = limited.Close()
			if err != nil {
				var sizeError *http.MaxBytesError
				if errors.As(err, &sizeError) {
					respondError(writer, r, newHTTPError(http.StatusRequestEntityTooLarge))
				} else {
					respondError(writer, r, newHTTPError(http.StatusBadRequest))
				}
				return
			}
			state.body = body
			r.Body = io.NopCloser(bytes.NewReader(body))
			defer func() {
				if r.MultipartForm != nil {
					_ = r.MultipartForm.RemoveAll()
				}
			}()
			next.ServeHTTP(writer, r)
		})
	}
}
func respondError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	state := stateFor(r)
	state.err = err
	if responseCommitted(r) {
		return
	}
	if state.errorHandler != nil {
		if renderErr := state.errorHandler(w, r, err); renderErr == nil || responseCommitted(r) {
			return
		}
	}
	status := http.StatusInternalServerError
	message := http.StatusText(status)
	var he *httpError
	if errors.As(err, &he) {
		status, message = he.Code, he.Message
	}
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		status, message = http.StatusRequestEntityTooLarge, http.StatusText(http.StatusRequestEntityTooLarge)
	}
	_ = writeText(w, status, message)
}
func serveEndpoint(handlers ...endpointHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, handler := range handlers {
			if err := handler(w, r); err != nil {
				respondError(w, r, err)
				return
			}
			if responseCommitted(r) {
				return
			}
		}
	}
}
func before(handler endpointHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := handler(w, r); err != nil {
				respondError(w, r, err)
				return
			}
			if !responseCommitted(r) {
				next.ServeHTTP(w, r)
			}
		})
	}
}

func requestPath(r *http.Request) string            { return r.URL.EscapedPath() }
func requestTarget(r *http.Request) string          { return r.URL.RequestURI() }
func routeParam(r *http.Request, key string) string { return chi.URLParam(r, key) }
func queryValue(r *http.Request, key string, fallback ...string) string {
	v := r.URL.Query().Get(key)
	if v == "" && len(fallback) > 0 {
		return fallback[0]
	}
	return v
}
func formValue(r *http.Request, key string, fallback ...string) string {
	v := r.PostFormValue(key)
	if v == "" && len(fallback) > 0 {
		return fallback[0]
	}
	return v
}
func requestHeader(r *http.Request, key string) string {
	if strings.EqualFold(key, "Host") {
		return r.Host
	}
	return r.Header.Get(key)
}
func requestCookie(r *http.Request, key string) string {
	c, err := r.Cookie(key)
	if err != nil {
		return ""
	}
	return c.Value
}

func trustedProxy(r *http.Request) bool {
	if solitudes.System == nil || solitudes.System.Config == nil {
		return false
	}
	cfg := solitudes.System.Config
	// Preserve the existing explicit opt-out while supporting trusted CIDRs.
	if !cfg.EnableTrustedProxyCheck {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	for _, proxy := range cfg.TrustedProxies {
		if parsed := net.ParseIP(proxy); parsed != nil && parsed.Equal(ip) {
			return true
		}
		if _, network, err := net.ParseCIDR(proxy); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
func requestIP(r *http.Request) string {
	if trustedProxy(r) && solitudes.System.Config.ProxyHeader != "" {
		if value := r.Header.Get(solitudes.System.Config.ProxyHeader); value != "" {
			return strings.TrimSpace(strings.Split(value, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func requestScheme(r *http.Request) string {
	if trustedProxy(r) {
		if proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); proto == "https" || proto == "http" {
			return proto
		}
		if r.Header.Get("X-Forwarded-Ssl") == "on" {
			return "https"
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
func requestHost(r *http.Request) string {
	host := r.Host
	if trustedProxy(r) {
		if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
			host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
		}
	}
	return host
}

type routeDescription struct {
	Path   string
	Params []string
}

func requestRoute(r *http.Request) *routeDescription {
	ctx := chi.RouteContext(r.Context())
	if ctx == nil {
		return &routeDescription{}
	}
	return &routeDescription{Path: ctx.RoutePattern(), Params: ctx.URLParams.Keys}
}

func requestBody(r *http.Request) []byte { return stateFor(r).body }
func decodeRequestBody(r *http.Request, target any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid content type")
	}
	switch media {
	case "application/json":
		// Unmarshal rejects trailing JSON; accepting only the first value could
		// perform a mutation for a malformed request previously rejected.
		return json.Unmarshal(requestBody(r), target)
	case "application/xml", "text/xml":
		return xml.Unmarshal(requestBody(r), target)
	case "application/x-www-form-urlencoded", "multipart/form-data":
		if media == "multipart/form-data" {
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				return err
			}
		} else if err := r.ParseForm(); err != nil {
			return err
		}
		decoder := schema.NewDecoder()
		decoder.SetAliasTag("form")
		decoder.IgnoreUnknownKeys(true)
		decoder.ZeroEmpty(true)
		return decoder.Decode(target, r.PostForm)
	default:
		return newHTTPError(http.StatusUnsupportedMediaType)
	}
}
func multipartForm(r *http.Request) (*multipart.Form, error) {
	err := r.ParseMultipartForm(4 << 20)
	return r.MultipartForm, err
}
func uploadedFile(r *http.Request, name string) (*multipart.FileHeader, error) {
	file, header, err := r.FormFile(name)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	return header, nil
}
func saveUploadedFile(header *multipart.FileHeader, path string) error {
	in, err := header.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	return errors.Join(copyErr, closeErr)
}

func renderPage(w http.ResponseWriter, r *http.Request, status int, name string, data any) error {
	engine := stateFor(r).views
	if engine == nil {
		return errors.New("template engine not configured")
	}
	var body bytes.Buffer
	// Render before committing headers: a template error must remain a real 500.
	if err := engine.Render(&body, name, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return writeBytes(w, status, body.Bytes())
}
func writeJSON(w http.ResponseWriter, status int, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	return writeBytes(w, status, body)
}
func writeText(w http.ResponseWriter, status int, text string) error {
	_, err := writeResponseString(w, status, text)
	return err
}
func writeResponseString(w http.ResponseWriter, status int, text string) (int, error) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	// HEAD carries the same representation headers even though its body is suppressed.
	w.Header().Set("Content-Length", strconv.Itoa(len(text)))
	w.WriteHeader(status)
	return io.WriteString(w, text)
}
func writeBytes(w http.ResponseWriter, status int, body []byte) error {
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, err := w.Write(body)
	return err
}
func writeStatus(w http.ResponseWriter, status int) error {
	if status == 204 || status == 304 {
		w.WriteHeader(status)
		return nil
	}
	return writeText(w, status, http.StatusText(status))
}
func redirectResponse(w http.ResponseWriter, r *http.Request, target string, status ...int) error {
	code := http.StatusFound
	if len(status) > 0 {
		code = status[0]
	}
	w.Header().Set("Location", target)
	w.WriteHeader(code)
	return nil
}
func setContentType(w http.ResponseWriter, extension string) {
	kind := mime.TypeByExtension("." + strings.TrimPrefix(extension, "."))
	if kind == "" {
		kind = "application/octet-stream"
	}
	w.Header().Set("Content-Type", kind)
}
func sendStream(w http.ResponseWriter, r *http.Request, stream io.Reader, size int) error {
	if closer, ok := stream.(io.Closer); ok {
		defer closer.Close()
	}
	w.Header().Set("Content-Length", strconv.Itoa(size))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return nil
	}
	_, err := io.Copy(w, stream)
	return err
}

// EscapedPath is required even when RawPath is empty: net/http may have already
// decoded %25. Decoding chi parameters twice would corrupt literal "%2F" slugs.
func prepareRoutePath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(r.URL.EscapedPath(), "/")
		if path == "" {
			path = "/"
		}
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		canonicalSystemPathParts(parts)
		chi.RouteContext(r.Context()).RoutePath = "/" + strings.Join(parts, "/")
		next.ServeHTTP(w, r)
	})
}
func equalFoldRouteLiteral(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
		isHTML := strings.Contains(r.Header.Get("Accept"), "html")
		w.Header().Set("Cache-Control", "no-store")
		if isHTML && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		stateFor(r).beforeCommit = func(status int) {
			if currentAccount(w, r) != nil {
				w.Header().Set("Cache-Control", "private, no-store")
			} else if isHTML && (status >= 300 || len(w.Header().Values("Set-Cookie")) > 0) {
				w.Header().Set("Cache-Control", "no-store")
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Log route patterns instead of request targets: verification links and OIDC
// callbacks carry credentials in query strings and sometimes path segments.
func requestAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("request completed method=%s route=%s status=%d duration=%s", r.Method, requestRoute(r).Path, responseStatus(r), time.Since(started))
	})
}
