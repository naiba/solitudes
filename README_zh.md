# Solitudes

![构建状态](https://github.com/naiba/solitudes/workflows/Build%20Docker%20Image/badge.svg)

📖 [English](README.md)

基于 **Go** 和 **Fiber** 构建的博客引擎，支持全文搜索、文章版本管理、哔哔（微博客）以及可换肤的前后台主题。

## 特色功能

- **全文搜索** — 不分「简体/繁体」中文，「大写/小写」英文，都能搜索到
- **专栏 / 写书** — 将文章组织为专栏，支持嵌套章节
  - 发布文章时勾选「这是专栏」，该文章将作为专栏封面
  - 填入封面文章的 UUID 到「专栏 ID」，文章即归入该专栏
  - 支持套娃式多级章节结构
- **修订历史** — 所有修改记录均可浏览和搜索
  - 编辑时勾选「大更新」可升级版本号
  - 在链接后加 `/v*` 浏览历史版本（如 `/my-article/v1`）
  - 新旧版本均出现在搜索结果中
- **哔哔（Topics）** — 类微博短内容，支持评论
  - 发布时添加 `Topic` 标签即可，标题和链接可留空自动补全
- **RSS 自动发现** — 将博客任意链接粘贴到 RSS 阅读器即可自动订阅
- **主题系统** — 前台和后台主题相互独立，可在管理后台热切换
- **多语言** — 支持多语言，主题级翻译可独立覆盖

## 快速开始

### Docker（推荐）

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

### 目录结构

```
blog-data/
├── conf.yml    # 配置文件（参考 data/conf.yml.example）
├── bleve/      # 全文搜索索引
├── upload/     # 上传的文件
└── logo.png    # 自定义 logo（可选）
```

### 默认账户

管理后台：`/admin`
邮箱：`hi@example.com`
密码：`123456`

## 用户与 OIDC

已有站点升级时，会从 `data/conf.yml` 的 `user.email`、`user.nickname`、`user.password`（bcrypt 哈希）创建首位管理员，并把旧文章归其所有。新站点对外开放前务必替换示例配置中的默认密码。首次迁移完成前请保留这些配置；旧的配置文件登录令牌不再有效。普通用户可在 `/admin/register` 注册，邮件验证链接一小时内有效，未验证不能登录；注册需配置 SMTP。`/account` 可管理身份和通行密钥。管理员在 `/admin/users` 分配管理员、编辑、普通用户角色；编辑可以直接发布并管理自己的文章，普通用户不能进入写作后台。

请在 `data/conf.yml` 中设置 `site.domain` 为对外域名（必要时含端口），生产环境使用 HTTPS，并配置 `email.host`、`email.port`、`email.user`、`email.pass`、`email.ssl`。可选的上游登录配置：

```yaml
auth:
  github: {client_id: "", client_secret: ""}
  google: {client_id: "", client_secret: ""}
  oidc: {issuer: "https://identity.example.com", client_id: "", client_secret: ""}
  webauthn: {rp_id: "blog.example.com", origin: "https://blog.example.com"}
```

各上游平台的回调地址为 `https://<site.domain>/auth/{github,google,oidc}/callback`。上游必须返回已验证邮箱；若本地已有相同邮箱账号，先登录，再在 `/account` 主动绑定。通行密钥需要 HTTPS 且 RP ID 与 Origin 的主机相同（本地 localhost 开发例外）。未配置的登录方式不会启用。

Solitudes 同时提供 OIDC 服务：管理员在 `/admin/oidc/clients` 注册下游应用，应用使用 `https://<site.domain>/.well-known/openid-configuration` 发现端点。仅允许精确匹配的回调 URI、授权码及 PKCE S256，并要求用户授权。公开客户端无密码；机密客户端密码仅创建时显示一次。禁用客户端会撤销 Access Token 和 Refresh Token；已签发的 ID Token 在过期前仍可通过签名验证，密钥轮换时会保留旧公钥。数据库备份包含 OIDC 签名私钥和客户端凭据，务必妥善保护；未设置 `site.domain` 或数据库不可用时，OIDC 服务不可用。变更域名后需重启服务。

## 主题系统

Solitudes 支持独立的前台和后台主题。

### 主题目录结构

```
resource/themes/
├── site/<theme_name>/    # 前台主题
└── admin/<theme_name>/   # 后台主题
```

每个主题目录下需要一个 `metadata.json`：

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

在 **管理后台 > 系统设置** 中切换主题。

## 开发

**前置依赖**：Go 1.24+、PostgreSQL

```bash
git clone https://github.com/naiba/solitudes.git
cd solitudes

# 安装依赖
go mod tidy

# 启动开发服务器
go run cmd/web/main.go

# 运行测试
go test ./...

# 集成测试：使用专用 PostgreSQL 测试数据库；每次仅创建、清理自身的临时 schema，
# 邮件由测试内的本地 SMTP catcher 接收，不会向外发送。
SOLITUDES_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/solitudes_test?sslmode=disable' \
  go test -tags postgres_test ./...

# 浏览器四组合矩阵：先在 e2e/ 内运行 `bun install` 和
# `bunx playwright install chromium`，使用专用 PostgreSQL 测试数据库。
# 测试启动隔离的 HTTP 服务及本地 SMTP catcher。
SOLITUDES_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/solitudes_test?sslmode=disable' \
  go test -tags 'e2e postgres_test' ./router -run TestBrowserThemeMatrix -count=1 -v

# 构建
go build -o solitudes cmd/web/main.go
```

浏览器矩阵覆盖 cactus/folio × default/glacie 的注册与邮件验证、登录、搜索、评论、发布、
角色修改、账户改密、OIDC 客户端管理及 Folio 主题切换等核心流程；不代表所有功能均有 E2E。
上游 OAuth、WebAuthn 设备交互、上传、所有设置页面及错误分支仍需补专项浏览器测试。

## 鸣谢

- 全文搜索引擎 — [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Markdown 引擎 — [88250/lute](https://github.com/88250/lute)
- Markdown 编辑器 — [Vanessa219/Vditor](https://github.com/Vanessa219/vditor)
- Cactus 主题 — [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)

## 许可证

[AGPL-3.0](LICENSE)
