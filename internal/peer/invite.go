package peer

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 邀请码：IH1.<base32(身份卡 JSON + 签名 + 校验和)>，用于跨网络交换身份与地址。
const (
	invitePrefix    = "IH1."
	inviteExpirySec = 60 * 24 * 3600 // 60 天有效
	inviteMaxLen    = 2048
)

type invitePayload struct {
	V     int      `json:"v"`
	ID    string   `json:"id"`
	Pub   string   `json:"pub"`
	XPub  string   `json:"xpub,omitempty"` // X25519 公钥（信封加密用）
	Nick  string   `json:"nick"`
	Addrs []string `json:"addrs,omitempty"` // 可选对外地址（ip:port）
	TS    int64    `json:"ts"`
	Sig   string   `json:"sig"`
}

func (p *invitePayload) signStr() string {
	return fmt.Sprintf("v1|id=%s|pub=%s|xpub=%s|nick=%s|addrs=%s|ts=%d",
		p.ID, p.Pub, p.XPub, p.Nick, strings.Join(p.Addrs, ","), p.TS)
}

// MakeInvite 生成邀请码（身份 + 地址 + 签名，60 天有效）。
func (id *Identity) MakeInvite(addrs []string) (string, error) {
	clean := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a = strings.TrimSpace(a); a != "" {
			clean = append(clean, a)
		}
	}
	p := invitePayload{
		V: 1, ID: id.NodeID(), Pub: hex.EncodeToString(id.Pub), XPub: hex.EncodeToString(id.XPub.Bytes()), Nick: id.Nickname,
		Addrs: clean, TS: time.Now().Unix(),
	}
	p.Sig = hex.EncodeToString(id.Sign([]byte(p.signStr())))
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	raw = append(raw, sum[:4]...)
	return invitePrefix + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// ParseInvite 解析邀请码 → 可信节点（校验和 / 验签 / 时效三道闸）。
func ParseInvite(code string) (Peer, error) {
	code = strings.TrimSpace(code)
	if len(code) > inviteMaxLen {
		return Peer{}, errors.New("邀请码过长")
	}
	if !strings.HasPrefix(code, invitePrefix) {
		return Peer{}, errors.New("邀请码格式不对（应以 IH1. 开头）")
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.TrimPrefix(code, invitePrefix)))
	if err != nil || len(raw) < 5 {
		return Peer{}, errors.New("邀请码解码失败（复制不全？）")
	}
	body, sum := raw[:len(raw)-4], raw[len(raw)-4:]
	calc := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum), hex.EncodeToString(calc[:4])) {
		return Peer{}, errors.New("邀请码校验失败（复制不全？）")
	}
	var p invitePayload
	if err := json.Unmarshal(body, &p); err != nil {
		return Peer{}, errors.New("邀请码解析失败")
	}
	pub, err := hex.DecodeString(p.Pub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return Peer{}, errors.New("邀请码公钥非法")
	}
	if NodeIDOf(ed25519.PublicKey(pub)) != p.ID {
		return Peer{}, errors.New("邀请码 ID 与公钥不符")
	}
	sig, err := hex.DecodeString(p.Sig)
	if err != nil || !Verify(ed25519.PublicKey(pub), []byte(p.signStr()), sig) {
		return Peer{}, errors.New("邀请码验签失败")
	}
	if p.TS < time.Now().Unix()-inviteExpirySec {
		return Peer{}, errors.New("邀请码已过期")
	}
	addr := ""
	if len(p.Addrs) > 0 {
		addr = p.Addrs[0]
	}
	if p.XPub != "" {
		if _, err := parseX25519Pub(p.XPub); err != nil {
			return Peer{}, errors.New("邀请码 X25519 公钥非法")
		}
	}
	return Peer{
		ID: p.ID, Nickname: SanitizeNick(p.Nick), Pub: p.Pub, XPub: p.XPub,
		Addr: addr, Source: "invite", Trusted: true, LastSeen: time.Now().Unix(),
	}, nil
}
