# Solitudes

![构建状态](https://github.com/naiba/solitudes/actions/workflows/docker.yml/badge.svg)

[English](README.md)

基于 Go 和 Fiber 的多用户博客，支持全文搜索、多级专栏、文章历史、哔哔、评论、读者圈、RSS 和可切换主题，也可作为 OIDC 服务端供其他应用登录。

## 安装

将以下内容保存为 `compose.yml`，替换示例数据库密码：

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

参考 [配置示例](data/conf.yml.example) 创建 `blog-data/conf.yml`：数据库主机填 `db`，用户名、密码和库名与上面一致；`site.domain` 填对外域名，不带协议。生产环境通过反向代理启用 HTTPS。

```bash
docker compose up -d db
# 确认数据库就绪后，再初始化管理员
docker compose exec db pg_isready -U solitudes -d solitudes
docker compose run --rm solitudes /solitudes/solitudes init-admin --email you@example.com --nickname 管理员
docker compose up -d solitudes
```

打开 `http://localhost:8080/login` 登录。没有默认账号或密码；初始化仅适用于空账户库，随机密码只显示一次，请妥善保存并在个人中心修改。

`blog-data/` 保存配置、上传文件和搜索索引；账号、密码及文章保存在 PostgreSQL。请同时备份目录和数据库，并保护其中的凭据与签名密钥。

旧单用户／旧权限数据不自动迁移，升级前须备份并核验数据。配置拒绝未知字段：删除旧的顶层 `user`、`configfilepath`，保留 SMTP 的 `email.user`。

## 使用

### 账户与写作

- `/register` 注册，验证邮箱后登录；需配置 SMTP：`email.host`、`email.port`、`email.user`、`email.pass`、`email.ssl`。
- `/account` 修改资料、密码、绑定登录方式及管理应用。密码保存在数据库，不回写配置。
- 管理员管理全站；编辑可直接发布和管理自己的文章；普通会员不能发布或编辑。写作入口在个人中心。
- 勾选“这是专栏”创建专栏，其他文章填写其 UUID 即可加入；专栏可以嵌套。
- 添加 `Topic` 标签即可发哔哔，标题和链接可自动生成；后台首页也提供快速发布。
- 编辑时勾选“大更新”保留标题和正文的历史版本，历史地址如 `/my-article/v1`；搜索只返回当前版本。此前未保存的历史标题显示“标题未记录”。
- 创建人和最后修改人分别记录。`/my-article/compare/v1...v3` 按渲染后的内容块对比版本，按读者权限过滤，并禁止索引、指向主文章 canonical。
- 读者圈默认展示已验证用户，可在个人中心退出；邮箱不会公开。

### 内容权限

整篇可见性支持公开、会员、编辑、仅作者与管理员。Vditor 的“受限内容”工具可插入 `access:members`、`access:editors`、`access:private` Markdown 围栏，在服务端过滤片段。编辑不能新增或修改可执行代码。

历史版本沿用最新文章的整体权限，片段沿用当时的规则：后来遮蔽的内容可能仍在旧版本中可见。上传文件始终公开，遮蔽链接不会限制下载；不要上传敏感附件或公开受限 Markdown 原文。

### 第三方登录博客

在 `/admin/auth/providers` 配置 GitHub、Google 或上游 OIDC，回调地址为 `https://<域名>/auth/{github,google,oidc}/callback`。已有同邮箱账户时，先登录，再从个人中心绑定。

通行密钥需 HTTPS；配置 `auth.webauthn.rp_id` 为站点域名、`auth.webauthn.origin` 为完整 HTTPS 地址。

### 用博客账户登录其他应用

所有已验证用户均可在 `/account/oidc/clients` 创建客户端，填写应用名称、主页和精确回调地址。生产环境使用 HTTPS；localhost 开发可用 HTTP。

- OIDC 发现地址：`https://<域名>/.well-known/openid-configuration`。
- OAuth 元数据：`https://<域名>/.well-known/oauth-authorization-server`。
- 使用授权码 + PKCE S256，支持轮换刷新令牌，不支持隐式或密码授权。请求 `openid` 才会获得 ID Token。
- 公开客户端无需密钥；机密客户端使用 `client_secret_basic`，密钥只在创建时显示。
- `/account/oidc/authorizations` 查看并撤销当前有效授权；凭据全部失效后不再展示。
- 已登录且有效访问／刷新令牌覆盖请求权限时跳过确认；新增权限或 `prompt=consent` 需要确认。`prompt=none` 在需要交互时返回 `login_required` 或 `consent_required`；`prompt=login`、`max_age` 检查登录是否足够新。

禁用或删除应用会清理相关令牌与待处理请求，但保留审计。撤销不能收回应用已保存的数据、退出其自身会话或使 ID Token 的离线签名立即失效。修改站点域名后需重启服务。

### 后台管理

管理员可在 `/admin/users` 管理用户、在 `/admin/oidc/clients` 查看应用及登录统计、在 `/admin/audit` 查询安全事件。

审计保留登录、授权决定、敏感变更和失败事件；普通浏览、成功的令牌刷新/校验及认证中间步骤不逐次记录。

`audit_retention_days: 0` 默认永久保留审计。设为正整数后会永久删除超期详情，只保留累计登录统计；重要部署请另行备份或归档。

## 主题

内置 Cactus、Folio 前台主题和一个默认后台主题，可在“后台 → 系统设置”独立切换。

自定义主题放在 `resource/themes/{site,admin}/<主题名>/`，包含 `metadata.json`、`screenshot.png`、`templates/`、`static/` 和 `translations/`。`metadata.json` 的 `id` 必须与目录名一致，格式参考 [Cactus](resource/themes/site/cactus/metadata.json)。

所有前台主题共用权限过滤后的 `.Queries` 数据接口，用法参考 [查询接口](router/template_queries.go) 和内置模板。只安装可信主题；不要绕过正文权限过滤，或对用户输入使用 `unsafe`。

## 开发与测试

需要 Go 1.26+、C/C++ 编译器和 PostgreSQL。配置 `data/conf.yml` 后：

```bash
go build -o solitudes cmd/web/main.go
./solitudes init-admin --email you@example.com --nickname 管理员  # 仅首次安装
./solitudes
```

集成及浏览器测试使用专用数据库，会自行启动隔离服务、SMTP catcher 和临时 schema：

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

CI 通过测试后构建镜像。E2E 覆盖两主题的核心流程，不代表全部功能；WebAuthn 使用虚拟认证器，真实上游 OAuth 和物理认证器需另测。

## 鸣谢与许可

[Bleve](https://github.com/blevesearch/bleve) · [Lute](https://github.com/88250/lute) · [Vditor](https://github.com/Vanessa219/Vditor) · [Cactus](https://github.com/probberechts/hexo-theme-cactus)

[AGPL-3.0](LICENSE)
