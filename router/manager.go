package router

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-uuid"
	"golang.org/x/sync/errgroup"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func manager(c *fiber.Ctx) error {
	var articleNum, commentNum int64
	var lastArticle model.Article
	var lastComment model.Comment
	type tagNum struct {
		Count int
	}
	var tn tagNum

	var g errgroup.Group
	g.Go(func() error {
		return solitudes.System.DB.Model(model.Article{}).Count(&articleNum).Error
	})
	g.Go(func() error {
		return solitudes.System.DB.Model(model.Comment{}).Count(&commentNum).Error
	})
	g.Go(func() error {
		return solitudes.System.DB.Raw(`select count(*) from (select tags,count(tags) from (select unnest(tags) as tags from articles) t group by tags) ts;`).Scan(&tn).Error
	})
	g.Go(func() error {
		return solitudes.System.DB.Select("created_at").Order("created_at DESC").Take(&lastArticle).Error
	})
	g.Go(func() error {
		return solitudes.System.DB.Select("created_at").Order("created_at DESC").Take(&lastComment).Error
	})
	var rssSubscriberCount int64
	type rssSubscriber struct {
		IP    string
		Count int64
	}
	var rssSubscribers []rssSubscriber
	g.Go(func() error {
		oneDayAgo := time.Now().Add(-24 * time.Hour)
		if err := solitudes.System.DB.Raw(`
			SELECT COUNT(*) FROM (
				SELECT ip, COUNT(*) as cnt
				FROM feed_visits
				WHERE created_at > ?
				GROUP BY ip
				HAVING COUNT(*) >= ?
			) t
		`, oneDayAgo, 3).Scan(&rssSubscriberCount).Error; err != nil {
			return err
		}
		return solitudes.System.DB.Raw(`
			SELECT ip, COUNT(*) as count
			FROM feed_visits
			WHERE created_at > ?
			GROUP BY ip
			ORDER BY count DESC
			LIMIT 4
		`, oneDayAgo).Scan(&rssSubscribers).Error
	})
	_ = g.Wait()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.Status(http.StatusOK).Render("admin/index", injectSiteData(c, fiber.Map{
		"title":              c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("dashboard"),
		"articleNum":         articleNum,
		"commentNum":         commentNum,
		"lastArticlePublish": fmt.Sprintf("%.2f", time.Since(lastArticle.CreatedAt).Hours()/24),
		"lastComment":        fmt.Sprintf("%.2f", time.Since(lastComment.CreatedAt).Hours()/24),
		"tagNum":             tn.Count,
		"rssSubscriberCount": rssSubscriberCount,
		"rssSubscribers":     rssSubscribers,

		"memoryUsage": bToMb(m.Sys),
		"gcNum":       m.NumGC,
		"routineNum":  runtime.NumGoroutine(),
	}))
	return nil
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

var validExtNames = map[string]string{
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"gif":  "image/gif",
	"mp4":  "video/mp4",
	"zip":  "application/zip",
	"rar":  "application/x-rar-compressed",
	"wav":  "audio/wav",
	"mp3":  "audio/mpeg",
}

var contentTypeList = map[string]string{
	"image/gif":  "gif",
	"image/png":  "png",
	"image/jpeg": "jpg",
}

const maxUploadSize = 50 * 1024 * 1024 // 50MB

type uploadResp struct {
	Msg  string `json:"msg,omitempty"`
	Code int    `json:"code"`
	Data struct {
		ErrFiles []string          `json:"errFiles,omitempty"`
		SuccMap  map[string]string `json:"succMap,omitempty"`
	} `json:"data,omitempty"`
}

func upload(c *fiber.Ctx) error {
	form, err := c.MultipartForm()
	if err != nil {
		c.Status(http.StatusOK).JSON(uploadResp{
			Msg:  err.Error(),
			Code: http.StatusBadRequest,
		})
		return err
	}

	var errfiles []string
	succMap := make(map[string]string)

	files := form.File["file[]"]
	for _, f := range files {
		if f.Size > maxUploadSize {
			errfiles = append(errfiles, f.Filename)
			continue
		}
		fs := strings.Split(f.Filename, ".")
		if len(fs) < 2 {
			errfiles = append(errfiles, f.Filename)
			continue
		}
		extName := strings.ToLower(fs[len(fs)-1])
		expectedMIME, ok := validExtNames[extName]
		if !ok {
			errfiles = append(errfiles, f.Filename)
			continue
		}
		contentType := f.Header.Get("Content-Type")
		if contentType != "" && !strings.HasPrefix(contentType, expectedMIME) && !strings.HasPrefix(contentType, "application/octet-stream") {
			errfiles = append(errfiles, f.Filename)
			continue
		}
		fid, err := uuid.GenerateUUID()
		if err != nil {
			return err
		}
		savePath := fmt.Sprintf("/upload/%s.%s", fid, extName)
		if err := c.SaveFile(f, "data"+savePath); err != nil {
			errfiles = append(errfiles, f.Filename)
		} else {
			succMap[f.Filename] = savePath
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
	return nil
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

func fetch(c *fiber.Ctx) error {
	var fr fetchRequest
	if err := c.BodyParser(&fr); err != nil {
		c.Status(http.StatusOK).JSON(fetchResp{
			Code: http.StatusBadRequest,
			Msg:  err.Error(),
		})
		return err
	}
	if err := validator.StructCtx(c.Context(), &fr); err != nil {
		c.Status(http.StatusOK).JSON(fetchResp{
			Code: http.StatusBadRequest,
			Msg:  err.Error(),
		})
		return err
	}

	fid, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}

	// Get the data
	resp, err := http.Get(fr.URL)
	if err != nil {
		c.Status(http.StatusOK).JSON(fetchResp{
			Code: http.StatusBadRequest,
			Msg:  err.Error(),
		})
		return err
	}
	defer resp.Body.Close()

	var filename string
	contentType := resp.Header.Get("Content-Type")
	if ext, ok := contentTypeList[contentType]; ok {
		filename = fmt.Sprintf("/upload/%s.%s", fid, ext)
		// Create the file
		out, err := os.Create("data/" + filename)
		if err != nil {
			c.Status(http.StatusOK).JSON(fetchResp{
				Code: http.StatusBadRequest,
				Msg:  err.Error(),
			})
			return err
		}
		defer out.Close()

		// Write the body to file
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			c.Status(http.StatusOK).JSON(fetchResp{
				Code: http.StatusBadRequest,
				Msg:  err.Error(),
			})
			return err
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
	return nil
}

func rebuildFullTextSearch(c *fiber.Ctx) error {
	solitudes.BuildArticleIndex()
	return nil
}
