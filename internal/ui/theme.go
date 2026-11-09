// Package ui —— 自写界面引擎：纯 Go 软渲染（2x 抗锯齿）+ 矢量/文字/控件。
// 不依赖 WebView2/浏览器；所有界面由本引擎直接画出来。
package ui

// Color sRGB + alpha（0-255）。
type Color struct {
	R, G, B, A uint8
}

// RGB 不透明色。
func RGB(r, g, b uint8) Color { return Color{r, g, b, 255} }

// RGBA 带透明度颜色。
func RGBA(r, g, b, a uint8) Color { return Color{r, g, b, a} }

// Mul 返回按 alpha 缩放后的颜色（用于透明叠加）。
func (c Color) Mul(a float64) Color {
	if a >= 1 {
		return c
	}
	if a <= 0 {
		return Color{}
	}
	c.A = uint8(float64(c.A) * a)
	return c
}

// Hex 解析 #RRGGBB / #RRGGBBAA。
func Hex(s string) Color {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	v := uint32(0)
	for i := 0; i < len(s) && i < 8; i++ {
		d := hexDigit(s[i])
		v = v<<4 | uint32(d)
	}
	switch len(s) {
	case 6:
		return Color{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
	case 8:
		return Color{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}
	}
	return Color{}
}

func hexDigit(b byte) byte {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10
	}
	return 0
}

// Rect 逻辑坐标矩形（float，布局自由）。
type Rect struct {
	X, Y, W, H float64
}

// Pt 点。
type Pt struct {
	X, Y float64
}

// Size 尺寸。
type Size struct {
	W, H float64
}

// Max 最大外接尺寸。
func Max(a, b Size) Size {
	if a.W > b.W {
		b.W = a.W
	}
	if a.H > b.H {
		b.H = a.H
	}
	return b
}

// In 点是否在矩形内。
func (r Rect) In(x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Shrink 四边内缩。
func (r Rect) Shrink(d float64) Rect {
	return Rect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d}
}

// Pad 四边外扩。
func (r Rect) Pad(d float64) Rect {
	return Rect{r.X - d, r.Y - d, r.W + 2*d, r.H + 2*d}
}

// Intersect 交集。
func (r Rect) Intersect(o Rect) Rect {
	x1 := maxF(r.X, o.X)
	y1 := maxF(r.Y, o.Y)
	x2 := minF(r.X+r.W, o.X+o.W)
	y2 := minF(r.Y+r.H, o.Y+o.H)
	if x2 <= x1 || y2 <= y1 {
		return Rect{}
	}
	return Rect{x1, y1, x2 - x1, y2 - y1}
}

// Add 平移。
func (r Rect) Add(dx, dy float64) Rect {
	r.X += dx
	r.Y += dy
	return r
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Theme 设计 token（亮色主题；界面观感的唯一来源）。
type Theme struct {
	BG         Color // 窗口底
	Panel      Color // 卡片
	Panel2     Color // 次级面板/输入底
	Border     Color
	Hover      Color
	Text       Color
	Text2      Color
	Muted      Color
	Accent     Color // 主色（靛蓝）
	AccentSoft Color
	AccentText Color
	Danger     Color
	DangerSoft Color
	OK         Color
	OKSoft     Color
	Warn       Color
	WarnSoft   Color
	Shadow     Color

	RadS float64 // 小圆角（输入/按钮）
	RadM float64 // 中圆角（卡片）
	RadL float64 // 大圆角（模态）

	FontBase string // 常规字重字体名
	FontBold string
}

// DefaultTheme 亮色（靛蓝主色，与 WebUI 同视觉语言）。
func DefaultTheme() *Theme {
	return &Theme{
		BG:         Hex("#f6f7fb"),
		Panel:      Hex("#ffffff"),
		Panel2:     Hex("#f1f2f7"),
		Border:     Hex("#e3e5ee"),
		Hover:      Hex("#eef0f7"),
		Text:       Hex("#1e2235"),
		Text2:      Hex("#515772"),
		Muted:      Hex("#878da3"),
		Accent:     Hex("#6366f1"),
		AccentSoft: Hex("#eef0fe"),
		AccentText: Hex("#ffffff"),
		Danger:     Hex("#e5484d"),
		DangerSoft: Hex("#fdecec"),
		OK:         Hex("#22a06b"),
		OKSoft:     Hex("#e6f6ee"),
		Warn:       Hex("#e8912c"),
		WarnSoft:   Hex("#fdf1e2"),
		Shadow:     RGBA(30, 34, 60, 28),

		RadS: 7,
		RadM: 12,
		RadL: 16,

		FontBase: "",
		FontBold: "",
	}
}
