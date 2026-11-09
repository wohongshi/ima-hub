package ui

// Modal 居中模态卡（压入 Shell.Overlays 后生效；Esc 由 Shell 关闭，X 角标走 OnClose）。
type Modal struct {
	Title   string
	Body    Node // 内容区
	Footer  Node // 底部按钮行（可空）
	W       float64
	OnClose func()
	frame   Rect
	card    Rect
	bodyH   float64
	footH   float64
}

// NewModal 模态卡快捷构造（宽 520）。
func NewModal(title string, body, footer Node) *Modal {
	return &Modal{Title: title, Body: body, Footer: footer, W: 520}
}

func (m *Modal) cardWidth(maxW float64) float64 {
	w := m.W
	if w <= 0 || w > maxW-80 {
		w = maxF(320, maxW-80)
	}
	return w
}

// Measure 实现 Node（内容高缓存给 SetFrame 复用）。
func (m *Modal) Measure(ctx *Ctx, max Size) Size {
	w := m.cardWidth(max.W)
	inner := Size{w - 56, max.H}
	m.bodyH, m.footH = 0, 0
	if m.Body != nil {
		m.bodyH = m.Body.Measure(ctx, inner).H
	}
	if m.Footer != nil {
		m.footH = m.Footer.Measure(ctx, inner).H
	}
	h := 24 + ctx.FS.LineHeight(TextStyle{Size: 15, Bold: true}) + 16 + m.bodyH + m.footH + 40 + 24
	return Size{w, h}
}

// SetFrame 实现 Node。
func (m *Modal) SetFrame(r Rect) {
	m.frame = r
	w := m.cardWidth(r.W)
	h := 24 + 20 + 16 + m.bodyH + m.footH + 40 + 24
	card := Rect{(r.W - w) / 2, maxF(30, (r.H-h)/2), w, h}
	m.card = card
	y := card.Y + 24 + 20 + 16
	if m.Body != nil {
		m.Body.SetFrame(Rect{card.X + 28, y, card.W - 56, m.bodyH})
		y += m.bodyH
	}
	if m.Footer != nil {
		m.Footer.SetFrame(Rect{card.X + 28, y + 16, card.W - 56, m.footH + 24})
	}
}

// Frame 实现 Node。
func (m *Modal) Frame() Rect { return m.frame }

// Paint 实现 Node。
func (m *Modal) Paint(cv *Canvas) {
	th := cv.Th
	cv.Fill(m.frame, RGBA(24, 26, 40, 120)) // 遮罩
	cv.Shadow(m.card, th.RadL, 24, RGBA(20, 22, 45, 70))
	cv.RoundRect(m.card, th.RadL, th.Panel)
	cv.TextAt(m.Title, m.card.X+28, m.card.Y+22, TextStyle{Size: 15, Bold: true, Color: th.Text})
	cv.DrawIcon(IconX, m.card.X+m.card.W-42, m.card.Y+20, 16, th.Muted)
	if m.Body != nil {
		m.Body.Paint(cv)
	}
	if m.Footer != nil {
		cv.Fill(Rect{m.card.X, m.card.Y + m.card.H - 64, m.card.W, 1}, th.Border)
		m.Footer.Paint(cv)
	}
}

// Tap X 角标关闭。
func (m *Modal) Tap(x, y float64) bool {
	xr := Rect{m.card.X + m.card.W - 50, m.card.Y + 12, 32, 32}
	if xr.In(x, y) {
		if m.OnClose != nil {
			m.OnClose()
		}
		return true
	}
	return false
}

// Hover 实现 Hoverer。
func (m *Modal) Hover(bool) {}
