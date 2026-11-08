#!/usr/bin/env bash
# P2P 双节点探针：局域网宣告/应答发现 + 邀请码 + 昵称 + 删除 + 非法邀请码拒绝
# 前置：go build -o /tmp/imahub-probe-bin .  （无需 mock，纯 P2P 面）
set -u -o pipefail
BIN=/tmp/imahub-probe-bin
A=http://127.0.0.1:18201
B=http://127.0.0.1:18202
DA=/tmp/ihp-a
DB=/tmp/ihp-b
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

[ -x "$BIN" ] || { echo "缺少 $BIN（先 go build -o $BIN .）"; exit 2; }

PA=""; PB=""
cleanup() {
  [ -n "$PA" ] && kill "$PA" 2>/dev/null
  [ -n "$PB" ] && kill "$PB" 2>/dev/null
  wait "$PA" "$PB" 2>/dev/null
  rm -rf "$DA" "$DB" /tmp/ihp*.jar
}
trap cleanup EXIT

rm -rf "$DA" "$DB" /tmp/ihp*.jar
mkdir -p "$DA" "$DB"

start_node() { # addr data port extra logfile var
  PEER_LAN_PORT="$3" PEER_LAN_EXTRA="$4" "$BIN" serve --addr "$1" --data "$2" > "$5" 2>&1 &
  local pid=$!
  for _ in $(seq 1 30); do
    curl -s --max-time 1 "http://$1/healthz" 2>/dev/null | grep -q '"ok":true' && break
    sleep 0.5
  done
  echo "$pid"
}

PA=$(start_node 127.0.0.1:18201 "$DA" 18211 127.0.0.1:18212 /tmp/ihp-a.log)
PB=$(start_node 127.0.0.1:18202 "$DB" 18212 127.0.0.1:18211 /tmp/ihp-b.log)

JA=/tmp/ihp-a.jar; JB=/tmp/ihp-b.jar
curl -s -c $JA -X POST $A/api/setup -H 'Content-Type: application/json' -d '{"username":"peer_a","password":"peera12345"}' >/dev/null
curl -s -c $JB -X POST $B/api/setup -H 'Content-Type: application/json' -d '{"username":"peer_b","password":"peerb12345"}' >/dev/null

