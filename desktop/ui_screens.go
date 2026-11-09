package main

// 各功能屏：知识库浏览 / 节点与共享 / 日程 / 问答 / 设置。

import (
	"fmt"
	"strings"

	"imahub/internal/ui"
)

// loadFor 按屏异步拉数据（完成后构树重绘）。
func (a *app) loadFor(nav string) {
	load := func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.rebuild()
	}
	switch nav {
	case "home":
		if d, err := a.cli.get("/api/dashboard"); err == nil {
			a.mu.Lock()
			a.dashboard = d
			a.mu.Unlock()
		}
		if d, err := a.cli.get("/api/tasks"); err == nil {
			a.mu.Lock()
			a.tasks = asArr(d["tasks"])
			a.mu.Unlock()
		}
		if d, err := a.cli.get("/api/peers"); err == nil {
			a.mu.Lock()
			a.peers = asArr(d["peers"])
			a.mu.Unlock()
		}
		if d, err := a.cli.get("/api/access/requests"); err == nil {
			a.mu.Lock()
			a.access = asArr(d["requests"])
			a.mu.Unlock()
		}
	case "files":
		if d, err := a.cli.get("/api/ima/kbs"); err == nil {
			a.mu.Lock()
			a.kbs = asArr(d["info_list"])
			a.mu.Unlock()
		}
	case "peers":
		if d, err := a.cli.get("/api/peers"); err == nil {
			a.mu.Lock()
			a.peers = asArr(d["peers"])
			a.mu.Unlock()
		}
		if d, err := a.cli.get("/api/access/requests"); err == nil {
			a.mu.Lock()
			a.access = asArr(d["requests"])
			a.mu.Unlock()
		}
	case "schedule":
		if d, err := a.cli.get("/api/tasks"); err == nil {
			a.mu.Lock()
			a.tasks = asArr(d["tasks"])
			a.mu.Unlock()
		}
		if d, err := a.cli.get("/api/dashboard"); err == nil {
			a.mu.Lock()
			a.dashboard = d
			a.mu.Unlock()
		}
	case "settings":
		if d, err := a.cli.get("/api/server-info"); err == nil {
			a.mu.Lock()
			a.settings = d
			a.mu.Unlock()
		}
	}
	load()
}

func asArr(v any) []map[string]any {
	if list, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(list))
		for _, it := range list {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ---------- 知识库/文件 ----------

func (a *app) buildFiles() ui.Node {
	th := a.sh.Th
	head := ui.HA(8, 0.5, ui.LB("知识库", 21, th.Text), ui.Fill(),
		ui.NewButton("上传文件", ui.BtnGhost, func() { a.toast("上传：请在浏览器端操作大文件上传", 1) }))

	if a.fileKB != "" {
		// 文件浏览视图
		rows := []ui.Node{
			ui.HA(8, 0.5,
				ui.NewButton("← 返回", ui.BtnGhost, func() {
					a.mu.Lock()
					a.fileKB, a.files = "", nil
					a.rebuild()
					a.mu.Unlock()
					a.loadFor("files")
				}),
				ui.L(a.filePath, 12.5, th.Muted),
			),
		}
		if len(a.files) == 0 {
			rows = append(rows, ui.L("加载中或空目录…", 12.5, th.Muted))
		}
		for _, f := range a.files {
			title := str(f["title"])
			isFolder, _ := f["is_folder"].(bool)
			if isFolder {
				fid := str(f["folder_id"])
				row := &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 9, MinH: 44,
					Child: ui.HA(12, 0.5,
						&ui.Avatar{Icon: ui.IconFolder, UseIcon: true, BG: th.Panel, Fore: th.Warn, Size_: 30},
						ui.L(title, 13, th.Text), ui.Fill(),
						&iconBox{icon: ui.IconChevronRight, size: 15, col: th.Muted},
					)}
				row.OnTap(func() { a.openFolder(a.fileKB, fid, title) })
				rows = append(rows, row)
				continue
			}
			media := str(f["media_id"])
			row := fileRowNode(th, title, "文件")
			row.OnTap(func() { a.downloadMedia(a.fileKB, media, title) })
			rows = append(rows, row)
		}
		return ui.V(16, head, &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8, rows...)})
	}

	// 知识库列表
	rows := []ui.Node{ui.L("我的知识库", 14, th.Text2)}
	if len(a.kbs) == 0 {
		rows = append(rows, ui.L("暂无知识库（先在设置里绑定 ima 账号）", 12.5, th.Muted))
	}
	for _, kb := range a.kbs {
		id := str(kb["id"])
		name := str(kb["name"])
		if name == "" {
			name = id
		}
		row := &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 10, MinH: 52,
			Child: ui.HA(12, 0.5,
				&ui.Avatar{Icon: ui.IconDatabase, UseIcon: true, BG: th.AccentSoft, Fore: th.Accent, Size_: 34},
				ui.VA(2, 0, ui.L(name, 13.5, th.Text), ui.L(id, 10.5, th.Muted)),
				ui.Fill(),
				&iconBox{icon: ui.IconChevronRight, size: 15, col: th.Muted},
			)}
		idCopy, nameCopy := id, name
		row.OnTap(func() { a.openFolder(idCopy, "", nameCopy) })
		rows = append(rows, row)
	}
	return ui.V(16, head, &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8, rows...)})
}

