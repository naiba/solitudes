# 主题设计定位

- 用户明确要求两个主题拥有独立的 UI/UX 设计，不能只是同一布局换配色；定位适用于所有页面。
- Cactus：源头是 [hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)（“clean and simple theme for a personal website”）。要的是它的干净、简单和生气：强调色标题、“日期 + 标题”的朴素列表、克制的装饰。用户明确纠正过：Cactus **不是**工具/日志/终端风，不要“偏程序员”——不要 `~/` 路径标题、等宽字体的界面文字、`↳`/`→` 之类的符号前缀、`who`/`git log` 式的比喻；等宽只用于代码，日期用正文字体加等宽数字对齐。不用装饰性卡片或期刊式双列标签目录。保持正常可读字号和触控面积。
- Folio：专业期刊风。以编辑式版面、严谨的字号层级、栏目划分和阅读节奏组织内容。
- 两主题都应提高信息密度，通过减少冗余留白和合理分栏实现；登录、注册也要有独立布局，不能只换字体配色。
- 后续主题改动需同时检查布局、信息层级和交互是否符合各自定位，并保持各主题配色的一致性。
- 用户明确要求：内容足够时，首页、读者圈、个人主页、个人中心这类大页面并排的两栏高度要大致相同，不能一栏内容多、另一栏空出一截，主体栏尤其不能空。
- 同一注册账号在个人中心、公开主页、评论等位置使用一致的头像来源：以稳定账号 ID 生成 DiceBear Lorelei 头像，不使用昵称首字或账号邮箱；主题只调整展示样式。游客评论保留已有 Gravatar 规则。

# 主题实现约定（2.2.0 起，已由浏览器契约测试覆盖）

- Cactus 的文章列表是一行一条：日期（首页 `2006-01-02`，按年分组的归档 `01-02`，本地化日期放 `title`）在前，标题后内联作者与计数；日期用正文字体 + `tabular-nums`，不用等宽字体。DOM 顺序保持“标题→作者→日期”，日期列只靠 CSS 网格前置。
- Cactus 各页标题（`.page-title`）与首页分区标题同一写法：强调色、正文字体、无前缀。话题、通知、授权应用用左侧强调线，不用圆角卡片；控件统一 2px 圆角。
- Cactus 只有一套浅色和一套深色（`color-scheme.css`），没有配色设置；页脚是三选一的切换（跟随设备/浅色/深色，读写 `solitudes_theme`），三个选项都显示，当前项加重。放页脚是因为文章页没有站点页头。
- Folio：评论是横线分隔的“来信”（头像悬挂、正文与署名对齐），话题是衬线引文，搜索是单列索引（标题｜摘要），书籍每行一卷，账户工作台是通栏链接；都不用卡片底色。
- Folio 必须保留 Tailwind 2 CDN：站长首页自定义内容使用其工具类（见 `e2e/fixtures/lifelonglearn-hometop.html`）。主题自带等价 reset（`style.css` 顶部），任何主题规则不得依赖该请求；Cactus 的 `m*/p*` 等工具类同理不能删。
- 前台主题截图用 `SOLITUDES_CAPTURE_SITE_THEME=1 go test -tags 'e2e postgres_test' ./router -run TestCaptureSiteThemeScreenshots -count=1` 从真实模板生成（1440×900，与后台截图一致），不要手工截图。

# 大页面的版面构成（已由 `pagination contract: community pages are composed of level sections` 与 `TestSiteThemeCommunityComposition` 覆盖）

