package ui

import (
	"math"
)

// Icon 矢量图标（自绘：0-24 坐标系描边线稿，风格统一 2px 圆头）。
type Icon int

// 图标集（覆盖 WebUI 全部语义）。
const (
	IconHome Icon = iota
	IconFolder
	IconFile
	IconFileText
	IconSearch
	IconPlus
	IconTrash
	IconEdit
	IconCheck
	IconX
	IconChevronRight
	IconChevronDown
	IconChevronLeft
	IconKey
	IconShield
	IconBell
	IconSettings
	IconUsers
	IconUpload
	IconDownload
	IconRefresh
	IconLogout
	IconDatabase
	IconCalendar
	IconClock
	IconList
	IconCopy
	IconLink
	IconPlay
	IconAlert
	IconInfo
	IconMessage
	IconSend
	IconGrid
	IconStar
	IconEye
	IconLock
	IconServer
	IconMenu
	IconMore
	IconFilter
)

// DrawIcon 在 (x,y) 左上角绘制 size 尺寸图标。
func (c *Canvas) DrawIcon(kind Icon, x, y, size float64, col Color) {
	s := size / 24 // 0-24 坐标缩放
	w := 2 * s     // 线宽
	if w < 1.1 {
		w = 1.1
	}
	// P 将 24 系坐标映射到画布
	P := func(px, py float64) Pt { return Pt{x + px*s, y + py*s} }
	poly := func(pts ...[2]float64) {
		pp := make([]Pt, len(pts))
		for i, v := range pts {
			pp[i] = P(v[0], v[1])
		}
		c.LinePath(pp, w, col)
	}
	circ := func(cx, cy, r float64) { c.StrokeCircle(x+cx*s, y+cy*s, r*s, w, col) }
	arc := func(cx, cy, r, a0, a1 float64) { c.Arc(x+cx*s, y+cy*s, r*s, w, a0, a1, col) }
	dot := func(cx, cy, r float64) { c.Circle(x+cx*s, y+cy*s, r*s, col) }
	ell := func(cx, cy, rx, ry float64) { ellipseFor(c, x+cx*s, y+cy*s, rx*s, ry*s, w, col) }
	fillPoly := func(pts ...[2]float64) {
		pp := make([]Pt, len(pts))
		for i, v := range pts {
			pp[i] = P(v[0], v[1])
		}
		c.FillPath(pp, col)
	}

	switch kind {
	case IconHome:
		poly([2]float64{3, 11}, [2]float64{12, 3}, [2]float64{21, 11})
		poly([2]float64{5, 9.5}, [2]float64{5, 20}, [2]float64{19, 20}, [2]float64{19, 9.5})
		poly([2]float64{10, 20}, [2]float64{10, 14}, [2]float64{14, 14}, [2]float64{14, 20})
	case IconFolder:
		poly([2]float64{3, 19}, [2]float64{3, 6}, [2]float64{10, 6}, [2]float64{12, 8.2}, [2]float64{21, 8.2}, [2]float64{21, 19}, [2]float64{3, 19})
	case IconFile:
		poly([2]float64{6, 3}, [2]float64{14, 3}, [2]float64{19, 8}, [2]float64{19, 21}, [2]float64{6, 21}, [2]float64{6, 3})
		poly([2]float64{14, 3}, [2]float64{14, 8}, [2]float64{19, 8})
	case IconFileText:
		poly([2]float64{6, 3}, [2]float64{14, 3}, [2]float64{19, 8}, [2]float64{19, 21}, [2]float64{6, 21}, [2]float64{6, 3})
		poly([2]float64{14, 3}, [2]float64{14, 8}, [2]float64{19, 8})
		poly([2]float64{9, 12}, [2]float64{15, 12})
		poly([2]float64{9, 16}, [2]float64{15, 16})
	case IconSearch:
		circ(10.5, 10.5, 6.5)
		poly([2]float64{15.6, 15.6}, [2]float64{21, 21})
	case IconPlus:
		poly([2]float64{12, 5}, [2]float64{12, 19})
		poly([2]float64{5, 12}, [2]float64{19, 12})
	case IconTrash:
		poly([2]float64{4, 7}, [2]float64{20, 7})
		poly([2]float64{9, 7}, [2]float64{9, 4.2}, [2]float64{15, 4.2}, [2]float64{15, 7})
		poly([2]float64{6.2, 7}, [2]float64{7.2, 20}, [2]float64{16.8, 20}, [2]float64{17.8, 7})
		poly([2]float64{10, 10.5}, [2]float64{10, 16.5})
		poly([2]float64{14, 10.5}, [2]float64{14, 16.5})
	case IconEdit:
		poly([2]float64{4, 20}, [2]float64{4, 16}, [2]float64{15, 5}, [2]float64{19, 9}, [2]float64{8, 20}, [2]float64{4, 20})
		poly([2]float64{13, 7}, [2]float64{17, 11})
	case IconCheck:
		poly([2]float64{4, 12.5}, [2]float64{9.5, 18}, [2]float64{20, 6})
	case IconX:
		poly([2]float64{6, 6}, [2]float64{18, 18})
		poly([2]float64{18, 6}, [2]float64{6, 18})
	case IconChevronRight:
		poly([2]float64{9, 5}, [2]float64{16, 12}, [2]float64{9, 19})
	case IconChevronLeft:
		poly([2]float64{15, 5}, [2]float64{8, 12}, [2]float64{15, 19})
	case IconChevronDown:
		poly([2]float64{5, 9}, [2]float64{12, 16}, [2]float64{19, 9})
	case IconKey:
		circ(8, 12, 4.2)
		poly([2]float64{12, 12}, [2]float64{21, 12})
		poly([2]float64{18, 12}, [2]float64{18, 15.5})
		poly([2]float64{21, 12}, [2]float64{21, 15})
	case IconShield:
		poly([2]float64{12, 3}, [2]float64{20, 6}, [2]float64{20, 11.5}, [2]float64{12, 21}, [2]float64{4, 11.5}, [2]float64{4, 6}, [2]float64{12, 3})
		poly([2]float64{8.5, 11.5}, [2]float64{11, 14}, [2]float64{15.5, 9})
	case IconBell:
		poly([2]float64{6, 16}, [2]float64{6, 10}, [2]float64{12, 4}, [2]float64{18, 10}, [2]float64{18, 16}, [2]float64{6, 16})
		poly([2]float64{4, 16}, [2]float64{20, 16})
		poly([2]float64{10, 18.5}, [2]float64{10, 19.5}, [2]float64{14, 19.5}, [2]float64{14, 18.5})
	case IconSettings:
		circ(12, 12, 3.2)
		for i := 0; i < 8; i++ {
			a := float64(i) * math.Pi / 4
			r0, r1 := 6.2, 9.0
			if i%2 == 1 {
				r0, r1 = 6.6, 8.2
			}
			poly([2]float64{12 + r0*math.Cos(a), 12 + r0*math.Sin(a)},
				[2]float64{12 + r1*math.Cos(a), 12 + r1*math.Sin(a)})
		}
	case IconUsers:
		circ(9, 8.5, 3.6)
		arc(9, 18, 5.4, math.Pi, 2*math.Pi)
		circ(16.5, 9.5, 2.8)
		arc(16.5, 17.5, 4.2, math.Pi*1.15, math.Pi*1.9)
	case IconUpload:
		poly([2]float64{12, 4}, [2]float64{12, 15})
		poly([2]float64{7.5, 8.5}, [2]float64{12, 4}, [2]float64{16.5, 8.5})
		poly([2]float64{4, 16}, [2]float64{4, 20}, [2]float64{20, 20}, [2]float64{20, 16})
	case IconDownload:
		poly([2]float64{12, 4}, [2]float64{12, 15})
		poly([2]float64{7.5, 10.5}, [2]float64{12, 15}, [2]float64{16.5, 10.5})
		poly([2]float64{4, 16}, [2]float64{4, 20}, [2]float64{20, 20}, [2]float64{20, 16})
	case IconRefresh:
		arc(12, 12, 7.5, math.Pi*0.25, math.Pi*1.65)
		fillPoly([2]float64{17.6, 3.6}, [2]float64{20.2, 8.8}, [2]float64{14.8, 8.6})
	case IconLogout:
		poly([2]float64{14, 5}, [2]float64{14, 8}, [2]float64{6, 8}, [2]float64{6, 16}, [2]float64{14, 16}, [2]float64{14, 19})
		poly([2]float64{10, 12}, [2]float64{21, 12})
		poly([2]float64{17.5, 8.5}, [2]float64{21, 12}, [2]float64{17.5, 15.5})
	case IconDatabase:
		ell(12, 6, 7, 2.8)
		poly([2]float64{5, 6}, [2]float64{5, 18})
		poly([2]float64{19, 6}, [2]float64{19, 18})
		arc(12, 18, 7, 0, math.Pi)
		arc(12, 12, 7, 0, math.Pi)
	case IconCalendar:
		roundBox := func(x0, y0, x1, y1 float64) {
			poly([2]float64{x0, y0}, [2]float64{x1, y0}, [2]float64{x1, y1}, [2]float64{x0, y1}, [2]float64{x0, y0})
		}
		roundBox(4, 6, 20, 20)
		poly([2]float64{4, 10}, [2]float64{20, 10})
		poly([2]float64{8.5, 3.5}, [2]float64{8.5, 7})
		poly([2]float64{15.5, 3.5}, [2]float64{15.5, 7})
		dot(8.5, 14, 1.1)
		dot(12, 14, 1.1)
		dot(15.5, 14, 1.1)
	case IconClock:
		circ(12, 12, 8.2)
		poly([2]float64{12, 7}, [2]float64{12, 12}, [2]float64{16, 14.5})
	case IconList:
		for _, yy := range []float64{7, 12, 17} {
			dot(5, yy, 1.2)
			poly([2]float64{9, yy}, [2]float64{20, yy})
		}
	case IconCopy:
		poly([2]float64{8, 8}, [2]float64{8, 20}, [2]float64{19, 20}, [2]float64{19, 8}, [2]float64{8, 8})
		poly([2]float64{5, 16}, [2]float64{5, 4}, [2]float64{16, 4})
	case IconLink:
		arc(8, 16, 4.2, math.Pi*1.25, math.Pi*2.75)
		arc(16, 8, 4.2, math.Pi*0.25, math.Pi*1.75)
		poly([2]float64{9.5, 14.5}, [2]float64{14.5, 9.5})
	case IconPlay:
		fillPoly([2]float64{8, 5}, [2]float64{19, 12}, [2]float64{8, 19})
	case IconAlert:
		poly([2]float64{12, 3.5}, [2]float64{21, 19.5}, [2]float64{3, 19.5}, [2]float64{12, 3.5})
		poly([2]float64{12, 9}, [2]float64{12, 14})
		dot(12, 17, 1.1)
	case IconInfo:
		circ(12, 12, 8.4)
		dot(12, 8, 1.2)
		poly([2]float64{12, 11}, [2]float64{12, 17})
	case IconMessage:
		poly([2]float64{4, 5.5}, [2]float64{20, 5.5}, [2]float64{20, 16}, [2]float64{12, 16}, [2]float64{8, 19.5}, [2]float64{8, 16}, [2]float64{4, 16}, [2]float64{4, 5.5})
	case IconSend:
		fillPoly([2]float64{3, 11.5}, [2]float64{21, 4}, [2]float64{13.5, 20.5}, [2]float64{11, 13}, [2]float64{3, 11.5})
	case IconGrid:
		for _, xx := range []float64{5, 14} {
			for _, yy := range []float64{5, 14} {
				poly([2]float64{xx, yy}, [2]float64{xx + 5, yy}, [2]float64{xx + 5, yy + 5}, [2]float64{xx, yy + 5}, [2]float64{xx, yy})
			}
		}
	case IconStar:
		fillPoly([2]float64{12, 3}, [2]float64{14.4, 8.8}, [2]float64{20.5, 9.2}, [2]float64{15.8, 13.3}, [2]float64{17.3, 19.3},
			[2]float64{12, 15.8}, [2]float64{6.7, 19.3}, [2]float64{8.2, 13.3}, [2]float64{3.5, 9.2}, [2]float64{9.6, 8.8})
	case IconEye:
		arc(12, 12, 9, math.Pi*1.05, math.Pi*1.95)
		arc(12, 12, 9, math.Pi*0.05, math.Pi*0.95)
		circ(12, 12, 3.1)
	case IconLock:
		poly([2]float64{5, 11}, [2]float64{19, 11}, [2]float64{19, 20}, [2]float64{5, 20}, [2]float64{5, 11})
		arc(8.2, 11, 3.6, math.Pi, 2*math.Pi)
		arc(15.8, 11, 3.6, math.Pi, 2*math.Pi)
		dot(12, 15.2, 1.3)
	case IconServer:
		poly([2]float64{4, 5}, [2]float64{20, 5}, [2]float64{20, 10}, [2]float64{4, 10}, [2]float64{4, 5})
		poly([2]float64{4, 14}, [2]float64{20, 14}, [2]float64{20, 19}, [2]float64{4, 19}, [2]float64{4, 14})
		dot(7.5, 7.5, 1)
		dot(7.5, 16.5, 1)
	case IconMenu:
		for _, yy := range []float64{6.5, 12, 17.5} {
			poly([2]float64{4, yy}, [2]float64{20, yy})
		}
	case IconMore:
		for _, xx := range []float64{5.5, 12, 18.5} {
			dot(xx, 12, 1.4)
		}
	case IconFilter:
		fillPoly([2]float64{3, 5}, [2]float64{21, 5}, [2]float64{14, 12.5}, [2]float64{14, 19}, [2]float64{10, 21}, [2]float64{10, 12.5})
	default:
		circ(12, 12, 8)
	}
}

// ellipse 椭圆描边（解析环形覆盖度）。
func ellipseFor(c *Canvas, x, y, rx, ry, w float64, col Color) {
	half := w / 2
	bb := Rect{x - rx - w, y - ry - w, 2 * (rx + w), 2 * (ry + w)}
	c.fillBox(bb, 0, col, func(lx, ly float64) float64 {
		dx := (lx - x) / maxF(0.1, rx)
		dy := (ly - y) / maxF(0.1, ry)
		d := (math.Hypot(dx, dy) - 1) * minF(rx, ry)
		return math.Abs(d) - half
	})
}
