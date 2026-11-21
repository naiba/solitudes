package router

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func multipartUpload(t *testing.T, fileSize int) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file[]", "video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("v"), fileSize)); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, form.FormDataContentType()
}

// Every request body was capped at 4 MiB before any handler ran, so the upload
// endpoint could never receive the 50 MB files it is written to accept.
func TestUploadEndpointReceivesFilesLargerThanTheGeneralBodyLimit(t *testing.T) {
	app := chi.NewRouter()
	app.Use(requestLifecycle(nil, nil))
	receivedBytes := int64(-1)
	app.Post("/admin/upload", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error {
		form, err := multipartForm(r)
		if err != nil {
			return err
		}
		receivedBytes = form.File["file[]"][0].Size
		return writeStatus(w, 204)
	}))
	otherEndpointCalled := false
	app.Post("/admin/publish", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error {
		otherEndpointCalled = true
		return writeStatus(w, 204)
	}))
	const fileSize = 6 << 20

	body, contentType := multipartUpload(t, fileSize)
	upload := httptest.NewRequest(http.MethodPost, "/admin/upload", body)
	upload.Header.Set("Content-Type", contentType)
	result := httptest.NewRecorder()
	app.ServeHTTP(result, upload)
	if result.Code != 204 || receivedBytes != fileSize {
		t.Fatalf("6 MiB upload: status = %d, file bytes seen by the handler = %d", result.Code, receivedBytes)
	}

	// The general limit still protects every other endpoint.
	body, contentType = multipartUpload(t, fileSize)
	other := httptest.NewRequest(http.MethodPost, "/admin/publish", body)
	other.Header.Set("Content-Type", contentType)
	result = httptest.NewRecorder()
	app.ServeHTTP(result, other)
	if result.Code != 413 || otherEndpointCalled {
		t.Fatalf("6 MiB body on another endpoint: status = %d, handler called = %t", result.Code, otherEndpointCalled)
	}
}

// The upload endpoint has a limit of its own, also when the client does not
// announce the length, and nothing is read before the handler asks for it:
// the handler rejects callers without publishing rights first.
func TestUploadEndpointBoundsTheBodyWithoutReadingItUpFront(t *testing.T) {
	app := chi.NewRouter()
	app.Use(requestLifecycle(nil, nil))
	var parseErr error
	app.Post("/admin/upload", serveEndpoint(func(w http.ResponseWriter, r *http.Request) error {
		if r.Header.Get("X-Test-Reject-Before-Reading") != "" {
			return newHTTPError(http.StatusForbidden)
		}
		_, parseErr = multipartForm(r)
		if parseErr != nil {
			return newHTTPError(http.StatusRequestEntityTooLarge)
		}
		return writeStatus(w, 204)
	}))

	announced := httptest.NewRequest(http.MethodPost, "/admin/upload", http.NoBody)
	announced.ContentLength = maxUploadRequestSize + 1
	result := httptest.NewRecorder()
	app.ServeHTTP(result, announced)
	if result.Code != 413 {
		t.Fatalf("announced oversized upload: status = %d, want 413", result.Code)
	}

	oversized, contentType := multipartUpload(t, maxUploadRequestSize+1)
	chunked := httptest.NewRequest(http.MethodPost, "/admin/upload", oversized)
	chunked.Header.Set("Content-Type", contentType)
	chunked.ContentLength = -1
	result = httptest.NewRecorder()
	app.ServeHTTP(result, chunked)
	if result.Code != 413 || parseErr == nil {
		t.Fatalf("chunked oversized upload: status = %d, parse error = %v", result.Code, parseErr)
	}

	unread := &countingReader{remaining: 8 << 20}
	rejected := httptest.NewRequest(http.MethodPost, "/admin/upload", unread)
	rejected.Header.Set("X-Test-Reject-Before-Reading", "1")
	rejected.ContentLength = 8 << 20
	result = httptest.NewRecorder()
	app.ServeHTTP(result, rejected)
	if result.Code != 403 || unread.read != 0 {
		t.Fatalf("upload rejected by the handler: status = %d, body bytes read = %d, want 0", result.Code, unread.read)
	}
}

type countingReader struct{ remaining, read int }

func (r *countingReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(buffer), r.remaining)
	r.remaining -= n
	r.read += n
	return n, nil
}
