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
- **Revision History** — Every edit is tracked and searchable
  - Mark an edit as "Major Update" to bump the version
  - Browse versions via `/v*` suffix (e.g. `/my-article/v1`)
  - Both old and new versions appear in search results
- **Microblogging (Topics)** — Twitter/Weibo-style short posts with comments
  - Add the `Topic` tag when publishing — title and slug auto-fill if left empty
- **RSS Auto-discovery** — Paste any blog URL into an RSS reader to discover feeds automatically
- **Theme System** — Independent frontend and backend themes, hot-swappable from admin UI
- **i18n** — Multi-language support with theme-level translation overrides

## Quick Start

### Docker (Recommended)

```yaml
version: '3.3'

services:
  db:
    image: postgres:13-alpine
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
docker-compose up -d
```

### Directory Structure

```
blog-data/
├── conf.yml    # Configuration (see data/conf.yml.example)
├── bleve/      # Full-text search index
├── upload/     # Uploaded files
└── logo.png    # Custom logo (optional)
```

### Default Credentials

Admin panel: `/admin`
Email: `hi@example.com`
Password: `123456`

## Accounts and OIDC

Existing installations seed the first administrator from `user.email`, `user.nickname`, and the existing bcrypt `user.password` in `data/conf.yml`. Replace the sample/default password before exposing a new installation to the Internet. Keep these values until the first successful migration. After that, login sessions and accounts live in PostgreSQL; the old config token is not accepted. Sign in at `/admin/login`, register at `/admin/register`, and manage your identity/passkeys at `/account`. New email/password accounts must confirm their email (one-hour link) before login; registration requires SMTP. Administrators assign roles at `/admin/users`: editors can publish and manage their own articles, while ordinary users cannot access publishing routes. Existing articles are assigned to the seeded administrator.

Set `site.domain` to the exact public host (include the port if necessary), and serve production traffic over HTTPS. Configure SMTP with `email.host`, `email.port`, `email.user`, `email.pass`, and `email.ssl`. Optional upstream login in `data/conf.yml`:

```yaml
auth:
  github: {client_id: "", client_secret: ""}
  google: {client_id: "", client_secret: ""}
  oidc: {issuer: "https://identity.example.com", client_id: "", client_secret: ""}
  webauthn: {rp_id: "blog.example.com", origin: "https://blog.example.com"}
```

Register the callback `https://<site.domain>/auth/{github,google,oidc}/callback` at each enabled upstream provider. A verified upstream email is required; if that email already exists locally, sign in first and explicitly link the provider from `/account`. Passkeys require a secure origin and matching RP ID (localhost is supported for development). Unconfigured providers are disabled.

Solitudes also acts as an OIDC provider. Register downstream clients in `/admin/oidc/clients`, then point them to `https://<site.domain>/.well-known/openid-configuration`. Use authorization code with PKCE S256 and an exact registered callback URI; a public client has no secret, while a confidential client's secret is displayed only on creation. Users must consent to each authorization. Disabling a client revokes its access and refresh tokens; already issued ID tokens remain cryptographically valid until expiry. Signing-key rotation keeps old public keys available for that interval. The PostgreSQL backup includes OIDC signing keys and client credentials: protect it accordingly. The issuer is unavailable until `site.domain` and the database are configured; restart after changing the domain.

## Theme System

Solitudes supports independent frontend and backend themes.

### Theme Directory Layout

```
resource/themes/
├── site/<theme_name>/    # Frontend themes
└── admin/<theme_name>/   # Backend themes
```

Each theme requires a `metadata.json`:

```json
{
  "id": "theme_id",
  "name": "Theme Name",
  "author": "Author",
  "version": "1.0",
  "description": "Theme Description",
  "link": "https://link.to.theme",
  "preview": "/static/images/preview.png"
}
```

Switch themes from **Admin > System Settings**.

## Development

**Prerequisites**: Go 1.24+, PostgreSQL

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
# and local SMTP catcher, testing cactus/folio × default/glacie.
SOLITUDES_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/solitudes_test?sslmode=disable' \
  go test -tags 'e2e postgres_test' ./router -run TestBrowserThemeMatrix -count=1 -v

# Build
go build -o solitudes cmd/web/main.go
```

The browser matrix covers shared core flows (registration and email verification,
login, search, comments, publishing, role changes, account password changes,
OIDC client management and Folio's theme toggle). It is not exhaustive coverage
of every feature: external OAuth providers, WebAuthn authenticator ceremonies,
uploads, every settings page and error path still need dedicated E2E tests.

## Credits

- Full-text search — [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Markdown engine — [88250/lute](https://github.com/88250/lute)
- Markdown editor — [Vanessa219/Vditor](https://github.com/Vanessa219/vditor)
- Cactus theme — [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)

## License

[AGPL-3.0](LICENSE)