func (a *app) openFolder(kb, folder, name string) {
	a.mu.Lock()
	a.fileKB = kb
	a.filePath = name
	a.files = nil
	a.rebuild()
	a.mu.Unlock()
	go func() {
		url := "/api/ima/list?kb=" + urlQ(kb) + "&folder=" + urlQ(folder)
		d, err := a.cli.get(url)
		if err != nil {
			a.toast(err.Error(), 2)
			return
		}
		a.mu.Lock()
		a.files = asArr(d["items"])
		a.rebuild()
		a.mu.Unlock()
	}()
}

func (a *app) downloadMedia(kb, media, name string) {
	a.toast("开始下载："+name, 0)
	go func() {
		url := a.eng.url + "/api/ima/download?kb=" + urlQ(kb) + "&media=" + urlQ(media)
		resp, err := a.cli.cli.Get(url) //nolint:gosec // 本机引擎地址由 boot 注入
		if err != nil {
			a.toast("下载失败", 2)
			return
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			a.toast("下载完成（浏览器同款直连）", 0)
		} else {
			a.toast(fmt.Sprintf("下载失败 %d", resp.StatusCode), 2)
		}
	}()
}

func urlQ(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "&", "%26")
}

// ---------- 节点与共享 ----------

func (a *app) buildPeers() ui.Node {
	th := a.sh.Th
	head := ui.HA(8, 0.5, ui.LB("节点", 21, th.Text), ui.Fill(),
		ui.NewButton("生成邀请码", ui.BtnPrimary, func() { a.openInvite() }),
		ui.NewButton("添加节点", ui.BtnGhost, func() { a.openAddPeer() }))

	rows := []ui.Node{ui.L("节点列表", 14, th.Text2)}
	if len(a.peers) == 0 {
		rows = append(rows, ui.L("局域网自动发现中… 也可用邀请码互联", 12.5, th.Muted))
	}
	for _, p := range a.peers {
		name := str(p["nickname"])
		if name == "" {
			name = str(p["id"])
		}
		online, _ := p["online"].(bool)
		dot := th.Muted
		state := "离线"
		if online {
			dot = th.OK
			state = "在线"
		}
		granted := 0
		if g, ok := p["granted"].([]any); ok {
			granted = len(g)
		}
		var rightNode ui.Node = ui.L(state, 11, dot)
		if granted > 0 {
			id := str(p["id"])
			kbBtn := ui.NewButton("知识库", ui.BtnSoft, func() { a.openSharedKB(id, name) })
			rightNode = ui.HA(8, 0.5, ui.NewBadge(fmt.Sprintf("已获 %d 项", granted), th.OKSoft, th.OK), kbBtn, rightNode)
		}
		row := &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 9, MinH: 46,
			Child: ui.HA(12, 0.5,
				&ui.Avatar{Icon: ui.IconServer, UseIcon: true, BG: th.Panel, Fore: th.Text2, Size_: 30},
				ui.VA(2, 0, ui.L(name, 13, th.Text), ui.L(str(p["source"])+" · "+shortID(str(p["id"])), 10.5, th.Muted)),
				ui.Fill(), rightNode,
			)}
		rows = append(rows, row)
	}

	// 访问申请
	accRows := []ui.Node{ui.L("访问申请", 14, th.Text2)}
	if len(a.access) == 0 {
		accRows = append(accRows, ui.L("暂无申请", 12.5, th.Muted))
	}
	for _, r := range a.access {
		who := str(r["peer_nick"])
		if who == "" {
			who = shortID(str(r["peer_id"]))
		}
		status := str(r["status"])
		dir := str(r["direction"])
		prefix := "收到"
		if dir == "sent" {
			prefix = "发出"
		}
		badge := ui.NewBadge(statusText(status), statusSoft(th, status), statusFore(th, status))
		var act ui.Node = ui.Fill()
		if dir == "received" && status == "pending" {
			id := str(r["id"])
			act = ui.HA(8, 0.5,
				uiBtn("批准", ui.BtnPrimary, func() { a.decideAccess(id, "approve") }),
				uiBtn("拒绝", ui.BtnGhost, func() { a.decideAccess(id, "deny") }),
			)
		}
		row := &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 9, MinH: 46,
			Child: ui.HA(12, 0.5,
				&ui.Avatar{Text: firstRune(who), BG: th.AccentSoft, Fore: th.Accent, Size_: 30},
				ui.VA(2, 0, ui.L(prefix+" · "+who, 13, th.Text), ui.L(strings.Join(asStrs(r["scopes"]), " / "), 10.5, th.Muted)),
				ui.Fill(), badge, act,
			)}
		accRows = append(accRows, row)
	}

	return ui.V(16, head,
		&ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8, rows...)},
		&ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8, accRows...)},
	)
}

