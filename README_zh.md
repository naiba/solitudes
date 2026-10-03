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
- **主题系统** — 前后台主题可独立热切换；后台仅内置一个现代默认主题
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

已有站点升级时，会从 `data/conf.yml` 的 `user.email`、`user.nickname`、`user.password`（bcrypt 哈希）创建首位管理员，并把旧文章归其所有。新站点对外开放前务必替换示例配置中的默认密码。首次迁移完成前请保留这些配置；旧的配置文件登录令牌不再有效。普通用户可在 `/register` 注册，邮件验证链接一小时内有效，未验证不能登录；注册需配置 SMTP。`/account` 可管理身份和通行密钥。管理员在 `/admin/users` 分配管理员、编辑、普通用户角色；编辑可以直接发布并管理自己的文章，普通用户不能进入写作后台。

前台 `/readers/`「读者圈」默认展示所有已验证且未停用的用户，包括现有账号；用户可在 `/account` 的公开资料中退出或重新加入。首页展示最新加入的四位成员和最多五条公开最新评论（包括游客评论）；读者圈页面展示近期公开动态，并根据最近 30 天的公开文章和非垃圾评论统计活跃度。邮箱、私密文章和垃圾评论不会进入目录或榜单。RSS、Atom、JSON Feed 为每篇公开文章标注真实作者，订阅源元数据不再暴露配置文件中的管理员邮箱。编辑及管理员可从前台导航的「创作工作台」直接进入文章管理，个人中心另提供写文章入口。

请在 `data/conf.yml` 中设置 `site.domain` 为对外域名（必要时含端口），生产环境使用 HTTPS，并配置 `email.host`、`email.port`、`email.user`、`email.pass`、`email.ssl`。管理员可在 `/admin/auth/providers` 页面配置 GitHub、Google 和上游 OIDC 登录（保存后密钥不再回显）；对应的配置文件写法是：

```yaml
auth:
  github: {client_id: "", client_secret: ""}
  google: {client_id: "", client_secret: ""}
  oidc: {issuer: "https://identity.example.com", client_id: "", client_secret: ""}
  webauthn: {rp_id: "blog.example.com", origin: "https://blog.example.com"}
```

各上游平台的回调地址为 `https://<site.domain>/auth/{github,google,oidc}/callback`。上游必须返回已验证邮箱；若本地已有相同邮箱账号，先登录，再在 `/account` 主动绑定。通行密钥需要 HTTPS 且 RP ID 与 Origin 的主机相同（本地 localhost 开发例外）。未配置的登录方式不会启用。

Solitudes 同时作为 OAuth 2.1 风格的授权服务端和 OIDC 服务端：**所有已验证的博客账户**（包括普通用户和编辑）都能登录自己接入的外部应用。前台导航栏的 `/account` 个人中心可修改密码、添加通行密钥、绑定或解绑上游登录提供方，并在 `/account/oidc/clients` 创建及停用自己的客户端；管理员仍可在 `/admin/oidc/clients` 管理全站客户端。外部应用可通过 `https://<site.domain>/.well-known/openid-configuration`（OIDC）或 `https://<site.domain>/.well-known/oauth-authorization-server`（OAuth 元数据）发现端点。这与上方“外部提供方登录博客”是两个独立方向。支持授权码（必须使用 PKCE S256）和轮换刷新令牌，不支持隐式或密码授权；回调 URI 必须精确匹配且使用 HTTPS（localhost 可用 HTTP）。公开客户端无密钥；机密客户端通过 `client_secret_basic` 认证，密钥仅创建时显示一次。申请 `openid` 范围以获取 ID Token；纯 OAuth 客户端无需申请。用户每次授权需同意。禁用客户端会撤销 Access Token 和 Refresh Token；已签发的 ID Token 在过期前仍可通过签名验证，密钥轮换时会保留旧公钥。数据库备份包含 OIDC 签名私钥和客户端凭据，务必妥善保护；未设置 `site.domain` 或数据库不可用时，OIDC 服务不可用。变更域名后需重启服务。

新客户端需填写应用名称、可选简介、应用主页（须为 HTTPS，开发环境 localhost 可使用 HTTP）和精确匹配的回调地址；退出后的回跳地址可选，但也必须预先注册。RP 发起的退出会撤销对应应用的令牌，不会退出博客本身。创建者与管理员可修改应用资料及回调地址。外部应用发起授权时，登录页和同意页都会展示应用信息、应用主页以及创建者的站内公开主页；没有这些元数据的旧客户端仍可使用并可补填资料，不会渲染未验证的网址。

### 后台统计与安全审计

