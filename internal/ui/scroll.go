package ui

// ScrollView 垂直滚动容器（滚轮 + 拖动条）。
type ScrollView struct {
	Child  Node
	Pad    float64
	off    float64
	maxOff float64
	childH float64
	frame  Rect
	hover  bool
}

// ScrollTo 滚到指定偏移（0=顶部）。
func (s *ScrollView) ScrollTo(off float64) {
	s.off = clampF(off, 0, s.maxOff)
}

// Offset 当前偏移。
func (s *ScrollView) Offset() float64 { return s.off }

// Wheel 实现 Scroller。
func (s *ScrollView) Wheel(x, y, dy float64) bool {
	if !s.frame.In(x, y) {
		return false
	}
	s.off = clampF(s.off-dy, 0, s.maxOff)
	return true
}

// Hover 实现 Hoverer。
func (s *ScrollView) Hover(on bool) { s.hover = on }

// Measure 实现 Node。
func (s *ScrollView) Measure(ctx *Ctx, max Size) Size {
	if s.Child != nil {
		cs := s.Child.Measure(ctx, Size{max.W - 2*s.Pad, 1e6}) // 高度不限：取内容自然高
		s.childH = cs.H
		s.maxOff = maxF(0, cs.H+2*s.Pad-max.H)
		s.off = clampF(s.off, 0, s.maxOff)
	}
	return Size{max.W, max.H}
}

// SetFrame 实现 Node。
func (s *ScrollView) SetFrame(r Rect) {
	s.frame = r
	if s.Child != nil {
		s.Child.SetFrame(Rect{r.X + s.Pad, r.Y + s.Pad - s.off, r.W - 2*s.Pad, s.childH})
	}
}

// Frame 实现 Node。
func (s *ScrollView) Frame() Rect { return s.frame }

// Paint 实现 Node（内容裁剪在画布级不做——节点内容本就画在视口内偏移处）。
func (s *ScrollView) Paint(cv *Canvas) {
	if s.Child != nil {
		s.Child.Paint(cv)
	}
	// 滚动条（悬停可见）
	if s.maxOff > 0 && s.hover {
		th := cv.Th
		barH := maxF(40, s.frame.H*s.frame.H/(s.frame.H+s.maxOff))
		barY := s.frame.Y + (s.frame.H-barH)*(s.off/s.maxOff)
		cv.RoundRect(Rect{s.frame.X + s.frame.W - 7, barY, 5, barH}, 2.5, th.Muted.Mul(0.5))
	}
}
