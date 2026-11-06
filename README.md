# ImaHub

腾讯 **ima**（ima.qq.com）开放平台的**多用户 Web 网关**：不用安装 ima 客户端，浏览器里就能管理知识库、上传/下载文件、写笔记；同时自带 **MCP 入口**，AI 客户端可以直接调用。

单二进制、零外部依赖（纯 Go 标准库），WebUI 内嵌，数据落在本地 JSON 文件。

## 功能

- **账号系统**（参考 OpenList，团队模型）：首次启动初始化管理员；**管理员=组长**：配置团队 ima 账号、管理成员；**成员**：浏览、搜索、下载、上传、导入、写笔记、知识库问答（全功能使用，仅账号配置除外）。可开关自助注册；会话 30 天。
- **团队容量池（多账号聚合）**：团队绑定多个 ima 凭证扩容；知识库/笔记本为全账号合并视图（条目标注所属账号），数据全团队互通；上传 `ia=auto` 自动轮询分配，可为每账号设「默认上传知识库」。
- **知识库问答（agent 级查询）**：自然语言提问 → 跨账号跨知识库检索 → 内置模型（可配 OpenAI 兼容端点）合成回答并附引用来源；首页问答框与 MCP `ima_ask` 同款能力，未配模型时降级为纯检索。
- **知识库**：浏览/搜索知识库、文件夹层级导航、条目搜索、分页加载。
- **文件上传/下载**：浏览器多文件上传（带进度）→ 自动走官方 create_media → COS 上传 → add_knowledge 全流程；同名检查+强制覆盖；下载经服务端代理（免 CORS，自动带认证头）。
- **压缩包自动解压上传**：上传 zip / 7z / rar 自动解压逐个入库（目录层级并入文件名）；带 zip-slip 路径穿越拦截、解压炸弹限额（5000 文件 / 2GB / 单包 500MB）；失败明细逐条回报。
- **导入 URL**：网页/微信公众号文章一键导入知识库（1-10 条批量）。
- **笔记**：笔记本/笔记列表、标题/正文搜索、阅读纯文本、新建 Markdown 笔记、追加内容。
- **IMA 引擎自写**：完整实现官方 `/openapi/wiki/v1` 与 `/openapi/note/v1` 接口 + COS PUT 签名上传（q-sign-algorithm=sha1），无需任何腾讯 SDK。
- **MCP 服务**：`/mcp`（Streamable HTTP / JSON-RPC），15 个工具覆盖知识库、笔记与问答全流程，按用户 token 隔离各自的 ima 凭证。
- **多 ima 账号 = 容量池**：单账号容量有限（约 2GB），绑定多个账号聚合扩容。浏览/笔记自动合并全部账号（条目带账号标签）；上传默认**自动轮询分配**到各账号（每账号可设默认上传知识库），也可手动指定目标；MCP 工具同样支持聚合与自动分配。
- **首页仪表盘 + 任务计划**：知识库统计、文件类型分布、账号贡献占比、近期添加（上传/导入/笔记/扫描新发现）；后台全量扫描（快照 `data/scan.json`，30 分钟自动补扫）；团队共享待办清单（带操作人、备注、截止日期）。
- **今日待办 + 日程**：今日智能聚合（到期/逾期任务 + 当日日程，红标逾期）；团队共享日程（月历视图 + 即将到来 + 倒计时「还有 N 天」+ 全天事件），全员可增删改；Hero 显示最近日程提醒。
- **批量下载 + 在线预览 + 团队公告**：文件多选打包 zip 下载、整文件夹一键打包（浅层递归）；详情弹窗内联预览图片 / PDF / 文本 / Markdown；组长发布团队公告置顶首页。

## 快速开始

```bash
# 运行（开发）
go run . --addr :8090 --data ./data

# 或编译后运行
go build -o imahub . && ./imahub --addr :8090
```

浏览器打开 `http://localhost:8090` → 首次进入创建管理员 → 「设置」里绑定 ima 凭证。

### 获取 ima 凭证

1. 打开 <https://ima.qq.com/agent-interface> 并登录（QQ/微信）
2. 点击「获取 API Key」，复制 **Client ID** 与 **API Key**
3. 填进 ImaHub 设置页（或让用户各自绑定）

### 参数

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--addr` | `:8090` | 监听地址 |
| `--data` | `./data` | 数据目录（data.json、上传中转 tmp/） |
| `--ima-base-url` | `https://ima.qq.com` | ima 开放平台地址（测试用） |

## MCP 接入

端点：`http://<host>:8090/mcp`，认证：`Authorization: Bearer <个人 token>`（设置页生成/重置）。

```json
{
  "mcpServers": {
    "imahub": {
      "type": "streamableHttp",
      "url": "http://<host>:8090/mcp",
      "headers": { "Authorization": "Bearer <你的token>" }
    }
  }
}
```

工具清单：`ima_ask`（知识库智能问答）、`ima_test`、`ima_search_knowledge_bases`、`ima_list_knowledge`、`ima_search_knowledge`、`ima_get_media_info`、`ima_download_media`、`ima_upload_file`、`ima_import_urls`、`ima_list_notebooks`、`ima_list_notes`、`ima_search_notes`、`ima_get_note`、`ima_create_note`、`ima_append_note`。所有工具都支持可选 `account` 参数（账号 id 或名称）选择 ima 账号。

## 打包

```bash
bash scripts/build.sh          # windows-x86_64.exe + linux-x86_64 + linux-arm64
bash scripts/build.sh --all    # 额外加 linux-armv7
```

产物在 `dist/`。全部 `CGO_ENABLED=0` 静态编译，拷到目标机器直接跑。

## 目录结构

```
main.go                 入口：flag、内嵌 WebUI、路由装配
internal/store/         JSON 持久化（用户/会话/设置，原子写）
internal/auth/          PBKDF2 口令哈希
internal/ima/           自写 ima 开放平台引擎（wiki/note/COS 上传签名、压缩包解压）
internal/ask/           知识库 RAG 问答（检索 + OpenAI 兼容模型合成）
internal/api/           REST 接口
internal/scan/          知识库后台扫描（统计快照）
internal/mcp/           MCP 服务（JSON-RPC / Streamable HTTP）
web/                    手写前端 SPA（零依赖）
scripts/build.sh        跨平台打包脚本
docs/api-contract.md    REST 契约
```

## 限制说明

- ima 开放平台本身没有「删除条目」接口，故 ImaHub 不提供删除（ima 客户端里删）。
- 音频最长 2 小时；单文件大小上限按官方规则（文本类 10MB、图片 30MB、EPUB 50MB、其余 200MB），服务端已内置校验。
- 服务端部署时「数据目录」包含 ima 凭证与口令哈希，备份该目录即备份全部数据，请勿把 `data/` 目录公开分发。
