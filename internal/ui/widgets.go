package ui

import (
	"strings"
	"time"
)

// Label 文本（单行省略 / 多行折行）。
type Label struct {
	Text    string
	Style   TextStyle
	Align   float64 // 0 左 0.5 中 1 右
	MaxLine int     // 0=单行省略；>0 折行行数
	frame   Rect
	lines   []string
}

// L 单行标签快捷构造。
func L(text string, size float64, col Color) *Label {
	return &Label{Text: text, Style: TextStyle{Size: size, Color: col}}
}

// LB 粗体标签。
func LB(text string, size float64, col Color) *Label {
	return &Label{Text: text, Style: TextStyle{Size: size, Bold: true, Color: col}}
}

// Measure 实现 Node。
func (l *Label) Measure(ctx *Ctx, max Size) Size {
	lh := ctx.FS.LineHeight(l.Style)
	if l.MaxLine > 0 {
		l.lines = ctx.FS.WrapText(l.Text, max.W, l.Style, l.MaxLine)
		w := 0.0
		for _, ln := range l.lines {
			if m := ctx.FS.Measure(ln, l.Style).W; m > w {
				w = m
			}
		}
		return Size{minF(w, max.W), lh * float64(len(l.lines))}
	}
	m := ctx.FS.Measure(l.Text, l.Style)
	return Size{minF(m.W, max.W), lh}
}

// SetFrame 实现 Node。
func (l *Label) SetFrame(r Rect) { l.frame = r }

// Frame 实现 Node。
func (l *Label) Frame() Rect { return l.frame }

// Paint 实现 Node。
func (l *Label) Paint(cv *Canvas) {
	st := l.Style
	if l.lines != nil {
		lh := cv.LineHeight(st)
		for i, ln := range l.lines {
			m := cv.MeasureText(ln, st).W
			x := l.frame.X + (l.frame.W-m)*l.Align
			cv.TextAt(ln, x, l.frame.Y+float64(i)*lh, st)
		}
		return
	}
	// 单行：超宽省略 + 对齐 + 垂直居中
	txt := cv.Ellipsize(l.Text, l.frame.W, st)
	m := cv.MeasureText(txt, st).W
	x := l.frame.X + (l.frame.W-m)*l.Align
	cv.TextAt(txt, x, l.frame.Y+(l.frame.H-cv.LineHeight(st))/2, st)
}

// Button 变体。
type BtnStyle int

// 按钮三态。
const (
	BtnPrimary BtnStyle = iota
	BtnGhost
	BtnDanger
	BtnSoft
)

// Button 按钮（图标+文字，四变体，禁用态）。
type Button struct {
	Text    string
	Icon    Icon
	HasIcon bool
	Style   BtnStyle
	Small   bool
	Disable bool
	Full    bool
	onClick func()
	frame   Rect
	hover   bool
	press   bool
}

// Btn 主按钮快捷构造。
func Btn(text string, onClick func()) *Button {
	return &Button{Text: text, onClick: onClick}
}

// NewButton 变体按钮快捷构造。
func NewButton(text string, style BtnStyle, onClick func()) *Button {
	return &Button{Text: text, Style: style, onClick: onClick}
}

// BtnIcon 图标按钮（ghost 小）。
func BtnIcon(icon Icon, onClick func()) *Button {
	return &Button{Icon: icon, HasIcon: true, Style: BtnGhost, Small: true, onClick: onClick}
}

// OnTap 实现 Tapper。
func (b *Button) Tap(x, y float64) bool {
	if b.Disable || !b.frame.In(x, y) {
		return false
	}
	if b.onClick != nil {
		b.onClick()
	}
	return true
}

// Hover 实现 Hoverer。
func (b *Button) Hover(on bool) { b.hover = on }

// Measure 实现 Node。
func (b *Button) Measure(ctx *Ctx, max Size) Size {
	padX, h := 14.0, 36.0
	if b.Small {
		padX, h = 9.0, 28.0
	}
	tw := 0.0
	if b.Text != "" {
		tw = ctx.FS.Measure(b.Text, TextStyle{Size: 13, Bold: true}).W
	}
	ic := 0.0
	if b.HasIcon {
		ic = 15
		if b.Text != "" {
			ic += 6
		}
	}
	w := tw + ic + 2*padX
	if b.Small {
		w = tw + ic + 2*padX
	}
	return Size{minF(w, max.W), h}
}

// SetFrame 实现 Node。
func (b *Button) SetFrame(r Rect) { b.frame = r }

// Frame 实现 Node。
func (b *Button) Frame() Rect { return b.frame }

