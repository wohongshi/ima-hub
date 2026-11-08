package api

// —— 知识库代理访问：人人都是客户端，同时人人都是服务端 ——
// 服务端半：所有者节点用本机 ima key 应答已批准范围内的列表/检索/浏览/下载，key 不出户；
// 客户端半：经申请制拿到授权后，通过加密 RPC 查询/下载对方知识库。
// 查询走同步加密回程信封（kb.reply）——对方离线即报错（其 key 在其机器上，等其上线）。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"imahub/internal/ima"
	"imahub/internal/peer"
	"imahub/internal/store"
)

const kbDownloadCap = 32 << 20 // 经节点代理下载的单文件上限

// ---------- 授权判定（纯函数，便于单测） ----------

// kbGrant 请求方在本节点的授权并集（多条未过期 approved 申请合并）。
// 每个范围单独记库集：browse 全库授权不会撑大 search 的白名单。
type kbGrant struct {
	allKBs map[string]bool            // scope -> 全库
	kbs    map[string]map[string]bool // scope -> 白名单
}

// kbGrantOf 从申请列表推导某节点的授权（received+approved，未过期）。
func kbGrantOf(reqs []store.AccessRequest, from string) kbGrant {
	g := kbGrant{allKBs: map[string]bool{}, kbs: map[string]map[string]bool{}}
	now := time.Now().Unix()
	for _, ar := range reqs {
		if ar.Direction != "received" || ar.PeerID != from || ar.Status != "approved" {
			continue
		}
		if ar.ExpiresAt > 0 && now > ar.ExpiresAt {
			continue
		}
		for _, sc := range ar.Scopes {
			if len(ar.KBs) == 0 {
				g.allKBs[sc] = true
				continue
			}
			if g.kbs[sc] == nil {
				g.kbs[sc] = map[string]bool{}
			}
			for _, k := range ar.KBs {
				g.kbs[sc][k] = true
			}
		}
	}
	return g
}

// has 该范围是否获批（不论库）。
func (g kbGrant) has(scope string) bool {
	return g.allKBs[scope] || len(g.kbs[scope]) > 0
}

// allow 范围 + 知识库双重判定（kb 空表示不限定具体库，交由调用方逐条过滤）。
func (g kbGrant) allow(scope, kb string) bool {
	if g.allKBs[scope] {
		return true
	}
	set := g.kbs[scope]
	if len(set) == 0 {
		return false
	}
	if kb == "" {
		return true
	}
	return set[kb]
}

// ---------- 服务端半：应答对方查询 ----------

// peerKBImaClients 服务方全部 ima 账号（本节点的 keys，聚合应答）。
func (s *Server) peerKBImaClients() []*ima.Client {
	accs := s.Store.AllIMAAccounts()
	out := make([]*ima.Client, 0, len(accs))
	for i := range accs {
		a := accs[i].IMAAccount
		if a.ClientID == "" || a.APIKey == "" {
			continue
		}
		out = append(out, s.clientFor(&a))
	}
	return out
}

// handlePeerKBQuery 处理对方的知识库查询（kb.*），生成加密回程信封同步应答。
func (s *Server) handlePeerKBQuery(w http.ResponseWriter, r *http.Request, e peer.Envelope, body []byte) {
	if e.XPub == "" {
		writeErr(w, http.StatusBadRequest, 400, "对方缺加密公钥")
		return
	}
	g := kbGrantOf(s.Store.AccessList(), e.From)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var reply map[string]any
	switch e.Kind {
	case "kb.list":
		reply = s.kbServeList(ctx, g)
	case "kb.search":
		var in struct {
			Query  string `json:"query"`
			KB     string `json:"kb"`
			Cursor string `json:"cursor"`
			Limit  uint64 `json:"limit"`
		}
		_ = json.Unmarshal(body, &in)
		reply = s.kbServeSearch(ctx, g, in.KB, in.Query, in.Cursor, in.Limit)
	case "kb.browse":
		var in struct {
			KB     string `json:"kb_id"`
			Folder string `json:"folder_id"`
			Cursor string `json:"cursor"`
			Limit  uint64 `json:"limit"`
		}
		_ = json.Unmarshal(body, &in)
		reply = s.kbServeBrowse(ctx, g, in.KB, in.Folder, in.Cursor, in.Limit)
	case "kb.download":
		var in struct {
			KB    string `json:"kb_id"`
			Media string `json:"media_id"`
			Name  string `json:"name"`
		}
		_ = json.Unmarshal(body, &in)
		reply = s.kbServeDownload(ctx, g, in.KB, in.Media, in.Name)
	default:
		writeErr(w, http.StatusBadRequest, 400, "未知查询: "+e.Kind)
		return
	}
	env, err := peer.MakeEnvelope(s.PeerIdentity, e.From, e.XPub, s.PeerAPIPort, "kb.reply", reply)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, "回包生成失败")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(env)
}

