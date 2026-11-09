package ui

import (
	"image"
	"sort"
	"time"
)

// Key 特殊按键（文本输入走 Type）。
type Key int

// 特殊键集合。
const (
	KeyBackspace Key = iota
	KeyEnter
	KeyTab
	KeyEsc
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyDelete
	KeyHome
	KeyEnd
)

// Shell 应用外壳：一帧渲染 + 输入分发 + 焦点 + 浮层/吐司 + 动画钩子。
type Shell struct {
	Cv   *Canvas
	FS   *FontSet
	Th   *Theme
	Root Node

	// OnKey 特殊键全局钩子（返回 true 表示已处理）。
	OnKey func(k Key) bool
	// Anim 每帧动画钩子（返回 true 需要持续重绘）。
	Anim func() bool

	overlays []Node
	toasts   []toastItem
	focus    Typable
	hovered  Hoverer
	pressed  Node // 实现 Tapper 的命中节点（按下-抬起同一控件才触发）
	pressPt  Pt
	Cursor   int // 0 箭头 1 手 2 文本（平台层换光标）
	Dirty    bool
}

type toastItem struct {
	Text string
	Kind int // 0 ok 1 warn 2 error
	At   time.Time
}

// NewShell 创建外壳（逻辑尺寸）。
func NewShell(w, h int, fs *FontSet, th *Theme) *Shell {
	cv := NewCanvas(w, h)
	cv.FS, cv.Th = fs, th
	return &Shell{Cv: cv, FS: fs, Th: th, Dirty: true}
}

// Resize 窗口尺寸变化。
func (s *Shell) Resize(w, h int) {
	s.Cv = NewCanvas(w, h)
	s.Cv.FS, s.Cv.Th = s.FS, s.Th
	s.Dirty = true
}

// Size 逻辑尺寸。
func (s *Shell) Size() Size { return s.Cv.Size() }

// PushOverlay 压入浮层（模态；后入在最上）。
func (s *Shell) PushOverlay(n Node) {
	s.overlays = append(s.overlays, n)
	s.Dirty = true
}

// PopOverlay 弹出最上浮层。
func (s *Shell) PopOverlay() {
	if len(s.overlays) > 0 {
		s.overlays = s.overlays[:len(s.overlays)-1]
		s.Dirty = true
	}
}

// Overlays 浮层数量。
func (s *Shell) Overlays() int { return len(s.overlays) }

// Toast 顶部提示（2.8s 自动消失）。
func (s *Shell) Toast(text string, kind int) {
	s.toasts = append(s.toasts, toastItem{Text: text, Kind: kind, At: time.Now()})
	s.Dirty = true
}

// SetFocus 设置文本焦点。
func (s *Shell) SetFocus(t Typable) {
	if s.focus == t {
		return
	}
	if s.focus != nil {
		s.focus.Focused(false)
	}
	s.focus = t
	if t != nil {
		t.Focused(true)
	}
	s.Dirty = true
}

// Render 布局 + 绘制一帧（降采样输出）。
func (s *Shell) Render() *image.RGBA {
	ctx := &Ctx{FS: s.FS, Theme: s.Th}
	size := s.Cv.Size()
	s.Cv.Clear(s.Th.BG)
	if s.Root != nil {
		s.Root.Measure(ctx, size)
		s.Root.SetFrame(Rect{0, 0, size.W, size.H})
		s.Root.Paint(s.Cv)
	}
	for _, o := range s.overlays {
		o.Measure(ctx, size)
		o.SetFrame(Rect{0, 0, size.W, size.H})
		o.Paint(s.Cv)
	}
	s.paintToasts()
	if s.Anim != nil && s.Anim() {
		s.Dirty = true
	}
	// 吐司过期清理
	now := time.Now()
	kept := s.toasts[:0]
	for _, t := range s.toasts {
		if now.Sub(t.At) < 2800*time.Millisecond {
			kept = append(kept, t)
		}
	}
	if len(kept) != len(s.toasts) {
		s.toasts = kept
		s.Dirty = true
	} else {
		s.toasts = kept
	}
	s.Dirty = false
	return s.Cv.Downsample()
}

func (s *Shell) paintToasts() {
	th := s.Th
	for i, t := range s.toasts {
		st := TextStyle{Size: 12.5, Bold: true, Color: RGB(255, 255, 255)}
		bg := th.Text
		switch t.Kind {
		case 1:
			bg = th.Warn
		case 2:
			bg = th.Danger
		}
		w := s.Cv.MeasureText(t.Text, st).W + 36
		x := (s.Cv.Size().W - w) / 2
		y := 18 + float64(i)*46
		r := Rect{x, y, w, 36}
		s.Cv.Shadow(r, 18, 12, RGBA(30, 34, 60, 40))
		s.Cv.RoundRect(r, 18, bg)
		s.Cv.TextCentered(t.Text, r, st)
	}
}

// ---------- 输入分发 ----------

