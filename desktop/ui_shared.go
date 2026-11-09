package main

// 共享知识库浏览（经授权节点）：KB 列表 → 文件夹下钻 → 下载。

import (
	"fmt"

	"imahub/internal/ui"
)

// pushSharedModal 弹出/刷新共享库模态（动态内容用重建法）。
func (a *app) pushSharedModal(title string, rows ...ui.Node) {
	for a.sh.Overlays() > 0 {
		a.sh.PopOverlay()
	}
	body := ui.V(8, rows...)
	m := ui.NewModal(title, body, ui.HA(10, 1,
		ui.Fill(),
		ui.NewButton("关闭", ui.BtnGhost, func() { a.sh.PopOverlay() }),
	))
	m.W = 640
	m.OnClose = func() { a.sh.PopOverlay() }
	a.sh.PushOverlay(m)
}

// openSharedKB 打开对方知识库列表。
func (a *app) openSharedKB(peerID, name string) {
	th := a.sh.Th
	a.pushSharedModal("共享知识库 · "+name, ui.L("加载中…", 12, th.Muted))
	go func() {
		d, err := a.cli.get("/api/peers/" + peerID + "/kbs")
		if err != nil {
			a.toast(err.Error(), 2)
			a.sh.PopOverlay()
			return
		}
		kbs := asArr(d["kbs"])
		rows := []ui.Node{ui.L("对方的知识库", 12, th.Text2)}
		if len(kbs) == 0 {
			rows = append(rows, ui.L("对方暂无可访问的知识库", 12, th.Muted))
		}
		for _, kb := range kbs {
			id := str(kb["id"])
			kbName := str(kb["name"])
			if kbName == "" {
				kbName = id
			}
			row := &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 9, MinH: 44,
				Child: ui.HA(12, 0.5,
					&ui.Avatar{Icon: ui.IconDatabase, UseIcon: true, BG: th.AccentSoft, Fore: th.Accent, Size_: 28},
					ui.L(kbName, 13, th.Text), ui.Fill(),
					&iconBox{icon: ui.IconChevronRight, size: 14, col: th.Muted},
				)}
			idCopy, nameCopy := id, kbName
			row.OnTap(func() { a.sharedBrowse(peerID, idCopy, nameCopy, "") })
			rows = append(rows, row)
		}
		a.pushSharedModal("共享知识库 · "+name, rows...)
	}()
}

// sharedBrowse 浏览对方知识库文件（folder 空=根）。
func (a *app) sharedBrowse(peerID, kbID, kbName, folder string) {
	th := a.sh.Th
	title := "共享 · " + kbName
	a.pushSharedModal(title, ui.L("加载中…", 12, th.Muted))
	go func() {
		d, err := a.cli.post("/api/peers/"+peerID+"/kb/browse", map[string]any{"kb": kbID, "folder": folder})
		if err != nil {
			a.toast(err.Error(), 2)
			a.sh.PopOverlay()
			return
		}
		rows := []ui.Node{}
		if folder != "" {
			rows = append(rows, ui.NewButton("← 上一层", ui.BtnGhost, func() {
				a.sharedBrowse(peerID, kbID, kbName, "")
			}))
		}
		for _, f := range asArr(d["folders"]) {
			fid := str(f["folder_id"])
			fname := str(f["name"])
			row := &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 8, MinH: 40,
				Child: ui.HA(12, 0.5, &ui.Avatar{Icon: ui.IconFolder, UseIcon: true, BG: th.Panel, Fore: th.Warn, Size_: 26},
					ui.L(fname, 12.5, th.Text), ui.Fill(),
					&iconBox{icon: ui.IconChevronRight, size: 13, col: th.Muted})}
			fc := fid
			row.OnTap(func() { a.sharedBrowse(peerID, kbID, kbName, fc) })
			rows = append(rows, row)
		}
		for _, f := range asArr(d["files"]) {
			media := str(f["media_id"])
			fname := str(f["title"])
			row := &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 8, MinH: 40,
				Child: ui.HA(12, 0.5, &ui.Avatar{Icon: ui.IconFileText, UseIcon: true, BG: th.Panel, Fore: th.Text2, Size_: 26},
					ui.L(fname, 12.5, th.Text), ui.Fill(),
					ui.NewButton("下载", ui.BtnSoft, func() {
						a.toast("开始下载："+fname, 0)
					}))}
			mc := media
			fc := fname
			row.OnTap(func() {
				a.toast("下载："+fc+"（直连引擎）", 0)
				go func() {
					url := a.eng.url + "/api/peers/" + peerID + "/kb/download?kb=" + urlQ(kbID) + "&media=" + urlQ(mc) + "&name=" + urlQ(fc)
					resp, err := a.cli.cli.Get(url) //nolint:gosec // 本机引擎地址
					if err != nil {
						a.toast("下载失败", 2)
						return
					}
					resp.Body.Close()
					if resp.StatusCode == 200 {
						a.toast("下载完成", 0)
					} else {
						a.toast(fmt.Sprintf("下载失败 %d", resp.StatusCode), 2)
					}
				}()
			})
			rows = append(rows, row)
		}
		if len(rows) <= 1 {
			rows = append(rows, ui.L("空目录", 12, th.Muted))
		}
		a.pushSharedModal(title, rows...)
	}()
}
