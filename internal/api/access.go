package api

// —— P2P 访问申请制共享：请求 / 审批 / 撤销 ——
// 消息走节点 RPC（Ed25519 签名 + X25519 加密封载），对方掉线进离线队列自动补投。

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"imahub/internal/peer"
	"imahub/internal/store"
)

// accessScopes 合法访问范围三档。
var accessScopes = map[string]bool{"search": true, "browse": true, "download": true}

func validScopes(scopes []string) ([]string, bool) {
	out := make([]string, 0, len(scopes))
	seen := map[string]bool{}
	for _, sc := range scopes {
		sc = strings.TrimSpace(sc)
		if !accessScopes[sc] || seen[sc] {
			return nil, false
		}
		seen[sc] = true
		out = append(out, sc)
	}
	return out, len(out) > 0
}

func trimNote(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > 200 {
		r = r[:200]
	}
	return string(r)
}

func accessView(ar store.AccessRequest) map[string]any {
	return map[string]any{
		"id": ar.ID, "direction": ar.Direction, "peer_id": ar.PeerID, "peer_nick": ar.PeerNick,
		"scopes": ar.Scopes, "kbs": ar.KBs, "note": ar.Note, "status": ar.Status,
		"created_at": ar.CreatedAt, "updated_at": ar.UpdatedAt, "expires_at": ar.ExpiresAt,
	}
}

// handleAccessList 申请列表（dir=sent|received、status 可筛）。
func (s *Server) handleAccessList(w http.ResponseWriter, r *http.Request, u *store.User) {
	dir := r.URL.Query().Get("dir")
	status := r.URL.Query().Get("status")
	out := []map[string]any{}
	for _, ar := range s.Store.AccessList() {
		if dir != "" && ar.Direction != dir {
			continue
		}
		if status != "" && ar.Status != status {
			continue
		}
		out = append(out, accessView(ar))
	}
	resp := map[string]any{"requests": out}
	if s.PeerOutbox != nil {
		resp["outbox_pending"] = s.PeerOutbox.PendingCount()
	}
	writeOK(w, resp)
}

// handleAccessCreate 发起访问申请（对方掉线也不阻塞：消息进队列补投）。
func (s *Server) handleAccessCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		To     string   `json:"to"`
		Scopes []string `json:"scopes"`
		KBs    []string `json:"kbs"`
		Note   string   `json:"note"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	scopes, ok := validScopes(req.Scopes)
	if !ok {
		writeBizErr(w, "范围只能是 search/browse/download")
		return
	}
	to, ok := s.PeerTable.Get(req.To)
	if !ok {
		writeBizErr(w, "节点不存在")
		return
	}
	if to.XPub == "" {
		writeBizErr(w, "对方缺加密公钥（等其重新宣告或用新邀请码添加）")
		return
	}
	now := time.Now().Unix()
	ar := store.AccessRequest{
		ID: "ar_" + store.RandomToken()[:12], Direction: "sent", PeerID: to.ID, PeerNick: to.Nickname,
		Scopes: scopes, KBs: req.KBs, Note: trimNote(req.Note), Status: "pending",
		CreatedAt: now, UpdatedAt: now,
	}
	s.Store.AccessUpsert(ar)
	msgID := ""
	if s.PeerOutbox != nil {
		msgID = s.PeerOutbox.Push(to.ID, "access.request", map[string]any{
			"request_id": ar.ID, "from_nick": s.PeerIdentity.Nickname,
			"scopes": scopes, "kbs": req.KBs, "note": ar.Note, "created_at": now,
		})
		s.PeerOutbox.Kick() // 入队立即投递一轮
	}
	writeOK(w, map[string]any{"request": accessView(ar), "delivery": "queued", "msg_id": msgID})
}

// handleAccessDecide 审批：approve/deny 仅对收到的待决申请；revoke 双向可用。
func (s *Server) handleAccessDecide(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	var req struct {
		Action    string   `json:"action"`
		Scopes    []string `json:"scopes"`
		ExpiresAt int64    `json:"expires_at"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	ar, ok := s.Store.AccessGet(id)
	if !ok {
		writeErr(w, http.StatusNotFound, 404, "申请不存在")
		return
	}
	now := time.Now().Unix()
	switch req.Action {
	case "approve":
		if ar.Direction != "received" || ar.Status != "pending" {
			writeBizErr(w, "只能批准收到的待决申请")
			return
		}
		scopes := ar.Scopes
		if len(req.Scopes) > 0 {
			v, ok := validScopes(req.Scopes)
			if !ok {
				writeBizErr(w, "范围只能是 search/browse/download")
				return
			}
			scopes = v
		}
		ar.Status, ar.Scopes, ar.UpdatedAt, ar.ExpiresAt = "approved", scopes, now, req.ExpiresAt
		s.PeerTable.GrantScopes(ar.PeerID, scopes) // 授权记入节点表（代理访问用）
	case "deny":
		if ar.Direction != "received" || ar.Status != "pending" {
			writeBizErr(w, "只能拒绝收到的待决申请")
			return
		}
		ar.Status, ar.UpdatedAt = "denied", now
	case "revoke":
		// 撤回发出的申请 / 撤销已给的授权 / 放弃得到的授权
		ar.Status, ar.UpdatedAt = "revoked", now
		if ar.Direction == "received" {
			s.PeerTable.GrantScopes(ar.PeerID, nil)
		}
	default:
		writeBizErr(w, "action 只能是 approve/deny/revoke")
		return
	}
	s.Store.AccessUpsert(ar)
	if s.PeerOutbox != nil {
		s.PeerOutbox.Push(ar.PeerID, "access.decision", map[string]any{
			"request_id": ar.ID, "status": ar.Status, "scopes": ar.Scopes,
			"expires_at": ar.ExpiresAt, "updated_at": ar.UpdatedAt,
		})
		s.PeerOutbox.Kick()
	}
	writeOK(w, map[string]any{"request": accessView(ar), "delivery": "queued"})
}

