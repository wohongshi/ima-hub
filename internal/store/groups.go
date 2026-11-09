package store

import "sort"

// —— 群组：对等节点的任务/日程共享圈（无管理员，人人可发，LWW 收敛） ——

// Group 群组：一组节点的共享圈；成员共同读写任务/日程。
type Group struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Members   []string `json:"members"`    // 成员 NodeID 集合（含创建者）
	CreatedBy string   `json:"created_by"` // 创建者 NodeID（记录归属，非管理权）
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
	Deleted   bool     `json:"deleted,omitempty"` // 解散墓碑（终局）
}

// GroupUpsert 合并群组（LWW：UpdatedAt 新者胜；解散为终局；成员并集防丢人）。
// 离组/移除成员不走本口（并集会抹掉删除），用 GroupLeave。
func (s *Store) GroupUpsert(g Group) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Groups == nil {
		s.d.Groups = map[string]Group{}
	}
	old, exists := s.d.Groups[g.ID]
	if exists {
		if old.Deleted {
			return false // 解散终局
		}
		if old.UpdatedAt > g.UpdatedAt {
			return false
		}
		if old.UpdatedAt == g.UpdatedAt {
			return false // 同刻后写不覆盖（保持稳定）
		}
		// 成员并集：多端同时加人互不覆盖
		seen := map[string]bool{}
		merged := make([]string, 0, len(old.Members)+len(g.Members))
		for _, m := range old.Members {
			if !seen[m] {
				seen[m] = true
				merged = append(merged, m)
			}
		}
		for _, m := range g.Members {
			if !seen[m] {
				seen[m] = true
				merged = append(merged, m)
			}
		}
		g.Members = merged
		if g.Name == "" {
			g.Name = old.Name
		}
	}
	s.d.Groups[g.ID] = g
	_ = s.saveLocked()
	return true
}

// GroupLeave 成员离组（对称移除，供离组/移除场景；并集合并抹不掉这个动作）。
func (s *Store) GroupLeave(id, nodeID string, ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.d.Groups[id]
	if !ok || g.Deleted {
		return
	}
	kept := g.Members[:0]
	for _, m := range g.Members {
		if m != nodeID {
			kept = append(kept, m)
		}
	}
	if len(kept) == len(g.Members) {
		return
	}
	g.Members = kept
	g.UpdatedAt = BumpTS(g.UpdatedAt, ts)
	s.d.Groups[id] = g
	_ = s.saveLocked()
}

// GroupForget 本地销组（仅当「我自己退组」时调用；他人退组不销）。
// 组历史条目保留，组更新经「无组即忽略」自然停收。
func (s *Store) GroupForget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Groups == nil {
		return
	}
	delete(s.d.Groups, id)
	_ = s.saveLocked()
}

// GroupList 未解散群组（最近更新优先）。
func (s *Store) GroupList() []Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Group, 0, len(s.d.Groups))
	for _, g := range s.d.Groups {
		if !g.Deleted {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// GroupGet 单查（含已解散的——同步路径用）。
func (s *Store) GroupGet(id string) (Group, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.d.Groups[id]
	return g, ok
}

// GroupRemoveLocal 群组解散本地落墓碑（事件条目保留历史）。
func (s *Store) GroupRemoveLocal(id string, ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Groups == nil {
		return
	}
	if g, ok := s.d.Groups[id]; ok {
		g.Deleted = true
		g.UpdatedAt = ts
		s.d.Groups[id] = g
		_ = s.saveLocked()
	}
}
