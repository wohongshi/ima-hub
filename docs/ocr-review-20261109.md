# ocr 评审处置表 — P2P 轮（2026-11-09）

范围：d3dcc95 / 4f6240d / 0dfeaad / f5cc82f 逐提交 diff 评审。第 1 轮（d3dcc95 40 条）与第 2 轮（4f6240d 29 条 + 0dfeaad 2 条 + f5cc82f 3 条）处置如下。后续轮次先读本表，已结案条目不重议。

## 已修（按评审编号对应 ocr_d3dcc95.txt 条目顺序）

| # | 严重度 | 问题 | 修法 |
|---|--------|------|------|
| 1 | high | AccessUpsert 静默丢写 + 远端 updated_at 可劫持 LWW（授权无法撤销） | Upsert 返回 bool、远端时间戳钳制到 now、同刻按状态优先级 tie-break（revoked/denied>approved>pending） |
| 2 | perf | AccessRequests 只增不减、ExpiresAt 从不生效 | accessCap 2000（满时淘汰最旧终态，pending 保留）、AccessExpireSweep 懒惰收敛 expired |
| 3/24/37 | high | Identity.Nickname 数据竞争（改名 × 宣告循环） | 字段私有化 + Nick()/NextTS() 加锁访问器；save 失败回滚 |
| 4 | med | 宣告 30s 一轮把本地备注覆盖掉 | Peer 加 Remark 独立字段（DisplayNick 备注优先），SetNickname 写备注 |
| 5 | med | outbox 退避移位溢出（Attempts≥60 变负） | 仅 ≤5 次移位，之后恒 480 |
| 6 | low | 信封 Nonce 只查非空，防重放是假承诺 | seenNonce 去重表（TTL 600s，>4096 清理），重放幂等吞掉 |
| 7 | med | identity.json 非原子写（写一半=永久丢身份） | tmp+rename，marshal 错误不再吞 |
| 8 | low | PeerAddr 把裸 IPv6 当 host:port | net.SplitHostPort 判定 + JoinHostPort 括号 |
| 9/15 | low/med | Outbox.Push 失败静默但 API 报 queued | 先入队后建档；msgID 为空如实报错（队满/未启用），decide 路径同 |
| 10 | low | SanitizeNick 按字节计 24（中文只剩 8 字） | 按 rune 计 24 |
| 11 | security | 入站 RPC 无限灌申请（未鉴权邻居可写爆 data.json） | 每节点 pending 上限 32 + validKBs（≤32 项×128 字符）+ store 全局 accessCap |
| 12 | med | decision 远端 scopes 不过 validScopes、时间戳不钳制 | 已修（并入 #1） |
| 13 | med | 请求方撤销后审批方残留授权（kbGrantOf 继续放行） | decision 分支处理 received 侧 revoked/expired → recomputeGrants |
| 14/25 | med | GrantScopes 整组覆盖/撤销误清别条申请；节点已删时假批准 | 改为按 received+approved 并集 recomputeGrants；批准前校验节点存在 |
| 16 | low | expires_at 从未生效 | 并入 #2（AccessExpireSweep + kbGrant/grantedToMe 过滤） |
| 17/23/33 | low | 邀请 addrs 不校验、前端端口范围不限、码长可超限 | validHostPort（含 IPv6、端口 1-65535）+ 码长校验 + 前端正则收紧 |
| 18 | med | client 模式宣告不可达地址（8090 环回） | client 模式跳过 RunLAN/RunOutbox |
| 19 | low | 端口 0/解析失败静默 8090 | 端口非法直接 Fatalf 拒绝启动 |
| 20/21 | test | probe 启动不验活 / 缺 python3 前置 | start_node fail-fast + 日志尾巴；python3 守卫 |
| 22 | med | 前端昵称校验与后端不一致 | 后端改 rune 计数 + 前端 \p{L}\p{N} 正则对齐 |
| 26 | med | 自愈覆盖邀请配置的可达地址 | **设计如此，延期 d5**：当前仅局域网场景，实见地址=正确地址；公网/NAT 需要地址来源优先级+回退列表，归 d5 发现层统一设计 |
| 27 | security | in.KBs 不限量直入白名单 | validKBs 净化（并入 #11） |
| 28 | security | decision 无发送方绑定（第三方可伪造） | ar.PeerID == e.From 才生效，其余幂等吞 |
| 29 | med | 删节点不清 outbox/授权、30s 被宣告复活 | Outbox.Remove + AccessRemovePeer + 删除墓碑（Removed，邀请码显式加回才复活） |
| 30 | security | server-info 未鉴权泄露节点指纹+主机名 | p2p 块仅登录后返回 |
| 31/32 | low | ParseInvite 不验版本 / 过期只单边 | V!=1 拒绝；TS 上界 +replayWindowSec |
| 34 | low | Upsert 补 Pub/XPub/Source 不置 changed（重启丢） | 全部补 changed=true |
| 35 | med | outbox 无重试上限（僵尸消息永久占位） | outboxMaxAttempts=200 弃投 |
| 36 | security | 宣告抓包重放可劫持节点指向+静默吞消息 | 发送方 NextTS 单调自增 + 收方 lastTS 拒绝不增帧（TS 签名不可篡改）；残余窗口≤下一轮宣告（30s），LAN 威胁模型可接受 |
| 38 | test | probe 无 deny 覆盖 | probe_kb 补拒绝路径 3 断言（denied 回传/不误伤已有授权） |
| 39 | med | 前端路由切换竞态 null deref | 捕获节点判空丢弃过期响应 |
| 40 | low | 节点页不刷新/在线状态不显示 | 30s 轮询（路由切走即停）；在线徽章已有 |

