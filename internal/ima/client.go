// Package ima 自写的腾讯 ima（ima.qq.com）开放平台引擎。
// 接口规范来自官方 ima skill 的 references/api.md（/openapi/wiki/v1、/openapi/note/v1）。
package ima

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://ima.qq.com"

// APIError ima 开放平台业务错误（code != 0）。
type APIError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e *APIError) Error() string { return fmt.Sprintf("IMA:%s (code=%d)", e.Msg, e.Code) }

type Client struct {
	ClientID string
	APIKey   string
	BaseURL  string
	HTTP     *http.Client
}

func New(clientID, apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		ClientID: clientID,
		APIKey:   apiKey,
		BaseURL:  strings.TrimRight(baseURL, "/"),
		HTTP:     &http.Client{Timeout: 5 * time.Minute},
	}
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// call POST 到 ima 开放平台并解包 {code,msg,data}。
func (c *Client) call(ctx context.Context, apiPath string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/"+apiPath, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ima-openapi-clientid", c.ClientID)
	req.Header.Set("ima-openapi-apikey", c.APIKey)
	req.Header.Set("ima-openapi-ctx", "skill_version=imahub-0.1.0")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respRaw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(respRaw, &env); err != nil {
		return fmt.Errorf("ima 响应解析失败(HTTP %d): %s", resp.StatusCode, truncate(string(respRaw), 200))
	}
	if env.Code != 0 {
		return &APIError{Code: env.Code, Msg: env.Msg}
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("ima data 解析失败: %w", err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---------- 数据结构 ----------

// FlexInt 兼容 ima 把数字序列化为字符串的字段（如 file_number:"0"）。
type FlexInt int64

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		str = strings.TrimSpace(str)
		if str == "" {
			*f = 0
			return nil
		}
		n, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			*f = 0 // 无法解析时置 0，不让整个响应失败
			return nil
		}
		*f = FlexInt(n)
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		*f = 0
		return nil
	}
	*f = FlexInt(n)
	return nil
}

// Int64 返回 int64。
func (f FlexInt) Int64() int64 { return int64(f) }

type KnowledgeBaseInfo struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	CoverURL             string   `json:"cover_url"`
	Description          string   `json:"description"`
	RecommendedQuestions []string `json:"recommended_questions"`
}

type KnowledgeInfo struct {
	MediaID        string `json:"media_id"`
	Title          string `json:"title"`
	ParentFolderID string `json:"parent_folder_id"`
}

type FolderInfo struct {
	FolderID       string   `json:"folder_id"`
	Name           string   `json:"name"`
	FileNumber     FlexInt  `json:"file_number"`
	FolderNumber   FlexInt  `json:"folder_number"`
	ParentFolderID string   `json:"parent_folder_id"`
	IsTop          bool     `json:"is_top"`
}

type SearchedKB struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CoverURL string `json:"cover_url"`
	// 真实 API 使用 kb_id/kb_name（文档写 id/name），两者都兼容
	AltKBID   string `json:"kb_id,omitempty"`
	AltKBName string `json:"kb_name,omitempty"`
}

func (s *SearchedKB) UnmarshalJSON(b []byte) error {
	type plain SearchedKB
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*s = SearchedKB(p)
	if s.ID == "" {
		s.ID = s.AltKBID
	}
	if s.Name == "" {
		s.Name = s.AltKBName
	}
	return nil
}

type SearchedKnowledge struct {
	MediaID          string `json:"media_id"`
	Title            string `json:"title"`
	ParentFolderID   string `json:"parent_folder_id"`
	HighlightContent string `json:"highlight_content"`
}

type URLInfo struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type NotebookExtInfo struct {
	NotebookID string `json:"notebook_id"`
}

type MediaInfo struct {
	MediaType   int              `json:"media_type"`
	URLInfo     *URLInfo         `json:"url_info"`
	NotebookExt *NotebookExtInfo `json:"notebook_ext_info"`
}

