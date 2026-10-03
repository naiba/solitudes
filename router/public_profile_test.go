//go:build !postgres_test

package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPublicProfilePageNumbersAreBounded(t *testing.T) {
	app := fiber.New()
	app.Get("/users/:id", func(c *fiber.Ctx) error {
		_, err := profilePageNumber(c, "comments_page")
		return err
	})
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"", 200}, {"1", 200}, {"1000", 200}, {"0", 400}, {"-1", 400}, {"1001", 400}, {"9999999999999999999999999", 400}, {"abc", 400},
	} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/users/any?comments_page="+tc.value, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("comments_page=%q: %d, want %d", tc.value, resp.StatusCode, tc.want)
		}
	}
}

func TestProfileUpdateRequiresOwnerAndSameOrigin(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	owner := testAccount(t, db, model.RoleUser)
	other := model.Account{ID: "20000000-0000-4000-8000-000000000002", Email: "other@example.com", Nickname: "Other", Role: model.RoleUser}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	previousLookup := sessionLookup
	t.Cleanup(func() { sessionLookup = previousLookup })
	sessionLookup = func(token string) (*model.Account, error) {
		if token == "owner" {
			return &owner, nil
		}
		return nil, errNoAccount
	}
	app := fiber.New()
	app.Use(auth, csrfGuard)
	app.Post("/account/profile", requireAccount, updateAccountProfile)
	request := func(token, origin, nickname, bio string) int {
		t.Helper()
		form := url.Values{"nickname": {nickname}, "bio": {bio}, "directory_visible": {"on"}, "account_id": {other.ID}}
		req := httptest.NewRequest(http.MethodPost, "/account/profile", strings.NewReader(form.Encode()))
		req.Host = "localhost:8080"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if token != "" {
			req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: token})
		}
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := request("", "http://localhost:8080", "Intruder", "bio"); got != http.StatusFound {
		t.Fatalf("anonymous update status %d", got)
	}
	if got := request("owner", "https://evil.example", "Intruder", "bio"); got != http.StatusForbidden {
		t.Fatalf("cross-origin update status %d", got)
	}
	if got := request("owner", "http://localhost:8080", "", "bio"); got != http.StatusBadRequest {
		t.Fatalf("empty nickname status %d", got)
	}
	if got := request("owner", "http://localhost:8080", "Name", strings.Repeat("x", 501)); got != http.StatusBadRequest {
		t.Fatalf("oversized bio status %d", got)
	}
	if got := request("owner", "http://localhost:8080", "New Name", "Public bio"); got != http.StatusSeeOther {
		t.Fatalf("owner update status %d", got)
	}
	var savedOwner, savedOther model.Account
	if err := db.Take(&savedOwner, "id = ?", owner.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&savedOther, "id = ?", other.ID).Error; err != nil {
		t.Fatal(err)
	}
	if savedOwner.Nickname != "New Name" || savedOwner.Bio != "Public bio" || savedOwner.DirectoryHidden ||
		savedOther.Nickname != "Other" || savedOther.Bio != "" || savedOther.DirectoryHidden {
		t.Fatalf("profile ownership mismatch owner=%+v other=%+v", savedOwner, savedOther)
	}
}
