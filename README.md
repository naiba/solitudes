# Solitudes

![Build status](https://github.com/naiba/solitudes/actions/workflows/docker.yml/badge.svg)

[中文](README_zh.md)

A multi-user blog built with Go and Fiber, with full-text search, nested series, revisions, short posts, comments, a reader circle, feeds and switchable themes. It also serves as an OIDC provider for other applications.

## Installation

Save this as `compose.yml` and replace the example database password:

```yaml
services:
  db:
    image: postgres:16-alpine
    restart: always
    environment:
      POSTGRES_USER: solitudes
      POSTGRES_PASSWORD: change-this-password
      POSTGRES_DB: solitudes
    volumes:
      - ./postgres-data:/var/lib/postgresql/data
  solitudes:
    image: ghcr.io/naiba/solitudes:latest
    restart: always
    depends_on:
      - db
    ports:
      - "8080:8080"
    volumes:
      - ./blog-data:/solitudes/data
```

Create `blog-data/conf.yml` using the [example](data/conf.yml.example). Set the database host to `db` and match the credentials above. Set `site.domain` to your public host without a scheme. Use an HTTPS reverse proxy in production.

```bash
docker compose up -d db
# Confirm PostgreSQL is ready before initializing the administrator
docker compose exec db pg_isready -U solitudes -d solitudes
docker compose run --rm solitudes /solitudes/solitudes init-admin --email you@example.com --nickname Administrator
docker compose up -d solitudes
```

Sign in at `http://localhost:8080/login`. There is no default account or password. Initialization requires an empty accounts table and displays a random password once; save it securely and change it in your account center.

`blog-data/` holds configuration, uploads and the search index. Accounts, passwords and articles live in PostgreSQL. Back up both the directory and database, protecting the credentials and signing keys they contain.

Single-user and old access-policy data are not migrated automatically: back up and validate before upgrading. Unknown configuration fields are rejected; remove obsolete top-level `user` and `configfilepath`, but retain SMTP's `email.user`.

## Usage

### Accounts and publishing

- Register at `/register` and verify your email. Configure SMTP through `email.host`, `email.port`, `email.user`, `email.pass` and `email.ssl`.
- Use `/account` to edit your profile/password, link sign-in methods and manage applications. Passwords are stored in the database, not configuration.
- Administrators manage the site; editors publish and manage their own articles; ordinary members cannot publish or edit. The account center links to the writing workspace.
- Mark an article as a series cover, then assign its UUID to other articles to add chapters. Series can be nested.
- Add the `Topic` tag for short posts; titles and slugs can be generated automatically. The dashboard also offers quick publishing.
- Select “Major Update” when editing to retain a new revision. Historical URLs look like `/my-article/v1`; search returns only the current version.
- Verified users appear in the reader circle by default and can leave in their account settings. Emails remain private.

### Content access

Articles can be public, members-only, editors-only or restricted to the author and administrators. Vditor's restricted-content tool inserts `access:members`, `access:editors` or `access:private` Markdown fences, filtered server-side. Editors cannot add or modify executable code.

Historical versions inherit the latest article's overall visibility but keep their own fragment rules: text hidden later may remain visible in older versions. Uploads are public, and hiding a link does not restrict downloads. Do not upload sensitive attachments or publish restricted raw Markdown.

### Sign in to the blog with another provider

Configure GitHub, Google or upstream OIDC at `/admin/auth/providers`. Register the callback `https://<host>/auth/{github,google,oidc}/callback`. If an account already uses that email, sign in first and link the provider from the account center.

Passkeys require HTTPS. Set `auth.webauthn.rp_id` to the site's host and `auth.webauthn.origin` to its full HTTPS address.

### Sign in to other applications with your blog account

Every verified user can create clients at `/account/oidc/clients`. Provide an application name, homepage and exact redirect URI. Use HTTPS in production; localhost HTTP is allowed for development.

- OIDC discovery: `https://<host>/.well-known/openid-configuration`.
- OAuth metadata: `https://<host>/.well-known/oauth-authorization-server`.
- Use authorization code + PKCE S256. Rotating refresh tokens are supported; implicit/password grants are not. Request `openid` for an ID token.
- Public clients need no secret; confidential clients use `client_secret_basic`. Secrets are displayed only on creation.
- View and revoke current access at `/account/oidc/authorizations`. Applications disappear when all credentials expire.

Disabling or deleting an application clears its tokens and pending requests while preserving audits. Revocation cannot erase data already copied by the application, end its own sessions or immediately invalidate an ID token's offline signature. Restart after changing the site's host.

### Administration

Administrators manage users at `/admin/users`, inspect applications and login statistics at `/admin/oidc/clients`, and investigate security events at `/admin/audit`.

Audits retain logins, consent decisions, sensitive changes and failures. Routine browsing, successful token refresh/validation and intermediate authentication steps are not logged individually.

The default `audit_retention_days: 0` keeps audit details indefinitely. A positive value permanently deletes older details, retaining only aggregate login statistics. Back up or archive important audit records separately.

## Themes

Cactus and Folio site themes and one default admin theme are bundled. Switch them independently in **Admin → System Settings**.

Custom themes live in `resource/themes/{site,admin}/<name>/`, containing `metadata.json`, `screenshot.png`, `templates/`, `static/` and `translations/`. The metadata `id` must match the directory name; see [Cactus](resource/themes/site/cactus/metadata.json) for the format.

Site themes share the permission-filtered `.Queries` interface; see [query definitions](router/template_queries.go) and built-in templates for usage. Only install trusted themes. Never bypass content filtering or apply `unsafe` to user input.

## Development and tests

Requires Go 1.26+, a C/C++ compiler and PostgreSQL. Configure `data/conf.yml`, then run:

```bash
go build -o solitudes cmd/web/main.go
./solitudes init-admin --email you@example.com --nickname Administrator  # First installation only
./solitudes
```

Use a dedicated database for integration/browser tests, which start isolated services, a local SMTP catcher and temporary schemas:

```bash
go test ./...
export SOLITUDES_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/solitudes_test?sslmode=disable'
go test -tags postgres_test ./...

cd e2e
bun install --frozen-lockfile
bunx playwright install chromium
cd ..
go test -tags 'e2e postgres_test' ./router -run TestBrowserThemeMatrix -count=1 -v -timeout 20m
```

CI builds images after tests pass. E2E covers core flows in both themes, not every feature. WebAuthn uses a virtual authenticator; real upstream OAuth providers and physical authenticators require separate testing.

## Credits and license

[Bleve](https://github.com/blevesearch/bleve) · [Lute](https://github.com/88250/lute) · [Vditor](https://github.com/Vanessa219/Vditor) · [Cactus](https://github.com/probberechts/hexo-theme-cactus)

[AGPL-3.0](LICENSE)