type Credential struct {
	Token        string `json:"token"`
	SecretID     string `json:"secret_id"`
	SecretKey    string `json:"secret_key"`
	StartTime    FlexInt `json:"start_time"`
	ExpiredTime  FlexInt `json:"expired_time"`
	AppID        string `json:"appid"`
	BucketName   string `json:"bucket_name"`
	Region       string `json:"region"`
	CustomDomain string `json:"custom_domain"`
	CosKey       string `json:"cos_key"`
}

type FileInfo struct {
	CosKey         string `json:"cos_key"`
	FileSize       uint64 `json:"file_size"`
	LastModifyTime int64  `json:"last_modify_time"`
	Password       string `json:"password"`
	FileName       string `json:"file_name"`
}

// ---------- 知识库 wiki API ----------

// KnowledgeListRsp 混排结果：knowledge_list 里文件与文件夹混在一起，
// 按字段形状拆分（带 folder_id 的是文件夹，带 media_id 的是文件）。
type KnowledgeListRsp struct {
	Folders     []FolderInfo    `json:"folders"`
	Files       []KnowledgeInfo `json:"files"`
	IsEnd       bool            `json:"is_end"`
	NextCursor  string          `json:"next_cursor"`
	CurrentPath []FolderInfo    `json:"current_path"`
}

func (k *KnowledgeListRsp) UnmarshalJSON(b []byte) error {
	var raw struct {
		KnowledgeList []json.RawMessage `json:"knowledge_list"`
		IsEnd         bool              `json:"is_end"`
		NextCursor    string            `json:"next_cursor"`
		CurrentPath   []FolderInfo      `json:"current_path"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	k.IsEnd = raw.IsEnd
	k.NextCursor = raw.NextCursor
	k.CurrentPath = raw.CurrentPath
	k.Folders = nil
	k.Files = nil
	for _, item := range raw.KnowledgeList {
		var probe struct {
			MediaID  string `json:"media_id"`
			FolderID string `json:"folder_id"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			continue
		}
		if probe.FolderID != "" && probe.MediaID == "" {
			var f FolderInfo
			if err := json.Unmarshal(item, &f); err == nil {
				k.Folders = append(k.Folders, f)
			}
		} else {
			var f KnowledgeInfo
			if err := json.Unmarshal(item, &f); err == nil {
				k.Files = append(k.Files, f)
			}
		}
	}
	return nil
}

func (c *Client) GetKnowledgeList(ctx context.Context, kbID, folderID, cursor string, limit uint64) (*KnowledgeListRsp, error) {
	body := map[string]any{"cursor": cursor, "limit": limit, "knowledge_base_id": kbID}
	if folderID != "" {
		body["folder_id"] = folderID
	}
	var rsp KnowledgeListRsp
	if err := c.call(ctx, "openapi/wiki/v1/get_knowledge_list", body, &rsp); err != nil {
		return nil, err
	}
	return &rsp, nil
}

type SearchKnowledgeRsp struct {
	InfoList   []SearchedKnowledge `json:"info_list"`
	IsEnd      bool                `json:"is_end"`
	NextCursor string              `json:"next_cursor"`
}

func (c *Client) SearchKnowledge(ctx context.Context, kbID, query, cursor string) (*SearchKnowledgeRsp, error) {
	var rsp SearchKnowledgeRsp
	err := c.call(ctx, "openapi/wiki/v1/search_knowledge",
		map[string]any{"query": query, "cursor": cursor, "knowledge_base_id": kbID}, &rsp)
	if err != nil {
		return nil, err
	}
	return &rsp, nil
}

type SearchKBRsp struct {
	InfoList   []SearchedKB `json:"info_list"`
	IsEnd      bool         `json:"is_end"`
	NextCursor string       `json:"next_cursor"`
}

