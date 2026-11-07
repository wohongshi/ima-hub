package api

import (
	"net/http"
	"time"

	"imahub/internal/store"
)

// handleSyncPush 客户端离线改动回传：活动流水幂等入库，任务/日程按 LWW 合并（含删除墓碑）。
func (s *Server) handleSyncPush(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Activities []store.Activity `json:"activities"`
		Tasks      []store.Task     `json:"tasks"`
		Events     []store.Event    `json:"events"`
		Tombstones map[string]int64 `json:"tombstones"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	for _, a := range req.Activities {
		if a.By == "" {
			a.By = u.Username
		}
		if a.UserID == "" {
			a.UserID = u.ID
		}
		s.Store.AddActivity(a) // 按 ID 幂等
	}
	adoptT, adoptE := s.Store.SyncMerge(req.Tasks, req.Events, req.Tombstones)
	writeOK(w, map[string]any{
		"adopted_tasks":  adoptT,
		"adopted_events": adoptE,
		"server_time":    time.Now().Unix(),
	})
}

// handleSyncPull 客户端拉取团队数据：任务/日程/公告 + 分发 key + 问答模型配置。
func (s *Server) handleSyncPull(w http.ResponseWriter, r *http.Request, u *store.User) {
	all := s.Store.AllIMAAccounts()
	keys := make([]map[string]any, 0, len(all))
	for i := range all {
		keys = append(keys, map[string]any{
			"id": all[i].ID, "name": all[i].Name, "owner": all[i].Owner, "owner_id": all[i].OwnerID,
			"source": all[i].Source, "client_id": all[i].ClientID, "api_key": all[i].APIKey,
			"upload_kb_id": all[i].UploadKBID,
		})
	}
	st := s.Store.GetSettings()
	writeOK(w, map[string]any{
		"tasks":         s.Store.ListTasks(),
		"events":        s.Store.ListEvents(),
		"announcements": s.Store.ListAnnouncements(50),
		"accounts":      keys,
		"settings": map[string]any{
			"ask_base_url": st.AskBaseURL,
			"ask_api_key":  st.AskAPIKey,
			"ask_model":    st.AskModel,
		},
		"tombstones":  s.Store.TombstonesCopy(),
		"server_time": time.Now().Unix(),
	})
}
