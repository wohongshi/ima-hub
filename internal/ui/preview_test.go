package ui

import (
	"image/png"
	"os"
	"testing"
	"time"
)

// TestDemoRender 控件全家福渲染到 PNG（视觉验收台：改样式后跑一遍看图）。
func TestDemoRender(t *testing.T) {
	t0 := time.Now()
	reg, bold := FontPaths()
	fs := LoadFontSet(reg, bold)
	t1 := time.Now()
	th := DefaultTheme()
	sh := NewShell(1280, 800, fs, th)

	// —— 左侧导航 ——
	sidebar := &Box{
		BG: th.Panel, Pad: 14,
		Child: V(4,
			HA(10, 0.5,
				&Avatar{Text: "IH", BG: th.Accent, Fore: RGB(255, 255, 255), Size_: 32},
				VA(2, 0, LB("ImaHub", 15, th.Text), L("P2P 节点", 11, th.Muted)),
			),
			Fixed(0, 14),
			navItem(th, IconHome, "首页", true),
			navItem(th, IconDatabase, "知识库", false),
			navItem(th, IconFolder, "文件", false),
			navItem(th, IconUsers, "节点", false),
			navItem(th, IconCalendar, "日程", false),
			navItem(th, IconMessage, "问答", false),
			navItem(th, IconSettings, "设置", false),
			Fill(),
			HA(8, 0.5, &Avatar{Icon: IconServer, UseIcon: true, BG: th.Panel2, Fore: th.Text2, Size_: 28},
				VA(1, 0, L("armbian", 12, th.Text), L("在线 · 节点 3", 10.5, th.OK))),
		),
	}

	// —— 主区：顶栏 + 统计卡 + 列表 + 模态样张 ——
	stats := Col(14,
		statCard(th, IconDatabase, "知识库", "3 个 · 128 文件", th.Accent, th.AccentSoft),
		statCard(th, IconUsers, "节点", "3 在线 / 5", th.OK, th.OKSoft),
		statCard(th, IconUpload, "上传队列", "2 待传", th.Warn, th.WarnSoft),
		statCard(th, IconShield, "共享授权", "4 项生效", th.Danger, th.DangerSoft),
	)

	rows := V(8,
		L("最近文件", 14, th.Text2),
		fileRow(th, IconFileText, "数学复习提纲.pdf", "2.3 MB · 2 小时前", "已共享"),
		fileRow(th, IconFile, "周会纪要.docx", "180 KB · 昨天", ""),
		fileRow(th, IconPlay, "demo.mp3", "4.1 MB · 昨天", ""),
		HA(10, 0.5,
			Btn("主要操作", func() {}),
			&Button{Text: "次要", Style: BtnGhost, onClick: func() {}},
			&Button{Text: "危险", Style: BtnDanger, onClick: func() {}},
			&Button{Text: "柔和", Style: BtnSoft, onClick: func() {}},
			&Button{Text: "禁用", Disable: true},
			&Spinner{Size_: 22},
			&Progress{Ratio: 0.62, Color_: th.Accent},
		),
		HA(10, 0.5,
			&Checkbox{Text: "自动同步", Checked: true},
			&Checkbox{Text: "只读模式"},
			NewBadge("待处理", th.WarnSoft, th.Warn),
			NewBadge("已批准", th.OKSoft, th.OK),
			NewBadge("已拒绝", th.DangerSoft, th.Danger),
		),
		HA(10, 0.5,
			&Stretch{Child: Input("搜索知识库、文件、节点…")},
			Btn("搜索", func() {}),
		),
		L("这是一段自动折行的说明文字：ImaHub 自写界面引擎不依赖 WebView2，所有控件由 2x 抗锯齿软渲染直接绘制，圆角、文字与图标皆矢量。", 12.5, th.Text2),
	)

	content := V(18,
		HA(8, 0.5, LB("仪表盘", 21, th.Text), Fill(), &Button{Text: "生成邀请码", HasIcon: true, Icon: IconKey, Style: BtnPrimary, onClick: func() {}}),
		L("欢迎回来，这里是 ImaHub 桌面版（自写渲染引擎）", 13, th.Muted),
		stats,
		&Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: rows},
	)

	main := &Box{Pad: 26, Child: content}
	root := H(0, sidebar, main)

	// 尺寸：侧栏固定宽
	sidebar.MinW = 210
	sh.Root = root
	t2 := time.Now()
	img := sh.Render()
	t3 := time.Now()
	img2 := sh.Render() // 二次渲染（字面缓存生效后的真实帧成本）
	t4 := time.Now()

	// 布局帧取证（溢出排查）
	dump := func(name string, n Node) {
		f := n.Frame()
		t.Logf("帧 %s: x=%.1f y=%.1f w=%.1f h=%.1f (右缘 %.1f)", name, f.X, f.Y, f.W, f.H, f.X+f.W)
	}
	dump("root", root)
	dump("sidebar", sidebar)
	dump("main", main)
	dump("content", content)
	dump("stats", stats)
	for i, ch := range stats.Children {
		dump("stat"+itoa(i), ch)
	}
	dump("rowsCard", rows)

	os.MkdirAll("/root/.hermes/cache/scratch", 0o755)
	f, err := os.Create("/root/.hermes/cache/scratch/ui_demo.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
	_ = img2
	t.Logf("字体解析 %v · 首帧 %v · 次帧 %v", t1.Sub(t0), t3.Sub(t2), t4.Sub(t3))
}

func navItem(th *Theme, ic Icon, text string, active bool) Node {
	bg := Color{}
	fg := th.Text2
	if active {
		bg = th.AccentSoft
		fg = th.Accent
	}
	return &Box{BG: bg, Radius: 9, PadX: 10, PadY: 8, MinH: 38,
		Child: HA(10, 0.5, &iconNode{icon: ic, size: 17, col: fg}, L(text, 13, fg))}
}

func statCard(th *Theme, ic Icon, title, sub string, fg, soft Color) Node {
	return &Box{BG: th.Panel, Radius: th.RadM, Pad: 16, Shadow: true, MinH: 92,
		Child: VA(6, 0,
			HA(8, 0.5, &Avatar{Icon: ic, UseIcon: true, BG: soft, Fore: fg, Size_: 30}, LB(title, 13, th.Text2)),
			L(sub, 17, th.Text),
		)}
}

func fileRow(th *Theme, ic Icon, name, meta, tag string) Node {
	right := Node(Fixed(0, 0))
	if tag != "" {
		right = NewBadge(tag, th.AccentSoft, th.Accent)
	}
	return &Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 9, MinH: 46,
		Child: HA(12, 0.5,
			&Avatar{Icon: ic, UseIcon: true, BG: th.Panel, Fore: th.Text2, Size_: 30},
			VA(2, 0, L(name, 13, th.Text), L(meta, 11, th.Muted)),
			Fill(), right,
			&iconNode{icon: IconChevronRight, size: 15, col: th.Muted},
		)}
}

// iconNode 裸图标节点（布局用）。
type iconNode struct {
	icon  Icon
	size  float64
	col   Color
	w, h  float64
	frame Rect
}

func (i *iconNode) Measure(_ *Ctx, _ Size) Size { return Size{i.size, i.size} }
func (i *iconNode) SetFrame(r Rect)             { i.frame = r }
func (i *iconNode) Frame() Rect                 { return i.frame }
func (i *iconNode) Paint(cv *Canvas) {
	cv.DrawIcon(i.icon, i.frame.X, i.frame.Y, i.size, i.col)
}
