package main

// 桌面应用界面：自写引擎控件直绘（不走 WebUI）。数据经 apiClient 访问本机引擎。

import (
	"fmt"
	"image"
	"strconv"
	"sync"

	"imahub/internal/ui"
)

type app struct {
	eng *engine
	sh  *ui.Shell
	cli *apiClient
	mu  sync.Mutex

	// 路由与会话
	screen string // login | main
	nav    string // home | files | peers | schedule | ask | settings
	user   string

	// 数据
	me        map[string]any
	dashboard map[string]any
	kbs       []map[string]any
	files     []map[string]any
	fileKB    string
	filePath  string
	peers     []map[string]any
	access    []map[string]any
	tasks     []map[string]any
	events    []map[string]any
	settings  map[string]any

	// 表单
	accInput *ui.TextField
	pwdInput *ui.TextField
	kbQuery  string
	askInput *ui.TextField
	askLog   []string
}

func newApp(eng *engine, sh *ui.Shell) *app {
	return &app{
		eng:    eng,
		sh:     sh,
		cli:    newAPIClient(eng.url),
		screen: "login",
		nav:    "home",
	}
}

// RenderLocked 持锁渲染一帧（平台层 paint 调用；与异步构树互斥）。
func (a *app) RenderLocked() *image.RGBA {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sh.Render()
}

// rebuild 构树（持锁调用者或内部加锁）。
func (a *app) rebuild() {
	if a.screen == "login" {
		a.sh.Root = a.buildLogin()
		return
	}
	a.sh.Root = a.buildMain()
	a.sh.Dirty = true
}

// build 首次构树 + 健康探测（未初始化则引导 setup）。
func (a *app) build() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.accInput = &ui.TextField{Placeholder: "用户名"}
	a.pwdInput = &ui.TextField{Placeholder: "密码", Password: true}
	a.askInput = &ui.TextField{Placeholder: "向知识库提问…"}
	a.rebuild()
}

// toast 提示（线程安全）。
func (a *app) toast(text string, kind int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sh.Toast(text, kind)
}

// setScreen 切换主屏并构树。
func (a *app) setScreen(nav string) {
	a.mu.Lock()
	a.nav = nav
	a.rebuild()
	a.mu.Unlock()
	go a.loadFor(nav)
}

// ---------- 登录/初始化 ----------

func (a *app) buildLogin() ui.Node {
	th := a.sh.Th
	title := ui.LB("ImaHub", 30, th.Text)
	sub := ui.L("腾讯 ima 开放平台 · 团队网关", 13, th.Muted)
	acc := a.accInput
	pwd := a.pwdInput
	pwd.OnSubmit = func() { a.doLogin() }
	loginBtn := ui.Btn("登  录", func() { a.doLogin() })
	body := ui.V(14,
		ui.L("账号", 12, th.Text2), acc,
		ui.L("密码", 12, th.Text2), pwd,
		ui.Fixed(0, 6),
		loginBtn,
		ui.L("首次使用：第一个账号即为管理员", 11.5, th.Muted),
	)
	card := &ui.Box{BG: th.Panel, Radius: th.RadL, PadX: 34, PadY: 32, Shadow: true,
		Child: ui.V(10, title, sub, ui.Fixed(0, 10), body)}
	wrap := &ui.Box{Pad: 20, Child: ui.V(0, ui.Fill(), ui.HA(0, 0.5, ui.Fill(), card, ui.Fill()), ui.Fill())}
	return wrap
}

func (a *app) doLogin() {
	user := a.accInput.Value()
	pass := a.pwdInput.Value()
	if user == "" || pass == "" {
		a.toast("请输入账号密码", 1)
		return
	}
	go func() {
		_, err := a.cli.post("/api/login", map[string]any{"username": user, "password": pass})
		if err != nil {
			// 未初始化则走 setup
			if _, err2 := a.cli.get("/api/setup/status"); err2 == nil {
				_, err = a.cli.post("/api/setup", map[string]any{"username": user, "password": pass})
			}
		}
		if err != nil {
			a.toast(err.Error(), 2)
			return
		}
		a.mu.Lock()
		a.user = user
		a.screen = "main"
		a.rebuild()
		a.mu.Unlock()
		a.toast("欢迎回来，"+user, 0)
		go a.loadFor("home")
	}()
}

