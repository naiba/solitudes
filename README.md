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

1. 在服务器中创建一个文件夹

    ```sh
    mkdir solitudes && cd solitudes
    ```

2. 创建 `docker-compose.yaml` 文件

    ```sh
    vi solitudes
    ```
    按 `i` 进入编辑模式，复制以下内容粘贴进去
    ```yaml
    version: '3.3'

    services:
      db:
        image: postgres:13-alpine
        volumes:
          - ./data/db:/var/lib/postgresql/data
        restart: always
        environment:
          POSTGRES_PASSWORD: mypaypassword
          POSTGRES_USER: solitudes
          POSTGRES_DB: solitudes

      solitudes:
        depends_on:
          - db
        image: docker.pkg.github.com/naiba/dockerfiles/solitudes:latest
        ports:
          - "80:8080"
        restart: always
        volumes:
          - ./data/solitudes:/solitudes/data
    ```
    
2. 创建数据文件夹
    ```sh
    mkdir -p data/solitudes
    ```
    
4. 创建 Solitudes 配置文件

    ```sh
    vi data/solitudes/conf.yml
    ```

    按 `i` 进入编辑模式，复制以下内容粘贴进去

    ```yaml
    debug: true
    database: postgres://postgres:mypassword@localhost/solitudes?sslmode=disable
    user:
      email: hi@example.com
      nickname: naiba
      password: $2a$10$qXMp0vfCL2rdhYGr7VT7NuJLEMysmO.EsGAfgQGtMupITe7ZNbi86
    site:
      spacename: Solitudes
      spacedesc: We love writing
      hometopcontent: "# Top:\n\nA fast, simple & powerful blog framework \U0001F44D\n"
      homebottomcontent: "# Bottom:\n\nA fast, simple & powerful blog framework \U0001F44D\n"
      theme: dark
      headermenus:
        - name: Home
          link: /
          icon: ""
          black: false
        - name: Archives
          link: /archives/
          icon: ""
          black: false
        - name: About
          link: /about
          icon: fa fa-lightbulb
          black: false
        - name: Solitudes
          link: https://github.com/naiba/solitudes
          icon: fab fa-github
          black: true
      footermenus:
        - name: Home
          link: /
          icon: ""
          black: false
        - name: About
          link: /about
          icon: far fa-lightbulb
          black: false
    ```

5. 启动环境

    ```sh
    docker-compose up -d
    ```

6. 在 postgres 数据库启用 `uuid` 扩展，在文件夹下执行：

    ```sh
    docker-compose exec db bash
    psql -U solitudes solitudes
    ```
    然后复制下面的SQL语句执行
    ```sql
    CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
    ```

7. 重启 Solitudes

    ```sh
    docker-compose restart solitudes
    ```

8. 打开 `http://yourdomain/login` 登录管理面板

    默认登陆邮箱：`hi@example.com`

    默认登陆密码：`123456`

## 感谢

- 来自 [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus) 的主题
- 管理面板UI [AdminLTE](https://adminlte.io/)
- 全文搜索引擎 [blevesearch/bleve](https://github.com/blevesearch/bleve)
- 黑客派 @88250 [lute](https://github.com/88250/lute) & @Vanessa219 [Vditor](https://github.com/Vanessa219/vditor)
