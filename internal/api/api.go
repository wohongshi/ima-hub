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

	"imahub/internal/auth"
	"imahub/internal/ima"
	"imahub/internal/scan"
	"imahub/internal/store"
)

const Version = "0.1.1"

type Server struct {
	Store      *store.Store
	ImaBaseURL string
	TmpDir     string
	MCP        http.Handler  // 可选：挂载到 /mcp
	Scanner    *scan.Scanner // 知识库扫描引擎

	rrCounters sync.Map // userID -> *int64，上传自动分配轮询计数
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

// resolveImaAccount 解析团队共享 ima 账号（知识库数据全团队互通）。
// accountID 空/"default" → 第一个团队账号；否则按 id 或名称匹配；
// 团队无账号时回落服务端默认凭证（虚拟账号 id "default"）。
func (s *Server) resolveImaAccount(_ *store.User, accountID string) (*store.IMAAccount, error) {
	pool := s.Store.GetTeamAccounts()
	if len(pool) > 0 {
		if accountID == "" || accountID == "default" {
			a := pool[0]
			return &a, nil
		}
		for _, a := range pool {
			if a.ID == accountID || a.Name == accountID {
				ac := a
				return &ac, nil
			}
		}
		return nil, fmt.Errorf("IMA 账号不存在: %s", accountID)
	}
	st := s.Store.GetSettings()
	if st.DefaultIMAClientID != "" && st.DefaultIMAAPIKey != "" {
		if accountID != "" && accountID != "default" {
			return nil, fmt.Errorf("IMA 账号不存在: %s", accountID)
		}
		return &store.IMAAccount{ID: "default", Name: "服务端默认账号", ClientID: st.DefaultIMAClientID, APIKey: st.DefaultIMAAPIKey}, nil
	}
	return nil, errors.New("团队尚未配置 IMA 账号（组长可在设置页添加，或配置服务端默认凭证）")
}

// effectiveIma 团队账号池优先，回落服务端默认凭证。
func (s *Server) effectiveIma(u *store.User, accountID string) (*ima.Client, error) {
	a, err := s.resolveImaAccount(u, accountID)
	if err != nil {
		return nil, err
	}
	return ima.New(a.ClientID, a.APIKey, s.ImaBaseURL), nil
}

// poolAccounts 团队共享账号池全部账号（空时回落服务端默认凭证）。
func (s *Server) poolAccounts(_ *store.User) []store.IMAAccount {
	pool := s.Store.GetTeamAccounts()
	if len(pool) > 0 {
		return pool
	}
	st := s.Store.GetSettings()
	if st.DefaultIMAClientID != "" && st.DefaultIMAAPIKey != "" {
		return []store.IMAAccount{{
			ID: "default", Name: "服务端默认账号",
			ClientID: st.DefaultIMAClientID, APIKey: st.DefaultIMAAPIKey,
		}}
	}
	return nil
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
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
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
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	u, ok := s.Store.FindUserByName(req.Username)
	if !ok || !auth.VerifyPassword(req.Password, u.PassHash) {
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
	http.SetCookie(w, &http.Cookie{Name: sidCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeOK(w, nil)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
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

// teamAccountViews 团队共享账号池的对外视图（含服务端默认兜底）。
func (s *Server) teamAccountViews() ([]map[string]any, string) {
	pool := s.Store.GetTeamAccounts()
	accounts := make([]map[string]any, 0, len(pool)+1)
	defaultID := ""
	if len(pool) > 0 {
		defaultID = pool[0].ID
		for i := range pool {
			a := publicAccount(&pool[i])
			a["source"] = "team"
			accounts = append(accounts, a)
		}
	} else {
		st := s.Store.GetSettings()
		if st.DefaultIMAClientID != "" && st.DefaultIMAAPIKey != "" {
			defaultID = "default"
			accounts = append(accounts, map[string]any{
				"id": "default", "name": "服务端默认账号",
				"client_id_preview": accountPreview(st.DefaultIMAClientID),
				"bound":             true, "source": "server_default",
			})
		}
	}
	return accounts, defaultID
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	accounts, defaultID := s.teamAccountViews()
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
	writeOK(w, nil)
}

// handleBindIma 新增团队 ima 账号（组长维护团队共享账号池）。
func (s *Server) handleBindIma(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name     string `json:"name"`
		ClientID string `json:"client_id"`
		APIKey   string `json:"api_key"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.APIKey = strings.TrimSpace(req.APIKey)
	if req.ClientID == "" || req.APIKey == "" {
		writeBizErr(w, "Client ID 与 API Key 均不能为空")
		return
	}
	if req.Name == "" {
		req.Name = fmt.Sprintf("账号 %d", len(s.Store.GetTeamAccounts())+1)
	}
	acc, err := s.Store.AddTeamAccount(store.IMAAccount{
		Name:     req.Name,
		ClientID: req.ClientID,
		APIKey:   req.APIKey,
	})
	if err != nil {
		writeBizErr(w, err.Error())
		return
	}
	writeOK(w, map[string]any{"account": publicAccount(&acc)})
}

// handleUpdateImaAccount 更新团队账号（PATCH /api/me/ima/{id}）。
func (s *Server) handleUpdateImaAccount(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
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
	err := s.Store.UpdateTeamAccount(id, func(a *store.IMAAccount) error {
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
		updated = a
		return nil
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

// handleUnbindIma 删除团队账号（?id= 指定；不带 id 清空全部）。
func (s *Server) handleUnbindIma(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.URL.Query().Get("id")
	if id == "" {
		for _, a := range s.Store.GetTeamAccounts() {
			_ = s.Store.DeleteTeamAccount(a.ID)
		}
		writeOK(w, nil)
		return
	}
	if err := s.Store.DeleteTeamAccount(id); err != nil {
		writeErr(w, http.StatusNotFound, 404, "ima 账号不存在")
		return
	}
	writeOK(w, nil)
}

// handleImaAccounts 团队共享 ima 账号清单（全员可见，数据互通）。
func (s *Server) handleImaAccounts(w http.ResponseWriter, r *http.Request, u *store.User) {
	accounts, defaultID := s.teamAccountViews()
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
	writeOK(w, map[string]any{
		"site_name":      st.SiteName,
		"version":        Version,
		"allow_register": st.AllowRegister,
	})
}
