// Package mcp 提供 ImaHub 的 MCP（Model Context Protocol）访问入口。
// 自实现 JSON-RPC 2.0 / Streamable HTTP 子集：initialize、ping、tools/list、tools/call。
package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"imahub/internal/ask"
	"imahub/internal/ima"
	"imahub/internal/store"
)

const (
	protocolVersion = "2025-06-18"
	serverName      = "imahub"
	serverVersion   = "0.1.0"
)

type Server struct {
	Store      *store.Store
	ImaBaseURL string
	TmpDir     string

	rrCounters sync.Map // userID -> *int64，上传自动分配轮询计数
}

// poolAccounts 团队共享账号池全部账号（空时回落服务端默认凭证）。
func (s *Server) poolAccounts(_ *store.User) []store.IMAAccount {
	pool := s.Store.GetTeamAccounts()
	if len(pool) > 0 {
		return pool
	}
	st := s.Store.GetSettings()
	if st.DefaultIMAClientID != "" && st.DefaultIMAAPIKey != "" {
		return []store.IMAAccount{{
			ID: "default", Name: "服务端默认账号",
			ClientID: st.DefaultIMAClientID, APIKey: st.DefaultIMAAPIKey,
		}}
	}
	return nil
}

// clientFor 构造指定账号的 ima 客户端。
func (s *Server) clientFor(a *store.IMAAccount) *ima.Client {
	return ima.New(a.ClientID, a.APIKey, s.ImaBaseURL)
}