func uiBtn(text string, style ui.BtnStyle, fn func()) ui.Node {
	return ui.NewButton(text, style, fn)
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return "?"
}

func shortID(id string) string {
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

func asStrs(v any) []string {
	if list, ok := v.([]any); ok {
		out := make([]string, 0, len(list))
		for _, it := range list {
			out = append(out, str(it))
		}
		return out
	}
	return nil
}

func statusText(s string) string {
	switch s {
	case "approved":
		return "已批准"
	case "denied":
		return "已拒绝"
	case "revoked":
		return "已撤销"
	case "expired":
		return "已过期"
	}
	return "待处理"
}

func statusSoft(th *ui.Theme, s string) ui.Color {
	switch s {
	case "approved":
		return th.OKSoft
	case "denied", "revoked":
		return th.DangerSoft
	}
	return th.WarnSoft
}

func statusFore(th *ui.Theme, s string) ui.Color {
	switch s {
	case "approved":
		return th.OK
	case "denied", "revoked":
		return th.Danger
	}
	return th.Warn
}

func (a *app) decideAccess(id, action string) {
	go func() {
		_, err := a.cli.post("/api/access/requests/"+id+"/decide", map[string]any{"action": action})
		if err != nil {
			a.toast(err.Error(), 2)
			return
		}
		a.toast("已完成", 0)
		a.loadFor("peers")
	}()
}

// openInvite 生成邀请码模态。
func (a *app) openInvite() {
	th := a.sh.Th
	codeLabel := ui.L("点击生成后显示邀请码", 12, th.Muted)
	body := ui.V(12,
		ui.L("对方凭此码添加你为节点，添加即互相信任", 12, th.Text2),
		&ui.Box{BG: th.Panel2, Radius: 10, Pad: 14, MinH: 72, Child: codeLabel},
	)
	var m *ui.Modal
	m = ui.NewModal("生成邀请码", body, ui.HA(10, 1,
		ui.Fill(),
		ui.NewButton("关闭", ui.BtnGhost, func() { a.sh.PopOverlay() }),
		ui.NewButton("生成", ui.BtnPrimary, func() {
			go func() {
				d, err := a.cli.post("/api/peers/me/invite", map[string]any{})
				if err != nil {
					a.toast(err.Error(), 2)
					return
				}
				code := str(d["code"])
				a.mu.Lock()
				codeLabel.Text = code
				a.rebuild()
				a.mu.Unlock()
			}()
		}),
	))
	m.OnClose = func() { a.sh.PopOverlay() }
	a.sh.PushOverlay(m)
}

// openAddPeer 凭邀请码添加节点。
func (a *app) openAddPeer() {
	th := a.sh.Th
	input := &ui.TextField{Placeholder: "IH1. 开头的邀请码"}
	body := ui.V(12, ui.L("粘贴对方的邀请码", 12, th.Text2), input)
	m := ui.NewModal("添加节点", body, ui.HA(10, 1,
		ui.Fill(),
		ui.NewButton("取消", ui.BtnGhost, func() { a.sh.PopOverlay() }),
		ui.NewButton("添加", ui.BtnPrimary, func() {
			code := input.Value()
			if code == "" {
				a.toast("请输入邀请码", 1)
				return
			}
			go func() {
				_, err := a.cli.post("/api/peers/invite", map[string]any{"code": code})
				if err != nil {
					a.toast(err.Error(), 2)
					return
				}
				a.sh.PopOverlay()
				a.toast("已添加节点", 0)
				a.loadFor("peers")
			}()
		}),
	))
	m.OnClose = func() { a.sh.PopOverlay() }
	a.sh.PushOverlay(m)
}

// ---------- 日程 ----------

func (a *app) buildSchedule() ui.Node {
	th := a.sh.Th
	head := ui.HA(8, 0.5, ui.LB("任务 · 日程", 21, th.Text), ui.Fill(),
		ui.NewButton("新建任务", ui.BtnPrimary, func() { a.openNewTask() }))

	rows := []ui.Node{ui.L("任务", 14, th.Text2)}
	for _, t := range a.tasks {
		title := str(t["title"])
		status := str(t["status"])
		due := str(t["due"])
		if due == "" {
			due = "无期限"
		}
		id := str(t["id"])
		done := status == "done"
		check := &ui.Checkbox{Checked: done}
		check.OnChange = func(v bool) {
			st := "done"
			if !v {
				st = "todo"
			}
			go func() {
				a.cli.do("PATCH", "/api/tasks/"+id, map[string]any{"status": st})
				a.loadFor("schedule")
			}()
		}
		fg := th.Text
		if done {
			fg = th.Muted
		}
		rows = append(rows, &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 9, MinH: 44,
			Child: ui.HA(12, 0.5, check,
				ui.VA(2, 0, ui.L(title, 13, fg), ui.L("截止 "+due, 10.5, th.Muted)),
				ui.Fill(),
				ui.L(statusLabel(status), 11, th.Muted),
			)})
	}
	if len(a.tasks) == 0 {
		rows = append(rows, ui.L("暂无任务", 12.5, th.Muted))
	}
	return ui.V(16, head, &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8, rows...)})
}

