package api

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"imahub/internal/ima"
	"imahub/internal/store"
)

// ---------- 通用 ----------

// imaClient 按 ia 参数解析 ima 账号并构造客户端。
func (s *Server) imaClient(w http.ResponseWriter, u *store.User, ia string) (*ima.Client, bool) {
	c, err := s.effectiveIma(u, ia)
	if err != nil {
		writeBizErr(w, err.Error())
		return nil, false
	}
	return c, true
}

// ---------- 知识库 ----------

func (s *Server) handleImaTest(w http.ResponseWriter, r *http.Request, u *store.User) {
	c, ok := s.imaClient(w, u, r.URL.Query().Get("ia"))
	if !ok {
		return
	}
	rsp, err := c.GetAddableKnowledgeBaseList(r.Context(), "", 1)
	if err != nil {
		writeOK(w, map[string]any{"ok": false, "note": err.Error()})
		return
	}
	writeOK(w, map[string]any{"ok": true, "note": fmt.Sprintf("连通正常，可添加知识库 %d 个起", len(rsp.AddableKBList))})
}

func (s *Server) handleImaKBs(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	ia := q.Get("ia")
	if ia == "" {
		// 聚合模式：合并容量池全部账号的知识库
		accs := s.poolAccounts(u)
		if len(accs) == 0 {
			writeBizErr(w, "尚未绑定 IMA 账号（设置页可添加，或让管理员配置默认凭证）")
			return
		}
		limit := parseLimit(q.Get("limit"), 20, 50)
		items := []map[string]any{}
		errs := []map[string]any{}
		for i := range accs {
			a := accs[i]
			rsp, err := s.clientFor(&a).SearchKnowledgeBase(r.Context(), q.Get("q"), "", limit)
			if err != nil {
				errs = append(errs, map[string]any{"ia": a.ID, "account_name": a.Name, "error": err.Error()})
				continue
			}
			for _, it := range rsp.InfoList {
				items = append(items, map[string]any{
					"id": it.ID, "name": it.Name, "cover_url": it.CoverURL,
					"ia": a.ID, "account_name": a.Name,
				})
			}
		}
		writeOK(w, map[string]any{"info_list": items, "errors": errs, "is_end": true, "next_cursor": "", "aggregated": true})
		return
	}
	c, ok := s.imaClient(w, u, ia)
	if !ok {
		return
	}
	limit := parseLimit(q.Get("limit"), 20, 20)
	rsp, err := c.SearchKnowledgeBase(r.Context(), q.Get("q"), q.Get("cursor"), limit)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, map[string]any{"info_list": rsp.InfoList, "is_end": rsp.IsEnd, "next_cursor": rsp.NextCursor})
}

func (s *Server) handleImaKB(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	c, ok := s.imaClient(w, u, q.Get("ia"))
	if !ok {
		return
	}
	ids := []string{}
	if v := q.Get("ids"); v != "" {
		ids = strings.Split(v, ",")
	} else if v := q.Get("id"); v != "" {
		ids = []string{v}
	}
	if len(ids) == 0 || len(ids) > 20 {
		writeBizErr(w, "id/ids 参数无效（1-20 个）")
		return
	}
	infos, err := c.GetKnowledgeBase(r.Context(), ids)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, map[string]any{"infos": infos})
}

func (s *Server) handleImaAddableKBs(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	ia := q.Get("ia")
	if ia == "" {
		// 聚合模式
		accs := s.poolAccounts(u)
		if len(accs) == 0 {
			writeBizErr(w, "尚未绑定 IMA 账号（设置页可添加，或让管理员配置默认凭证）")
			return
		}
		limit := parseLimit(q.Get("limit"), 20, 50)
		items := []map[string]any{}
		errs := []map[string]any{}
		for i := range accs {
			a := accs[i]
			rsp, err := s.clientFor(&a).GetAddableKnowledgeBaseList(r.Context(), "", limit)
			if err != nil {
				errs = append(errs, map[string]any{"ia": a.ID, "account_name": a.Name, "error": err.Error()})
				continue
			}
			for _, it := range rsp.AddableKBList {
				items = append(items, map[string]any{
					"id": it.ID, "name": it.Name,
					"ia": a.ID, "account_name": a.Name,
				})
			}
		}
		writeOK(w, map[string]any{"info_list": items, "errors": errs, "is_end": true, "next_cursor": "", "aggregated": true})
		return
	}
	c, ok := s.imaClient(w, u, ia)
	if !ok {
		return
	}
	limit := parseLimit(q.Get("limit"), 20, 50)
	rsp, err := c.GetAddableKnowledgeBaseList(r.Context(), q.Get("cursor"), limit)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"info_list":   rsp.AddableKBList,
		"is_end":      rsp.IsEnd,
		"next_cursor": rsp.NextCursor,
	})
}