// kbAggKBs 跨账号并发聚合知识库条目（单账号 15s 超时；全部失败报错，不冒充空结果）。
// 单账号时透传上游分页游标；多账号聚合游标暂不支持（is_end 恒 true）。
func (s *Server) kbAggKBs(ctx context.Context, g kbGrant, scope, query, cursor string, limit uint64) ([]map[string]any, bool, string, error) {
	clients := s.peerKBImaClients()
	if len(clients) == 0 {
		return nil, true, "", errors.New("本节点未配置 ima 账号")
	}
	outs := make([][]map[string]any, len(clients))
	errs := make([]error, len(clients))
	ends := make([]bool, len(clients))
	curs := make([]string, len(clients))
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *ima.Client) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			rsp, e := c.SearchKnowledgeBase(cctx, query, cursor, limit)
			if e != nil {
				errs[i] = e
				return
			}
			ends[i], curs[i] = rsp.IsEnd, rsp.NextCursor
			for _, it := range rsp.InfoList {
				if !g.allow(scope, it.ID) {
					continue
				}
				outs[i] = append(outs[i], map[string]any{"id": it.ID, "name": it.Name, "cover_url": it.CoverURL})
			}
		}(i, c)
	}
	wg.Wait()
	fails := 0
	for _, e := range errs {
		if e != nil {
			log.Printf("peer kb 聚合: 账号查询失败: %v", e)
			fails++
		}
	}
	if fails == len(clients) {
		return nil, true, "", errors.New("对方 ima 账号查询失败（key 失效或上游故障）")
	}
	seen := map[string]bool{}
	kbs := []map[string]any{}
	for _, items := range outs {
		for _, kb := range items {
			id, _ := kb["id"].(string)
			if seen[id] {
				continue
			}
			seen[id] = true
			kbs = append(kbs, kb)
		}
	}
	isEnd, next := true, ""
	if len(clients) == 1 {
		isEnd, next = ends[0], curs[0]
	}
	return kbs, isEnd, next, nil
}

// kbServeList 知识库列表（browse），跨账号并发聚合去重。
func (s *Server) kbServeList(ctx context.Context, g kbGrant) map[string]any {
	if !g.has("browse") {
		return map[string]any{"error": "未获 browse 授权"}
	}
	kbs, isEnd, next, err := s.kbAggKBs(ctx, g, "browse", "", "", 50)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"kbs": kbs, "is_end": isEnd, "next_cursor": next}
}

// kbServeSearch 检索（search）：指定库→库内检索；未指定→跨库检索（结果按授权过滤）。
func (s *Server) kbServeSearch(ctx context.Context, g kbGrant, kb, query, cursor string, limit uint64) map[string]any {
	if !g.has("search") {
		return map[string]any{"error": "未获 search 授权"}
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	clients := s.peerKBImaClients()
	if len(clients) == 0 {
		return map[string]any{"error": "本节点未配置 ima 账号"}
	}
	if kb != "" {
		if !g.allow("search", kb) {
			return map[string]any{"error": "未授权该知识库"}
		}
		for _, c := range clients {
			rsp, err := c.SearchKnowledge(ctx, kb, query, cursor)
			if err != nil {
				continue
			}
			files := []map[string]any{}
			for _, f := range rsp.InfoList {
				files = append(files, map[string]any{
					"media_id": f.MediaID, "title": f.Title,
					"parent_folder_id": f.ParentFolderID, "highlight_content": f.HighlightContent,
				})
			}
			return map[string]any{"kb": kb, "files": files, "is_end": rsp.IsEnd, "next_cursor": rsp.NextCursor}
		}
		return map[string]any{"error": "检索失败（账号均未命中该知识库）"}
	}
	kbs, isEnd, next, err := s.kbAggKBs(ctx, g, "search", query, cursor, limit)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"kbs": kbs, "is_end": isEnd, "next_cursor": next}
}

