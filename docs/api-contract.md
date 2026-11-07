# ImaHub REST API 契约（v1）

统一约定：
- 所有响应 JSON；业务封装为 `{"code":0,"msg":"ok","data":{...}}`（code!=0 为错误，msg 为中文错误信息）。
- 登录态：HttpOnly Cookie `sid`；同时接受 `Authorization: Bearer <mcp_token>`（等同登录该用户）。
- 未登录返回 401 `{"code":401,"msg":"未登录"}`。
- 需要管理员的接口若非 admin 返回 403。
- 服务端错误 500；参数错误 400。

## 认证 / 账号

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/setup/status` | `data:{need_setup:bool}`（无用户时为 true） |
| POST | `/api/setup` | 首次初始化：`{username,password}` 创建管理员，自动登录 |
| POST | `/api/login` | `{username,password}` → `data:{user}`，设 Cookie |
| POST | `/api/logout` | 注销 |
| POST | `/api/register` | `{username,password}`（`allow_register` 开启时可用） |
| GET | `/api/me` | `data:{user,ima:{accounts:[{id,name,client_id_preview,bound}],default_id},mcp_token_set:bool}`（兼容保留 `ima.bound`） |
| POST | `/api/me/password` | `{old_password,new_password}` |
| POST | `/api/me/ima` | `{name?,client_id,api_key}` 新增一个 ima 账号 → `data:{account:{id,name,client_id_preview}}` |
| PATCH | `/api/me/ima/{id}` | `{name?,client_id?,api_key?}` 更新账号（空字段不改） |
| DELETE | `/api/me/ima?id=<id>` | 删除指定账号；不带 id = 清空全部 |
| GET | `/api/me/mcp-token` | `data:{token}`（无则自动生成并持久化） |
| POST | `/api/me/mcp-token/rotate` | `data:{token}` 重新生成 |

`user` 结构：`{id,username,role:"admin"|"user",disabled:bool,created_at:int}`。

## 管理（role=admin）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/admin/users` | `data:{users:[user]}` |
| POST | `/api/admin/users` | `{username,password,role}` |
| PATCH | `/api/admin/users/{id}` | `{password?,role?,disabled?}`（不能禁用/降级自己） |
| DELETE | `/api/admin/users/{id}` | 删除用户（不能删自己） |
| GET | `/api/admin/settings` | `data:{site_name,allow_register,default_ima_client_id,default_ima_api_key_set}` |
| PUT | `/api/admin/settings` | `{site_name?,allow_register?,default_ima_client_id?,default_ima_api_key?}`（空字符串 default_ima_api_key 表示不改） |

IMA 凭证优先级：用户本人绑定 > 服务端默认凭证；都没有则相关接口返回 400 `尚未绑定 IMA 凭证`。

## IMA 代理（登录可用）

以下接口透传 ima.qq.com 开放平台（用户凭证）。IMA 侧业务错误（code!=0）原样透出：`{"code":<ima_code>,"msg":"IMA:<ima_msg>"}`。

