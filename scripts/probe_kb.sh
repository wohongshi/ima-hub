#!/usr/bin/env bash
# 知识库代理访问探针：人人都是客户端，同时人人都是服务端。
# 双节点 + ima mock：B 配 mock 账号当服务端，A 经申请制当客户端——
# 三档授权逐档放行、白名单限库（含 media↔kb 绑定防绕过）、撤销/拒绝即断、敌意输入防护。
# 前置：go build -o /tmp/imahub-probe-bin . ；mock2.py 随仓库（scripts/mock2.py，可用 MOCK= 覆盖）。
set -u -o pipefail
BIN=/tmp/imahub-probe-bin
MOCK=${MOCK:-$(dirname "$0")/mock2.py}
A=http://127.0.0.1:18201
B=http://127.0.0.1:18202
DA=/tmp/ihk-a
DB=/tmp/ihk-b
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

# 业务错误断言：JSON 解析（grep raw 会因空格/措辞假通过）
biz_err() { # response 期望msg片段 标签
  echo "$1" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('code')!=0 and '''$2''' in d.get('msg',''), d" 2>/dev/null && ok "$3" || bad "$3: $1"
}

[ -x "$BIN" ] || { echo "缺少 $BIN（先 go build -o $BIN .）"; exit 2; }
command -v python3 >/dev/null || { echo "缺少 python3（断言依赖它解析 JSON）"; exit 2; }
[ -f "$MOCK" ] || { echo "缺少 mock（$MOCK，可用 MOCK= 指定）"; exit 2; }

PA=""; PB=""
cleanup() {
  [ -n "$PA" ] && kill "$PA" 2>/dev/null
  [ -n "$PB" ] && kill "$PB" 2>/dev/null
  wait "$PA" "$PB" 2>/dev/null
  rm -rf "$DA" "$DB" /tmp/ihk*.jar /tmp/ihk-dl.bin
}
trap cleanup EXIT INT TERM

rm -rf "$DA" "$DB" /tmp/ihk*.jar /tmp/ihk-dl.bin
mkdir -p "$DA" "$DB"

# mock 不在则拉起（共享服务，探针不杀它）
if ! curl -s --max-time 1 http://127.0.0.1:18094/file/ping >/dev/null 2>&1; then
  nohup python3 "$MOCK" > /tmp/ihk-mock.log 2>&1 &
  sleep 1
fi

start_node() { # addr data lanport extra logfile ima_base
  PEER_LAN_PORT="$3" PEER_LAN_EXTRA="$4" "$BIN" serve --addr "$1" --data "$2" ${6:+--ima-base-url "$6"} > "$5" 2>&1 &
  local pid=$!
  for _ in $(seq 1 30); do
    curl -s --max-time 1 "http://$1/healthz" 2>/dev/null | grep -q '"ok":true' && { echo "$pid"; return 0; }
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.5
  done
  echo "节点启动失败: $1（日志 $5）" >&2
  tail -n 20 "$5" >&2
  echo "$pid"
  return 1
}

PA=$(start_node 127.0.0.1:18201 "$DA" 18211 127.0.0.1:18212 /tmp/ihk-a.log "") || exit 2
PB=$(start_node 127.0.0.1:18202 "$DB" 18212 127.0.0.1:18211 /tmp/ihk-b.log "http://127.0.0.1:18094") || exit 2

JA=/tmp/ihk-a.jar; JB=/tmp/ihk-b.jar
curl -s -c $JA -X POST $A/api/setup -H 'Content-Type: application/json' -d '{"username":"kb_a","password":"kba123456"}' >/dev/null
curl -s -c $JB -X POST $B/api/setup -H 'Content-Type: application/json' -d '{"username":"kb_b","password":"kbb123456"}' >/dev/null

