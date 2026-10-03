package router

import (
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
)

type databaseQueryPlan struct {
	NodeType    string              `json:"Node Type"`
	SubplanName string              `json:"Subplan Name"`
	IndexName   string              `json:"Index Name"`
	ActualLoops float64             `json:"Actual Loops"`
	ActualRows  float64             `json:"Actual Rows"`
	Plans       []databaseQueryPlan `json:"Plans"`
}

func TestPostgresDatabaseListQueryPlans(t *testing.T) {
	db, _, _ := auditTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.ExternalIdentity{}, &model.Passkey{}, &model.PasskeyCeremony{}, &model.OAuthAttempt{}, &model.OIDCAuthRequest{}, &model.FeedVisit{}); err != nil {
		t.Fatal(err)
	}
	if err := model.MigrateDatabasePolicy(db); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO accounts(email,nickname,role,created_at) SELECT 'plan-'||n||'@test','Plan '||n,'user',now()-n*interval '1 second' FROM generate_series(1,5000) n`,
		`INSERT INTO o_id_c_clients(id,owner_id,name,redirect_uris_json,created_at) SELECT 'plan-'||row_number() OVER (ORDER BY email),id,nickname,'[]',created_at FROM accounts WHERE email LIKE 'plan-%'`,
		`INSERT INTO audit_events(id,created_at,action,outcome,client_id,actor_id) SELECT 'plan-'||n,now()-n*interval '1 second',CASE WHEN n%2=0 THEN 'oidc.login' ELSE 'session.login' END,'success',c.id,c.owner_id::text FROM generate_series(1,200000) n JOIN o_id_c_clients c ON c.id='plan-'||((n-1)%5000+1)`,
		`INSERT INTO articles(slug,title,created_at,tags) SELECT 'plan-'||n,'Plan',now()-n*interval '1 second',ARRAY[CASE WHEN n%1000=0 THEN 'rare' ELSE 'common' END] FROM generate_series(1,10000) n`,
		`UPDATE articles SET book_refer=(SELECT id FROM articles WHERE slug='plan-10000') WHERE slug IN (SELECT 'plan-'||n FROM generate_series(1,50) n)`,
		`ANALYZE accounts`, `ANALYZE o_id_c_clients`, `ANALYZE audit_events`, `ANALYZE articles`, `ANALYZE login_summaries`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	explain := func(name, sql string, vars []interface{}, wantedIndex string, bounded bool) {
		t.Helper()
		var raw string
		if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, vars...).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		var results []struct {
			Plan          databaseQueryPlan
			ExecutionTime float64 `json:"Execution Time"`
		}
		if err := json.Unmarshal([]byte(raw), &results); err != nil || len(results) != 1 {
			t.Fatalf("plan: %v %s", err, raw)
		}
		found := false
		var visit func(databaseQueryPlan)
		visit = func(plan databaseQueryPlan) {
			if plan.IndexName == wantedIndex {
				found = true
			}
			if bounded && strings.HasPrefix(plan.SubplanName, "SubPlan") && plan.ActualLoops > identityPageSize {
				t.Errorf("%s statistics evaluated outside page: %s loops=%v", name, plan.SubplanName, plan.ActualLoops)
			}
			for _, child := range plan.Plans {
				visit(child)
			}
		}
		visit(results[0].Plan)
		if !found {
			t.Errorf("%s did not use %s: %s", name, wantedIndex, raw)
		}
		t.Logf("%s: %.3f ms, index=%s", name, results[0].ExecutionTime, wantedIndex)
	}
	for _, page := range []int{1, 5} {
		var clients []adminClientView
		query := adminClientsPageQuery(db.Table("o_id_c_clients AS c"), page).Session(&gorm.Session{DryRun: true}).Find(&clients)
		explain("application page", query.Statement.SQL.String(), query.Statement.Vars, "idx_audit_oidc_client_actor", true)
		var users []adminUserView
		query = adminUsersPageQuery(db.Table("accounts AS a"), page).Session(&gorm.Session{DryRun: true}).Find(&users)
		explain("user page", query.Statement.SQL.String(), query.Statement.Vars, "idx_audit_session_actor", true)
	}
	explain("article slug", `SELECT id FROM articles WHERE slug='plan-777'`, nil, "idx_articles_slug", false)
	explain("article tags", `SELECT id FROM articles WHERE tags @> ARRAY['rare']::varchar[]`, nil, "idx_articles_tags_gin", false)
	explain("article page", `SELECT id FROM articles ORDER BY created_at DESC,id DESC LIMIT 25`, nil, "idx_articles_page", false)
	explain("book children", `SELECT id FROM articles WHERE book_refer=(SELECT id FROM articles WHERE slug='plan-10000') ORDER BY created_at,id`, nil, "idx_articles_book_refer", false)
	var storage struct{ Tables, Indexes int64 }
	if err := db.Raw(`SELECT SUM(pg_table_size(c.oid)) AS tables, SUM(pg_indexes_size(c.oid)) AS indexes
	 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
	 WHERE n.nspname=current_schema() AND c.relkind='r'`).Scan(&storage).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("isolated fixture storage: tables %.2f MiB, indexes %.2f MiB", float64(storage.Tables)/(1<<20), float64(storage.Indexes)/(1<<20))
}
