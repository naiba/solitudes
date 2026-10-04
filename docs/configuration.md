# 配置与兼容策略 / Configuration and compatibility

## 当前配置

以 `data/conf.yml.example` 为准。配置只包含站点、主题、数据库、SMTP、代理、通知、登录提供方及资源策略。`user`（包括 email、nickname、password、token、tokenexpires）已删除；`configfilepath` 仅是程序内部运行信息，不写入 YAML。配置加载会拒绝未知或已删除的字段，请先从部署文件中删除这些旧项目，保留 `email.user`（SMTP 用户名），它不是网站账户。

账号资料、角色和密码唯一来源为 PostgreSQL 的 `accounts` 表；修改密码只更新 bcrypt 哈希。备份必须包含数据库，只有配置文件不能恢复账户或新密码。首次管理员通过本机命令创建：

```sh
./solitudes init-admin --email you@example.com --nickname Administrator
```

该命令不启动 HTTP 服务，不依赖公开注册或 SMTP，只能在账户表为空时执行。随机密码仅输出一次；请妥善保存，并在首次登录后从个人中心修改。初始化是事务化操作，重复执行不重置密码、不自动提升用户。初始化身份写入安全审计，但密码不写入审计或配置。

## 部署边界

本次版本明确取消旧版本兼容：不再从配置文件创建账户、不自动回填无作者文章、不迁移 `is_private` 或评论的 `is_admin`、不补齐旧 OAuth 应用资料、不兼容缺少权限字段的搜索索引，也不清理旧版本专用索引。正常的当前模型建表、索引、约束维护，以及权限检查、网址校验和异常数据拒绝仍保留；这些不是兼容分支。

已经使用当前多用户数据模型的安装，账号、密码和文章不被初始化逻辑改写；无需重建数据库或重跑 `init-admin`。移除旧配置即可。旧列即使还留在数据库中也不再读取；此清理不自动删除现有数据库里的列或用户数据。

更早的单用户/旧权限安装不能直接依赖本版本自动升级。部署前备份，并在副本中显式导入当前模型：确认每篇文章的作者和 `visibility`，已注册评论的 `account_id`，以及 OAuth 应用创建者和合法主页。完成数据核验后，用后台「重建索引」建立当前搜索索引再对外开放。不要把旧私密文章当作默认公开数据导入，也不要把初始化命令作为密码重置工具。

## English summary

Remove the obsolete top-level `user` block and `configfilepath` before starting the server; unknown or removed fields are rejected. Retain `email.user`, which is the SMTP login. Accounts and bcrypt password hashes live only in PostgreSQL. Run the local `init-admin` command for an empty installation; it prints a random password once and never overwrites credentials or promotes registered users. Save the output securely and change the password at `/account` after signing in.

This release drops automatic compatibility with single-user configurations, ownerless articles, old visibility/admin flags, incomplete OAuth application metadata and search documents without an access policy. Current-model schema/index/constraint setup and fail-closed security checks remain. Existing multi-user accounts and content are not rewritten. No deployed database columns or user records are automatically deleted by this cleanup. Older installations require a backed-up, explicitly validated import before deployment and an administrator-triggered search-index rebuild before public access.