func (c *Client) SearchKnowledgeBase(ctx context.Context, query, cursor string, limit uint64) (*SearchKBRsp, error) {
	var rsp SearchKBRsp
	err := c.call(ctx, "openapi/wiki/v1/search_knowledge_base",
		map[string]any{"query": query, "cursor": cursor, "limit": limit}, &rsp)
	if err != nil {
		return nil, err
	}
	return &rsp, nil
}

type AddableKBRsp struct {
	AddableKBList []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"addable_knowledge_base_list"`
	NextCursor string `json:"next_cursor"`
	IsEnd      bool   `json:"is_end"`
}

func (c *Client) GetAddableKnowledgeBaseList(ctx context.Context, cursor string, limit uint64) (*AddableKBRsp, error) {
	var rsp AddableKBRsp
	err := c.call(ctx, "openapi/wiki/v1/get_addable_knowledge_base_list",
		map[string]any{"cursor": cursor, "limit": limit}, &rsp)
	if err != nil {
		return nil, err
	}
	return &rsp, nil
}

func (c *Client) GetKnowledgeBase(ctx context.Context, ids []string) (map[string]KnowledgeBaseInfo, error) {
	var rsp struct {
		Infos map[string]KnowledgeBaseInfo `json:"infos"`
	}
	err := c.call(ctx, "openapi/wiki/v1/get_knowledge_base", map[string]any{"ids": ids}, &rsp)
	if err != nil {
		return nil, err
	}
	return rsp.Infos, nil
}

func (c *Client) CheckRepeatedNames(ctx context.Context, kbID, folderID string, names []string, mediaType int) (map[string]bool, error) {
	params := make([]map[string]any, 0, len(names))
	for _, n := range names {
		params = append(params, map[string]any{"name": n, "media_type": mediaType})
	}
	body := map[string]any{"params": params, "knowledge_base_id": kbID}
	if folderID != "" {
		body["folder_id"] = folderID
	}
	var rsp struct {
		Results []struct {
			Name       string `json:"name"`
			IsRepeated bool   `json:"is_repeated"`
		} `json:"results"`
	}
	if err := c.call(ctx, "openapi/wiki/v1/check_repeated_names", body, &rsp); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, r := range rsp.Results {
		out[r.Name] = r.IsRepeated
	}
	return out, nil
}

type ImportURLData struct {
	URL     string `json:"url"`
	RetCode int    `json:"ret_code"`
	MediaID string `json:"media_id"`
}

func (c *Client) ImportURLs(ctx context.Context, kbID, folderID string, urls []string) (map[string]ImportURLData, error) {
	var rsp struct {
		Results map[string]ImportURLData `json:"results"`
	}
	err := c.call(ctx, "openapi/wiki/v1/import_urls",
		map[string]any{"knowledge_base_id": kbID, "folder_id": folderID, "urls": urls}, &rsp)
	if err != nil {
		return nil, err
	}
	return rsp.Results, nil
}

func (c *Client) GetMediaInfo(ctx context.Context, mediaID string) (*MediaInfo, error) {
	var rsp MediaInfo
	if err := c.call(ctx, "openapi/wiki/v1/get_media_info", map[string]any{"media_id": mediaID}, &rsp); err != nil {
		return nil, err
	}
	return &rsp, nil
}

// ---------- 上传流程 ----------

type createMediaRsp struct {
	MediaID       string     `json:"media_id"`
	CosCredential Credential `json:"cos_credential"`
}

// DetectMediaType 按扩展名判定 media_type；不支持返回 -1。
func DetectMediaType(filename string) int {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	switch ext {
	case "pdf":
		return 1
	case "doc", "docx":
		return 3
	case "ppt", "pptx":
		return 4
	case "xls", "xlsx", "csv":
		return 5
	case "md", "markdown":
		return 7
	case "png", "jpg", "jpeg", "webp":
		return 9
	case "txt":
		return 13
	case "xmind":
		return 14
	case "mp3", "m4a", "wav", "aac":
		return 15
	case "html", "htm":
		return 20
	case "epub":
		return 21
	}
	return -1
}

// MaxUploadSize 各类型大小上限（字节），来自官方约束。
func MaxUploadSize(mediaType int) int64 {
	const (
		MB = 1 << 20
	)
	switch mediaType {
	case 5, 13, 14, 7, 20:
		return 10 * MB
	case 9:
		return 30 * MB
	case 21:
		return 50 * MB
	default:
		return 200 * MB
	}
}

// MIMEForFile 尽量给出准确 content_type。
func MIMEForFile(filename string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	special := map[string]string{
		"md": "text/markdown", "markdown": "text/markdown",
		"xmind": "application/vnd.xmind.workbook",
		"m4a":   "audio/x-m4a",
	}
	if ct, ok := special[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension("." + ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// UploadFile 完整上传流程：查重 → create_media → COS PUT → add_knowledge。
// 返回 media_id 与是否命中同名。
func (c *Client) UploadFile(ctx context.Context, kbID, folderID, filename string, size int64, r io.Reader) (string, bool, error) {
	mediaType := DetectMediaType(filename)
	if mediaType < 0 {
		return "", false, errors.New("不支持的文件类型：" + filepath.Ext(filename))
	}
	if size > MaxUploadSize(mediaType) {
		return "", false, fmt.Errorf("文件超过大小上限（%dMB）", MaxUploadSize(mediaType)>>20)
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	contentType := MIMEForFile(filename)

	repeated := false
	if names, err := c.CheckRepeatedNames(ctx, kbID, folderID, []string{filename}, mediaType); err == nil {
		repeated = names[filename]
	}

	var cm createMediaRsp
	if err := c.call(ctx, "openapi/wiki/v1/create_media", map[string]any{
		"file_name":         filename,
		"file_size":         size,
		"content_type":      contentType,
		"knowledge_base_id": kbID,
		"file_ext":          ext,
	}, &cm); err != nil {
		return "", repeated, err
	}

	if err := cosUpload(ctx, cm.CosCredential, contentType, r, size); err != nil {
		return "", repeated, fmt.Errorf("COS 上传失败: %w", err)
	}

	addBody := map[string]any{
		"media_type":        mediaType,
		"media_id":          cm.MediaID,
		"title":             filename,
		"knowledge_base_id": kbID,
		"file_info": FileInfo{
			CosKey:         cm.CosCredential.CosKey,
			FileSize:       uint64(size),
			LastModifyTime: time.Now().Unix(),
			FileName:       filename,
		},
	}
	if folderID != "" {
		addBody["folder_id"] = folderID
	}
	var addRsp struct {
		MediaID string `json:"media_id"`
	}
	if err := c.call(ctx, "openapi/wiki/v1/add_knowledge", addBody, &addRsp); err != nil {
		return "", repeated, err
	}
	if addRsp.MediaID != "" {
		return addRsp.MediaID, repeated, nil
	}
	return cm.MediaID, repeated, nil
}

// ---------- 笔记 note API ----------

type NoteInfo struct {
	NoteID     string `json:"note_id"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	CreateTime FlexInt `json:"create_time"`
	ModifyTime FlexInt `json:"modify_time"`
	CoverImage string `json:"cover_image"`
	NoteExt    *struct {
		FolderID   string `json:"folder_id"`
		FolderName string `json:"folder_name"`
	} `json:"note_ext_info"`
}

