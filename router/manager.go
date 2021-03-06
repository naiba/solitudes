package router

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func manager(c *fiber.Ctx) {
	var articleNum, commentNum int
	var lastArticle model.Article
	var lastComment model.Comment
	type tagNum struct {
		Count int
	}
	var tn tagNum

	var wg sync.WaitGroup
	wg.Add(5)
	checkPoolSubmit(&wg, solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Model(model.Article{}).Count(&articleNum)
		wg.Done()
	}))
	checkPoolSubmit(&wg, solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Model(model.Comment{}).Count(&commentNum)
		wg.Done()
	}))
	checkPoolSubmit(&wg, solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Raw(`select count(*) from (select tags,count(tags) from (select unnest(tags) as tags from articles) t group by tags) ts;`).Scan(&tn)
		wg.Done()
	}))
	checkPoolSubmit(&wg, solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Select("created_at").Order("created_at DESC").Take(&lastArticle)
		wg.Done()
	}))
	checkPoolSubmit(&wg, solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Select("created_at").Order("created_at DESC").Take(&lastComment)
		wg.Done()
	}))
	wg.Wait()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.Status(http.StatusOK).Render("admin/index", injectSiteData(c, fiber.Map{
		"title":              c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("dashboard"),
		"articleNum":         articleNum,
		"commentNum":         commentNum,
		"lastArticlePublish": fmt.Sprintf("%.2f", time.Since(lastArticle.CreatedAt).Hours()/24),
		"lastComment":        fmt.Sprintf("%.2f", time.Since(lastComment.CreatedAt).Hours()/24),
		"tagNum":             tn.Count,

		"memoryUsage": bToMb(m.Sys),
		"gcNum":       m.NumGC,
		"routineNum":  runtime.NumGoroutine(),
	}))
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

var validExtNames = map[string]interface{}{
	"jpg":  nil,
	"jpeg": nil,
	"png":  nil,
	"gif":  nil,
	"mp4":  nil,
	"zip":  nil,
	"rar":  nil,
}

var contentTypeList = map[string]string{
	"image/gif":  "gif",
	"image/png":  "png",
	"image/jpeg": "jpg",
}

type uploadResp struct {
	Msg  string `json:"msg,omitempty"`
	Code int    `json:"code"`
	Data struct {
		ErrFiles []string          `json:"errFiles,omitempty"`
		SuccMap  map[string]string `json:"succMap,omitempty"`
	} `json:"data,omitempty"`
}

func upload(c *fiber.Ctx) {
	form, err := c.MultipartForm()
	if err != nil {
		c.Status(http.StatusOK).JSON(uploadResp{
			Msg:  err.Error(),
			Code: http.StatusBadRequest,
		})
		return
	}

	var errfiles []string
	succMap := make(map[string]string)

	files := form.File["file[]"]
	for _, f := range files {
		fs := strings.Split(f.Filename, ".")
		if len(fs) < 2 {
			errfiles = append(errfiles, f.Filename)
			continue
		}
		extName := fs[len(fs)-1]
		if _, ok := validExtNames[extName]; !ok {
			errfiles = append(errfiles, f.Filename)
			continue
		}
		extName = fmt.Sprintf("/upload/%d.%s", time.Now().UnixNano(), extName)
		if err := c.SaveFile(f, "data"+extName); err != nil {
			errfiles = append(errfiles, f.Filename)
		} else {
			succMap[f.Filename] = extName
		}
	}
	c.Status(http.StatusOK).JSON(uploadResp{
		Code: 0,
		Data: struct {
			ErrFiles []string          "json:\"errFiles,omitempty\""
			SuccMap  map[string]string "json:\"succMap,omitempty\""
		}{
			ErrFiles: errfiles,
			SuccMap:  succMap,
		},
	})
}

type fetchRequest struct {
	URL string `json:"url,omitempty" validate:"required,min=11"`
}

type fetchResp struct {
	Msg  string `json:"msg,omitempty"`
	Code int    `json:"code"`
	Data struct {
		OriginalURL string `json:"originalURL,omitempty"`
		URL         string `json:"url,omitempty"`
	} `json:"data,omitempty"`
}

func fetch(c *fiber.Ctx) {
	var fr fetchRequest
	if err := c.BodyParser(&fr); err != nil {
		c.Status(http.StatusOK).JSON(fetchResp{
			Code: http.StatusBadRequest,
			Msg:  err.Error(),
		})
		return
	}
	if err := validator.StructCtx(c.Context(), &fr); err != nil {
		c.Status(http.StatusOK).JSON(fetchResp{
			Code: http.StatusBadRequest,
			Msg:  err.Error(),
		})
		return
	}

	// Get the data
	resp, err := http.Get(fr.URL)
	if err != nil {
		c.Status(http.StatusOK).JSON(fetchResp{
			Code: http.StatusBadRequest,
			Msg:  err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	var filename string
	contentType := resp.Header.Get("Content-Type")
	if ext, ok := contentTypeList[contentType]; ok {
		filename = fmt.Sprintf("/upload/%d.%s", time.Now().UnixNano(), ext)
		// Create the file
		out, err := os.Create("data/" + filename)
		if err != nil {
			c.Status(http.StatusOK).JSON(fetchResp{
				Code: http.StatusBadRequest,
				Msg:  err.Error(),
			})
			return
		}
		defer out.Close()

		// Write the body to file
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			c.Status(http.StatusOK).JSON(fetchResp{
				Code: http.StatusBadRequest,
				Msg:  err.Error(),
			})
			return
		}
	}

	c.Status(http.StatusOK).JSON(fetchResp{
		Code: 0,
		Data: struct {
			OriginalURL string "json:\"originalURL,omitempty\""
			URL         string "json:\"url,omitempty\""
		}{
			fr.URL,
			filename,
		},
	})
}

func rebuildFullTextSearch(c *fiber.Ctx) {
	solitudes.BuildArticleIndex()
}
