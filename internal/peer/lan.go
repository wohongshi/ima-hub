package peer

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// 发现参数：组播宣告/应答两段式（借鉴 LocalSend 单端口、防乒乓 announce 标志）。
const (
	lanGroup         = "224.0.0.167" // LocalSend 实测各平台兼容性最好的组
	defaultLanPort   = 29820
	announceInterval = 30 * time.Second
	replayWindowSec  = 300
)

// LanPort 局域网发现端口（PEER_LAN_PORT 可覆盖）。
func LanPort() int {
	if v := os.Getenv("PEER_LAN_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 65536 {
			return n
		}
	}
	return defaultLanPort
}

// lanExtras 显式宣告地址（PEER_LAN_EXTRA=host:port,host:port，测试/复杂网络用）。
func lanExtras() []string {
	var out []string
	for _, s := range strings.Split(os.Getenv("PEER_LAN_EXTRA"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Packet 局域网宣告帧：Ed25519 签名 + 时间窗防重放。
type Packet struct {
	V    int    `json:"v"`
	A    bool   `json:"a"` // true=宣告，false=应答（防乒乓）
	ID   string `json:"id"`
	Nick string `json:"nick"`
	Pub  string `json:"pub"`
	XPub string `json:"xpub"` // X25519 公钥（信封加密用）
	TCP  int    `json:"tcp"`  // 预留：P2P 直连端口
	TS   int64  `json:"ts"`
	Sig  string `json:"sig"`
}

func (p *Packet) payload() string {
	return fmt.Sprintf("v1|a=%t|id=%s|nick=%s|pub=%s|xpub=%s|tcp=%d|ts=%d",
		p.A, p.ID, p.Nick, p.Pub, p.XPub, p.TCP, p.TS)
}

// MakePacket 生成带签名的宣告/应答帧。
func MakePacket(id *Identity, tcpPort int, announce bool) Packet {
	p := Packet{V: 1, A: announce, ID: id.NodeID(), Nick: id.Nick(),
		Pub: hex.EncodeToString(id.Pub), XPub: hex.EncodeToString(id.XPub.Bytes()), TCP: tcpPort, TS: id.NextTS()}
	p.Sig = hex.EncodeToString(id.Sign([]byte(p.payload())))
	return p
}

// VerifyPacket 验证帧（版本 / 公钥-ID 一致 / 时间窗 / 签名）。
func VerifyPacket(p Packet) error {
	if p.V != 1 {
		return fmt.Errorf("协议版本不支持: %d", p.V)
	}
	pub, err := hex.DecodeString(p.Pub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("公钥非法")
	}
	if NodeIDOf(ed25519.PublicKey(pub)) != p.ID {
		return errors.New("ID 与公钥不符")
	}
	if p.XPub != "" {
		if _, err := parseX25519Pub(p.XPub); err != nil {
			return err
		}
	}
	now := time.Now().Unix()
	if p.TS < now-replayWindowSec || p.TS > now+replayWindowSec {
		return errors.New("时间戳超窗")
	}
	sig, err := hex.DecodeString(p.Sig)
	if err != nil || !Verify(ed25519.PublicKey(pub), []byte(p.payload()), sig) {
		return errors.New("验签失败")
	}
	return nil
}

// RunLAN 局域网发现：启动即宣告，之后每 30 秒；收到他人宣告立即应答（对方秒见我）。
// onPeer：发现/更新节点时回调（用于触发离线队列补投），可为 nil。
func RunLAN(id *Identity, t *Table, tcpPort int, onPeer func(Peer), stop <-chan struct{}) {
	port := LanPort()
	group := &net.UDPAddr{IP: net.ParseIP(lanGroup), Port: port}
	rx, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		log.Printf("局域网发现监听失败（已禁用）: %v", err)
		return
	}
	go func() { <-stop; rx.Close() }()

	send := func(announce bool, dsts []string) {
		b, _ := json.Marshal(MakePacket(id, tcpPort, announce))
		for _, dst := range dsts {
			addr, err := net.ResolveUDPAddr("udp4", dst)
			if err != nil {
				continue
			}
			// 从监听套接字发出：源端口=发现端口，对方应答才能回到本机监听
			_, _ = rx.WriteToUDP(b, addr)
		}
	}
	announceTargets := func() []string {
		out := []string{lanGroup + ":" + strconv.Itoa(port)}
		return append(out, lanExtras()...)
	}

	// 收包协程：验证 → 防重放 → 入表 → 对宣告帧立即应答
	go func() {
		buf := make([]byte, 64*1024)
		lastTS := map[string]int64{} // 每 NodeID 已见最高 TS（TS 已签名，篡改无效）
		for {
			n, from, err := rx.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var p Packet
			if err := json.Unmarshal(buf[:n], &p); err != nil {
				continue
			}
			if p.ID == id.NodeID() {
				continue // 自己的宣告（组播回环）
			}
			if err := VerifyPacket(p); err != nil {
				continue // 坏包静默丢弃
			}
			if p.TS <= lastTS[p.ID] {
				continue // 旧帧重放：拒绝（防抓包改源地址劫持节点指向）
			}
			lastTS[p.ID] = p.TS
			host, _, _ := net.SplitHostPort(from.String())
			np := Peer{
				ID: p.ID, Nickname: SanitizeNick(p.Nick), Pub: p.Pub, XPub: p.XPub,
				Addr: host, TCPPort: p.TCP, Source: "lan", LastSeen: time.Now().Unix(),
			}
			t.Upsert(np)
			if onPeer != nil {
				onPeer(np) // 对方上线/更新 → 立即补投离线队列
			}
			if p.A {
				// 两段式发现：听我者即刻知我（LocalSend 同款）
				send(false, []string{from.String()})
			}
		}
	}()

	send(true, announceTargets())
	tk := time.NewTicker(announceInterval)
	defer tk.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tk.C:
			send(true, announceTargets())
		}
	}
}
