package main

// runPreview 预览验收：逐屏渲染 PNG（视觉回归台——改样式/界面后跑一遍看图）。

import (
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"imahub/internal/ui"
)

func runPreview(eng *engine, dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	reg, bold := ui.FontPaths()
	sh := ui.NewShell(1280, 800, ui.LoadFontSet(reg, bold), ui.DefaultTheme())
	app := newApp(eng, sh)
	app.build()

	shot := func(name string) {
		img := app.RenderLocked()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			log.Fatal(err)
		}
		png.Encode(f, img)
		f.Close()
		log.Println("渲染", name)
	}

	shot("1-login")
	app.accInput.Value_ = "preview"
	app.pwdInput.Value_ = "preview123"
	app.doLogin()
	time.Sleep(1500 * time.Millisecond)
	// 播演示数据（截图真实感；全部虚构演示内容）
	seedPreview(app)
	shot("2-home")
	for _, nav := range []string{"files", "peers", "schedule", "ask", "settings"} {
		app.setScreen(nav)
		time.Sleep(1200 * time.Millisecond)
		shot("3-" + nav)
	}
	// 生成邀请码模态样张
	app.setScreen("peers")
	time.Sleep(800 * time.Millisecond)
	app.openInvite()
	time.Sleep(300 * time.Millisecond)
	shot("4-invite-modal")
}

// seedPreview 播演示数据（虚构内容，只写进预览用的数据目录）。
func seedPreview(app *app) {
	today := time.Now().Format("2006-01-02")
	app.cli.post("/api/tasks", map[string]any{"title": "整理周会纪要", "note": "附议程与决议", "due": today})
	app.cli.post("/api/tasks", map[string]any{"title": "验收 P2P 共享联调", "due": today, "status": "doing"})
	app.cli.post("/api/tasks", map[string]any{"title": "更新部署文档", "status": "done"})
	app.cli.post("/api/me/ima", map[string]any{"name": "演示账号", "client_id": "demo_client", "api_key": "demo_key", "upload_kb_id": "kb1"})
}
