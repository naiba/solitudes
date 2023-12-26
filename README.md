# Solitudes

![构建状态](https://github.com/naiba/solitudes/workflows/Build%20Docker%20Image/badge.svg)

:smoking: 在那些寂寞的日子里，有写作伴随着我。

奶爸的一个小梦想，写一本书。

本博客引擎的特色：

- **专栏（系列文章）**
- **全文搜索**
- **哔哔**（类似微博，短消息）
- Feed 自动发现（粘贴博客任意链接到 RSS 阅读器即可发现订阅）

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
        - ./blog-data/logo.png:/solitudes/resource/static/cactus/images/logo.png
```

```shell
$ ls blog-data
bleve  conf.yml  logo.png  upload
# conf.yml 是配置文件，参考 data/conf.yml.example
# logo.png 是自己 logo，替换主题自带的 logo
```

### 鸣谢

- 主题来自 [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)
- 管理面板界面 [AdminLTE](https://adminlte.io/)
- 全文搜索引擎 [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Hacker Pie @88250 [lute](https://github.com/88250/lute) & @Vanessa219 [Vditor](https://github.com/Vanessa219/vditor)
