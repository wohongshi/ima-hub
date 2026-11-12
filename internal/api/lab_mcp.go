package api

// lab_mcp.go — 教研工作台 MCP 端点（POST /lab/mcp，streamable-HTTP 简单模式）。
// 供 WorkBuddy 等 MCP 客户端直连：无鉴权（与 /lab 页面同为公开收集面），
// 上传走与网页同款的限速/限大/文件名净化。只实现 initialize / tools/list / tools/call / ping。

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"imahub/internal/store"
)

// mcpReq JSON-RPC 2.0 请求（MCP 子集）。
type mcpReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// mcpResp JSON-RPC 2.0 响应。
type mcpResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const labMCPVersion = "2025-06-18"

// handleLabMCP POST /lab/mcp
func (s *Server) handleLabMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req mcpReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCP(w, mcpResp{JSONRPC: "2.0", Error: &mcpError{Code: -32700, Message: "解析失败"}})
		return
	}
	// 通知（无 id）不回内容
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		writeMCP(w, mcpResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": labMCPVersion,
			"serverInfo":      map[string]any{"name": "jiaoyan-lab", "version": "1.0.0"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}})
	case "ping":
		writeMCP(w, mcpResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		writeMCP(w, mcpResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": labMCPTools()}})
	case "tools/call":
		s.labMCPToolCall(w, r, req)
	default:
		writeMCP(w, mcpResp{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{Code: -32601, Message: "不支持的方法: " + req.Method}})
	}
}

func writeMCP(w http.ResponseWriter, resp mcpResp) {
	_ = json.NewEncoder(w).Encode(resp)
}

// mcpToolText 成功结果：文本内容块。
func mcpToolText(id json.RawMessage, text string) mcpResp {
	return mcpResp{JSONRPC: "2.0", ID: id, Result: map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}}
}

func labMCPTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "lab_info",
			"description": "获取教研工作台信息：标题、提示语、资源链接、上传限制。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "lab_files",
			"description": "列出教研工作台已收集的资料（文件名/大小/上传者/时间/ID），最新在前。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "lab_upload",
			"description": "上传一份资料到教研工作台（文件内容用 base64，单个 ≤ 10MB）。上传后同事即可在网页下载。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"filename":       map[string]any{"type": "string", "description": "文件名（含扩展名，如 期中试卷分析.docx）"},
					"content_base64": map[string]any{"type": "string", "description": "文件内容的 base64 编码"},
					"by":             map[string]any{"type": "string", "description": "上传者署名（选填，≤24 字）"},
				},
				"required": []string{"filename", "content_base64"},
			},
		},
		{
			"name":        "lab_download",
			"description": "按 ID 下载一份资料（返回 base64 内容与文件名）。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "资料 ID（来自 lab_files）"},
				},
				"required": []string{"id"},
			},
		},
	}
}

func (s *Server) labMCPToolCall(w http.ResponseWriter, r *http.Request, req mcpReq) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeMCP(w, mcpResp{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{Code: -32602, Message: "参数解析失败"}})
		return
	}
	switch p.Name {
	case "lab_info":
		st := s.Store.GetSettings()
		title := st.LabTitle
		if title == "" {
			title = "教研工作台"
		}
		b, _ := json.Marshal(map[string]any{
			"enabled": st.LabEnabled, "title": title, "notice": st.LabNotice,
			"links": st.LabLinks, "max_mb": labMaxBytes >> 20,
		})
		writeMCP(w, mcpToolText(req.ID, string(b)))
	case "lab_files":
		b, _ := json.Marshal(map[string]any{"files": s.Store.LabList()})
		writeMCP(w, mcpToolText(req.ID, string(b)))
	case "lab_upload":
		if !s.Store.GetSettings().LabEnabled {
			writeMCP(w, mcpToolText(req.ID, "错误：工作台未启用"))
			return
		}
		ip := clientIP(r)
		if labLimiter.blocked(ip) {
			writeMCP(w, mcpToolText(req.ID, "错误：上传太频繁，歇一分钟再来"))
			return
		}
		var a struct {
			Filename      string `json:"filename"`
			ContentBase64 string `json:"content_base64"`
			By            string `json:"by"`
		}
		if err := json.Unmarshal(p.Arguments, &a); err != nil {
			writeMCP(w, mcpToolText(req.ID, "错误：参数解析失败"))
			return
		}
		raw, err := base64.StdEncoding.DecodeString(a.ContentBase64)
		if err != nil {
			writeMCP(w, mcpToolText(req.ID, "错误：content_base64 不是合法 base64"))
			return
		}
		if len(raw) > 10<<20 {
			writeMCP(w, mcpToolText(req.ID, "错误：MCP 通道单个文件 ≤ 10MB，大文件请用网页上传"))
			return
		}
		labLimiter.hit(ip)
		id, name, size, err := s.labSave(kbSafeName(strings.TrimSpace(a.Filename)), strings.TrimSpace(a.By), raw)
		if err != nil {
			writeMCP(w, mcpToolText(req.ID, "错误：保存失败，请重试"))
			return
		}
		b, _ := json.Marshal(map[string]any{"id": id, "name": name, "size": size})
		writeMCP(w, mcpToolText(req.ID, string(b)))
	case "lab_download":
		var a struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(p.Arguments, &a); err != nil {
			writeMCP(w, mcpToolText(req.ID, "错误：参数解析失败"))
			return
		}
		meta, raw, err := s.labRead(strings.TrimSpace(a.ID))
		if err != nil {
			writeMCP(w, mcpToolText(req.ID, "错误：文件不存在"))
			return
		}
		b, _ := json.Marshal(map[string]any{
			"id": meta.ID, "name": meta.Name, "size": meta.Size,
			"content_base64": base64.StdEncoding.EncodeToString(raw),
		})
		writeMCP(w, mcpToolText(req.ID, string(b)))
	default:
		writeMCP(w, mcpResp{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{Code: -32602, Message: "未知工具: " + p.Name}})
	}
}

// labSave 落盘一份资料（供 REST 与 MCP 共用；返回 id/名/大小）。
func (s *Server) labSave(name, by string, raw []byte) (string, string, int64, error) {
	if name == "" {
		name = "未命名文件"
	}
	if by == "" {
		by = "匿名"
	}
	if len([]rune(by)) > 24 {
		by = string([]rune(by)[:24])
	}
	id := store.RandomToken()[:12]
	if err := writeLabBlob(s.labDir(), id, raw); err != nil {
		return "", "", 0, err
	}
	if err := s.Store.LabAdd(store.LabFile{ID: id, Name: name, Size: int64(len(raw)), By: by}); err != nil {
		return "", "", 0, err
	}
	return id, name, int64(len(raw)), nil
}

// labRead 按 ID 读资料（元数据 + 文件体）。
func (s *Server) labRead(id string) (store.LabFile, []byte, error) {
	if id == "" || strings.ContainsAny(id, "/\\.") {
		return store.LabFile{}, nil, store.ErrNotFound
	}
	for _, f := range s.Store.LabList() {
		if f.ID == id {
			raw, err := readLabBlob(s.labDir(), id)
			return f, raw, err
		}
	}
	return store.LabFile{}, nil, store.ErrNotFound
}
