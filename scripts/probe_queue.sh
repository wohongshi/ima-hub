#!/usr/bin/env bash
# 离线上传队列行为探针：入队/列表/续传/失败标记/删除
# 前置：mock 栈已起（ima :18094 / COS :18093），go build -o /tmp/imahub-probe-bin .
set -u
BASE=http://127.0.0.1:18097
BIN=/tmp/imahub-probe-bin
DATA=/tmp/ihq-data
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

rm -rf "$DATA" /tmp/ihq.jar /tmp/ihq-up.txt; mkdir -p "$DATA"
export IMA_COS_ENDPOINT=http://127.0.0.1:18093
"$BIN" serve --addr 127.0.0.1:18097 --data "$DATA" --ima-base-url http://127.0.0.1:18094 > /tmp/ihq.log 2>&1 &
SRV=$!
sleep 2
J=/tmp/ihq.jar
curl -s -c $J -X POST $BASE/api/setup -H 'Content-Type: application/json' \
  -d '{"username":"qadmin","password":"qadmin123"}' >/dev/null
curl -s -b $J -X POST $BASE/api/me/ima -H 'Content-Type: application/json' \
  -d '{"name":"队列号","client_id":"fake_q","api_key":"key_q","upload_kb_id":"kb1"}' >/dev/null

echo "== 入队 =="
echo "queue probe content" > /tmp/ihq-up.txt
R=$(curl -s -b $J -X POST $BASE/api/queue/upload -F "file=@/tmp/ihq-up.txt" -F "ia=" -F "kb_id=kb1")
echo "$R" | grep -q '"code":0' && ok "入队成功" || bad "入队失败: $R"
QID=$(python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["item"]["id"])' <<<"$R" 2>/dev/null)
R=$(curl -s -b $J $BASE/api/queue)
echo "$R" | grep -q 'qadmin\|待传\|pending' && ok "列表可见" || bad "列表异常: $R"

echo "== 失败标记（坏账号） =="
echo "bad account item" > /tmp/ihq-up2.txt
R=$(curl -s -b $J -X POST $BASE/api/queue/upload -F "file=@/tmp/ihq-up2.txt" -F "ia=no-such-account" -F "kb_id=kb1")
QID2=$(python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["item"]["id"])' <<<"$R" 2>/dev/null)
curl -s -b $J -X POST $BASE/api/queue/drain >/dev/null
R=$(curl -s -b $J $BASE/api/queue)
echo "$R" | grep -q 'failed' && ok "坏账号条目标记 failed" || bad "失败标记缺失: $R"

echo "== 续传（好账号已出队） =="
R=$(curl -s -b $J $BASE/api/queue)
echo "$R" | grep -q "$QID" && bad "成功条目未出队" || ok "成功条目已出队"
R=$(curl -s -b $J -X POST $BASE/api/queue/"$QID2"/retry)
echo "$R" | grep -q '"ok":false\|"ok": false' && ok "重试仍失败（坏账号）" || bad "重试结果异常: $R"

echo "== 删除 =="
R=$(curl -s -b $J -X DELETE $BASE/api/queue/"$QID2")
echo "$R" | grep -q '"code":0' && ok "删除成功" || bad "删除失败: $R"
R=$(curl -s -b $J $BASE/api/queue)
echo "$R" | grep -q "$QID2" && bad "删除后仍在列表" || ok "列表已清空"

kill $SRV 2>/dev/null; wait $SRV 2>/dev/null
echo
echo "探针结果: PASS=$PASS FAIL=$FAIL"
[ $FAIL -eq 0 ] && echo "ALL_GREEN" || echo "HAS_FAILURE"
