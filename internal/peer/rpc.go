package peer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Envelope 节点间 RPC 信封：Ed25519 签名（身份绑定 + 防篡改）
// + X25519 密封载荷（防窃听）+ 时间窗 / nonce（防重放）。
type Envelope struct {
	V     int    `json:"v"`
	From  string `json:"from"`
	To    string `json:"to"`
	Pub   string `json:"pub"`   // Ed25519 公钥 hex（绑定 NodeID）
	XPub  string `json:"xpub"`  // X25519 公钥 hex（签名声称，首次见即信任）
	Ek    string `json:"ek"`    // 本次协商的临时 X25519 公钥
	Pay   string `json:"pay"`   // base64(nonce|AES-GCM 密文)
	Nonce string `json:"nonce"` // 防重放随机数
	Ts    int64  `json:"ts"`
	Port  int    `json:"port"` // 发送方 HTTP 端口（收方按实见地址自愈节点表）
	Kind  string `json:"kind"` // 路由明文（不含敏感信息）
	Sig   string `json:"sig"`
}

func (e *Envelope) signStr() string {
	return fmt.Sprintf("v1|from=%s|to=%s|pub=%s|xpub=%s|ek=%s|pay=%s|nonce=%s|ts=%d|port=%d|kind=%s",
		e.From, e.To, e.Pub, e.XPub, e.Ek, e.Pay, e.Nonce, e.Ts, e.Port, e.Kind)
}

// MakeEnvelope 生成密封信封（to=目标 NodeID，toXPub=目标 X25519 公钥，myPort=本方 HTTP 端口）。
func MakeEnvelope(id *Identity, to, toXPub string, myPort int, kind string, plain any) (Envelope, error) {
	body, err := json.Marshal(plain)
	if err != nil {
		return Envelope{}, err
	}
	ek, pay, err := id.SealFor(toXPub, body)
	if err != nil {
		return Envelope{}, err
	}
	e := Envelope{V: 1, From: id.NodeID(), To: to,
		Pub: hex.EncodeToString(id.Pub), XPub: hex.EncodeToString(id.XPub.Bytes()),
		Ek: ek, Pay: pay, Nonce: randomHex(16), Ts: time.Now().Unix(), Port: myPort, Kind: kind}
	e.Sig = hex.EncodeToString(id.Sign([]byte(e.signStr())))
	return e, nil
}

// VerifyEnvelope 验证信封（版本 / ID-公钥绑定 / 时间窗 / 签名）。
func VerifyEnvelope(e Envelope) error {
	if e.V != 1 {
		return fmt.Errorf("协议版本不支持: %d", e.V)
	}
	if e.To == "" || e.Kind == "" || e.Nonce == "" {
		return errors.New("信封字段缺失")
	}
	pub, err := hex.DecodeString(e.Pub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("公钥非法")
	}
	if NodeIDOf(ed25519.PublicKey(pub)) != e.From {
		return errors.New("ID 与公钥不符")
	}
	now := time.Now().Unix()
	if e.Ts < now-replayWindowSec || e.Ts > now+replayWindowSec {
		return errors.New("时间戳超窗")
	}
	sig, err := hex.DecodeString(e.Sig)
	if err != nil || !Verify(ed25519.PublicKey(pub), []byte(e.signStr()), sig) {
		return errors.New("验签失败")
	}
	return nil
}

// OpenEnvelope 用本机私钥解封载荷（验签通过后调用）。
func (id *Identity) OpenEnvelope(e Envelope) ([]byte, error) {
	return id.OpenFrom(e.Ek, e.Pay)
}

// PeerAddr 可达地址（邀请节点 Addr=ip:port；发现节点 Addr=IP + TCPPort）。
func PeerAddr(p Peer) string {
	if p.Addr == "" {
		return ""
	}
	if _, port, err := net.SplitHostPort(p.Addr); err == nil && port != "" {
		return p.Addr // 已是 host:port（含 IPv6 [..]:port）
	}
	if p.TCPPort > 0 {
		return net.JoinHostPort(p.Addr, strconv.Itoa(p.TCPPort)) // JoinHostPort 自动给 IPv6 加括号
	}
	return ""
}

// 共享 HTTP 客户端：连接复用（节点间高频 RPC 不再每次新建连接）；
// 基于 DefaultTransport 克隆，保留代理环境（HTTP_PROXY 等）与默认拨号/HTTP2 行为。
var rpcTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 32
	t.MaxIdleConnsPerHost = 8
	t.IdleConnTimeout = 90 * time.Second
	return t
}()

// PostEnvelope 投递信封到对方 /api/peer/rpc。
func PostEnvelope(addr string, e Envelope) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	cli := &http.Client{Timeout: 8 * time.Second, Transport: rpcTransport}
	resp, err := cli.Post("http://"+addr+"/api/peer/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("对方返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// PostEnvelopeResp 投递信封并取回同步回程信封（知识库查询用，对方须在线；回包可达几十 MB）。
// ctx 可随 HTTP 请求取消；回包超 64MB 显式报错（不静默截断）。
func PostEnvelopeResp(ctx context.Context, addr string, e Envelope) ([]byte, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/peer/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	cli := &http.Client{Timeout: 120 * time.Second, Transport: rpcTransport}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 64<<20 {
		return nil, errors.New("回包过大（>64MB），已中止")
	}
	if resp.StatusCode/100 != 2 {
		msg := raw
		if len(msg) > 4096 {
			msg = msg[:4096] // 错误回显不放大（对方可回大 body 撑内存）
		}
		return nil, fmt.Errorf("对方返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return raw, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
