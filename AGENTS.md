# Solitudes Agent Guidelines

本文件为操作此代码库的 AI Agent 提供指导。请严格遵守以下约定和命令。

## 1. 环境与构建命令

项目基于 Go 1.24.3，使用 Fiber 框架和 GORM。

### 常用命令
- **启动开发服务器**: 
  ```bash
  # 启动前清理端口
  lsof -ti:8080 | xargs kill -9 2>/dev/null || true
  go run cmd/web/main.go
  ```
- **运行所有测试**: `go test ./...`
- **运行特定包的测试**: `go test ./internal/model/...`
- **运行单个测试函数**: `go test -v -run TestValidateThemeConfig ./internal/model`
- **查看依赖**: `go mod tidy`
- **构建项目**: `go build -o solitudes cmd/web/main.go`

## 2. 代码风格与约定

### 命名规范
- **导出项**: 使用 `PascalCase`（如 `Config`, `Serve`）。
- **非导出项**: 使用 `camelCase`（如 `newSystem`, `auth`）。
- **包名**: 简短、全小写、单名词（如 `model`, `router`, `theme`）。
- **缩写**: 对于 ID、URL、HTML 等缩写，保持全大写（如 `ArticleTemplateID`, `encodedURL`）。

### 导入规范 (Imports)
应按以下顺序分组，组间空行：
1. 标准库 (Standard library)
2. 第三方库 (Third-party libraries)
3. 本项目包 (Local packages)

### 错误处理
- **包级错误**: 在文件顶部使用 `errors.New` 定义。
- **错误返回**: 优先返回 `error`。仅在系统启动/初始化失败且无法继续时使用 `panic`。
- **错误包装**: 使用 `fmt.Errorf("...: %w", err)` 包装错误以保留原始错误上下文。
- **数据库错误**: 使用 `errors.Is(err, gorm.ErrRecordNotFound)` 检查 GORM 错误。

### 依赖注入 (DI)
项目使用 `go.uber.org/dig` 进行依赖注入。
- 初始化逻辑位于 `solitudes.go` 的 `provide()` 函数中。
- 新增全局组件时需在此注册。

### 资源与路径
- **配置**: `data/conf.yml`。
- **模板**: `resource/themes/{kind}/{theme}/templates/`。
- **静态文件**: 
  - 物理路径: `resource/themes/{kind}/{theme}/static/`。
  - URL 规范: `/static/{kind}/{theme}/{path}`（例如 `/static/site/cactus/css/main.css`）。
- **翻译文件**:
  - 基础/后端翻译: `resource/translation/{lang}.json`。
  - 主题专属翻译: `resource/themes/{kind}/{theme}/translations/{lang}.json`。
- **上传文件**: `data/upload/`。

## 3. 架构组件
...
- **翻译系统**: 采用多层合并机制。基础翻译负责后端 Go 代码中的 `.T()` 调用；主题翻译负责模板中的 `.Tr.T` 调用。主题翻译应保持自给自足，不应依赖基础翻译的 Key。

## 4. 协作守则
...
6. **翻译管理**: 修改模板或后端逻辑后，必须进行双向检查：
   - **正向**: 确保所有新增的 Key 已在对应的 JSON 文件中定义。
   - **反向**: 及时删除不再使用的冗余 Key，保持翻译文件精简。
7. **静态资源引用**: 在主题模板中引用静态资源时，必须使用统一的路径格式 `/static/{kind}/{theme}/{path}`，严禁使用旧的相对路径或不带主题标识的路径。
8. **浏览器存储规范**: 统一使用以下 `localStorage` Key 以确保跨主题数据一致性：
   - 暗黑模式: `solitudes_theme` (可选值: `auto`, `light`, `dark`)。
   - 评论者信息: `solitudes_cm_nickname`, `solitudes_cm_email`, `solitudes_cm_website`。
