package store

import (
	"sort"
)

// —— P2P 访问申请制共享：请求 / 审批 / 撤销 ——

// AccessRequest 一条访问申请：我发出的（sent）或我收到的（received）。
type AccessRequest struct {
	ID        string   `json:"id"`
	Direction string   `json:"direction"` // sent 发出 | received 收到
	PeerID    string   `json:"peer_id"`   // 对方节点 ID
	PeerNick  string   `json:"peer_nick,omitempty"`
	Scopes    []string `json:"scopes"` // search 搜索 | browse 浏览 | download 下载
	KBs       []string `json:"kbs,omitempty"`
	Note      string   `json:"note,omitempty"`
	Status    string   `json:"status"` // pending | approved | denied | revoked | expired
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
	ExpiresAt int64    `json:"expires_at,omitempty"`
}

// accessCap 访问申请容量上限（防未鉴权邻居无限灌记录写爆 data.json）。
const accessCap = 2000

// accessStatusPriority 状态优先级：终态压过过程态。同时间戳时按此确定性收敛，
// 防止旧消息同秒重投把 revoked 回退成 approved。
func accessStatusPriority(st string) int {
	switch st {
	case "pending":
		return 1
	case "approved":
		return 2
	case "expired":
		return 3
	case "denied", "revoked":
		return 4
	}
	return 0
}

// AccessUpsert 合并申请（同 ID：UpdatedAt 新者胜；同刻状态优先级高者胜）。
// 返回 false 表示本次写入被拒（较旧/较弱/容量满），调用方必须据此反馈，不许静默吞。
func (s *Store) AccessUpsert(ar AccessRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.AccessRequests == nil {
		s.d.AccessRequests = map[string]AccessRequest{}
	}
	if old, ok := s.d.AccessRequests[ar.ID]; ok {
		if old.UpdatedAt > ar.UpdatedAt {
			return false
		}
		if old.UpdatedAt == ar.UpdatedAt && accessStatusPriority(old.Status) >= accessStatusPriority(ar.Status) {
			return false
		}
		s.d.AccessRequests[ar.ID] = ar
		_ = s.saveLocked()
		return true
	}
	// 新记录：容量满时先淘汰最旧的终态（pending 一律保留，审批不能被挤丢）
	if len(s.d.AccessRequests) >= accessCap {
		dropID, dropAt := "", int64(0)
		for id, x := range s.d.AccessRequests {
			if x.Status == "pending" {
				continue
			}
			if dropID == "" || x.UpdatedAt < dropAt {
				dropID, dropAt = id, x.UpdatedAt
			}
		}
		if dropID == "" {
			return false // 全是 pending，拒绝新灌入
		}
		delete(s.d.AccessRequests, dropID)
	}
	s.d.AccessRequests[ar.ID] = ar
	_ = s.saveLocked()
	return true
}

// AccessExpireSweep 把已过期的 pending/approved 懒惰收敛为 expired（过期授权即断）。
func (s *Store) AccessExpireSweep(now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, ar := range s.d.AccessRequests {
		if ar.ExpiresAt > 0 && now > ar.ExpiresAt && (ar.Status == "pending" || ar.Status == "approved") {
			ar.Status = "expired"
			ar.UpdatedAt = now
			s.d.AccessRequests[id] = ar
			changed = true
		}
	}
	if changed {
		_ = s.saveLocked()
	}
}

// AccessRemovePeer 清理某节点的全部申请（节点删除时不留残余授权）。
func (s *Store) AccessRemovePeer(peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, ar := range s.d.AccessRequests {
		if ar.PeerID == peerID {
			delete(s.d.AccessRequests, id)
			changed = true
		}
	}
	if changed {
		_ = s.saveLocked()
	}
}

// AccessList 全量申请（最近更新优先）。
func (s *Store) AccessList() []AccessRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AccessRequest, 0, len(s.d.AccessRequests))
	for _, ar := range s.d.AccessRequests {
		out = append(out, ar)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// AccessGet 单查。
func (s *Store) AccessGet(id string) (AccessRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ar, ok := s.d.AccessRequests[id]
	return ar, ok
}
