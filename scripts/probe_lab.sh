#!/usr/bin/env bash
# 教研工作台探针（/lab 免登录收集）：开关门控 / 匿名上传 / 清单 / 下载一致 / 敌意名净化 /
# 上传限速 / 管理员删除 / 链接净化 / 登录失败限速。
# 前置：go build -o /tmp/imahub-probe-bin .
set -u -o pipefail
BIN=/tmp/imahub-probe-bin
BASE=http://127.0.0.1:18204
D=/tmp/ihlab-data
CNT=/tmp/ihlab-count
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; echo P >> "$CNT"; }
bad() { echo "  ✗ $1"; echo F >> "$CNT"; }
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
cleanup() { [ -n "$SP" ] && kill "$SP" 2>/dev/null; wait "$SP" 2>/dev/null; rm -rf "$D" /tmp/ihlab*.jar /tmp/ihlab-*.bin; }
trap cleanup EXIT INT TERM

rm -rf "$D"; mkdir -p "$D"; : > "$CNT"
"$BIN" serve --addr 127.0.0.1:18204 --data "$D" > /tmp/ihlab.log 2>&1 &
SP=$!
for _ in $(seq 1 30); do
  curl -s --max-time 1 $BASE/healthz 2>/dev/null | grep -q '"ok":true' && break
  kill -0 "$SP" 2>/dev/null || { echo "服务起不来"; tail -5 /tmp/ihlab.log; exit 2; }
  sleep 0.5
done

echo "== 默认关闭：info 暴露开关，上传拒 =="
R=$(curl -s $BASE/api/lab/info)
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert d['enabled']==False and d['title']=='教研工作台', d" 2>/dev/null && ok "info 默认关闭" || bad "info 默认态异常: $R"
echo hello > /tmp/ihlab-a.bin
R=$(curl -s -X POST $BASE/api/lab/upload -F "file=@/tmp/ihlab-a.bin")
echo "$R" | jerr "未启用" "未启用拒上传"

echo "== 管理员开通 + 配提示语/链接（含 javascript: 应被滤） =="
curl -s -c /tmp/ihlab.jar -X POST $BASE/api/setup -H 'Content-Type: application/json' -d '{"username":"admin","password":"lab123456"}' >/dev/null
R=$(curl -s -b /tmp/ihlab.jar -X PUT $BASE/api/admin/settings -H 'Content-Type: application/json' -d '{
  "lab_enabled": true,
  "lab_title": "化学组教研工作台",
  "lab_notice": "本周教研：请上传期中试卷分析",
  "lab_links": [
    {"name":"打开 ima 知识库","url":"https://ima.qq.com"},
    {"name":"恶意","url":"javascript:alert(1)"}
  ]}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert d['lab_enabled']==True and len(d['lab_links'])==1 and d['lab_links'][0]['name']=='打开 ima 知识库', d" 2>/dev/null \
  && ok "配置落库 + javascript: 链接被滤" || bad "配置异常: $R"

echo "== 匿名上传（自报名字） =="
R=$(curl -s -X POST $BASE/api/lab/upload -F "file=@/tmp/ihlab-a.bin" -F "by=张老师")
echo "$R" | jassert 'assert d["data"]["name"]=="ihlab-a.bin", d' "匿名上传成功"

echo "== 清单含名字/大小/上传者 =="
R=$(curl -s $BASE/api/lab/files)
echo "$R" | python3 -c "
import json,sys
fs=json.load(sys.stdin)['data']['files']
assert len(fs)==1 and fs[0]['by']=='张老师' and fs[0]['size']==6 and 'secret' not in fs[0], fs" 2>/dev/null \
  && ok "清单 1 条且字段齐" || bad "清单异常: $R"

echo "== 下载字节一致 =="
ID=$(echo "$R" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['files'][0]['id'])")
curl -s "$BASE/api/lab/file?id=$ID" -o /tmp/ihlab-dl.bin
cmp -s /tmp/ihlab-a.bin /tmp/ihlab-dl.bin && ok "下载字节一致" || bad "下载不一致"

echo "== 敌意文件名净化（../ 穿越） =="
cp /tmp/ihlab-a.bin '/tmp/ihlab-../evil.bin' 2>/dev/null || cp /tmp/ihlab-a.bin /tmp/ihlab-evil.bin
R=$(curl -s -X POST $BASE/api/lab/upload -F "file=@/tmp/ihlab-a.bin;filename=../../evil.sh" -F "by=x")
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert '/' not in d['name'] and '\\\\' not in d['name'] and '..' not in d['name'], d" 2>/dev/null \
  && ok "文件名已净化" || bad "文件名净化异常: $R"
