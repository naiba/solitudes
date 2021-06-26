# Solitudes

![构建状态](https://github.com/naiba/solitudes/workflows/Build%20Docker%20Image/badge.svg) <a href="README_en-US.md">
    <img height="20px" src="https://img.shields.io/badge/EN-flag.svg?color=555555&style=flat&logo=data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMTIwMCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIiB2aWV3Qm94PSIwIDAgNjAgMzAiIGhlaWdodD0iNjAwIj4NCjxkZWZzPg0KPGNsaXBQYXRoIGlkPSJ0Ij4NCjxwYXRoIGQ9Im0zMCwxNWgzMHYxNXp2MTVoLTMwemgtMzB2LTE1enYtMTVoMzB6Ii8+DQo8L2NsaXBQYXRoPg0KPC9kZWZzPg0KPHBhdGggZmlsbD0iIzAwMjQ3ZCIgZD0ibTAsMHYzMGg2MHYtMzB6Ii8+DQo8cGF0aCBzdHJva2U9IiNmZmYiIHN0cm9rZS13aWR0aD0iNiIgZD0ibTAsMGw2MCwzMG0wLTMwbC02MCwzMCIvPg0KPHBhdGggc3Ryb2tlPSIjY2YxNDJiIiBzdHJva2Utd2lkdGg9IjQiIGQ9Im0wLDBsNjAsMzBtMC0zMGwtNjAsMzAiIGNsaXAtcGF0aD0idXJsKCN0KSIvPg0KPHBhdGggc3Ryb2tlPSIjZmZmIiBzdHJva2Utd2lkdGg9IjEwIiBkPSJtMzAsMHYzMG0tMzAtMTVoNjAiLz4NCjxwYXRoIHN0cm9rZT0iI2NmMTQyYiIgc3Ryb2tlLXdpZHRoPSI2IiBkPSJtMzAsMHYzMG0tMzAtMTVoNjAiLz4NCjwvc3ZnPg0K">
</a>



:smoking: 在那些寂寞的日子里，有写作伴随着我。

## 特征

- 可以写书的博客引擎
- 适用于撰写多章节专栏文章
- SEO友好
- 内置全文本搜索
- 保存文章的历史版本（可浏览和可搜索）
- Markdown发表文章
- 邮件，Server酱 通知
- 多语言

## 指南

1. 在服务器中创建一个文件夹

    ```sh
    mkdir solitudes && cd solitudes
    ```

2. 创建 `docker-compose.yaml` 文件

    ```sh
    vi docker-compose.yaml
    ```
    按 `i` 进入编辑模式，复制并粘贴以下内容
    ```yaml
    version: '3.3'

    services:
      db:
        image: postgres:alpine
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
          - "80:8080"
        restart: always
        volumes:
          - ./blog-data:/solitudes/data
    ```
    
3. 创建数据文件夹
    ```sh
    mkdir -p data/solitudes
    ```
    
4. 创建 Solitudes 配置文件

    ```sh
    vi data/solitudes/conf.yml
    ```

    按 `i` 进入编辑模式，复制并粘贴以下内容

    ```yaml
    debug: true
    database: postgres://solitudes:thisispassword@db/solitudes?sslmode=disable
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

5. 启动

    ```sh
    docker-compose up -d
    ```

6. 在 Postgres 数据库中启用 `UUID` 扩展并在以下文件夹下执行：

    ```sh
    docker-compose exec db psql -U solitudes solitudes -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";'
    ```

7. 重新启动

    ```sh
    docker-compose restart solitudes
    ```

8. 打开 `http://yourdomain/login` 以登录到管理面板

    默认登录电子邮件：`hi@example.com`

    默认登录密码：123456

## 致谢

- 主题来自 [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)
- 管理面板界面 [AdminLTE](https://adminlte.io/)
- 全文搜索引擎 [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Hacker Pie @88250 [lute](https://github.com/88250/lute) & @Vanessa219 [Vditor](https://github.com/Vanessa219/vditor)
