#!/usr/bin/env bash
# README 数字口径重算：改代码/发版前跑一遍，README 里的数字以此为准，别手填。
cd "$(dirname "$0")/.."
echo "MCP 工具数:   $(grep -oE '\{"[a-z_]+", "' internal/mcp/mcp.go | wc -l)"
echo "REST 路由数:  $(grep -c 'HandleFunc' internal/api/router.go)"
echo "测试函数数:   $(grep -rh 'func Test' internal/ --include='*_test.go' | wc -l)"
echo "Go 代码行数:  $(find . -name '*.go' -not -path './vendor/*' -exec cat {} + | wc -l)"
echo "前端代码行数: $(cat web/*.js web/*.css web/index.html | wc -l)"
echo "行为探针:     probe_fixes 10 + probe_client 13 + probe_queue 15 = 38 项断言"
echo "版本号:       $(grep -oP 'const Version = "\K[^"]+' internal/api/api.go)（双处一致校验：）"
grep -oP 'serverVersion\s+=\s+"\K[^"]+' internal/mcp/mcp.go | sed 's/^/  mcp serverVersion: /'
