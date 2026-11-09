//go:build !windows

package main

// 非 Windows 平台：没有桌面窗口壳，以前台服务模式运行（自检 / 调试 / 服务器用）。

import "log"

func runWindow(eng *engine) {
	log.Printf("此平台无桌面窗口；服务模式运行中，WebUI: %s （Ctrl+C 退出）", eng.url)
	eng.wait()
}