// listItem 返回给前端的条目（规整文件夹/文件），title/name 双字段方便前端取用。
type listItem struct {
	MediaID      string `json:"media_id"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	ParentFolder string `json:"parent_folder_id"`
	IsFolder     bool   `json:"is_folder"`
	FolderID     string `json:"folder_id,omitempty"`
	FileNumber   int64  `json:"file_number,omitempty"`
	FolderNumber int64  `json:"folder_number,omitempty"`
}

func (s *Server) handleImaList(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	c, ok := s.imaClient(w, u, q.Get("ia"))
	if !ok {
		return
	}
	kb := q.Get("kb")
	if kb == "" {
		writeBizErr(w, "缺少 kb 参数")
		return
	}
	limit := parseLimit(q.Get("limit"), 50, 50)
	rsp, err := c.GetKnowledgeList(r.Context(), kb, q.Get("folder"), q.Get("cursor"), limit)
	if err != nil {
		writeImaErr(w, err)
		return
	}

	items := make([]listItem, 0, len(rsp.Folders)+len(rsp.Files))
	for _, f := range rsp.Folders {
		items = append(items, listItem{
			Title: f.Name, Name: f.Name, IsFolder: true, FolderID: f.FolderID,
			ParentFolder: f.ParentFolderID,
			FileNumber:   f.FileNumber.Int64(), FolderNumber: f.FolderNumber.Int64(),
		})
	}
	for _, f := range rsp.Files {
		items = append(items, listItem{MediaID: f.MediaID, Title: f.Title, Name: f.Title, ParentFolder: f.ParentFolderID})
	}
	writeOK(w, map[string]any{
		"knowledge_list": items,
		"items":          items,
		"is_end":         rsp.IsEnd,
		"next_cursor":    rsp.NextCursor,
		"current_path":   rsp.CurrentPath,
	})
}

func (s *Server) handleImaSearch(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	c, ok := s.imaClient(w, u, q.Get("ia"))
	if !ok {
		return
	}
	if q.Get("kb") == "" || q.Get("q") == "" {
		writeBizErr(w, "缺少 kb/q 参数")
		return
	}
	rsp, err := c.SearchKnowledge(r.Context(), q.Get("kb"), q.Get("q"), q.Get("cursor"))
	if err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, map[string]any{"info_list": rsp.InfoList, "is_end": rsp.IsEnd, "next_cursor": rsp.NextCursor})
}

func (s *Server) handleImaMedia(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	c, ok := s.imaClient(w, u, q.Get("ia"))
	if !ok {
		return
	}
	mediaID := q.Get("media_id")
	if mediaID == "" {
		writeBizErr(w, "缺少 media_id")
		return
	}
	info, err := c.GetMediaInfo(r.Context(), mediaID)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	out := map[string]any{"media_type": info.MediaType}
	if info.URLInfo != nil {
		out["url_info"] = info.URLInfo
	}
	if info.NotebookExt != nil {
		out["notebook_ext_info"] = info.NotebookExt
		if content, err := c.GetNoteContent(r.Context(), info.NotebookExt.NotebookID); err == nil {
			out["note_content"] = content
		}
	}
	writeOK(w, out)
}

func (s *Server) handleImaDownload(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	c, ok := s.imaClient(w, u, q.Get("ia"))
	if !ok {
		return
	}
	mediaID := q.Get("media_id")
	title := q.Get("title")
	if mediaID == "" {
		writeBizErr(w, "缺少 media_id")
		return
	}
	if title == "" {
		title = mediaID
	}
	info, err := c.GetMediaInfo(r.Context(), mediaID)
	if err != nil {
		writeImaErr(w, err)
		return
	}

	// 笔记类：导出纯文本
	if info.NotebookExt != nil && info.NotebookExt.NotebookID != "" {
		content, err := c.GetNoteContent(r.Context(), info.NotebookExt.NotebookID)
		if err != nil {
			writeImaErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", contentDisposition(title+".txt"))
		_, _ = io.WriteString(w, content)
		return
	}

	body, _, err := c.FetchMedia(r.Context(), info)
	if err != nil {
		writeErr(w, http.StatusBadGateway, 502, err.Error())
		return
	}
	defer body.Close()
	ext := extForMediaType(info.MediaType)
	name := title
	if ext != "" && !strings.HasSuffix(strings.ToLower(name), "."+ext) {
		name += "." + ext
	}
	inline := q.Get("inline") == "1"
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if inline {
		mt := ima.MIMEForFile(name)
		base := strings.ToLower(strings.TrimSpace(strings.SplitN(mt, ";", 2)[0]))
		switch base {
		case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", "image/x-icon", "application/pdf":
			w.Header().Set("Content-Type", mt)
		case "text/plain", "text/markdown":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		default:
			// 防存储型 XSS：HTML/SVG/XML 等一律降级纯文本（预览仍可读）
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "sandbox")
		}
		w.Header().Set("Content-Disposition", "inline")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", contentDisposition(name))
	}
	if _, err := io.Copy(w, body); err != nil {
		// 头已发出，只能记日志（连接中断/上游短读）
		log.Printf("下载代理流式拷贝中断: %v", err)
	}
}

func extForMediaType(t int) string {
	switch t {
	case 1:
		return "pdf"
	case 3:
		return "docx"
	case 4:
		return "pptx"
	case 5:
		return "xlsx"
	case 7:
		return "md"
	case 9:
		return "png"
	case 13:
		return "txt"
	case 14:
		return "xmind"
	case 15:
		return "mp3"
	case 20:
		return "html"
	case 21:
		return "epub"
	}
	return ""
}

func contentDisposition(filename string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": filename})
}

func (s *Server) handleImaCheckNames(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	c, ok := s.imaClient(w, u, q.Get("ia"))
	if !ok {
		return
	}
	if q.Get("kb") == "" || q.Get("name") == "" {
		writeBizErr(w, "缺少 kb/name 参数")
		return
	}
	mt, _ := strconv.Atoi(q.Get("media_type"))
	if mt == 0 {
		mt = ima.DetectMediaType(q.Get("name"))
	}
	names, err := c.CheckRepeatedNames(r.Context(), q.Get("kb"), q.Get("folder"), []string{q.Get("name")}, mt)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, map[string]any{"is_repeated": names[q.Get("name")]})
}

// handleImaUpload multipart：file / kb_id / folder_id / force / ia
func (s *Server) handleImaUpload(w http.ResponseWriter, r *http.Request, u *store.User) {
	// 请求体上限：单包 500MB 上限 + 余量，防无界 multipart 落盘
	const maxUploadBody = 550 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBody)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, http.StatusRequestEntityTooLarge, 413, fmt.Sprintf("上传超过 %dMB 上限", maxUploadBody>>20))
			return
		}
		writeBizErr(w, "multipart 解析失败: "+err.Error())
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	ia := r.FormValue("ia")
	kbID := r.FormValue("kb_id")
	folderID := r.FormValue("folder_id")
	force := r.FormValue("force") == "1" || r.FormValue("force") == "true"

	// 账号解析：ia=auto → 容量池轮询分配；否则按 id/名称指定
	var acc *store.IMAAccount
	if ia == "auto" || ia == "" {
		accs := s.poolAccounts(u)
		if len(accs) == 0 {
			writeBizErr(w, "尚未绑定 IMA 账号（设置页可添加，或让管理员配置默认凭证）")
			return
		}
		start := int(s.nextRR(u.ID) % int64(len(accs)))
		// 轮询时跳过未配置默认上传知识库的账号（手动指定 kb 则任意账号可用）
		var chosen *store.IMAAccount
		for i := 0; i < len(accs); i++ {
			a := accs[(start+i)%len(accs)]
			if kbID != "" || a.UploadKBID != "" {
				chosen = &a
				break
			}
		}
		if chosen == nil {
			writeBizErr(w, "团队账号池均未配置默认上传知识库（设置页配置），或手动指定目标知识库")
			return
		}
		acc = chosen
		if kbID == "" {
			kbID = acc.UploadKBID
		}
	} else {
		resolved, err := s.resolveImaAccount(u, ia)
		if err != nil {
			writeBizErr(w, err.Error())
			return
		}
		acc = resolved
		if kbID == "" {
			writeBizErr(w, "缺少 kb_id")
			return
		}
	}
	c := s.clientFor(acc)
	fh, okF := r.MultipartForm.File["file"]
	if !okF || len(fh) == 0 {
		writeBizErr(w, "缺少 file")
		return
	}
	fileHeader := fh[0]
	name := filepath.Base(fileHeader.Filename)
	// 压缩包（zip/7z/rar）：自动解压并逐个上传内容
	if ima.IsArchive(name) {
		s.uploadArchive(w, r, u, acc, c, kbID, folderID, name, fileHeader, force)
		return
	}
	if ima.DetectMediaType(name) < 0 {
		writeBizErr(w, "不支持的文件类型："+filepath.Ext(name))
		return
	}

	// 同名检查：未强制且重复时 409，由前端确认后带 force 重试
	if !force {
		names, err := c.CheckRepeatedNames(r.Context(), kbID, folderID, []string{name}, ima.DetectMediaType(name))
		if err == nil && names[name] {
			writeErr(w, http.StatusConflict, 409, "知识库中已存在同名文件："+name)
			return
		}
	}

	f, err := fileHeader.Open()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	defer f.Close()

	mediaID, repeated, err := c.UploadFile(r.Context(), kbID, folderID, name, fileHeader.Size, f)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	s.Store.BumpUpload(u.ID, fileHeader.Size)
	s.Store.AddActivity(store.Activity{
		Kind: "upload", Title: name,
		IA: acc.ID, AccountName: acc.Name,
		KBName:  r.FormValue("kb_name"),
		MediaID: mediaID, Size: fileHeader.Size,
		UserID: u.ID, By: u.Username,
	})
	writeOK(w, map[string]any{
		"media_id":     mediaID,
		"repeated":     repeated,
		"ia":           acc.ID,
		"account_name": acc.Name,
		"kb_id":        kbID,
	})
}

func (s *Server) handleImaImportURLs(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		IA       string   `json:"ia"`
		KBID     string   `json:"kb_id"`
		KBName   string   `json:"kb_name"`
		FolderID string   `json:"folder_id"`
		URLs     []string `json:"urls"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	c, ok := s.imaClient(w, u, req.IA)
	if !ok {
		return
	}
	acc, _ := s.resolveImaAccount(u, req.IA)
	if req.KBID == "" {
		writeBizErr(w, "缺少 kb_id")
		return
	}
	clean := make([]string, 0, len(req.URLs))
	for _, raw := range req.URLs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		uu, err := url.Parse(raw)
		if err != nil || (uu.Scheme != "http" && uu.Scheme != "https") || uu.Host == "" {
			writeBizErr(w, "URL 无效（需 http/https 完整地址）："+raw)
			return
		}
		clean = append(clean, raw)
	}
	if len(clean) == 0 || len(clean) > 10 {
		writeBizErr(w, "URL 数量需在 1-10 之间")
		return
	}
	if req.FolderID == "" {
		req.FolderID = req.KBID // 根目录 = 知识库 ID
	}
	results, err := c.ImportURLs(r.Context(), req.KBID, req.FolderID, clean)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	if acc != nil {
		for _, rawURL := range clean {
			if rd, ok := results[rawURL]; ok && rd.MediaID != "" {
				s.Store.AddActivity(store.Activity{
					Kind: "import", Title: rawURL,
					IA: acc.ID, AccountName: acc.Name,
					KBName: req.KBName, MediaID: rd.MediaID,
					UserID: u.ID, By: u.Username,
				})
			}
		}
	}
	writeOK(w, map[string]any{"results": results})
}

