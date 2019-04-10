# Solitudes

[![Go Report Card](https://goreportcard.com/badge/github.com/naiba/solitudes)](https://goreportcard.com/report/github.com/naiba/solitudes) [![Build Status](https://travis-ci.com/naiba/solitudes.svg?branch=master)](https://travis-ci.com/naiba/solitudes) [![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes.svg?type=shield)](https://app.fossa.io/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes?ref=badge_shield)
[![Size](https://images.microbadger.com/badges/image/naiba/solitudes.svg)](https://microbadger.com/images/naiba/solitudes) [![Pulls](https://img.shields.io/docker/pulls/naiba/solitudes.svg)](https://microbadger.com/images/naiba/solitudes)

:smoking: 在那些孤独的日子里，还有写作陪伴着我。

[![简体中文 README](https://img.shields.io/badge/简体中文-README-informational.svg)](README.md) [![English README](https://img.shields.io/badge/English-README-informational.svg)](README_en-US.md)

## 特色

- 一个极简博客引擎
- 适合长篇多章节文章写作（专栏）
- SEO 友好
- 内置全文搜索
- 文章历史版本保存（可浏览可被搜索）
- Markdown 发布文章
- 邮件、Server酱 通知
- 多语言支持

## 指南

1. 你的 postgres 服务器**必须**启用 `uuid` 扩展

    ```sql
    CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public;
    ```

2. 创建一个数据文件夹
3. 在 `path/to/data/conf.yml` 创建配置文件 (eg: `data/conf.yml`)
4. 在 docker 中部署

    ```shell
    docker run --name solitudes -p 8080:8080 -v /path/to/data:/solitudes/data naiba/solitudes
    ```

5. 打开 `https://yourdomain/login` 登录管理面板

## 提醒

有三处 hack，请留意

- yanyiwu/gojieba#46 英文单词分词问题。
- yanyiwu/gojieba `dep ensure` 没有将数据文件下载下来。
- yanyiwu/gojieba `getCurrentFilePath` 函数无法正常获取运行目录。

## 感谢

- 来自 [@probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus) 的主题
- 管理面板UI [@AdminLTE](https://adminlte.io/)
- 全文搜索引擎 [@blevesearch/bleve](https://github.com/blevesearch/bleve)

## 许可

[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fnaiba%2Fsolitudes?ref=badge_large)