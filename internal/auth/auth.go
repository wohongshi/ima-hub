// Package auth 口令哈希（PBKDF2-HMAC-SHA256）与登录辅助。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

const (
	defaultIter = 120000
	saltLen     = 16
	keyLen      = 32
)

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	dk := make([]byte, 0, numBlocks*hashLen)
	buf := make([]byte, 4)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf)
		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

// HashPassword 返回 "pbkdf2$<iter>$<saltB64>$<hashB64>"。
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk := pbkdf2SHA256([]byte(password), salt, defaultIter, keyLen)
	return "pbkdf2$" + strconv.Itoa(defaultIter) + "$" +
		base64.RawStdEncoding.EncodeToString(salt) + "$" +
		base64.RawStdEncoding.EncodeToString(dk), nil
}

// VerifyPassword 校验口令。
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 || iter > 10_000_000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	// 防御畸形/截断记录：want 为空时 pbkdf2(0 长度) 与空比较恒等通过（登录绕过）
	if len(want) < 16 || len(want) > 64 {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iter, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ValidateUsername 用户名规则：3-32 位字母数字下划线中划线。
func ValidateUsername(name string) error {
	if len(name) < 3 || len(name) > 32 {
		return errors.New("用户名长度需在 3-32 位之间")
	}
	for _, r := range name {
		ok := r == '_' || r == '-' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			r >= 0x4e00 // 允许中文
		if !ok {
			return errors.New("用户名只能包含字母、数字、下划线、中划线或中文")
		}
	}
	return nil
}

// ValidatePassword 口令至少 6 位。
func ValidatePassword(pw string) error {
	if len(pw) < 6 {
		return errors.New("密码至少 6 位")
	}
	return nil
}