// Paint 实现 Node。
func (b *Button) Paint(cv *Canvas) {
	th := cv.Th
	rad := th.RadS
	bg, fg, bd := th.Accent, th.AccentText, Color{}
	switch b.Style {
	case BtnGhost:
		bg, fg = th.Panel, th.Text2
		if b.hover {
			bg = th.Hover
		}
	case BtnDanger:
		bg, fg = th.Danger, RGB(255, 255, 255)
		if b.hover {
			bg = Hex("#d13b40")
		}
	case BtnSoft:
		bg, fg = th.AccentSoft, th.Accent
		if b.hover {
			bg = Hex("#e2e5fd")
		}
	default:
		if b.hover {
			bg = Hex("#5457e5")
		}
	}
	_ = bd
	if b.Disable {
		bg, fg = th.Panel2, th.Muted
	}
	cv.RoundRect(b.frame, rad, bg)
	if b.Style == BtnGhost && b.hover {
		cv.StrokeRoundRectOutline(b.frame, rad, 1, th.Border)
	}
	// 内容排布
	mid := b.frame
	textStyle := TextStyle{Size: 13, Bold: true, Color: fg}
	tw := 0.0
	if b.Text != "" {
		tw = cv.MeasureText(b.Text, textStyle).W
	}
	ic := 0.0
	if b.HasIcon {
		ic = 15
	}
	total := tw + ic
	if b.HasIcon && b.Text != "" {
		total += 6
	}
	x := mid.X + (mid.W-total)/2
	if b.HasIcon {
		sy := mid.Y + (mid.H-15)/2
		cv.DrawIcon(b.Icon, x, sy, 15, fg)
		x += 15 + 6
	}
	if b.Text != "" {
		cv.TextAt(b.Text, x, mid.Y+(mid.H-cv.LineHeight(textStyle))/2, textStyle)
	}
}

// TextField 文本输入（单行，光标，密码模式）。
type TextField struct {
	Placeholder string
	Password    bool
	Value_      string
	OnChange    func(string)
	OnSubmit    func()
	frame       Rect
	focused     bool
	caret       int
	hover       bool
}

// Input 输入框快捷构造。
func Input(placeholder string) *TextField {
	return &TextField{Placeholder: placeholder}
}

// Value 实现 Typable。
func (t *TextField) Value() string { return t.Value_ }

// Focused 实现 Typable。
func (t *TextField) Focused(on bool) {
	t.focused = on
	if on {
		t.caret = len([]rune(t.Value_))
	}
}

// Type 实现 Typable。
func (t *TextField) Type(s string) {
	rs := []rune(t.Value_)
	c := t.caret
	t.Value_ = string(rs[:c]) + s + string(rs[c:])
	t.caret += len([]rune(s))
	if t.OnChange != nil {
		t.OnChange(t.Value_)
	}
}

// Backspace 实现 Typable。
func (t *TextField) Backspace() {
	rs := []rune(t.Value_)
	if t.caret == 0 {
		return
	}
	t.Value_ = string(rs[:t.caret-1]) + string(rs[t.caret:])
	t.caret--
	if t.OnChange != nil {
		t.OnChange(t.Value_)
	}
}

// MoveCaret 实现 Typable。
func (t *TextField) MoveCaret(delta int) {
	t.caret += delta
	if t.caret < 0 {
		t.caret = 0
	}
	if max := len([]rune(t.Value_)); t.caret > max {
		t.caret = max
	}
}

// Submit 实现 Typable。
func (t *TextField) Submit() bool {
	if t.OnSubmit != nil {
		t.OnSubmit()
		return true
	}
	return false
}

// Tap 实现 Tapper（点击聚焦由 shell 处理）。
func (t *TextField) Tap(x, y float64) bool { return t.frame.In(x, y) }

// Hover 实现 Hoverer。
func (t *TextField) Hover(on bool) { t.hover = on }

// Measure 实现 Node。
func (t *TextField) Measure(_ *Ctx, max Size) Size { return Size{minF(max.W, 320), 38} }

// SetFrame 实现 Node。
func (t *TextField) SetFrame(r Rect) { t.frame = r }

// Frame 实现 Node。
func (t *TextField) Frame() Rect { return t.frame }

// Paint 实现 Node。
func (t *TextField) Paint(cv *Canvas) {
	th := cv.Th
	bg := th.Panel2
	if t.focused {
		bg = th.Panel
	}
	cv.RoundRect(t.frame, th.RadS, bg)
	if t.focused {
		cv.StrokeRoundRectOutline(t.frame, th.RadS, 1.6, th.Accent)
	} else {
		cv.StrokeRoundRectOutline(t.frame, th.RadS, 1, th.Border)
	}
	st := TextStyle{Size: 13.5, Color: th.Text}
	show := t.Value_
	if t.Password && show != "" {
		show = strings.Repeat("•", len([]rune(show)))
	}
	x := t.frame.X + 12
	y := t.frame.Y + (t.frame.H-cv.LineHeight(st))/2
	if show == "" && !t.focused {
		cv.TextAt(t.Placeholder, x, y, TextStyle{Size: 13.5, Color: th.Muted})
		return
	}
	cv.TextAt(show, x, y, st)
	if t.focused {
		pre := string([]rune(show)[:minI(t.caret, len([]rune(show)))])
		cx := x + cv.MeasureText(pre, st).W
		cv.Fill(Rect{cx, t.frame.Y + 9, 1.5, t.frame.H - 18}, th.Accent)
	}
}

