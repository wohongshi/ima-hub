package api

// lab.go — 教研工作台（/lab）：免登录资料收集 + 资源链接。
// 归属混搭：文件先存服务端本地（开箱即用），管理设置配置 ima 默认账号后由管理员另行导入知识库。
// 公开接口全部限速限大；删除仅管理员。

import (
	"crypto/subtle"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"imahub/internal/store"
)

const labMaxBytes = 100 << 20 // 单文件 100MB 上限

// ipLimiter 每 IP 滑动窗口限速（内存态，重启清零）。
type ipLimiter struct {
	mu      sync.Mutex
	hits    map[string][]time.Time
	limit   int
	window  time.Duration
	lastGC  time.Time
}

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{hits: map[string][]time.Time{}, limit: limit, window: window}
}

// prune 清窗口外条目（调用方持锁）。
func (l *ipLimiter) prune(now time.Time) {
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, v := range l.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > l.window {
				delete(l.hits, k)
			}
		}
		l.lastGC = now
	}
}

// windowed 返回窗口内条数（调用方持锁）。
func (l *ipLimiter) windowed(ip string, now time.Time) []time.Time {
	q := l.hits[ip]
	cut := now.Add(-l.window)
	i := 0
	for i < len(q) && q[i].Before(cut) {
		i++
	}
	return q[i:]
}

// hit 记一次命中（滑动窗口）。
func (l *ipLimiter) hit(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.prune(now)
	l.hits[ip] = append(l.windowed(ip, now), now)
}

// blocked 是否超限（不记账，只查询）。
func (l *ipLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(time.Now())
	return len(l.windowed(ip, time.Now())) >= l.limit
}

// reset 清零（登录成功即豁免，正常流程不受限）。
func (l *ipLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, ip)
}

var (
	labLimiter   = newIPLimiter(20, time.Minute) // 教研上传：20 个/分钟每 IP
	loginLimiter = newIPLimiter(20, time.Minute) // 登录失败：20 次/分钟每 IP（成功即清零）
)

// clientIP 取真实客户端 IP（隧道/反代场景取 X-Forwarded-For 首项）。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// labDir 教研资料落盘目录（data/lab/）。
func (s *Server) labDir() string {
	return filepath.Join(filepath.Dir(s.TmpDir), "lab")
}

// writeLabBlob 落文件体（MCP/REST 共用）。
func writeLabBlob(dir, id string, raw []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id), raw, 0o644)
}

// readLabBlob 读文件体。
func readLabBlob(dir, id string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, id))
}

// labManageKeyOK 免账号管理鉴权：管理密钥（头 X-Manage-Key 或查询 ?k=）。
func (s *Server) labManageKeyOK(r *http.Request) bool {
	key := r.Header.Get("X-Manage-Key")
	if key == "" {
		key = r.URL.Query().Get("k")
	}
	want := s.Store.GetSettings().LabManageKey
	if key == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(want)) == 1
}

// labManageAuth 教研管理双通道：管理密钥 或 已登录管理员。免账号模式走密钥，无需任何账号。
func (s *Server) labManageAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.labManageKeyOK(r) {
		return true
	}
	if u, ok := s.authUser(r); ok && u.Role == "admin" {
		return true
	}
	writeErr(w, http.StatusUnauthorized, 401, "需要管理密钥")
	return false
}

