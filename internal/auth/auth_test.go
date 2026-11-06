package auth

import (
	"encoding/base64"
	"testing"
)

// 畸形/截断的哈希记录不得 fail-open（空哈希曾可导致任意口令通过）。
func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	short := base64.RawStdEncoding.EncodeToString([]byte("tiny"))
	for _, enc := range []string{
		"pbkdf2$120000$" + salt + "$",     // 空哈希
		"pbkdf2$120000$" + salt + "$" + short, // 过短哈希
		"pbkdf2$120000$$",                 // 空盐空哈希
		"pbkdf2$0$" + salt + "$" + short,  // 非法迭代数
		"not-a-hash",
		"",
	} {
		if VerifyPassword("any-password", enc) {
			t.Errorf("畸形哈希 %q 不应通过校验", enc)
		}
	}
}

func TestVerifyPasswordRoundtrip(t *testing.T) {
	h, err := HashPassword("s3cret-口令")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("s3cret-口令", h) {
		t.Error("正确口令应通过校验")
	}
	if VerifyPassword("wrong-password", h) {
		t.Error("错误口令不应通过校验")
	}
}
