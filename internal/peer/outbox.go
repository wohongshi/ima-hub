package peer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Outbox 离线投递队列：对方掉线时消息先排队，指数退避重试，送达即出队。
// 群组 / 申请消息的掉线容错底座——发送不阻塞、不丢消息，对方回归自动补投。
const (
	outboxBackoffBase = 30  // 秒：30 → 60 → 120 → 240 → 480 封顶
	outboxBackoffMax  = 480 // ≈8 分钟
	outboxMaxMsgs     = 1000
	outboxMaxAttempts = 200 // 重试上限：超过即弃投（防僵尸消息永久占位）
)

// OutboxMsg 一条待投递消息。
type OutboxMsg struct {
	ID        string          `json:"id"`
	To        string          `json:"to"` // 目标 NodeID
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt int64           `json:"created_at"`
	Attempts  int             `json:"attempts"`
	NextAt    int64           `json:"next_at"`
	LastErr   string          `json:"last_err,omitempty"`
}

// Outbox 投递队列：outbox.json 持久化（0600）。
type Outbox struct {
	mu   sync.Mutex
	path string
	Msgs []OutboxMsg `json:"msgs"`
	kick chan struct{}
}

// OpenOutbox 加载（或初始化）投递队列。
func OpenOutbox(dir string) (*Outbox, error) {
	o := &Outbox{path: filepath.Join(dir, "outbox.json"), kick: make(chan struct{}, 1)}
	raw, err := os.ReadFile(o.path)
	if err == nil {
		if err := json.Unmarshal(raw, o); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return o, nil
}

// Kick 立即触发一轮投递（对方回归 / 新消息入队时调用）。
func (o *Outbox) Kick() {
	select {
	case o.kick <- struct{}{}:
	default:
	}
}

// RetryNow 对方回归：立即重置该节点所有待投消息的投递时间（退避不挡回归补投）。
func (o *Outbox) RetryNow(to string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now().Unix()
	changed := false
	for i := range o.Msgs {
		if o.Msgs[i].To == to && o.Msgs[i].NextAt > now {
			o.Msgs[i].NextAt = now
			changed = true
		}
	}
	if changed {
		o.saveLocked()
	}
}

func (o *Outbox) saveLocked() {
	raw, _ := json.MarshalIndent(o, "", " ")
	tmp := o.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, o.path)
	}
}

// Push 入队（立即可投）。返回消息 ID（队满或序列化失败返回空）。
func (o *Outbox) Push(to, kind string, payload any) string {
	body, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.Msgs) >= outboxMaxMsgs {
		return ""
	}
	now := time.Now().Unix()
	m := OutboxMsg{ID: randomHex(12), To: to, Kind: kind, Payload: body, CreatedAt: now, NextAt: now}
	o.Msgs = append(o.Msgs, m)
	o.saveLocked()
	return m.ID
}

// Due 到期待投消息快照。
func (o *Outbox) Due(now int64) []OutboxMsg {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []OutboxMsg
	for _, m := range o.Msgs {
		if m.NextAt <= now {
			out = append(out, m)
		}
	}
	return out
}

// Done 送达出队。
func (o *Outbox) Done(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i, m := range o.Msgs {
		if m.ID == id {
			o.Msgs = append(o.Msgs[:i], o.Msgs[i+1:]...)
			o.saveLocked()
			return
		}
	}
}

// Fail 投递失败：指数退避后再试（对方回归时到期自动补投）；重试超限弃投。
func (o *Outbox) Fail(id string, cause error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range o.Msgs {
		if o.Msgs[i].ID != id {
			continue
		}
		m := &o.Msgs[i]
		m.Attempts++
		if m.Attempts >= outboxMaxAttempts {
			// 僵尸消息：永久不可达的对方不再占位（删除节点/地址失效场景）
			o.Msgs = append(o.Msgs[:i], o.Msgs[i+1:]...)
			o.saveLocked()
			return
		}
		delay := outboxBackoffMax
		if m.Attempts <= 5 { // 30/60/120/240/480，之后恒 480（防移位溢出）
			delay = outboxBackoffBase << (m.Attempts - 1)
		}
		m.NextAt = time.Now().Unix() + int64(delay)
		m.LastErr = cause.Error()
		o.saveLocked()
		return
	}
}

// Remove 清空某节点的全部待投消息（节点删除时不留僵尸重试）。
func (o *Outbox) Remove(to string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	kept := make([]OutboxMsg, 0, len(o.Msgs))
	for _, m := range o.Msgs {
		if m.To != to {
			kept = append(kept, m)
		}
	}
	if len(kept) != len(o.Msgs) {
		o.Msgs = kept
		o.saveLocked()
	}
}

// PendingCount 待投条数（状态展示用）。
func (o *Outbox) PendingCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.Msgs)
}

// RunOutbox 投递循环：到期消息 → 组信封 → HTTP 投递；直连失败且配置了发现服务器时
// 走中继信箱回退（异步消息；kb.* 同步查询不入队不走中继）；对方回归自动补投。
// 收到 Kick（对方上线宣告 / 新消息入队）立即穿插一轮，不等周期。
// myPort=本方 HTTP 端口（写进信封供对方自愈地址）。
func RunOutbox(stop <-chan struct{}, o *Outbox, t *Table, me *Identity, myPort int, tick time.Duration, relayURL string) {
	tk := time.NewTicker(tick)
	defer tk.Stop()
	for {
		select {
		case <-stop:
			return
		case <-o.kick:
		case <-tk.C:
		}
		for _, m := range o.Due(time.Now().Unix()) {
			p, ok := t.Get(m.To)
			addr := PeerAddr(p)
			canSeal := ok && p.XPub != ""
			var env Envelope
			var err error
			if canSeal {
				env, err = MakeEnvelope(me, m.To, p.XPub, myPort, m.Kind, m.Payload)
				if err != nil {
					o.Fail(m.ID, err)
					continue
				}
			}
			delivered := false
			var lastErr error
			if canSeal && addr != "" {
				if err := PostEnvelope(addr, env); err == nil {
					delivered = true
				} else {
					lastErr = err
				}
			}
			if !delivered && relayURL != "" && canSeal && !strings.HasPrefix(m.Kind, "kb.") {
				// 直连失败/无地址 → 中继信箱回退（端到端密文照旧）
				if err := relayEnvelope(relayURL, m.To, me.NodeID(), env); err == nil {
					delivered = true
				} else {
					lastErr = err
				}
			}
			if delivered {
				o.Done(m.ID)
				continue
			}
			if lastErr == nil {
				lastErr = errors.New("对方地址/密钥未知")
			}
			o.Fail(m.ID, lastErr)
		}
	}
}
