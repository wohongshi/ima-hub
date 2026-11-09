package api

// —— 群组：对等节点的任务/日程共享圈 ——
// 无管理员：建组者发邀请即入组，人人可发任务/日程；变更经 RPC 广播（离线走 outbox 补投），
// 收端 LWW+墓碑收敛（复用 SyncMerge 语义）。解散为终局墓碑。

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"imahub/internal/store"
)

// groupEventMsg 群内条目变更信封载荷。
type groupEventMsg struct {
	GroupID  string          `json:"group_id"`
	ItemKind string          `json:"item_kind"`
	Op       string          `json:"op"`
	Item     json.RawMessage `json:"item"`
	TombTs   int64           `json:"tomb_ts"`
}

// groupLeaveMsg 成员离组信封载荷。
type groupLeaveMsg struct {
	GroupID string `json:"group_id"`
	NodeID  string `json:"node_id"`
}

// groupView 群组视图（成员带显示名）。
func (s *Server) groupView(g store.Group) map[string]any {
	members := make([]map[string]any, 0, len(g.Members))
	for _, m := range g.Members {
		name := m
		if len(name) > 10 {
			name = name[:10]
		}
		if p, ok := s.PeerTable.Get(m); ok {
			name = p.DisplayNick()
		}
		if s.PeerIdentity != nil && m == s.PeerIdentity.NodeID() {
			name = "我"
		}
		members = append(members, map[string]any{"id": m, "nick": name})
	}
	return map[string]any{
		"id": g.ID, "name": g.Name, "members": members,
		"created_by": g.CreatedBy, "created_at": g.CreatedAt, "updated_at": g.UpdatedAt,
	}
}

// groupBroadcast 给群内其他成员逐个投递消息（outbox 保证离线不丢）。
func (s *Server) groupBroadcast(g store.Group, kind string, payload map[string]any) {
	if s.PeerOutbox == nil || s.PeerIdentity == nil {
		return
	}
	me := s.PeerIdentity.NodeID()
	for _, m := range g.Members {
		if m == me {
			continue
		}
		s.PeerOutbox.Push(m, kind, payload)
	}
	s.PeerOutbox.Kick()
}

// broadcastGroupUpsert 广播群组形态（建/改名/加人/解散）。
func (s *Server) broadcastGroupUpsert(g store.Group) {
	s.groupBroadcast(g, "group.upsert", map[string]any{"group": g})
}

// broadcastItem 广播群内条目变更（任务/日程 upsert/delete）。
func (s *Server) broadcastItem(g store.Group, itemKind, op string, item any, tombTs int64) {
	s.groupBroadcast(g, "group.event", map[string]any{
		"group_id": g.ID, "item_kind": itemKind, "op": op,
		"item": item, "tomb_ts": tombTs,
	})
}

// hookTaskChanged 任务增改删后广播到所在群（仅群组条目）。
func (s *Server) hookTaskChanged(op string, t store.Task) {
	if t.GroupID == "" {
		return
	}
	if g, ok := s.Store.GroupGet(t.GroupID); ok && !g.Deleted {
		s.broadcastItem(g, "task", op, t, 0)
	}
}

// hookTaskDeleted 任务删除广播（墓碑时间戳给对端做 LWW）。
func (s *Server) hookTaskDeleted(t store.Task) {
	if t.GroupID == "" {
		return
	}
	if g, ok := s.Store.GroupGet(t.GroupID); ok && !g.Deleted {
		s.broadcastItem(g, "task", "delete", map[string]any{"id": t.ID}, time.Now().Unix())
	}
}

// hookEventChanged / hookEventDeleted 日程同构。
func (s *Server) hookEventChanged(op string, e store.Event) {
	if e.GroupID == "" {
		return
	}
	if g, ok := s.Store.GroupGet(e.GroupID); ok && !g.Deleted {
		s.broadcastItem(g, "event", op, e, 0)
	}
}

func (s *Server) hookEventDeleted(e store.Event) {
	if e.GroupID == "" {
		return
	}
	if g, ok := s.Store.GroupGet(e.GroupID); ok && !g.Deleted {
		s.broadcastItem(g, "event", "delete", map[string]any{"id": e.ID}, time.Now().Unix())
	}
}

// handleGroupList GET /api/groups 我的群组。
func (s *Server) handleGroupList(w http.ResponseWriter, r *http.Request, u *store.User) {
	if s.PeerIdentity == nil {
		writeBizErr(w, "P2P 未启用")
		return
	}
	out := []map[string]any{}
	for _, g := range s.Store.GroupList() {
		out = append(out, s.groupView(g))
	}
	writeOK(w, map[string]any{"groups": out})
}

// handleGroupCreate POST /api/groups {name, members[]} 建组 + 邀请。
func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	if s.PeerIdentity == nil {
		writeBizErr(w, "P2P 未启用")
		return
	}
	var req struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 40 {
		writeBizErr(w, "群名不能为空且不超过 40 字")
		return
	}
	me := s.PeerIdentity.NodeID()
	members := []string{me}
	for _, m := range req.Members {
		m = strings.TrimSpace(m)
		if m == "" || m == me {
			continue
		}
		if _, ok := s.PeerTable.Get(m); !ok {
			writeBizErr(w, "成员节点不存在："+m)
			return
		}
		members = append(members, m)
	}
	now := time.Now().Unix()
	g := store.Group{
		ID: "grp_" + store.RandomToken()[:12], Name: req.Name, Members: members,
		CreatedBy: me, CreatedAt: now, UpdatedAt: now,
	}
	s.Store.GroupUpsert(g)
	s.broadcastGroupUpsert(g)
	writeOK(w, map[string]any{"group": s.groupView(g), "delivery": "queued"})
}

