package ui

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

// scale 内部超采样倍率：2x 渲染后降采样，圆角/文字/图标皆抗锯齿。
const scale = 2

// Canvas 逻辑坐标画布（内部 2x 预乘 RGBA，导出时降采样）。
// 图元走解析覆盖度直写像素（比通用栅格化器快一个量级），复杂多边形走扫描线。
type Canvas struct {
	buf  *image.RGBA
	w, h int      // 逻辑尺寸
	FS   *FontSet // 字体（Shell 注入）
	Th   *Theme   // 主题（Shell 注入）
}

// NewCanvas 创建画布。
func NewCanvas(w, h int) *Canvas {
	return &Canvas{buf: image.NewRGBA(image.Rect(0, 0, w*scale, h*scale)), w: w, h: h}
}

// Size 逻辑尺寸。
func (c *Canvas) Size() Size { return Size{float64(c.w), float64(c.h)} }

// blendPx 预乘 src-over 单像素（col 为预乘色，cov 0..1 覆盖度）。
func (c *Canvas) blendPx(px, py int, col color.RGBA, cov float64) {
	if cov <= 0 {
		return
	}
	if cov > 1 {
		cov = 1
	}
	i := c.buf.PixOffset(px, py)
	a := uint32(float64(col.A) * cov)
	if a == 0 {
		return
	}
	if a >= 255 {
		c.buf.Pix[i+0] = col.R
		c.buf.Pix[i+1] = col.G
		c.buf.Pix[i+2] = col.B
		c.buf.Pix[i+3] = 255
		return
	}
	inv := 255 - a
	c.buf.Pix[i+0] = uint8(uint32(float64(col.R)*cov) + uint32(c.buf.Pix[i+0])*inv/255)
	c.buf.Pix[i+1] = uint8(uint32(float64(col.G)*cov) + uint32(c.buf.Pix[i+1])*inv/255)
	c.buf.Pix[i+2] = uint8(uint32(float64(col.B)*cov) + uint32(c.buf.Pix[i+2])*inv/255)
	c.buf.Pix[i+3] = uint8(a + uint32(c.buf.Pix[i+3])*inv/255)
}

// fillBox 按覆盖度函数在矩形域内填充（d<0 在形内，-d 为进形深度，1px 边缘羽化）。
func (c *Canvas) fillBox(r Rect, radius float64, col Color, sd func(lx, ly float64) float64) {
	if col.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	_ = radius
	px := toRGBA(col)
	x0 := int(r.X*scale) - 1
	y0 := int(r.Y*scale) - 1
	x1 := int((r.X+r.W)*scale) + 1
	y1 := int((r.Y+r.H)*scale) + 1
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > c.w*scale {
		x1 = c.w * scale
	}
	if y1 > c.h*scale {
		y1 = c.h * scale
	}
	for py := y0; py < y1; py++ {
		ly := (float64(py) + 0.5) / scale
		for pxx := x0; pxx < x1; pxx++ {
			lx := (float64(pxx) + 0.5) / scale
			d := sd(lx, ly)
			if d >= 0.5 {
				continue
			}
			cov := 0.5 - d
			if cov > 1 {
				cov = 1
			}
			c.blendPx(pxx, py, px, cov)
		}
	}
}

