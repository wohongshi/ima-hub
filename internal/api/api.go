// Package api 提供 WebUI 与外部调用的 REST 接口。
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"imahub/internal/auth"
	"imahub/internal/ima"
	"imahub/internal/peer"
	"imahub/internal/scan"
	"imahub/internal/store"
)

const Version = "0.6.0"

type Server struct {
	Store      *store.Store
	ImaBaseURL string
	TmpDir     string
	MCP        http.Handler  // 可选：挂载到 /mcp
	Scanner    *scan.Scanner // 知识库扫描引擎

	// Mode "serve"（默认，全功能服务端）| "client"（客户端：直连 + 离线同步）
	Mode string
	Link *ClientLink // 客户端模式的服务端链路

	// P2P（去中心化互联）：节点身份 / 发现表 / 局域网发现端口 / 离线投递队列
	PeerIdentity *peer.Identity
	PeerTable    *peer.Table
	PeerLanPort  int
	PeerAPIPort  int // 本节点 HTTP 端口（RPC 回包信封 Port 字段）
	PeerOutbox   *peer.Outbox

	rrCounters sync.Map // userID -> *int64，上传自动分配轮询计数

	nonceMu    sync.Mutex
	seenNonces map[string]int64 // 信封防重放：(From,Nonce) 只收一次（TTL 600s）
}

// ---------- 统一响应 ----------

type resp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, httpStatus, bizCode int, msg string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(resp{Code: bizCode, Msg: msg, Data: data})
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, 0, "ok", data)
}

func writeErr(w http.ResponseWriter, httpStatus, bizCode int, msg string) {
	writeJSON(w, httpStatus, bizCode, msg, nil)
}

func writeBizErr(w http.ResponseWriter, msg string) {
	writeErr(w, http.StatusBadRequest, 400, msg)
}

// writeImaErr 透传 ima 侧业务错误。
func writeImaErr(w http.ResponseWriter, err error) {
	var ae *ima.APIError
	if errors.As(err, &ae) {
		writeErr(w, http.StatusOK, ae.Code, "IMA:"+ae.Msg)
		return
	}
	writeErr(w, http.StatusBadGateway, 502, err.Error())
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	// JSON 请求体上限 2MB（login/register/setup 也走这里，防超大 body）
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, http.StatusRequestEntityTooLarge, 413, "请求体过大")
			return false
		}
		writeBizErr(w, "请求体不是合法 JSON")
		return false
	}
	return true
}

// ---------- 认证 ----------

const sidCookie = "sid"

func (s *Server) authUser(r *http.Request) (*store.User, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if tok != "" {
			if u, ok := s.Store.FindUserByMCPToken(tok); ok && !u.Disabled {
				return u, true
			}
		}
	}
	if c, err := r.Cookie(sidCookie); err == nil && c.Value != "" {
		if sess, ok := s.Store.GetSession(c.Value); ok {
			if u, ok := s.Store.FindUserByID(sess.UserID); ok && !u.Disabled {
				return u, true
			}
		}
	}
	return nil, false
}

func (s *Server) requireAuth(next func(w http.ResponseWriter, r *http.Request, u *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.authUser(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, 401, "未登录")
			return
		}
		next(w, r, u)
	}
}

func (s *Server) requireAdmin(next func(w http.ResponseWriter, r *http.Request, u *store.User)) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if u.Role != "admin" {
			writeErr(w, http.StatusForbidden, 403, "需要管理员权限")
			return
		}
		next(w, r, u)
	})
}

// resolveImaAccount 解析 ima 账号：成员自有账号优先，其次团队公共池/服务端默认；
// accountID 可为账号 id、名称或归属用户名（团队内容访问）。
func (s *Server) resolveImaAccount(u *store.User, accountID string) (*store.IMAAccount, error) {
	all := s.Store.AllIMAAccounts()
	ownFirst := func() *store.IMAAccount {
		if u != nil {
			for i := range all {
				if all[i].Source == "user" && all[i].OwnerID == u.ID {
					a := all[i].IMAAccount
					return &a
				}
			}
		}
		if len(all) > 0 {
			a := all[0].IMAAccount
			return &a
		}
		return nil
	}
	if accountID == "" {
		if a := ownFirst(); a != nil {
			return a, nil
		}
		return nil, errors.New("尚未配置 IMA 账号（设置页绑定自己的 key）")
	}
	for i := range all {
		if all[i].ID == accountID || all[i].Name == accountID || all[i].Owner == accountID {
			a := all[i].IMAAccount
			return &a, nil
		}
	}
	if accountID == "default" {
		if a := ownFirst(); a != nil {
			return a, nil
		}
	}
	return nil, fmt.Errorf("IMA 账号不存在: %s", accountID)
}