// Checkbox 复选框。
type Checkbox struct {
	Text     string
	Checked  bool
	OnChange func(bool)
	frame    Rect
	hover    bool
}

// Tap 实现 Tapper。
func (c *Checkbox) Tap(x, y float64) bool {
	if !c.frame.In(x, y) {
		return false
	}
	c.Checked = !c.Checked
	if c.OnChange != nil {
		c.OnChange(c.Checked)
	}
	return true
}

// Hover 实现 Hoverer。
func (c *Checkbox) Hover(on bool) { c.hover = on }

// Measure 实现 Node。
func (c *Checkbox) Measure(ctx *Ctx, _ Size) Size {
	w := 20.0
	if c.Text != "" {
		w += 8 + ctx.FS.Measure(c.Text, TextStyle{Size: 13}).W
	}
	return Size{w, 22}
}

// SetFrame 实现 Node。
func (c *Checkbox) SetFrame(r Rect) { c.frame = r }

// Frame 实现 Node。
func (c *Checkbox) Frame() Rect { return c.frame }

// Paint 实现 Node。
func (c *Checkbox) Paint(cv *Canvas) {
	th := cv.Th
	box := Rect{c.frame.X, c.frame.Y + (c.frame.H-18)/2, 18, 18}
	if c.Checked {
		cv.RoundRect(box, 5, th.Accent)
		cv.DrawIcon(IconCheck, box.X+2.5, box.Y+2.5, 13, RGB(255, 255, 255))
	} else {
		cv.RoundRect(box, 5, th.Panel)
		cv.StrokeRoundRectOutline(box, 5, 1.3, th.Border)
	}
	if c.Text != "" {
		cv.TextAt(c.Text, box.X+26, c.frame.Y+(c.frame.H-cv.LineHeight(TextStyle{Size: 13}))/2, TextStyle{Size: 13, Color: th.Text})
	}
}

// Badge 状态徽章。
type Badge struct {
	Text  string
	Soft  Color // 底色
	Fore  Color // 字色
	frame Rect
}

// NewBadge 徽章快捷构造。
func NewBadge(text string, soft, fore Color) *Badge {
	return &Badge{Text: text, Soft: soft, Fore: fore}
}

// Measure 实现 Node。
func (b *Badge) Measure(ctx *Ctx, _ Size) Size {
	return Size{ctx.FS.Measure(b.Text, TextStyle{Size: 11, Bold: true}).W + 16, 20}
}

// SetFrame 实现 Node。
func (b *Badge) SetFrame(r Rect) { b.frame = r }

// Frame 实现 Node。
func (b *Badge) Frame() Rect { return b.frame }

// Paint 实现 Node。
func (b *Badge) Paint(cv *Canvas) {
	cv.RoundRect(b.frame, 10, b.Soft)
	cv.TextAt(b.Text, b.frame.X+8, b.frame.Y+(b.frame.H-cv.LineHeight(TextStyle{Size: 11}))/2, TextStyle{Size: 11, Bold: true, Color: b.Fore})
}

// Spinner 旋转加载（动画由 shell 驱动重绘）。
type Spinner struct {
	Size_  float64
	Color_ Color
	frame  Rect
}

// Measure 实现 Node。
func (s *Spinner) Measure(_ *Ctx, _ Size) Size { return Size{s.Size_, s.Size_} }

// SetFrame 实现 Node。
func (s *Spinner) SetFrame(r Rect) { s.frame = r }

// Frame 实现 Node。
func (s *Spinner) Frame() Rect { return s.frame }

// Paint 实现 Node。
func (s *Spinner) Paint(cv *Canvas) {
	col := s.Color_
	if col.A == 0 {
		col = cv.Th.Accent
	}
	r := minF(s.frame.W, s.frame.H) / 2
	t := float64(time.Now().UnixNano()%int64(time.Second)) / float64(time.Second)
	start := t * 2 * 3.14159
	cv.StrokeCircle(s.frame.X+s.frame.W/2, s.frame.Y+s.frame.H/2, r-1.5, 2.5, col.Mul(0.18))
	cv.Arc(s.frame.X+s.frame.W/2, s.frame.Y+s.frame.H/2, r-1.5, 2.5, start, start+1.9, col)
}

// Progress 水平进度条。
type Progress struct {
	Ratio  float64
	Color_ Color
	frame  Rect
}