// ---------- 笔记 ----------

func (s *Server) handleImaNotebooks(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	ia := q.Get("ia")
	if ia == "" {
		// 聚合模式：合并全部账号的笔记本
		accs := s.poolAccounts(u)
		if len(accs) == 0 {
			writeBizErr(w, "尚未绑定 IMA 账号（设置页可添加，或让管理员配置默认凭证）")
			return
		}
		limit := parseLimit(q.Get("limit"), 20, 50)
		items := []map[string]any{}
		errs := []map[string]any{}
		for i := range accs {
			a := accs[i]
			// 聚合模式逐账号翻页拉全量（上限 10 页/账号），is_end 才能如实为 true
			cursor := "0"
			for page := 0; page < 10; page++ {
				rsp, err := s.clientFor(&a).ListNotebook(r.Context(), cursor, limit)
				if err != nil {
					errs = append(errs, map[string]any{"ia": a.ID, "account_name": a.Name, "error": err.Error()})
					break
				}
				for _, f := range rsp.NoteFolderInfos {
					items = append(items, map[string]any{
						"folder_id": f.FolderID, "name": f.Name,
						"create_time": f.CreateTime, "modify_time": f.ModifyTime,
						"note_number": f.NoteNumber, "parent_folder_id": f.ParentFolderID,
						"folder_type": f.FolderType,
						"ia":          a.ID, "account_name": a.Name,
					})
				}
				if rsp.IsEnd || rsp.NextCursor == "" {
					break
				}
				cursor = rsp.NextCursor
			}
		}
		writeOK(w, map[string]any{"note_folder_infos": items, "errors": errs, "is_end": true, "next_cursor": "", "aggregated": true})
		return
	}
	c, ok := s.imaClient(w, u, ia)
	if !ok {
		return
	}
	cursor := q.Get("cursor")
	if cursor == "" {
		cursor = "0"
	}
	limit := parseLimit(q.Get("limit"), 20, 20)
	rsp, err := c.ListNotebook(r.Context(), cursor, limit)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, map[string]any{"note_folder_infos": rsp.NoteFolderInfos, "is_end": rsp.IsEnd, "next_cursor": rsp.NextCursor})
}

