package api

// devices.go — 免登录设备（单人模式「记住本机」）：签发/列表/吊销。
// 设备登录复用 POST /api/login 的 device_token 字段（见 api.go handleLogin）。

import (
	"net/http"
	"strings"

	"imahub/internal/store"
)

// handleDeviceCreate POST /api/me/devices 签发本机免登录 token（原始 token 仅此一次返回）。
func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "此设备"
	}
	id, raw, err := s.Store.DeviceCreate(u.ID, name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, "签发失败")
		return
	}
	writeOK(w, map[string]any{"id": id, "name": name, "token": raw})
}

// handleDeviceList GET /api/me/devices 免登录设备清单（不回 Secret）。
func (s *Server) handleDeviceList(w http.ResponseWriter, r *http.Request, u *store.User) {
	list := s.Store.DeviceList(u.ID)
	views := make([]map[string]any, 0, len(list))
	for _, d := range list {
		views = append(views, map[string]any{
			"id": d.ID, "name": d.Name,
			"created_at": d.CreatedAt, "last_used": d.LastUsed,
		})
	}
	writeOK(w, map[string]any{"devices": views})
}

// handleDeviceDelete DELETE /api/me/devices/{id} 吊销单台设备（仅属主）。
func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	if err := s.Store.DeviceDelete(u.ID, id); err != nil {
		writeErr(w, http.StatusNotFound, 404, "设备不存在")
		return
	}
	writeOK(w, nil)
}