// Measure 实现 Node。
func (p *Progress) Measure(_ *Ctx, _ Size) Size { return Size{120, 6} }

// SetFrame 实现 Node。
func (p *Progress) SetFrame(r Rect) { p.frame = r }

// Frame 实现 Node。
func (p *Progress) Frame() Rect { return p.frame }

// Paint 实现 Node。
func (p *Progress) Paint(cv *Canvas) {
	cv.RoundRect(p.frame, 3, cv.Th.Panel2)
	w := p.frame.W * clampF(p.Ratio, 0, 1)
	if w > 0 {
		cv.RoundRect(Rect{p.frame.X, p.frame.Y, w, p.frame.H}, 3, p.Color_)
	}
}

// Avatar 头像圆（首字母/图标）。
type Avatar struct {
	Text    string
	Icon    Icon
	UseIcon bool
	BG      Color
	Fore    Color
	Size_   float64
	frame   Rect
}

// Measure 实现 Node。
func (a *Avatar) Measure(_ *Ctx, _ Size) Size { return Size{a.Size_, a.Size_} }

// SetFrame 实现 Node。
func (a *Avatar) SetFrame(r Rect) { a.frame = r }

// Frame 实现 Node。
func (a *Avatar) Frame() Rect { return a.frame }

// Paint 实现 Node。
func (a *Avatar) Paint(cv *Canvas) {
	bg := a.BG
	if bg.A == 0 {
		bg = cv.Th.AccentSoft
	}
	cv.Circle(a.frame.X+a.frame.W/2, a.frame.Y+a.frame.H/2, a.frame.W/2, bg)
	if a.UseIcon {
		s := a.frame.W * 0.5
		cv.DrawIcon(a.Icon, a.frame.X+(a.frame.W-s)/2, a.frame.Y+(a.frame.H-s)/2, s, a.Fore)
		return
	}
	if a.Text != "" {
		st := TextStyle{Size: a.frame.W * 0.38, Bold: true, Color: a.Fore}
		cv.TextCentered(a.Text, a.frame, st)
	}
}

// SegmentTabs 分段标签页（节点页小分栏用）。
type SegmentTabs struct {
	Items    []string
	Active   int
	Badges   []int // 可选角标数
	OnChange func(int)
	frame    Rect
	frames   []Rect
	widths   []float64
	hover    int
}

// Measure 实现 Node。
func (s *SegmentTabs) Measure(ctx *Ctx, max Size) Size {
	s.widths = make([]float64, len(s.Items))
	total := 0.0
	for i, it := range s.Items {
		label := it
		if i < len(s.Badges) && s.Badges[i] > 0 {
			label = it + " (" + itoa(s.Badges[i]) + ")"
		}
		s.widths[i] = ctx.FS.Measure(label, TextStyle{Size: 12.5, Bold: true}).W + 30
		total += s.widths[i]
	}
	return Size{minF(total, max.W), 32}
}

// SetFrame 实现 Node。
func (s *SegmentTabs) SetFrame(r Rect) {
	s.frame = r
	s.frames = make([]Rect, len(s.Items))
	total := 0.0
	for _, w := range s.widths {
		total += w
	}
	if total <= 0 {
		total = 1
	}
	x := r.X
	for i := range s.Items {
		w := s.widths[i] / total * r.W // 按文本占比分配（与 Measure 一致）
		s.frames[i] = Rect{x, r.Y, w, r.H}
		x += w
	}
}

// Frame 实现 Node。
func (s *SegmentTabs) Frame() Rect { return s.frame }

// Paint 实现 Node。
func (s *SegmentTabs) Paint(cv *Canvas) {
	th := cv.Th
	cv.RoundRect(s.frame, th.RadS, th.Panel2)
	for i, it := range s.Items {
		r := s.frames[i]
		if i == s.Active {
			cv.RoundRect(r.Shrink(3), th.RadS-2, th.Panel)
			cv.Shadow(r.Shrink(3), th.RadS-2, 6, RGBA(30, 34, 60, 20))
		}
		st := TextStyle{Size: 12.5, Bold: i == s.Active, Color: th.Text}
		if i != s.Active {
			st.Color = th.Text2
		}
		label := it
		if i < len(s.Badges) && s.Badges[i] > 0 {
			label = it + " (" + itoa(s.Badges[i]) + ")"
		}
		cv.TextCentered(label, r, st)
	}
}

// Tap 实现 Tapper。
func (s *SegmentTabs) Tap(x, y float64) bool {
	for i, r := range s.frames {
		if r.In(x, y) {
			s.Active = i
			if s.OnChange != nil {
				s.OnChange(i)
			}
			return true
		}
	}
	return false
}

// Hover 实现 Hoverer。
func (s *SegmentTabs) Hover(on bool) {}

func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
