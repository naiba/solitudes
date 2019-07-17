# Solitudes

[![GolangCI](https://golangci.com/badges/github.com/naiba/solitudes.svg)](https://golangci.com/r/github.com/naiba/solitudes)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes.svg?type=small)](https://app.fossa.com/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes?ref=badge_small)
[![Actions Status](https://wdp9fww0r9.execute-api.us-west-2.amazonaws.com/production/badge/naiba/solitudes)](https://wdp9fww0r9.execute-api.us-west-2.amazonaws.com/production/badge/naiba/solitudes)
[![Size](https://images.microbadger.com/badges/image/naiba/solitudes.svg)](https://microbadger.com/images/naiba/solitudes)
[![Pulls](https://img.shields.io/docker/pulls/naiba/solitudes.svg)](https://microbadger.com/images/naiba/solitudes)

:smoking: In those days when I feel solitude, there is writing to accompany me.

[![简体中文 README](https://img.shields.io/badge/简体中文-README-informational.svg)](README.md) [![English README](https://img.shields.io/badge/English-README-informational.svg)](README_en-US.md)

## Features

- A blog engine that can publish books
- Easy to build a book
- SEO friendly
- Full text search
- Article history
- Markdown support
- Email, ServerChan notification
- i18n

## Quick start

1. exec this command under your postgres database to enable `uuid` plugin

    ```sql
    CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
    ```

2. create a data dir
3. create config file `path/to/data/conf.yml` (eg: `data/conf.yml`)
4. deploy on docker

    ```shell
    docker run --name solitudes -p 8080:8080 -v /path/to/data:/solitudes/data naiba/solitudes
    ```

5. open `https://yourdomain/login` to login Dashboard

## Thanks

- theme from [@probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)
- dashboard UI from [@AdminLTE](https://adminlte.io/)
- full text search engine [@go-ego/riot](https://github.com/go-ego/riot)

## License

[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes?ref=badge_large)