管理员在 `/admin/users` 查看用户总数、按昵称/邮箱和角色筛选，并从用户详情进入其注册应用列表。`/admin/oidc/clients` 支持按应用名称、创建者和状态筛选，展示登录次数、独立登录用户及最近登录时间；应用详情可查看近期登录用户。默认后台工作台提供分页列表，每页 25 条。

`/admin/audit` 是仅管理员可访问的只读审计页，可按事件、结果、用户、应用、IP、请求 ID 和 UTC 日期筛选。记录站内登录、应用授权与令牌签发、应用变更、角色调整、账户安全操作、后台写操作及失败/拒绝请求；OAuth 错误保留标准错误码，不保留错误描述。审计不记录密码、客户端密钥、令牌、Cookie、请求正文或查询字符串。IP 使用站点配置的可信代理规则，请勿无条件信任外部转发头。

统计从首次启用审计开始，不回填无法验证的历史登录。一次授权码签发令牌计一次应用登录，授权页访问、刷新及失败请求不计；注销、撤销或停用应用不删除历史记录。应用变更、角色变更和凭据签发与审计在同一数据库事务中，审计写入失败时回滚；其他请求记录失败时输出不含敏感内容的 `audit_write_failed` 服务端日志，可通过响应中的 `X-Request-ID` 关联故障。审计表不自动清理，应纳入数据库容量监控和备份；网站没有修改/删除审计记录的入口，但这不等于防止数据库管理员篡改，重要部署应另行归档至受保护的外部日志系统。

## 主题系统

Solitudes 支持独立切换前台和后台主题。目前仅内置一个现代默认后台主题，但保留安装其他可信主题和切换的能力。

### 主题目录结构

```
resource/themes/
├── site/<theme_name>/    # 前台主题
└── admin/<theme_name>/   # 后台主题（内置 default）
```

前后台主题使用完全相同的目录约定：

```text
<theme_name>/
├── metadata.json
├── screenshot.png       # 设置页的真实预览图，推荐 16:10
├── templates/
├── static/
└── translations/
    ├── en.json
    └── zh.json
```

两类主题共用 `metadata.json` 格式；`id` 必须与目录名一致：

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

在 **管理后台 > 系统设置** 中，前后台均通过相同的截图卡片选择，保存后独立生效。截图统一读取主题根目录的 `screenshot.png`，没有 `preview` metadata 字段；缺图时显示本地占位提示，不请求第三方图片服务。其他后台主题遵循上面的相同结构即可自动发现。主题属于可信的服务端代码，只应安装可信来源的主题。

所有前台主题使用同一套[模型查询与模板基础接口](docs/theme-data.md)，自行组合排序、数量和随机抽样；后端不根据主题名切换查询策略。[文章与局部内容权限](docs/content-access.md)由服务端统一处理。

## 开发

**前置依赖**：Go 1.26+、PostgreSQL

数据库结构、索引、连接池和审计清理配置见[数据库设计与维护](docs/database.md)。

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

# 浏览器双主题矩阵：先在 e2e/ 内运行 `bun install` 和
# `bunx playwright install chromium`，使用专用 PostgreSQL 测试数据库。
# 测试启动隔离的 HTTP 服务及本地 SMTP catcher。
SOLITUDES_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/solitudes_test?sslmode=disable' \
  go test -tags 'e2e postgres_test' ./router -run TestBrowserThemeMatrix -count=1 -v -timeout 20m

# 构建
go build -o solitudes cmd/web/main.go
```

浏览器矩阵覆盖 cactus/folio × default 的注册与邮件验证、登录、搜索、评论、发布、
角色修改、账户改密、OIDC 客户端归属与管理、所有账户角色登录外部 OIDC 应用、登录 Provider 配置
及 Folio 主题切换等核心流程。PR 和 `master` 推送会在 CI 中运行 PostgreSQL 与 Chromium 矩阵，
通过后才构建镜像；不代表所有功能均有 E2E。
WebAuthn 注册/删除使用 Chromium 虚拟认证器；真实上游 OAuth 和物理认证器不在自动测试范围内。
上传、所有设置页面及错误分支仍需补专项浏览器测试。

需要截图时，在同一浏览器矩阵命令中设置 `SOLITUDES_VISUAL_AUDIT=1`，
并把 `SOLITUDES_VISUAL_DIR` 指向临时目录，添加 `-timeout 20m`。
隔离的视觉测试已取代旧 localhost 截图脚本，不会覆盖主题预览图或向正在运行的博客灌数据。
检查后删除生成的截图。

## 鸣谢

- 全文搜索引擎 — [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Markdown 引擎 — [88250/lute](https://github.com/88250/lute)
- Markdown 编辑器 — [Vanessa219/Vditor](https://github.com/Vanessa219/vditor)
- Cactus 主题 — [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)

## 许可证

[AGPL-3.0](LICENSE)
