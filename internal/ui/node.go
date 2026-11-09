package ui

// Node 控件树节点：测量 → 布局 → 绘制；交互由可选接口表达。
type Node interface {
	Measure(ctx *Ctx, max Size) Size // 期望尺寸（max 为可用空间）
	SetFrame(r Rect)                 // 布局器指派位置
	Frame() Rect
	Paint(cv *Canvas)
}

// Tapper 可点击。
type Tapper interface {
	Tap(x, y float64) bool
}

// Hoverer 悬停态。
type Hoverer interface {
	Hover(on bool)
}

// Scroller 滚轮。
type Scroller interface {
	Wheel(x, y, dy float64) bool
}

// Typable 文本输入焦点。
type Typable interface {
	Focused(on bool)
	Type(s string)
	Backspace()
	MoveCaret(delta int)
	Submit() bool
	Value() string
}

// Ctx 测量/绘制环境（字体与主题）。
type Ctx struct {
	FS    *FontSet
	Theme *Theme
}

// ---------- 基础容器 ----------

// Box 样式盒：背景/圆角/描边/阴影/内边距 + 单子节点。
type Box struct {
	Pad     float64
	PadX    float64 // 覆盖左右
	PadY    float64 // 覆盖上下
	BG      Color
	Radius  float64
	Border  Color
	BorderW float64
	Shadow  bool
	Child   Node
	MinW    float64
	MinH    float64
	frame   Rect
	onTap   func()
	hover   bool
}

// OnTap 点击回调（整盒热区）。
func (b *Box) OnTap(f func()) { b.onTap = f }

// Tap 实现 Tapper。
func (b *Box) Tap(x, y float64) bool {
	if b.onTap != nil && b.frame.In(x, y) {
		b.onTap()
		return true
	}
	return false
}

// Hover 实现 Hoverer。
func (b *Box) Hover(on bool) { b.hover = on }

// Measure 实现 Node。
func (b *Box) Measure(ctx *Ctx, max Size) Size {
	px, py := b.padXY()
	inner := Size{max.W - 2*px, max.H - 2*py}
	var cs Size
	if b.Child != nil {
		cs = b.Child.Measure(ctx, inner)
	}
	return Size{maxF(cs.W+2*px, b.MinW), maxF(cs.H+2*py, b.MinH)}
}

// SetFrame 实现 Node。
func (b *Box) SetFrame(r Rect) {
	b.frame = r
	if b.Child != nil {
		px, py := b.padXY()
		b.Child.SetFrame(Rect{r.X + px, r.Y + py, r.W - 2*px, r.H - 2*py})
	}
}

// Frame 实现 Node。
func (b *Box) Frame() Rect { return b.frame }

// Paint 实现 Node。
func (b *Box) Paint(cv *Canvas) {
	if b.Shadow {
		cv.Shadow(b.frame, b.Radius, 14, RGBA(30, 34, 60, 32))
	}
	if b.BG.A > 0 {
		cv.RoundRect(b.frame, b.Radius, b.BG)
	}
	if b.Border.A > 0 && b.BorderW > 0 {
		cv.StrokeRoundRectOutline(b.frame, b.Radius, b.BorderW, b.Border)
	}
	if b.Child != nil {
		b.Child.Paint(cv)
	}
}

func (b *Box) padXY() (float64, float64) {
	px, py := b.Pad, b.Pad
	if b.PadX > 0 {
		px = b.PadX
	}
	if b.PadY > 0 {
		py = b.PadY
	}
	return px, py
}

// Stack 布局方向。
type Axis int

// 纵向 / 横向。
const (
	Vertical Axis = iota
	Horizontal
)

// Stack 线性布局：方向、间距、主轴对齐、交叉轴拉伸。
type Stack struct {
	Axis     Axis
	Gap      float64
	Align    float64 // 交叉轴对齐 0=起始 0.5=居中 1=末端
	Expand   bool    // 交叉轴拉伸
	Pad      float64
	Children []Node
	frame    Rect
	sizes    []Size // Measure 缓存（SetFrame 复用，避免空 Ctx 复测）
}

