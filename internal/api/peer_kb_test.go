package api

import (
	"testing"
	"time"

	"imahub/internal/store"
)

// TestKBGrantOf 授权并集/过期/知识库白名单/方向与状态过滤。
func TestKBGrantOf(t *testing.T) {
	now := time.Now().Unix()
	reqs := []store.AccessRequest{
		{Direction: "received", PeerID: "p1", Status: "approved", Scopes: []string{"search"}, KBs: []string{"kb1"}, CreatedAt: now, UpdatedAt: now},
		{Direction: "received", PeerID: "p1", Status: "approved", Scopes: []string{"browse"}, CreatedAt: now, UpdatedAt: now},
		{Direction: "received", PeerID: "p1", Status: "approved", Scopes: []string{"download"}, KBs: []string{"kbX"}, ExpiresAt: now - 10, CreatedAt: now, UpdatedAt: now},
		{Direction: "sent", PeerID: "p1", Status: "approved", Scopes: []string{"download"}, CreatedAt: now, UpdatedAt: now},
		{Direction: "received", PeerID: "p2", Status: "approved", Scopes: []string{"download"}, CreatedAt: now, UpdatedAt: now},
		{Direction: "received", PeerID: "p1", Status: "revoked", Scopes: []string{"download"}, KBs: []string{"kbY"}, CreatedAt: now, UpdatedAt: now},
	}
	g := kbGrantOf(reqs, "p1")
	if !g.allow("search", "kb1") {
		t.Fatal("kb1 search 应放行")
	}
	if g.allow("search", "kb2") {
		t.Fatal("kb2 search 应拒绝（白名单外）")
	}
	if !g.allow("browse", "kb2") {
		t.Fatal("browse 全库授权应放行")
	}
	if g.allow("download", "kbX") {
		t.Fatal("过期的 download 不应放行")
	}
	if g.allow("download", "kbY") {
		t.Fatal("已撤销的 download 不应放行")
	}
	g2 := kbGrantOf(reqs, "p2")
	if !g2.allow("download", "kb1") {
		t.Fatal("p2 自己的全库 download 应放行")
	}
	if kbGrantOf(reqs, "p3").allow("search", "") {
		t.Fatal("无申请的节点应全拒")
	}
}

// TestKBSafeName 下载文件名净化。
func TestKBSafeName(t *testing.T) {
	cases := map[string]string{
		"a/b/c.txt": "c.txt",
		"..":        "download",
		"x\r\ny":    "xy",
		"":          "download",
		"ok.md":     "ok.md",
	}
	for in, want := range cases {
		if got := kbSafeName(in); got != want {
			t.Fatalf("kbSafeName(%q)=%q want %q", in, got, want)
		}
	}
}
