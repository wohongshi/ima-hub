#!/usr/bin/env bash
# 免登录设备探针（单人模式「记住本机」）：默认 admin / 设备签发 / 免密登录 / 吊销 / 改密全清。
# 前置：go build -o /tmp/imahub-probe-bin .
set -u -o pipefail
BIN=/tmp/imahub-probe-bin
BASE=http://127.0.0.1:18203
D=/tmp/ihl-data
CNT=/tmp/ihl-count
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; echo P >> "$CNT"; }
bad() { echo "  ✗ $1"; echo F >> "$CNT"; }
# JSON 断言：python 解析信封（别 grep raw）
jget() { python3 -c "import json,sys;d=json.load(sys.stdin);print($1)"; }
jassert() { # $1=python断言行 $2=标签（响应走 stdin）
  python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('code')==0, d
$1" 2>/dev/null && ok "$2" || bad "$2"
}
jerr() { # $1=期望msg片段 $2=标签
  python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('code')!=0 and '''$1''' in d.get('msg',''), d" 2>/dev/null && ok "$2" || bad "$2"
}

[ -x "$BIN" ] || { echo "缺少 $BIN（先 go build -o $BIN .）"; exit 2; }
SP=""
cleanup() { [ -n "$SP" ] && kill "$SP" 2>/dev/null; wait "$SP" 2>/dev/null; rm -rf "$D" /tmp/ihl*.jar; }
trap cleanup EXIT INT TERM

rm -rf "$D"; mkdir -p "$D"; : > "$CNT"
"$BIN" serve --addr 127.0.0.1:18203 --data "$D" > /tmp/ihl.log 2>&1 &
SP=$!
for _ in $(seq 1 30); do
  curl -s --max-time 1 $BASE/healthz 2>/dev/null | grep -q '"ok":true' && break
  kill -0 "$SP" 2>/dev/null || { echo "服务起不来"; tail -5 /tmp/ihl.log; exit 2; }
  sleep 0.5
done

echo "== 单人模式初始化（用户名可省，默认 admin） =="
R=$(curl -s -c /tmp/ihl.jar -X POST $BASE/api/setup -H 'Content-Type: application/json' -d '{"password":"pass123456"}')
echo "$R" | jassert 'assert d["data"]["user"]["username"]=="admin", d' "setup 省用户名 → 默认 admin"

echo "== 密码登录照常 =="
R=$(curl -s -c /tmp/ihl.jar -X POST $BASE/api/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"pass123456"}')
echo "$R" | jassert 'assert d["data"]["user"]["username"]=="admin", d' "密码登录"

echo "== 签发免登录设备 =="
R=$(curl -s -b /tmp/ihl.jar -X POST $BASE/api/me/devices -H 'Content-Type: application/json' -d '{"name":"测试机"}')
TOK=$(echo "$R" | jget 'd["data"]["token"]')
DID=$(echo "$R" | jget 'd["data"]["id"]')
[ -n "$TOK" ] && [ "$TOK" != "None" ] && [ -n "$DID" ] && [ "$DID" != "None" ] && ok "token/id 非空" || bad "签发返回异常: $R"

echo "== 登出后设备 token 免密直登 =="
curl -s -b /tmp/ihl.jar -c /tmp/ihl.jar -X POST $BASE/api/logout >/dev/null
R=$(curl -s -c /tmp/ihl2.jar -X POST $BASE/api/login -H 'Content-Type: application/json' -d "{\"device_token\":\"$TOK\"}")
echo "$R" | jassert 'assert d["data"]["user"]["username"]=="admin", d' "设备 token 免密登录"
R=$(curl -s -b /tmp/ihl2.jar $BASE/api/me)
echo "$R" | jassert 'assert d["data"]["user"]["id"] is not None, d' "免密登录会话可用"

echo "== 设备清单 =="
R=$(curl -s -b /tmp/ihl2.jar $BASE/api/me/devices)
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']['devices']
assert len(d)==1 and d[0]['name']=='测试机' and 'secret' not in d[0] and 'token' not in d[0], d" 2>/dev/null \
  && ok "清单 1 台且不泄密钥" || bad "清单异常: $R"

echo "== 吊销后设备登录拒绝 =="
curl -s -b /tmp/ihl2.jar -X DELETE $BASE/api/me/devices/$DID >/dev/null
R=$(curl -s -X POST $BASE/api/login -H 'Content-Type: application/json' -d "{\"device_token\":\"$TOK\"}")
echo "$R" | jerr "失效" "吊销后设备登录 401"
R=$(curl -s -X POST $BASE/api/login -H 'Content-Type: application/json' -d '{"device_token":"随便编的"}')
echo "$R" | jerr "失效" "伪造 token 拒绝"

echo "== 密码登录不受影响 =="
R=$(curl -s -c /tmp/ihl.jar -X POST $BASE/api/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"pass123456"}')
echo "$R" | jassert 'assert d["data"]["user"]["username"]=="admin", d' "密码登录照常"

echo "== 改密清全部设备 =="
R=$(curl -s -b /tmp/ihl.jar -X POST $BASE/api/me/devices -H 'Content-Type: application/json' -d '{"name":"旧设备"}')
TOK2=$(echo "$R" | jget 'd["data"]["token"]')
curl -s -b /tmp/ihl.jar -X POST $BASE/api/me/password -H 'Content-Type: application/json' -d '{"old_password":"pass123456","new_password":"newpass123456"}' >/dev/null
R=$(curl -s -X POST $BASE/api/login -H 'Content-Type: application/json' -d "{\"device_token\":\"$TOK2\"}")
echo "$R" | jerr "失效" "改密后旧设备失效"
R=$(curl -s -X POST $BASE/api/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"newpass123456"}')
echo "$R" | jassert 'assert d["data"]["user"]["username"]=="admin", d' "新密码登录"

P=$(grep -c '^P$' "$CNT" 2>/dev/null); F=$(grep -c '^F$' "$CNT" 2>/dev/null)
echo
echo "probe_login: ${P:-0} 通过 / ${F:-0} 失败"
[ "${F:-0}" -eq 0 ] || exit 1
