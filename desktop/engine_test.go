package main

import (
	"strings"
	"testing"
)

// TestEngineBoot 桌面引擎自举：随机端口起引擎，healthz / 版本 / WebUI 三断言。
func TestEngineBoot(t *testing.T) {
	eng, err := boot(t.TempDir(), "127.0.0.1:0", "")
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	defer eng.stop()
	if err := eng.selftest(); err != nil {
		t.Fatalf("selftest: %v", err)
	}
	if !strings.HasPrefix(eng.url, "http://127.0.0.1:") {
		t.Fatalf("引擎 URL 异常: %s", eng.url)
	}
}
