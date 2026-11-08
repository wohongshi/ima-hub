package peer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Peer 已发现 / 已信任的节点。
type Peer struct {
	ID       string   `json:"id"`
	Nickname string   `json:"nickname"`
	Pub      string   `json:"pub"`
	XPub     string   `json:"xpub,omitempty"` // X25519 公钥（信封加密用）
	Addr     string   `json:"addr,omitempty"`
	TCPPort  int      `json:"tcp_port,omitempty"`
	Source   string   `json:"source"`  // lan | invite
	Trusted  bool     `json:"trusted"` // 邀请码加入即信任（申请制前置）
	LastSeen int64    `json:"last_seen"`
	Scopes   []string `json:"scopes,omitempty"` // 已授予对方的访问范围（申请制）
}

// Table 节点表：内存 map + peers.json 持久化（0600）。
type Table struct {
	mu    sync.RWMutex
	path  string
	peers map[string]Peer
}

// OpenTable 加载（或初始化）节点表。
func OpenTable(dir string) (*Table, error) {
	t := &Table{path: filepath.Join(dir, "peers.json"), peers: map[string]Peer{}}
	raw, err := os.ReadFile(t.path)
	if err == nil {
		if err := json.Unmarshal(raw, &t.peers); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return t, nil
}

// Upsert 合并节点信息（昵称/地址/端口取最新，信任并集，LastSeen 取最大）。
func (t *Table) Upsert(p Peer) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	old, ok := t.peers[p.ID]
	changed := !ok
	if ok {
		if p.Nickname != "" && p.Nickname != old.Nickname {
			old.Nickname = p.Nickname
			changed = true
		}
		if p.Addr != "" && p.Addr != old.Addr {
			old.Addr = p.Addr
			changed = true
		}
		if p.TCPPort != 0 && p.TCPPort != old.TCPPort {
			old.TCPPort = p.TCPPort
			changed = true
		}
		if p.Pub != "" && old.Pub == "" {
			old.Pub = p.Pub
		}
		if p.XPub != "" && old.XPub == "" {
			old.XPub = p.XPub
		}
		if len(p.Scopes) > 0 {
			for _, sc := range p.Scopes {
				dup := false
				for _, os := range old.Scopes {
					if os == sc {
						dup = true
						break
					}
				}
				if !dup {
					old.Scopes = append(old.Scopes, sc)
					changed = true
				}
			}
		}
		if p.Trusted && !old.Trusted {
			old.Trusted = true
			changed = true
		}
		if p.Source == "invite" && old.Source != "invite" {
			old.Source = p.Source
		}
		if p.LastSeen > old.LastSeen {
			old.LastSeen = p.LastSeen
			changed = true
		}
		t.peers[p.ID] = old
	} else {
		if p.LastSeen == 0 {
			p.LastSeen = time.Now().Unix()
		}
		t.peers[p.ID] = p
	}
	if changed {
		t.saveLocked()
	}
	return changed
}

// List 全量快照（最近出现优先）。
func (t *Table) List() []Peer {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Peer, 0, len(t.peers))
	for _, p := range t.peers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
	return out
}

// Get 单查。
func (t *Table) Get(id string) (Peer, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.peers[id]
	return p, ok
}

// GrantScopes 授权/更新已授予对方的访问范围（申请制批准时调用）。
func (t *Table) GrantScopes(id string, scopes []string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.peers[id]
	if !ok {
		return false
	}
	p.Scopes = scopes
	t.peers[id] = p
	t.saveLocked()
	return true
}

// SetNickname 本地备注名（不改变对方自称）。
func (t *Table) SetNickname(id, nick string) bool {
	nick = SanitizeNick(nick)
	if nick == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.peers[id]
	if !ok {
		return false
	}
	p.Nickname = nick
	t.peers[id] = p
	t.saveLocked()
	return true
}

// Remove 删除节点。
func (t *Table) Remove(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.peers[id]; !ok {
		return false
	}
	delete(t.peers, id)
	t.saveLocked()
	return true
}

func (t *Table) saveLocked() {
	raw, _ := json.MarshalIndent(t.peers, "", " ")
	tmp := t.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, t.path)
	}
}

// ShortID 短指纹（列表省略显示用）。
func ShortID(id string) string {
	if len(id) > 10 {
		return id[:10]
	}
	return strings.ToUpper(id)
}
