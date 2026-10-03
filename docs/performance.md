# 阅读链路性能检查（2026-10-03）

## 方法与边界

在 Linux arm64、Go 1.27.0、PostgreSQL 16.15 上运行真实 Fiber handler 和 Cactus/Folio 模板。测试创建独立随机 schema，退出后删除；不向预览站点的 public schema 写入测试数据。

`BenchmarkPostgresReading` 数据集包含 20 个根专栏，每个专栏 5 个子专栏、每个子专栏 10 篇章节，共 1120 条记录。每条约 8 KB、包含标题、链接、强调和中文的 Markdown；为保持确定性，正文相同。每个路由先预热再串行重复请求，数据库连接池限制为 1，搜索使用内存 Bleve。测量首页、专栏列表、专栏详情、章节正文、搜索首页。

这衡量的是**重复阅读的热路径**，不是生产容量承诺。`B/op` 是每请求累计分配量，不是常驻内存；CPU/heap profile 和进程峰值 RSS 还包含建表、造数和建索引。两次 profile 的采样总量不能直接当成同等请求数的 CPU 降幅。未覆盖公网延迟、并发峰值、超长 Markdown、超大章节树或长时间运行下的 RSS。

## 实际改动

- 专栏列表的逐专栏、逐子专栏查询改为整页一次递归 CTE + SQL SUM；复用 `book_refer` 索引，不加冗余统计表，不增加索引写成本。
- 专栏详情仅查询章节元数据，一次构造整棵树；不读取每篇章节正文。每一层应用可见性范围，隐藏父专栏的子树不参与标题展示和统计。`UNION` 终止异常循环，构造树时不把根节点重新挂回子节点。
- 正文不变的摘要不再反复构造 Lute AST：256 条 LRU，仅缓存最长 512 字符的纯文本摘要，以内容 SHA-256 + 截断长度寻址；更长摘要绕过缓存。权限过滤仍实时执行，编辑内容后自然生成新 key。
- 搜索从“第 1 页固定重建 100 个候选”改为按当前页需求取候选，不够再扩展，每批最多 100。活跃候选一次批量写入临时索引，并取消不被使用的持久索引高亮。保留当前数据库权限／内容重验、历史版本去重、稳定排名、下一页哨兵和 5 秒上下文超时。
- 章节前后导航增加 ID 作为相同发布时间的稳定排序键，避免漏章。

没有引入 Redis、搜索服务或主题专属后端分支。

## 回归约束

- `TestPostgresBookTreeVisibilityBatchingAndCycles`：四种身份、隐藏父节点、嵌套树、重复聚合不累加、一次 SQL、无子正文加载、循环引用终止、数据库错误传播。
- `TestPostgresSearchAdaptiveBatchesPreservePrivacyAndPagination`：210 条含旧版本／过时正文／过时可见性的索引记录，跨批次及三页验证无泄漏、不重复、不丢失。
- 摘要缓存测试：LRU 上限、淘汰、并发、内容与长度区分、受限内容、缓存与未缓存输出一致；`go test -race ./pkg/content`。
- SQL `EXPLAIN (ANALYZE, BUFFERS)`：5000 用户、5000 应用、200000 审计、10000 文章，验证用户／应用统计仅计算当前页、slug／标签／文章分页／章节父键命中索引。
- 两主题浏览器用例：专栏列表 → 根专栏 → 前后章 → 子专栏 → 子章节 → 返回父专栏；四身份 × 390/1280 宽度，长标题、空专栏、首末章和权限边界。专栏截图检查后删除，不作为仓库资源。

## 测量结果

优化前后的二进制串行运行，每项 `-benchtime=2s`，期间未并行运行编译或浏览器回归。以下是本机一轮观测值，不是统计置信区间：