- 教训：用户否定过“只把两栏底边凑齐”的做法（裁剪可选行、把文本框拉高、列表中途换成简排）。底边对齐是版面构成的结果，不是目标；先想清楚页面给谁看、内容的主次，再让结构自然不出现“一栏拖着空白”。评审要看整页原尺寸截图，不能只看缩略图和差值。
- 原则：不把“各自增长、互不相干”的两个列表并排（话题↔评论、动态↔成员、文章↔评论）。改成通栏分段，每段自带栅格；只有内容量可控的才并排（头条↔两篇推荐）。
- Folio：首页“期刊周边”是自上而下的“栏目”（`.journal-department`，左侧页边写栏目名，同归档页的年份）：话题两列瀑布（回复预览限 3 条）、来信三列（6 条，先正文后署名）、读者圈一行。读者圈页：动态两列（12 条，同首页“最新文章”栅格）、成员三列卡片。个人主页：文章两列、评论两列（评论正文为主，所评文章为注）。个人中心：左资料、右登录安全（通行密钥和已连接账号是其下的小节），应用链接是下方通栏；简介框只允许小幅伸缩（上限 13rem）。
- Cactus：首页保留两栏，侧栏的“最新评论”是唯一的弹性列表（`.fill-rows`，加载 12 条，只显示放得下的整行，行间均分余量，使侧栏与主栏底边齐平；被裁掉的行由 `main.js` 标 `inert`）。无话题时文章列表显示 10 条。读者圈：通栏动态 + 一人一行的成员列表（昵称、角色、简介首行、加入日期，列对齐）。个人主页：昵称作页标题 + 单列列表（文章同归档页；评论是“日期｜正文｜所评文章”）。个人中心：“设置行”。
- 用户把取舍交给我定，并强调不要同质化：同一页面两个主题要用各自的骨架，而不只是换字体配色。成员：Folio 是三列卡片，Cactus 是一行一人的表；个人主页：Folio 是两列栅格，Cactus 是单列日志；首页：Folio 是通栏栏目，Cactus 是主栏 + 弹性侧栏；个人中心：Folio 两栏，Cactus 设置行。改其中一个主题时不要顺手把另一个改成同样结构。
- 读者圈首页的“新面孔”不重复列出已在“活跃成员”里的人（`readersNotListed`，在路由层处理）；全员都活跃且没有下一页时不渲染“新面孔”标题。
- flex/grid 子项不与内部子元素折叠外边距：把块级栏改成 flex 栏后要显式归零首个标题的上边距和末尾列表的下边距。
- Folio 列表项有入场动画（`fadeInUp`）：测量布局的测试要先等 `document.getAnimations()` 结束，否则会量到位移中的盒子。
- 核对工具：`/root/workspace/solitudes-visual/balance.mjs <theme> <width> <route>…` 报告并排栏“墨迹底边”的差值（被拉伸的盒子高度永远相等，不能用来判断）。

# 本机运行浏览器测试

- 容器内没有 PostgreSQL 和 Docker：需 `apt-get install postgresql` 后 `pg_ctlcluster 18 main start` 并自建专用测试库（装在 /root 之外，容器重建后要重装）。
- 开工前先 `git fetch`：PR 常在 GitHub 上合并，本地 master 曾落后 origin 4 个提交而 `git status` 不提示。
- `e2e` 依赖的 Playwright 版本要与 `/opt/cloudcode-browsers` 里预装的 Chromium 修订号一致（1.65.1 ↔ chromium-1255）；本机无法自行下载浏览器，版本不一致时用 `E2E_CHROMIUM_EXECUTABLE` 指向预装的 chrome。
- 容器重建后预装的 Chromium 修订号可能变化（曾从 1255 变为 1248），`env.sh` 已改为自动取 `/opt/cloudcode-browsers/chromium-*` 并导出 `E2E_CHROMIUM_EXECUTABLE`；PostgreSQL 也要重装并重建 `solitudes_test` 角色和库。
- 本机较慢：全量矩阵约 16 分钟、Folio 单主题约 9 分钟，会撞上默认的每主题 9 分钟预算，本机运行需设置 `SOLITUDES_E2E_TIMEOUT`。GitHub CI 上整套矩阵约 10 分钟，没有这个问题。
- 主题静态资源被浏览器缓存 30 天，改 CSS/JS 后要同步更新模板里的 `?v=`。
- 常驻 fixture 服务（`solitudes-visual/serve.sh`）只实时读取 `/static`，模板在启动时解析一次：改模板后要 `refresh.sh` 重启（`PAGING=1 RICH=0 ./refresh.sh` 换成多页分页 fixture；默认会灌入 `rich-seed.sql` 的成员和评论）。只有分页 fixture 和灌过数据的 fixture 才算“内容足够”。
- 每个内置前台主题都要自带 `static/images/logo.png` 和 `favicon.ico`：没有上传文件时 `/logo.png`、`/favicon.ico` 回退到当前主题，缺失即 404（Folio 曾如此）。
- 清理主题 CSS 的做法：同一服务实例上做改动前后的整页像素对比；逐条声明探测“是否生效”时必须先注入 `transition: none`，否则主题的 reduced-motion 规则（0.01ms 过渡）会让 computed style 滞后而误判。用户正文（`.app-prose`、标题、`code` 等元素选择器）和带状态的规则（`:empty`、`details[open]`、hover）不能靠样本判断，保留。
- 首页“推荐”从阅读量前 10 随机取、单页面不显示标题、Folio 首段首字下沉（含短段落）都是既有测试固定的产品决定，不是缺陷。

- 大段替换 CSS 时不要用“从某注释删到下一个注释”的区间切除：曾因此误删 Cactus 页脚布局、分页和搜索样式。替换前先打印要删的范围，替换后对比整页截图。