// kbServeBrowse 浏览文件列表（browse）。
func (s *Server) kbServeBrowse(ctx context.Context, g kbGrant, kb, folder, cursor string, limit uint64) map[string]any {
	if !g.has("browse") {
		return map[string]any{"error": "未获 browse 授权"}
	}
	if kb == "" {
		return map[string]any{"error": "缺 kb_id"}
	}
	if !g.allow("browse", kb) {
		return map[string]any{"error": "未授权该知识库"}
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	for _, c := range s.peerKBImaClients() {
		rsp, err := c.GetKnowledgeList(ctx, kb, folder, cursor, limit)
		if err != nil {
			continue
		}
		return map[string]any{
			"kb": kb, "folders": rsp.Folders, "files": rsp.Files,
			"is_end": rsp.IsEnd, "next_cursor": rsp.NextCursor,
		}
	}
	return map[string]any{"error": "浏览失败（账号均未命中该知识库）"}
}

// kbDownloadSem 下载并发闸：大文件封载后内存约 3 倍，防并发放大把节点推入 OOM。
var kbDownloadSem = make(chan struct{}, 2)

// kbWalkFind 在知识库目录树内查找 media_id（BFS，页/目录/条目全上限；超限视为未找到，
// 宁拒绝不越权——白名单必须先证明 media 归属授权知识库）。
func kbWalkFind(ctx context.Context, c *ima.Client, kb, mediaID string) bool {
	queue := []string{""}
	visited := map[string]bool{}
	total := 0
	for len(queue) > 0 && len(visited) <= 64 {
		folder := queue[0]
		queue = queue[1:]
		if visited[folder] {
			continue
		}
		visited[folder] = true
		cursor := ""
		for page := 0; page < 20; page++ {
			rsp, err := c.GetKnowledgeList(ctx, kb, folder, cursor, 200)
			if err != nil {
				break
			}
			total += len(rsp.Files) + len(rsp.Folders)
			if total > 5000 {
				return false
			}
			for _, f := range rsp.Files {
				if f.MediaID == mediaID {
					return true
				}
			}
			for _, fo := range rsp.Folders {
				queue = append(queue, fo.FolderID)
			}
			if rsp.IsEnd {
				break
			}
			cursor = rsp.NextCursor
		}
	}
	return false
}

// mediaInKB 校验 media 确实属于授权知识库（防 kb 白名单被他人 media_id 绕过）。
func (s *Server) mediaInKB(ctx context.Context, kb, mediaID string) bool {
	for _, c := range s.peerKBImaClients() {
		if kbWalkFind(ctx, c, kb, mediaID) {
			return true
		}
	}
	return false
}

// kbServeDownload 下载文件内容（download，上限 kbDownloadCap；先证 media 归属再取）。
func (s *Server) kbServeDownload(ctx context.Context, g kbGrant, kb, mediaID, name string) map[string]any {
	if !g.has("download") {
		return map[string]any{"error": "未获 download 授权"}
	}
	if kb == "" || mediaID == "" {
		return map[string]any{"error": "缺 kb_id/media_id"}
	}
	if !g.allow("download", kb) {
		return map[string]any{"error": "未授权该知识库"}
	}
	select {
	case kbDownloadSem <- struct{}{}:
		defer func() { <-kbDownloadSem }()
	default:
		return map[string]any{"error": "对方正在下载中，请稍后重试"}
	}
	if !s.mediaInKB(ctx, kb, mediaID) {
		return map[string]any{"error": "文件不在授权知识库内"}
	}
	for _, c := range s.peerKBImaClients() {
		info, err := c.GetMediaInfo(ctx, mediaID)
		if err != nil || info == nil || info.URLInfo == nil {
			continue
		}
		rc, _, err := c.FetchMedia(ctx, info)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, int64(kbDownloadCap)+1))
		rc.Close()
		if err != nil {
			continue
		}
		if len(data) > kbDownloadCap {
			return map[string]any{"error": "文件超过 32MB 上限"}
		}
		return map[string]any{
			"name": name, "size": len(data),
			"content_b64": base64.StdEncoding.EncodeToString(data),
		}
	}
	return map[string]any{"error": "下载失败（账号均未命中该文件）"}
}

// ---------- 客户端半：经所有者节点查询（同步） ----------

