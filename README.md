# Solitudes

:smoking: In those lonely days, writing accompanied me.

## Features

- A blog engine that can publish books
- Suitable for writing long multi-chapter articles (columns)
- SEO friendly
- Built-in full-text search
- Save historical versions of articles (browsable and searchable)
- Markdown publish article
- Mail, Server sauce notification
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
    
2. Create data folder
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

5. Boot environment

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