// nextRR 上传自动分配轮询序号（按用户计数）。
func (s *Server) nextRR(userID string) int64 {
	v, _ := s.rrCounters.LoadOrStore(userID, new(int64))
	return atomic.AddInt64(v.(*int64), 1) - 1
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodDelete {
		// 无 SSE 长连接；无状态模式。
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	user := s.auth(r)
	if user == nil {
		writeRPCError(w, nil, http.StatusUnauthorized, -32000, "MCP token 无效或缺失（Authorization: Bearer <token>）")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		writeRPCError(w, nil, http.StatusBadRequest, -32700, "读取请求体失败")
		return
	}

	// 批量请求（数组）逐条处理
	trim := strings.TrimSpace(string(body))
	if strings.HasPrefix(trim, "[") {
		var reqs []rpcRequest
		if err := json.Unmarshal(body, &reqs); err != nil {
			writeRPCError(w, nil, http.StatusBadRequest, -32700, "JSON 解析失败")
			return
		}
		resps := make([]rpcResponse, 0, len(reqs))
		for _, req := range reqs {
			if resp, ok := s.dispatch(r.Context(), user, req); ok {
				resps = append(resps, resp)
			}
		}
		writeJSON(w, http.StatusOK, resps)
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, http.StatusBadRequest, -32700, "JSON 解析失败")
		return
	}
	resp, ok := s.dispatch(r.Context(), user, req)
	if !ok {
		// 通知类请求：202 无响应体
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) auth(r *http.Request) *store.User {
	tok := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	if tok == "" {
		return nil
	}
	u, ok := s.Store.FindUserByMCPToken(tok)
	if !ok || u.Disabled {
		return nil
	}
	return u
}

func (s *Server) dispatch(ctx context.Context, u *store.User, req rpcRequest) (rpcResponse, bool) {
	if strings.HasPrefix(req.Method, "notifications/") {
		return rpcResponse{}, false
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		pv := protocolVersion
		if p.ProtocolVersion == "2024-11-05" || p.ProtocolVersion == "2025-03-26" {
			pv = p.ProtocolVersion
		}
		return okResp(req.ID, map[string]any{
			"protocolVersion": pv,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
			"instructions":    "ImaHub：腾讯 ima 知识库/笔记的多用户网关。所有工具操作的是当前 token 对应用户的 ima 账号。",
		}), true
	case "ping":
		return okResp(req.ID, map[string]any{}), true
	case "tools/list":
		return okResp(req.ID, map[string]any{"tools": toolDefs()}), true
	case "tools/call":
		return s.handleToolsCall(ctx, u, req), true
	default:
		return errResp(req.ID, -32601, "方法不存在: "+req.Method), true
	}
}

func okResp(id json.RawMessage, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errResp(id json.RawMessage, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, httpStatus, code int, msg string) {
	writeJSON(w, httpStatus, errResp(id, code, msg))
}

// ---------- 工具定义 ----------

type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func schema(s string) json.RawMessage { return json.RawMessage(s) }

func toolDefs() []toolDef {
	obj := func(props string, required ...string) json.RawMessage {
		// 每个工具统一附加可选 account 参数（多 ima 账号选择）
		if props == "{}" {
			props = `{"account":{"type":"string"}}`
		} else {
			props = props[:len(props)-1] + `,"account":{"type":"string"}}`
		}
		req := "[]"
		if len(required) > 0 {
			reqBytes, _ := json.Marshal(required)
			req = string(reqBytes)
		}
		return schema(`{"type":"object","properties":` + props + `,"required":` + req + `}`)
	}
	str := `{"type":"string"}`
	num := `{"type":"integer"}`
	arr := `{"type":"array","items":{"type":"string"}}`

	return []toolDef{
		{"ima_ask", "知识库智能问答：跨团队知识库检索并用内置模型合成回答，返回答案与引用来源", obj(`{"q":` + str + `,"kb_id":` + str + `,"top_k":` + num + `}`, "q")},
		{"ima_test", "测试 ima 开放平台凭证连通性", obj(`{}`)},
		{"ima_search_knowledge_bases", "搜索或列出 ima 知识库（q 空为默认列表）", obj(`{"q":` + str + `,"cursor":` + str + `,"limit":` + num + `}`)},
		{"ima_list_knowledge", "浏览知识库内容（可传 folder_id 进入文件夹），返回文件与文件夹", obj(`{"knowledge_base_id":`+str+`,"folder_id":`+str+`,"cursor":`+str+`,"limit":`+num+`}`, "knowledge_base_id")},
		{"ima_search_knowledge", "在指定知识库中搜索内容", obj(`{"knowledge_base_id":`+str+`,"q":`+str+`,"cursor":`+str+`}`, "knowledge_base_id", "q")},
		{"ima_get_media_info", "获取知识库条目的原文访问地址或笔记信息", obj(`{"media_id":`+str+`}`, "media_id")},
		{"ima_download_media", "下载/读取知识库条目内容（文本截断返回，二进制返回元信息）", obj(`{"media_id":`+str+`}`, "media_id")},
		{"ima_upload_file", "上传文件到 ima 知识库（file_path 服务端本地路径或 content_base64）", obj(`{"knowledge_base_id":`+str+`,"folder_id":`+str+`,"file_name":`+str+`,"file_path":`+str+`,"content_base64":`+str+`}`, "knowledge_base_id", "file_name")},
		{"ima_import_urls", "把网页/微信公众号文章 URL 导入知识库", obj(`{"knowledge_base_id":`+str+`,"folder_id":`+str+`,"urls":`+arr+`}`, "knowledge_base_id", "urls")},
		{"ima_list_notebooks", "列出 ima 笔记本", obj(`{"cursor":` + str + `,"limit":` + num + `}`)},
		{"ima_list_notes", "列出笔记（可按笔记本过滤）", obj(`{"folder_id":` + str + `,"cursor":` + str + `,"limit":` + num + `}`)},
		{"ima_search_notes", "搜索笔记（search_type: 0 标题 / 1 正文）", obj(`{"q":`+str+`,"search_type":`+num+`,"start":`+num+`,"end":`+num+`}`, "q")},
		{"ima_get_note", "读取笔记纯文本内容", obj(`{"note_id":`+str+`}`, "note_id")},
		{"ima_create_note", "用 Markdown 创建 ima 笔记", obj(`{"content":`+str+`,"folder_id":`+str+`,"folder_name":`+str+`}`, "content")},
		{"ima_append_note", "向已有笔记末尾追加 Markdown", obj(`{"note_id":`+str+`,"content":`+str+`}`, "note_id", "content")},
	}
}

// ---------- 工具调用 ----------

func (s *Server) handleToolsCall(ctx context.Context, u *store.User, req rpcRequest) rpcResponse {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, -32602, "params 无效")
	}
	args := map[string]any{}
	if len(p.Arguments) > 0 {
		_ = json.Unmarshal(p.Arguments, &args)
	}

	accountID, _ := args["account"].(string)
	accountID = strings.TrimSpace(accountID)

	// 大多数工具按单一账号执行；聚合/自动分配工具在 case 内自行解析账号。
	var c *ima.Client
	var err error
	if p.Name != "ima_search_knowledge_bases" && p.Name != "ima_list_notebooks" && p.Name != "ima_upload_file" && p.Name != "ima_ask" {
		c, err = s.effectiveIma(u, accountID)
		if err != nil {
			return toolErr(req.ID, err.Error())
		}
	}

	str := func(k string) string {
		v, _ := args[k].(string)
		return strings.TrimSpace(v)
	}
	num := func(k string) uint64 {
		switch v := args[k].(type) {
		case float64:
			return uint64(v)
		case string:
			n, _ := strconv.ParseUint(v, 10, 64)
			return n
		}
		return 0
	}

	var result any
	switch p.Name {
	case "ima_test":
		rsp, err := c.GetAddableKnowledgeBaseList(ctx, "", 1)
		if err != nil {
			return toolErr(req.ID, err.Error())
		}
		result = map[string]any{"ok": true, "addable_kbs": len(rsp.AddableKBList)}
	case "ima_search_knowledge_bases":
		limit := num("limit")
		if limit == 0 {
			limit = 20
		}
		if accountID == "" {
			// 聚合全部账号（容量池）
			accs := s.poolAccounts(u)
			if len(accs) == 0 {
				return toolErr(req.ID, "当前用户未绑定 IMA 账号，且服务端无默认凭证")
			}
			items := []map[string]any{}
			for i := range accs {
				a := accs[i]
				rsp, e := s.clientFor(&a).SearchKnowledgeBase(ctx, str("q"), "", 50)
				if e != nil {
					items = append(items, map[string]any{"account": a.Name, "account_id": a.ID, "error": e.Error()})
					continue
				}
				for _, it := range rsp.InfoList {
					items = append(items, map[string]any{
						"id": it.ID, "name": it.Name, "cover_url": it.CoverURL,
						"account": a.Name, "account_id": a.ID,
					})
				}
			}
			result = map[string]any{"aggregated": true, "info_list": items}
		} else {
			c, err = s.effectiveIma(u, accountID)
			if err == nil {
				result, err = c.SearchKnowledgeBase(ctx, str("q"), str("cursor"), limit)
			}
		}
	case "ima_list_knowledge":
		limit := num("limit")
		if limit == 0 {
			limit = 50
		}
		result, err = c.GetKnowledgeList(ctx, str("knowledge_base_id"), str("folder_id"), str("cursor"), limit)
	case "ima_search_knowledge":
		result, err = c.SearchKnowledge(ctx, str("knowledge_base_id"), str("q"), str("cursor"))
	case "ima_get_media_info":
		var info *ima.MediaInfo
		info, err = c.GetMediaInfo(ctx, str("media_id"))
		if err == nil {
			out := map[string]any{"media_type": info.MediaType}
			if info.URLInfo != nil {
				out["url_info"] = info.URLInfo
			}
			if info.NotebookExt != nil {
				out["notebook_ext_info"] = info.NotebookExt
			}
			result = out
		}
	case "ima_download_media":
		result, err = s.downloadMedia(ctx, c, str("media_id"))
	case "ima_upload_file":
		if accountID == "" {
			// 自动分配：容量池轮询挑账号，未给知识库时用该账号默认上传知识库
			accs := s.poolAccounts(u)
			if len(accs) == 0 {
				return toolErr(req.ID, "当前用户未绑定 IMA 账号，且服务端无默认凭证")
			}
			a := accs[s.nextRR(u.ID)%int64(len(accs))]
			c = s.clientFor(&a)
			if kb, _ := args["knowledge_base_id"].(string); strings.TrimSpace(kb) == "" && a.UploadKBID != "" {
				args["knowledge_base_id"] = a.UploadKBID
			}
		} else {
			c, err = s.effectiveIma(u, accountID)
			if err != nil {
				return toolErr(req.ID, err.Error())
			}
		}
		result, err = s.uploadFile(ctx, c, args)
	case "ima_import_urls":
		kb := str("knowledge_base_id")
		folder := str("folder_id")
		if folder == "" {
			folder = kb
		}
		urlsAny, _ := args["urls"].([]any)
		urls := make([]string, 0, len(urlsAny))
		for _, x := range urlsAny {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				urls = append(urls, strings.TrimSpace(s))
			}
		}
		if len(urls) == 0 || len(urls) > 10 {
			return toolErr(req.ID, "urls 需要 1-10 个")
		}
		result, err = c.ImportURLs(ctx, kb, folder, urls)
	case "ima_list_notebooks":
		limit := num("limit")
		if limit == 0 {
			limit = 20
		}
		cursor := str("cursor")
		if cursor == "" {
			cursor = "0"
		}
		if accountID == "" {
			// 聚合全部账号（容量池）
			accs := s.poolAccounts(u)
			if len(accs) == 0 {
				return toolErr(req.ID, "当前用户未绑定 IMA 账号，且服务端无默认凭证")
			}
			items := []map[string]any{}
			for i := range accs {
				a := accs[i]
				rsp, e := s.clientFor(&a).ListNotebook(ctx, "0", 50)
				if e != nil {
					items = append(items, map[string]any{"account": a.Name, "account_id": a.ID, "error": e.Error()})
					continue
				}
				for _, f := range rsp.NoteFolderInfos {
					items = append(items, map[string]any{
						"folder_id": f.FolderID, "name": f.Name, "note_number": f.NoteNumber,
						"parent_folder_id": f.ParentFolderID, "folder_type": f.FolderType,
						"account": a.Name, "account_id": a.ID,
					})
				}
			}
			result = map[string]any{"aggregated": true, "note_folder_infos": items}
		} else {
			c, err = s.effectiveIma(u, accountID)
			if err == nil {
				result, err = c.ListNotebook(ctx, cursor, limit)
			}
		}
	case "ima_list_notes":
		limit := num("limit")
		if limit == 0 {
			limit = 20
		}
		result, err = c.ListNote(ctx, str("folder_id"), str("cursor"), limit)
	case "ima_search_notes":
		st := int(num("search_type"))
		start := int64(num("start"))
		end := int64(num("end"))
		if end <= start {
			end = start + 20
		}
		var title, content string
		if st == 1 {
			content = str("q")
		} else {
			title = str("q")
		}
		result, err = c.SearchNote(ctx, title, content, st, 0, start, end)
	case "ima_get_note":
		var content string
		content, err = c.GetNoteContent(ctx, str("note_id"))
		if err == nil {
			result = map[string]any{"content": content}
		}
	case "ima_create_note":
		var noteID string
		noteID, err = c.ImportNote(ctx, str("content"), str("folder_id"), str("folder_name"))
		if err == nil {
			result = map[string]any{"note_id": noteID}
		}
	case "ima_append_note":
		err = c.AppendNote(ctx, str("note_id"), str("content"))
		if err == nil {
			result = map[string]any{"ok": true}
		}
	case "ima_ask":
		question := str("q")
		if question == "" {
			return toolErr(req.ID, "q 不能为空")
		}
		accs := s.poolAccounts(u)
		askAccs := []ask.Account{}
		for i := range accs {
			a := accs[i]
			if accountID != "" && accountID != "default" && a.ID != accountID && a.Name != accountID {
				continue
			}
			askAccs = append(askAccs, ask.Account{ID: a.ID, Name: a.Name, Client: s.clientFor(&a)})
		}
		if len(askAccs) == 0 {
			return toolErr(req.ID, "IMA 账号不存在: "+accountID)
		}
		st := s.Store.GetSettings()
		res, aerr := ask.Run(ctx, askAccs, question, ask.Options{
			KBID: str("kb_id"), TopK: int(num("top_k")),
			BaseURL: st.AskBaseURL, Model: st.AskModel, APIKey: st.AskAPIKey,
		})
		if aerr != nil {
			result = map[string]any{"answer": "", "sources": res.Sources, "mode": res.Mode, "error": aerr.Error()}
		} else {
			result = map[string]any{"answer": res.Answer, "sources": res.Sources, "mode": res.Mode, "model": res.Model}
		}
	default:
		return errResp(req.ID, -32602, "未知工具: "+p.Name)
	}
	if err != nil {
		return toolErr(req.ID, err.Error())
	}
	return toolOK(req.ID, result)
}

