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

# Build
go build -o solitudes cmd/web/main.go
```

## Credits

- Full-text search — [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Markdown engine — [88250/lute](https://github.com/88250/lute)
- Markdown editor — [Vanessa219/Vditor](https://github.com/Vanessa219/vditor)
- Cactus theme — [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)

## License

[AGPL-3.0](LICENSE)