| 主题 / 路由 | 毫秒/请求（前 → 后） | 分配 MiB/请求（前 → 后） | SQL/请求（前 → 后） |
|---|---:|---:|---:|
| cactus/Home | 3.84 → 3.87 | 0.45 → 0.45 | 7 → 7 |
| cactus/Books | 10.83 → 3.68 | 0.78 → 0.47 | 53 → 4 |
| cactus/Book | 11.63 → 10.48 | 4.65 → 3.08 | 18 → 7 |
| cactus/Article | 7.20 → 6.41 | 3.95 → 2.85 | 7 → 7 |
| cactus/Search | 145.47 → 13.21 | 96.89 → 7.79 | 1 → 1 |
| folio/Home | 12.12 → 3.80 | 10.31 → 0.48 | 7 → 7 |
| folio/Books | 29.02 → 4.06 | 22.88 → 0.70 | 53 → 4 |
| folio/Book | 11.49 → 8.48 | 4.66 → 3.10 | 18 → 7 |
| folio/Article | 7.06 → 6.66 | 3.96 → 2.85 | 7 → 7 |
| folio/Search | 148.31 → 12.69 | 96.86 → 7.80 | 1 → 1 |

注意专栏列表按时间和随机 UUID 排序，所有样本时间相同；不同造数轮次首页的根／子专栏比例会变化。优化前多轮观察为 38～53 次 SQL，本表这轮为 53；优化后恒为 4（分页计数、文章、作者、整页专栏聚合）。因此不要把列表耗时的小数或降幅当作精确可复现的常数。固定 slug 的专栏详情始终包含相同规模的章节树。

- Cactus 首页时间基本不变；主要收益在 Folio 摘要、专栏批量查询和搜索候选处理，并非每个页面都大幅加速。
- 进程峰值 RSS：178852 → 166140 KiB（约 175 → 162 MiB），含建索引及整个 benchmark 生命周期；不能等同于线上常驻内存降低 7%。
- 独立摘要微基准（约 8 KB Markdown）：不缓存约 1.86 ms、1.76 MiB/次；热缓存约 7.8 µs、8 KiB/次。冷请求仍要正常解析，并非全部请求都能命中。
- CPU profile 中主要开销为 GC 对象扫描、Bleve 分词、模板反射及 Lute 解析；allocation profile 确认大量临时对象来自 Lute AST 和 Bleve term vectors。改动减少重复工作，没有以缓存权限判断换速度。
- SQL 执行计划样本：章节父键查询约 0.055 ms；后台应用页约 2.5 ms、用户页约 1.6～1.7 ms，并确认相关统计只覆盖当前页。
- 浏览器回归：Cactus 62 条常规用例、Folio 63 条（分组运行），另有两主题共 4 条分页用例通过。常规矩阵中 11 次条件跳过对应另一主题专有能力／单独运行的分页数据集，不算通过条目。
- `go test -tags postgres_test ./... -count=1`、`go test -race ./pkg/content`、`go vet ./...`、TypeScript 检查和构建通过。32 张专栏截图已逐类检查并删除。

## 复现

先设置 `SOLITUDES_TEST_POSTGRES_DSN` 指向可创建测试 schema 的 PostgreSQL，再从仓库根目录运行：

```bash
go test -tags postgres_test ./... -count=1
go test -race ./pkg/content
go test ./router -run '^$' -bench BenchmarkPostgresReading -benchmem -benchtime=2s \
  -cpuprofile=/tmp/solitudes-reading.cpu -memprofile=/tmp/solitudes-reading.mem
go tool pprof -top /tmp/solitudes-reading.cpu
go tool pprof -top -alloc_space /tmp/solitudes-reading.mem
go test ./pkg/content -run '^$' -bench BenchmarkExcerpt -benchmem
go test -tags 'e2e postgres_test' ./router -run TestBrowserThemeMatrix -count=1 -timeout 20m
```

比较不同版本时应先分别编译测试二进制，然后串行运行，期间不要并行跑浏览器、编译或其他压测。SQL 次数／时间通过 GORM Trace 采集；这是包含客户端开销的 SQL 调用耗时，不等同于 PostgreSQL 纯执行时间。
