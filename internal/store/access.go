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

// AccessUpsert 合并申请（同 ID：UpdatedAt 新者胜，保证双方状态收敛）。
// 注意：请求消息重投不得覆盖审批结果——处理层先查存在性做幂等。
func (s *Store) AccessUpsert(ar AccessRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.AccessRequests == nil {
		s.d.AccessRequests = map[string]AccessRequest{}
	}
	if old, ok := s.d.AccessRequests[ar.ID]; ok && old.UpdatedAt > ar.UpdatedAt {
		return
	}
	s.d.AccessRequests[ar.ID] = ar
	_ = s.saveLocked()
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