func (s *Server) handleImaNotes(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	c, ok := s.imaClient(w, u, q.Get("ia"))
	if !ok {
		return
	}
	limit := parseLimit(q.Get("limit"), 20, 20)
	rsp, err := c.ListNote(r.Context(), q.Get("folder"), q.Get("cursor"), limit)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, map[string]any{"note_book_list": rsp.NoteBookList, "is_end": rsp.IsEnd, "next_cursor": rsp.NextCursor})
}

func (s *Server) handleImaNotesSearch(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	c, ok := s.imaClient(w, u, q.Get("ia"))
	if !ok {
		return
	}
	if q.Get("q") == "" {
		writeBizErr(w, "缺少 q 参数")
		return
	}
	searchType, _ := strconv.Atoi(q.Get("search_type"))
	start, _ := strconv.ParseInt(q.Get("start"), 10, 64)
	end, _ := strconv.ParseInt(q.Get("end"), 10, 64)
	if end <= start {
		end = start + 20
	}
	if end-start > 20 {
		end = start + 20
	}
	var title, content string
	if searchType == 1 {
		content = q.Get("q")
	} else {
		title = q.Get("q")
	}
	rsp, err := c.SearchNote(r.Context(), title, content, searchType, 0, start, end)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"search_note_infos": rsp.SearchNoteInfos,
		"is_end":            rsp.IsEnd,
		"total_hit_num":     rsp.TotalHit,
	})
}