// isFlex 是否主轴弹性子件（Stretch/游标空白：吃剩余主轴空间）。
func isFlex(n Node) bool {
	switch v := n.(type) {
	case *Stretch:
		return v != nil
	case *Spacer:
		return v.Grow
	}
	return false
}

// Measure 实现 Node（两遍法：先常规后弹性分剩余主轴）。
func (s *Stack) Measure(ctx *Ctx, max Size) Size {
	s.sizes = make([]Size, len(s.Children))
	gaps := s.Gap * float64(maxInt(0, len(s.Children)-1))
	var main, cross float64
	if s.Axis == Horizontal {
		avail := max.W - gaps
		flex := []int{}
		for i, ch := range s.Children {
			if isFlex(ch) {
				flex = append(flex, i)
				continue
			}
			cs := ch.Measure(ctx, Size{avail, max.H})
			s.sizes[i] = cs
			main += cs.W
			avail -= cs.W
			if cs.H > cross {
				cross = cs.H
			}
		}
		if len(flex) > 0 {
			each := maxF(0, avail) / float64(len(flex))
			for _, i := range flex {
				cs := s.Children[i].Measure(ctx, Size{each, max.H})
				s.sizes[i] = Size{each, cs.H}
				main += each
				if cs.H > cross {
					cross = cs.H
				}
			}
		}
		return Size{main + gaps + 2*s.Pad, cross + 2*s.Pad}
	}
	avail := max.H - gaps
	flex := []int{}
	for i, ch := range s.Children {
		if isFlex(ch) {
			flex = append(flex, i)
			continue
		}
		cs := ch.Measure(ctx, Size{max.W, avail})
		s.sizes[i] = cs
		main += cs.H
		avail -= cs.H
		if cs.W > cross {
			cross = cs.W
		}
	}
	if len(flex) > 0 {
		each := maxF(0, avail) / float64(len(flex))
		for _, i := range flex {
			cs := s.Children[i].Measure(ctx, Size{max.W, each})
			s.sizes[i] = Size{cs.W, each}
			main += each
			if cs.W > cross {
				cross = cs.W
			}
		}
	}
	return Size{cross + 2*s.Pad, main + gaps + 2*s.Pad}
}

// SetFrame 实现 Node。
func (s *Stack) SetFrame(r Rect) {
	s.frame = r
	inner := r.Shrink(s.Pad)
	off := 0.0
	for i, ch := range s.Children {
		var cs Size
		if i < len(s.sizes) {
			cs = s.sizes[i]
		} else {
			cs = ch.Measure(&Ctx{}, Size{inner.W, inner.H})
		}
		if s.Axis == Horizontal {
			w := cs.W // 主轴恒取自然宽（Expand 只作用于交叉轴）
			h := cs.H
			y := inner.Y
			if s.Expand {
				h = inner.H
			} else {
				y += (inner.H - h) * s.Align
			}
			ch.SetFrame(Rect{inner.X + off, y, w, h})
			off += w + s.Gap
		} else {
			h := cs.H // 主轴恒取自然高
			w := cs.W
			x := inner.X
			if s.Expand {
				w = inner.W
			} else {
				x += (inner.W - w) * s.Align
			}
			ch.SetFrame(Rect{x, inner.Y + off, w, h})
			off += h + s.Gap
		}
	}
}

// Frame 实现 Node。
func (s *Stack) Frame() Rect { return s.frame }

// Paint 实现 Node。
func (s *Stack) Paint(cv *Canvas) {
	for _, ch := range s.Children {
		ch.Paint(cv)
	}
}

// V 纵向堆叠快捷构造。
func V(gap float64, children ...Node) *Stack {
	return &Stack{Axis: Vertical, Gap: gap, Expand: true, Children: children}
}

// H 横向堆叠快捷构造。
func H(gap float64, children ...Node) *Stack {
	return &Stack{Axis: Horizontal, Gap: gap, Expand: true, Children: children}
}

// VA 纵向堆叠（交叉轴自适应对齐）。
func VA(gap, align float64, children ...Node) *Stack {
	return &Stack{Axis: Vertical, Gap: gap, Align: align, Children: children}
}

