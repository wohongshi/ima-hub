#!/usr/bin/env bash
# 离线上传队列行为探针：入队校验/归属隔离/失败自动重试/删除/重启后续传
# 前置：mock 栈已起（ima :18094 / COS :18093），go build -o /tmp/imahub-probe-bin .
set -u -o pipefail
BASE=http://127.0.0.1:18097
BIN=/tmp/imahub-probe-bin
DATA=/tmp/ihq-data
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

# 断言一律走 JSON 解析（grep raw JSON 会因引号/空格差异假通过）
jf() { python3 -c "import json,sys
try:
    d=json.load(sys.stdin)
    for k in '$1'.split('.'):
        d=d[k] if isinstance(d,dict) and k in d else ''
    print(d if d is not None else '')
except Exception:
    print('')" 2>/dev/null; }

[ -x "$BIN" ] || { echo "缺少 $BIN（先 go build -o $BIN .）"; exit 2; }
curl -s --max-time 2 http://127.0.0.1:18094/ >/dev/null 2>&1 \
  || { echo "mock 栈未就绪（ima :18094）"; exit 2; }

SRV=""
cleanup() { [ -n "$SRV" ] && kill "$SRV" 2>/dev/null; wait "$SRV" 2>/dev/null; rm -rf "$DATA" /tmp/ihq*.jar /tmp/ihq-up*; }
trap cleanup EXIT

rm -rf "$DATA" /tmp/ihq*.jar /tmp/ihq-up*; mkdir -p "$DATA"
export IMA_COS_ENDPOINT=http://127.0.0.1:18093

start_srv() {
  "$BIN" serve --addr 127.0.0.1:18097 --data "$DATA" --ima-base-url http://127.0.0.1:18094 >> /tmp/ihq.log 2>&1 &
  SRV=$!
  for _ in $(seq 1 30); do
    curl -s --max-time 1 "$BASE/healthz" 2>/dev/null | grep -q '"ok":true' && return 0
    sleep 0.5
  done
  echo "服务未就绪"; return 1
}
start_srv || exit 2

J=/tmp/ihq.jar
curl -s -c $J -X POST $BASE/api/setup -H 'Content-Type: application/json' \
  -d '{"username":"qadmin","password":"qadmin123"}' >/dev/null
curl -s -b $J -X POST $BASE/api/me/ima -H 'Content-Type: application/json' \
  -d '{"name":"队列号","client_id":"fake_q","api_key":"key_q","upload_kb_id":"kb1"}' >/dev/null
curl -s -b $J -X POST $BASE/api/admin/users -H 'Content-Type: application/json' \
  -d '{"username":"qmember","password":"qmember123","role":"user"}' >/dev/null
J2=/tmp/ihq2.jar
curl -s -c $J2 -X POST $BASE/api/login -H 'Content-Type: application/json' \
  -d '{"username":"qmember","password":"qmember123"}' >/dev/null

echo "== 入队校验 =="
echo "queue probe content" > /tmp/ihq-up.txt
R=$(curl -s -b $J -X POST $BASE/api/queue/upload -F "file=@/tmp/ihq-up.txt" -F "ia=" -F "kb_id=kb1")
QID=$(echo "$R" | jf data.item.id)
[ -n "$QID" ] && ok "入队成功 id=$QID" || bad "入队失败: $R"
echo "not an archive" > /tmp/ihq-up-fake.zip
R=$(curl -s -b $J -X POST $BASE/api/queue/upload -F "file=@/tmp/ihq-up-fake.zip" -F "kb_id=kb1")
echo "$R" | grep -q '不支持排队的文件类型' && ok "压缩包拒收" || bad "压缩包未拒收: $R"
R=$(curl -s -b $J -X POST $BASE/api/queue/upload -F "file=@/tmp/ihq-up.txt;filename=../evil.txt" -F "kb_id=kb1")
QBAD=$(echo "$R" | jf data.item.id)
case "$QBAD" in
  *"/"*|*".."*) bad "文件名未做 Base 净化: $QBAD" ;;
  *) [ -n "$QBAD" ] && ok "文件名净化（无路径段）" || bad "净化用例未入队: $R" ;;
esac

echo "== 归属隔离 =="
R=$(curl -s -b $J2 $BASE/api/queue)
echo "$R" | grep -q "$QID" && bad "他人能看到我的队列条目" || ok "普通用户只见自己的条目"
R=$(curl -s -b $J2 -X DELETE $BASE/api/queue/"$QID")
echo "$R" | grep -q '"code":404' && ok "他人删不掉我的条目" || bad "越权删除未拦: $R"
R=$(curl -s -b $J $BASE/api/queue)
echo "$R" | grep -q '"path"' && bad "响应泄漏服务端路径" || ok "响应不含服务端路径"

echo "== 失败标记 + 自动重试（坏账号） =="
echo "bad account item" > /tmp/ihq-up2.txt
R=$(curl -s -b $J -X POST $BASE/api/queue/upload -F "file=@/tmp/ihq-up2.txt" -F "ia=no-such-account" -F "kb_id=kb1")
QID2=$(echo "$R" | jf data.item.id)
[ -n "$QID2" ] && ok "坏账号条目入队 id=$QID2" || bad "入队失败: $R"
curl -s -b $J -X POST $BASE/api/queue/drain > /dev/null
sleep 1
status_of() {
  curl -s -b $J $BASE/api/queue | python3 -c "import json,sys
d=json.load(sys.stdin)['data']['items']
print([i['status'] for i in d if i['id']=='$1'][0] if any(True for _ in d) else '')" 2>/dev/null
}
[ "$(status_of "$QID2")" = "failed" ] && ok "坏账号条目标记 failed" || bad "状态异常: $(status_of "$QID2")"
[ -z "$(status_of "$QID")" ] && ok "好账号条目已出队（续传成功）" || bad "好账号条目仍在队列: $(status_of "$QID")"

echo "== 异步重试（失败条目持续可重试） =="
R=$(curl -s -b $J -X POST $BASE/api/queue/"$QID2"/retry)
echo "$R" | grep -q '"started":true' && ok "重试已受理（异步）" || bad "重试响应异常: $R"
sleep 1
[ "$(status_of "$QID2")" = "failed" ] && ok "坏账号重试后回到 failed（下轮还会试）" || bad "重试后状态异常: $(status_of "$QID2")"

echo "== 重启持久化 =="
kill $SRV 2>/dev/null; wait $SRV 2>/dev/null; SRV=""
start_srv || exit 2
[ -n "$(status_of "$QID2")" ] && ok "重启后队列仍在" || bad "重启丢队列"
curl -s -b $J -X POST $BASE/api/queue/drain > /dev/null
sleep 1
[ "$(status_of "$QID2")" = "failed" ] && ok "重启后自动重试仍工作" || bad "重启后重试异常: $(status_of "$QID2")"

echo "== 删除 =="
R=$(curl -s -b $J -X DELETE $BASE/api/queue/"$QID2")
echo "$R" | grep -q '"code":0' && ok "删除成功" || bad "删除失败: $R"
[ -z "$(status_of "$QID2")" ] && ok "列表已清" || bad "删除后仍在列表"

echo
echo "探针结果: PASS=$PASS FAIL=$FAIL"
if [ "$FAIL" -eq 0 ]; then echo "ALL_GREEN"; else echo "HAS_FAILURE"; exit 1; fi