func (s *Server) handleImaNote(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	c, ok := s.imaClient(w, u, q.Get("ia"))
	if !ok {
		return
	}
	noteID := q.Get("note_id")
	if noteID == "" {
		writeBizErr(w, "缺少 note_id")
		return
	}
	content, err := c.GetNoteContent(r.Context(), noteID)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, map[string]any{"content": content})
}

func (s *Server) handleImaCreateNote(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		IA         string `json:"ia"`
		Content    string `json:"content"`
		FolderID   string `json:"folder_id"`
		FolderName string `json:"folder_name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	c, ok := s.imaClient(w, u, req.IA)
	if !ok {
		return
	}
	acc, _ := s.resolveImaAccount(u, req.IA)
	if strings.TrimSpace(req.Content) == "" {
		writeBizErr(w, "笔记内容不能为空")
		return
	}
	noteID, err := c.ImportNote(r.Context(), req.Content, req.FolderID, req.FolderName)
	if err != nil {
		writeImaErr(w, err)
		return
	}
	if acc != nil {
		title := strings.TrimSpace(req.Content)
		if i := strings.IndexAny(title, "\r\n"); i > 0 {
			title = title[:i]
		}
		title = strings.TrimLeft(title, "# ")
		if len([]rune(title)) > 30 {
			title = string([]rune(title)[:30]) + "…"
		}
		s.Store.AddActivity(store.Activity{
			Kind: "note", Title: title,
			IA: acc.ID, AccountName: acc.Name,
			KBName: req.FolderName, MediaID: noteID,
			UserID: u.ID, By: u.Username,
		})
	}
	writeOK(w, map[string]any{"note_id": noteID})
}

func (s *Server) handleImaAppendNote(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		IA      string `json:"ia"`
		NoteID  string `json:"note_id"`
		Content string `json:"content"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	c, ok := s.imaClient(w, u, req.IA)
	if !ok {
		return
	}
	if req.NoteID == "" || strings.TrimSpace(req.Content) == "" {
		writeBizErr(w, "note_id/content 不能为空")
		return
	}
	if err := c.AppendNote(r.Context(), req.NoteID, req.Content); err != nil {
		writeImaErr(w, err)
		return
	}
	writeOK(w, nil)
}

// ---------- 工具 ----------

func parseLimit(v string, def, max uint64) uint64 {
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || n == 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// ensure 无用引用不报错
var (
	_ = os.ErrNotExist
	_ multipart.File
	_ = errors.New
)