IDA=$(curl -s -b $JA $A/api/peers/me | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
IDB=$(curl -s -b $JB $B/api/peers/me | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
[ -n "$IDA" ] && [ -n "$IDB" ] && [ "$IDA" != "$IDB" ] && ok "双节点身份就绪（$IDA vs $IDB）" || bad "身份获取异常: A=$IDA B=$IDB"

echo "== 局域网发现（宣告/应答两段式） =="
found_ab=""; found_ba=""
for _ in $(seq 1 12); do
  found_ab=$(curl -s -b $JA $A/api/peers | python3 -c "import json,sys;d=json.load(sys.stdin)['data']['peers'];print('yes' if any(p['id']=='$IDB' for p in d) else '')" 2>/dev/null)
  found_ba=$(curl -s -b $JB $B/api/peers | python3 -c "import json,sys;d=json.load(sys.stdin)['data']['peers'];print('yes' if any(p['id']=='$IDA' for p in d) else '')" 2>/dev/null)
  [ -n "$found_ab" ] && [ -n "$found_ba" ] && break
  sleep 1
done
[ -n "$found_ab" ] && ok "A 发现了 B" || bad "A 未发现 B"
[ -n "$found_ba" ] && ok "B 发现了 A" || bad "B 未发现 A"
ONLINE=$(curl -s -b $JA $A/api/peers | python3 -c "import json,sys;d=json.load(sys.stdin)['data']['peers'];m=[p for p in d if p['id']=='$IDB'];print('yes' if m and m[0].get('online') else '')" 2>/dev/null)
[ -n "$ONLINE" ] && ok "在线状态（online=true）" || bad "在线状态缺失"

echo "== 邀请码（含地址，60 天有效） =="
CODE=$(curl -s -b $JA -X POST $A/api/peers/me/invite -H 'Content-Type: application/json' -d '{"addrs":["10.9.9.9:29821"]}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['code'])" 2>/dev/null)
case "$CODE" in IH1.*) ok "邀请码生成（${#CODE} 字符）" ;; *) bad "邀请码异常: $CODE" ;; esac
R=$(curl -s -b $JB -X POST $B/api/peers/invite -H 'Content-Type: application/json' -d "{\"code\":\"$CODE\"}")
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['peer']
assert d['id']=='$IDA', d
assert d['trusted'] is True, d
assert d['source']=='invite', d
assert d['addr']=='10.9.9.9:29821', d
" 2>/dev/null && ok "凭邀请码信任入库（ID/信任/来源/地址全对）" || bad "邀请码入库异常: $R"

R=$(curl -s -b $JB -X POST $B/api/peers/invite -H 'Content-Type: application/json' -d '{"code":"IH1.ZZZ"}')
echo "$R" | grep -q '"code":400' && ok "非法邀请码被拒（400）" || bad "非法邀请码未拒: $R"

echo "== 昵称与删除 =="
R=$(curl -s -b $JB -X PATCH $B/api/peers/me -H 'Content-Type: application/json' -d '{"nickname":"乙B改"}')
echo "$R" | python3 -c "import json,sys;d=json.load(sys.stdin)['data'];assert d['nickname']=='乙B改',d" 2>/dev/null && ok "昵称更新" || bad "昵称异常: $R"
curl -s -b $JA -X DELETE $A/api/peers/$IDB >/dev/null
R=$(curl -s -b $JA $A/api/peers)
echo "$R" | python3 -c "import json,sys;d=json.load(sys.stdin)['data']['peers'];assert not any(p['id']=='$IDB' for p in d)" 2>/dev/null && ok "删除节点生效" || bad "删除后仍在: $R"

echo "== 访问申请制（RPC 加密通道） =="
# 上一节刚把 B 从 A 表里删掉——先凭邀请码加回（顺带覆盖邀请卡携带加密公钥链路）
CODE2=$(curl -s -b $JB -X POST $B/api/peers/me/invite -H 'Content-Type: application/json' -d '{"addrs":["127.0.0.1:18202"]}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['code'])" 2>/dev/null)
curl -s -b $JA -X POST $A/api/peers/invite -H 'Content-Type: application/json' -d "{\"code\":\"$CODE2\"}" >/dev/null
REQ=$(curl -s -b $JA -X POST $A/api/access/requests -H 'Content-Type: application/json' -d "{\"to\":\"$IDB\",\"scopes\":[\"search\",\"download\"],\"note\":\"想借你的知识库\"}")
RID=$(echo "$REQ" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['request']['id'])" 2>/dev/null)
echo "$REQ" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert d['request']['status']=='pending', d
assert d['delivery']=='queued', d
assert d['request']['id'], d
" 2>/dev/null && ok "A 发起申请（pending/queued，ID 非空）" || bad "发起申请异常: $REQ"
got=""
for _ in $(seq 1 15); do
  got=$(curl -s -b $JB "$B/api/access/requests?dir=received" | python3 -c "import json,sys;d=json.load(sys.stdin)['data']['requests'];print('yes' if any(r['id']=='$RID' and r['status']=='pending' for r in d) else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "B 侧收到申请（加密通道送达）" || bad "B 侧未收到申请 $RID"

curl -s -b $JB -X POST $B/api/access/requests/$RID/decide -H 'Content-Type: application/json' -d '{"action":"approve","scopes":["search"]}' >/dev/null
got=""
for _ in $(seq 1 15); do
  got=$(curl -s -b $JA $A/api/access/requests | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['requests']
m=[r for r in d if r['id']=='$RID']
print('yes' if m and m[0]['status']=='approved' and m[0]['scopes']==['search'] else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "A 侧收到审批（approved，范围收窄为 search）" || bad "审批未回传 A"

echo "== 掉线容错：离线补投 =="
kill $PB 2>/dev/null
PB=""
sleep 1
REQ2=$(curl -s -b $JA -X POST $A/api/access/requests -H 'Content-Type: application/json' -d "{\"to\":\"$IDB\",\"scopes\":[\"browse\"],\"note\":\"掉线也不阻塞\"}")
RID2=$(echo "$REQ2" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['request']['id'])" 2>/dev/null)
[ -n "$RID2" ] && ok "B 掉线期间 A 发起申请不阻塞（$RID2）" || bad "掉线发申请异常: $REQ2"
PB=$(start_node 127.0.0.1:18202 "$DB" 18212 127.0.0.1:18211 /tmp/ihp-b.log)
got=""
for _ in $(seq 1 20); do
  got=$(curl -s -b $JB "$B/api/access/requests?dir=received" | python3 -c "import json,sys;d=json.load(sys.stdin)['data']['requests'];print('yes' if any(r['id']=='$RID2' for r in d) else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "B 回归后自动补投送达" || bad "补投未送达 $RID2"

echo
echo "探针结果: PASS=$PASS FAIL=$FAIL"
if [ "$FAIL" -eq 0 ]; then echo "ALL_GREEN"; else echo "HAS_FAILURE"; exit 1; fi
