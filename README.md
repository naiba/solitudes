# Solitudes

![Build Status](https://github.com/naiba/solitudes/workflows/Build%20Docker%20Image/badge.svg)

📖 [中文文档](README_zh.md)

A blog engine built with **Go** and **Fiber**, featuring full-text search, article versioning, microblogging, and a themeable frontend/backend.

## Features

- **Full-text Search** — CJK-aware search that handles Simplified/Traditional Chinese and case-insensitive English
- **Books / Series** — Organize articles into books with nested chapters
  - Mark an article as a "Book" to use it as a cover page
  - Assign articles to a book by filling in the cover's UUID
  - Nest books for multi-level chapter structures
- **Revision History** — Major revisions remain available to authorized readers
  - Mark an edit as "Major Update" to bump the version
  - Browse versions via `/v*` suffix (e.g. `/my-article/v1`)
  - Search returns the current article only; historical bodies are not indexed
- **Microblogging (Topics)** — Twitter/Weibo-style short posts with comments
  - Add the `Topic` tag when publishing — title and slug auto-fill if left empty
- **RSS Auto-discovery** — Paste any blog URL into an RSS reader to discover feeds automatically
- **Theme System** — Independent frontend and administration themes; one modern admin theme bundled
- **i18n** — Multi-language support with theme-level translation overrides

## Quick Start

### Docker (Recommended)

```yaml
services:
  db:
    image: postgres:16-alpine
    volumes:
      - ./postgres-data:/var/lib/postgresql/data
    restart: always
    environment:
      POSTGRES_PASSWORD: thisispassword
      POSTGRES_USER: solitudes
      POSTGRES_DB: solitudes

  solitudes:
    depends_on:
      - db
    image: ghcr.io/naiba/solitudes:latest
    ports:
      - "8080:8080"
    restart: always
    volumes:
      - ./blog-data:/solitudes/data
```

```bash
docker compose up -d db
# Wait until PostgreSQL is ready. Configure blog-data/conf.yml first.
docker compose run --rm solitudes /solitudes/solitudes init-admin --email you@example.com --nickname Administrator
# Store the generated password securely: it is displayed once, not logged to a file.
docker compose up -d solitudes
```

### Directory Structure

```
blog-data/
├── conf.yml    # Configuration (see data/conf.yml.example)
├── bleve/      # Full-text search index
├── upload/     # Uploaded files
└── logo.png    # Custom logo (optional)
```

### Administrator setup

There is no default account or password. For a native installation, run `./solitudes init-admin --email you@example.com --nickname Administrator` from the project directory before starting the server. The local command creates the database schema and a verified administrator, prints a cryptographically random password once, and stores only its bcrypt hash in PostgreSQL. It works only when the accounts table is empty; repeated/concurrent commands never overwrite credentials or promote registered users. Run it in a private terminal and do not redirect its output to shared logs.

## Accounts and OIDC

Accounts, passwords, sessions and roles live exclusively in PostgreSQL. Sign in at `/login` and change the initial password in `/account`; password changes do not modify configuration and survive restarts. Public registration at `/register` creates ordinary users, requires SMTP, and requires email verification (one-hour link) before login. Administrators assign roles at `/admin/users`: editors publish/manage their own articles; ordinary users cannot access publishing routes. Back up PostgreSQL to preserve identities and credentials. See [configuration and compatibility policy](docs/configuration.md) before deploying over an older installation.

The front-site `/readers/` reader circle lists all verified, enabled accounts by default, including existing accounts; anyone can leave or rejoin in their public-profile settings at `/account`. The homepage highlights the four newest members and up to five recent public comments (including guest comments). The directory shows a recent activity feed and ranks active members using public posts and non-spam comments from the last 30 days; emails, private posts, and spam never appear. RSS, Atom, and JSON Feed credit each public post's actual author, rather than a site-wide identity; feed metadata never exposes the administrator's email. Editors and administrators reach article management and publishing through their account center.

Set `site.domain` to the exact public host (include the port if necessary), and serve production traffic over HTTPS. Configure SMTP with `email.host`, `email.port`, `email.user`, `email.pass`, and `email.ssl`. Administrators can configure GitHub, Google, and upstream OIDC sign-in under `/admin/auth/providers` (secrets are never displayed again). The equivalent configuration in `data/conf.yml` is:

```yaml
auth:
  github: {client_id: "", client_secret: ""}
  google: {client_id: "", client_secret: ""}
  oidc: {issuer: "https://identity.example.com", client_id: "", client_secret: ""}
  webauthn: {rp_id: "blog.example.com", origin: "https://blog.example.com"}
```

Register the callback `https://<site.domain>/auth/{github,google,oidc}/callback` at each enabled upstream provider. A verified upstream email is required; if that email already exists locally, sign in first and explicitly link the provider from `/account`. Passkeys require a secure origin and matching RP ID (localhost is supported for development). Unconfigured providers are disabled.

Solitudes also acts as an OAuth 2.1-style authorization server and OIDC provider: **every verified blog account**, including ordinary users and editors, can sign in to registered external applications. The front-site navigation links to `/account`, where users can change their password, add passkeys, link/unlink upstream sign-in providers, and create or disable their own downstream clients under `/account/oidc/clients`; administrators can manage every client under `/admin/oidc/clients`. External applications can use `https://<site.domain>/.well-known/openid-configuration` (OIDC) or `https://<site.domain>/.well-known/oauth-authorization-server` (OAuth metadata). This is separate from the upstream sign-in providers above. Supported grants are authorization code (mandatory PKCE S256) and rotating refresh tokens; implicit/password grants are not supported. Register an exact HTTPS callback URI (localhost HTTP is allowed). Public clients authenticate without a secret; confidential clients use `client_secret_basic`, and their secret is displayed only on creation. Request `openid` for an ID token; pure OAuth access tokens can be requested without it. Users must consent to each authorization. Disabling a client revokes its access and refresh tokens; already issued ID tokens remain cryptographically valid until expiry. Signing-key rotation keeps old public keys available for that interval. The PostgreSQL backup includes OIDC signing keys and client credentials: protect it accordingly. The issuer is unavailable until `site.domain` and the database are configured; restart after changing the domain.

New clients must provide an application name, optional description, an HTTPS homepage (localhost HTTP is allowed for development), and exact redirect URIs; post-logout redirect URIs are optional and must also be registered explicitly. RP-initiated logout revokes the application's tokens, but does not sign the user out of their blog account. Owners and administrators can edit client details and redirect URIs later. During an external authorization, both the sign-in and consent screens show the registered application details, a link to its website, and a link to its creator's public blog profile. Clients with missing owners or missing/invalid homepage metadata cannot authorize or authenticate. Stored URLs are still validated before display.

The account center's `/account/oidc/authorizations` derives applications directly from your valid access tokens, refresh tokens and approved requests awaiting exchange. It shows owners, effective scopes and latest credential expiry, with per-application revocation. No separate long-lived consent records are stored. Entries disappear when all credentials expire; a valid refresh token alone still keeps the application listed. Revocation invalidates your application tokens and pending authorizations; it cannot erase data already copied by an application, invalidate an ID token's offline signature, or terminate the application's own session. Owners and administrators can permanently delete clients, clearing every user's pending requests and tokens while preserving security audits and accumulated login statistics. Disabling a client also clears tokens and pending requests.

### Administration statistics and security audit

Administrators can search users by name/email and role at `/admin/users`, inspect their application counts, and navigate from a user to their registered clients. `/admin/oidc/clients` supports name, owner and status filters, login counts, distinct login users and last-login times. Client details show recent login users. The default administration workspace provides paginated views (25 records per page).

`/admin/audit` is an administrator-only, read-only event log, filterable by action, outcome, user, client, IP, request ID and UTC dates. It records site sign-ins, application authorization/token issuance, client and role changes, account-security operations, administrative writes and rejected/failed requests. OAuth errors retain only standardized error codes. Passwords, secrets, tokens, cookies, bodies and query strings are not recorded. Source IPs follow the configured trusted-proxy policy; never blindly trust forwarded headers.

Statistics begin when auditing is first enabled; unverifiable historical logins are not backfilled. One authorization-code token issuance counts as one application login. Consent visits, refreshes and failures do not count, and revocation/logout/client disable do not remove history. Client changes, role changes and credential issuance commit atomically with their audit events; audit failure rolls them back. Other audit-write failures emit a secret-free `audit_write_failed` server log, correlated with the response's `X-Request-ID`. There is no automatic audit retention purge: monitor database growth and back it up. The website offers no audit editing/deletion, but the ledger is not tamper-proof against database administrators; important deployments should also archive to a protected external logging system.

## Theme System

Solitudes supports independently switchable frontend and administration themes. Only the modern default administration theme is bundled; additional trusted themes can be installed without changing this architecture.

### Theme Directory Layout

```
resource/themes/
├── site/<theme_name>/    # Frontend themes
└── admin/<theme_name>/   # Admin themes (default is bundled)
```

Both kinds use the same resource layout:

```text
<theme_name>/
├── metadata.json
├── screenshot.png       # Actual preview image, preferably 16:10
├── templates/
├── static/
└── translations/
    ├── en.json
    └── zh.json
```

Both kinds share the same `metadata.json` schema; `id` must match the directory name:

```json
{
  "id": "theme_id",
  "name": "Theme Name",
  "author": "Author",
  "version": "1.0",
  "description": "Theme Description",
  "link": "https://link.to.theme",
  "config": {}
}
```

Select both kinds using the same screenshot cards in **Admin > System Settings**, then save to apply them independently. Previews come from `screenshot.png` in each theme root, not a `preview` metadata field. Missing screenshots show a local placeholder, without third-party image requests. Installed administration themes follow the same layout and are discovered automatically. Theme files are trusted server-side code: only install themes you trust.

All frontend themes use the same [model queries and template primitives](docs/theme-data.md) (Chinese), composing ordering, limits and random sampling themselves. Queries never branch on theme names; [article and partial-content access](docs/content-access.md) is enforced server-side.

## Development

**Prerequisites**: Go 1.26+, PostgreSQL

See [database design and maintenance](docs/database.md) (Chinese) for schema, indexes, connection pool limits and audit retention.

```bash
git clone https://github.com/naiba/solitudes.git
cd solitudes

# Install dependencies
go mod tidy

# Start dev server
go run cmd/web/main.go

# Run tests
go test ./...

# Integration tests (use a dedicated PostgreSQL database; each run creates
# and removes only its own temporary schema). SMTP is caught locally in-test.
SOLITUDES_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/solitudes_test?sslmode=disable' \
  go test -tags postgres_test ./...

# Browser matrix: install dependencies in e2e/ (`bun install`), install
# Playwright Chromium (`bunx playwright install chromium`), then run against
# a dedicated PostgreSQL test database. This starts an isolated HTTP server
# and local SMTP catcher, testing cactus/folio × default.
SOLITUDES_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/solitudes_test?sslmode=disable' \
  go test -tags 'e2e postgres_test' ./router -run TestBrowserThemeMatrix -count=1 -v -timeout 20m

# Build
go build -o solitudes cmd/web/main.go
```

The browser matrix covers shared core flows (registration and email verification,
login, search, comments, publishing, role changes, account password changes,
OIDC client management and ownership, external OIDC login for users/editors/admins, sign-in
provider configuration and Folio's theme toggle). Pull requests and pushes to
`master` run the PostgreSQL and Chromium matrix in CI before Docker images are
published. It is not exhaustive coverage
of every feature: WebAuthn registration/removal uses Chromium's virtual authenticator;
real external OAuth providers and physical authenticators are not exercised.
Uploads and every settings/error path still need dedicated browser coverage.

For opt-in screenshots, set `SOLITUDES_VISUAL_AUDIT=1` and
`SOLITUDES_VISUAL_DIR` to a temporary directory with the same browser-matrix
command (use `-timeout 20m`). The isolated visual suite replaces the old
localhost screenshot scripts; it never overwrites theme preview images or seeds
a running blog. Review and remove generated screenshots afterward.

## Credits

- Full-text search — [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Markdown engine — [88250/lute](https://github.com/88250/lute)
- Markdown editor — [Vanessa219/Vditor](https://github.com/Vanessa219/vditor)
- Cactus theme — [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)

## License

[AGPL-3.0](LICENSE)
