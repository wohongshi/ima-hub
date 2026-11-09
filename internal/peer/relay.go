package peer

// 公网发现 + 中继客户端：①心跳 announce（含本机地址 + 服务端观测地址）②拉取
// 目录更新已信任节点地址 ③直连失败时把信封投进对方中继信箱 ④轮询自己信箱
// 注回本地 /api/peer/rpc（同一套验签/去重/处理逻辑，地址自愈跳过）。
// 注意：知识库查询（kb.*）为同步请求/响应，中继信箱只承载异步消息；
// 给离线/中继节点发 kb 查询会被拒（提示需直连）。

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// announcePayload 注册/心跳载荷（与 discover 服务端签名校验一致）。
type announcePayload struct {
	NodeID string   `json:"node_id"`
	XPub   string   `json:"xpub"`
	Pub    string   `json:"pub"`
	Addrs  []string `json:"addrs"`
	TS     int64    `json:"ts"`
	Sig    string   `json:"sig"`
}

// AnnounceSignStr 签名串（服务端同一份）。
func AnnounceSignStr(nodeID string, addrs []string, ts int64) string {
	return "announce|" + nodeID + "|" + strings.Join(addrs, ",") + "|" + fmt.Sprint(ts)
}

// InboxSignStr 收件签名串。
func InboxSignStr(nodeID string, ts int64) string {
	return "inbox|" + nodeID + "|" + fmt.Sprint(ts)
}

// relayHTTP 共享客户端（连接复用）。
var relayHTTP = &http.Client{Timeout: 20 * time.Second, Transport: rpcTransport}

// Announce 注册/心跳到发现服务器。addrs=对外可达地址候选（host:port）。
func Announce(ctx context.Context, baseURL string, id *Identity, addrs []string) error {
	ts := time.Now().Unix()
	p := announcePayload{
		NodeID: id.NodeID(), XPub: hex.EncodeToString(id.XPub.Bytes()),
		Pub: hex.EncodeToString(id.Pub), Addrs: addrs, TS: ts,
		Sig: hex.EncodeToString(id.Sign([]byte(AnnounceSignStr(id.NodeID(), addrs, ts)))),
	}
	body, _ := json.Marshal(p)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/announce", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := relayHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("announce 被拒 %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// RelayPeer 目录条目（含服务端观测地址，NAT 场景的候选出口）。
type RelayPeer struct {
	NodeID       string   `json:"node_id"`
	XPub         string   `json:"xpub"`
	Pub          string   `json:"pub"`
	Addrs        []string `json:"addrs"`
	ObservedAddr string   `json:"observed_addr,omitempty"`
	UpdatedAt    int64    `json:"updated_at"`
}

// FetchPeers 拉取目录（since 增量）。
func FetchPeers(ctx context.Context, baseURL string, since int64) ([]RelayPeer, int64, error) {
	u := strings.TrimRight(baseURL, "/") + "/peers?since=" + fmt.Sprint(since)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := relayHTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out struct {
		Peers []RelayPeer `json:"peers"`
		Now   int64       `json:"now"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, 0, err
	}
	return out.Peers, out.Now, nil
}

// RelayPush 把加密信封投进对方信箱（kb.* 同步查询不走中继）。
func RelayPush(ctx context.Context, baseURL, to, from string, envJSON []byte) error {
	if len(envJSON) > 40<<20 {
		return fmt.Errorf("信封过大")
	}
	body, _ := json.Marshal(map[string]any{"from": from, "body": json.RawMessage(envJSON)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/relay/"+url.PathEscape(to), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := relayHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("中继投递被拒 %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// FetchInbox 拉取自己信箱（验签防冒领）→ 返回信封 JSON 列表并清箱。
func FetchInbox(ctx context.Context, baseURL string, id *Identity) ([][]byte, error) {
	ts := time.Now().Unix()
	sig := hex.EncodeToString(id.Sign([]byte(InboxSignStr(id.NodeID(), ts))))
	u := strings.TrimRight(baseURL, "/") + "/relay/inbox?node=" + url.QueryEscape(id.NodeID()) +
		"&ts=" + fmt.Sprint(ts) + "&sig=" + sig
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := relayHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Messages []struct {
			From string          `json:"from"`
			Body json.RawMessage `json:"body"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 48<<20)).Decode(&out); err != nil {
		return nil, err
	}
	// 清箱（同验签口径）
	go func() {
		body, _ := json.Marshal(map[string]any{"node": id.NodeID(), "ts": ts, "sig": sig})
		req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/relay/ack", bytes.NewReader(body))
		if req != nil {
			req.Header.Set("Content-Type", "application/json")
			if resp, err := relayHTTP.Do(req); err == nil {
				resp.Body.Close()
			}
		}
	}()
	list := make([][]byte, 0, len(out.Messages))
	for _, m := range out.Messages {
		list = append(list, m.Body)
	}
	return list, nil
}