**多账号 = 容量池（不做手动切换）**：多个 ima 账号聚合使用，提高总容量。
- **聚合列表**：`GET /api/ima/kbs`、`GET /api/ima/addable-kbs`、`GET /api/ima/notebooks` **省略 `ia` = 聚合全部账号**，每项附加 `ia`（所属账号 id）与 `account_name`（单页最多每账号 50 条，`is_end` 恒为聚合口径）；传 `ia` = 只查该账号。
- **单点路由**：`/api/ima/list`、`/api/ima/search`、`/api/ima/notes`、`/api/ima/media`、`/api/ima/download` 等针对具体 KB/笔记本/条目的接口需传 `ia`（取聚合列表项自带的 `ia` 回传）。
- **上传自动分配**：`POST /api/ima/upload` 传 `ia=auto`（推荐）→ 服务端在各账号间轮询分配，`kb_id` 可省略（落到该账号设置的「默认上传知识库」，未配置则 400 提示）；`ia=<id>` 指定账号（必须带 kb_id）。响应含实际使用的 `ia`、`account_name`、`kb_id`。
- **账号默认上传知识库**：`PATCH /api/me/ima/{id}` 支持 `upload_kb_id`（配合 `/api/ima/kbs?ia=<id>` 挑选）。
- `GET /api/ima/accounts` 返回账号清单（含 `upload_kb_id` 预览）。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/ima/accounts` | `data:{accounts:[{id,name,client_id_preview,source:"user"|"server_default"}],default_id}` |
| GET | `/api/ima/test?ia=` | 连通性测试，调 `get_addable_knowledge_base_list(limit=1)`，`data:{ok:bool,note:str}` |
| GET | `/api/ima/kbs?q=&cursor=&limit=` | `search_knowledge_base`（q 空=默认列表）→ `data:{info_list:[{id,name,cover_url}],is_end,next_cursor}` |
| GET | `/api/ima/kb?id=,ids=` | `get_knowledge_base` → `data:{infos:{id:KBInfo}}` |
| GET | `/api/ima/addable-kbs?cursor=&limit=` | `get_addable_knowledge_base_list` |
| GET | `/api/ima/list?kb=&folder=&cursor=&limit=` | `get_knowledge_list` → `data:{knowledge_list:[{media_id,title,parent_folder_id,is_folder,folder_id,name,file_number,folder_number}],is_end,next_cursor,current_path:[FolderInfo]}`（服务端把文件夹条目规整出 is_folder 标记，文件夹优先排序） |
| GET | `/api/ima/search?kb=&q=&cursor=` | `search_knowledge` → `data:{info_list,is_end,next_cursor}` |
| GET | `/api/ima/media?media_id=` | `get_media_info` → `data:{media_type,url_info?,notebook_ext_info?,note_content?}`（笔记类自动补 `note_content` 纯文本） |
| GET | `/api/ima/download?media_id=&ia=&inline=1` | 服务端代理下载；`inline=1` 时以 `Content-Disposition: inline` + 正确 MIME 返回（供预览内嵌） |
| POST | `/api/ima/download-zip` | `{ia?,items:[{media_id,title}],kb?,folder?}` 批量打包 zip 流式下载（items 优先；否则打包 kb/folder 整个文件夹，浅层递归上限 200 个；失败条目以 `[失败] xx.txt` 记录） |
| GET | `/api/ima/check-names?kb=&folder=&name=&media_type=` | `check_repeated_names` → `data:{is_repeated:bool}` |
| POST | `/api/ima/upload` | multipart：`file`、`kb_id`、`folder_id?`、`force?`（同名强制）。流程 check→create_media→COS PUT→add_knowledge；返回 `data:{media_id,repeated:bool}`。服务端按扩展名判定 media_type/大小上限（Excel/TXT/Xmind/MD/HTML 10MB；图片 30MB；EPUB 50MB；PDF/Word/PPT/音频等 200MB），不支持的类型 400 |
| POST | `/api/ima/import-urls` | `{kb_id,folder_id,urls:[]}`（1-10 个）→ `data:{results}` |
| GET | `/api/ima/notebooks?cursor=&limit=` | `list_notebook`（cursor 首页传 `"0"`） |
| GET | `/api/ima/notes?folder=&cursor=&limit=` | `list_note` |
| GET | `/api/ima/notes/search?q=&search_type=&start=&end=` | `search_note`（search_type: 0 标题/1 正文） |
| GET | `/api/ima/note?note_id=` | `get_doc_content` → `data:{content}` |
| POST | `/api/ima/note` | `{content,folder_id?,folder_name?}`（Markdown）→ `data:{note_id}` |
| POST | `/api/ima/note/append` | `{note_id,content}` |

## 首页仪表盘

**权限模型（团队）**：ima 账号管理（`/api/me/ima*`）为 **admin（组长）专属**；其余全部接口（浏览/搜索/下载/**上传/导入/写笔记**/问答）全员可用（知识库数据全团队互通，成员可正常贡献内容）。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/ask` | `{q,kb_id?,top_k?,ia?}` 知识库智能问答 → `data:{answer,sources:[{ia,account_name,kb_id,kb_name,media_id,title,snippet}],mode:"llm"\|"retrieval",model?,error?}`（全员可用） |
| GET | `/api/ask/status` | `data:{configured,model}` |

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/dashboard` | `data:{stats,by_type,by_account,recent,scan}` |
| POST | `/api/scan` | 触发知识库全量扫描（异步、单飞）→ `data:{status}` |
| GET | `/api/tasks` | `data:{tasks:[task]}` |
| POST | `/api/tasks` | `{title,note?,due?,status?}` → `data:{task}` |
| PATCH | `/api/tasks/{id}` | `{title?,note?,due?,status?}` |
| DELETE | `/api/tasks/{id}` | 删除任务 |
| GET | `/api/events?from=&to=` | `data:{events:[event]}` 团队日程（按开始时间升序，from/to 可选过滤） |
| POST | `/api/events` | `{title,start_at,end_at?,all_day?,location?,note?}` → `data:{event}` |
| PATCH | `/api/events/{id}` | 同上字段部分更新 |
| DELETE | `/api/events/{id}` | 删除日程 |

- `event`: `{id,title,start_at,end_at,all_day,location,note,by,created_at,updated_at}`（团队共享，全员可增删改；start_at/end_at 为 Unix 秒，end_at=0 表示无）
- **团队公告**：`GET /api/announcements` 全员可读（最新 5 条）；`POST/PATCH/DELETE /api/announcements[/{id}]` 仅管理员；`announcement`: `{id,content,by,created_at,updated_at}`

- `stats`: `{accounts,kbs,files,folders,uploads,bytes}`（uploads/bytes = 经 ImaHub 上传累计）
- `by_type`: `[{type,label,count,pct}]` 文件类型分布（来自扫描快照，pct 0~1）
- `by_account`: `[{ia,name,kbs,files,folders,uploads,bytes,share,error?}]` 账号贡献（share = 文件数占比 0~1；扫描失败的账号带 error）
- `recent`: `[{kind,title,ia,account_name,kb_name,media_id,size,at}]` 近期添加（≤30 条，kind: `upload`/`import`/`scan_new`/`note`）
- `scan`: `{status:"idle"|"running"|"done"|"error",last_scan_at,progress:{done,total},error?}`
- `task`: `{id,title,note,status:"todo"|"doing"|"done",due,created_at,updated_at}`（每用户独立）
- 扫描行为：后台遍历容量池每个账号每个知识库（游标翻页，单知识库上限 5000 条、深度上限 10 层），产出统计与快照（`data/scan.json`）；`GET /api/dashboard` 发现快照超过 30 分钟自动触发一次；同时刻仅一个扫描在跑。

## 服务器信息

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | `{"ok":true}` |
| GET | `/api/server-info` | `data:{site_name,version,allow_register}`（未登录可见，供登录页显示站点名） |

## MCP

- `POST /mcp`，认证 `Authorization: Bearer <mcp_token>`（也可 query `?token=`）。
- JSON-RPC 2.0，实现：`initialize`、`notifications/initialized`、`ping`、`tools/list`、`tools/call`。
- 响应头带 `MCP-Protocol-Version` 无所谓；直接返回 `application/json` 即可。
- 工具集（名称固定；所有 ima_* 工具均接受可选 `account` 参数 = 账号 id 或名称）：
  - **省略 `account` 时**：`ima_search_knowledge_bases`、`ima_list_notebooks` 为**聚合全部账号**（每项带 account 字段）；`ima_upload_file` 自动轮询分配账号（file_name 目标知识库由 knowledge_base_id 决定或用账号默认上传知识库）；其余工具用默认账号（第一个）。
  - `ima_test` 无参
  - `ima_search_knowledge_bases` {q?,cursor?,limit?}
  - `ima_list_knowledge` {knowledge_base_id,folder_id?,cursor?,limit?}
  - `ima_search_knowledge` {knowledge_base_id,q,cursor?}
  - `ima_get_media_info` {media_id}
  - `ima_download_media` {media_id}（文本化返回内容或给出下载 URL）
  - `ima_upload_file` {knowledge_base_id,folder_id?,file_path?,content_base64?,file_name}（服务端本地路径或 base64）
  - `ima_import_urls` {knowledge_base_id,folder_id,urls[]}
  - `ima_list_notebooks` {cursor?,limit?}
  - `ima_list_notes` {folder_id?,cursor?,limit?}
  - `ima_search_notes` {q,search_type?,start?,end?}
  - `ima_get_note` {note_id}
  - `ima_create_note` {content,folder_id?,folder_name?}
  - `ima_append_note` {note_id,content}
- `tools/call` 返回 `content:[{type:"text",text:<JSON 字符串>}]`；业务错误时 `isError:true`。

## v0.2.0 服务端/客户端架构

账号模型：1 成员 1 个 ima 账号（成员自助配置），管理员纯管理；全部账号对全员可见（免授权分发 key）。

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | /api/me/ima-keys | 登录 | 全部账号完整凭证（id/name/owner/source/client_id/api_key/upload_kb_id） |
| POST | /api/me/ima | 登录 | 自绑/更新我的 ima 账号（1 人 1 号）{name,client_id,api_key,upload_kb_id} |
| PATCH | /api/me/ima/{id} | 登录 | 部分更新（管理员可带 ?user_id= 代管） |
| DELETE | /api/me/ima?id= | 登录 | 解绑（不带 id 清空；管理员可带 ?user_id=） |
| POST | /api/sync/push | 登录 | 客户端改动回传 {activities,tasks,events,tombstones}；任务/日程 LWW（UpdatedAt 决胜，删除墓碑压旧更新） |
| GET | /api/sync/pull | 登录 | 拉取 {tasks,events,announcements,accounts,settings(ask_*),tombstones,server_time} |
| POST | /api/client/sync | 登录 | 客户端立即同步（push+pull） |
| GET | /api/client/status | 登录 | {connected,server,last_sync} |

- accounts 项带 owner（归属用户名）、own（是否我的）、source（user|team|server_default）。
- GET /api/server-info 增加 mode("serve"|"client")，client 模式带 connected/server/last_sync。
- 客户端模式：POST /api/login 增加可选 server 字段（服务端地址）；/api/setup、/api/register 禁用；公告写接口返回 400（公告在服务端管理）。
- 活动流水按 ID 幂等（同步重放不重复计）；删除任务/日程写 tombstones（"task:<id>"/"event:<id>" → unix 秒）。

## v0.3.0 增强（离线上传队列 / 增量同步）

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| POST | /api/queue/upload | 登录 | 入队（multipart file/kb_id/folder_id/ia），文件暂存 data/queue/ |
| GET | /api/queue | 登录 | 队列列表（pending \| failed，含 error/attempts） |
| POST | /api/queue/drain | 登录 | 立即续传全部 pending，返回 {sent} |
| POST | /api/queue/{id}/retry | 登录 | 单条重试（状态复位后立即尝试） |
| DELETE | /api/queue/{id} | 登录 | 删除条目与暂存文件 |

- 队列由后台引擎每 60 秒自动续传；成功条目出队并记活动流水（含 BumpUpload 统计）。
- `GET /api/sync/pull?since=<unix>`：任务/日程/公告/墓碑仅回 UpdatedAt 大于 since 的变更项；账号与问答配置总是全量。客户端以 ClientLastSync 为 since，活动流水按 ClientActivitySync 游标增量推送（30s 重叠窗口 + ID 幂等去重）。
- 聚合浏览（知识库/笔记本）服务端并发拉取各账号，单账号 15s/30s 超时，失败逐账号隔离。