// collect 深度优先收集节点（父先于子 = 绘制序）。
func collect(n Node, out *[]Node) {
	if n == nil {
		return
	}
	*out = append(*out, n)
	switch v := n.(type) {
	case *Box:
		collect(v.Child, out)
	case *Stack:
		for _, ch := range v.Children {
			collect(ch, out)
		}
	case *ScrollView:
		collect(v.Child, out)
	case *Stretch:
		collect(v.Child, out)
	case *Columns:
		for _, ch := range v.Children {
			collect(ch, out)
		}
	case *Modal:
		collect(v.Body, out)
		collect(v.Footer, out)
	}
}

// hit 浮层优先、后画优先，找命中点的最上交互节点。
func (s *Shell) hit(x, y float64, overlayOnly bool) Node {
	list := []Node{}
	if len(s.overlays) > 0 {
		collect(s.overlays[len(s.overlays)-1], &list)
	} else if !overlayOnly {
		collect(s.Root, &list)
	}
	sort.SliceStable(list, func(i, j int) bool { return i > j }) // 后画在上
	for _, n := range list {
		if _, ok := n.(Tapper); ok {
			if n.Frame().In(x, y) {
				return n
			}
		}
	}
	// 无 Tapper 命中也找滚动/悬停容器
	for _, n := range list {
		if _, ok := n.(Scroller); ok {
			if n.Frame().In(x, y) {
				return n
			}
		}
	}
	return nil
}

// MouseMove 鼠标移动。
func (s *Shell) MouseMove(x, y float64) {
	n := s.hit(x, y, false)
	hov, _ := n.(Hoverer)
	if hov != s.hovered {
		if s.hovered != nil {
			s.hovered.Hover(false)
		}
		if hov != nil {
			hov.Hover(true)
		}
		s.hovered = hov
		s.Dirty = true
	}
	// 光标形状
	s.Cursor = 0
	if n != nil {
		if _, ok := n.(Typable); ok {
			s.Cursor = 2
		} else if _, ok := n.(Tapper); ok {
			s.Cursor = 1
		}
	}
	if s.focus != nil && len(s.overlays) == 0 {
		if f, ok := s.focus.(Node); ok && f.Frame().In(x, y) {
			s.Cursor = 2
		}
	}
}

// MouseDown 鼠标按下。
func (s *Shell) MouseDown(x, y float64) {
	s.pressPt = Pt{x, y}
	n := s.hit(x, y, len(s.overlays) > 0)
	if _, ok := n.(Tapper); ok {
		s.pressed = n
	} else {
		s.pressed = nil
	}
	if t, ok := n.(Typable); ok {
		s.SetFocus(t)
	} else if len(s.overlays) == 0 {
		if _, ok := n.(Tapper); !ok {
			s.SetFocus(nil)
		}
	}
	s.Dirty = true
}

// MouseUp 鼠标抬起（按下与抬起命中同一控件才触发）。
func (s *Shell) MouseUp(x, y float64) {
	if s.pressed != nil {
		n := s.hit(x, y, len(s.overlays) > 0)
		if n == s.pressed {
			if t, ok := s.pressed.(Tapper); ok {
				t.Tap(x, y)
			}
		}
		s.pressed = nil
		s.Dirty = true
	}
}

// Wheel 滚轮。
func (s *Shell) Wheel(x, y, dy float64) {
	list := []Node{}
	if len(s.overlays) > 0 {
		collect(s.overlays[len(s.overlays)-1], &list)
	} else {
		collect(s.Root, &list)
	}
	sort.SliceStable(list, func(i, j int) bool { return i > j })
	for _, n := range list {
		if sc, ok := n.(Scroller); ok && n.Frame().In(x, y) {
			if sc.Wheel(x, y, dy) {
				s.Dirty = true
				return
			}
		}
	}
}

// Type 文本输入。
func (s *Shell) Type(str string) {
	if s.focus != nil {
		s.focus.Type(str)
		s.Dirty = true
	}
}

// KeyPress 特殊按键。
func (s *Shell) KeyPress(k Key) {
	if s.focus != nil {
		switch k {
		case KeyBackspace:
			s.focus.Backspace()
			s.Dirty = true
			return
		case KeyLeft:
			s.focus.MoveCaret(-1)
			s.Dirty = true
			return
		case KeyRight:
			s.focus.MoveCaret(1)
			s.Dirty = true
			return
		case KeyEnter:
			if s.focus.Submit() {
				s.Dirty = true
				return
			}
		}
	}
	if s.OnKey != nil && s.OnKey(k) {
		s.Dirty = true
		return
	}
	if k == KeyEsc && len(s.overlays) > 0 {
		s.PopOverlay()
		return
	}
	if k == KeyTab {
		s.cycleFocus()
	}
}

// cycleFocus Tab 焦点轮转（仅主树）。
func (s *Shell) cycleFocus() {
	list := []Node{}
	collect(s.Root, &list)
	var typables []Typable
	for _, n := range list {
		if t, ok := n.(Typable); ok {
			typables = append(typables, t)
		}
	}
	if len(typables) == 0 {
		return
	}
	idx := -1
	for i, t := range typables {
		if t == s.focus {
			idx = i
			break
		}
	}
	s.SetFocus(typables[(idx+1)%len(typables)])
}

// Focus 当前文本焦点（弹窗重建后恢复用）。
func (s *Shell) Focus() Typable { return s.focus }
