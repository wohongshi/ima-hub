//go:build windows

package main

// Windows 原生窗口壳：WebView2 承载完整 WebUI，同一套引擎对外仍是全功能节点。
// 依赖 Microsoft Edge WebView2 运行时（Win11 自带；Win10 一般由更新带上）。

import (
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func runWindow(eng *engine) {
	err := wails.Run(&options.App{
		Title:            "ImaHub",
		Width:            1280,
		Height:           820,
		MinWidth:         980,
		MinHeight:        640,
		BackgroundColour: options.NewRGB(248, 249, 252),
		AssetServer: &assetserver.Options{
			Handler: eng.mux, // 窗口里就是完整 WebUI + API（同一套路由）
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.hongshi.imahub-desktop",
		},
	})
	if err != nil {
		log.Fatalf("窗口启动失败: %v", err)
	}
}
