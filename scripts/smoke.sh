#!/usr/bin/env bash
# ImaHub 全接口冒烟测试（假凭证验证管线，业务错误应为 IMA 侧错误透传）
set -u
BASE=http://127.0.0.1:18091
J() { python3 -c "import json,sys;d=json.load(sys.stdin);print(json.dumps(d,ensure_ascii=False)[:300])"; }

echo "== healthz / server-info =="
curl -s $BASE/healthz; echo
curl -s $BASE/api/server-info | J

echo "== setup 管理员 =="
curl -s -c /tmp/ih-admin.txt -X POST $BASE/api/setup -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}' | J

echo "== me（应含 ima.accounts 结构） =="
curl -s -b /tmp/ih-admin.txt $BASE/api/me | J

echo "== 新增 ima 账号（假凭证 x2） =="
curl -s -b /tmp/ih-admin.txt -X POST $BASE/api/me/ima -H 'Content-Type: application/json' -d '{"name":"测试号A","client_id":"fake_a","api_key":"key_a"}' | J
curl -s -b /tmp/ih-admin.txt -X POST $BASE/api/me/ima -H 'Content-Type: application/json' -d '{"name":"测试号B","client_id":"fake_b","api_key":"key_b"}' | J

echo "== /api/ima/accounts =="
curl -s -b /tmp/ih-admin.txt $BASE/api/ima/accounts | J

echo "== PATCH 账号（改名 + upload_kb_id） =="
ACC1=$(curl -s -b /tmp/ih-admin.txt $BASE/api/ima/accounts | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["accounts"][0]["id"])')
curl -s -b /tmp/ih-admin.txt -X PATCH $BASE/api/me/ima/$ACC1 -H 'Content-Type: application/json' -d '{"name":"测试号A-改名","upload_kb_id":"kb_fake_1"}' | J

echo "== /api/ima/test?ia=（假凭证应返回 ok:false + IMA 错误） =="
curl -s -b /tmp/ih-admin.txt "$BASE/api/ima/test?ia=$ACC1" | J

echo "== /api/ima/kbs 聚合（无 ia，看每项 ia/account_name 与错误处理） =="
curl -s -b /tmp/ih-admin.txt "$BASE/api/ima/kbs?q=test" | J

echo "== /api/ima/kbs 单账号 =="
curl -s -b /tmp/ih-admin.txt "$BASE/api/ima/kbs?ia=$ACC1&q=test" | J

echo "== 任务 CRUD（due 用日期字符串） =="
curl -s -b /tmp/ih-admin.txt -X POST $BASE/api/tasks -H 'Content-Type: application/json' -d '{"title":"测试任务一","note":"备注A","due":"2026-11-07"}' | J
curl -s -b /tmp/ih-admin.txt -X POST $BASE/api/tasks -H 'Content-Type: application/json' -d '{"title":"测试任务二"}' | J
T1=$(curl -s -b /tmp/ih-admin.txt $BASE/api/tasks | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"]["tasks"];print([t["id"] for t in d if t["title"]=="测试任务一"][0])')
echo "task id = $T1"
curl -s -b /tmp/ih-admin.txt -X PATCH $BASE/api/tasks/$T1 -H 'Content-Type: application/json' -d '{"status":"doing","due":"","note":""}' | J
curl -s -b /tmp/ih-admin.txt $BASE/api/tasks | J
curl -s -b /tmp/ih-admin.txt -X DELETE $BASE/api/tasks/$T1 | J

echo "== dashboard（首次，应触发/呈现扫描状态） =="
curl -s -b /tmp/ih-admin.txt $BASE/api/dashboard | J
echo "== POST /api/scan =="
curl -s -b /tmp/ih-admin.txt -X POST $BASE/api/scan -H 'Content-Type: application/json' -d '{}' | J
sleep 3
echo "== dashboard 扫描后（假凭证预期 error 或 0 值） =="
curl -s -b /tmp/ih-admin.txt $BASE/api/dashboard | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];print("scan:",json.dumps(d.get("scan"),ensure_ascii=False)[:200]);print("stats:",json.dumps(d.get("stats"),ensure_ascii=False)[:200]);print("by_account:",json.dumps(d.get("by_account"),ensure_ascii=False)[:300]);print("by_type:",json.dumps(d.get("by_type"),ensure_ascii=False)[:200]);print("recent:",json.dumps(d.get("recent"),ensure_ascii=False)[:200])'

echo "== MCP：initialize / tools/list / tools/call =="
TOK=$(curl -s -b /tmp/ih-admin.txt $BASE/api/me/mcp-token | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["token"])')
curl -s -X POST $BASE/mcp -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' | J
curl -s -X POST $BASE/mcp -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | python3 -c 'import json,sys;d=json.load(sys.stdin);print("tools:",len(d["result"]["tools"]));print([t["name"] for t in d["result"]["tools"]])'
curl -s -X POST $BASE/mcp -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ima_test","arguments":{"account":"测试号A"}}}' | J

echo "== 权限：普通用户 =="
curl -s -b /tmp/ih-admin.txt -X POST $BASE/api/admin/users -H 'Content-Type: application/json' -d '{"username":"alice","password":"alice123","role":"user"}' | J
curl -s -c /tmp/ih-alice.txt -X POST $BASE/api/login -H 'Content-Type: application/json' -d '{"username":"alice","password":"alice123"}' | J
curl -s -b /tmp/ih-alice.txt $BASE/api/admin/users | J
curl -s -b /tmp/ih-alice.txt $BASE/api/tasks | J
echo "== alice 无凭证下 ima 探测 =="
curl -s -b /tmp/ih-alice.txt "$BASE/api/ima/kbs?q=x" | J
echo "== DELETE /api/me/ima?id= 删单个 =="
ACC2=$(curl -s -b /tmp/ih-admin.txt $BASE/api/ima/accounts | python3 -c 'import json,sys;a=json.load(sys.stdin)["data"]["accounts"];print([x["id"] for x in a if x["source"]!="server_default"][-1])')
curl -s -b /tmp/ih-admin.txt -X DELETE "$BASE/api/me/ima?id=$ACC2" | J
curl -s -b /tmp/ih-admin.txt $BASE/api/ima/accounts | J
echo "== DONE =="