// ---------- 发现/中继后台循环 ----------

// DiscoveryOpts 发现客户端配置。
type DiscoveryOpts struct {
	BaseURL    string          // 发现服务器地址（http(s)://…）
	Advertised []string        // 附加对外地址（公网映射时手填 host:port）
	LocalAddrs func() []string // 本机地址候选（LAN IP:port）
	SelfURL    string          // 本节点 HTTP 地址（信箱回注用）
}

// RunDiscovery 心跳/目录/信箱三循环（可断电重启，幂等）。
func RunDiscovery(stop <-chan struct{}, id *Identity, t *Table, opts DiscoveryOpts) {
	var sinceMu sync.Mutex
	since := int64(0)
	announce := func() {
		addrs := append([]string{}, opts.Advertised...)
		if opts.LocalAddrs != nil {
			addrs = append(addrs, opts.LocalAddrs()...)
		}
		if len(addrs) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := Announce(ctx, opts.BaseURL, id, addrs); err != nil {
			log.Printf("发现心跳失败: %v", err)
		}
	}
	go func() {
		announce()
		tk := time.NewTicker(30 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				announce()
			}
		}
	}()
	// 目录拉取 → 已信任节点地址补全/更新（未信任不入表：信任仍走邀请码）
	fetchPeersOnce := func() {
		sinceMu.Lock()
		s := since
		sinceMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		peers, now, err := FetchPeers(ctx, opts.BaseURL, s)
		cancel()
		if err != nil {
			log.Printf("发现目录拉取失败: %v", err)
			return
		}
		sinceMu.Lock()
		if now > since {
			since = now
		}
		sinceMu.Unlock()
		for _, rp := range peers {
			if rp.NodeID == id.NodeID() {
				continue
			}
			cur, ok := t.Get(rp.NodeID)
			if !ok {
				continue // 只更新已信任节点（发现≠信任）
			}
			cands := append([]string{}, rp.Addrs...)
			if rp.ObservedAddr != "" {
				cands = append(cands, rp.ObservedAddr)
			}
			if len(cands) == 0 {
				continue
			}
			// 地址为空才补；已有地址不被目录覆盖（实见优先原则）
			if cur.Addr == "" {
				cur.Addr = cands[0]
				if cur.XPub == "" {
					cur.XPub = rp.XPub
				}
				t.Upsert(cur)
			}
		}
	}
	go func() {
		tk := time.NewTicker(60 * time.Second)
		defer tk.Stop()
		fetchPeersOnce()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				fetchPeersOnce()
			}
		}
	}()
	// 信箱轮询 → 回注本地 /api/peer/rpc（同一套处理逻辑）；进循环先收一趟
	pollInbox := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		msgs, err := FetchInbox(ctx, opts.BaseURL, id)
		if err != nil {
			log.Printf("中继收件失败: %v", err)
			return
		}
		for _, m := range msgs {
			injectEnvelope(opts.SelfURL, m)
		}
	}
	go func() {
		tk := time.NewTicker(20 * time.Second)
		defer tk.Stop()
		pollInbox()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				pollInbox()
			}
		}
	}()
}

// relayEnvelope 把待投信封投进对方信箱（to 允许是尚无地址的已知节点）。
func relayEnvelope(baseURL, to, from string, env Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return RelayPush(ctx, baseURL, to, from, body)
}

// injectEnvelope 把中继信封回注本地处理（带 X-Peer-Relay 头：跳过地址自愈，
// 中继无法证明来源地址，避免把节点表地址改成 127.0.0.1）。
func injectEnvelope(selfURL string, envJSON []byte) {
	if selfURL == "" {
		return
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(selfURL, "/")+"/api/peer/rpc", bytes.NewReader(envJSON))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Peer-Relay", "1")
	resp, err := relayHTTP.Do(req)
	if err != nil {
		log.Printf("中继信封回注失败: %v", err)
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
}

// LocalAddrs 拼本机非环回 IPv4 的 host:port 候选。
func LocalAddrs(port int) []string {
	out := []string{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				out = append(out, net.JoinHostPort(ip4.String(), fmt.Sprint(port)))
			}
		}
	}
	sort.Strings(out)
	return out
}
