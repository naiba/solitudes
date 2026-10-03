# 模板基础接口

所有主题都获得同样的接口；后端不识别 Folio、Cactus 的布局，也不生成“头条”“推荐区”等专用数据。首页控制器不预先查询集合。模板按需调用请求绑定的 `.Queries`，只读、无任意 SQL，自动应用当前访问者的文章权限和局部正文裁剪。

## 数据查询

| 方法 | 参数及返回值 |
| --- | --- |
| `.Queries.Articles order count [key value ...]` | 文章模型集合，预加载作者公开字段 |
| `.Queries.Comments articleID count` | 有权访问文章的可见根评论，最新优先，预加载评论者公开身份 |
| `.Queries.Users order count` | 公开读者资料，遵守读者圈隐藏、邮箱验证、封禁规则；`newest` / `oldest` |
| `.Queries.RecentComments count` | 公开文章的最新评论正文及其作者/来源，不包含邮箱、IP；过滤垃圾评论及其后代；截取长度由模板决定 |

`count` 范围 0～100，0 返回空集合。错误参数返回模板错误，不静默回退，也不能取消权限范围。

文章排序：`newest`、`oldest`、`updated`、`reads`、`comments`；并列时使用稳定 ID 排序。

文章过滤参数是成对字符串：

- `author`：作者 ID。
- `tag` / `without_tag`：包含 / 不包含指定标签（Topic 也只是标签）。
- `template`：模板类型编号，如 `"1"` 文章、`"2"` 页面。
- `exclude`：排除文章 ID，可重复。
- `offset`：跳过 0～10000 条。

没有固定的首页展示条数，也不强制排除 Topic。新主题可以直接组合这些查询。

## 组合示例：最新一篇、六篇近期、热门池随机两篇

```gotemplate
{{$articles := .Queries.Articles "newest" 7 "without_tag" "Topic"}}
{{if $articles}}
  {{$lead := index $articles 0}}
  <h1>{{$lead.Title}}</h1>
  {{range $i, $article := $articles}}
    {{if gt $i 0}}<a href="/{{$article.Slug}}">{{$article.Title}}</a>{{end}}
  {{end}}

  {{$pool := .Queries.Articles "reads" 10 "without_tag" "Topic" "template" "1" "exclude" $lead.ID}}
  {{range randomIndices (len $pool) 2}}
    {{$article := index $pool .}}
    <a href="/{{$article.Slug}}">{{$article.Title}}</a>
  {{end}}
{{end}}
```

`randomIndices length count` 返回不重复的随机下标，最多 `min(length, count)` 个；不修改原集合。参数范围 0～10000。它不代表推荐算法，也不要求池内文章与近期列表互斥。

## 模板函数审计

- 移除 `articleData`、`commentsData`、`tocTemplateData`：统一使用 `dict "key" value ...` 传递模板参数。
- 移除 `articleIdx`：直接使用模型的 `.GetIndexID`。
- 移除 `oldVersions`：不再由 Go 拼 HTML；模板遍历 `.PreviousVersions` 并自行输出链接。文章页仅在 `.Data.can_read_history` 为真时提供历史入口。
- 移除 `tocNumberLabel`：用内置 `printf` 和 `add` 在模板组合序号。
- 保留纯格式化/内容工具：`md`、`mdExcerpt`、`firstImage`、`tocHeadingCount`、`tf`、`iso8601`、`substr`、`trim`、`hasPrefix`、`add`、`last`、`ptrStrEq`、`int2str`、`uint2str`、`json`、`yaml`、`md5`、`urlencode`、`jsonEscape`、`externalLink`、`unsafe`。这些不做主题业务查询。

`unsafe` 仅适合受信任的站点配置和已经服务端处理的 Markdown；不能用于会员昵称、评论文本等不可信输入。主题代码由站点管理员安装，是受信任的服务端代码，不是针对恶意主题的沙盒。

列表/详情页的 `.Data` 保留路由自身的分页、文章、评论等模型数据；不增加其他主题专属数据。文章 `.Content` 已按当前访问者裁剪，主题不得另外读取数据库原文或把编辑接口原文传入前台。

## 内容权限

见 [文章与局部内容权限](content-access.md)。默认主题名称仅用于资源选择，不参与业务查询。
