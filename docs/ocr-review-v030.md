# ocr 评审处置表（v0.3.0 增强轮，2026-11-08）

工具：open-code-review（`ocr review --commit HEAD`，diff=本轮增强 1af8ac2）。结果：39 条（high 7 / medium 22 / low 10；15 文件、8.9M tokens）。原始意见：`ocr-v030-findings.json`。

## 已修（37/39，逐条对源码核实属实后动手）

**high（7 全修）**
1. `probe_queue.sh` QID 空值导致断言假通过 → 探针重写：JSON 解析断言 + ID 非空校验 + 失败退出码 + trap 清理 + 就绪探测。
2. `clientlink.go` since 游标用客户端时钟对服务端时间过滤（时钟偏差漏项）→ 游标改用 pull 返回的 server_time（减 1s 重叠窗口）。
3. `queue.go` 定时器与手动续传并发双传 → drainMu 单飞锁（drainQueue/drainOne 全走锁）。
4. `queue.go` 瞬时失败永久变 failed、永不自动重试（离线场景根本性坏）→ failed 条目每轮自动重试，错误文案保留展示。
5. `queue.go` 队列接口无归属隔离 + 泄漏服务端绝对路径 → 归属校验（本人/admin）+ 响应剥掉 path/user_id（json omitempty）。
6. `view-files.js` 入队只认传输层错误（业务 5xx/上游不可用永远进不了队列）→ 非永久性失败（排除同名/类型/超限）一律转入队列，含「正在入队…」状态。
7. `queue.go` 入队无类型/大小校验（压缩包入了也永远传不上去）→ 与直传同规则：类型白名单、压缩包拒收、按类型大小上限、配额 20 个/2GB、文件名 Base 净化、暂存 Close 错误检查、落盘失败清孤儿文件。

**medium（18 修复）**：公告删除墓碑（ann:）+ 同步窗口 50→200；队列续传改异步立即返回（UI 延迟回看）；`auto` 语义对齐（未指定目标时跳过无默认知识库的账号）；已入队条目移出本地列表防重复提交；queue-list/queue-item/srv-chip 补样式；`.chip`/`.crumb` 被 overflow 裁掉的假命中区改真实尺寸；localStorage 脏数据 Array 防护；留空地址回落生效地址；chip 点击用渲染快照防下标漂移；换服务端游标归零全量重拉；AddQueueItem 落盘报错上抛；探针补 拒收/净化/归属/路径泄漏/重启持久化/逐条断言。

**low（12 修复）**：探针 pipefail/curl 检查；删除接口 404/500 区分；队列配额（20 个/2GB）；删除二次确认；死样式规则清理（.icon-btn/.theme-toggle/label.checkbox/.badge.clickable 假命中区）；入队进度状态文案；其余同上复用修复。

## 设计如此/留待后续（2）

- **队列 GC**：failed 条目不自动清除（配额 + 手动删除 + attempts 展示足够；自动清理会误删用户想重试的文件）。
- **mock 栈不在仓库内**：探针前置检查已加（缺 mock 明确报 2 退出）；mock 契约见 imahub-dev 技能 testing-stack.md。

## 回归记录（全绿）

go vet/build/test ✓；JS 7 文件语法 + CSS 586/586 平衡 ✓；probe_queue **15/15**（重写版）；probe_fixes **10/10**；probe_client **13/13**；smoke 23 项 0 错。
