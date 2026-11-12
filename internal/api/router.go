package api

import "net/http"

// Handler 组装全部路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 公共
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /api/server-info", s.handleServerInfo)
	mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("POST /api/register", s.handleRegister)

	// 个人
	mux.HandleFunc("GET /api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("POST /api/me/password", s.requireAuth(s.handleChangePassword))

	// ima 账号：成员自助（1 人 1 号），管理员可带 user_id 代管
	mux.HandleFunc("POST /api/me/ima", s.requireAuth(s.handleBindIma))
	mux.HandleFunc("PATCH /api/me/ima/{id}", s.requireAuth(s.handleUpdateImaAccount))
	mux.HandleFunc("DELETE /api/me/ima", s.requireAuth(s.handleUnbindIma))
	mux.HandleFunc("GET /api/me/ima-keys", s.requireAuth(s.handleImaKeys))
	mux.HandleFunc("GET /api/me/mcp-token", s.requireAuth(s.handleMCPToken))
	mux.HandleFunc("POST /api/me/mcp-token/rotate", s.requireAuth(s.handleRotateMCPToken))

	// 免登录设备（单人模式「记住本机」）
	mux.HandleFunc("GET /api/me/devices", s.requireAuth(s.handleDeviceList))
	mux.HandleFunc("POST /api/me/devices", s.requireAuth(s.handleDeviceCreate))
	mux.HandleFunc("DELETE /api/me/devices/{id}", s.requireAuth(s.handleDeviceDelete))

	// 教研工作台（/lab 免登录资料收集；开关/提示语/链接在管理设置）
	mux.HandleFunc("GET /lab", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/lab.html", http.StatusFound)
	})
	mux.HandleFunc("GET /api/lab/info", s.handleLabInfo)
	mux.HandleFunc("POST /api/lab/upload", s.handleLabUpload)
	mux.HandleFunc("GET /api/lab/files", s.handleLabFiles)
	mux.HandleFunc("GET /api/lab/file", s.handleLabDownload)
	mux.HandleFunc("DELETE /api/lab/file", s.requireAdmin(s.handleLabDelete))

	// 离线同步（客户端）：改动回传 + 团队数据/分发 key 拉取
	mux.HandleFunc("POST /api/sync/push", s.requireAuth(s.handleSyncPush))
	mux.HandleFunc("GET /api/sync/pull", s.requireAuth(s.handleSyncPull))
	mux.HandleFunc("POST /api/client/sync", s.requireAuth(s.handleClientSyncNow))
	mux.HandleFunc("GET /api/client/status", s.requireAuth(s.handleClientStatus))

	// 离线上传队列（排队/自动续传/重试）
	mux.HandleFunc("POST /api/queue/upload", s.requireAuth(s.handleQueueUpload))
	mux.HandleFunc("GET /api/queue", s.requireAuth(s.handleQueueList))
	mux.HandleFunc("POST /api/queue/drain", s.requireAuth(s.handleQueueDrain))
	mux.HandleFunc("POST /api/queue/{id}/retry", s.requireAuth(s.handleQueueRetry))
	mux.HandleFunc("DELETE /api/queue/{id}", s.requireAuth(s.handleQueueDelete))

	// P2P 节点：身份 / 局域网发现 / 邀请码
	mux.HandleFunc("GET /api/peers", s.requireAuth(s.handlePeers))
	mux.HandleFunc("GET /api/peers/me", s.requireAuth(s.handlePeersMe))
	mux.HandleFunc("PATCH /api/peers/me", s.requireAuth(s.handlePeersMeNickname))
	mux.HandleFunc("POST /api/peers/me/invite", s.requireAuth(s.handlePeersMakeInvite))
	mux.HandleFunc("POST /api/peers/invite", s.requireAuth(s.handlePeersAddInvite))
	mux.HandleFunc("PATCH /api/peers/{id}", s.requireAuth(s.handlePeersPatch))
	mux.HandleFunc("DELETE /api/peers/{id}", s.requireAuth(s.handlePeersDelete))

	// 知识库代理访问（经所有者节点查询/下载，按授权范围）
	mux.HandleFunc("GET /api/peers/{id}/kbs", s.requireAuth(s.handlePeerKBList))
	mux.HandleFunc("POST /api/peers/{id}/kb/search", s.requireAuth(s.handlePeerKBSearch))
	mux.HandleFunc("POST /api/peers/{id}/kb/browse", s.requireAuth(s.handlePeerKBBrowse))
	mux.HandleFunc("GET /api/peers/{id}/kb/download", s.requireAuth(s.handlePeerKBDownload))

	// 访问申请制共享（请求/审批/撤销）
	mux.HandleFunc("GET /api/access/requests", s.requireAuth(s.handleAccessList))
	mux.HandleFunc("POST /api/access/requests", s.requireAuth(s.handleAccessCreate))
	mux.HandleFunc("POST /api/access/requests/{id}/decide", s.requireAuth(s.handleAccessDecide))

	// 群组：任务/日程共享圈（对等广播 + LWW 收敛）
	mux.HandleFunc("GET /api/groups", s.requireAuth(s.handleGroupList))
	mux.HandleFunc("POST /api/groups", s.requireAuth(s.handleGroupCreate))
	mux.HandleFunc("PATCH /api/groups/{id}", s.requireAuth(s.handleGroupUpdate))
	mux.HandleFunc("DELETE /api/groups/{id}", s.requireAuth(s.handleGroupDelete))
	mux.HandleFunc("POST /api/groups/{id}/leave", s.requireAuth(s.handleGroupLeave))
	mux.HandleFunc("GET /api/groups/{id}/feed", s.requireAuth(s.handleGroupFeed))
	// 节点间 RPC（免 Cookie：信封 Ed25519 签名鉴权 + X25519 加密封载）
	mux.HandleFunc("POST /api/peer/rpc", s.handlePeerRPC)

	// 管理
	mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.handleAdminUsers))
	mux.HandleFunc("POST /api/admin/users", s.requireAdmin(s.handleAdminCreateUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", s.requireAdmin(s.handleAdminUpdateUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", s.requireAdmin(s.handleAdminDeleteUser))
	mux.HandleFunc("GET /api/admin/settings", s.requireAdmin(s.handleAdminGetSettings))
	mux.HandleFunc("PUT /api/admin/settings", s.requireAdmin(s.handleAdminPutSettings))

	// IMA 代理
	mux.HandleFunc("GET /api/ima/accounts", s.requireAuth(s.handleImaAccounts))
	mux.HandleFunc("GET /api/ima/test", s.requireAuth(s.handleImaTest))
	mux.HandleFunc("GET /api/ima/kbs", s.requireAuth(s.handleImaKBs))
	mux.HandleFunc("GET /api/ima/kb", s.requireAuth(s.handleImaKB))
	mux.HandleFunc("GET /api/ima/addable-kbs", s.requireAuth(s.handleImaAddableKBs))
	mux.HandleFunc("GET /api/ima/list", s.requireAuth(s.handleImaList))
	mux.HandleFunc("GET /api/ima/search", s.requireAuth(s.handleImaSearch))
	mux.HandleFunc("GET /api/ima/media", s.requireAuth(s.handleImaMedia))
	mux.HandleFunc("GET /api/ima/download", s.requireAuth(s.handleImaDownload))
	mux.HandleFunc("POST /api/ima/download-zip", s.requireAuth(s.handleDownloadZip))
	mux.HandleFunc("GET /api/ima/check-names", s.requireAuth(s.handleImaCheckNames))
	// 内容操作（上传/导入/写笔记）：全员可用（用户指示：成员可上传、下载、访问）
	mux.HandleFunc("POST /api/ima/upload", s.requireAuth(s.handleImaUpload))
	mux.HandleFunc("POST /api/ima/import-urls", s.requireAuth(s.handleImaImportURLs))
	mux.HandleFunc("GET /api/ima/notebooks", s.requireAuth(s.handleImaNotebooks))
	mux.HandleFunc("GET /api/ima/notes", s.requireAuth(s.handleImaNotes))
	mux.HandleFunc("GET /api/ima/notes/search", s.requireAuth(s.handleImaNotesSearch))
	mux.HandleFunc("GET /api/ima/note", s.requireAuth(s.handleImaNote))
	mux.HandleFunc("POST /api/ima/note", s.requireAuth(s.handleImaCreateNote))
	mux.HandleFunc("POST /api/ima/note/append", s.requireAuth(s.handleImaAppendNote))

	// 首页仪表盘
	mux.HandleFunc("GET /api/dashboard", s.requireAuth(s.handleDashboard))
	mux.HandleFunc("POST /api/scan", s.requireAuth(s.handleScan))
	// 知识库智能问答（agent 级查询，全员可用）
	mux.HandleFunc("POST /api/ask", s.requireAuth(s.handleAsk))
	mux.HandleFunc("GET /api/ask/status", s.requireAuth(s.handleAskStatus))
	mux.HandleFunc("GET /api/tasks", s.requireAuth(s.handleListTasks))
	mux.HandleFunc("POST /api/tasks", s.requireAuth(s.handleCreateTask))
	mux.HandleFunc("PATCH /api/tasks/{id}", s.requireAuth(s.handleUpdateTask))
	mux.HandleFunc("DELETE /api/tasks/{id}", s.requireAuth(s.handleDeleteTask))

	// 日程事件（团队共享，全员可增删改）
	mux.HandleFunc("GET /api/events", s.requireAuth(s.handleListEvents))
	mux.HandleFunc("POST /api/events", s.requireAuth(s.handleCreateEvent))
	mux.HandleFunc("PATCH /api/events/{id}", s.requireAuth(s.handleUpdateEvent))
	mux.HandleFunc("DELETE /api/events/{id}", s.requireAuth(s.handleDeleteEvent))

	// 团队公告（全员可读，组长发布/管理）
	mux.HandleFunc("GET /api/announcements", s.requireAuth(s.handleListAnnouncements))
	mux.HandleFunc("POST /api/announcements", s.requireAdmin(s.handleCreateAnnouncement))
	mux.HandleFunc("PATCH /api/announcements/{id}", s.requireAdmin(s.handleUpdateAnnouncement))
	mux.HandleFunc("DELETE /api/announcements/{id}", s.requireAdmin(s.handleDeleteAnnouncement))

	// MCP 入口（在 main 中挂载到 /mcp，此处占位避免 404 时混淆）
	if s.MCP != nil {
		mux.Handle("/mcp", s.MCP)
	}
	return mux
}