[ -f "$D/../evil.sh" ] && bad "穿越成功（严重）" || ok "未写出目录外"

echo "== 上传限速（第 21 个应 429） =="
GOT429=""
for i in $(seq 1 22); do
  R=$(curl -s -X POST $BASE/api/lab/upload -F "file=@/tmp/ihlab-a.bin")
  echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('code')!=0 and '频繁' in d.get('msg',''), d" 2>/dev/null && { GOT429=1; break; }
done
[ -n "$GOT429" ] && ok "限速生效" || bad "限速未触发"

echo "== 管理员删除 =="
curl -s -b /tmp/ihlab.jar -X DELETE "$BASE/api/lab/file?id=$ID" >/dev/null
R=$(curl -s $BASE/api/lab/files)
echo "$R" | python3 -c "
import json,sys
fs=json.load(sys.stdin)['data']['files']
assert all(f['id']!='$ID' for f in fs), fs" 2>/dev/null && ok "删除成功" || bad "删除未生效: $R"

echo "== 登录失败限速（21 次错密码后应 429） =="
GOT429=""
for i in $(seq 1 22); do
  R=$(curl -s -X POST $BASE/api/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"wrongpass"}')
  echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('code')!=0 and '频繁' in d.get('msg',''), d" 2>/dev/null && { GOT429=1; break; }
done
[ -n "$GOT429" ] && ok "登录失败限速生效" || bad "登录限速未触发"

echo "== MCP 端点（WorkBuddy 直连） =="
R=$(curl -s -X POST $BASE/lab/mcp -H 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d['result']['protocolVersion']=='2025-06-18' and d['result']['serverInfo']['name']=='jiaoyan-lab', d" 2>/dev/null \
  && ok "MCP initialize" || bad "MCP initialize 异常: $R"
R=$(curl -s -X POST $BASE/lab/mcp -H 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}')
echo "$R" | python3 -c "
import json,sys
tools=json.load(sys.stdin)['result']['tools']
names=sorted(t['name'] for t in tools)
assert names==['lab_download','lab_files','lab_info','lab_upload'], names" 2>/dev/null \
  && ok "MCP tools/list 四工具" || bad "MCP 工具清单异常: $R"
B64=$(printf 'mcp 上传内容' | base64 -w0)
R=$(curl -s -X POST $BASE/lab/mcp -H 'Content-Type: application/json' -H 'X-Forwarded-For: 10.9.8.7' --data-raw "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"lab_upload\",\"arguments\":{\"filename\":\"MCP来稿.docx\",\"content_base64\":\"$B64\",\"by\":\"MCP老师\"}}}")
echo "$R" | python3 -c "
import json,sys
t=json.load(sys.stdin)['result']['content'][0]['text']
d=json.loads(t)
assert d['name']=='MCP来稿.docx' and d['id'], d" 2>/dev/null \
  && ok "MCP 上传" || bad "MCP 上传异常: $R"
MID=$(echo "$R" | python3 -c "import json,sys;print(json.loads(json.load(sys.stdin)['result']['content'][0]['text'])['id'])")
R=$(curl -s -X POST $BASE/lab/mcp -H 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"lab_files","arguments":{}}}')
echo "$R" | python3 -c "
import json,sys
t=json.load(sys.stdin)['result']['content'][0]['text']
fs=json.loads(t)['files']
assert any(f['name']=='MCP来稿.docx' and f['by']=='MCP老师' for f in fs), fs" 2>/dev/null \
  && ok "MCP 清单可见" || bad "MCP 清单异常: $R"
R=$(curl -s -X POST $BASE/lab/mcp -H 'Content-Type: application/json' --data-raw "{\"jsonrpc\":\"2.0\",\"id\":5,\"method\":\"tools/call\",\"params\":{\"name\":\"lab_download\",\"arguments\":{\"id\":\"$MID\"}}}")
echo "$R" | python3 -c "
import json,sys,base64
t=json.load(sys.stdin)['result']['content'][0]['text']
d=json.loads(t)
assert base64.b64decode(d['content_base64'])=='mcp 上传内容'.encode(), d" 2>/dev/null \
  && ok "MCP 下载字节一致" || bad "MCP 下载异常: $R"

