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

P=$(grep -c '^P$' "$CNT" 2>/dev/null); F=$(grep -c '^F$' "$CNT" 2>/dev/null)
echo
echo "probe_lab: ${P:-0} 通过 / ${F:-0} 失败"
[ "${F:-0}" -eq 0 ] || exit 1
