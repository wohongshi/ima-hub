// Package peer 实现 P2P 节点身份、局域网发现与邀请码（去中心化互联，借鉴
// LocalSend 组播宣告/应答两段式与 Syncthing 身份指纹思想）。
package peer

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Identity 节点身份：Ed25519 密钥对（签名/身份）+ X25519 密钥对（载荷加密）+ 昵称。
// 私钥永不出本机。
type Identity struct {
	dir  string
	Pub  ed25519.PublicKey
	Priv ed25519.PrivateKey
	// X25519：信封载荷加密封装用（与 Ed25519 分离，crypto/ecdh）
	XPriv *ecdh.PrivateKey
	XPub  *ecdh.PublicKey

	Nickname string
}

type identityFile struct {
	Priv  string `json:"priv"`  // hex 私钥（仅本机 0600 存放）
	XPriv string `json:"xpriv"` // X25519 hex 私钥（同上）
	Nick  string `json:"nickname"`
}

// LoadOrCreate 读取（或首次生成）节点身份，存于 dir/identity.json（0600）。
func LoadOrCreate(dir, nickname string) (*Identity, error) {
	p := filepath.Join(dir, "identity.json")
	id := &Identity{dir: dir}
	if raw, err := os.ReadFile(p); err == nil {
		var f identityFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, err
		}
		priv, err := hex.DecodeString(f.Priv)
		if err != nil || len(priv) != ed25519.PrivateKeySize {
			return nil, errors.New("identity.json 私钥损坏")
		}
		id.Priv = ed25519.PrivateKey(priv)
		id.Pub = id.Priv.Public().(ed25519.PublicKey)
		id.Nickname = SanitizeNick(f.Nick)
		if id.Nickname == "" {
			id.Nickname = "节点-" + id.NodeID()[:6]
		}
		if f.XPriv != "" {
			xp, err := hex.DecodeString(f.XPriv)
			if err != nil || len(xp) != 32 {
				return nil, errors.New("identity.json X25519 私钥损坏")
			}
			k, err := ecdh.X25519().NewPrivateKey(xp)
			if err != nil {
				return nil, err
			}
			id.XPriv, id.XPub = k, k.Public().(*ecdh.PublicKey)
		} else {
			// 旧版身份补生成 X25519 密钥对
			k, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				return nil, err
			}
			id.XPriv, id.XPub = k, k.Public().(*ecdh.PublicKey)
			if err := id.save(); err != nil {
				return nil, err
			}
		}
		return id, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id.Pub, id.Priv = pub, priv
	xk, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id.XPriv, id.XPub = xk, xk.Public().(*ecdh.PublicKey)
	id.Nickname = SanitizeNick(nickname)
	if id.Nickname == "" {
		id.Nickname = "节点-" + id.NodeID()[:6]
	}
	if err := id.save(); err != nil {
		return nil, err
	}
	return id, nil
}

func (id *Identity) save() error {
	raw, _ := json.MarshalIndent(identityFile{
		Priv: hex.EncodeToString(id.Priv), XPriv: hex.EncodeToString(id.XPriv.Bytes()), Nick: id.Nickname,
	}, "", " ")
	return os.WriteFile(filepath.Join(id.dir, "identity.json"), raw, 0o600)
}

// SetNickname 更新昵称并持久化。
func (id *Identity) SetNickname(nick string) error {
	n := SanitizeNick(nick)
	if n == "" {
		return errors.New("昵称不合法（中英文/数字/_-，≤24 字）")
	}
	id.Nickname = n
	return id.save()
}

// NodeID 节点 ID = 公钥 SHA-256 前 20 字节 hex（40 字符，类 Syncthing 设备指纹）。
func (id *Identity) NodeID() string { return NodeIDOf(id.Pub) }

// NodeIDOf 由公钥推导节点 ID（供验证方核对 ID 与公钥一致性）。
func NodeIDOf(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:20])
}

// Sign 节点身份签名。
func (id *Identity) Sign(msg []byte) []byte { return ed25519.Sign(id.Priv, msg) }

// Verify 校验某节点公钥的签名。
func Verify(pub ed25519.PublicKey, msg, sig []byte) bool {
	return ed25519.Verify(pub, msg, sig)
}

// FormatID 节点 ID 分组显示（8-8-8-8-8）。
func FormatID(nodeID string) string {
	var b strings.Builder
	for i := 0; i < len(nodeID); i += 8 {
		if i > 0 {
			b.WriteByte('-')
		}
		end := i + 8
		if end > len(nodeID) {
			end = len(nodeID)
		}
		b.WriteString(nodeID[i:end])
	}
	return b.String()
}

// SanitizeNick 昵称白名单：中英文/数字/_-，最长 24 字符。
func SanitizeNick(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if b.Len() >= 24 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
