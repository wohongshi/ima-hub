#!/usr/bin/env python3
"""桌面版图标生成：靛蓝渐变圆角方 + 白色 IH 字标（与横幅同视觉语言）。
输出 desktop/icon.ico（16/24/32/48/64/128/256 多尺寸，Windows 资源用）。"""
from PIL import Image, ImageDraw, ImageFont

SIZES = [16, 24, 32, 48, 64, 128, 256]


def font(size):
    for p in (
        "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    ):
        try:
            return ImageFont.truetype(p, size)
        except OSError:
            continue
    return ImageFont.load_default()


def draw(size):
    s = size * 4  # 超采样抗锯齿
    img = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    # 靛蓝纵向渐变底
    for y in range(s):
        t = y / s
        r = int(99 + (79 - 99) * t)    # #6366F1 -> #4F46E5
        g = int(102 + (70 - 102) * t)
        b = int(241 + (229 - 241) * t)
        d.line([(0, y), (s, y)], fill=(r, g, b, 255))
    # 圆角遮罩
    mask = Image.new("L", (s, s), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, s - 1, s - 1], radius=s // 5, fill=255)
    img.putalpha(mask)
    # 白色 IH 字标（轻微阴影层次）
    f = font(int(s * 0.42))
    txt = "IH"
    bb = d.textbbox((0, 0), txt, font=f)
    w, h = bb[2] - bb[0], bb[3] - bb[1]
    x, y = (s - w) / 2 - bb[0], (s - h) / 2 - bb[1]
    d.text((x, y + s * 0.02), txt, font=f, fill=(30, 27, 75, 90))   # 阴影
    d.text((x, y), txt, font=f, fill=(255, 255, 255, 255))
    return img.resize((size, size), Image.LANCZOS)


imgs = [draw(s) for s in SIZES]
imgs[-1].save("desktop/icon.ico", sizes=[(s, s) for s in SIZES], append_images=imgs[:-1])
print("desktop/icon.ico", ", ".join(f"{s}px" for s in SIZES))
