package router

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/pagination"
	"github.com/naiba/solitudes/pkg/translator"
)

type paginationView struct{ data fiber.Map }

func (v *paginationView) Load() error { return nil }
func (v *paginationView) Render(w io.Writer, name string, binding interface{}, layout ...string) error {
	v.data = binding.(fiber.Map)["Data"].(fiber.Map)
	_, err := io.WriteString(w, "rendered")
	return err
}

func TestPostgresPaginationBoundariesAndPrivacy(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.Passkey{}, &model.ExternalIdentity{}, &model.OIDCClient{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}); err != nil {
		t.Fatal(err)
	}
	admin := model.Account{Email: "admin@pagination.test", Nickname: "Admin", Role: model.RoleAdmin}
	reader := model.Account{Email: "reader@pagination.test", Nickname: "Reader", Role: model.RoleUser}
	for _, account := range []*model.Account{&admin, &reader} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	root, err := seedPaginationFixture(db, admin, reader)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for page := 1; page <= 6; page++ {
		var rows []model.Article
		pg, err := pagination.Paging(&pagination.Param{DB: db.Where("visibility = 'public'"), Page: page, Limit: 20, OrderBy: []string{"created_at DESC, id DESC"}}, &rows)
		if err != nil || pg.TotalRecord != 112 {
			t.Fatalf("page %d: %v %+v", page, err, pg)
		}
		for _, row := range rows {
			if seen[row.ID] {
				t.Fatal("duplicate between pages")
			}
			seen[row.ID] = true
		}
	}
	if len(seen) != 112 {
		t.Fatal("missing articles")
	}
	var rows []model.Article
	empty, err := pagination.Paging(&pagination.Param{DB: db.Where("1 = 0")}, &rows)
	if err != nil || empty.TotalRecord != 0 || empty.TotalPage != 1 || empty.NextPage != 1 {
		t.Fatalf("invalid empty first page: %+v %v", empty, err)
	}
	if _, err := pagination.Paging(&pagination.Param{DB: db.Table("missing_table")}, &rows); err == nil {
		t.Fatal("count error hidden")
	}
	if _, err := pagination.Paging(&pagination.Param{DB: db.Select("missing_column")}, &rows); err == nil {
		t.Fatal("query error hidden")
	}

	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.Symlink(filepath.Join(repo, "resource"), "resource"); err != nil {
		t.Fatal(err)
	}
	conf := &model.Config{}
	conf.Site.Theme = "cactus"
	conf.Admin.Theme = "default"
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	solitudes.System = &solitudes.SysVariable{DB: db, Config: conf, Search: index}
	if err := LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	var all []model.Article
	if err := db.Find(&all).Error; err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if err := index.Index(a.GetIndexID(), map[string]interface{}{"Title": a.Title, "Content": a.Content, "IsPrivate": !a.Public()}); err != nil {
			t.Fatal(err)
		}
	}
	view := &paginationView{}
	var actor *model.Account
	app := fiber.New(fiber.Config{Views: view})
	app.Use(trans, func(c *fiber.Ctx) error {
		if actor != nil {
			c.Locals(solitudes.CtxAccount, actor)
		}
		return c.Next()
	})
	app.Get("/search", search)
	app.Get("/account", requireAccount, accountPage)
	app.Get("/account/oidc/clients", requireAccount, oidcClientsPage)
	app.Get("/admin/media", requireAdmin, media)
	app.Get("/:slug", article)
	request := func(path string, status int) fiber.Map {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest("GET", path, nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s status=%d want=%d %s", path, resp.StatusCode, status, body)
		}
		return view.data
	}
	data := request("/search?w=pagingneedle&page=12", 200)
	if len(data["results"].([]searchResp)) != 2 || data["navigation"].(pageNavigation).Next != "" {
		t.Fatal("search truncated at first candidate batch")
	}
	request("/search?w=pagingneedle&page=1001", 400)
	data = request("/paging-000?thread="+root, 200)
	thread := data["article"].(*model.Article)
	if len(thread.Comments) != 1 || len(thread.Comments[0].ChildComments) != 20 {
		t.Fatal("reply page unbounded")
	}
	data = request("/paging-000?thread="+root+"&replies_page=2", 200)
	if len(data["article"].(*model.Article).Comments[0].ChildComments) != 3 {
		t.Fatal("reply last page wrong")
	}
	request("/paging-private?thread="+root, 404)
	request("/paging-001?thread="+root, 404)
	var child model.Comment
	if err := db.Where("reply_to = ?", root).First(&child).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Comment{}).Where("id = ?", root).Update("is_spam", true).Error; err != nil {
		t.Fatal(err)
	}
	request("/paging-000?thread="+root, 404)
	request("/paging-000?thread="+child.ID, 404)
	actor = &reader
	request("/admin/media?page=2", 403)
	data = request("/account/oidc/clients?page=2", 200)
	// Views are a private per-owner slice, not the administrator's collection.
	if data["navigation"].(pageNavigation).Next != "" || reflect.ValueOf(data["clients"]).Len() != 8 {
		t.Fatal("unexpected third app page")
	}
	actor = &admin
	data = request("/account/oidc/clients?page=2", 200)
	if data["navigation"].(pageNavigation).Next != "" || reflect.ValueOf(data["clients"]).Len() != 0 {
		t.Fatal("owner filter lost")
	}
	data = request("/account?passkeys_page=2", 200)
	if len(data["passkeys"].([]model.Passkey)) != 2 {
		t.Fatal("passkeys not paginated")
	}
	request("/account?passkeys_page=1001", 400)
	// Empty media libraries must still provide a way back from a stale page.
	data = request("/admin/media?page=2", 200)
	if nav := data["navigation"].(pageNavigation); nav.Next != "" || nav.Previous == "" {
		t.Fatal("invalid empty media navigation")
	}
}