echo "== 免账号管理（管理密钥，无需登录） =="
R=$(curl -s -b /tmp/ihlab.jar -X POST $BASE/api/lab/manage/config -H 'Content-Type: application/json' --data-raw '{"lab_manage_key":"probe-key-123"}')
echo "$R" | jassert 'assert d["data"]["lab_title"] is not None, d' "管理员可设管理密钥"
R=$(curl -s $BASE/api/lab/info)
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert d['manage']==True, d" 2>/dev/null && ok "info 标记 manage" || bad "info.manage 异常: $R"
R=$(curl -s -X POST $BASE/api/lab/manage/config -H 'Content-Type: application/json' --data-raw '{"lab_title":"x"}')
echo "$R" | jerr "管理密钥" "无密钥拒管理"
R=$(curl -s -X POST "$BASE/api/lab/manage/config?k=wrong-key" -H 'Content-Type: application/json' --data-raw '{"lab_title":"x"}')
echo "$R" | jerr "管理密钥" "错密钥拒管理"
R=$(curl -s -X POST "$BASE/api/lab/manage/config?k=probe-key-123" -H 'Content-Type: application/json' --data-raw '{"lab_title":"免账号标题","lab_notice":"新提示语","lab_links":[{"name":"百度","url":"https://baidu.com"},{"name":"恶","url":"javascript:alert(1)"}]}')
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert d['lab_title']=='免账号标题' and len(d['lab_links'])==1, d" 2>/dev/null \
  && ok "密钥免登录改配置（javascript: 仍被滤）" || bad "密钥改配置异常: $R"
R=$(curl -s -X POST $BASE/api/lab/upload -F "file=@/tmp/ihlab-a.bin;filename=待删文件.bin" -F "by=测试" -H 'X-Forwarded-For: 10.9.8.8')
DID=$(echo "$R" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
R=$(curl -s -X DELETE "$BASE/api/lab/file?id=$DID" -H 'X-Manage-Key: probe-key-123')
echo "$R" | jassert 'assert d.get("code")==0, d' "密钥免登录删文件"
R=$(curl -s -X POST $BASE/api/lab/manage/config -H 'X-Manage-Key: probe-key-123' -H 'Content-Type: application/json' --data-raw '{"lab_manage_key":"probe-key-456"}')
echo "$R" | jassert 'assert d.get("code")==0, d' "密钥可自换新钥"
R=$(curl -s -X POST $BASE/api/lab/manage/config -H 'X-Manage-Key: probe-key-123' -H 'Content-Type: application/json' --data-raw '{"lab_title":"x"}')
echo "$R" | jerr "管理密钥" "旧钥换后即废"

echo "== 无账号实例：lab-init 直达教研页（二实例 18206） =="
ND=/tmp/ihlab-node
rm -rf "$ND"; mkdir -p "$ND"
INIT=$("$BIN" lab-init --data "$ND" --title "免账号站" --notice "来传资料" --links "开卷面|https://ima.qq.com" 2>&1)
NKEY=$(echo "$INIT" | sed -n 's/.*管理密钥: //p' | head -1)
[ -n "$NKEY" ] && ok "lab-init 出密钥（无任何账号）" || bad "lab-init 无密钥: $INIT"
"$BIN" serve --addr 127.0.0.1:18206 --data "$ND" > /tmp/ihlab-node.log 2>&1 &
NP=$!
for _ in $(seq 1 20); do curl -s --max-time 1 http://127.0.0.1:18206/healthz 2>/dev/null | grep -q '"ok":true' && break; sleep 0.5; done
LOC=$(curl -s -o /dev/null -w '%{redirect_url}' http://127.0.0.1:18206/)
echo "$LOC" | grep -q '/lab' && ok "根路径直达 /lab（免登录）" || bad "根路径未直达: $LOC"
R=$(curl -s http://127.0.0.1:18206/api/lab/info)
echo "$R" | python3 -c "
import json,sys
d=json.load(sys.stdin)['data']
assert d['enabled']==True and d['title']=='免账号站' and d['manage']==True, d" 2>/dev/null \
  && ok "lab-init 配置落库" || bad "lab-init 配置异常: $R"
R=$(curl -s -X POST "http://127.0.0.1:18206/api/lab/manage/config?k=$NKEY" -H 'Content-Type: application/json' --data-raw '{"lab_notice":"改好了"}')
echo "$R" | jassert 'assert d["data"]["lab_notice"]=="改好了", d' "密钥直管（全程无账号）"
kill "$NP" 2>/dev/null; wait "$NP" 2>/dev/null; rm -rf "$ND"

P=$(grep -c '^P$' "$CNT" 2>/dev/null); F=$(grep -c '^F$' "$CNT" 2>/dev/null)
echo
echo "probe_lab: ${P:-0} 通过 / ${F:-0} 失败"
[ "${F:-0}" -eq 0 ] || exit 1
