package ui

import (
	"testing"
	"time"
)

// TestPerf 细项计时（渲染慢时先跑这个找元凶）。
func TestPerf(t *testing.T) {
	reg, bold := FontPaths()
	fs := LoadFontSet(reg, bold)
	th := DefaultTheme()
	cv := NewCanvas(1280, 800)
	cv.FS, cv.Th = fs, th

	t0 := time.Now()
	for i := 0; i < 100; i++ {
		cv.RoundRect(Rect{10, 10, 300, 200}, 12, RGB(255, 0, 0))
	}
	t.Log("100 圆角填充:", time.Since(t0))

	t0 = time.Now()
	for i := 0; i < 100; i++ {
		cv.TextAt("测试文字abcdefg 123", 10, 10, TextStyle{Size: 13, Color: RGB(0, 0, 0)})
	}
	t.Log("100 文本:", time.Since(t0))

	t0 = time.Now()
	for i := 0; i < 100; i++ {
		cv.MeasureText("测试文字abcdefg 123", TextStyle{Size: 13})
	}
	t.Log("100 测量:", time.Since(t0))

	t0 = time.Now()
	for i := 0; i < 100; i++ {
		cv.Shadow(Rect{10, 10, 300, 200}, 12, 14, RGBA(0, 0, 0, 32))
	}
	t.Log("100 阴影(7层):", time.Since(t0))

	t0 = time.Now()
	cv.DrawIcon(IconHome, 500, 500, 24, RGB(0, 0, 0))
	t.Log("1 图标:", time.Since(t0))

	t0 = time.Now()
	cv.Downsample()
	t.Log("降采样:", time.Since(t0))
}
