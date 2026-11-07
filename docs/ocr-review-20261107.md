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

## medium 51 条处置（第二批）

- **上轮类修复顺带解决（6）**：[0][18][19] store 三个 Update* 半途提交（副本提交类修复）、[1][6] 闭包持指针越锁访问（副本后指针指向安全快照）、[29] 业务 401 误踢（api.js 已改为仅 HTTP 401）。
- **本轮已修（28）**：[3] logout 落盘失败不再静默、[4] ReadHeaderTimeout/IdleTimeout 防 Slowloris、[5] 优雅退出 Shutdown、[7] /api/setup 原子初始化防双管理员、[8] 会话创建失败不再假 200、[9] Cookie Secure 随 TLS（明文 HTTP 不破坏）、[16] 解压预算写入中即拦截、[17] COS PUT 兜底超时、[20] 上传体 550MB 上限、[21] zip 条目名净化防穿越、[22] 打包截断有说明文件、[23] 流拷贝错误回传、[24] 账号改名查重、[25][30] 改密/管理员重置后吊销旧会话、[26] MCP 尺寸重复计、[27] MCP 读错不再冒充完整、[28] FindUserByID 判空、[33] items 数量上限、[34] 轮询跳过未配默认库账号、[35] 下载流中断记日志、[36] 聚合笔记本翻页拉全量、[37] 压缩包同名闸（跳过+清单，force 覆盖）、[38] 问答检索失败如实报错、[39] 引用数按 TopK 收敛、[40] 二进制魔数识别（PDF/图片/压缩包）、[41] list_note 游标透传、[42] 笔记本时间字段 FlexInt。
- **设计如此（1）**：[2] saveLocked 同步写盘持锁——团队工具低写入量，换异步落盘增加丢数据面，不值。
- **web 16 条全部属实已修**（子代理逐条核实，语法复查通过）：app.js 按钮文字覆盖/needSetup 未复位/死代码图标赋值；api.js XHR 401 不跳登录/xhr.timeout 未设（30 分钟兜底）；ui.js Esc 监听器泄漏与叠加模态全关（modalStack 栈顶才关、close() 统一解绑）/confirmDialog 参数改名 messageHtml 澄清转义责任/嵌套三元改 if-else/未用参数清理；style.css .home-page 死块清理/--card 未声明改 var(--panel)（暗色模式白洞修复）/登录容器 height:100vh 可滚；view-files.js kbLoading 永久 true（finally 复位）/在途导航与搜索被吞（listPending/kbPending 补跑 + 过期响应快照丢弃）/切库清空多选/folderOptions 预选修正/详情弹窗死分支删除/import-urls 结果按对象映射计数/双击重复进目录；view-notes.js pickPage 无游标即末页（配合后端已补 next_cursor）/笔记内容串台（noteOpenSeq 代数计数）/返回列表按钮可达；view-home.js ask 弹窗全局 id 串写（限定弹窗内查询）/任务徽章抽 taskStatusClass/hero-import 250ms 定时器改 pendingImport 挂起（首用可弹）；view-misc.js 问答配置加载失败不再可误存空值/default_ima_api_key 补 trim。

## low 36 条处置

- **已修（15）**：[0] MkdirAll 错误不再吞、[1] **spaHandler 用 path.Clean（filepath 在 Windows exe 上会把静态资源全打成 index.html——真 bug）**、[2] /mcp/ 尾斜杠、[5] JSON 请求体 2MB 上限、[13] 笔记导出不再双扩展名、[15] 导入 URL 强制 http/https+Host、[16] 过期会话随建随清、[17] 启动日志地址拼接、[18] build.sh 先清 dist、[19] base64 真兼容 URL-safe、[22][23][24] 删除类接口错误区分 404/500、[26] 口令长度按字符计（中文）、[27] 扫描遍历中断记入 AccountStat.Error。
- **设计如此/不改（9）**：[3][4] build.sh 可移植性（构建只在本机 GNU 环境跑，产物是二进制）、[6] 设置空值=保持不变（防误清，清除需后续加显式开关）、[9] IMA_COS_ENDPOINT 是运维测试钩子（环境变量本身即运维控制面）、[10] ima 错误透传原文（产品设计）、[12] due 宽松解析（兼容字符串/时间戳是需求）、[14] 重名去重极端组合碰撞（概率极低）、[25] extForMediaType 粗粒度（上游 media_type 无法细分 jpg/png）。
- **web 12 条全部属实已修**：ui.js confirmDialog HTML 拼接（JSDoc 澄清，调用点已转义）/嵌套三元清理、app.js 死代码、view-files.js 详情死分支/import-urls 计数/双击冗余、view-notes.js 返回按钮不可达、view-misc.js api_key 未 trim、view-home.js 嵌套三元/ask 弹窗串写/hero-import 首用弹不出。（与 medium web 条有复用修复，见上节）

## 回归记录

- `go vet ./...` 干净、`go build ./...` OK、`go test ./...` 全过（含新 auth_test）
- `node --check` 改动的 5 个 JS 全过
- 行为探针 `probe_fixes.sh`（mock 栈）**ALL_GREEN 10/10**
- `scripts/smoke.sh` 全接口冒烟 SMOKE_EXIT=0（23 个成功响应，无 401/500）
- 已重新构建并重启 `imahub.service`，healthz OK
