#!/usr/bin/env python3
"""README 横幅生成：2x 画布排版后缩小，深色底 + 噪点，装饰元素即信息。用法：python3 tools/make_banner.py"""
from PIL import Image, ImageDraw, ImageFont, ImageFilter
import glob
import os

W, H = 1280, 380
S = 2
BG = (13, 17, 23)
FG = (240, 246, 252)
DIM = (139, 148, 158)
ACCENT = (255, 98, 61)
BLUE = (88, 166, 255)
BORDER = (48, 54, 61)

FONT_CJK = None
for p in ("/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
          "/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc"):
    if os.path.exists(p):
        FONT_CJK = p
        break
if FONT_CJK is None:
    cands = glob.glob("/usr/share/fonts/**/Noto*CJK*Bold*", recursive=True)
    FONT_CJK = cands[0] if cands else "/usr/share/fonts/truetype/dejavu/DejaVuSerif-Bold.ttf"


def font(size):
    return ImageFont.truetype(FONT_CJK, size)


img = Image.new("RGB", (W * S, H * S), BG)
# 噪点：避免大面积纯色出带宽
noise = Image.effect_noise((W * S, H * S), 12).convert("L")
img = Image.composite(img, Image.new("RGB", img.size, (24, 30, 38)), noise.point(lambda p: 255 if p > 128 else 0))
d = ImageDraw.Draw(img)

# —— 左侧文案 ——
x = 100 * S // 2
y = 78 * S // 2
label = "SELF-HOSTED  ·  TEAM GATEWAY  ·  TENCENT IMA"
d.text((x, y), label, font=font(26), fill=DIM)
y += 46

title = "ImaHub"
tsize = 150
tfont = font(tsize)
# 标题右边界自适应（给右侧胶囊组留 60px）
limit = W * S - 620
while d.textlength(title, font=tfont) > limit and tsize > 90:
    tsize -= 6
    tfont = font(tsize)
d.text((x, y), title, font=tfont, fill=FG)
y += int(tsize * 1.12)

d.rectangle((x, y, x + 130, y + 8), fill=ACCENT)
y += 26
d.text((x, y), "腾讯 ima 开放平台 · 团队 Web 网关", font=font(42), fill=(201, 209, 217))
y += 62
d.text((x, y), "单二进制 · serve / client 双模式 · 离线可用 · MCP", font=font(28), fill=DIM)

# —— 右侧信息胶囊（装饰即信息） ——
pills = ["离线上传队列", "LWW 双向同步", f"MCP × 15 工具", "手机端适配"]
px = W * S - 600
py = 100 * S // 2
for p in pills:
    f = font(30)
    tw = d.textlength(p, font=f)
    pw, ph = int(tw) + 56, 64
    d.rounded_rectangle((px, py, px + pw, py + ph), radius=ph // 2, outline=BORDER, width=3)
    d.text((px + 28, py + (ph - 32) // 2 - 2), p, font=f, fill=BLUE)
    px += pw + 18
    if px > W * S - 80:
        px = W * S - 600
        py += ph + 20

caption = "v0.3.1 · windows-x86_64 / linux-x86_64 / linux-arm64"
cf = font(22)
cw = d.textlength(caption, font=cf)
d.text((W * S - 60 - cw, H * S - 64), caption, font=cf, fill=DIM)

img = img.resize((W, H), Image.LANCZOS)
os.makedirs("docs/shots", exist_ok=True)
img.save("docs/shots/banner.png")
print("docs/shots/banner.png", img.size)
