package main

// 桌面壳内嵌引擎：与 `imahub serve` 同一套能力（WebUI/REST/MCP/P2P 节点），
// 起在本机端口上供窗口与局域网节点共用——人人都是客户端，同时人人都是服务端。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
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
	"imahub/web"
)

// engine 一个完整节点引擎（serve 能力全量）。
type engine struct {
	url     string
	mux     *http.ServeMux
	httpSrv *http.Server
	ln      net.Listener
	stopLan chan struct{}
	drain   chan struct{}
	stopped bool
}

// boot 启动引擎：数据目录 + 监听端口（默认端口被占自动顺延）+ P2P 节点。
func boot(dataDir, addr, imaBase string) (*engine, error) {
	if imaBase == "" {
		imaBase = "https://ima.qq.com"
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录: %w", err)
	}
	st, err := store.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("打开数据目录: %w", err)
	}
	srv := &api.Server{
		Store:      st,
		ImaBaseURL: imaBase,
		TmpDir:     filepath.Join(dataDir, "tmp"),
		Scanner:    scan.Open(dataDir, st, imaBase),
		Mode:       "serve",
	}
	srv.MCP = &mcp.Server{Store: st, ImaBaseURL: imaBase, TmpDir: srv.TmpDir}
	if err := os.MkdirAll(srv.TmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建临时目录: %w", err)
	}

	// —— P2P 节点身份与局域网发现 ——
	hostNick, _ := os.Hostname()
	pid, err := peer.LoadOrCreate(dataDir, hostNick)
	if err != nil {
		return nil, fmt.Errorf("加载节点身份: %w", err)
	}
	ptable, err := peer.OpenTable(dataDir)
	if err != nil {
		return nil, fmt.Errorf("加载节点表: %w", err)
	}
	pob, err := peer.OpenOutbox(dataDir)
	if err != nil {
		return nil, fmt.Errorf("加载投递队列: %w", err)
	}
	srv.PeerIdentity = pid
	srv.PeerTable = ptable
	srv.PeerLanPort = peer.LanPort()
	srv.PeerOutbox = pob

	// 监听端口：默认端口被占自动顺延（桌面上 8090 常被别的服务占）
	ln, err := listenWithFallback(addr)
	if err != nil {
		return nil, err
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	apiPort, _ := strconv.Atoi(portStr)
	srv.PeerAPIPort = apiPort

	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Handler())
	mux.Handle("/mcp", srv.MCP)
	mux.Handle("/mcp/", srv.MCP)
	mux.Handle("/healthz", srv.Handler())
	mux.Handle("/", web.SPAHandler())

	e := &engine{
		url:     "http://127.0.0.1:" + portStr,
		mux:     mux,
		ln:      ln,
		stopLan: make(chan struct{}),
		drain:   make(chan struct{}),
		httpSrv: &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second},
	}
	go peer.RunLAN(pid, ptable, apiPort, func(p peer.Peer) { pob.RetryNow(p.ID); pob.Kick() }, e.stopLan) // 发现即补投
	go peer.RunOutbox(e.stopLan, pob, ptable, pid, apiPort, 5*time.Second)                                // 离线补投
	go srv.RunQueueDrainer(e.drain)
	go func() {
		if err := e.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP 服务异常: %v", err)
		}
	}()
	log.Printf("节点 ID: %s（%s）· 引擎 %s · 局域网发现 %d/udp", peer.FormatID(pid.NodeID()), pid.Nick(), e.url, srv.PeerLanPort)
	return e, nil
}

// listenWithFallback 按给定地址监听；默认 :8090 被占时顺延 8091–8119，再不行随机端口。
// 桌面端口以实见为准宣告（P2P 直连用真实端口）。
func listenWithFallback(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	_, portStr, splitErr := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	if splitErr != nil || port == 8090 {
		for p := 8091; p <= 8119; p++ {
			if ln, err := net.Listen("tcp", ":"+strconv.Itoa(p)); err == nil {
				log.Printf("端口 %d 被占用，改用 %d", port, p)
				return ln, nil
			}
		}
	}
	return net.Listen("tcp", "127.0.0.1:0") // 最后兜底：随机端口
}

// selftest 自检：引擎起在任意端口，HTTP 断言后即可退出（打包流水线用）。
func (e *engine) selftest() error {
	cli := &http.Client{Timeout: 5 * time.Second}
	resp, err := cli.Get(e.url + "/healthz")
	if err != nil {
		return fmt.Errorf("healthz 不可达: %w", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var h struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(raw, &h); err != nil || !h.OK {
		return fmt.Errorf("healthz 响应异常: %s", string(raw))
	}
	resp2, err := cli.Get(e.url + "/api/server-info")
	if err != nil {
		return fmt.Errorf("server-info 不可达: %w", err)
	}
	raw2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	var si struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw2, &si); err != nil || si.Data.Version != api.Version {
		return fmt.Errorf("版本不符: %s", strings.TrimSpace(string(raw2)))
	}
	resp3, err := cli.Get(e.url + "/")
	if err != nil {
		return fmt.Errorf("WebUI 不可达: %w", err)
	}
	raw3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	if !strings.Contains(string(raw3), "<div id=") {
		return fmt.Errorf("WebUI 首页异常: %.80s", string(raw3))
	}
	return nil
}

// wait 前台等待退出信号（无窗口/服务模式）。
func (e *engine) wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}

// stop 优雅收尾：HTTP → P2P → 上传队列。
func (e *engine) stop() {
	if e.stopped {
		return
	}
	e.stopped = true
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := e.httpSrv.Shutdown(ctx); err != nil {
		log.Printf("优雅退出未完成（强制关闭）: %v", err)
	}
	close(e.stopLan)
	close(e.drain)
}
