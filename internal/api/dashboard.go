package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"imahub/internal/scan"
	"imahub/internal/store"
)

// normDue 任务截止时间：接受日期字符串（"2026-11-07"）、Unix 秒/毫秒数字，"" 或 null 清空。
func normDue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x // "" 即清空
	case float64:
		return x
	default:
		return ""
	}
}

// ---------- 首页仪表盘 ----------

type recentItem struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	IA          string `json:"ia"`
	AccountName string `json:"account_name"`
	KBName      string `json:"kb_name"`
	MediaID     string `json:"media_id,omitempty"`
	Size        int64  `json:"size,omitempty"`
	At          int64  `json:"at"`
	By          string `json:"by,omitempty"` // 操作人（团队共享展示）
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request, u *store.User) {
	if s.Scanner == nil {
		writeErr(w, http.StatusInternalServerError, 500, "扫描器未初始化")
		return
	}
	if s.Scanner.Stale(30 * time.Minute) {
		s.Scanner.Start() // 异步补扫
	}
	snap := s.Scanner.Snapshot()
	st := s.Scanner.Status()

	accs := s.poolAccounts(u)

	// 扫描统计索引（按账号；全团队互通）
	idx := map[string]scan.AccountStat{}
	for _, a := range snap.Accounts {
		idx[a.ID] = a
	}

	// 活动日志聚合：每账号上传数/字节（团队共享）
	acts := s.Store.ListActivities(500)
	upCount := map[string]int{}
	upBytes := map[string]int64{}
	for _, a := range acts {
		if a.Kind == "upload" {
			upCount[a.IA]++
			upBytes[a.IA] += a.Size
		}
	}

	typeCount := map[string]int{}
	totalFiles := 0
	byAccount := make([]map[string]any, 0, len(accs))
	for _, a := range accs {
		stat := idx[a.ID]
		for t, c := range stat.Types {
			typeCount[t] += c
		}
		totalFiles += stat.Files
		byAccount = append(byAccount, map[string]any{
			"ia": a.ID, "name": a.Name,
			"kbs": stat.KBs, "files": stat.Files, "folders": stat.Folders,
			"uploads": upCount[a.ID], "bytes": upBytes[a.ID],
			"share": 0.0, "error": stat.Error,
		})
	}
	for i := range byAccount {
		if f, ok := byAccount[i]["files"].(int); ok && totalFiles > 0 {
			byAccount[i]["share"] = float64(f) / float64(totalFiles)
		}
	}

	// 文件类型分布
	byType := []map[string]any{}
	for t, c := range typeCount {
		pct := 0.0
		if totalFiles > 0 {
			pct = float64(c) / float64(totalFiles)
		}
		byType = append(byType, map[string]any{"type": t, "label": scan.TypeLabel(t), "count": c, "pct": pct})
	}
	sort.Slice(byType, func(i, j int) bool {
		return byType[i]["count"].(int) > byType[j]["count"].(int)
	})

	// 近期添加：活动日志 + 扫描发现的新文件（团队共享，全员可见）
	recent := []recentItem{}
	for _, a := range acts {
		if len(recent) >= 20 {
			break
		}
		recent = append(recent, recentItem{
			Kind: a.Kind, Title: a.Title, IA: a.IA, AccountName: a.AccountName,
			KBName: a.KBName, MediaID: a.MediaID, Size: a.Size, At: a.At, By: a.By,
		})
	}
	for _, nf := range snap.NewFiles {
		recent = append(recent, recentItem{
			Kind: "scan_new", Title: nf.Title, IA: nf.AccountID, AccountName: nf.AccountName,
			KBName: nf.KBName, MediaID: nf.MediaID, At: nf.FirstSeen,
		})
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].At > recent[j].At })
	if len(recent) > 30 {
		recent = recent[:30]
	}

	// 团队累计上传（全员总和）
	var teamUploads, teamBytes int64
	for _, tu := range s.Store.ListUsers() {
		teamUploads += tu.Uploads
		teamBytes += tu.Bytes
	}
	stats := map[string]any{
		"accounts": len(accs),
		"kbs":      snapCount(idx, "kbs", accs),
		"files":    totalFiles,
		"folders":  snapCount(idx, "folders", accs),
		"uploads":  teamUploads,
		"bytes":    teamBytes,
	}

	scanMap := map[string]any{
		"status":       st.Status,
		"last_scan_at": st.LastScanAt,
		"progress":     map[string]any{"done": st.Done, "total": st.Total},
		// 平铺兼容
		"done":  st.Done,
		"total": st.Total,
	}
	if st.Error != "" {
		scanMap["error"] = st.Error
	}
	writeOK(w, map[string]any{
		"stats":      stats,
		"by_type":    byType,
		"by_account": byAccount,
		"recent":     recent,
		"scan":       scanMap,
	})
}

