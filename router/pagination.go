package router

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/naiba/solitudes/pkg/pagination"
)

func listPage(raw string) (int, error) {
	page, err := pagination.Parse(raw)
	if err != nil {
		return 0, newHTTPError(http.StatusBadRequest)
	}
	return page, nil
}

// Navigation is data only; themes can render it in any layout. Preserve all
// filters and independent page parameters, and use same-origin relative URLs.
type pageNavigation struct {
	Page           int
	Previous, Next string
}

func pageNavigationFor(writer http.ResponseWriter, httpRequest *http.Request, key string, page int, more bool, fragment string) pageNavigation {
	link := func(n int) string {
		values, _ := url.ParseQuery(string(httpRequest.URL.RawQuery))
		values.Set(key, strconv.Itoa(n))
		return (&url.URL{Path: requestPath(httpRequest), RawQuery: values.Encode(), Fragment: fragment}).String()
	}
	nav := pageNavigation{Page: page}
	if page > 1 {
		nav.Previous = link(page - 1)
	}
	if more && page < pagination.MaxPage {
		nav.Next = link(page + 1)
	}
	return nav
}

func archiveNavigation(writer http.ResponseWriter, httpRequest *http.Request, base string, pg *pagination.Paginator) pageNavigation {
	nav := pageNavigation{Page: pg.Page}
	link := func(n int) string {
		if n == 1 {
			return base
		}
		return base + strconv.Itoa(n) + "/"
	}
	if pg.Page > 1 {
		nav.Previous = link(pg.Page - 1)
	}
	if pg.Page < pg.TotalPage && pg.Page < pagination.MaxPage {
		nav.Next = link(pg.Page + 1)
	}
	return nav
}