func (a *app) openNewTask() {
	th := a.sh.Th
	title := &ui.TextField{Placeholder: "任务标题"}
	due := &ui.TextField{Placeholder: "截止日期（如 2026-11-30，可空）"}
	body := ui.V(12, ui.L("标题", 12, th.Text2), title, ui.L("截止", 12, th.Text2), due)
	m := ui.NewModal("新建任务", body, ui.HA(10, 1,
		ui.Fill(),
		ui.NewButton("取消", ui.BtnGhost, func() { a.sh.PopOverlay() }),
		ui.NewButton("创建", ui.BtnPrimary, func() {
			if title.Value() == "" {
				a.toast("请输入标题", 1)
				return
			}
			go func() {
				_, err := a.cli.post("/api/tasks", map[string]any{"title": title.Value(), "due": due.Value()})
				if err != nil {
					a.toast(err.Error(), 2)
					return
				}
				a.sh.PopOverlay()
				a.toast("已创建", 0)
				a.loadFor("schedule")
			}()
		}),
	))
	m.OnClose = func() { a.sh.PopOverlay() }
	a.sh.PushOverlay(m)
}

// ---------- 问答 ----------

func (a *app) buildAsk() ui.Node {
	th := a.sh.Th
	head := ui.LB("知识库问答", 21, th.Text)
	msgs := []ui.Node{ui.L("向知识库提问，回答附引用来源", 12, th.Muted)}
	for i, s := range a.askLog {
		role := th.Text2
		if i%2 == 1 {
			role = th.Text
		}
		msgs = append(msgs, &ui.Box{BG: th.Panel2, Radius: 12, Pad: 12,
			Child: &ui.Label{Text: s, Style: ui.TextStyle{Size: 12.5, Color: role}, MaxLine: 8}})
	}
	input := a.askInput
	input.OnSubmit = func() { a.doAsk() }
	send := uiBtn("发送", ui.BtnPrimary, func() { a.doAsk() })
	return ui.V(16, head,
		&ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(10, msgs...)},
		ui.HA(10, 0.5, &ui.Stretch{Child: input}, send),
	)
}

