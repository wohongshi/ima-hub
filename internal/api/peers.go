package api

import (
	"net/http"
	"strings"
	"time"

	"imahub/internal/peer"
	"imahub/internal/store"
)

// onlineWindowSec 超过此时长未宣告视为离线（3 个宣告周期）。
const onlineWindowSec = 90

func (s *Server) peerView(p peer.Peer) map[string]any {
	return map[string]any{
		"id":        p.ID,
		"id_pretty": peer.FormatID(p.ID),
		"nickname":  p.DisplayNick(),
		"source":    p.Source,
		"addr":      p.Addr,
		"trusted":   p.Trusted,
		"online":    p.LastSeen >= time.Now().Unix()-onlineWindowSec,
		"scopes":    p.Scopes,
		"granted":   s.grantedToMe(p.ID),
		"last_seen": p.LastSeen,
	}
}

// grantedToMe 对方授予我的访问范围（我发出且已批准、未过期申请的并集）。
// 与 scopes 方向相反：scopes 是我授予对方的，granted 是对方授予我的。
func (s *Server) grantedToMe(peerID string) []string {
	now := time.Now().Unix()
	g := map[string]bool{}
	for _, ar := range s.Store.AccessList() {
		if ar.Direction != "sent" || ar.PeerID != peerID || ar.Status != "approved" {
			continue
		}
		if ar.ExpiresAt > 0 && now > ar.ExpiresAt {
			continue
		}
		for _, sc := range ar.Scopes {
			g[sc] = true
		}
	}
	out := []string{}
	for _, sc := range []string{"search", "browse", "download"} {
		if g[sc] {
			out = append(out, sc)
		}
	}
	return out
}

func (s *Server) meView() map[string]any {
	return map[string]any{
		"id":        s.PeerIdentity.NodeID(),
		"id_pretty": peer.FormatID(s.PeerIdentity.NodeID()),
		"nickname":  s.PeerIdentity.Nick(),
	}
}

// handlePeers 节点列表（P2P 发现结果）+ 我的节点。
func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request, u *store.User) {
	peers := []map[string]any{}
	for _, p := range s.PeerTable.List() {
		peers = append(peers, s.peerView(p))
	}
	writeOK(w, map[string]any{
		"me":       s.meView(),
		"peers":    peers,
		"lan_port": s.PeerLanPort,
	})
}

// handlePeersMe 我的节点信息。
func (s *Server) handlePeersMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeOK(w, s.meView())
}

// handlePeersMeNickname 改我的昵称。
func (s *Server) handlePeersMeNickname(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Nickname string `json:"nickname"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if err := s.PeerIdentity.SetNickname(req.Nickname); err != nil {
		writeBizErr(w, err.Error())
		return
	}
	writeOK(w, s.meView())
}

// handlePeersMakeInvite 生成邀请码（可带对外地址 ip:port）。
func (s *Server) handlePeersMakeInvite(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Addrs []string `json:"addrs"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.Addrs) > 8 {
		writeBizErr(w, "地址最多 8 个")
		return
	}
	code, err := s.PeerIdentity.MakeInvite(req.Addrs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	writeOK(w, map[string]any{"code": code})
}

// handlePeersAddInvite 凭邀请码添加节点（即信任，为申请制前置）。
func (s *Server) handlePeersAddInvite(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	p, err := peer.ParseInvite(req.Code)
	if err != nil {
		writeBizErr(w, err.Error())
		return
	}
	if p.ID == s.PeerIdentity.NodeID() {
		writeBizErr(w, "不能添加自己")
		return
	}
	s.PeerTable.Upsert(p)
	if got, ok := s.PeerTable.Get(p.ID); ok {
		p = got
	}
	writeOK(w, map[string]any{"peer": s.peerView(p)})
}

// handlePeersPatch 节点本地备注名。
func (s *Server) handlePeersPatch(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	var req struct {
		Nickname *string `json:"nickname"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Nickname == nil || strings.TrimSpace(*req.Nickname) == "" {
		writeBizErr(w, "没有要修改的字段")
		return
	}
	if !s.PeerTable.SetNickname(id, *req.Nickname) {
		writeErr(w, http.StatusNotFound, 404, "节点不存在或昵称不合法")
		return
	}
	p, _ := s.PeerTable.Get(id)
	writeOK(w, map[string]any{"peer": s.peerView(p)})
}

// handlePeersDelete 删除节点。
func (s *Server) handlePeersDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	if !s.PeerTable.Remove(id) {
		writeErr(w, http.StatusNotFound, 404, "节点不存在")
		return
	}
	if s.PeerOutbox != nil {
		s.PeerOutbox.Remove(id) // 不留僵尸重试占位
	}
	s.Store.AccessRemovePeer(id) // 不留残余授权
	writeOK(w, nil)
}
