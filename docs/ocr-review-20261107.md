# ocr 代码评审处置表（2026-11-07）

工具：open-code-review v1.12.9（`ocr review --commit HEAD`，scratch git 快照 32 文件 / 10800 行）。
结果：95 条意见（critical 1 / high 7 / medium 51 / low 36；bug 64、security 18、maintainability 8、performance 3、style 2）。
原始意见全文：`ocr-findings.json`（本目录）；评审原始输出 `/tmp/ocr_imahub.json`。

## 已修（8 条，全部逐条对源码核实属实，修复后行为级探针验证）

| # | 级别 | 位置 | 问题 | 修法 | 验证 |
|---|------|------|------|------|------|
| 1 | critical/security | `internal/mcp/mcp.go` uploadFile | `file_path` 直通 `os.Open`，任何 MCP token 持有者可读服务端任意文件（含 data.json 凭证）并经知识库外传 | `file_path` 限定在 `data/mcp-uploads/` 目录内（Abs+EvalSymlinks 前缀校验），工具描述同步更新 | 探针：`/etc/hostname`、`data.json` 均被拒；目录内文件正常放行 |
| 2 | high/bug | `internal/store/store.go` 全部 6 个 Update* | 闭包直接改活元素，fn 报错时不回滚，改动随下次 save 持久化（半途提交） | 改为副本修改-成功后写回（含 UpdatedAt 也打在副本上） | 探针：`{password,role:"root"}` 非法请求后旧口令仍可登录、新口令不生效 |
| 3 | high/security | `internal/api/ima_handlers.go` inline 下载 | `inline=1` 按扩展名直出 `text/html`/`image/svg+xml`，同源存储型 XSS | MIME 白名单（图片/PDF/纯文本），其余降级 `text/plain` + `CSP: sandbox` + 全程 `X-Content-Type-Options: nosniff` | 探针：上传 HTML 内联下载 → text/plain + nosniff + sandbox 三头齐 |
| 4 | high/security | `internal/auth/auth.go` VerifyPassword | 畸形哈希记录（空哈希）时 pbkdf2(0) 与空比较恒等通过 → 任意口令登录绕过 | 校验解码后哈希长度 16–64，越界直接拒绝 | 单测 `auth_test.go`（畸形 6 例 + 正常往返）全过 |
| 5 | high/bug | `web/api.js` | 业务 code 401（ima 上游透传）也被当会话过期踢回登录页；登录页错口令也触发 onUnauthorized | 仅 HTTP 401 视为会话失效，且 /api/login、/api/setup 除外 | 代码审 + 冒烟（错口令 401 语义探针过） |
| 6 | high/bug | `web/view-home.js` ×6 | 数据分支丢 `head`，卡片标题消失（日程面板连「添加日程」按钮都没了） | 六处 `box.innerHTML` 前拼回 head（379/607/752/803/864/957） | node --check + diff 逐处核对 |
| 7 | high/bug | `web/view-notes.js` | 行属性 `data-nb-idx` 与选择器 `[data-nb]` 不匹配，笔记本点击全死 | 选择器改 `.kb-item`（事件分支读 `__all`/`data-nb-idx` 不变） | node --check + 逻辑核对 |
| 8 | high/security | `web/view-files.js` | `onerror=` 属性拼串 XSS（escapeHtml 的 `&#39;` 会被属性解析还原） | 改 `data-fallback` 属性 + addEventListener 错误兜底（值仅作 textContent） | node --check + 探针栈上传流走通 |

## 顺手核过、判定非缺陷

- `view-home.js:461` 公告条 `box.innerHTML = html`：该面板本来就没有 card-head（公告条非卡片布局），不改。

## 待处理（medium 51 / low 36）

逐条在 `ocr-findings.json`（含 severity/category/path/行号/内容），按同规程「先核实再改、处置落表」分批进行。重点先看：
- security 类剩余 ~10 条（medium/low 里的输入校验、权限边界）
- bug 类里影响数据正确性的（51 medium 里多数标 bug）

## 回归记录

- `go vet ./...` 干净、`go build ./...` OK、`go test ./...` 全过（含新 auth_test）
- `node --check` 改动的 5 个 JS 全过
- 行为探针 `probe_fixes.sh`（mock 栈）**ALL_GREEN 10/10**
- `scripts/smoke.sh` 全接口冒烟 SMOKE_EXIT=0（23 个成功响应，无 401/500）
- 已重新构建并重启 `imahub.service`，healthz OK