// ---------- 主框架 ----------

func (a *app) buildMain() ui.Node {
	th := a.sh.Th
	nav := func(ic ui.Icon, id, label string) ui.Node {
		active := a.nav == id
		bg := ui.Color{}
		fg := th.Text2
		if active {
			bg = th.AccentSoft
			fg = th.Accent
		}
		b := &ui.Box{BG: bg, Radius: 9, PadX: 10, PadY: 8, MinH: 38,
			Child: ui.HA(10, 0.5, &iconBox{icon: ic, size: 17, col: fg}, ui.L(label, 13, fg))}
		idCopy := id
		b.OnTap(func() { a.setScreen(idCopy) })
		return b
	}
	sidebar := &ui.Box{BG: th.Panel, Pad: 14, MinW: 208, Child: ui.V(4,
		ui.HA(10, 0.5,
			&ui.Avatar{Text: "IH", BG: th.Accent, Fore: ui.RGB(255, 255, 255), Size_: 32},
			ui.VA(2, 0, ui.LB("ImaHub", 15, th.Text), ui.L("P2P 节点", 11, th.Muted)),
		),
		ui.Fixed(0, 14),
		nav(ui.IconHome, "home", "首页"),
		nav(ui.IconDatabase, "files", "知识库"),
		nav(ui.IconUsers, "peers", "节点"),
		nav(ui.IconCalendar, "schedule", "日程"),
		nav(ui.IconMessage, "ask", "问答"),
		nav(ui.IconSettings, "settings", "设置"),
		ui.Fill(),
		&ui.Divider{Color: th.Border},
		ui.Fixed(0, 8),
		ui.HA(8, 0.5, &ui.Avatar{Icon: ui.IconServer, UseIcon: true, BG: th.Panel2, Fore: th.Text2, Size_: 30},
			ui.VA(1, 0, ui.L(a.user, 12, th.Text), ui.L("节点在线", 10.5, th.OK))),
	)}

	var content ui.Node
	switch a.nav {
	case "files":
		content = a.buildFiles()
	case "peers":
		content = a.buildPeers()
	case "schedule":
		content = a.buildSchedule()
	case "ask":
		content = a.buildAsk()
	case "settings":
		content = a.buildSettings()
	default:
		content = a.buildHome()
	}
	main := &ui.Stretch{Child: &ui.ScrollView{Pad: 0, Child: &ui.Box{Pad: 26, Child: content}}}
	return ui.H(0, sidebar, main)
}

// ---------- 首页 ----------

