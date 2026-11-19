package router

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

// Every site template links /logo.png and /favicon.ico (icons, social cards,
// structured data). Without an uploaded file the handlers fall back to the
// active theme, so each bundled theme must ship both: Folio once had neither
// and answered 404 for its own icons on a fresh installation.
func TestBundledSiteThemesServeFallbackIcons(t *testing.T) {
	t.Chdir("..")
	themes, err := filepath.Glob(filepath.Join("resource", "themes", "site", "*", "metadata.json"))
	if err != nil || len(themes) == 0 {
		t.Fatalf("list site themes: %v", err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	for _, metadata := range themes {
		theme := filepath.Base(filepath.Dir(metadata))
		t.Run(theme, func(t *testing.T) {
			cfg := &model.Config{}
			cfg.Site.Theme = theme
			cfg.Admin.Theme = "default"
			solitudes.System = &solitudes.SysVariable{Config: cfg}
			translator.Init()
			app := newTestRouter(testHTTPOptions{Views: themeStaticTestViews{}})
			app.Get("/logo.png", logoHandler)
			app.Get("/favicon.ico", faviconHandler)

			fetch := func(path string) []byte {
				t.Helper()
				resp, err := testRequest(app, httptest.NewRequest(http.MethodGet, path, nil))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("%s status = %d; the theme must ship a fallback", path, resp.StatusCode)
				}
				var body bytes.Buffer
				if _, err := body.ReadFrom(resp.Body); err != nil {
					t.Fatal(err)
				}
				return body.Bytes()
			}

			logo, err := png.DecodeConfig(bytes.NewReader(fetch("/logo.png")))
			if err != nil {
				t.Fatalf("logo.png is not a PNG image: %v", err)
			}
			// Social cards and touch icons upscale anything smaller into a blur.
			if logo.Width != logo.Height || logo.Width < 192 {
				t.Errorf("logo.png is %dx%d; want a square of at least 192px", logo.Width, logo.Height)
			}

			icon := fetch("/favicon.ico")
			if len(icon) < 22 || binary.LittleEndian.Uint16(icon[0:2]) != 0 || binary.LittleEndian.Uint16(icon[2:4]) != 1 {
				t.Fatal("favicon.ico is not an ICO container")
			}
			images := int(binary.LittleEndian.Uint16(icon[4:6]))
			if images == 0 {
				t.Fatal("favicon.ico contains no images")
			}
			for i := 0; i < images; i++ {
				entry := icon[6+16*i:]
				size, offset := binary.LittleEndian.Uint32(entry[8:12]), binary.LittleEndian.Uint32(entry[12:16])
				if size == 0 || int(offset)+int(size) > len(icon) {
					t.Errorf("favicon.ico image %d points outside the file", i)
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join("data", "upload", "logo.png")); err == nil {
		t.Log("a local upload shadows the theme fallback; this run did not exercise it")
	}
}
