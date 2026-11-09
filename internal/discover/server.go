// Package discover —— 公网发现 + 中继服务器（imahub discover 单命令部署）。
// 职责三件：①地址目录（节点 announce 自己的可达地址，他人查询）②中继信箱
// （直连失败时转发加密信封，端到端密文不落地解析）③在线状态（TTL 心跳）。
// 无状态设计：内存表 + 周期清理，重启丢注册由节点 30s 心跳自动补齐。
// 安全模型：目录仅暴露「节点ID→地址」元数据；信封本身 Ed25519 签名 + X25519 加密，
// 中继即使被旁路也读不到内容；建议部署时挂 HTTPS 反代。
package discover

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// entry 一个节点的注册信息。
type entry struct {
	NodeID       string   `json:"node_id"`
	XPub         string   `json:"xpub"` // X25519 公钥（信封加密用）
	Pub          string   `json:"pub"`  // Ed25519 公钥 hex（验签）
	Addrs        []string `json:"addrs"`
	ObservedAddr string   `json:"observed_addr,omitempty"` // 服务端观测到的来源地址（NAT 候选出口）
	TS           int64    `json:"ts"`
	Sig          string   `json:"sig"`
	UpdatedAt    int64    `json:"updated_at"`
}

// announceReq announce/心跳载荷（Sig = Sign(priv, "announce|node|addr1,addr2|ts")）。
type announceReq struct {
	entry
}

// relayMsg 信箱消息（不透明信封）。
type relayMsg struct {
	From string          `json:"from"`
	Body json.RawMessage `json:"body"`
	At   int64           `json:"at"`
}

// Server 发现/中继服务（内存态）。
type Server struct {
	mu      sync.Mutex
	entries map[string]entry
	mailbox map[string][]relayMsg
	maxBox  int
	ttl     time.Duration
}

// New 创建服务实例。
func New() *Server {
	return &Server{
		entries: map[string]entry{},
		mailbox: map[string][]relayMsg{},
		maxBox:  512,
		ttl:     5 * time.Minute,
	}
}

// Handler HTTP 路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"service":"imahub-discover"}`))
	})
	mux.HandleFunc("POST /announce", s.handleAnnounce)
	mux.HandleFunc("GET /peers", s.handlePeers)
	mux.HandleFunc("POST /relay/", s.handleRelayPush)
	mux.HandleFunc("GET /relay/inbox", s.handleRelayInbox)
	mux.HandleFunc("POST /relay/ack", s.handleRelayAck)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// handleAnnounce 节点注册/心跳（验签防伪造：地址变动必须由本人签名）。
func (s *Server) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	var req announceReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "载荷非法"})
		return
	}
	if req.NodeID == "" || req.TS == 0 || len(req.Addrs) == 0 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "字段缺失"})
		return
	}
	// 时间窗 ±300s 防重放
	now := time.Now().Unix()
	if req.TS < now-300 || req.TS > now+300 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "时间戳超窗"})
		return
	}
	// 验签：announce|node|addr,addr|ts
	pub, err := hex.DecodeString(req.Pub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "公钥非法"})
		return
	}
	sig, err := hex.DecodeString(req.Sig)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "签名非法"})
		return
	}
	msg := announceSignStr(req.NodeID, req.Addrs, req.TS)
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(msg), sig) {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "验签失败"})
		return
	}
	e := req.entry
	e.UpdatedAt = now
	e.ObservedAddr = observedFrom(r.RemoteAddr)
	s.mu.Lock()
	s.entries[e.NodeID] = e
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true})
}

// observedFrom 取 HTTP 对端的 host:port（NAT 候选出口线索）。
func observedFrom(remote string) string {
	if h, p, err := net.SplitHostPort(remote); err == nil && h != "" {
		return net.JoinHostPort(h, p)
	}
	return remote
}

// announceSignStr 签名串（与客户端一致）。
func announceSignStr(nodeID string, addrs []string, ts int64) string {
	return "announce|" + nodeID + "|" + strings.Join(addrs, ",") + "|" + fmt.Sprint(ts)
}