## 误报 / 不改

- 评审对 `AccessUpsert` 建议的等值 tie-break 原文（"相等时间戳后写覆盖"）已按状态优先级实现，比建议更保守。
- 「Additive/subtractive GrantScopes」建议以 recomputeGrants（并集重算）实现，语义等价且更简单。

## 修复验证

- go vet/build/test 全过（peer/api/store 单测含新增 grantedToMe/kbGrant 逻辑）
- probe_peer 14/14、probe_kb 17/17（含拒绝路径）、probe_queue 15/15、probe_fixes 10/10、probe_client 13/13、smoke 23 项 0 错
- 总计 69 探针断言全绿

---

# 第 2 轮（4f6240d 29 条 + 0dfeaad 2 条 + f5cc82f 3 条）

## 已修

| 来源 | 严重度 | 问题 | 修法 |
|------|--------|------|------|
| 4f62 | high | **download 白名单可绕过**：media 只按 ID 全账号取，不验归属 kb（kb1 授权可下 kb2 的文件） | mediaInKB：BFS 遍历授权 kb 目录树先证归属再取（页/目录/条目上限，超限拒绝） |
| 4f62 | high | probe 白名单测试不可观测（mock 只有 kb1） | mock 加 kb2「未授权库」，probe 断言 kb2 被滤掉；补跨库 media 绑定绕过用例 |
| 4f62/0dfe | med | 内存放大（32MB→~60MB×并发）/ margin 折叠失效 / checkbox 死 CSS | 下载并发闸（2）+ 修正 field 间距写法 + 删死声明 |
| 4f62 | med | 跨库检索 is_end 硬编码 true（被滤库永不翻页）；全账号失败冒充空成功 | kbAggKBs 统一聚合：单账号透传游标、失败计数、全失败报错（兼 f5cc 同条） |
| 4f62 | med | PostEnvelopeResp 无 ctx（断连后继续拉 32MB）；回包截断静默；错误回显无上限 | ctx 透传（NewRequestWithContext）+ 超限显式报错 + 错误回显截 4KB |
| 4f62 | med | content_b64 缺失→空文件 200 假成功 | 判空显式报错 |
| 4f62 | med | KBs 请求方全控，审批方看不见也收窄不了 | decide 接受 kbs（可设/收窄白名单）+ 批准弹窗白名单输入框（预填申请值） |
| 4f62 | med | probe：空断言（xpub 恒真）/req_and_approve 不验送达审批/负向断言 grep 俚/不 trap INT/陈旧 dl.bin 假绿/并集无回归检查 | 全部重写：JSON 断言 biz_err、送达+审批失败即中止、trap INT TERM、dl.bin 前置清理+curl -f、并集断言；mock 入库 scripts/mock2.py + MOCK 覆盖 + 缺失守卫 |
| 4f62 | low | 状态徽章未转义/嵌套三元/行内 style/deny 非 danger/授权后节点行不刷新 | 逐条修（转义、if/else、acc-hint 类、danger、双刷新） |
| 4f62 | low | .card-head 移动端逐字换行 | .peers-page .card-head flex-wrap |
| 0dfe | low | 勾选框 border-radius 死 CSS；.modal .field+.field margin 叠加 | 删除/改写 |
| f5cc | low | rpcTransport 裸建丢代理环境（HTTP_PROXY 失效） | DefaultTransport.Clone 保留代理/拨号/HTTP2 默认 |
| f5cc | low | 15s 超时吞错无日志 | log.Printf 记录失败账号（并入聚合修复） |

## 误报 / 不改

- scopeBadgesHtml 抽取（4f62 maintainability）：两处重复可接受，改动面大于收益——不改。
- access 列表双请求合并单请求（4f62 perf low）：双请求本就并行，收益微——不改。
- probe wait 非子进程问题（4f62 med）：start_node 已 fail-fast + kill 兜底，wait 仅优雅等待——维持现状。

## 第 2 轮验证

- probe_kb 重写后 **22/22**（白名单滤 kb2/跨库 media 绑定拒/敌意文件名净化/未知 media 干净报错/并集回归/拒绝路径）
- 全量：probe_kb 22 + probe_peer 14 + queue 15 + fixes 10 + client 13 = **74 断言** + smoke 0 报错