// effectiveIma 成员自有账号优先，回落团队/默认凭证。
func (s *Server) effectiveIma(u *store.User, accountID string) (*ima.Client, error) {
	a, err := s.resolveImaAccount(u, accountID)
	if err != nil {
		return nil, err
	}
	return ima.New(a.ClientID, a.APIKey, s.ImaBaseURL), nil
}

// poolAccounts 账号池：成员自有排前（上传默认落自己的号），再团队公共/服务端默认。
func (s *Server) poolAccounts(u *store.User) []store.IMAAccount {
	all := s.Store.AllIMAAccounts()
	out := make([]store.IMAAccount, 0, len(all))
	for i := range all {
		if u != nil && all[i].Source == "user" && all[i].OwnerID == u.ID {
			out = append(out, all[i].IMAAccount)
		}
	}
	for i := range all {
		if u == nil || !(all[i].Source == "user" && all[i].OwnerID == u.ID) {
			out = append(out, all[i].IMAAccount)
		}
	}
	return out
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

func accountPreview(clientID string) string {
	if clientID == "" {
		return ""
	}
	if len(clientID) > 6 {
		return clientID[:6] + "****"
	}
	return clientID
}

func publicAccount(a *store.IMAAccount) map[string]any {
	return map[string]any{
		"id":                a.ID,
		"name":              a.Name,
		"client_id_preview": accountPreview(a.ClientID),
		"bound":             a.ClientID != "" && a.APIKey != "",
		"upload_kb_id":      a.UploadKBID,
		"created_at":        a.CreatedAt,
	}
}

// ---------- 账号 ----------

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	writeOK(w, map[string]any{"need_setup": s.Store.UserCount() == 0})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.Mode == "client" {
		writeErr(w, http.StatusForbidden, 403, "客户端模式无需初始化")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	// 单人模式：用户名可省（默认 admin），界面只问密码
	if strings.TrimSpace(req.Username) == "" {
		req.Username = "admin"
	}
	u, err := s.buildUser(req.Username, req.Password, "admin")
	if err != nil {
		writeBizErr(w, err.Error())
		return
	}
	// 原子初始化：并发双请求只有一个能成功（防双管理员）
	if err := s.Store.SetupAdmin(u); err != nil {
		writeErr(w, http.StatusForbidden, 403, err.Error())
		return
	}
	if err := s.setSessionCookie(w, r, u); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, "会话创建失败，请重新登录")
		return
	}
	writeOK(w, map[string]any{"user": publicUser(u)})
}

func (s *Server) buildUser(username, password, role string) (*store.User, error) {
	if err := auth.ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return nil, err
	}
	if role != "admin" && role != "user" {
		return nil, errors.New("角色无效")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	return &store.User{
		Username: username,
		PassHash: hash,
		Role:     role,
		MCPToken: store.RandomToken(),
	}, nil
}

func (s *Server) createUser(username, password, role string) (*store.User, error) {
	u, err := s.buildUser(username, password, role)
	if err != nil {
		return nil, err
	}
	if err := s.Store.AddUser(u); err != nil {
		return nil, err
	}
	return u, nil
}

// requestIsTLS 判断请求是否经 TLS（含反代 X-Forwarded-Proto），决定 Cookie 的 Secure 标志。
func requestIsTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, u *store.User) error {
	tok, err := s.Store.CreateSession(u.ID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sidCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsTLS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 3600,
	})
	return nil
}

