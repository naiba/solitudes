package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
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
		var err error
		rssSubscriberCount, err = countFeedSubscribers()
		if err != nil {
			return err
		}
		oneDayAgo := time.Now().Add(-24 * time.Hour)
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

var errUnsafeFetchURL = errors.New("remote image URL must resolve to a public HTTP(S) address")

var blockedFetchNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func publicFetchIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	for _, blocked := range blockedFetchNetworks {
		if blocked.Contains(addr) {
			return false
		}
	}
	return true
}

func validateFetchURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errUnsafeFetchURL
	}
	return nil
}

func fetchImage(ctx context.Context, raw string) ([]byte, string, error) {
	if err := validateFetchURL(raw); err != nil {
		return nil, "", err
	}
	transport := &http.Transport{
		// Never use environment proxies for user-provided URLs.
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errUnsafeFetchURL
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(addresses) == 0 {
				return nil, errUnsafeFetchURL
			}
			for _, addr := range addresses {
				if !publicFetchIP(addr.IP) {
					return nil, errUnsafeFetchURL
				}
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errUnsafeFetchURL
			}
			return validateFetchURL(req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("remote image returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxUploadSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxUploadSize {
		return nil, "", errors.New("remote image exceeds upload limit")
	}
	ext, ok := contentTypeList[http.DetectContentType(data)]
	if !ok {
		return nil, "", errors.New("remote file is not a supported image")
	}
	return data, ext, nil
}

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

	data, ext, err := fetchImage(c.UserContext(), fr.URL)
	if err != nil {
		return fiber.NewError(http.StatusBadRequest, err.Error())
	}
	filename := fmt.Sprintf("/upload/%s.%s", fid, ext)
	if err := os.WriteFile("data"+filename, data, 0o644); err != nil {
		return err
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
