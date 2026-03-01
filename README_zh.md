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

# 构建
go build -o solitudes cmd/web/main.go
```

## 鸣谢

- 全文搜索引擎 — [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Markdown 引擎 — [88250/lute](https://github.com/88250/lute)
- Markdown 编辑器 — [Vanessa219/Vditor](https://github.com/Vanessa219/vditor)
- Cactus 主题 — [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)

## 许可证

[AGPL-3.0](LICENSE)