func (a *app) doAsk() {
	q := a.askInput.Value()
	if q == "" {
		return
	}
	a.mu.Lock()
	a.askLog = append(a.askLog, q, "检索中…")
	a.askInput.Value_ = ""
	a.rebuild()
	a.mu.Unlock()
	go func() {
		d, err := a.cli.post("/api/ask", map[string]any{"q": q})
		a.mu.Lock()
		if err != nil {
			a.askLog = append(a.askLog[:len(a.askLog)-1], "出错："+err.Error())
		} else {
			ans := str(d["answer"])
			if ans == "" {
				ans = "（未配置问答模型，仅检索）"
			}
			a.askLog = append(a.askLog[:len(a.askLog)-1], ans)
		}
		a.rebuild()
		a.mu.Unlock()
	}()
}

// ---------- 设置 ----------

func (a *app) buildSettings() ui.Node {
	th := a.sh.Th
	head := ui.LB("设置", 21, th.Text)
	version := str(a.settings["version"])
	if version == "" {
		version = "…"
	}
	mode := str(a.settings["mode"])
	info := &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8,
		ui.L("服务信息", 14, th.Text2),
		kvRow(th, "版本", "v"+version),
		kvRow(th, "模式", mode),
		kvRow(th, "引擎", "自写渲染（无 WebView2）"),
	)}
	accCard := &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8,
		ui.L("我的 ima 账号", 14, th.Text2),
		ui.L("在 WebUI 或浏览器端绑定 Client ID / API Key", 12, th.Muted),
		ui.NewButton("打开 WebUI", ui.BtnSoft, func() { a.toast("浏览器访问："+a.eng.url, 0) }),
	)}
	peerCard := &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8,
		ui.L("我的节点", 14, th.Text2),
		ui.L("私钥不出本机 · 节点 ID 见 WebUI「节点」页", 12, th.Muted),
	)}
	return ui.V(16, head, info, accCard, peerCard)
}

func kvRow(th *ui.Theme, k, v string) ui.Node {
	return ui.HA(10, 0.5, ui.L(k, 12.5, th.Text2), ui.Fill(), ui.L(v, 12.5, th.Text))
}
