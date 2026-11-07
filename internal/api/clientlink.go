package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"imahub/internal/store"
)

// ClientLink 客户端模式的服务端链路：登录、密钥分发缓存、离线同步。
// 服务端不可达时直连功能照常（凭已分发 key），恢复后自动同步。
type ClientLink struct {
	s *Server

	mu         sync.Mutex
	serverURL  string       // 规范化服务端地址 http(s)://host:port
	hc         *http.Client // 带 cookie jar 的服务端会话
	connected  bool
	lastErr    string
	lastSyncAt int64
}

func NewClientLink(s *Server) *ClientLink {
	l := &ClientLink{s: s}
	jar, _ := cookiejar.New(nil)
	l.hc = &http.Client{Jar: jar, Timeout: 30 * time.Second}
	st := s.Store.GetSettings()
	l.serverURL = normalizeServerURL(st.ClientServerURL)
	if l.serverURL != "" && st.ClientSession != "" {
		restoreSession(l.hc, l.serverURL, st.ClientSession)
	}
	l.lastSyncAt = st.ClientLastSync
	return l
}

// normalizeServerURL 补全协议、去尾斜杠。
func normalizeServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}
	return strings.TrimRight(raw, "/")
}

// restoreSession 重启后恢复服务端会话（把持久化的 sid 写回 cookie jar）。
func restoreSession(hc *http.Client, serverURL, sid string) {
	u := serverURL + "/api/me"
	req, _ := http.NewRequest("GET", u, nil)
	if req == nil {
		return
	}
	hc.Jar.SetCookies(req.URL, []*http.Cookie{{Name: "sid", Value: sid, Path: "/"}})
}

// Status 供 server-info 展示。
func (l *ClientLink) Status() (connected bool, server string, lastSync int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.connected, l.serverURL, l.lastSyncAt
}

func (l *ClientLink) setErr(err error) {
	l.mu.Lock()
	l.connected = false
	l.lastErr = err.Error()
	l.mu.Unlock()
}

