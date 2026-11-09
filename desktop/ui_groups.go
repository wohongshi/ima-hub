package main

// 桌面端群组界面：群组卡（建/拉人/退组/解散）+ 任务/日程共享到群 + 群组徽章。

import (
	"strings"

	"imahub/internal/ui"
)

// openNewGroup 建群弹窗（名称 + 成员勾选）。
func (a *app) openNewGroup() {
	th := a.sh.Th
	name := &ui.TextField{Placeholder: "群名（如 学习小组）"}
	checks := []*ui.Checkbox{}
	rows := []ui.Node{ui.L("群名", 12, th.Text2), name, ui.L("成员", 12, th.Text2)}
	if len(a.peers) == 0 {
		rows = append(rows, ui.L("暂无可选节点（先在节点页互联）", 12, th.Muted))
	}
	for _, p := range a.peers {
		nm := str(p["nickname"])
		if nm == "" {
			nm = shortID(str(p["id"]))
		}
		cb := &ui.Checkbox{Text: nm}
		checks = append(checks, cb)
		rows = append(rows, cb)
	}
	m := ui.NewModal("新建群组", ui.V(10, rows...), ui.HA(10, 1,
		ui.Fill(),
		ui.NewButton("取消", ui.BtnGhost, func() { a.sh.PopOverlay() }),
		ui.NewButton("创建", ui.BtnPrimary, func() {
			if name.Value() == "" {
				a.toast("请输入群名", 1)
				return
			}
			members := []string{}
			for i, cb := range checks {
				if cb.Checked && i < len(a.peers) {
					members = append(members, str(a.peers[i]["id"]))
				}
			}
			go func() {
				_, err := a.cli.post("/api/groups", map[string]any{"name": name.Value(), "members": members})
				if err != nil {
					a.toast(err.Error(), 2)
					return
				}
				a.sh.PopOverlay()
				a.toast("群组已创建", 0)
				a.loadFor("peers")
			}()
		}),
	))
	m.OnClose = func() { a.sh.PopOverlay() }
	a.sh.PushOverlay(m)
}

// groupPickRows 新建任务/日程弹窗里的「共享到」选项行（单选语义）。
func (a *app) groupPickRows(sel *string) []ui.Node {
	th := a.sh.Th
	rows := []ui.Node{ui.L("共享到", 12, th.Text2)}
	pick := func(label, gid string) ui.Node {
		b := ui.NewButton(label, ui.BtnGhost, func() {
			*sel = gid
			a.toast("已选："+label, 0)
		})
		return b
	}
	rows = append(rows, ui.HA(8, 0.5, pick("不共享", "")))
	for _, g := range a.groups {
		gid := str(g["id"])
		gname := str(g["name"])
		rows = append(rows, pick("→ "+gname, gid))
	}
	return rows
}

// buildGroupsCard 节点屏的群组卡。
func (a *app) buildGroupsCard() ui.Node {
	th := a.sh.Th
	rows := []ui.Node{ui.HA(8, 0.5, ui.L("我的群组", 14, th.Text2), ui.Fill(),
		ui.NewButton("建群", ui.BtnSoft, func() { a.openNewGroup() }))}
	if len(a.groups) == 0 {
		rows = append(rows, ui.L("暂无群组——建群后任务/日程一键共享给成员", 12, th.Muted))
	}
	for _, g := range a.groups {
		gid := str(g["id"])
		gname := str(g["name"])
		members := asArr(g["members"])
		names := []string{}
		for _, m := range members {
			names = append(names, str(m["nick"]))
		}
		leaveBtn := ui.NewButton("退组", ui.BtnGhost, func() {
			go func() {
				a.cli.post("/api/groups/"+gid+"/leave", nil)
				a.toast("已退组", 0)
				a.loadFor("peers")
			}()
		})
		dissolveBtn := ui.NewButton("解散", ui.BtnDanger, func() {
			go func() {
				a.cli.do("DELETE", "/api/groups/"+gid, nil)
				a.toast("已解散", 0)
				a.loadFor("peers")
			}()
		})
		rows = append(rows, &ui.Box{BG: th.Panel2, Radius: 10, PadX: 12, PadY: 10, MinH: 52,
			Child: ui.HA(12, 0.5,
				&ui.Avatar{Icon: ui.IconUsers, UseIcon: true, BG: th.AccentSoft, Fore: th.Accent, Size_: 30},
				ui.VA(2, 0,
					ui.L(gname, 13.5, th.Text),
					ui.L(strings.Join(names, " · "), 11, th.Muted)),
				ui.Fill(), leaveBtn, dissolveBtn,
			)})
	}
	return &ui.Box{BG: th.Panel, Radius: th.RadM, Pad: 18, Shadow: true, Child: ui.V(8, rows...)}
}

// groupBadgeOf 任务/日程行的群组徽章（无群组返回占位空白）。
func (a *app) groupBadgeOf(gid string) ui.Node {
	th := a.sh.Th
	if gid == "" {
		return ui.Fixed(0, 0)
	}
	name := gid
	for _, g := range a.groups {
		if str(g["id"]) == gid {
			name = str(g["name"])
			break
		}
	}
	return ui.NewBadge(name, th.AccentSoft, th.Accent)
}