IDA=$(curl -s -b $JA $A/api/peers/me | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
IDB=$(curl -s -b $JB $B/api/peers/me | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
[ -n "$IDA" ] && [ -n "$IDB" ] && [ "$IDA" != "$IDB" ] && ok "双节点身份就绪" || bad "身份获取异常: A=$IDA B=$IDB"

echo "== B 绑定 ima 账号（mock 指向） =="
R=$(curl -s -b $JB -X POST $B/api/me/ima -H 'Content-Type: application/json' -d '{"name":"mock号","client_id":"mock-cid","api_key":"mock-key"}')
echo "$R" | grep -q '"code":0' && ok "B 绑定 ima 账号" || bad "绑定失败: $R"

echo "== 邀请互信（双表就位） =="
CODE_B=$(curl -s -b $JB -X POST $B/api/peers/me/invite -H 'Content-Type: application/json' -d '{"addrs":["127.0.0.1:18202"]}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['code'])" 2>/dev/null)
curl -s -b $JA -X POST $A/api/peers/invite -H 'Content-Type: application/json' -d "{\"code\":\"$CODE_B\"}" >/dev/null
CODE_A=$(curl -s -b $JA -X POST $A/api/peers/me/invite -H 'Content-Type: application/json' -d '{"addrs":["127.0.0.1:18201"]}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['code'])" 2>/dev/null)
curl -s -b $JB -X POST $B/api/peers/invite -H 'Content-Type: application/json' -d "{\"code\":\"$CODE_A\"}" >/dev/null
R=$(curl -s -b $JB $B/api/peers)
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['peers']
m=[p for p in d if p['id']=='$IDA']
assert m and m[0].get('trusted'), d
" 2>/dev/null && ok "互信就位（B 表里有 A，trusted）" || bad "互信异常: $R"

# 申请→等待送达→审批（送达与审批都验，失败即中止——不给级联假红）
req_and_approve() { # scopes_json kbs_json approve_scopes_json -> 输出 request_id
  local rid delivered=""
  rid=$(curl -s -b $JA -X POST $A/api/access/requests -H 'Content-Type: application/json' \
    -d "{\"to\":\"$IDB\",\"scopes\":$1,\"kbs\":$2}" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['request']['id'])" 2>/dev/null)
  [ -n "$rid" ] || { echo "申请创建失败" >&2; return 1; }
  for _ in $(seq 1 15); do
    curl -s -b $JB "$B/api/access/requests?dir=received" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['requests']
assert any(r['id']=='$rid' and r['status']=='pending' for r in d)" 2>/dev/null && { delivered=1; break; }
    sleep 1
  done
  [ -n "$delivered" ] || { echo "申请未送达 B: $rid" >&2; return 1; }
  curl -s -b $JB -X POST $B/api/access/requests/$rid/decide -H 'Content-Type: application/json' \
    -d "{\"action\":\"approve\",\"scopes\":$3}" | grep -q '"code":0' \
    || { echo "审批失败: $rid" >&2; return 1; }
  echo "$rid"
}

echo "== 第一档：仅 search（白名单限库 kb1） =="
RID1=$(req_and_approve '["search","browse","download"]' '["kb1"]' '["search"]') || exit 2
R=$(curl -s -b $JA -X POST $A/api/peers/$IDB/kb/search -H 'Content-Type: application/json' -d '{"q":"数学"}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert any(k['id']=='kb1' for k in d['kbs']), d
assert all(k['id']!='kb2' for k in d['kbs']), d
" 2>/dev/null && ok "跨库检索命中 kb1 且白名单滤掉 kb2" || bad "跨库检索/白名单异常: $R"
R=$(curl -s -b $JA -X POST $A/api/peers/$IDB/kb/search -H 'Content-Type: application/json' -d '{"q":"数学","kb":"kb1"}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
ids=[f['media_id'] for f in d['files']]
assert 'm_a' in ids and 'm_d' in ids, d
" 2>/dev/null && ok "库内检索命中 m_a/m_d" || bad "库内检索异常: $R"
R=$(curl -s -b $JA $A/api/peers/$IDB/kbs)
biz_err "$R" "browse" "kb.list 被拒（browse 未授权）"

echo "== 第二档：+ browse（限库 kb1） =="
RID2=$(req_and_approve '["browse"]' '["kb1"]' '["browse"]') || exit 2
R=$(curl -s -b $JA $A/api/peers/$IDB/kbs)
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert any(k['id']=='kb1' and k['name']=='测试知识库' for k in d['kbs']), d
assert all(k['id']!='kb2' for k in d['kbs']), d
" 2>/dev/null && ok "kb.list 命中 kb1 且滤掉 kb2" || bad "kb.list/白名单异常: $R"
R=$(curl -s -b $JA -X POST $A/api/peers/$IDB/kb/browse -H 'Content-Type: application/json' -d '{"kb":"kb1"}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
fo=[f['folder_id'] for f in d['folders']]
fi=[f['media_id'] for f in d['files']]
assert 'f1' in fo, d
assert set(fi)>={'m_a','m_b','m_c'}, d
" 2>/dev/null && ok "根目录浏览（f1 + 3 文件）" || bad "根目录浏览异常: $R"
R=$(curl -s -b $JA -X POST $A/api/peers/$IDB/kb/browse -H 'Content-Type: application/json' -d '{"kb":"kb1","folder":"f1"}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
fi=[f['media_id'] for f in d['files']]
assert set(fi)>={'m_d','m_e'}, d
" 2>/dev/null && ok "子目录浏览（d.md/e.txt）" || bad "子目录浏览异常: $R"
R=$(curl -s -b $JA -X POST $A/api/peers/$IDB/kb/search -H 'Content-Type: application/json' -d '{"q":"数学","kb":"kb1"}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert any(f['media_id']=='m_a' for f in d['files']), d
" 2>/dev/null && ok "多申请并集：browse 批准后 search 仍生效" || bad "并集回归: $R"
R=$(curl -s -b $JA "$A/api/peers/$IDB/kb/download?kb=kb1&media=m_a&name=a.pdf")
biz_err "$R" "download" "下载仍被拒（download 未授权）"

echo "== 第三档：+ download（限库 kb1） =="
RID3=$(req_and_approve '["download"]' '["kb1"]' '["download"]') || exit 2
curl -s -f -b $JA "$A/api/peers/$IDB/kb/download?kb=kb1&media=m_a&name=a.pdf" -o /tmp/ihk-dl.bin \
  && grep -q "mock 文件内容 m_a" /tmp/ihk-dl.bin 2>/dev/null \
  && ok "代理下载内容正确（HTTP 200 + m_a 正文）" || bad "下载内容异常: $(head -c 80 /tmp/ihk-dl.bin 2>/dev/null)"
R=$(curl -s -b $JA "$A/api/peers/$IDB/kb/download?kb=kb2&media=m_a&name=a.pdf")
biz_err "$R" "未授权该知识库" "越库下载被拒（白名单 kb1）"
R=$(curl -s -b $JA "$A/api/peers/$IDB/kb/download?kb=kb1&media=m_x&name=x.pdf")
biz_err "$R" "文件不在授权知识库内" "跨库 media 绑定绕过被拒（kb1 配他人 media）"
R=$(curl -s -b $JA "$A/api/peers/$IDB/kb/download?kb=kb1&media=m_zzz&name=z.pdf")
biz_err "$R" "" "未知 media 干净报错（非 panic/非空文件）"

echo "== 敌意文件名防护 =="
H=$(curl -s -i -b $JA "$A/api/peers/$IDB/kb/download?kb=kb1&media=m_a&name=..%2F..%2Fetc%2Fpasswd" | tr -d '\r')
echo "$H" | grep -qi "filename\*=" && echo "$H" | grep -q "passwd" && ! echo "$H" | grep -q "\.\./" \
  && ok "路径穿越名被净化（Content-Disposition 无 ../）" || bad "路径穿越名净化失败"
H=$(curl -s -i -b $JA "$A/api/peers/$IDB/kb/download?kb=kb1&media=m_a&name=a%0d%0aX-Injected:1" | tr -d '\r')
! echo "$H" | grep -qi "^X-Injected:" && ok "CRLF 头注入被拒（无 X-Injected 头）" || bad "头注入穿透"

echo "== 撤销即断 =="
curl -s -b $JB -X POST $B/api/access/requests/$RID3/decide -H 'Content-Type: application/json' -d '{"action":"revoke"}' >/dev/null
R=$(curl -s -b $JA "$A/api/peers/$IDB/kb/download?kb=kb1&media=m_a&name=a.pdf")
biz_err "$R" "download" "撤销后下载即断"

echo "== 拒绝路径（denied 回传 + 不误伤已有授权） =="
RID4=$(curl -s -b $JA -X POST $A/api/access/requests -H 'Content-Type: application/json' \
  -d "{\"to\":\"$IDB\",\"scopes\":[\"search\"]}" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['request']['id'])" 2>/dev/null)
for _ in $(seq 1 15); do
  curl -s -b $JB "$B/api/access/requests?dir=received" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['requests']
assert any(r['id']=='$RID4' and r['status']=='pending' for r in d)" 2>/dev/null && break
  sleep 1
done
R=$(curl -s -b $JB -X POST $B/api/access/requests/$RID4/decide -H 'Content-Type: application/json' -d '{"action":"deny"}')
echo "$R" | grep -q '"code":0' && ok "拒绝操作返回成功" || bad "拒绝操作异常: $R"
got=""
for _ in $(seq 1 15); do
  got=$(curl -s -b $JA $A/api/access/requests | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['requests']
m=[r for r in d if r['id']=='$RID4']
print('yes' if m and m[0]['status']=='denied' else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "拒绝回传（denied）" || bad "拒绝未回传 A"
R=$(curl -s -b $JA -X POST $A/api/peers/$IDB/kb/search -H 'Content-Type: application/json' -d '{"q":"数学","kb":"kb1"}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert any(f['media_id']=='m_a' for f in d['files']), d
" 2>/dev/null && ok "拒绝不误伤已有 search 授权" || bad "拒绝误伤已有授权: $R"

echo "== 边角 =="
R=$(curl -s -b $JA "$A/api/peers/no-such-peer/kbs")
echo "$R" | grep -q '"code":400' && ok "未知节点查询 400" || bad "未知节点未拒: $R"

echo
echo "探针结果: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ] && { echo ALL_GREEN; exit 0; } || exit 1
