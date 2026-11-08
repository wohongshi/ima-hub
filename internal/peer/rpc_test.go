package peer

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// 信封往返：加密封载 + 验签 + 解封。
func TestEnvelopeSealRoundTrip(t *testing.T) {
	a, err := LoadOrCreate(t.TempDir(), "甲")
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreate(t.TempDir(), "乙")
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"request_id": "ar_1", "scopes": []string{"search", "download"}}
	env, err := MakeEnvelope(a, b.NodeID(), hex.EncodeToString(b.XPub.Bytes()), 8090, "access.request", payload)
	if err != nil {
		t.Fatalf("组包: %v", err)
	}
	if err := VerifyEnvelope(env); err != nil {
		t.Fatalf("验签: %v", err)
	}
	if env.To != b.NodeID() || env.Kind != "access.request" {
		t.Fatalf("信封字段错: %+v", env)
	}
	// 只有收件人能解
	plain, err := b.OpenEnvelope(env)
	if err != nil {
		t.Fatalf("解封: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(plain, &got); err != nil || got["request_id"] != "ar_1" {
		t.Fatalf("载荷不对: %s err=%v", plain, err)
	}
	if _, err := a.OpenEnvelope(env); err == nil {
		t.Fatal("发件人不该能凭自己的信封解出明文（临时密钥单向）")
	}
}

// 篡改 / 超窗 / ID 冒充一律拒绝。
func TestEnvelopeRejects(t *testing.T) {
	a, _ := LoadOrCreate(t.TempDir(), "甲")
	b, _ := LoadOrCreate(t.TempDir(), "乙")
	env, err := MakeEnvelope(a, b.NodeID(), hex.EncodeToString(b.XPub.Bytes()), 8090, "ping", map[string]any{"n": 1})
	if err != nil {
		t.Fatal(err)
	}
	bad := env
	bad.Pay = bad.Pay[:len(bad.Pay)-4] + "AAAA" // 破坏密文
	if err := VerifyEnvelope(bad); err == nil {
		t.Fatal("篡改载荷应验签失败")
	}
	stale := env
	stale.Ts = time.Now().Unix() - replayWindowSec - 60
	if err := VerifyEnvelope(stale); err == nil {
		t.Fatal("超窗应拒绝")
	}
	imp := env
	imp.From = b.NodeID() // ID 与公钥不符
	if err := VerifyEnvelope(imp); err == nil {
		t.Fatal("ID 冒充应拒绝")
	}
}

// 离线投递队列：入队 → 到期 → 失败退避 → 送达出队 → 持久化。
func TestOutboxLifecycle(t *testing.T) {
	o, err := OpenOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := o.Push("peer-x", "access.request", map[string]any{"n": 1})
	if id == "" {
		t.Fatal("入队失败")
	}
	if n := len(o.Due(time.Now().Unix())); n != 1 {
		t.Fatalf("应有 1 条到期，got %d", n)
	}
	o.Fail(id, errors.New("对方掉线"))
	if n := len(o.Due(time.Now().Unix())); n != 0 {
		t.Fatal("退避期内不该到期")
	}
	if n := len(o.Due(time.Now().Unix() + 3600)); n != 1 {
		t.Fatal("退避期后应到期重试")
	}
	o.Done(id)
	if o.PendingCount() != 0 {
		t.Fatal("送达应出队")
	}
	// 持久化：重启后仍在
	dir := t.TempDir()
	o1, err := OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	id2 := o1.Push("peer-y", "ping", map[string]any{"n": 2})
	o2, err := OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	if o2.PendingCount() != 1 || o2.Msgs[0].ID != id2 {
		t.Fatalf("持久化丢消息: %+v", o2.Msgs)
	}
}
