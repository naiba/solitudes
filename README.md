# Solitudes

[![Go Report Card](https://goreportcard.com/badge/github.com/naiba/solitudes)](https://goreportcard.com/report/github.com/naiba/solitudes) [![Build Status](https://travis-ci.com/naiba/solitudes.svg?branch=master)](https://travis-ci.com/naiba/solitudes) [![](https://images.microbadger.com/badges/image/naiba/solitudes.svg)](https://microbadger.com/images/naiba/solitudes) [![](https://img.shields.io/docker/pulls/naiba/solitudes.svg)](https://microbadger.com/images/naiba/solitudes)

:smoking:When I feel solitude, there is writing to accompany me.

## Features

- A died simple blog-engine
- Easy to build a book
- SEO friendly
- Full text search

## Quick start

1. create a data dir
2. setting up config file like `/data/conf.yml`
3. deploy on docker

    ```shell
    docker run --name solitudes -p 8080:8080 -v /path/to/data:/solitudes/data naiba/solitudes
    ```

## Todo

- [x] docker deploy
- [x] multi level chapter
- [x] safe cache
- [x] show article edit history
    - [x] search page: hide old version article
    - [x] show edit history handler
- [x] media manager
- [ ] comment manager

## Notice

Has three hacks in current revolution.

- yanyiwu/gojieba#46 english word segmentation issue.
- yanyiwu/gojieba dep ensure not include `deps` and `dict` dir.
- yanyiwu/gojieba getCurrentFilePath func can't get running path

## Thanks

- theme from [@probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)
- dashboard UI from [@AdminLTE](https://adminlte.io/)
- full text search engine [@blevesearch/bleve](https://github.com/blevesearch/bleve)