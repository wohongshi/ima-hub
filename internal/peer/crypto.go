package peer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

// —— 载荷加密（P2P 通道）：X25519 临时密钥协商 + HKDF-SHA256 派生 + AES-256-GCM ——
// 全 stdlib（crypto/ecdh、crypto/hkdf）：私钥不出户，链路窃听者只见密文。

const peerKDFInfo = "imahub-peer-v1"

func parseX25519Pub(hexKey string) (*ecdh.PublicKey, error) {
	b, err := hex.DecodeString(hexKey)
	if err != nil || len(b) != 32 {
		return nil, errors.New("X25519 公钥非法")
	}
	return ecdh.X25519().NewPublicKey(b)
}

// sealKey 共享秘密 → 会话密钥（临时私钥 × 对方公钥 = 对方私钥 × 临时公钥）。
func sealKey(priv *ecdh.PrivateKey, pub *ecdh.PublicKey) ([]byte, error) {
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, err
	}
	return hkdf.Key(sha256.New, shared, nil, peerKDFInfo, 32)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// SealFor 用对方 X25519 公钥密封载荷。返回临时公钥 hex 与 base64(nonce|密文)。
func (id *Identity) SealFor(xpubHex string, plain []byte) (ek, blob string, err error) {
	theirPub, err := parseX25519Pub(xpubHex)
	if err != nil {
		return "", "", err
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	key, err := sealKey(eph, theirPub)
	if err != nil {
		return "", "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	ct := gcm.Seal(nil, nonce, plain, nil)
	out := append(nonce, ct...)
	return hex.EncodeToString(eph.PublicKey().Bytes()), base64.StdEncoding.EncodeToString(out), nil
}

// OpenFrom 用本机私钥 + 对方临时公钥解封（与 SealFor 成对）。
func (id *Identity) OpenFrom(ekHex, blob string) ([]byte, error) {
	ephPub, err := parseX25519Pub(ekHex)
	if err != nil {
		return nil, err
	}
	if id.XPriv == nil {
		return nil, errors.New("本机缺少 X25519 私钥")
	}
	key, err := sealKey(id.XPriv, ephPub)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil || len(raw) < gcm.NonceSize() {
		return nil, errors.New("密文非法")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}
