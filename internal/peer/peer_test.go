package peer

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIdentityCreateReload(t *testing.T) {
	dir := t.TempDir()
	id, err := LoadOrCreate(dir, "甲-A")
	if err != nil {
		t.Fatal(err)
	}
	if id.Nick() != "甲-A" {
		t.Fatalf("昵称不对: %q", id.Nick())
	}
	if len(id.NodeID()) != 40 {
		t.Fatalf("NodeID 长度不对: %d", len(id.NodeID()))
	}
	// 重新加载 → 同身份
	id2, err := LoadOrCreate(dir, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if id2.NodeID() != id.NodeID() {
		t.Fatal("重载后身份变了")
	}
	// 签名/验签 + 篡改拒绝
	sig := id.Sign([]byte("msg"))
	if !Verify(id.Pub, []byte("msg"), sig) {
		t.Fatal("验签失败")
	}
	if Verify(id.Pub, []byte("msg!"), sig) {
		t.Fatal("篡改后验签竟通过")
	}
	// 昵称持久化
	if err := id.SetNickname("乙|B<改>"); err != nil {
		t.Fatal(err)
	}
	if id.Nick() != "乙B改" {
		t.Fatalf("昵称净化不对: %q", id.Nick())
	}
	id3, _ := LoadOrCreate(dir, "")
	if id3.Nick() != "乙B改" {
		t.Fatalf("昵称未持久化: %q", id3.Nick())
	}
	// 私钥文件权限
	fi, _ := filepath.Glob(filepath.Join(dir, "identity.json"))
	if len(fi) != 1 {
		t.Fatal("identity.json 缺失")
	}
}

func TestInviteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	id, err := LoadOrCreate(dir, "inviter")
	if err != nil {
		t.Fatal(err)
	}
	code, err := id.MakeInvite([]string{"192.168.1.5:29821", "", " 10.0.0.2:29821 "})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(code, "IH1.") {
		t.Fatalf("邀请码前缀不对: %s", code[:8])
	}
	p, err := ParseInvite(code)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != id.NodeID() || !p.Trusted || p.Source != "invite" {
		t.Fatalf("解析结果不对: %+v", p)
	}
	if p.Addr != "192.168.1.5:29821" || p.Nickname != "inviter" {
		t.Fatalf("地址/昵称不对: %+v", p)
	}
	// 篡改一位 → 校验失败
	bad := code[:len(code)-2] + strings.ToUpper(code[len(code)-2:])
	if bad != code {
		if _, err := ParseInvite(bad); err == nil {
			t.Fatal("篡改邀请码竟通过")
		}
	}
	if _, err := ParseInvite("IH1.ZZZ"); err == nil {
		t.Fatal("垃圾邀请码竟通过")
	}
	if _, err := ParseInvite("nope"); err == nil {
		t.Fatal("无前缀竟通过")
	}
	// 过期拒绝：手工造旧时间戳 + 合法签名的完整码
	var p2 invitePayload
	p2.V = 1
	p2.ID = id.NodeID()
	p2.Pub = hex.EncodeToString(id.Pub)
	p2.Nick = "old"
	p2.TS = time.Now().Unix() - inviteExpirySec - 10
	p2.Sig = hex.EncodeToString(id.Sign([]byte(p2.signStr())))
	raw, _ := json.Marshal(p2)
	sum := sha256.Sum256(raw)
	raw = append(raw, sum[:4]...)
	expired := invitePrefix + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	if _, err := ParseInvite(expired); err == nil {
		t.Fatal("过期邀请码竟通过")
	}
}

func TestVerifyPacket(t *testing.T) {
	dir := t.TempDir()
	id, err := LoadOrCreate(dir, "pkt")
	if err != nil {
		t.Fatal(err)
	}
	p := MakePacket(id, 29821, true)
	if err := VerifyPacket(p); err != nil {
		t.Fatalf("合法帧未通过: %v", err)
	}
	// 坏签名
	p2 := p
	p2.Nick = "改了昵称"
	if err := VerifyPacket(p2); err == nil {
		t.Fatal("坏签名竟通过")
	}
	// 时间窗
	p3 := MakePacket(id, 0, true)
	p3.TS = time.Now().Unix() - 10000
	p3.Sig = hex.EncodeToString(id.Sign([]byte(p3.payload())))
	if err := VerifyPacket(p3); err == nil {
		t.Fatal("过期帧竟通过")
	}
	// ID 与公钥不符
	p4 := MakePacket(id, 0, true)
	p4.ID = strings.Repeat("0", 40)
	p4.Sig = hex.EncodeToString(id.Sign([]byte(p4.payload())))
	if err := VerifyPacket(p4); err == nil {
		t.Fatal("ID 伪装竟通过")
	}
}

func TestTableUpsert(t *testing.T) {
	tb, err := OpenTable(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !tb.Upsert(Peer{ID: "aa", Nickname: "旧名", LastSeen: 100}) {
		t.Fatal("新增未变化")
	}
	tb.Upsert(Peer{ID: "aa", Nickname: "新名", Addr: "1.2.3.4:5", Trusted: true, LastSeen: 200})
	p, ok := tb.Get("aa")
	if !ok || p.Nickname != "新名" || !p.Trusted || p.LastSeen != 200 || p.Addr != "1.2.3.4:5" {
		t.Fatalf("合并结果不对: %+v", p)
	}
	// 旧 LastSeen 不回退
	tb.Upsert(Peer{ID: "aa", LastSeen: 50})
	p, _ = tb.Get("aa")
	if p.LastSeen != 200 {
		t.Fatalf("时间回退了: %+v", p)
	}
	// 持久化重载
	dir := t.TempDir()
	tb2, _ := OpenTable(dir)
	tb2.Upsert(Peer{ID: "x", Nickname: "x"})
	tb3, _ := OpenTable(dir)
	if _, ok := tb3.Get("x"); !ok {
		t.Fatal("重载丢失节点")
	}
	if !tb.Remove("aa") || tb.Remove("aa") {
		t.Fatal("删除语义不对")
	}
}
