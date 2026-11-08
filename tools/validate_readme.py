#!/usr/bin/env python3
"""README 验证：链接/图片本地存在性 + 渲染 GitHub 风格 HTML（含破图探针）。
用法：python3 tools/validate_readme.py  → 生成 readme_render.html，再用 chromium 截图/dump-dom 验证。"""
import os
import re
import sys

import markdown

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
os.chdir(ROOT)

src = open("README.md", encoding="utf-8").read()

# ---- 1) 链接与图片扫描 ----
refs = re.findall(r'(?:src="([^"]+)"|\]\(([^)]+)\)|href="([^"]+)")', src)
local_missing, remote, local_ok = [], [], 0
for a, b, c in refs:
    u = a or b or c
    if not u or u.startswith("#"):
        continue
    if re.match(r'https?://', u):
        if "img.shields.io" not in u:  # 徽章外链不逐个探活
            remote.append(u)
        continue
    p = u.split("#")[0]
    if p and not os.path.exists(p):
        local_missing.append(u)
    elif p:
        local_ok += 1

print(f"本地引用 OK: {local_ok}，缺失: {len(local_missing)}")
for u in local_missing:
    print("  ✗ 缺失:", u)
print(f"外链（非徽章）: {len(remote)}")
for u in remote:
    print("  →", u)

# ---- 2) 渲染 HTML（GitHub 暗色风格近似），放仓库根目录以便相对路径解析 ----
body = markdown.markdown(src, extensions=["tables", "fenced_code"])
html = """<!doctype html><html><head><meta charset="utf-8">
<style>
  body { background:#0d1117; color:#c9d1d9; font:15px/1.7 -apple-system,"Segoe UI",sans-serif;
         max-width:880px; margin:0 auto; padding:32px 16px; }
  h1,h2,h3 { color:#f0f6fc; } a { color:#58a6ff; text-decoration:none; }
  table { border-collapse:collapse; width:100%; margin:12px 0; }
  th,td { border:1px solid #30363d; padding:6px 10px; text-align:left; }
  th { background:#161b22; } tr:nth-child(even) { background:#161b2244; }
  code { background:#161b22; padding:2px 6px; border-radius:6px; font-size:13px; }
  pre { background:#161b22; padding:14px; border-radius:10px; overflow:auto; }
  img { max-width:100%; } .mermaid { background:#fff; border-radius:10px; padding:8px; }
  blockquote { border-left:3px solid #30363d; margin:0; padding:2px 12px; color:#8b949e; }
</style></head><body>
""" + body + """
<pre id="probe">PROBE:pending</pre>
<script>
window.addEventListener('load', function () {
  setTimeout(function () {
    var bad = [];
    document.images.forEach(function (img) {
      var s = img.getAttribute('src') || '';
      if (s.indexOf('http') === 0) return; // 外链徽章不计入（受本机网络影响）
      if (!img.complete || img.naturalWidth === 0) bad.push(s);
    });
    document.getElementById('probe').textContent =
      bad.length ? 'PROBE:BROKEN:' + bad.join(',') : 'PROBE:OK';
  }, 1200);
});
</script>
</body></html>"""
open("readme_render.html", "w", encoding="utf-8").write(html)
print("生成 readme_render.html（相对图片以仓库根解析）")

sys.exit(1 if local_missing else 0)