type NoteFolderInfo struct {
	FolderID       string `json:"folder_id"`
	Name           string `json:"name"`
	CreateTime     int64  `json:"create_time"`
	ModifyTime     int64  `json:"modify_time"`
	NoteNumber     FlexInt `json:"note_number"`
	ParentFolderID string `json:"parent_folder_id"`
	FolderType     FlexInt `json:"folder_type"`
}

type SearchNoteRsp struct {
	SearchNoteInfos []struct {
		NoteBookInfo NoteInfo          `json:"note_book_info"`
		Highlight    map[string]string `json:"highlightInfo"`
	} `json:"search_note_infos"`
	IsEnd    bool  `json:"is_end"`
	TotalHit FlexInt `json:"total_hit_num"`
}

func (c *Client) SearchNote(ctx context.Context, title, content string, searchType, sortType int, start, end int64) (*SearchNoteRsp, error) {
	qi := map[string]string{}
	if title != "" {
		qi["title"] = title
	}
	if content != "" {
		qi["content"] = content
	}
	body := map[string]any{
		"search_type": searchType,
		"sort_type":   sortType,
		"query_info":  qi,
		"start":       start,
		"end":         end,
	}
	var rsp SearchNoteRsp
	if err := c.call(ctx, "openapi/note/v1/search_note", body, &rsp); err != nil {
		return nil, err
	}
	return &rsp, nil
}