// Heartbeat 探活服务端（5 秒超时）。
func (l *ClientLink) Heartbeat() bool {
	l.mu.Lock()
	url := l.serverURL
	hc := l.hc
	l.mu.Unlock()
	if url == "" {
		return false
	}
	hbc := &http.Client{Jar: hc.Jar, Timeout: 5 * time.Second}
	resp, err := hbc.Get(url + "/api/server-info")
	if err != nil {
		l.setErr(err)
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	l.mu.Lock()
	l.connected = resp.StatusCode == 200
	l.mu.Unlock()
	return resp.StatusCode == 200
}

// Login 服务端登录 + 密钥分发 + 本地用户配置。返回本地用户。
func (l *ClientLink) Login(server, username, password string) (*store.User, error) {
	url := normalizeServerURL(server)
	if url == "" {
		return nil, fmt.Errorf("请填写服务端地址")
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := hc.Post(url+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("服务端不可达：%v", err)
	}
	defer resp.Body.Close()
	var loginRsp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	json.Unmarshal(raw, &loginRsp)
	if resp.StatusCode != 200 || loginRsp.Code != 0 {
		msg := loginRsp.Msg
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("登录失败：%s", msg)
	}

	// 持久化服务端会话（取 sid）
	sid := ""
	if cookies := jar.Cookies(mustURL(url)); cookies != nil {
		for _, c := range cookies {
			if c.Name == "sid" {
				sid = c.Value
			}
		}
	}

	l.mu.Lock()
	l.serverURL = url
	l.hc = hc
	l.connected = true
	l.lastErr = ""
	l.mu.Unlock()

	if err := l.provision0(username); err != nil {
		return nil, err
	}
	l.persistSession(username, sid)
	// 首登即同步一次（失败不影响本地直连）
	if err := l.SyncNow(); err != nil {
		log.Printf("客户端首同步失败（离线可用，稍后自动重试）: %v", err)
	}
	u, ok := l.s.Store.FindUserByName(username)
	if !ok {
		return nil, fmt.Errorf("本地用户创建失败")
	}
	return u, nil
}

// provision0 确保本地用户存在（本地 admin=全部本地能力，权威校验在服务端）。
func (l *ClientLink) provision0(username string) error {
	if _, ok := l.s.Store.FindUserByName(username); ok {
		return nil
	}
	u := &store.User{
		Username: username, PassHash: "-", Role: "admin",
		MCPToken: store.RandomToken(),
	}
	if err := l.s.Store.AddUser(u); err != nil {
		if _, ok := l.s.Store.FindUserByName(username); ok {
			return nil
		}
		return err
	}
	return nil
}

// SyncNow 先推后拉：离线改动回传（LWW），再用合并结果整表更新本地。
func (l *ClientLink) SyncNow() error {
	l.mu.Lock()
	url := l.serverURL
	hc := l.hc
	l.mu.Unlock()
	if url == "" {
		return fmt.Errorf("未连接服务端")
	}

	// 1) push：本地任务/日程/墓碑/活动
	push := map[string]any{
		"tasks":      l.s.Store.ListTasks(),
		"events":     l.s.Store.ListEvents(),
		"tombstones": l.s.Store.TombstonesCopy(),
		"activities": l.s.Store.ListActivities(500),
	}
	raw, _ := json.Marshal(push)
	resp, err := hc.Post(url+"/api/sync/push", "application/json", bytes.NewReader(raw))
	if err != nil {
		l.setErr(err)
		return fmt.Errorf("同步推送失败：%v", err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode != 200 {
		l.setErr(fmt.Errorf("push HTTP %d", resp.StatusCode))
		return fmt.Errorf("同步推送失败：HTTP %d", resp.StatusCode)
	}

	// 2) pull：团队数据 + 分发 key
	resp2, err := hc.Get(url + "/api/sync/pull")
	if err != nil {
		l.setErr(err)
		return fmt.Errorf("同步拉取失败：%v", err)
	}
	defer resp2.Body.Close()
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp2.Body, 8<<20))
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code != 0 {
		l.setErr(fmt.Errorf("pull 响应异常"))
		return fmt.Errorf("同步拉取失败：响应异常")
	}
	var pull struct {
		Tasks         []store.Task         `json:"tasks"`
		Events        []store.Event        `json:"events"`
		Announcements []store.Announcement `json:"announcements"`
		Accounts      []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Owner      string `json:"owner"`
			Source     string `json:"source"`
			ClientID   string `json:"client_id"`
			APIKey     string `json:"api_key"`
			UploadKBID string `json:"upload_kb_id"`
		} `json:"accounts"`
		Settings struct {
			AskBaseURL string `json:"ask_base_url"`
			AskAPIKey  string `json:"ask_api_key"`
			AskModel   string `json:"ask_model"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(envelope.Data, &pull); err != nil {
		return fmt.Errorf("同步拉取失败：%v", err)
	}

	// 3) 整表落本地（push 已合并，pull 结果即权威）
	l.s.Store.ReplaceTasks(pull.Tasks)
	l.s.Store.ReplaceEvents(pull.Events)
	l.s.Store.ReplaceAnnouncements(pull.Announcements)
	// 我的 key 落用户自有账号，其余进团队缓存（直连团队内容用）
	meName := l.s.Store.GetSettings().ClientSessionUser
	own := []store.IMAAccount{}
	cache := []store.IMAAccount{}
	for _, a := range pull.Accounts {
		acc := store.IMAAccount{
			ID: a.ID, Name: a.Name, Owner: a.Owner,
			ClientID: a.ClientID, APIKey: a.APIKey, UploadKBID: a.UploadKBID,
		}
		if a.Owner == meName && a.Source == "user" {
			own = append(own, acc)
		} else {
			cache = append(cache, acc)
		}
	}
	l.s.Store.ReplaceTeamAccounts(cache)
	if me, ok := l.s.Store.FindUserByName(meName); ok && len(own) > 0 {
		_ = l.s.Store.UpdateUser(me.ID, func(x *store.User) error {
			x.IMAAccounts = own
			return nil
		})
	}
	if err := l.s.Store.UpdateSettings(func(st *store.Settings) error {
		st.AskBaseURL = pull.Settings.AskBaseURL
		st.AskAPIKey = pull.Settings.AskAPIKey
		st.AskModel = pull.Settings.AskModel
		st.ClientLastSync = time.Now().Unix()
		return nil
	}); err != nil {
		return err
	}
	l.mu.Lock()
	l.connected = true
	l.lastSyncAt = time.Now().Unix()
	l.mu.Unlock()
	return nil
}

// Run 同步引擎：30 秒心跳，2 分钟同步，离线→恢复时立即补同步。
func (l *ClientLink) Run(stop <-chan struct{}) {
	hb := time.NewTicker(30 * time.Second)
	sy := time.NewTicker(2 * time.Minute)
	defer hb.Stop()
	defer sy.Stop()
	wasDown := !l.Heartbeat()
	for {
		select {
		case <-stop:
			return
		case <-hb.C:
			ok := l.Heartbeat()
			if ok && wasDown {
				if err := l.SyncNow(); err == nil {
					log.Printf("服务端已恢复，离线数据同步完成")
				}
			}
			wasDown = !ok
		case <-sy.C:
			if l.muLockConnected() {
				if err := l.SyncNow(); err != nil {
					log.Printf("同步失败（离线直连不受影响）: %v", err)
				}
			}
		}
	}
}

func (l *ClientLink) muLockConnected() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.connected
}

// persistSession 把服务端会话写入本地设置（重启免重登）。
func (l *ClientLink) persistSession(username, sid string) {
	_ = l.s.Store.UpdateSettings(func(st *store.Settings) error {
		st.ClientServerURL = l.serverURL
		st.ClientSession = sid
		st.ClientSessionUser = username
		return nil
	})
}

func mustURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

// handleClientLogin 客户端首连：输入服务端地址 + 账号密码 → 本地会话 + 分发 key。
func (s *Server) handleClientLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Server   string `json:"server"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		writeBizErr(w, "账号与密码不能为空")
		return
	}
	u, err := s.Link.Login(req.Server, username, req.Password)
	if err != nil {
		writeBizErr(w, err.Error())
		return
	}
	if err := s.setSessionCookie(w, r, u); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, "会话创建失败，请重试")
		return
	}
	writeOK(w, map[string]any{"user": publicUser(u)})
}

// handleClientSyncNow 手动同步（设置页按钮）。
func (s *Server) handleClientSyncNow(w http.ResponseWriter, r *http.Request, u *store.User) {
	if s.Link == nil {
		writeBizErr(w, "非客户端模式")
		return
	}
	if err := s.Link.SyncNow(); err != nil {
		writeBizErr(w, err.Error())
		return
	}
	connected, server, lastSync := s.Link.Status()
	writeOK(w, map[string]any{"connected": connected, "server": server, "last_sync": lastSync})
}

// handleClientStatus 同步状态。
func (s *Server) handleClientStatus(w http.ResponseWriter, r *http.Request, u *store.User) {
	connected, server, lastSync := s.Link.Status()
	writeOK(w, map[string]any{"connected": connected, "server": server, "last_sync": lastSync})
}
