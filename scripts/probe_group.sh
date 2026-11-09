#!/usr/bin/env bash
# 群组探针：建群邀请 → 任务/日程双向广播 → LWW 合并 → 删除墓碑 → 退组/解散收敛。
# 前置：go build -o /tmp/imahub-probe-bin .
set -u -o pipefail
BIN=/tmp/imahub-probe-bin
A=http://127.0.0.1:18201
B=http://127.0.0.1:18202
DA=/tmp/ihg-a
DB=/tmp/ihg-b
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

[ -x "$BIN" ] || { echo "缺少 $BIN"; exit 2; }
command -v python3 >/dev/null || { echo "缺少 python3"; exit 2; }

PA=""; PB=""
cleanup() {
  [ -n "$PA" ] && kill "$PA" 2>/dev/null
  [ -n "$PB" ] && kill "$PB" 2>/dev/null
  wait "$PA" "$PB" 2>/dev/null
  rm -rf "$DA" "$DB" /tmp/ihg*.jar
}
trap cleanup EXIT INT TERM
rm -rf "$DA" "$DB" /tmp/ihg*.jar
mkdir -p "$DA" "$DB"

start_node() {
  PEER_LAN_PORT="$3" PEER_LAN_EXTRA="$4" "$BIN" serve --addr "$1" --data "$2" > "$5" 2>&1 &
  local pid=$!
  for _ in $(seq 1 60); do
    curl -s --max-time 1 "http://$1/healthz" 2>/dev/null | grep -q '"ok":true' && { echo "$pid"; return 0; }
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.5
  done
  echo "节点启动失败: $1" >&2
  echo "$pid"
  return 1
}

PA=$(start_node 127.0.0.1:18201 "$DA" 18211 127.0.0.1:18212 /tmp/ihg-a.log) || exit 2
PB=$(start_node 127.0.0.1:18202 "$DB" 18212 127.0.0.1:18211 /tmp/ihg-b.log) || exit 2

JA=/tmp/ihg-a.jar; JB=/tmp/ihg-b.jar
curl -s -c $JA -X POST $A/api/setup -H 'Content-Type: application/json' -d '{"username":"grp_a","password":"grpa12345"}' >/dev/null
curl -s -c $JB -X POST $B/api/setup -H 'Content-Type: application/json' -d '{"username":"grp_b","password":"grpb12345"}' >/dev/null

IDB=$(curl -s -b $JB $B/api/peers/me | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
CODE=$(curl -s -b $JB -X POST $B/api/peers/me/invite -H 'Content-Type: application/json' -d '{"addrs":["127.0.0.1:18202"]}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['code'])" 2>/dev/null)
curl -s -b $JA -X POST $A/api/peers/invite -H 'Content-Type: application/json' -d "{\"code\":\"$CODE\"}" >/dev/null
IDA=$(curl -s -b $JA $A/api/peers/me | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
CODE2=$(curl -s -b $JA -X POST $A/api/peers/me/invite -H 'Content-Type: application/json' -d '{"addrs":["127.0.0.1:18201"]}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['code'])" 2>/dev/null)
curl -s -b $JB -X POST $B/api/peers/invite -H 'Content-Type: application/json' -d "{\"code\":\"$CODE2\"}" >/dev/null
ok "双节点互信就绪"

echo "== 建群邀请 =="
GID=$(curl -s -b $JA -X POST $A/api/groups -H 'Content-Type: application/json' \
  -d "{\"name\":\"学习小组\",\"members\":[\"$IDB\"]}" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['group']['id'])" 2>/dev/null)
[ -n "$GID" ] && ok "A 建群成功（$GID）" || bad "建群失败"
got=""
for _ in $(seq 1 60); do
  got=$(curl -s -b $JB $B/api/groups | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['groups']
print('yes' if any(g['id']=='$GID' for g in d) else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "B 收到群邀请即入组" || bad "B 未入组"

echo "== 任务/日程双向广播 =="
TID=$(curl -s -b $JA -X POST $A/api/tasks -H 'Content-Type: application/json' \
  -d "{\"title\":\"小组任务一\",\"group_id\":\"$GID\"}" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['task']['id'])" 2>/dev/null)
got=""
for _ in $(seq 1 60); do
  got=$(curl -s -b $JB "$B/api/groups/$GID/feed" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['tasks']
m=[t for t in d if t['id']=='$TID']
print('yes' if m and m[0]['title']=='小组任务一' else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "A 建任务 → B 群内可见" || bad "任务广播失败"

curl -s -b $JB -X PATCH $B/api/tasks/$TID -H 'Content-Type: application/json' -d '{"status":"doing"}' >/dev/null
got=""
for _ in $(seq 1 60); do
  got=$(curl -s -b $JA "$A/api/groups/$GID/feed" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['tasks']
m=[t for t in d if t['id']=='$TID']
print('yes' if m and m[0]['status']=='doing' else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
if [ -n "$got" ]; then ok "B 改状态 → A 收敛（LWW）"; else
  bad "状态收敛失败"
  echo "--- 诊断：A feed ---"; curl -s -b $JA "$A/api/groups/$GID/feed" | head -c 600; echo
fi

EID=$(curl -s -b $JB -X POST $B/api/events -H 'Content-Type: application/json' \
  -d "{\"title\":\"小组会议\",\"start_at\":1893456000,\"group_id\":\"$GID\"}" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['event']['id'])" 2>/dev/null)
got=""
for _ in $(seq 1 60); do
  got=$(curl -s -b $JA "$A/api/groups/$GID/feed" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['events']
print('yes' if any(e['id']=='$EID' for e in d) else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "B 建日程 → A 群内可见" || bad "日程广播失败"

echo "== 删除墓碑收敛 =="
curl -s -b $JA -X DELETE $A/api/tasks/$TID >/dev/null
got=""
for _ in $(seq 1 60); do
  got=$(curl -s -b $JB "$B/api/groups/$GID/feed" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['tasks']
print('yes' if not any(t['id']=='$TID' for t in d) else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "A 删任务 → B 墓碑收敛（条目消失）" || bad "删除收敛失败"

echo "== 退组与解散 =="
curl -s -b $JB -X POST $B/api/groups/$GID/leave >/dev/null
got=""
for _ in $(seq 1 60); do
  got=$(curl -s -b $JA "$A/api/groups/$GID/feed" | python3 -c "
import json,sys
g=json.load(sys.stdin)['data']['group']
print('yes' if len(g['members'])==1 else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "B 退组 → A 侧成员收敛为 1" || bad "退组收敛失败"

curl -s -b $JA -X DELETE $A/api/groups/$GID >/dev/null
got=""
for _ in $(seq 1 60); do
  got=$(curl -s -b $JB $B/api/groups | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['groups']
print('yes' if not any(g['id']=='$GID' for g in d) else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
if [ -n "$got" ]; then ok "A 解散 → B 侧群组消失（终局墓碑）"; else
  bad "解散收敛失败"
  echo "--- 诊断：B groups ---"; curl -s -b $JB $B/api/groups | head -c 600; echo
fi

echo
echo "探针结果: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ] && { echo ALL_GREEN; exit 0; } || exit 1
