# Solitudes

:smoking:When I feel solitude, there is writing to accompany me.

## Features

- A died simple blog-engine
- Easy to build a book
- SEO friendly
- Full text search

## Quick start

1. clone repo to local gopath
2. into repo root & run `go build app/web/main.go`
3. copy binary file to your path
4. mkdir `data/upload` `data/conf.yml`
5. setting system like `data/conf.yml`

## Todo

- [ ] show article edit history
- [ ] dashboard rebuild search index
- [ ] comment like github
- [ ] file manager

## Notice

Has two hacks in current revolution.

- yanyiwu/gojieba#46 english word segmentation issue.
- yanyiwu/gojieba dep ensure not include `deps` and `dict` dir.

## Thanks

- theme from [@probberechts/hexo-theme-cactus](https://github.com/probberechts/hexo-theme-cactus)
- dashboard UI from [@AdminLTE](https://adminlte.io/)
- full text search engine [@blevesearch/bleve](https://github.com/blevesearch/bleve)