// peerKBQuery 向对方节点发起知识库查询，验证并解密回包（ctx 可随请求取消）。
func (s *Server) peerKBQuery(ctx context.Context, peerID, kind string, args map[string]any) (map[string]any, error) {
	if s.PeerIdentity == nil || s.PeerTable == nil {
		return nil, errors.New("P2P 未启用")
	}
	p, ok := s.PeerTable.Get(peerID)
	if !ok {
		return nil, errors.New("节点不存在")
	}
	addr := peer.PeerAddr(p)
	if addr == "" {
		return nil, errors.New("节点地址未知（等其发现或重新添加）")
	}
	if p.XPub == "" {
		return nil, errors.New("对方缺加密公钥")
	}
	env, err := peer.MakeEnvelope(s.PeerIdentity, p.ID, p.XPub, s.PeerAPIPort, kind, args)
	if err != nil {
		return nil, err
	}
	raw, err := peer.PostEnvelopeResp(ctx, addr, env)
	if err != nil {
		return nil, err
	}
	var re peer.Envelope
	if err := json.Unmarshal(raw, &re); err != nil {
		return nil, errors.New("回包非法")
	}
	if err := peer.VerifyEnvelope(re); err != nil {
		return nil, errors.New("回包验签失败")
	}
	if re.Kind != "kb.reply" || re.From != p.ID || re.To != s.PeerIdentity.NodeID() {
		return nil, errors.New("回包不匹配")
	}
	plain, err := s.PeerIdentity.OpenEnvelope(re)
	if err != nil {
		return nil, errors.New("回包解密失败")
	}
	out := map[string]any{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, errors.New("回包载荷非法")
	}
	return out, nil
}

// writePeerKB 透传查询结果（对方在加密载荷里给的 error 也转业务错误）。
func (s *Server) writePeerKB(w http.ResponseWriter, out map[string]any, err error) {
	if err != nil {
		writeBizErr(w, err.Error())
		return
	}
	if msg, _ := out["error"].(string); msg != "" {
		writeBizErr(w, msg)
		return
	}
	writeOK(w, out)
}

// handlePeerKBList 对方知识库列表。
func (s *Server) handlePeerKBList(w http.ResponseWriter, r *http.Request, u *store.User) {
	out, err := s.peerKBQuery(r.Context(), r.PathValue("id"), "kb.list", map[string]any{
		"cursor": r.URL.Query().Get("cursor"), "limit": 50,
	})
	s.writePeerKB(w, out, err)
}

// handlePeerKBSearch 对方知识库检索（kb 留空=跨库）。
func (s *Server) handlePeerKBSearch(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Q      string `json:"q"`
		KB     string `json:"kb"`
		Cursor string `json:"cursor"`
		Limit  uint64 `json:"limit"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Q) == "" {
		writeBizErr(w, "缺检索词")
		return
	}
	out, err := s.peerKBQuery(r.Context(), r.PathValue("id"), "kb.search", map[string]any{
		"query": req.Q, "kb": req.KB, "cursor": req.Cursor, "limit": req.Limit,
	})
	s.writePeerKB(w, out, err)
}

// handlePeerKBBrowse 浏览对方知识库文件。
func (s *Server) handlePeerKBBrowse(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		KB     string `json:"kb"`
		Folder string `json:"folder"`
		Cursor string `json:"cursor"`
		Limit  uint64 `json:"limit"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.KB == "" {
		writeBizErr(w, "缺 kb")
		return
	}
	out, err := s.peerKBQuery(r.Context(), r.PathValue("id"), "kb.browse", map[string]any{
		"kb_id": req.KB, "folder_id": req.Folder, "cursor": req.Cursor, "limit": req.Limit,
	})
	s.writePeerKB(w, out, err)
}

// handlePeerKBDownload 代理下载对方知识库文件（流式回浏览器）。
func (s *Server) handlePeerKBDownload(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	out, err := s.peerKBQuery(r.Context(), r.PathValue("id"), "kb.download", map[string]any{
		"kb_id": q.Get("kb"), "media_id": q.Get("media"), "name": q.Get("name"),
	})
	if err != nil {
		writeBizErr(w, err.Error())
		return
	}
	if msg, _ := out["error"].(string); msg != "" {
		writeBizErr(w, msg)
		return
	}
	b64, ok := out["content_b64"].(string)
	if !ok || b64 == "" {
		writeBizErr(w, "回包缺少文件内容")
		return
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		writeBizErr(w, "回包内容非法")
		return
	}
	name := kbSafeName(q.Get("name"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// kbSafeName 下载文件名净化（防头注入/路径穿越）。
func kbSafeName(s string) string {
	s = strings.TrimSpace(s)
	s = path.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '"' || r == ';' {
			return -1
		}
		return r
	}, s)
	if s == "" || s == "." || s == ".." {
		s = "download"
	}
	return s
}
