package main

// device.go — 桌面端免登录（单人模式「记住本机」）。
// 登录成功后签发设备 token 存 dataDir/desktop_device.json（0600），下次启动直登。

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func (a *app) deviceFile() string {
	return filepath.Join(a.eng.dataDir, "desktop_device.json")
}

// tryDeviceLogin 启动时用本地 token 直登；失效则清文件回登录页。
func (a *app) tryDeviceLogin() bool {
	raw, err := os.ReadFile(a.deviceFile())
	if err != nil {
		return false
	}
	var d struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &d) != nil || d.Token == "" {
		return false
	}
	if _, err := a.cli.post("/api/login", map[string]any{"device_token": d.Token}); err != nil {
		_ = os.Remove(a.deviceFile())
		return false
	}
	return true
}

// issueDeviceToken 登录/初始化成功后签发并保存本机免登录 token（失败不挡使用）。
func (a *app) issueDeviceToken() {
	d, err := a.cli.post("/api/me/devices", map[string]any{"name": "桌面版"})
	if err != nil {
		return
	}
	tok, _ := d["token"].(string)
	id, _ := d["id"].(string)
	if tok == "" {
		return
	}
	b, _ := json.Marshal(map[string]string{"id": id, "token": tok})
	_ = os.WriteFile(a.deviceFile(), b, 0o600)
}