func publicUser(u *store.User) map[string]any {
	return map[string]any{
		"id":         u.ID,
		"username":   u.Username,
		"role":       u.Role,
		"disabled":   u.Disabled,
		"created_at": u.CreatedAt,
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.Mode == "client" {
		s.handleClientLogin(w, r)
		return
	}
	// 登录失败限速（公网暴露防爆破）：20 次/分钟每 IP，成功即清零
	if loginLimiter.blocked(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, 429, "尝试太频繁，歇一分钟再来")
		return
	}
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DeviceToken string `json:"device_token"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	// 设备免登录（单人模式「记住本机」）：token 有效即建会话，无需密码
	if req.DeviceToken != "" {
		u, err := s.Store.DeviceLogin(req.DeviceToken)
		if err != nil {
			loginLimiter.hit(clientIP(r))
			writeErr(w, http.StatusUnauthorized, 401, "设备免登录已失效，请用密码登录")
			return
		}
		if u.Disabled {
			writeErr(w, http.StatusForbidden, 403, "账号已被禁用")
			return
		}
		if err := s.setSessionCookie(w, r, u); err != nil {
			writeErr(w, http.StatusInternalServerError, 500, "会话创建失败，请重新登录")
			return
		}
		loginLimiter.reset(clientIP(r))
		writeOK(w, map[string]any{"user": publicUser(u)})
		return
	}
	u, ok := s.Store.FindUserByName(req.Username)
	if !ok || !auth.VerifyPassword(req.Password, u.PassHash) {
		loginLimiter.hit(clientIP(r))
		writeErr(w, http.StatusUnauthorized, 401, "用户名或密码错误")
		return
	}
	if u.Disabled {
		writeErr(w, http.StatusForbidden, 403, "账号已被禁用")
		return
	}
	if err := s.setSessionCookie(w, r, u); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, "会话创建失败，请重新登录")
		return
	}
	loginLimiter.reset(clientIP(r))
	writeOK(w, map[string]any{"user": publicUser(u)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sidCookie); err == nil {
		if err := s.Store.DeleteSession(c.Value); err != nil {
			// 持久化失败时会话可能在重启后复活，如实报错让用户重试
			writeErr(w, http.StatusInternalServerError, 500, "登出未持久化，请重试")
			return
		}
	}
	if s.Mode == "client" {
		// 客户端登出 = 断开服务端会话（地址保留，下次一键重连）
		_ = s.Store.UpdateSettings(func(st *store.Settings) error {
			st.ClientSession = ""
			st.ClientSessionUser = ""
			return nil
		})
	}
	http.SetCookie(w, &http.Cookie{Name: sidCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeOK(w, nil)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if s.Mode == "client" {
		writeErr(w, http.StatusForbidden, 403, "客户端模式请用服务端账号登录")
		return
	}
	if !s.Store.GetSettings().AllowRegister {
		writeErr(w, http.StatusForbidden, 403, "本站暂未开放注册")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	u, err := s.createUser(req.Username, req.Password, "user")
	if err != nil {
		writeBizErr(w, err.Error())
		return
	}
	if err := s.setSessionCookie(w, r, u); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, "会话创建失败，请重新登录")
		return
	}
	writeOK(w, map[string]any{"user": publicUser(u)})
}

// teamAccountViews 全部账号的对外视图（成员自有 + 团队公共 + 服务端默认，带归属）。
func (s *Server) teamAccountViews(u *store.User) ([]map[string]any, string) {
	all := s.Store.AllIMAAccounts()
	accounts := make([]map[string]any, 0, len(all))
	defaultID := ""
	for i := range all {
		a := publicAccount(&all[i].IMAAccount)
		src := all[i].Source
		if src == "default" {
			src = "server_default"
		}
		a["source"] = src
		a["owner"] = all[i].Owner
		if u != nil {
			a["own"] = all[i].OwnerID == u.ID
		}
		if defaultID == "" {
			defaultID = all[i].ID
		}
		accounts = append(accounts, a)
	}
	return accounts, defaultID
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	accounts, defaultID := s.teamAccountViews(u)
	writeOK(w, map[string]any{
		"user": publicUser(u),
		"ima": map[string]any{
			"accounts":   accounts,
			"default_id": defaultID,
			"bound":      len(accounts) > 0, // 兼容旧字段
		},
		"mcp_token_set": u.MCPToken != "",
	})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if !auth.VerifyPassword(req.OldPassword, u.PassHash) {
		writeBizErr(w, "原密码错误")
		return
	}
	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		writeBizErr(w, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	if err := s.Store.UpdateUser(u.ID, func(x *store.User) error {
		x.PassHash = hash
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	// 改密后吊销其余会话（保留当前会话），被窃会话不再有效
	keep := ""
	if c, err := r.Cookie(sidCookie); err == nil {
		keep = c.Value
	}
	if err := s.Store.RevokeUserSessionsExcept(u.ID, keep); err != nil {
		log.Printf("吊销旧会话失败: %v", err)
	}
	// 改密同时清免登录设备：旧设备不再能免密进来
	if err := s.Store.RevokeUserDevices(u.ID); err != nil {
		log.Printf("吊销免登录设备失败: %v", err)
	}
	writeOK(w, nil)
}

// handleBindIma 成员自绑 ima 账号（1 人 1 号；重复提交即更新）。
func (s *Server) handleBindIma(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name       string `json:"name"`
		ClientID   string `json:"client_id"`
		APIKey     string `json:"api_key"`
		UploadKBID string `json:"upload_kb_id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.UploadKBID = strings.TrimSpace(req.UploadKBID)
	if req.ClientID == "" || req.APIKey == "" {
		writeBizErr(w, "Client ID 与 API Key 均不能为空")
		return
	}
	if req.Name == "" {
		req.Name = u.Username + " 的 ima"
	}
	var saved store.IMAAccount
	if err := s.Store.UpdateUser(u.ID, func(x *store.User) error {
		if len(x.IMAAccounts) > 0 {
			saved = x.IMAAccounts[0]
			saved.Name = req.Name
			saved.ClientID = req.ClientID
			saved.APIKey = req.APIKey
			if req.UploadKBID != "" {
				saved.UploadKBID = req.UploadKBID
			}
			x.IMAAccounts[0] = saved
		} else {
			saved = store.IMAAccount{
				ID: "u" + store.RandomToken()[:11], Name: req.Name,
				ClientID: req.ClientID, APIKey: req.APIKey,
				UploadKBID: req.UploadKBID, Owner: u.Username,
				CreatedAt: time.Now().Unix(),
			}
			x.IMAAccounts = []store.IMAAccount{saved}
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	writeOK(w, map[string]any{"account": publicAccount(&saved)})
}

// handleUpdateImaAccount 更新自己的 ima 账号（PATCH /api/me/ima/{id}）；管理员可带 user_id 代管成员的号。
func (s *Server) handleUpdateImaAccount(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	targetID := u.ID
	if q := r.URL.Query().Get("user_id"); q != "" {
		if u.Role != "admin" {
			writeErr(w, http.StatusForbidden, 403, "仅管理员可修改他人账号")
			return
		}
		targetID = q
	}
	var req struct {
		Name       *string `json:"name"`
		ClientID   *string `json:"client_id"`
		APIKey     *string `json:"api_key"`
		UploadKBID *string `json:"upload_kb_id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	var updated *store.IMAAccount
	err := s.Store.UpdateUser(targetID, func(x *store.User) error {
		for i := range x.IMAAccounts {
			if x.IMAAccounts[i].ID == id {
				a := &x.IMAAccounts[i]
				if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
					a.Name = strings.TrimSpace(*req.Name)
				}
				if req.ClientID != nil && strings.TrimSpace(*req.ClientID) != "" {
					a.ClientID = strings.TrimSpace(*req.ClientID)
				}
				if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
					a.APIKey = strings.TrimSpace(*req.APIKey)
				}
				if req.UploadKBID != nil {
					a.UploadKBID = strings.TrimSpace(*req.UploadKBID)
				}
				cp := *a
				updated = &cp
				return nil
			}
		}
		return store.ErrNotFound
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, 404, "ima 账号不存在")
			return
		}
		writeBizErr(w, err.Error())
		return
	}
	writeOK(w, map[string]any{"account": publicAccount(updated)})
}

// handleUnbindIma 解绑自己的 ima 账号（?id= 指定；不带 id 清空全部；管理员可带 user_id 代管）。
func (s *Server) handleUnbindIma(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.URL.Query().Get("id")
	targetID := u.ID
	if q := r.URL.Query().Get("user_id"); q != "" && u.Role == "admin" {
		targetID = q
	}
	if err := s.Store.UpdateUser(targetID, func(x *store.User) error {
		if id == "" {
			x.IMAAccounts = nil
			return nil
		}
		for i := range x.IMAAccounts {
			if x.IMAAccounts[i].ID == id {
				x.IMAAccounts = append(x.IMAAccounts[:i], x.IMAAccounts[i+1:]...)
				return nil
			}
		}
		return store.ErrNotFound
	}); err != nil {
		writeErr(w, http.StatusNotFound, 404, "ima 账号不存在")
		return
	}
	writeOK(w, nil)
}

// handleImaKeys 密钥分发：返回全部账号完整凭证（架构决策：成员免授权取用）。
func (s *Server) handleImaKeys(w http.ResponseWriter, r *http.Request, u *store.User) {
	all := s.Store.AllIMAAccounts()
	keys := make([]map[string]any, 0, len(all))
	for i := range all {
		keys = append(keys, map[string]any{
			"id": all[i].ID, "name": all[i].Name, "owner": all[i].Owner, "owner_id": all[i].OwnerID,
			"source": all[i].Source, "client_id": all[i].ClientID, "api_key": all[i].APIKey,
			"upload_kb_id": all[i].UploadKBID,
		})
	}
	writeOK(w, map[string]any{"keys": keys})
}

// handleImaAccounts 团队共享 ima 账号清单（全员可见，数据互通）。
func (s *Server) handleImaAccounts(w http.ResponseWriter, r *http.Request, u *store.User) {
	accounts, defaultID := s.teamAccountViews(u)
	writeOK(w, map[string]any{"accounts": accounts, "default_id": defaultID})
}

func (s *Server) handleMCPToken(w http.ResponseWriter, r *http.Request, u *store.User) {
	if u.MCPToken == "" {
		if err := s.Store.UpdateUser(u.ID, func(x *store.User) error {
			x.MCPToken = store.RandomToken()
			return nil
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, 500, err.Error())
			return
		}
		if nu, ok := s.Store.FindUserByID(u.ID); ok {
			u = nu
		}
	}
	writeOK(w, map[string]any{"token": u.MCPToken})
}

func (s *Server) handleRotateMCPToken(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.Store.UpdateUser(u.ID, func(x *store.User) error {
		x.MCPToken = store.RandomToken()
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	nu, ok := s.Store.FindUserByID(u.ID)
	if !ok {
		writeErr(w, http.StatusNotFound, 404, "用户不存在")
		return
	}
	writeOK(w, map[string]any{"token": nu.MCPToken})
}

// ---------- 管理 ----------

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request, u *store.User) {
	users := s.Store.ListUsers()
	out := make([]map[string]any, 0, len(users))
	for i := range users {
		out = append(out, publicUser(&users[i]))
	}
	writeOK(w, map[string]any{"users": out})
}

func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	nu, err := s.createUser(req.Username, req.Password, req.Role)
	if err != nil {
		writeBizErr(w, err.Error())
		return
	}
	writeOK(w, map[string]any{"user": publicUser(nu)})
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	if id == u.ID {
		writeBizErr(w, "不能修改自己（请用设置页改密码）")
		return
	}
	var req struct {
		Password *string `json:"password"`
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	err := s.Store.UpdateUser(id, func(x *store.User) error {
		if req.Password != nil {
			if err := auth.ValidatePassword(*req.Password); err != nil {
				return err
			}
			hash, err := auth.HashPassword(*req.Password)
			if err != nil {
				return err
			}
			x.PassHash = hash
		}
		if req.Role != nil {
			if *req.Role != "admin" && *req.Role != "user" {
				return errors.New("角色无效")
			}
			x.Role = *req.Role
		}
		if req.Disabled != nil {
			x.Disabled = *req.Disabled
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, 404, "用户不存在")
			return
		}
		writeBizErr(w, err.Error())
		return
	}
	if req.Password != nil {
		// 管理员重置口令：吊销该用户全部会话（旧口令对应会话不可信）
		if err := s.Store.RevokeUserSessionsExcept(id, ""); err != nil {
			log.Printf("吊销用户 %s 会话失败: %v", id, err)
		}
	}
	writeOK(w, nil)
}

func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	if id == u.ID {
		writeBizErr(w, "不能删除自己")
		return
	}
	if err := s.Store.DeleteUser(id); err != nil {
		writeErr(w, http.StatusNotFound, 404, "用户不存在")
		return
	}
	writeOK(w, nil)
}

func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	st := s.Store.GetSettings()
	writeOK(w, map[string]any{
		"site_name":               st.SiteName,
		"allow_register":          st.AllowRegister,
		"default_ima_client_id":   st.DefaultIMAClientID,
		"default_ima_api_key_set": st.DefaultIMAAPIKey != "",
		"ask_base_url":            st.AskBaseURL,
		"ask_model":               st.AskModel,
		"ask_api_key_set":         st.AskAPIKey != "",
		"discover_url":            st.DiscoverURL,
		"public_addr":             st.PublicAddr,
		"lab_enabled":             st.LabEnabled,
		"lab_title":               st.LabTitle,
		"lab_notice":              st.LabNotice,
		"lab_links":               st.LabLinks,
	})
}

func (s *Server) handleAdminPutSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		SiteName           *string `json:"site_name"`
		AllowRegister      *bool   `json:"allow_register"`
		DefaultIMAClientID *string `json:"default_ima_client_id"`
		DefaultIMAAPIKey   *string `json:"default_ima_api_key"`
		AskBaseURL         *string `json:"ask_base_url"`
		AskModel           *string `json:"ask_model"`
		AskAPIKey          *string `json:"ask_api_key"`
		DiscoverURL        *string `json:"discover_url"`
		PublicAddr         *string `json:"public_addr"`
		LabEnabled         *bool   `json:"lab_enabled"`
		LabTitle           *string `json:"lab_title"`
		LabNotice          *string `json:"lab_notice"`
		LabLinks           []store.LabLink `json:"lab_links"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	err := s.Store.UpdateSettings(func(st *store.Settings) error {
		if req.SiteName != nil {
			st.SiteName = strings.TrimSpace(*req.SiteName)
		}
		if req.AllowRegister != nil {
			st.AllowRegister = *req.AllowRegister
		}
		if req.DefaultIMAClientID != nil {
			st.DefaultIMAClientID = strings.TrimSpace(*req.DefaultIMAClientID)
		}
		if req.DefaultIMAAPIKey != nil && *req.DefaultIMAAPIKey != "" {
			st.DefaultIMAAPIKey = strings.TrimSpace(*req.DefaultIMAAPIKey)
		}
		if req.AskBaseURL != nil {
			st.AskBaseURL = strings.TrimSpace(*req.AskBaseURL)
		}
		if req.AskModel != nil {
			st.AskModel = strings.TrimSpace(*req.AskModel)
		}
		if req.AskAPIKey != nil && *req.AskAPIKey != "" {
			st.AskAPIKey = strings.TrimSpace(*req.AskAPIKey)
		}
		if req.DiscoverURL != nil {
			st.DiscoverURL = strings.TrimSpace(*req.DiscoverURL)
		}
		if req.PublicAddr != nil {
			st.PublicAddr = strings.TrimSpace(*req.PublicAddr)
		}
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
			// 净化：空项丢弃，链接仅收 http(s)（防 javascript: 伪协议进公开页）
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
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	s.handleAdminGetSettings(w, r, u)
}

// ---------- 公共信息 ----------

func (s *Server) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	st := s.Store.GetSettings()
	mode := s.Mode
	if mode == "" {
		mode = "serve"
	}
	out := map[string]any{
		"site_name":      st.SiteName,
		"version":        Version,
		"allow_register": st.AllowRegister,
		"mode":           mode,
	}
	if mode == "client" && s.Link != nil {
		connected, server, lastSync := s.Link.Status()
		out["connected"] = connected
		out["server"] = server
		out["last_sync"] = lastSync
	}
	if s.PeerIdentity != nil {
		if _, ok := s.authUser(r); ok {
			// 节点指纹/昵称仅登录后可见（默认昵称是主机名，不给未鉴权方）
			out["p2p"] = map[string]any{
				"id":        s.PeerIdentity.NodeID(),
				"id_pretty": peer.FormatID(s.PeerIdentity.NodeID()),
				"nickname":  s.PeerIdentity.Nick(),
				"lan_port":  s.PeerLanPort,
			}
		}
	}
	writeOK(w, out)
}