// handleLabManageConfig POST /api/lab/manage/config 免登录改教研配置（持管理密钥即可）。
func (s *Server) handleLabManageConfig(w http.ResponseWriter, r *http.Request) {
	if !s.labManageAuth(w, r) {
		return
	}
	var req struct {
		LabEnabled    *bool           `json:"lab_enabled"`
		LabTitle      *string         `json:"lab_title"`
		LabNotice     *string         `json:"lab_notice"`
		LabLinks      []store.LabLink `json:"lab_links"`
		LabManageKey  *string         `json:"lab_manage_key"` // 持有旧密钥可自换新钥
	}
	if !decodeBody(w, r, &req) {
		return
	}
	err := s.Store.UpdateSettings(func(st *store.Settings) error {
		if req.LabEnabled != nil {
			st.LabEnabled = *req.LabEnabled
		}
		if req.LabTitle != nil {
			st.LabTitle = strings.TrimSpace(*req.LabTitle)
		}
		if req.LabNotice != nil {
			st.LabNotice = strings.TrimSpace(*req.LabNotice)
		}
		if req.LabLinks != nil {
			links := []store.LabLink{}
			for _, l := range req.LabLinks {
				name := strings.TrimSpace(l.Name)
				u := strings.TrimSpace(l.URL)
				if name == "" || u == "" || !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
					continue
				}
				links = append(links, store.LabLink{Name: name, URL: u})
			}
			st.LabLinks = links
		}
		if req.LabManageKey != nil && strings.TrimSpace(*req.LabManageKey) != "" {
			st.LabManageKey = strings.TrimSpace(*req.LabManageKey)
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	st := s.Store.GetSettings()
	writeOK(w, map[string]any{
		"lab_enabled": st.LabEnabled, "lab_title": st.LabTitle,
		"lab_notice": st.LabNotice, "lab_links": st.LabLinks,
	})
}

// handleLabInfo GET /api/lab/info 工作台配置（公开）。
func (s *Server) handleLabInfo(w http.ResponseWriter, r *http.Request) {
	st := s.Store.GetSettings()
	title := st.LabTitle
	if title == "" {
		title = "教研工作台"
	}
	links := st.LabLinks
	if links == nil {
		links = []store.LabLink{}
	}
	writeOK(w, map[string]any{
		"enabled": st.LabEnabled,
		"title":   title,
		"notice":  st.LabNotice,
		"links":   links,
		"max_mb":  labMaxBytes >> 20,
		"manage":  st.LabManageKey != "",
	})
}

// handleLabUpload POST /api/lab/upload 匿名上传（multipart 字段 file / by）。
func (s *Server) handleLabUpload(w http.ResponseWriter, r *http.Request) {
	if !s.Store.GetSettings().LabEnabled {
		writeErr(w, http.StatusForbidden, 403, "工作台未启用")
		return
	}
	ip := clientIP(r)
	if labLimiter.blocked(ip) {
		writeErr(w, http.StatusTooManyRequests, 429, "上传太频繁，歇一分钟再来")
		return
	}
	labLimiter.hit(ip)
	r.Body = http.MaxBytesReader(w, r.Body, labMaxBytes+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeBizErr(w, "文件太大或格式不对（单个 ≤ 100MB）")
		return
	}
	src, fh, err := r.FormFile("file")
	if err != nil {
		writeBizErr(w, "缺少文件")
		return
	}
	defer src.Close()
	name := kbSafeName(strings.TrimSpace(fh.Filename))
	if name == "" {
		name = "未命名文件"
	}
	by := strings.TrimSpace(r.FormValue("by"))
	if by == "" {
		by = "匿名"
	}
	if len([]rune(by)) > 24 {
		by = string([]rune(by)[:24])
	}

	id := store.RandomToken()[:12]
	if err := os.MkdirAll(s.labDir(), 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, "存储不可用")
		return
	}
	dst, err := os.Create(filepath.Join(s.labDir(), id))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, "存储不可用")
		return
	}
	n, err := io.Copy(dst, src)
	dst.Close()
	if err != nil {
		_ = os.Remove(filepath.Join(s.labDir(), id))
		writeBizErr(w, "保存失败，请重试")
		return
	}
	if err := s.Store.LabAdd(store.LabFile{ID: id, Name: name, Size: n, By: by}); err != nil {
		_ = os.Remove(filepath.Join(s.labDir(), id))
		writeErr(w, http.StatusInternalServerError, 500, "记录失败，请重试")
		return
	}
	writeOK(w, map[string]any{"id": id, "name": name, "size": n})
}

// handleLabFiles GET /api/lab/files 资料清单（公开，最多 200 条）。
func (s *Server) handleLabFiles(w http.ResponseWriter, r *http.Request) {
	list := s.Store.LabList()
	if len(list) > 200 {
		list = list[:200]
	}
	writeOK(w, map[string]any{"files": list})
}

// handleLabDownload GET /api/lab/file?id= 下载（公开）。
func (s *Server) handleLabDownload(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || strings.ContainsAny(id, "/\\.") {
		writeErr(w, http.StatusNotFound, 404, "文件不存在")
		return
	}
	var meta store.LabFile
	for _, f := range s.Store.LabList() {
		if f.ID == id {
			meta = f
			break
		}
	}
	if meta.ID == "" {
		writeErr(w, http.StatusNotFound, 404, "文件不存在")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(meta.Name))
	http.ServeFile(w, r, filepath.Join(s.labDir(), id))
}

// handleLabDelete DELETE /api/lab/file?id= 删除（管理密钥 或 管理员）。
func (s *Server) handleLabDelete(w http.ResponseWriter, r *http.Request) {
	if !s.labManageAuth(w, r) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || strings.ContainsAny(id, "/\\.") {
		writeErr(w, http.StatusNotFound, 404, "文件不存在")
		return
	}
	if _, err := s.Store.LabDelete(id); err != nil {
		writeErr(w, http.StatusNotFound, 404, "文件不存在")
		return
	}
	_ = os.Remove(filepath.Join(s.labDir(), id))
	writeOK(w, nil)
}
