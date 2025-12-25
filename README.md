# Solitudes

![构建状态](https://github.com/naiba/solitudes/workflows/Build%20Docker%20Image/badge.svg)

:smoking: 在那些寂寞的日子里，有写作伴随着我。

奶爸的一个小梦想，写一本书。<br>
Solitud.es 域名已赠予朋友。

本博客引擎的特色：

- **全文搜索** 不分「简体/繁体」中文，「大写/小写」英文，都能搜索到。
- **写书** 例子：<https://lifelonglearn.ing/dapp-cookbook>
  - 封面：在发布文章时勾选为 `这是专栏`，发布的文章会作为你的书的封面。
  - 内容：在发布文章时将封面文章的 `UUID` 填入 `专栏ID`，发布的文章将会划为你的封面内的内容。
  - 章节：如果你想写一个超长篇内容，可以套嵌，将 `内容` 变成 `封面` 进行套娃。
- **修订历史** 你对文章的所有修订记录都可被搜索及浏览
  - 新版本：编辑文章时，勾选 `大更新` 选项，会将你的文章版本升级。
  - 浏览所有版本：在链接后加 `/v*` <https://lifelonglearn.ing/dapp-cookbook/v1>
    - *无版本号展示最新版本*
    - *最新版本号会自动跳转到无版本号链接*
  - 可搜索：新旧两个版本文章都会出现在搜索结果。
- **哔哔** 类似微博、推文，例子：<https://lifelonglearn.ing/tags/Topic/>
  - 发布：在发布文章时将 `Topic` 添加到标签，为了省心 `标题` 和 `链接` 可以留空会自动补充。
- **Feed 自动发现** 粘贴博客任意链接到 RSS 阅读器即可自动发现订阅地址

## 部署

docker-compose.yml

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
        # - ./blog-data/logo.png:/solitudes/resource/static/cactus/images/logo.png # 自定义logo
```

```shell
$ ls blog-data
bleve  conf.yml  logo.png  upload
# conf.yml 是配置文件，参考 data/conf.yml.example
# logo.png 是自己 logo，替换主题自带的 logo

```

先启动 db 然后启用 UUID 扩展，再启动 solitudes


```
docker-compose up -d db
docker-compose exec db psql -U solitudes solitudes -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";'
docker-compose up -d solitudes
```

管理页面的地址是链接/admin

### 鸣谢

- 主题来自 [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)
- 全文搜索引擎 [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Hacker Pie @88250 [lute](https://github.com/88250/lute) & @Vanessa219 [Vditor](https://github.com/Vanessa219/vditor)