func snapCount(idx map[string]scan.AccountStat, field string, accs []store.IMAAccount) int {
	total := 0
	for _, a := range accs {
		st := idx[a.ID]
		switch field {
		case "kbs":
			total += st.KBs
		case "folders":
			total += st.Folders
		}
	}
	return total
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request, u *store.User) {
	started := s.Scanner.Start()
	writeOK(w, map[string]any{"status": s.Scanner.Status().Status, "started": started})
}

// ---------- 任务计划 ----------

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeOK(w, map[string]any{"tasks": s.Store.ListTasks()})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Title   string          `json:"title"`
		Note    string          `json:"note"`
		Status  string          `json:"status"`
		Due     json.RawMessage `json:"due"`
		GroupID string          `json:"group_id"` // 群组共享（空=本地/团队）
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Title == "" {
		writeBizErr(w, "任务标题不能为空")
		return
	}
	if req.Status == "" {
		req.Status = "todo"
	}
	if req.Status != "todo" && req.Status != "doing" && req.Status != "done" {
		writeBizErr(w, "status 无效")
		return
	}
	t, err := s.Store.AddTask(store.Task{
		UserID: u.ID, By: u.Username, Title: req.Title, Note: req.Note,
		Status: req.Status, Due: normDue(req.Due), GroupID: req.GroupID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	s.hookTaskChanged("upsert", t) // 群组条目广播（离线走 outbox 补投）
	writeOK(w, map[string]any{"task": t})
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	var req struct {
		Title  *string          `json:"title"`
		Note   *string          `json:"note"`
		Status *string          `json:"status"`
		Due    *json.RawMessage `json:"due"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	err := s.Store.UpdateTask(id, func(t *store.Task) error {
		if req.Title != nil {
			if *req.Title == "" {
				return errors.New("任务标题不能为空")
			}
			t.Title = *req.Title
		}
		if req.Note != nil {
			t.Note = *req.Note
		}
		if req.Status != nil {
			if *req.Status != "todo" && *req.Status != "doing" && *req.Status != "done" {
				return errors.New("status 无效")
			}
			t.Status = *req.Status
		}
		if req.Due != nil {
			t.Due = normDue(*req.Due)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, 404, "任务不存在")
			return
		}
		writeBizErr(w, err.Error())
		return
	}
	// 广播取 bump 后的最终态（闭包拷贝在时间戳递增之前，会被 LWW 判旧拒收）
	var final store.Task
	for _, t := range s.Store.ListTasks() {
		if t.ID == id {
			final = t
		}
	}
	s.hookTaskChanged("upsert", final) // 群组条目广播
	writeOK(w, nil)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	// 先取删除前副本：群组墓碑广播需要 group_id
	var prev store.Task
	for _, t := range s.Store.ListTasks() {
		if t.ID == id {
			prev = t
			break
		}
	}
	if err := s.Store.DeleteTask(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, 404, "任务不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	s.hookTaskDeleted(prev) // 群组删除广播（墓碑时间戳给对端 LWW）
	writeOK(w, nil)
}
