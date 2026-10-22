package router

import (
	"fmt"
	"testing"
)

func TestRedirectManagementRejectsCurrentRevisionLoop(t *testing.T) {
	app, db := redirectTestApplication(t)
	if err := db.Exec("INSERT INTO articles(id,slug,title,visibility,version) VALUES ('revision','revision-story','Versioned','public',1)").Error; err != nil {
		t.Fatal(err)
	}
	status, _, body := redirectRequest(t, app, "POST", "/admin/redirects", `{"source_path":"/revision-story","target_path":"/revision-story/v1"}`, "admin")
	if status != 409 {
		t.Fatalf("current revision loop accepted: %d %s", status, body)
	}
}

func TestRedirectDeletionCannotActivateAnImplicitRevisionLoop(t *testing.T) {
	app, db := redirectTestApplication(t)
	if err := db.Exec("INSERT INTO articles(id,slug,title,visibility,version) VALUES ('revision','revision-story','Versioned','public',1)").Error; err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"source_path":"/revision-story/v1","target_path":"/destination"}`,
		`{"source_path":"/revision-story","target_path":"/revision-story/v1"}`,
	} {
		status, _, response := redirectRequest(t, app, "POST", "/admin/redirects", body, "admin")
		if status != 201 {
			t.Fatalf("valid graph: %d %s", status, response)
		}
	}
	status, _, _ := redirectRequest(t, app, "DELETE", "/admin/redirects", `{"ids":[1]}`, "admin")
	if status != 409 {
		t.Fatalf("deletion activated a revision loop: %d", status)
	}
	status, headers, _ := redirectRequest(t, app, "GET", "/revision-story", "", "")
	if status != 301 || headers.Get("Location") != "/destination" {
		t.Fatal("rejected deletion changed the existing graph")
	}
}

func TestRedirectManagementRejectsChainGrowthWithoutBreakingExistingSources(t *testing.T) {
	app, _ := redirectTestApplication(t)
	for i := 0; i < 32; i++ {
		body := fmt.Sprintf(`{"source_path":"/archive-chain-%d","target_path":"/archive-chain-%d"}`, i, i+1)
		status, _, response := redirectRequest(t, app, "POST", "/admin/redirects", body, "admin")
		if status != 201 {
			t.Fatalf("chain link %d: %d %s", i, status, response)
		}
	}
	status, header, _ := redirectRequest(t, app, "GET", "/archive-chain-0", "", "")
	if status != 301 || header.Get("Location") != "/archive-chain-32" {
		t.Fatalf("accepted chain no longer resolves: %d %s", status, header.Get("Location"))
	}
	status, _, _ = redirectRequest(t, app, "POST", "/admin/redirects", `{"source_path":"/archive-chain-32","target_path":"/archive-chain-33"}`, "admin")
	if status != 409 {
		t.Fatalf("growth breaking upstream should fail atomically: %d", status)
	}
	status, header, _ = redirectRequest(t, app, "GET", "/archive-chain-0", "", "")
	if status != 301 || header.Get("Location") != "/archive-chain-32" {
		t.Fatal("rejected update broke an existing source")
	}
}