// handlePeers 查询目录（?since= 增量；返回未过期条目）。
func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	var since int64
	fmt.Sscanf(r.URL.Query().Get("since"), "%d", &since)
	now := time.Now().Unix()
	s.mu.Lock()
	out := make([]entry, 0, len(s.entries))
	for _, e := range s.entries {
		if now-e.UpdatedAt > int64(s.ttl/time.Second) {
			continue // 过期不外发
		}
		if e.UpdatedAt <= since {
			continue
		}
		out = append(out, e)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	writeJSON(w, 200, map[string]any{"ok": true, "peers": out, "now": now})
}

// handleRelayPush 投递信封到对方信箱（POST /relay/{node_id}）。
func (s *Server) handleRelayPush(w http.ResponseWriter, r *http.Request) {
	to := strings.TrimPrefix(r.URL.Path, "/relay/")
	if to == "" || len(to) > 64 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "目标非法"})
		return
	}
	var env struct {
		From string          `json:"from"`
		Body json.RawMessage `json:"body"` // 原样转发的加密信封 JSON
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 48<<20)).Decode(&env); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "载荷非法"})
		return
	}
	if len(env.Body) == 0 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "空信封"})
		return
	}
	s.mu.Lock()
	box := s.mailbox[to]
	box = append(box, relayMsg{From: env.From, Body: env.Body, At: time.Now().Unix()})
	if len(box) > s.maxBox {
		box = box[len(box)-s.maxBox:] // 环形：只留最新
	}
	s.mailbox[to] = box
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "queued": len(box)})
}

// handleRelayInbox 收信（?node=&ts=&sig=：收件人私钥签 "inbox|node|ts" 防冒领）。
func (s *Server) handleRelayInbox(w http.ResponseWriter, r *http.Request) {
	node := r.URL.Query().Get("node")
	ts := r.URL.Query().Get("ts")
	sigHex := r.URL.Query().Get("sig")
	if node == "" || ts == "" || sigHex == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "字段缺失"})
		return
	}
	e, ok := s.lookup(node)
	if !ok {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "未注册"})
		return
	}
	var tsN int64
	fmt.Sscanf(ts, "%d", &tsN)
	now := time.Now().Unix()
	if tsN < now-300 || tsN > now+300 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "时间戳超窗"})
		return
	}
	pub, _ := hex.DecodeString(e.Pub)
	sig, err := hex.DecodeString(sigHex)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), []byte("inbox|"+node+"|"+ts), sig) {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "验签失败"})
		return
	}
	s.mu.Lock()
	box := s.mailbox[node]
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "messages": box})
}

// handleRelayAck 确认取件（清空自己信箱；同一验签口径）。
func (s *Server) handleRelayAck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Node string `json:"node"`
		TS   int64  `json:"ts"`
		Sig  string `json:"sig"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "载荷非法"})
		return
	}
	e, ok := s.lookup(req.Node)
	if !ok {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "未注册"})
		return
	}
	pub, _ := hex.DecodeString(e.Pub)
	sig, _ := hex.DecodeString(req.Sig)
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte("inbox|"+req.Node+"|"+fmt.Sprint(req.TS)), sig) {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "验签失败"})
		return
	}
	s.mu.Lock()
	delete(s.mailbox, req.Node)
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) lookup(node string) (entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[node]
	return e, ok
}

// Run 清理循环 + 服务（阻塞）。
func (s *Server) Run(addr string) error {
	go func() {
		tk := time.NewTicker(time.Minute)
		defer tk.Stop()
		for range tk.C {
			now := time.Now().Unix()
			s.mu.Lock()
			for id, e := range s.entries {
				if now-e.UpdatedAt > int64(s.ttl/time.Second) {
					delete(s.entries, id)
				}
			}
			for id, box := range s.mailbox {
				kept := box[:0]
				for _, m := range box {
					if now-m.At < 24*3600 {
						kept = append(kept, m)
					}
				}
				if len(kept) == 0 {
					delete(s.mailbox, id)
				} else {
					s.mailbox[id] = kept
				}
			}
			s.mu.Unlock()
		}
	}()
	log.Printf("imahub discover 监听 %s（目录 TTL 5 分钟 · 信箱 24h 清理）", addr)
	return http.ListenAndServe(addr, s.Handler())
}
