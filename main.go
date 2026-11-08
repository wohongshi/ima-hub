// ImaHub —— 腾讯 ima 开放平台的多用户 Web 网关。
// 单二进制：内嵌 WebUI，JSON 存储，自带 MCP 入口。
package main

import (
	"context"
	"embed"
	"flag"
	"hash/fnv"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"imahub/internal/api"
	"imahub/internal/mcp"
	"imahub/internal/peer"
	"imahub/internal/scan"
	"imahub/internal/store"
)

//go:embed web
var webFS embed.FS

func main() {
	// 双模式：imahub serve（默认，全功能服务端）| imahub client（组员客户端：直连 + 离线同步）
	mode := "serve"
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "client" || args[0] == "serve") {
		mode = args[0]
		args = args[1:]
	}
	defaultAddr := ":8090"
	if mode == "client" {
		defaultAddr = "127.0.0.1:8090" // 客户端默认只听本机
	}
	fl := flag.NewFlagSet("imahub", flag.ExitOnError)
	addr := fl.String("addr", defaultAddr, "监听地址（如 :8090、127.0.0.1:8090）")
	dataDir := fl.String("data", "./data", "数据目录（用户/会话/设置）")
	imaBase := fl.String("ima-base-url", "", "ima 开放平台地址（默认 https://ima.qq.com，测试用）")
	server := fl.String("server", "", "客户端模式：服务端地址（可留空，页面里填）")
	fl.Parse(args)

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
		Mode:       mode,
	}
	if mode == "client" {
		if *server != "" {
			_ = st.UpdateSettings(func(s *store.Settings) error {
				s.ClientServerURL = *server
				return nil
			})
		}
		srv.Link = api.NewClientLink(srv)
	}
	srv.MCP = &mcp.Server{
		Store:      st,
		ImaBaseURL: *imaBase,
		TmpDir:     srv.TmpDir,
	}
	if err := os.MkdirAll(srv.TmpDir, 0o755); err != nil {
		log.Fatalf("创建临时目录失败: %v", err)
	}

	// —— P2P 节点身份与局域网发现（去中心化互联） ——
	hostNick, _ := os.Hostname()
	pid, err := peer.LoadOrCreate(*dataDir, hostNick)
	if err != nil {
		log.Fatalf("加载节点身份失败: %v", err)
	}
	ptable, err := peer.OpenTable(*dataDir)
	if err != nil {
		log.Fatalf("加载节点表失败: %v", err)
	}
	srv.PeerIdentity = pid
	srv.PeerTable = ptable
	srv.PeerLanPort = peer.LanPort()
	// 宣告里的直连端口 = 本服务 HTTP 端口（对方经 /api/peer/rpc 找我们）
	apiPort := 8090
	if _, p, err := net.SplitHostPort(*addr); err == nil {
		if n, err := strconv.Atoi(p); err == nil {
			apiPort = n
		}
	}
	pob, err := peer.OpenOutbox(*dataDir)
	if err != nil {
		log.Fatalf("加载离线投递队列失败: %v", err)
	}
	srv.PeerOutbox = pob
	stopLan := make(chan struct{})
	go peer.RunLAN(pid, ptable, apiPort, func(p peer.Peer) { pob.RetryNow(p.ID); pob.Kick() }, stopLan) // 发现即补投
	go peer.RunOutbox(stopLan, pob, ptable, pid, apiPort, 5*time.Second)                                // 离线补投：掉线消息自动重试
	defer close(stopLan)
	log.Printf("节点 ID: %s（%s）· 局域网发现 %d/udp · 直连 %d/tcp", peer.FormatID(pid.NodeID()), pid.Nickname, srv.PeerLanPort, apiPort)

	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Handler())
	mux.Handle("/mcp", srv.MCP)
	mux.Handle("/mcp/", srv.MCP) // 带尾斜杠别落到 SPA
	mux.Handle("/healthz", srv.Handler())

	// 静态 WebUI（内嵌），SPA 回退到 index.html
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("内嵌资源失败: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("/", spaHandler(sub, fileServer))

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		display := *addr
		if strings.HasPrefix(display, ":") {
			display = "localhost" + display
		}
		log.Printf("ImaHub v%s [%s] 已启动: http://%s  (数据目录 %s)", api.Version, mode, display, *dataDir)
		if mode == "client" {
			_, serverURL, _ := srv.Link.Status()
			if serverURL == "" {
				log.Printf("客户端首连：打开页面填写服务端地址与账号")
			}
		} else if st.UserCount() == 0 {
			log.Printf("首次使用：打开页面完成管理员初始化")
		}
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	if mode == "client" {
		stopSync := make(chan struct{})
		go srv.Link.Run(stopSync) // 心跳 + 离线数据同步引擎
		defer close(stopSync)
	}

	// 上传队列自动续传（双模式；离线/失败的文件恢复后静默补传）
	stopDrain := make(chan struct{})
	go srv.RunQueueDrainer(stopDrain)
	defer close(stopDrain)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Println("正在退出…")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("优雅退出未完成（强制关闭）: %v", err)
	}
}

// spaHandler 静态资源优先；未命中的路径回退 index.html。
func spaHandler(sub fs.FS, fileServer http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != "/" {
			// fs.FS 路径恒为斜杠分隔，用 path.Clean（filepath 在 Windows 上会转反斜杠导致永远查不到）
			if _, err := fs.Stat(sub, path.Clean(strings.TrimPrefix(p, "/"))); err != nil {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		if isStaticAsset(p) {
			// ETag + must-revalidate：内容不变回 304 省流量，发版即生效（无需版本号簿记）
			if data, err := fs.ReadFile(sub, path.Clean(strings.TrimPrefix(p, "/"))); err == nil {
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
