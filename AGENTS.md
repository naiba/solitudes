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
- **静态文件**: `resource/themes/{kind}/{theme}/static/`。
- **上传文件**: `data/upload/`。

## 3. 架构组件

- **`solitudes.go`**: 核心初始化逻辑、全局 `System` 变量（包含 DB、Config、Cache）。
- **`internal/model/`**: 数据库模型定义与业务逻辑逻辑（如配置校验）。
- **`internal/theme/`**: 主题加载与管理逻辑。
- **`router/`**: 路由定义、控制器函数、中间件（auth, trans）。
- **`pkg/`**: 独立于业务的通用工具包。

## 4. 协作守则

1. **修改代码前先阅读**: 在修改任何逻辑前，使用 `Grep` 或 `Read` 了解现有的实现模式。
2. **保持注释一致**: 为导出的函数和结构体添加清晰的中文注释，描述其用途。
3. **测试先行**: 修改模型或核心逻辑后，应运行相关测试（如 `internal/model/config_test.go`）确保未破坏现有功能。
4. **不要随意更改目录结构**: 遵循现有的主题和资源组织方式。
5. **安全第一**: 严禁在代码中硬编码任何敏感信息（密钥、密码等），应使用配置文件。