type ListNoteRsp struct {
	NoteBookList []NoteInfo `json:"note_book_list"`
	IsEnd        bool       `json:"is_end"`
}

func (c *Client) ListNote(ctx context.Context, folderID, cursor string, limit uint64) (*ListNoteRsp, error) {
	body := map[string]any{"cursor": cursor, "limit": limit}
	if folderID != "" {
		body["folder_id"] = folderID
	}
	var rsp ListNoteRsp
	if err := c.call(ctx, "openapi/note/v1/list_note", body, &rsp); err != nil {
		return nil, err
	}
	return &rsp, nil
}

func (c *Client) GetNoteContent(ctx context.Context, noteID string) (string, error) {
	var rsp struct {
		Content string `json:"content"`
	}
	err := c.call(ctx, "openapi/note/v1/get_doc_content",
		map[string]any{"note_id": noteID, "target_content_format": 0}, &rsp)
	if err != nil {
		return "", err
	}
	return rsp.Content, nil
}

func (c *Client) ImportNote(ctx context.Context, content, folderID, folderName string) (string, error) {
	body := map[string]any{"content_format": 1, "content": content}
	if folderID != "" {
		body["folder_id"] = folderID
	}
	if folderName != "" {
		body["folder_name"] = folderName
	}
	var rsp struct {
		NoteID string `json:"note_id"`
	}
	if err := c.call(ctx, "openapi/note/v1/import_doc", body, &rsp); err != nil {
		return "", err
	}
	return rsp.NoteID, nil
}

func (c *Client) AppendNote(ctx context.Context, noteID, content string) error {
	return c.call(ctx, "openapi/note/v1/append_doc",
		map[string]any{"note_id": noteID, "content_format": 1, "content": content}, nil)
}

type ListNotebookRsp struct {
	NoteFolderInfos []NoteFolderInfo `json:"note_folder_infos"`
	NextCursor      string           `json:"next_cursor"`
	IsEnd           bool             `json:"is_end"`
}

func (c *Client) ListNotebook(ctx context.Context, cursor string, limit uint64) (*ListNotebookRsp, error) {
	if cursor == "" {
		cursor = "0"
	}
	var rsp ListNotebookRsp
	if err := c.call(ctx, "openapi/note/v1/list_notebook",
		map[string]any{"cursor": cursor, "limit": limit}, &rsp); err != nil {
		return nil, err
	}
	return &rsp, nil
}

// FetchMedia 拉取媒体原文内容（url_info 场景），返回 body reader 与响应头。
func (c *Client) FetchMedia(ctx context.Context, info *MediaInfo) (io.ReadCloser, http.Header, error) {
	if info == nil || info.URLInfo == nil || info.URLInfo.URL == "" {
		return nil, nil, errors.New("该条目无可访问链接")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.URLInfo.URL, nil)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range info.URLInfo.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("原文链接访问失败(HTTP %d)", resp.StatusCode)
	}
	return resp.Body, resp.Header, nil
}

// WriteTemp 供 MCP 上传使用：把 reader 落临时文件并返回大小。
func WriteTemp(dir, pattern string, r io.Reader) (string, int64, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	n, err := io.Copy(f, r)
	if err != nil {
		os.Remove(f.Name())
		return "", 0, err
	}
	return f.Name(), n, nil
}
