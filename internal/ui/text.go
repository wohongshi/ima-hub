package ui

import (
	"image"
	"os"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// FontSet 字体族（常规/粗体两字重；系统字体，中英文混排）。
type FontSet struct {
	regular *opentype.Font
	bold    *opentype.Font
	faces   map[string]font.Face
}

// TextStyle 文字样式（Size 为逻辑像素）。
type TextStyle struct {
	Size  float64
	Bold  bool
	Color Color
}

// LoadFontSet 按候选路径加载字体（第一个可用者胜；ttf/ttc 均可，多字重同文件时取首字体）。
func LoadFontSet(regular, bold []string) *FontSet {
	fs := &FontSet{faces: map[string]font.Face{}}
	fs.regular = firstFont(regular)
	fs.bold = firstFont(bold)
	if fs.bold == nil {
		fs.bold = fs.regular
	}
	return fs
}

func firstFont(paths []string) *opentype.Font {
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.HasSuffix(strings.ToLower(p), ".ttc") {
			if col, err := opentype.ParseCollection(raw); err == nil && col.NumFonts() > 0 {
				if f, err := col.Font(0); err == nil {
					return f
				}
			}
		}
		if f, err := opentype.Parse(raw); err == nil {
			return f
		}
	}
	return nil
}

// FontPaths 平台默认字体候选（Windows 微软雅黑 / Linux Noto Sans CJK / 兜底 DejaVu）。
func FontPaths() (regular, bold []string) {
	return []string{
			`C:\Windows\Fonts\msyh.ttc`,
			`C:\Windows\Fonts\simhei.ttf`,
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		}, []string{
			`C:\Windows\Fonts\msyhbd.ttc`,
			`C:\Windows\Fonts\msyh.ttc`,
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
		}
}

// face 取字号字重对应的字面（2x 内部尺寸渲染，缓存复用）。
func (fs *FontSet) face(st TextStyle) font.Face {
	if fs == nil {
		return nil
	}
	key := ""
	if st.Bold {
		key = "b"
	}
	fnt := fs.regular
	if st.Bold {
		fnt = fs.bold
	}
	if fnt == nil {
		return nil
	}
	px := st.Size * scale
	k := key + string(rune(int(px*4)))
	if f, ok := fs.faces[k]; ok {
		return f
	}
	f, err := opentype.NewFace(fnt, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil
	}
	fs.faces[k] = f
	return f
}

// Measure 文本逻辑尺寸（单行）。
func (fs *FontSet) Measure(s string, st TextStyle) Size {
	f := fs.face(st)
	if f == nil {
		return Size{float64(len([]rune(s))) * st.Size * 0.6, st.Size * 1.4}
	}
	w := font.MeasureString(f, s)
	m := f.Metrics()
	return Size{float64(w.Ceil()) / scale, float64(m.Ascent.Ceil()+m.Descent.Ceil()) / scale}
}

// LineHeight 行高（逻辑像素，含下行）。
func (fs *FontSet) LineHeight(st TextStyle) float64 {
	m := fs.Measure("中Ag", st)
	return m.H
}

// TextAt 绘制单行文本（x,y 为左上角；引擎内部换算基线）。
func (c *Canvas) TextAt(s string, x, y float64, st TextStyle) {
	if c.FS == nil || s == "" {
		return
	}
	f := c.FS.face(st)
	if f == nil {
		return
	}
	m := f.Metrics()
	baseline := y + float64(m.Ascent.Ceil())/scale
	d := &font.Drawer{
		Dst:  c.buf,
		Src:  image.NewUniform(toRGBA(st.Color)),
		Face: f,
		Dot:  fixed.P(int(x*scale), int(baseline*scale)),
	}
	d.DrawString(s)
}

// TextCentered 矩形内水平垂直居中绘制。
func (c *Canvas) TextCentered(s string, r Rect, st TextStyle) {
	m := c.MeasureText(s, st)
	c.TextAt(s, r.X+(r.W-m.W)/2, r.Y+(r.H-m.H)/2, st)
}

// TextRight 右对齐垂直居中绘制。
func (c *Canvas) TextRight(s string, r Rect, st TextStyle) {
	m := c.MeasureText(s, st)
	c.TextAt(s, r.X+r.W-m.W, r.Y+(r.H-m.H)/2, st)
}

// MeasureText 文本逻辑尺寸（单行）。
func (c *Canvas) MeasureText(s string, st TextStyle) Size {
	if c.FS == nil {
		return Size{float64(len([]rune(s))) * st.Size * 0.6, st.Size * 1.4}
	}
	return c.FS.Measure(s, st)
}

// LineHeight 行高。
func (c *Canvas) LineHeight(st TextStyle) float64 { return c.MeasureText("中Ag", st).H }

// Ellipsize 超宽截断。
func (c *Canvas) Ellipsize(s string, maxW float64, st TextStyle) string {
	if c.FS == nil {
		return s
	}
	return c.FS.Ellipsize(s, maxW, st)
}

// WrapText 折行。
func (c *Canvas) WrapText(s string, maxW float64, st TextStyle, maxLines int) []string {
	if c.FS == nil {
		return []string{s}
	}
	return c.FS.WrapText(s, maxW, st, maxLines)
}

// Ellipsize 超宽文本截断加省略号。
func (fs *FontSet) Ellipsize(s string, maxW float64, st TextStyle) string {
	if fs.Measure(s, st).W <= maxW {
		return s
	}
	rs := []rune(s)
	for len(rs) > 0 {
		rs = rs[:len(rs)-1]
		t := string(rs) + "…"
		if fs.Measure(t, st).W <= maxW {
			return t
		}
	}
	return "…"
}

// WrapText 按宽度折行（中文逐字、西文按空格；最多 maxLines 行，超出省略）。
func (fs *FontSet) WrapText(s string, maxW float64, st TextStyle, maxLines int) []string {
	var lines []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if r == '\n' {
			flush()
			if maxLines > 0 && len(lines) >= maxLines {
				return lines
			}
			continue
		}
		t := cur.String() + string(r)
		if fs.Measure(t, st).W > maxW && cur.Len() > 0 {
			flush()
			if maxLines > 0 && len(lines) >= maxLines {
				// 最后一行截断
				lines[len(lines)-1] = fs.Ellipsize(lines[len(lines)-1], maxW, st)
				return lines
			}
		}
		cur.WriteRune(r)
	}
	flush()
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = fs.Ellipsize(lines[maxLines-1], maxW, st)
	}
	return lines
}
