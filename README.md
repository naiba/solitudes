# Solitudes

:smoking: 在那些孤独的日子里，还有写作陪伴着我。

## 特色

- 一个可以出书的博客引擎
- 适合长篇多章节文章写作（专栏）
- SEO 友好
- 内置全文搜索
- 文章历史版本保存（可浏览可被搜索）
- Markdown 发布文章
- 邮件、Server酱 通知
- 多语言

## 指南

1. 在 postgres 数据库执行以下命令，启用 `uuid` 扩展

    ```sql
    CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
    ```

2. 创建一个数据文件夹
3. 在 `path/to/data/conf.yml` 创建配置文件 (eg: `data/conf.yml`)
4. 在 docker 中部署

    ```shell
    docker run --name solitudes -p 8080:8080 -v /path/to/data:/solitudes/data github.com/naiba/dockerfiles/solitudes
    ```

5. 打开 `https://yourdomain/login` 登录管理面板

## 感谢

- 来自 [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus) 的主题
- 管理面板UI [AdminLTE](https://adminlte.io/)
- 全文搜索引擎 [blevesearch/bleve](https://github.com/blevesearch/bleve)
- 黑客派 @88250 [lute](https://github.com/88250/lute) & @Vanessa219 [Vditor](https://github.com/Vanessa219/vditor)
