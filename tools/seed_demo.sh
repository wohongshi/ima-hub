#!/usr/bin/env bash
# README 截图用演示数据注入：起测试实例后跑本脚本，数据全是虚构演示内容。
# 用法：bash tools/seed_demo.sh <BASE> （如 http://127.0.0.1:18091）
set -u
BASE="${1:?用法: seed_demo.sh <BASE>}"
J=/tmp/seed-demo.jar
rm -f $J

say() { echo "[seed] $1"; }

curl -s -c $J -X POST $BASE/api/setup -H 'Content-Type: application/json' \
  -d '{"username":"demo","password":"demopass123"}' >/dev/null && say "初始化演示管理员"

curl -s -b $J -X POST $BASE/api/me/ima -H 'Content-Type: application/json' \
  -d '{"name":"演示账号","client_id":"demo_client","api_key":"demo_key","upload_kb_id":"kb1"}' >/dev/null && say "绑定演示 ima 账号"

# 任务（不同状态/到期日）
TODAY=$(date +%F)
TOMORROW=$(date -d "+1 day" +%F 2>/dev/null || date -v+1d +%F)
NEXTWEEK=$(date -d "+7 day" +%F 2>/dev/null || date -v+7d +%F)
curl -s -b $J -X POST $BASE/api/tasks -H 'Content-Type: application/json' \
  -d "{\"title\":\"整理周会纪要\",\"note\":\"附议程与决议\",\"due\":\"$TODAY\"}" >/dev/null
curl -s -b $J -X POST $BASE/api/tasks -H 'Content-Type: application/json' \
  -d "{\"title\":\"验收 v0.3.1 发布清单\",\"note\":\"三平台产物 + 校验和\",\"due\":\"$TOMORROW\",\"status\":\"doing\"}" >/dev/null
curl -s -b $J -X POST $BASE/api/tasks -H 'Content-Type: application/json' \
  -d '{"title":"更新部署文档","status":"done"}' >/dev/null
curl -s -b $J -X POST $BASE/api/tasks -H 'Content-Type: application/json' \
  -d "{\"title\":\"收集组员使用反馈\",\"due\":\"$NEXTWEEK\"}" >/dev/null
say "任务 ×4"

# 日程（今天/明天/下周）
NOW=$(date +%s)
curl -s -b $J -X POST $BASE/api/events -H 'Content-Type: application/json' \
  -d "{\"title\":\"团队周会\",\"start_at\":$((NOW+7200)),\"end_at\":$((NOW+10800)),\"location\":\"线上\"}" >/dev/null
curl -s -b $J -X POST $BASE/api/events -H 'Content-Type: application/json' \
  -d "{\"title\":\"版本发布复盘\",\"start_at\":$((NOW+86400)),\"location\":\"会议室\"}" >/dev/null
curl -s -b $J -X POST $BASE/api/events -H 'Content-Type: application/json' \
  -d "{\"title\":\"季度规划日\",\"start_at\":$((NOW+604800)),\"all_day\":true}" >/dev/null
say "日程 ×3"

# 公告（Markdown）
curl -s -b $J -X POST $BASE/api/announcements -H 'Content-Type: application/json' \
  -d '{"content":"**v0.3.1 已发布**：离线上传队列 + 手机端适配上线。\n- 下载与更新说明见 Releases\n- 使用问题随时反馈给组长"}' >/dev/null && say "公告（Markdown）"

# 上传队列 ×2（顶栏角标与队列卡演示）
echo "演示文件：季度总结" > /tmp/seed-a.txt
echo "演示文件：会议录音转写" > /tmp/seed-b.txt
curl -s -b $J -X POST $BASE/api/queue/upload -F "file=@/tmp/seed-a.txt" -F "kb_id=kb1" >/dev/null
curl -s -b $J -X POST $BASE/api/queue/upload -F "file=@/tmp/seed-b.txt" -F "kb_id=kb1" >/dev/null
say "上传队列 ×2"

# 直传两个文件（活动流水 + 知识库条目）+ 扫描
curl -s -b $J -X POST $BASE/api/ima/upload -F "file=@/tmp/seed-a.txt;filename=季度总结.txt" -F "ia=auto" >/dev/null
curl -s -b $J -X POST $BASE/api/ima/upload -F "file=@/tmp/seed-b.txt;filename=产品路线图.txt" -F "ia=auto" >/dev/null
say "直传 ×2（mock 流）"
curl -s -b $J -X POST $BASE/api/scan -H 'Content-Type: application/json' -d '{}' >/dev/null && say "触发扫描"
sleep 1
rm -f /tmp/seed-a.txt /tmp/seed-b.txt
echo "[seed] 完成"
