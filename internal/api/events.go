package api

import (
	"errors"
	"net/http"
	"strconv"

	"imahub/internal/store"
)

// ---------- 日程事件（团队共享） ----------

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request, u *store.User) {
	events := s.Store.ListEvents()
	// 可选 from/to 过滤（Unix 秒）
	q := r.URL.Query()
	if from := parseInt64(q.Get("from")); from > 0 {
		filtered := events[:0]
		for _, e := range events {
			if e.StartAt >= from {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}
	if to := parseInt64(q.Get("to")); to > 0 {
		filtered := events[:0]
		for _, e := range events {
			if e.StartAt <= to {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}
	writeOK(w, map[string]any{"events": events})
}

func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Title    string `json:"title"`
		StartAt  int64  `json:"start_at"`
		EndAt    int64  `json:"end_at"`
		AllDay   bool   `json:"all_day"`
		Location string `json:"location"`
		Note     string `json:"note"`
		GroupID  string `json:"group_id"` // 群组共享（空=本地/团队）
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Title == "" {
		writeBizErr(w, "日程标题不能为空")
		return
	}
	if req.StartAt <= 0 {
		writeBizErr(w, "开始时间无效")
		return
	}
	if req.EndAt > 0 && req.EndAt < req.StartAt {
		writeBizErr(w, "结束时间早于开始时间")
		return
	}
	e, err := s.Store.AddEvent(store.Event{
		UserID: u.ID, By: u.Username,
		Title: req.Title, StartAt: req.StartAt, EndAt: req.EndAt,
		AllDay: req.AllDay, Location: req.Location, Note: req.Note, GroupID: req.GroupID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	s.hookEventChanged("upsert", e) // 群组条目广播
	writeOK(w, map[string]any{"event": e})
}

func (s *Server) handleUpdateEvent(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	var req struct {
		Title    *string `json:"title"`
		StartAt  *int64  `json:"start_at"`
		EndAt    *int64  `json:"end_at"`
		AllDay   *bool   `json:"all_day"`
		Location *string `json:"location"`
		Note     *string `json:"note"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	err := s.Store.UpdateEvent(id, func(e *store.Event) error {
		if req.Title != nil {
			if *req.Title == "" {
				return errors.New("日程标题不能为空")
			}
			e.Title = *req.Title
		}
		if req.StartAt != nil {
			if *req.StartAt <= 0 {
				return errors.New("开始时间无效")
			}
			e.StartAt = *req.StartAt
		}
		if req.EndAt != nil {
			e.EndAt = *req.EndAt
		}
		if req.AllDay != nil {
			e.AllDay = *req.AllDay
		}
		if req.Location != nil {
			e.Location = *req.Location
		}
		if req.Note != nil {
			e.Note = *req.Note
		}
		if e.EndAt > 0 && e.EndAt < e.StartAt {
			return errors.New("结束时间早于开始时间")
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, 404, "日程不存在")
			return
		}
		writeBizErr(w, err.Error())
		return
	}
	// 广播取 bump 后的最终态（同任务：防 LWW 判旧拒收）
	var final store.Event
	for _, e := range s.Store.ListEvents() {
		if e.ID == id {
			final = e
		}
	}
	s.hookEventChanged("upsert", final) // 群组条目广播
	writeOK(w, nil)
}

func (s *Server) handleDeleteEvent(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	// 先取删除前副本：群组墓碑广播需要 group_id
	var prev store.Event
	for _, e := range s.Store.ListEvents() {
		if e.ID == id {
			prev = e
			break
		}
	}
	if err := s.Store.DeleteEvent(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, 404, "日程不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	s.hookEventDeleted(prev) // 群组删除广播（墓碑时间戳给对端 LWW）
	writeOK(w, nil)
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
