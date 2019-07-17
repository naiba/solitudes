# Solitudes

[![GolangCI](https://golangci.com/badges/github.com/naiba/solitudes.svg)](https://golangci.com/r/github.com/naiba/solitudes)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes.svg?type=small)](https://app.fossa.com/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes?ref=badge_small)
[![Actions Status](https://wdp9fww0r9.execute-api.us-west-2.amazonaws.com/production/badge/naiba/solitudes)](https://wdp9fww0r9.execute-api.us-west-2.amazonaws.com/production/badge/naiba/solitudes)
[![Size](https://images.microbadger.com/badges/image/naiba/solitudes.svg)](https://microbadger.com/images/naiba/solitudes)
[![Pulls](https://img.shields.io/docker/pulls/naiba/solitudes.svg)](https://microbadger.com/images/naiba/solitudes)

:smoking: 在那些孤独的日子里，还有写作陪伴着我。

[![简体中文 README](https://img.shields.io/badge/简体中文-README-informational.svg)](README.md) [![English README](https://img.shields.io/badge/English-README-informational.svg)](README_en-US.md)

## 特色

- 一个可以出书的博客引擎
- 适合长篇多章节文章写作（专栏）
- SEO 友好
- 内置全文搜索
- 文章历史版本保存（可浏览可被搜索）
- Markdown 发布文章
- 邮件、Server酱 通知
- 多语言支持

## 指南

1. 在 postgres 数据库执行以下命令，启用 `uuid` 扩展

    ```sql
    CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
    ```

2. 创建一个数据文件夹
3. 在 `path/to/data/conf.yml` 创建配置文件 (eg: `data/conf.yml`)
4. 在 docker 中部署

    ```shell
    docker run --name solitudes -p 8080:8080 -v /path/to/data:/solitudes/data naiba/solitudes
    ```

5. 打开 `https://yourdomain/login` 登录管理面板

## 感谢

- 来自 [@probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus) 的主题
- 管理面板UI [@AdminLTE](https://adminlte.io/)
- 全文搜索引擎 [@go-ego/riot](https://github.com/go-ego/riot)

## 许可

[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes?ref=badge_large)