func (s *Server) effectiveIma(_ *store.User, accountID string) (*ima.Client, error) {
	pool := s.Store.GetTeamAccounts()
	if len(pool) > 0 {
		if accountID == "" || accountID == "default" {
			a := pool[0]
			return ima.New(a.ClientID, a.APIKey, s.ImaBaseURL), nil
		}
		for _, a := range pool {
			if a.ID == accountID || a.Name == accountID {
				return ima.New(a.ClientID, a.APIKey, s.ImaBaseURL), nil
			}
		}
		return nil, fmt.Errorf("IMA 账号不存在: %s", accountID)
	}
	st := s.Store.GetSettings()
	if st.DefaultIMAClientID != "" && st.DefaultIMAAPIKey != "" {
		if accountID != "" && accountID != "default" {
			return nil, fmt.Errorf("IMA 账号不存在: %s", accountID)
		}
		return ima.New(st.DefaultIMAClientID, st.DefaultIMAAPIKey, s.ImaBaseURL), nil
	}
	return nil, errors.New("团队尚未配置 IMA 账号，且服务端无默认凭证")
}

const downloadTextLimit = 256 << 10

func (s *Server) downloadMedia(ctx context.Context, c *ima.Client, mediaID string) (any, error) {
	if mediaID == "" {
		return nil, errors.New("media_id 不能为空")
	}
	info, err := c.GetMediaInfo(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if info.NotebookExt != nil && info.NotebookExt.NotebookID != "" {
		content, err := c.GetNoteContent(ctx, info.NotebookExt.NotebookID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"media_type": info.MediaType, "kind": "note", "content": content}, nil
	}
	body, hdr, err := c.FetchMedia(ctx, info)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	sniff := make([]byte, 512)
	n, _ := io.ReadFull(body, sniff)
	isText := looksLikeText(sniff[:n])
	rest := io.MultiReader(strings.NewReader(string(sniff[:n])), body)
	if !isText {
		total, _ := io.Copy(io.Discard, rest)
		return map[string]any{
			"media_type":   info.MediaType,
			"kind":         "binary",
			"url":          info.URLInfo.URL,
			"note":         fmt.Sprintf("二进制内容（约 %d 字节），请通过 url 下载", total+int64(n)),
			"content_type": hdr.Get("Content-Type"),
		}, nil
	}
	raw, _ := io.ReadAll(io.LimitReader(rest, downloadTextLimit))
	truncated := false
	var extra [1]byte
	if m, _ := rest.Read(extra[:]); m > 0 {
		truncated = true
	}
	return map[string]any{
		"media_type": info.MediaType,
		"kind":       "text",
		"content":    string(raw),
		"truncated":  truncated,
	}, nil
}

func looksLikeText(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return false
		}
	}
	return true
}