func (a *app) buildHome() ui.Node {
	th := a.sh.Th
	d := a.dashboard
	get := func(m map[string]any, path ...string) any {
		cur := any(m)
		for _, k := range path {
			mm, ok := cur.(map[string]any)
			if !ok {
				return nil
			}
			cur = mm[k]
		}
		return cur
	}
	num := func(v any, fallback string) string {
		switch t := v.(type) {
		case float64:
			return strconv.FormatInt(int64(t), 10)
		case string:
			if t != "" {
				return t
			}
		}
		return fallback
	}
	stat := func(ic ui.Icon, title, sub string, fg, soft ui.Color) ui.Node {
		return &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 16, Shadow: true, MinH: 88,
			Child: ui.VA(6, 0,
				ui.HA(8, 0.5, &ui.Avatar{Icon: ic, UseIcon: true, BG: soft, Fore: fg, Size_: 30}, ui.LB(title, 12.5, th.Text2)),
				ui.L(sub, 17, th.Text),
			)}
	}
	kbCount := "—"
	if v := get(d, "stats", "kbs"); v != nil {
		kbCount = num(v, "—") + " 个"
	}
	fileCount := "—"
	if v := get(d, "stats", "files"); v != nil {
		fileCount = num(v, "—") + " 文件"
	}
	stats := ui.Col(14,
		stat(ui.IconDatabase, "知识库", kbCount+" · "+fileCount, th.Accent, th.AccentSoft),
		stat(ui.IconUsers, "节点", fmt.Sprintf("%d 个", len(a.peers)), th.OK, th.OKSoft),
		stat(ui.IconUpload, "上传队列", "查看队列", th.Warn, th.WarnSoft),
		stat(ui.IconShield, "共享授权", fmt.Sprintf("%d 项", len(a.access)), th.Danger, th.DangerSoft),
	)

	// 待办任务
	taskRows := []ui.Node{ui.L("今日待办", 14, th.Text2)}
	if len(a.tasks) == 0 {
		taskRows = append(taskRows, ui.L("暂无任务", 12.5, th.Muted))
	}
	for i, t := range a.tasks {
		if i >= 5 {
			break
		}
		title, _ := t["title"].(string)
		status, _ := t["status"].(string)
		done := status == "done"
		fg := th.Text
		if done {
			fg = th.Muted
		}
		row := &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 9, MinH: 42,
			Child: ui.HA(10, 0.5,
				&ui.Checkbox{Text: "", Checked: done},
				ui.L(title, 13, fg),
				ui.Fill(),
				ui.L(statusLabel(status), 11, th.Muted),
			)}
		taskRows = append(taskRows, row)
	}
	taskCard := &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8, taskRows...)}

	// 最近文件
	fileRows := []ui.Node{ui.L("最近文件", 14, th.Text2)}
	if len(a.files) == 0 {
		fileRows = append(fileRows, ui.L("暂无文件", 12.5, th.Muted))
	}
	for i, f := range a.files {
		if i >= 6 {
			break
		}
		title, _ := f["title"].(string)
		fileRows = append(fileRows, fileRowNode(th, title, "知识库文件"))
	}
	fileCard := &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8, fileRows...)}

	head := ui.HA(8, 0.5,
		ui.LB("仪表盘", 21, th.Text), ui.Fill(),
		ui.NewButton("生成邀请码", ui.BtnSoft, func() { a.openInvite() }),
	)
	sub := ui.L("人人都是客户端，同时人人都是服务端", 12.5, th.Muted)
	return ui.V(18, head, sub, stats, taskCard, fileCard)
}

func statusLabel(s string) string {
	switch s {
	case "done":
		return "已完成"
	case "doing":
		return "进行中"
	}
	return "待办"
}

// iconBox 裸图标节点。
type iconBox struct {
	icon  ui.Icon
	size  float64
	col   ui.Color
	frame ui.Rect
}

func (i *iconBox) Measure(_ *ui.Ctx, _ ui.Size) ui.Size { return ui.Size{W: i.size, H: i.size} }
func (i *iconBox) SetFrame(r ui.Rect)                   { i.frame = r }
func (i *iconBox) Frame() ui.Rect                       { return i.frame }
func (i *iconBox) Paint(cv *ui.Canvas) {
	cv.DrawIcon(i.icon, i.frame.X, i.frame.Y, i.size, i.col)
}

// fileRowNode 文件行。
func fileRowNode(th *ui.Theme, name, meta string) *ui.Box {
	return &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 9, MinH: 44,
		Child: ui.HA(12, 0.5,
			&ui.Avatar{Icon: ui.IconFileText, UseIcon: true, BG: th.Panel, Fore: th.Text2, Size_: 30},
			ui.VA(2, 0, ui.L(name, 13, th.Text), ui.L(meta, 11, th.Muted)),
			ui.Fill(),
			&iconBox{icon: ui.IconChevronRight, size: 15, col: th.Muted},
		)}
}
