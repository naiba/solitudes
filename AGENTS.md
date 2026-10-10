# Solitudes project conventions

- Theme templates: `resource/themes/{site|admin}/{theme}/templates`; assets must use `/static/{kind}/{theme}/{path}`. Theme translation catalogs must be self-contained; run `TestTranslationCatalogContracts` to check missing and unused keys.
- Dependencies are registered in `solitudes.go` using `dig`. Never bypass article visibility/fragment filtering for feeds, excerpts, indexes or machine exports. Machine representations always use anonymous permissions, regardless of session.
- Article URLs use mutable slugs; machine Markdown uses stable article IDs. Outbound navigation, including machine-readable content, must use `/r/go`; do not create direct-link exceptions for crawlers. Redirect targets stay client-only: never reflect arbitrary request targets in server-rendered HTML or relax `/r/` crawler blocking for GEO.
- Cross-theme localStorage keys: `solitudes_theme` (`auto`, `light`, `dark`), `solitudes_cm_nickname`, `solitudes_cm_email`, `solitudes_cm_website`.
- PostgreSQL integration tests need a dedicated `SOLITUDES_TEST_POSTGRES_DSN`; tests create isolated schemas. Run `go test -p 2 -tags postgres_test ./...`.
- Browser tests install via `bun install --cwd e2e`; install Playwright Chromium. Full matrix: `go test -p 2 -tags 'e2e postgres_test' ./router -run TestBrowserThemeMatrix -count=1 -v -timeout 20m`. It starts isolated servers and SMTP; do not launch a separate application. Also run the standalone `e2e/article-images.spec.ts` suite and TypeScript typecheck.
