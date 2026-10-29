package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/naiba/solitudes/pkg/pagination"
)

func TestSearchHonorsCancelledContext(t *testing.T) {
	app := newTestRouter()
	app.Get("/search", func(writer http.ResponseWriter, httpRequest *http.Request) error {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		setRequestContext(httpRequest, ctx)
		return search(writer, httpRequest)
	})
	resp, err := testRequest(app, httptest.NewRequest("GET", "/search?w=query", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("cancelled search status=%d", resp.StatusCode)
	}
}

func TestPageLinksPreserveFiltersAndFragments(t *testing.T) {
	app := newTestRouter()
	app.Get("/list", func(writer http.ResponseWriter, httpRequest *http.Request) error {
		nav := pageNavigationFor(writer, httpRequest, "page", 2, true, "comments")
		if nav.Previous != "/list?owner_id=owner&page=1&q=a%26b&status=active#comments" || !strings.Contains(nav.Next, "page=3") {
			t.Errorf("invalid navigation: %+v", nav)
		}
		last := pageNavigationFor(writer, httpRequest, "page", pagination.MaxPage, true, "")
		if last.Next != "" {
			t.Error("generated out-of-bounds link")
		}
		return nil
	})
	resp, err := testRequest(app, httptest.NewRequest("GET", "/list?owner_id=owner&page=2&q=a%26b&status=active", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestArchiveLinksRetainEncodedTag(t *testing.T) {
	app := newTestRouter()
	app.Get("/", func(writer http.ResponseWriter, httpRequest *http.Request) error {
		nav := archiveNavigation(writer, httpRequest, "/tags/A%2FB%20%26%20C/", &pagination.Paginator{Page: 2, TotalPage: 3})
		if nav.Previous != "/tags/A%2FB%20%26%20C/" || nav.Next != "/tags/A%2FB%20%26%20C/3/" {
			t.Fatalf("lost tag: %+v", nav)
		}
		return nil
	})
	resp, err := testRequest(app, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}
