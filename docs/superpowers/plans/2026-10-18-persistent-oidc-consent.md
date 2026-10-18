# Persistent OIDC Consent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve explicitly approved OAuth permissions across token expiration without broadening newly issued tokens.

**Architecture:** Add one durable account/client consent row, merge scopes only on explicit approval under the existing client lock, and query consent instead of live credentials. Introduce an atomic, schema-local one-time migration for live legacy credentials and update application revocation and listings.

**Tech Stack:** Go, GORM, PostgreSQL, existing HTML themes, Playwright/Bun.

**Spec:** `docs/superpowers/specs/2026-10-18-persistent-oidc-consent-design.md`

## Global Constraints
- Consent accumulates; token scopes remain request-specific; lifetimes unchanged.
- No token/history fallback during runtime consent decisions.
- Explicit revocation removes consent and credentials atomically; logout/token revocation do not remove consent.
- Use existing client lock order; maintain authentication freshness, account/client isolation, disabled checks and audit rollback.
- Only live access/refresh credentials are migrated once; empty migrations are marked complete too.
- Both themes have self-contained translation catalogs; no unused keys.
- Dedicated test PostgreSQL only; no skipped tests accepted as validation.
- Final commits end exactly once with `Co-authored-by: naiba/CloudCode <hi+cloudcode@nai.ba>`; no other AI coauthor.

## Review Focus
- Migration rerun after revocation or empty initial migration must not resurrect grants (Task 1).
- Concurrent explicit approvals must not lose scopes; revoke-winning stale requests cannot restore consent (Task 2).
- Smaller requests and refreshes must neither enlarge tokens nor shrink stored consent (Task 2).
- Audit/storage failures roll back consent approval and application revocation (Tasks 2/3).
- Consent-only applications remain visible and revocable after token cleanup; other accounts remain isolated (Task 3).

### Task 1: Durable consent model and legacy migration
**Files:** `internal/model/oidc.go`, `internal/model/database_schema.go`, new `internal/model/oidc_consent_migration.go`, `solitudes.go`, `internal/model/database_postgres_test.go`, new `internal/model/oidc_consent_migration_postgres_test.go`, test schema fixtures.
**Interfaces:** `model.OIDCConsent{AccountID, ClientID, Scopes, CreatedAt, UpdatedAt}` with composite primary key and client index; `model.MigrateOIDCConsent(db *gorm.DB) error` called after model/schema creation. Durable migration marker must be independent of audit retention.
- [ ] Add failing migration tests asserting live access+refresh scopes union, expired/equal-cutoff exclusion, no request/history import, empty completion, rerun after deletion no resurrection, transaction rollback/retry and schema isolation.
- [ ] Run targeted PostgreSQL tests and record expected failures before implementing.
- [ ] Implement model, migration marker and single-transaction import with schema-local search_path and migration serialization; normalize scopes by fields, deduplication and sorting; preserve tokens.
- [ ] Add account/client cascade constraints and all applicable AutoMigrate/manual SQLite fixture registrations, without adding consent to expiration cleanup.
- [ ] Run model/initialization tests to green.

### Task 2: Explicit approval and durable reuse
**Files:** `router/oidc_consent.go`, `router/oidc_consent_postgres_test.go`, `router/identity_integration_test.go`, related OIDC fixtures/tests.
**Interfaces:** Keep `oidcScopesGranted(db *gorm.DB, clientID, accountID string, scopes []string, now time.Time) (bool,error)` initially or remove unused time consistently; consult only model.OIDCConsent. Explicit persistence lives within `completeOIDCConsent` transaction after authentication checks and before approving request.
- [ ] Write failing tests for consent reuse after all tokens expire/are removed; subset preserves historical consent; extra scope requires consent; explicit approvals accumulate into one row; declined/failed approvals do not mutate; token scopes remain subset; concurrent approval and revoke serialization.
- [ ] Record red failures, then implement minimal merge-on-explicit-approval and consent-only lookup with explanatory comments at the former expiry-coupled logic.
- [ ] Update prior token-backed fixtures to create consent explicitly; retain negative token-only regression. Keep prompt, session, hint, max_age and disabled/verified tests substantive.
- [ ] Verify targeted tests green, including token revocation/logout preserving consent and storage failure rollback.

### Task 3: Revocation and durable authorization listings
**Files:** `router/oidc_authorizations.go`, `router/oidc_authorizations_postgres_test.go`, `resource/themes/site/{cactus,folio}/templates/account_authorizations.html`, corresponding `translations/{en,zh}.json`, `e2e/theme-contract.spec.ts`.
**Interfaces:** `authorizedApplicationsQuery` now reads consent rows and active-client metadata; revocation ownership includes consent. Separate credential deletion from application-consent deletion to preserve logout/token semantics.
- [ ] Add failing tests for consent-only listing/revocation, cumulative scopes, user isolation, audit rollback, application/account deletion and expired-token cleanup preservation.
- [ ] Implement application revocation/deletion cleanup and consent-driven list; remove ExpiresAt display/model if unused.
- [ ] Replace credential expiry copy with durable-until-revoked copy in both themes and all four catalogs; strengthen browser assertions for persistent listing, narrowed token request, and revoke/re-consent.
- [ ] Run targeted authorization, translation, database constraint and browser tests green.

### Task 4: Full local acceptance, independent review, push and CI
- [ ] Run `go test -p 2 ./...` and `go test -p 2 -tags postgres_test ./...` with configured dedicated PostgreSQL; confirm no relevant skips.
- [ ] Run `e2e/node_modules/.bin/tsc --noEmit -p e2e/tsconfig.json`, standalone `article-images.spec.ts`, and `go test -p 2 -tags 'e2e postgres_test' ./router -run TestBrowserThemeMatrix -count=1 -v -timeout 20m` with matching Chromium.
- [ ] Independent review of whole diff, especially security boundaries and meaningful test assertions; fix verified findings with red/green and repeat impacted full validation.
- [ ] Inspect final diff, commit feature branch with required attribution, push origin and watch GitHub Actions for exact HEAD until terminal success. If CI fails, diagnose and reproduce locally before another push.
