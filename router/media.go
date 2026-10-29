package router

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func validateMediaFilename(name string) (string, error) {
	if name == "" {
		return "", errors.New("missing filename")
	}
	if name == "." || name == ".." || filepath.Base(name) != name ||
		strings.ContainsAny(name, `/\`+"\x00") {
		return "", errors.New("invalid filename")
	}
	return name, nil
}

func mediaHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	cleanName, err := validateMediaFilename(queryValue(httpRequest, "name"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, err.Error())
	}
	root, err := os.OpenRoot("data/upload")
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(cleanName)
}

type mediaInfo struct {
	Filename   string
	Article    model.Article
	UploadedAt time.Time
}

var errEnded = errors.New("file walk eneded")

func media(writer http.ResponseWriter, httpRequest *http.Request) error {
	page, err := listPage(queryValue(httpRequest, "page"))
	if err != nil {
		return err
	}
	var files []os.FileInfo
	start := (page - 1) * 15
	end := page*15 + 1
	fileIndex := 0
	err = filepath.Walk("data/upload", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == "data/upload" {
			return nil
		}
		if info.IsDir() {
			return filepath.SkipDir
		}
		if fileIndex >= start && fileIndex < end {
			files = append(files, info)
		}
		fileIndex++
		if fileIndex >= end {
			return errEnded
		}
		return nil
	})
	// A fresh installation may not have an upload directory yet. Treat it as
	// an empty library instead of making the entire media page a 500.
	if err != nil && err != errEnded && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	more := len(files) > 15
	if more {
		files = files[:15]
	}
	var innerMedias []mediaInfo
	for _, f := range files {
		var item mediaInfo
		item.UploadedAt = f.ModTime()
		item.Filename = f.Name()
		if err := solitudes.System.DB.Take(&item.Article, "content like ?", "%/upload/"+item.Filename+"%").Error; err == gorm.ErrRecordNotFound {
			var ah model.ArticleHistory
			if solitudes.System.DB.Take(&ah, "content like ?", "%/upload/"+item.Filename+"%").Error == nil {
				solitudes.System.DB.Take(&item.Article, "id = ?", ah.ArticleID)
				item.Article.Version = ah.Version
			}
		}
		innerMedias = append(innerMedias, item)
	}
	return renderPage(writer, httpRequest, http.StatusOK, "admin/media", injectSiteData(writer, httpRequest, viewData{
		"title":      requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator).T("manage_media"),
		"medias":     innerMedias,
		"page":       page,
		"navigation": pageNavigationFor(writer, httpRequest, "page", page, more, ""),
	}))

}
