// ImaHub —— 腾讯 ima 开放平台的多用户 Web 网关。
// 单二进制：内嵌 WebUI，JSON 存储，自带 MCP 入口。
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"imahub/internal/api"
	"imahub/internal/mcp"
	"imahub/internal/scan"
	"imahub/internal/store"
)

//go:embed web
var webFS embed.FS

func main() {
	addr := flag.String("addr", ":8090", "监听地址（如 :8090、0.0.0.0:8080）")
	dataDir := flag.String("data", "./data", "数据目录（用户/会话/设置）")
	imaBase := flag.String("ima-base-url", "", "ima 开放平台地址（默认 https://ima.qq.com，测试用）")
	flag.Parse()

	if *imaBase == "" {
		*imaBase = "https://ima.qq.com"
	}

	st, err := store.Open(*dataDir)
	if err != nil {
		log.Fatalf("打开数据目录失败: %v", err)
	}

	srv := &api.Server{
		Store:      st,
		ImaBaseURL: *imaBase,
		TmpDir:     filepath.Join(*dataDir, "tmp"),
		Scanner:    scan.Open(*dataDir, st, *imaBase),
	}
	srv.MCP = &mcp.Server{
		Store:      st,
		ImaBaseURL: *imaBase,
		TmpDir:     srv.TmpDir,
	}
	_ = os.MkdirAll(srv.TmpDir, 0o755)

	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Handler())
	mux.Handle("/mcp", srv.MCP)
	mux.Handle("/healthz", srv.Handler())

	// 静态 WebUI（内嵌），SPA 回退到 index.html
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("内嵌资源失败: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("/", spaHandler(sub, fileServer))

	httpSrv := &http.Server{Addr: *addr, Handler: mux}

	go func() {
		log.Printf("ImaHub v%s 已启动: http://localhost%s  (数据目录 %s)", api.Version, *addr, *dataDir)
		if st.UserCount() == 0 {
			log.Printf("首次使用：打开页面完成管理员初始化")
		}
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Println("正在退出…")
	_ = httpSrv.Close()
}

// spaHandler 静态资源优先；未命中的路径回退 index.html。
func spaHandler(sub fs.FS, fileServer http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != "/" {
			if _, err := fs.Stat(sub, filepath.Clean(strings.TrimPrefix(p, "/"))); err != nil {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
