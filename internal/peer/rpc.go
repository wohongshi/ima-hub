package peer

import (
	"bytes"
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
	if strings.Contains(p.Addr, ":") {
		return p.Addr
	}
	if p.TCPPort > 0 {
		return net.JoinHostPort(p.Addr, strconv.Itoa(p.TCPPort))
	}
	return ""
}

// PostEnvelope 投递信封到对方 /api/peer/rpc。
func PostEnvelope(addr string, e Envelope) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	cli := &http.Client{Timeout: 8 * time.Second}
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

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