// HA 横向堆叠（交叉轴自适应对齐）。
func HA(gap, align float64, children ...Node) *Stack {
	return &Stack{Axis: Horizontal, Gap: gap, Align: align, Children: children}
}

// Spacer 弹性空白。
type Spacer struct {
	W     float64
	H     float64
	Grow  bool
	frame Rect
}

// Measure 实现 Node。
func (s *Spacer) Measure(_ *Ctx, _ Size) Size { return Size{s.W, s.H} }

// SetFrame 实现 Node。
func (s *Spacer) SetFrame(r Rect) {
	s.frame = r
	if s.Grow {
		s.W, s.H = r.W, r.H
	}
}

// Frame 实现 Node。
func (s *Spacer) Frame() Rect { return s.frame }

// Paint 实现 Node。
func (s *Spacer) Paint(_ *Canvas) {}

// Fill 占满剩余空间。
func Fill() *Spacer { return &Spacer{Grow: true} }

// Fixed 固定尺寸空白。
func Fixed(w, h float64) *Spacer { return &Spacer{W: w, H: h} }

// Divider 分隔线。
type Divider struct {
	Color  Color
	Frame_ Rect
	frame  Rect
}

// Measure 实现 Node（0 宽参与主轴统计，交叉/宽度由容器 Expand 拉伸）。
func (d *Divider) Measure(_ *Ctx, max Size) Size {
	if d.Frame_.H > 0 {
		return Size{0, d.Frame_.H}
	}
	return Size{0, 1}
}

// SetFrame 实现 Node。
func (d *Divider) SetFrame(r Rect) { d.frame = r }

// Frame 实现 Node。
func (d *Divider) Frame() Rect { return d.frame }

// Paint 实现 Node。
func (d *Divider) Paint(cv *Canvas) {
	col := d.Color
	if col.A == 0 {
		col = RGB(227, 229, 238)
	}
	cv.Fill(d.frame, col)
}

// Stretch 弹性子件：占满可用宽度（行内输入框/搜索框用），高度取自然。
type Stretch struct {
	Child  Node
	frame  Rect
	height float64
}

// Measure 实现 Node。
func (st *Stretch) Measure(ctx *Ctx, max Size) Size {
	if st.Child != nil {
		cs := st.Child.Measure(ctx, max)
		st.height = cs.H
	}
	return Size{max.W, st.height}
}

// SetFrame 实现 Node。
func (st *Stretch) SetFrame(r Rect) {
	st.frame = r
	if st.Child != nil {
		st.Child.SetFrame(r)
	}
}

// Frame 实现 Node。
func (st *Stretch) Frame() Rect { return st.frame }

// Paint 实现 Node。
func (st *Stretch) Paint(cv *Canvas) {
	if st.Child != nil {
		st.Child.Paint(cv)
	}
}

// Columns 等宽多列（统计卡行/网格）。
type Columns struct {
	Gap      float64
	Children []Node
	frame    Rect
}

// Measure 实现 Node。
func (cn *Columns) Measure(ctx *Ctx, max Size) Size {
	cw := (max.W - cn.Gap*float64(maxInt(0, len(cn.Children)-1))) / float64(maxInt(1, len(cn.Children)))
	var h float64
	for _, ch := range cn.Children {
		cs := ch.Measure(ctx, Size{cw, max.H})
		if cs.H > h {
			h = cs.H
		}
	}
	return Size{max.W, h}
}

// SetFrame 实现 Node。
func (cn *Columns) SetFrame(r Rect) {
	cn.frame = r
	cw := (r.W - cn.Gap*float64(maxInt(0, len(cn.Children)-1))) / float64(maxInt(1, len(cn.Children)))
	x := r.X
	for _, ch := range cn.Children {
		ch.SetFrame(Rect{x, r.Y, cw, r.H})
		x += cw + cn.Gap
	}
}

// Frame 实现 Node。
func (cn *Columns) Frame() Rect { return cn.frame }

// Paint 实现 Node。
func (cn *Columns) Paint(cv *Canvas) {
	for _, ch := range cn.Children {
		ch.Paint(cv)
	}
}

// Col 等分列快捷构造。
func Col(gap float64, children ...Node) *Columns { return &Columns{Gap: gap, Children: children} }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