// handleGroupUpdate PATCH /api/groups/{id} {name?, add?[]}。
func (s *Server) handleGroupUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	g, ok := s.Store.GroupGet(id)
	if !ok || g.Deleted {
		writeErr(w, http.StatusNotFound, 404, "群组不存在")
		return
	}
	var req struct {
		Name string   `json:"name"`
		Add  []string `json:"add"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if n := strings.TrimSpace(req.Name); n != "" && len([]rune(n)) <= 40 {
		g.Name = n
	}
	for _, m := range req.Add {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := s.PeerTable.Get(m); !ok {
			writeBizErr(w, "成员节点不存在："+m)
			return
		}
		found := false
		for _, x := range g.Members {
			if x == m {
				found = true
				break
			}
		}
		if !found {
			g.Members = append(g.Members, m)
		}
	}
	g.UpdatedAt = store.BumpTS(g.UpdatedAt, time.Now().Unix())
	s.Store.GroupUpsert(g)
	s.broadcastGroupUpsert(g)
	writeOK(w, map[string]any{"group": s.groupView(g)})
}

// handleGroupDelete DELETE /api/groups/{id} 解散（终局墓碑广播）。
func (s *Server) handleGroupDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	g, ok := s.Store.GroupGet(id)
	if !ok {
		writeErr(w, http.StatusNotFound, 404, "群组不存在")
		return
	}
	now := store.BumpTS(g.UpdatedAt, time.Now().Unix())
	g.Deleted = true
	g.UpdatedAt = now
	s.Store.GroupUpsert(g) // 墓碑（LWW：新于现存即生效，解散终局）
	s.broadcastGroupUpsert(g)
	writeOK(w, nil)
}

// handleGroupLeave POST /api/groups/{id}/leave 自己退组。
func (s *Server) handleGroupLeave(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	if s.PeerIdentity == nil {
		writeBizErr(w, "P2P 未启用")
		return
	}
	me := s.PeerIdentity.NodeID()
	g, ok := s.Store.GroupGet(id)
	if !ok || g.Deleted {
		writeErr(w, http.StatusNotFound, 404, "群组不存在")
		return
	}
	now := time.Now().Unix()
	s.Store.GroupLeave(id, me, now)
	if !s.inGroup(id) {
		s.Store.GroupForget(id) // 自己退组：本地销组（他人退组不销）
	}
	s.groupBroadcast(g, "group.leave", map[string]any{"group_id": id, "node_id": me})
	writeOK(w, nil)
}

// handleGroupFeed GET /api/groups/{id}/feed 群内任务+日程（本地收敛结果）。
func (s *Server) handleGroupFeed(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	g, ok := s.Store.GroupGet(id)
	if !ok || g.Deleted {
		writeErr(w, http.StatusNotFound, 404, "群组不存在")
		return
	}
	tasks := []store.Task{}
	for _, t := range s.Store.ListTasks() {
		if t.GroupID == id {
			tasks = append(tasks, t)
		}
	}
	events := []store.Event{}
	for _, e := range s.Store.ListEvents() {
		if e.GroupID == id {
			events = append(events, e)
		}
	}
	writeOK(w, map[string]any{"group": s.groupView(g), "tasks": tasks, "events": events})
}

// handlePeerGroupUpsert RPC 收敛：群组形态（邀请/改名/解散）。
func (s *Server) handlePeerGroupUpsert(g store.Group) {
	s.Store.GroupUpsert(g)
}

// handlePeerGroupEvent RPC 收敛：群内条目变更（LWW+墓碑）。
// 只收「我仍是成员的组」的条目（退组/解散后停收，防杂物回流）。
func (s *Server) handlePeerGroupEvent(in groupEventMsg) {
	if !s.inGroup(in.GroupID) {
		return
	}
	if in.Op == "delete" && in.TombTs > 0 {
		var meta struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(in.Item, &meta)
		if meta.ID == "" {
			return
		}
		prefix := "task:"
		if in.ItemKind == "event" {
			prefix = "event:"
		}
		s.Store.SyncMerge(nil, nil, map[string]int64{prefix + meta.ID: in.TombTs})
		return
	}
	switch in.ItemKind {
	case "task":
		var t store.Task
		if json.Unmarshal(in.Item, &t) == nil && t.ID != "" && t.GroupID == in.GroupID {
			s.Store.SyncMerge([]store.Task{t}, nil, nil)
		}
	case "event":
		var e store.Event
		if json.Unmarshal(in.Item, &e) == nil && e.ID != "" && e.GroupID == in.GroupID {
			s.Store.SyncMerge(nil, []store.Event{e}, nil)
		}
	}
}

// inGroup 我是否仍是该组成员。
func (s *Server) inGroup(gid string) bool {
	if s.PeerIdentity == nil {
		return false
	}
	g, ok := s.Store.GroupGet(gid)
	if !ok || g.Deleted {
		return false
	}
	me := s.PeerIdentity.NodeID()
	for _, m := range g.Members {
		if m == me {
			return true
		}
	}
	return false
}

// handlePeerGroupLeave RPC：成员离组。
func (s *Server) handlePeerGroupLeave(in groupLeaveMsg) {
	s.Store.GroupLeave(in.GroupID, in.NodeID, time.Now().Unix())
}