func (s *Server) uploadFile(ctx context.Context, c *ima.Client, args map[string]any) (any, error) {
	str := func(k string) string {
		v, _ := args[k].(string)
		return strings.TrimSpace(v)
	}
	kb := str("knowledge_base_id")
	name := str("file_name")
	if kb == "" || name == "" {
		return nil, errors.New("knowledge_base_id 与 file_name 必填")
	}
	folder := str("folder_id")

	var cleanup func()
	var reader io.Reader
	var size int64

	if p := str("file_path"); p != "" {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("打开 file_path 失败: %w", err)
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		size = st.Size()
		reader = f
		cleanup = func() { f.Close() }
	} else if b64 := str("content_base64"); b64 != "" {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			// 兼容 URL-safe 与带换行
			raw, err = base64.RawStdEncoding.DecodeString(strings.ReplaceAll(b64, "\n", ""))
			if err != nil {
				return nil, errors.New("content_base64 解码失败")
			}
		}
		tmp, err := os.CreateTemp(s.TmpDir, "mcpup-*")
		if err != nil {
			return nil, err
		}
		if _, err := tmp.Write(raw); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return nil, err
		}
		tmp.Seek(0, io.SeekStart)
		size = int64(len(raw))
		reader = tmp
		cleanup = func() { tmp.Close(); os.Remove(tmp.Name()) }
	} else {
		return nil, errors.New("需要 file_path 或 content_base64")
	}
	defer cleanup()

	mediaID, repeated, err := c.UploadFile(ctx, kb, folder, name, size, reader)
	if err != nil {
		return nil, err
	}
	return map[string]any{"media_id": mediaID, "repeated": repeated, "file_name": filepath.Base(name)}, nil
}

func toolOK(id json.RawMessage, result any) rpcResponse {
	payload, _ := json.MarshalIndent(result, "", "  ")
	return okResp(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(payload)}},
		"isError": false,
	})
}

func toolErr(id json.RawMessage, msg string) rpcResponse {
	return okResp(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	})
}
