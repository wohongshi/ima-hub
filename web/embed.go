// Package web 内嵌 WebUI 静态资源——服务端二进制与桌面壳共用同一份产物。
package web

import (
	"embed"
	"hash/fnv"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// FS web/ 目录下的前端资源（html/js/css）。
//
//go:embed *.html *.js *.css
var FS embed.FS

// SPAHandler 静态资源优先（ETag + must-revalidate）；未命中的路径回退 index.html。
func SPAHandler() http.Handler {
	fileServer := http.FileServer(http.FS(FS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != "/" {
			// fs.FS 路径恒为斜杠分隔，用 path.Clean（filepath 在 Windows 上会转反斜杠导致永远查不到）
			if _, err := fs.Stat(FS, path.Clean(strings.TrimPrefix(p, "/"))); err != nil {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		if isStaticAsset(p) {
			// ETag + must-revalidate：内容不变回 304 省流量，发版即生效（无需版本号簿记）
			if data, err := fs.ReadFile(FS, path.Clean(strings.TrimPrefix(p, "/"))); err == nil {
				h := fnv.New32a()
				h.Write(data)
				etag := `"` + strconv.FormatUint(uint64(h.Sum32()), 16) + `"`
				w.Header().Set("ETag", etag)
				w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
				if r.Header.Get("If-None-Match") == etag {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// isStaticAsset 可指纹缓存的静态资源（HTML 始终 no-cache）。
func isStaticAsset(p string) bool {
	for _, ext := range []string{".js", ".css", ".png", ".svg", ".ico", ".jpg", ".jpeg", ".gif", ".woff", ".woff2"} {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}