// roundRectSD 圆角矩形有符号距离（内负外正）。
func roundRectSD(lx, ly float64, r Rect, radius float64) float64 {
	cx := lx - r.X - r.W/2
	cy := ly - r.Y - r.H/2
	dx := math.Abs(cx) - (r.W/2 - radius)
	dy := math.Abs(cy) - (r.H/2 - radius)
	if dx > 0 && dy > 0 {
		return math.Hypot(dx, dy) - radius
	}
	if dx > 0 {
		return dx
	}
	if dy > 0 {
		return dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

// fillConvex 凸形填充快路径：每行二分求截面边界，内区整行批量写、边带逐像素羽化。
// 适用于圆角矩形/圆/矩形这类行截面单调的形状（比逐像素快约 5-10 倍）。
func (c *Canvas) fillConvex(r Rect, radius float64, col Color) {
	if col.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	px := toRGBA(col)
	cx := r.X + r.W/2
	cy := r.Y + r.H/2
	hw := r.W / 2
	hh := r.H / 2
	sd := func(lx, ly float64) float64 {
		dx := math.Abs(lx-cx) - (hw - radius)
		dy := math.Abs(ly-cy) - (hh - radius)
		if dx > 0 && dy > 0 {
			return math.Hypot(dx, dy) - radius
		}
		return maxF(dx, dy)
	}
	y0 := clampI(int(r.Y*scale)-1, 0, c.h*scale)
	y1 := clampI(int((r.Y+r.H)*scale)+1, 0, c.h*scale)
	xMin := r.X
	xMax := r.X + r.W
	for py := y0; py < y1; py++ {
		ly := (float64(py) + 0.5) / scale
		// 行截面为空快速跳过
		if sd(cx, ly) >= 0.5 {
			continue
		}
		// 二分左/右边界（sd 在行内单调）
		bisect := func(lo, hi float64, left bool) float64 {
			for i := 0; i < 14; i++ {
				mid := (lo + hi) / 2
				inside := sd(mid, ly) < 0
				if inside == left {
					hi = mid
				} else {
					lo = mid
				}
			}
			return (lo + hi) / 2
		}
		xL := bisect(xMin, cx, true)
		xR := bisect(cx, xMax, false)
		// 边带（±1px）逐像素羽化
		pL := clampI(int((xL-1)*scale), 0, c.w*scale)
		pR := clampI(int((xR+1)*scale)+1, 0, c.w*scale)
		row := c.buf.PixOffset(0, py)
		// 内区批量写：sd < -0.5 的区间
		iL := clampI(int((xL+0.5)*scale), pL, pR)
		iR := clampI(int((xR-0.5)*scale), pL, pR)
		if iR > iL {
			a := px.A
			inv := 255 - int(a)
			for pxx := iL; pxx < iR; pxx++ {
				o := row + pxx*4
				if a >= 255 {
					c.buf.Pix[o+0] = px.R
					c.buf.Pix[o+1] = px.G
					c.buf.Pix[o+2] = px.B
					c.buf.Pix[o+3] = 255
				} else {
					c.buf.Pix[o+0] = uint8(int(px.R) + int(c.buf.Pix[o+0])*inv/255)
					c.buf.Pix[o+1] = uint8(int(px.G) + int(c.buf.Pix[o+1])*inv/255)
					c.buf.Pix[o+2] = uint8(int(px.B) + int(c.buf.Pix[o+2])*inv/255)
					c.buf.Pix[o+3] = uint8(int(a) + int(c.buf.Pix[o+3])*inv/255)
				}
			}
		}
		// 边带逐像素
		for pxx := pL; pxx < iL; pxx++ {
			lx := (float64(pxx) + 0.5) / scale
			d := sd(lx, ly)
			if d >= 0.5 {
				continue
			}
			cov := 0.5 - d
			if cov > 1 {
				cov = 1
			}
			c.blendPx(pxx, py, px, cov)
		}
		for pxx := iR; pxx < pR; pxx++ {
			lx := (float64(pxx) + 0.5) / scale
			d := sd(lx, ly)
			if d >= 0.5 {
				continue
			}
			cov := 0.5 - d
			if cov > 1 {
				cov = 1
			}
			c.blendPx(pxx, py, px, cov)
		}
	}
}

// Fill 矩形填充。
func (c *Canvas) Fill(r Rect, col Color) {
	c.fillConvex(r, 0, col)
}

// RoundRect 圆角矩形填充。
func (c *Canvas) RoundRect(r Rect, radius float64, col Color) {
	if col.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	radius = clampF(radius, 0, minF(r.W, r.H)/2)
	c.fillConvex(r, radius, col)
}

// Circle 圆填充。
func (c *Canvas) Circle(cx, cy, r float64, col Color) {
	c.fillConvex(Rect{cx - r, cy - r, 2 * r, 2 * r}, r, col)
}

// StrokeRoundRect 圆角矩形描边（环形覆盖度）。
func (c *Canvas) StrokeRoundRect(r Rect, radius, width float64, col Color) {
	c.StrokeRoundRectOutline(r, radius, width, col)
}

// StrokeRoundRectOutline 圆环描边（|d|<width/2 羽化）。
func (c *Canvas) StrokeRoundRectOutline(r Rect, radius, width float64, col Color) {
	if col.A == 0 || width <= 0 {
		return
	}
	radius = clampF(radius, 0, minF(r.W, r.H)/2)
	rr, half := radius, width/2
	c.fillBox(r.Pad(width), radius+width, col, func(lx, ly float64) float64 {
		return math.Abs(roundRectSD(lx, ly, r, rr)) - half
	})
}

// StrokeCircle 圆环描边。
func (c *Canvas) StrokeCircle(cx, cy, r, width float64, col Color) {
	if col.A == 0 || width <= 0 {
		return
	}
	half := width / 2
	bb := Rect{cx - r - width, cy - r - width, 2 * (r + width), 2 * (r + width)}
	c.fillBox(bb, r+width, col, func(lx, ly float64) float64 {
		return math.Abs(math.Hypot(lx-cx, ly-cy)-r) - half
	})
}

// Arc 粗圆弧（进度环/加载圈）。start/end 弧度（0=右，顺时针）。
func (c *Canvas) Arc(cx, cy, r, width, start, end float64, col Color) {
	if col.A == 0 || width <= 0 || end <= start {
		return
	}
	half := width / 2
	bb := Rect{cx - r - width, cy - r - width, 2 * (r + width), 2 * (r + width)}
	// 角度区间外的覆盖度置正（不画）
	full := 2 * math.Pi
	c.fillBox(bb, r+width, col, func(lx, ly float64) float64 {
		dy, dx := ly-cy, lx-cx
		a := math.Atan2(dy, dx)
		if a < 0 {
			a += full
		}
		s, e := start, end
		for s < 0 {
			s += full
		}
		for e < s {
			e += full
		}
		if a < s-0.02 || a > e+0.02 {
			// 端头圆帽
			ds := math.Hypot(lx-(cx+r*math.Cos(s)), ly-(cy+r*math.Sin(s))) - half
			de := math.Hypot(lx-(cx+r*math.Cos(e)), ly-(cy+r*math.Sin(e))) - half
			return minF(ds, de)
		}
		return math.Abs(math.Hypot(dx, dy)-r) - half
	})
}

// Line 线段（圆头）。
func (c *Canvas) Line(x1, y1, x2, y2, width float64, col Color) {
	c.LinePath([]Pt{{x1, y1}, {x2, y2}}, width, col)
}

// LinePath 折线描边（圆头接合，整体一次覆盖取 max，接缝不叠色）。
func (c *Canvas) LinePath(pts []Pt, width float64, col Color) {
	if col.A == 0 || len(pts) < 2 || width <= 0 {
		return
	}
	minX, minY := pts[0].X, pts[0].Y
	maxX, maxY := minX, minY
	for _, p := range pts {
		minX = minF(minX, p.X)
		minY = minF(minY, p.Y)
		maxX = maxF(maxX, p.X)
		maxY = maxF(maxY, p.Y)
	}
	half := width / 2
	bb := Rect{minX - width, minY - width, maxX - minX + 2*width, maxY - minY + 2*width}
	segs := make([][4]float64, 0, len(pts)-1)
	for i := 0; i+1 < len(pts); i++ {
		segs = append(segs, [4]float64{pts[i].X, pts[i].Y, pts[i+1].X, pts[i+1].Y})
	}
	px := toRGBA(col)
	x0 := int(bb.X*scale) - 1
	y0 := int(bb.Y*scale) - 1
	x1 := int((bb.X+bb.W)*scale) + 1
	y1 := int((bb.Y+bb.H)*scale) + 1
	clampBox := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	x0 = clampBox(x0, 0, c.w*scale)
	x1 = clampBox(x1, 0, c.w*scale)
	y0 = clampBox(y0, 0, c.h*scale)
	y1 = clampBox(y1, 0, c.h*scale)
	for py := y0; py < y1; py++ {
		ly := (float64(py) + 0.5) / scale
		for pxx := x0; pxx < x1; pxx++ {
			lx := (float64(pxx) + 0.5) / scale
			best := 1e9
			for _, s := range segs {
				d := distToSeg(lx, ly, s[0], s[1], s[2], s[3])
				if d < best {
					best = d
				}
			}
			sd := best - half
			if sd >= 0.5 {
				continue
			}
			cov := 0.5 - sd
			if cov > 1 {
				cov = 1
			}
			c.blendPx(pxx, py, px, cov)
		}
	}
}

func distToSeg(lx, ly, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	len2 := dx*dx + dy*dy
	if len2 < 1e-12 {
		return math.Hypot(lx-x1, ly-y1)
	}
	t := clampF(((lx-x1)*dx+(ly-y1)*dy)/len2, 0, 1)
	return math.Hypot(lx-(x1+t*dx), ly-(y1+t*dy))
}

// FillPath 多边形填充（扫描线 + 水平 4x 超采样抗锯齿；任意简单多边形）。
func (c *Canvas) FillPath(pts []Pt, col Color) {
	if col.A == 0 || len(pts) < 3 {
		return
	}
	minY, maxY := pts[0].Y, pts[0].Y
	minX, maxX := pts[0].X, pts[0].X
	for _, p := range pts {
		minY = minF(minY, p.Y)
		maxY = maxF(maxY, p.Y)
		minX = minF(minX, p.X)
		maxX = maxF(maxX, p.X)
	}
	px := toRGBA(col)
	y0 := int(minY*scale) - 1
	y1 := int(maxY*scale) + 1
	x0 := clampI(int(minX*scale)-1, 0, c.w*scale)
	x1 := clampI(int(maxX*scale)+2, 0, c.w*scale)
	y0 = clampI(y0, 0, c.h*scale)
	y1 = clampI(y1, 0, c.h*scale)
	xs := make([]float64, 0, 16)
	for py := y0; py < y1; py++ {
		ly := (float64(py) + 0.5) / scale
		xs = xs[:0]
		for i := 0; i < len(pts); i++ {
			a := pts[i]
			b := pts[(i+1)%len(pts)]
			if (a.Y <= ly && b.Y > ly) || (b.Y <= ly && a.Y > ly) {
				t := (ly - a.Y) / (b.Y - a.Y)
				xs = append(xs, a.X+t*(b.X-a.X))
			}
		}
		// 排序交点，奇偶填充
		for i := 1; i < len(xs); i++ {
			v := xs[i]
			j := i - 1
			for j >= 0 && xs[j] > v {
				xs[j+1] = xs[j]
				j--
			}
			xs[j+1] = v
		}
		for k := 0; k+1 < len(xs); k += 2 {
			segA, segB := xs[k], xs[k+1]
			// 逐像素 + 4x 水平超采样
			pxStart := clampI(int(segA*scale), x0, x1)
			pxEnd := clampI(int(segB*scale)+1, x0, x1)
			for pxx := pxStart; pxx < pxEnd; pxx++ {
				cov := 0.0
				for s := 0; s < 4; s++ {
					lx := (float64(pxx) + (float64(s)+0.5)/4) / scale
					if lx >= segA && lx < segB {
						cov += 0.25
					}
				}
				if cov > 0 {
					c.blendPx(pxx, py, px, cov)
				}
			}
		}
	}
}

// Shadow 圆角矩形柔和阴影（多层同心渐隐近似高斯）。
func (c *Canvas) Shadow(r Rect, radius, blur float64, col Color) {
	if col.A == 0 {
		return
	}
	steps := 4
	for i := steps; i >= 1; i-- {
		f := float64(i) / float64(steps)
		expand := blur * f
		a := 0.17 * (1 - f) * (1 - f)
		c.RoundRect(r.Add(0, blur*0.3*f).Pad(expand), radius+expand, col.Mul(a))
	}
}

// Image 将外部图片绘制到矩形（缩放+透明混合）。
func (c *Canvas) Image(img image.Image, r Rect) {
	dst := image.Rect(int(r.X*scale), int(r.Y*scale), int((r.X+r.W)*scale), int((r.Y+r.H)*scale))
	tmp := image.NewRGBA(dst)
	xdraw.CatmullRom.Scale(tmp, dst, img, img.Bounds(), draw.Over, nil)
	draw.Draw(c.buf, dst, tmp, image.Point{}, draw.Over)
}

// Clear 用纯色清空。
func (c *Canvas) Clear(col Color) {
	p := toRGBA(col)
	px := c.buf.Pix
	for i := 0; i < len(px); i += 4 {
		px[i+0] = p.R
		px[i+1] = p.G
		px[i+2] = p.B
		px[i+3] = p.A
	}
}

// Downsample 2x 降采样输出（预乘域平均，输出仍为预乘 RGBA——与 png 编码兼容）。
func (c *Canvas) Downsample() *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	src := c.buf.Pix
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			i0 := c.buf.PixOffset(x*scale, y*scale)
			i1 := i0 + 4
			i2 := i0 + c.buf.Stride
			i3 := i2 + 4
			o := out.PixOffset(x, y)
			out.Pix[o+0] = uint8((uint32(src[i0]) + uint32(src[i1]) + uint32(src[i2]) + uint32(src[i3])) / 4)
			out.Pix[o+1] = uint8((uint32(src[i0+1]) + uint32(src[i1+1]) + uint32(src[i2+1]) + uint32(src[i3+1])) / 4)
			out.Pix[o+2] = uint8((uint32(src[i0+2]) + uint32(src[i1+2]) + uint32(src[i2+2]) + uint32(src[i3+2])) / 4)
			out.Pix[o+3] = uint8((uint32(src[i0+3]) + uint32(src[i1+3]) + uint32(src[i2+3]) + uint32(src[i3+3])) / 4)
		}
	}
	return out
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func toRGBA(col Color) color.RGBA {
	if col.A == 255 {
		return color.RGBA{col.R, col.G, col.B, 255}
	}
	// 预乘 alpha（buffer 语义：RGB 已乘 A）
	return color.RGBA{
		uint8(uint32(col.R) * uint32(col.A) / 255),
		uint8(uint32(col.G) * uint32(col.A) / 255),
		uint8(uint32(col.B) * uint32(col.A) / 255),
		col.A,
	}
}