// handlePeerRPC 节点间 RPC 通道：免 Cookie，信封 Ed25519 签名鉴权 + X25519 加密封载。
func (s *Server) handlePeerRPC(w http.ResponseWriter, r *http.Request) {
	var e peer.Envelope
	if !decodeBody(w, r, &e) {
		return
	}
	if err := peer.VerifyEnvelope(e); err != nil {
		writeErr(w, http.StatusBadRequest, 400, "信封无效: "+err.Error())
		return
	}
	if e.To != s.PeerIdentity.NodeID() {
		writeErr(w, http.StatusBadRequest, 400, "信封不是发给本节点")
		return
	}
	p, ok := s.PeerTable.Get(e.From)
	if !ok {
		writeErr(w, http.StatusForbidden, 403, "未知节点（先添加节点或让其在局域网发现）")
		return
	}
	if p.XPub != "" && e.XPub != "" && p.XPub != e.XPub {
		writeErr(w, http.StatusForbidden, 403, "对方加密公钥漂移，已拒绝")
		return
	}
	body, err := s.PeerIdentity.OpenEnvelope(e)
	if err != nil {
		writeErr(w, http.StatusBadRequest, 400, "载荷解密失败")
		return
	}
	// 实见地址自愈：真实可达地址覆盖声称地址（签名信封已证明身份，防假地址投递失败）
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && e.Port > 0 {
		s.PeerTable.Upsert(peer.Peer{
			ID: e.From, Pub: p.Pub, XPub: e.XPub,
			Addr: host, TCPPort: e.Port, LastSeen: time.Now().Unix(),
		})
	}
	// 知识库查询：同步加密回包应答（kb.list/search/browse/download）
	if strings.HasPrefix(e.Kind, "kb.") {
		s.handlePeerKBQuery(w, r, e, body)
		return
	}
	now := time.Now().Unix()
	switch e.Kind {
	case "access.request":
		var in struct {
			RequestID string   `json:"request_id"`
			FromNick  string   `json:"from_nick"`
			Scopes    []string `json:"scopes"`
			KBs       []string `json:"kbs"`
			Note      string   `json:"note"`
			CreatedAt int64    `json:"created_at"`
		}
		if err := json.Unmarshal(body, &in); err != nil || in.RequestID == "" {
			writeErr(w, http.StatusBadRequest, 400, "载荷非法")
			return
		}
		scopes, ok := validScopes(in.Scopes)
		if !ok {
			writeErr(w, http.StatusBadRequest, 400, "范围非法")
			return
		}
		// 重投幂等：已存在的申请（可能已被审批）不得被原消息覆盖
		if _, exists := s.Store.AccessGet(in.RequestID); exists {
			writeOK(w, map[string]any{"ok": true, "dup": true})
			return
		}
		created := in.CreatedAt
		if created <= 0 {
			created = now
		}
		s.Store.AccessUpsert(store.AccessRequest{
			ID: in.RequestID, Direction: "received", PeerID: e.From, PeerNick: peer.SanitizeNick(in.FromNick),
			Scopes: scopes, KBs: in.KBs, Note: trimNote(in.Note), Status: "pending",
			CreatedAt: created, UpdatedAt: now,
		})
	case "access.decision":
		var in struct {
			RequestID string   `json:"request_id"`
			Status    string   `json:"status"`
			Scopes    []string `json:"scopes"`
			ExpiresAt int64    `json:"expires_at"`
			UpdatedAt int64    `json:"updated_at"`
		}
		if err := json.Unmarshal(body, &in); err != nil || in.RequestID == "" {
			writeErr(w, http.StatusBadRequest, 400, "载荷非法")
			return
		}
		switch in.Status {
		case "approved", "denied", "revoked", "expired":
		default:
			writeErr(w, http.StatusBadRequest, 400, "状态非法")
			return
		}
		ar, ok := s.Store.AccessGet(in.RequestID)
		if !ok || ar.Direction != "sent" {
			// 未知/错向申请：幂等吞掉（重投安全）
			writeOK(w, map[string]any{"ok": true, "dup": true})
			return
		}
		ar.Status = in.Status
		if len(in.Scopes) > 0 {
			ar.Scopes = in.Scopes
		}
		ar.ExpiresAt = in.ExpiresAt
		upd := in.UpdatedAt
		if upd <= 0 {
			upd = now
		}
		ar.UpdatedAt = upd
		s.Store.AccessUpsert(ar)
	default:
		writeErr(w, http.StatusBadRequest, 400, "未知消息类型: "+e.Kind)
		return
	}
	writeOK(w, map[string]any{"ok": true})
}
