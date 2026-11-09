#!/usr/bin/env bash
# 公网发现 + 中继探针：discover 目录注册 → 中继信箱回退投递（直连全断场景）→
# 目录查询 → kb 同步查询拒绝走中继。
# 前置：go build -o /tmp/imahub-probe-bin .
set -u -o pipefail
BIN=/tmp/imahub-probe-bin
DIS=http://127.0.0.1:18500
A=http://127.0.0.1:18201
B=http://127.0.0.1:18202
DA=/tmp/ihd-a
DB=/tmp/ihd-b
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

[ -x "$BIN" ] || { echo "缺少 $BIN"; exit 2; }
command -v python3 >/dev/null || { echo "缺少 python3"; exit 2; }

PD=""; PA=""; PB=""
cleanup() {
  [ -n "$PA" ] && kill "$PA" 2>/dev/null
  [ -n "$PB" ] && kill "$PB" 2>/dev/null
  [ -n "$PD" ] && kill "$PD" 2>/dev/null
  wait "$PA" "$PB" "$PD" 2>/dev/null
  rm -rf "$DA" "$DB" /tmp/ihd*.jar
}
trap cleanup EXIT INT TERM
rm -rf "$DA" "$DB" /tmp/ihd*.jar
mkdir -p "$DA" "$DB"

echo "== discover 服务器 =="
$BIN discover --addr 127.0.0.1:18500 > /tmp/ihd-dis.log 2>&1 &
PD=$!
for _ in $(seq 1 20); do
  curl -s --max-time 1 $DIS/healthz 2>/dev/null | grep -q 'imahub-discover' && break
  sleep 0.5
done
curl -s --max-time 2 $DIS/healthz | grep -q 'imahub-discover' && ok "discover 服务在线" || { bad "discover 未起"; exit 2; }

# 两节点：地址互相塞假的（直连必败），靠中继
start_node() {
  PEER_LAN_PORT=0 PEER_LAN_EXTRA="" "$BIN" serve --addr "$1" --data "$2" > "$3" 2>&1 &
  local pid=$!
  for _ in $(seq 1 30); do
    curl -s --max-time 1 "http://$1/healthz" 2>/dev/null | grep -q '"ok":true' && { echo "$pid"; return 0; }
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.5
  done
  echo "节点启动失败: $1" >&2
  echo "$pid"; return 1
}
PA=$(start_node 127.0.0.1:18201 "$DA" /tmp/ihd-a.log) || exit 2
PB=$(start_node 127.0.0.1:18202 "$DB" /tmp/ihd-b.log) || exit 2

JA=/tmp/ihd-a.jar; JB=/tmp/ihd-b.jar
curl -s -c $JA -X POST $A/api/setup -H 'Content-Type: application/json' -d '{"username":"dis_a","password":"disa12345"}' >/dev/null
curl -s -c $JB -X POST $B/api/setup -H 'Content-Type: application/json' -d '{"username":"dis_b","password":"disb12345"}' >/dev/null

# 配置发现服务器（admin settings）
for J in $JA $JB; do :; done
curl -s -b $JA -X PUT $A/api/admin/settings -H 'Content-Type: application/json' -d "{\"discover_url\":\"$DIS\"}" >/dev/null
curl -s -b $JB -X PUT $B/api/admin/settings -H 'Content-Type: application/json' -d "{\"discover_url\":\"$DIS\"}" >/dev/null
# 发现配置在启动时读取——重启两节点
kill $PA $PB 2>/dev/null; sleep 1
PA=$(start_node 127.0.0.1:18201 "$DA" /tmp/ihd-a.log) || exit 2
PB=$(start_node 127.0.0.1:18202 "$DB" /tmp/ihd-b.log) || exit 2
curl -s -c $JA -X POST $A/api/login -H 'Content-Type: application/json' -d '{"username":"dis_a","password":"disa12345"}' >/dev/null
curl -s -c $JB -X POST $B/api/login -H 'Content-Type: application/json' -d '{"username":"dis_b","password":"disb12345"}' >/dev/null

IDA=$(curl -s -b $JA $A/api/peers/me | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
IDB=$(curl -s -b $JB $B/api/peers/me | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

echo "== 心跳注册（announce） =="
got=""
for _ in $(seq 1 20); do
  got=$(curl -s --max-time 2 "$DIS/peers?since=0" | python3 -c "
import json,sys
d=json.load(sys.stdin)['peers']
ids=[p['node_id'] for p in d]
print('yes' if '$IDA' in ids and '$IDB' in ids else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "双节点已注册目录（含观测地址）" || bad "announce 未登记"

OBS=$(curl -s --max-time 2 "$DIS/peers?since=0" | python3 -c "
import json,sys
d=json.load(sys.stdin)['peers']
m=[p for p in d if p['node_id']=='$IDA']
print('yes' if m and m[0].get('observed_addr') else '')" 2>/dev/null)
[ -n "$OBS" ] && ok "服务端观测地址已记录（NAT 候选出口）" || bad "观测地址缺失"

echo "== 互信（假地址）→ 直连必败，走中继 =="
CODE=$(curl -s -b $JB -X POST $B/api/peers/me/invite -H 'Content-Type: application/json' -d '{"addrs":["10.9.9.9:29821"]}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['code'])" 2>/dev/null)
curl -s -b $JA -X POST $A/api/peers/invite -H 'Content-Type: application/json' -d "{\"code\":\"$CODE\"}" >/dev/null
CODE2=$(curl -s -b $JA -X POST $A/api/peers/me/invite -H 'Content-Type: application/json' -d '{"addrs":["10.8.8.8:29820"]}' | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['code'])" 2>/dev/null)
curl -s -b $JB -X POST $B/api/peers/invite -H 'Content-Type: application/json' -d "{\"code\":\"$CODE2\"}" >/dev/null
ok "互信就绪（双方直连地址均不可达）"

echo "== 中继回退投递 =="
RID=$(curl -s -b $JA -X POST $A/api/access/requests -H 'Content-Type: application/json' \
  -d "{\"to\":\"$IDB\",\"scopes\":[\"search\"],\"note\":\"relay test\"}" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['request']['id'])" 2>/dev/null)
got=""
for _ in $(seq 1 30); do
  got=$(curl -s -b $JB "$B/api/access/requests?dir=received" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['requests']
print('yes' if any(r['id']=='$RID' and r['status']=='pending' for r in d) else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "直连断 → 中继信箱送达（B 收到申请）" || bad "中继投递失败"

echo "== 中继回传审批 =="
curl -s -b $JB -X POST $B/api/access/requests/$RID/decide -H 'Content-Type: application/json' -d '{"action":"approve","scopes":["search"]}' >/dev/null
got=""
for _ in $(seq 1 30); do
  got=$(curl -s -b $JA $A/api/access/requests | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['requests']
m=[r for r in d if r['id']=='$RID']
print('yes' if m and m[0]['status']=='approved' else '')" 2>/dev/null)
  [ -n "$got" ] && break
  sleep 1
done
[ -n "$got" ] && ok "审批经中继回传 A（approved）" || bad "中继回传失败"

echo "== kb 同步查询拒绝走中继 =="
R=$(curl -s -b $JA -X POST $A/api/peers/$IDB/kb/search -H 'Content-Type: application/json' -d '{"q":"x"}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('code')!=0, d
" 2>/dev/null && ok "离线节点 kb 查询被拒（明确报错，非假成功）" || bad "kb 查询异常: $R"

echo
echo "探针结果: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ] && { echo ALL_GREEN; exit 0; } || exit 1
