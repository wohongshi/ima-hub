// ImaHub 桌面版 —— 原生窗口 + 内嵌引擎（人人都是客户端，同时人人都是服务端）。
// Windows 上是一个 .exe 双击即用；其它平台退化为服务模式（自检/调试用）。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"imahub/internal/api"
)

// versionString 版本号（与服务端/MCP 自报一致）。
func versionString() string { return "v" + api.Version }

func main() {
	version := flag.Bool("version", false, "打印版本号")
	selftest := flag.Bool("selftest", false, "自检：起引擎 + HTTP 断言后退出（无窗口）")
	serveOnly := flag.Bool("serve-only", false, "不开窗口，仅后台服务")
	addr := flag.String("addr", ":8090", "监听地址（被占自动顺延）")
	dataDir := flag.String("data", "", "数据目录（默认 系统配置目录/ImaHub）")
	imaBase := flag.String("ima-base-url", "", "ima 开放平台地址（默认 https://ima.qq.com）")
	flag.Parse()

	if *version {
		fmt.Println("ImaHub Desktop", versionString())
		return
	}
	if *dataDir == "" {
		*dataDir = defaultDataDir()
	}
	eng, err := boot(*dataDir, *addr, *imaBase)
	if err != nil {
		log.Fatalf("启动失败: %v", err)
	}
	defer eng.stop()

	if *selftest {
		if err := eng.selftest(); err != nil {
			fmt.Println("SELFTEST FAIL:", err)
			os.Exit(1)
		}
		fmt.Println("SELFTEST PASS", eng.url)
		return
	}
	log.Printf("ImaHub Desktop %s · %s  (数据目录 %s)", versionString(), eng.url, *dataDir)
	if *serveOnly {
		eng.wait()
		return
	}
	runWindow(eng) // 按平台：Windows 原生窗口 / 其它平台服务模式
}

// defaultDataDir Windows=%APPDATA%\ImaHub，其它=系统配置目录/ImaHub（备份该目录=备份全部）。
func defaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return "./data"
	}
	return filepath.Join(base, "ImaHub")
}
