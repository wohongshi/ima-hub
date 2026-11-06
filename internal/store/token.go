package store

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
)

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// RandomToken 导出给认证层生成 MCP token 等。
func RandomToken() string { return randomToken() }

func itoaID(n int) string { return strconv.Itoa(n) }
