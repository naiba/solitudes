# Solitudes

![构建状态](https://github.com/naiba/solitudes/workflows/Build%20Docker%20Image/badge.svg) <a href="README.md">
    <img height="20px" src="https://img.shields.io/badge/CN-flag.svg?color=555555&style=flat&logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAxMjAwIDgwMCIgeG1sbnM6eGxpbms9Imh0dHA6Ly93d3cudzMub3JnLzE5OTkveGxpbmsiPg0KPHBhdGggZmlsbD0iI2RlMjkxMCIgZD0ibTAsMGgxMjAwdjgwMGgtMTIwMHoiLz4NCjxwYXRoIGZpbGw9IiNmZmRlMDAiIGQ9Im0tMTYuNTc5Niw5OS42MDA3bDIuMzY4Ni04LjEwMzItNi45NTMtNC43ODgzIDguNDM4Ni0uMjUxNCAyLjQwNTMtOC4wOTI0IDIuODQ2Nyw3Ljk0NzkgOC40Mzk2LS4yMTMxLTYuNjc5Miw1LjE2MzQgMi44MTA2LDcuOTYwNy02Ljk3NDctNC43NTY3LTYuNzAyNSw1LjEzMzF6IiB0cmFuc2Zvcm09Im1hdHJpeCg5LjkzMzUyIC4yNzc0NyAtLjI3NzQ3IDkuOTMzNTIgMzI0LjI5MjUgLTY5NS4yNDE1KSIvPg0KPHBhdGggZmlsbD0iI2ZmZGUwMCIgaWQ9InN0YXIiIGQ9Im0zNjUuODU1MiwzMzIuNjg5NWwyOC4zMDY4LDExLjM3NTcgMTkuNjcyMi0yMy4zMTcxLTIuMDcxNiwzMC40MzY3IDI4LjI1NDksMTEuNTA0LTI5LjU4NzIsNy40MzUyLTIuMjA5NywzMC40MjY5LTE2LjIxNDItMjUuODQxNS0yOS42MjA2LDcuMzAwOSAxOS41NjYyLTIzLjQwNjEtMTYuMDk2OC0yNS45MTQ4eiIvPg0KPGcgZmlsbD0iI2ZmZGUwMCI+DQo8cGF0aCBkPSJtNTE5LjA3NzksMTc5LjMxMjlsLTMwLjA1MzQtNS4yNDE4LTE0LjM5NDUsMjYuODk3Ni00LjMwMTctMzAuMjAyMy0zMC4wMjkzLTUuMzc4MSAyNy4zOTQ4LTEzLjQyNDItNC4xNjQ3LTMwLjIyMTUgMjEuMjMyNiwyMS45MDU3IDI3LjQ1NTQtMTMuMjk5OC0xNC4yNzIzLDI2Ljk2MjcgMjEuMTMzMSwyMi4wMDE3eiIvPg0KPHBhdGggZD0ibTQ1NS4yNTkyLDMxNS45Nzk1bDkuMzczNC0yOS4wMzE0LTI0LjYzMjUtMTcuOTk3OCAzMC41MDctLjA1NjYgOS41MDUtMjguOTg4NiA5LjQ4MSwyOC45OTY0IDMwLjUwNywuMDgxOC0yNC42NDc0LDE3Ljk3NzQgOS4zNDkzLDI5LjAzOTItMjQuNzE0LTE3Ljg4NTgtMjQuNzI4OCwxNy44NjUzeiIvPg0KPC9nPg0KPHVzZSB4bGluazpocmVmPSIjc3RhciIgdHJhbnNmb3JtPSJtYXRyaXgoLjk5ODYzIC4wNTIzNCAtLjA1MjM0IC45OTg2MyAxOS40MDAwNSAtMzAwLjUzNjgxKSIvPg0KPC9zdmc+DQo=">
  </a>


:smoking: In those lonely days, writing accompanied me.

## Features

- A blog engine that can publish books
- Suitable for writing long multi-chapter articles (columns)
- SEO friendly
- Built-in full-text search
- Save historical versions of articles (browsable and searchable)
- Markdown publish article
- Mail, Server-Chan notification
- multi-language

## Guide

1. Create a folder in the server

    ```sh
    mkdir solitudes && cd solitudes
    ```

2. Create the `docker-compose.yaml` file

    ```sh
    vi solitudes
    ```
    Press `i` to enter the editing mode, copy and paste the following content
    ```yaml
    version: '3.3'

    services:
      db:
        image: postgres:13-alpine
        volumes:
          - ./data/db:/var/lib/postgresql/data
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
          - ./data/solitudes:/solitudes/data
    ```
    
3. Create data folder
    ```sh
    mkdir -p data/solitudes
    ```
    
4. Create Solitudes configuration file

    ```sh
    vi data/solitudes/conf.yml
    ```

    Press `i` to enter the editing mode, copy and paste the following content

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

5. Boot

    ```sh
    docker-compose up -d
    ```

6. Enable the `UUID` extension in the Postgres database and execute it under the folder:

    ```sh
    docker-compose exec db psql -U solitudes solitudes -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";'
    ```

7. Restart Solitudes

    ```sh
    docker-compose restart solitudes
    ```

8. Open `http://yourdomain/login` to log in to the management panel

    Default login email: `hi@example.com`

    Default login password: `123456`

## Thanks

- Theme from [probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)
- Admin Panel UI [AdminLTE](https://adminlte.io/)
- Full-text search engine [blevesearch/bleve](https://github.com/blevesearch/bleve)
- Hacker Pie @88250 [lute](https://github.com/88250/lute) & @Vanessa219 [Vditor](https://github.com/Vanessa219/vditor)