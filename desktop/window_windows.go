//go:build windows

package main

// Win32 平台层：原生窗口 + GDI 贴屏 + 输入（鼠标/键盘/滚轮/IME 文字/剪贴板）。
// 不依赖 WebView2/任何浏览器——画面由 internal/ui 自写引擎软渲染后 StretchDIBits 上屏。

import (
	"log"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"imahub/internal/ui"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW           = user32.NewProc("RegisterClassExW")
	pCreateWindowExW            = user32.NewProc("CreateWindowExW")
	pDefWindowProcW             = user32.NewProc("DefWindowProcW")
	pShowWindow                 = user32.NewProc("ShowWindow")
	pUpdateWindow               = user32.NewProc("UpdateWindow")
	pGetMessageW                = user32.NewProc("GetMessageW")
	pTranslateMessage           = user32.NewProc("TranslateMessage")
	pDispatchMessageW           = user32.NewProc("DispatchMessageW")
	pPostQuitMessage            = user32.NewProc("PostQuitMessage")
	pLoadCursorW                = user32.NewProc("LoadCursorW")
	pSetCursor                  = user32.NewProc("SetCursor")
	pInvalidateRect             = user32.NewProc("InvalidateRect")
	pValidateRect               = user32.NewProc("ValidateRect")
	pSetProcessDPIAware         = user32.NewProc("SetProcessDPIAware")
	pScreenToClient             = user32.NewProc("ScreenToClient")
	pFindWindowW                = user32.NewProc("FindWindowW")
	pSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	pOpenClipboard              = user32.NewProc("OpenClipboard")
	pCloseClipboard             = user32.NewProc("CloseClipboard")
	pGetClipboardData           = user32.NewProc("GetClipboardData")
	pSetClipboardData           = user32.NewProc("SetClipboardData")
	pEmptyClipboard             = user32.NewProc("EmptyClipboard")
	pIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	pGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	pGlobalLock                 = kernel32.NewProc("GlobalLock")
	pGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	pGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")

	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pStretchDIBits      = gdi32.NewProc("StretchDIBits")
	pDeleteDC           = gdi32.NewProc("DeleteDC")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
)

const (
	wsOverlappedwindow = 0x00CF0000
	wsVisible          = 0x10000000
	swShow             = 5
	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmPaint            = 0x000F
	wmSize             = 0x0005
	wmMouseMove        = 0x0200
	wmLButtonDown      = 0x0201
	wmLButtonUp        = 0x0202
	wmMouseWheel       = 0x020A
	wmChar             = 0x0102
	wmKeyDown          = 0x0100
	wmSetCursor        = 0x0020
	wmEraseBkgnd       = 0x0014
	idcArrow           = 32512
	idcIBeam           = 32513
	idcHand            = 32649
	csHRedraw          = 0x0002
	csVRedraw          = 0x0001
	biRgb              = 0
	dibRgbColors       = 0
	srccopy            = 0x00CC0020
	cfUnicodeText      = 13
	gmemMoveable       = 0x0002
	vkLeft             = 0x25
	vkRight            = 0x27
	vkDelete           = 0x2E
	vkHome             = 0x24
	vkEnd              = 0x23
	vkReturn           = 0x0D
	vkEscape           = 0x1B
	vkTab              = 0x09
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type winPoint struct{ X, Y int32 }

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// window Win32 窗口壳。
type window struct {
	hwnd uintptr
	sh   *ui.Shell
	app  *app
	bits []byte // DIB 像素（32bpp 自顶向下）
	bmp  uintptr
	dc   uintptr
	w, h int
}

var win *window

// runWindow 原生窗口主循环（自写引擎渲染帧 → DIB → StretchDIBits 上屏）。
func runWindow(eng *engine) {
	runtime.LockOSThread()
	_, _, _ = pSetProcessDPIAware.Call()

	// 单实例：已有窗口就前置提示
	cls, _ := syscall.UTF16PtrFromString("ImaHubWindow")
	if hwnd, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0); hwnd != 0 {
		pSetForegroundWindow.Call(hwnd)
		log.Println("ImaHub 已在运行")
		return
	}

	reg, bold := ui.FontPaths()
	sh := ui.NewShell(1280, 800, ui.LoadFontSet(reg, bold), ui.DefaultTheme())
	app := newApp(eng, sh)
	app.build()

	win = &window{sh: sh, app: app, w: 1280, h: 800}

	hInst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		hCursor:       cursor,
		lpszClassName: cls,
	}
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		log.Fatalf("注册窗口类失败: %v", err)
	}
	title, _ := syscall.UTF16PtrFromString("ImaHub")
	hwnd, _, err := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		wsOverlappedwindow|wsVisible,
		80, 60, 1280, 840, 0, 0, hInst, 0)
	if hwnd == 0 {
		log.Fatalf("创建窗口失败: %v", err)
	}
	win.hwnd = hwnd
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)

	// 脏帧驱动重绘（动画/吐司/异步数据到达）
	go func() {
		tk := time.NewTicker(100 * time.Millisecond)
		defer tk.Stop()
		for range tk.C {
			if sh.Dirty {
				pInvalidateRect.Call(hwnd, 0, 0)
			}
		}
	}()

	var m winMsg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	win.destroy()
	eng.stop()
}

// wndProc 窗口过程。
func wndProc(hwnd uintptr, msgNo uint32, wParam, lParam uintptr) uintptr {
	switch msgNo {
	case wmPaint:
		win.paint()
		return 0
	case wmEraseBkgnd:
		return 1
	case wmSize:
		w := int(uint32(lParam) & 0xFFFF)
		h := int(uint32(lParam) >> 16)
		if w > 0 && h > 0 && (w != win.w || h != win.h) {
			win.w, win.h = w, h
			win.createDIB()
			win.sh.Resize(w, h)
		}
		return 0
	case wmMouseMove:
		x := int(int16(uint32(lParam) & 0xFFFF))
		y := int(int16(uint32(lParam) >> 16))
		win.sh.MouseMove(float64(x), float64(y))
		win.syncCursor()
		return 0
	case wmLButtonDown:
		x := int(int16(uint32(lParam) & 0xFFFF))
		y := int(int16(uint32(lParam) >> 16))
		win.sh.MouseDown(float64(x), float64(y))
		return 0
	case wmLButtonUp:
		x := int(int16(uint32(lParam) & 0xFFFF))
		y := int(int16(uint32(lParam) >> 16))
		win.sh.MouseUp(float64(x), float64(y))
		return 0
	case wmMouseWheel:
		delta := int16(uint32(wParam) >> 16)
		var pt winPoint
		pt.X = int32(int16(uint32(lParam) & 0xFFFF))
		pt.Y = int32(int16(uint32(lParam) >> 16))
		pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		win.sh.Wheel(float64(pt.X), float64(pt.Y), float64(delta)/40)
		return 0
	case wmChar:
		switch wParam {
		case 8:
			win.sh.KeyPress(ui.KeyBackspace)
		case 9:
			win.sh.KeyPress(ui.KeyTab)
		case 13:
			win.sh.KeyPress(ui.KeyEnter)
		case 27:
			win.sh.KeyPress(ui.KeyEsc)
		default:
			if wParam >= 32 {
				win.sh.Type(string(rune(wParam))) // IME 合成结果经此进入（WM_CHAR 收 UTF-16）
			}
		}
		return 0
	case wmKeyDown:
		switch wParam {
		case vkLeft:
			win.sh.KeyPress(ui.KeyLeft)
		case vkRight:
			win.sh.KeyPress(ui.KeyRight)
		case vkDelete:
			win.sh.KeyPress(ui.KeyDelete)
		case vkHome:
			win.sh.KeyPress(ui.KeyHome)
		case vkEnd:
			win.sh.KeyPress(ui.KeyEnd)
		}
		return 0
	case wmSetCursor:
		win.syncCursor()
		return 1
	case wmClose:
		pPostQuitMessage.Call(0)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msgNo), wParam, lParam)
	return r
}

// syncCursor 按命中控件切换光标（箭头/手形/文本）。
func (w *window) syncCursor() {
	id := idcArrow
	switch w.sh.Cursor {
	case 1:
		id = idcHand
	case 2:
		id = idcIBeam
	}
	c, _, _ := pLoadCursorW.Call(0, uintptr(id))
	pSetCursor.Call(c)
}

// createDIB 建与客户区匹配的 DIB 段（32bpp，自顶向下）。
func (w *window) createDIB() {
	if w.bmp != 0 {
		pDeleteObject.Call(w.bmp)
		w.bmp = 0
	}
	if w.dc != 0 {
		pDeleteDC.Call(w.dc)
		w.dc = 0
	}
	var hdr bitmapInfoHeader
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	hdr.Width = int32(w.w)
	hdr.Height = -int32(w.h) // 负高 = 自顶向下
	hdr.Planes = 1
	hdr.BitCount = 32

	screen, _, _ := user32.NewProc("GetDC").Call(0)
	w.dc, _, _ = pCreateCompatibleDC.Call(screen)
	user32.NewProc("ReleaseDC").Call(0, screen)

	var bitsPtr uintptr
	bmp, _, _ := pCreateDIBSection.Call(w.dc, uintptr(unsafe.Pointer(&hdr)), dibRgbColors,
		uintptr(unsafe.Pointer(&bitsPtr)), 0, 0)
	w.bmp = bmp
	if bmp == 0 || bitsPtr == 0 {
		log.Fatal("创建 DIB 失败")
	}
	w.bits = unsafe.Slice((*byte)(unsafe.Pointer(bitsPtr)), w.w*w.h*4)
	pSelectObject.Call(w.dc, bmp)
}

// paint WM_PAINT：引擎渲染一帧 → RGBA 转 BGRA → 上屏。
func (w *window) paint() {
	if w.bmp == 0 {
		w.createDIB()
	}
	img := w.app.RenderLocked()
	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	rows := ih
	if rows > w.h {
		rows = w.h
	}
	cols := iw
	if cols > w.w {
		cols = w.w
	}
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			si := img.PixOffset(x, y)
			di := (y*w.w + x) * 4
			w.bits[di+0] = img.Pix[si+2]
			w.bits[di+1] = img.Pix[si+1]
			w.bits[di+2] = img.Pix[si+0]
			w.bits[di+3] = 255
		}
	}
	var hdr bitmapInfoHeader
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	hdr.Width = int32(w.w)
	hdr.Height = -int32(w.h)
	hdr.Planes = 1
	hdr.BitCount = 32
	pStretchDIBits.Call(w.dc, 0, 0, uintptr(w.w), uintptr(w.h),
		0, 0, uintptr(w.w), uintptr(w.h),
		uintptr(unsafe.Pointer(&w.bits[0])),
		uintptr(unsafe.Pointer(&hdr)), dibRgbColors, srccopy)
	pValidateRect.Call(w.hwnd, 0)
}

// destroy 释放 GDI 资源。
func (w *window) destroy() {
	if w.bmp != 0 {
		pDeleteObject.Call(w.bmp)
	}
	if w.dc != 0 {
		pDeleteDC.Call(w.dc)
	}
}

// clipboardText 读剪贴板（粘贴）。
func clipboardText() string {
	r, _, _ := pIsClipboardFormatAvailable.Call(cfUnicodeText)
	if r == 0 {
		return ""
	}
	if r, _, _ := pOpenClipboard.Call(0); r == 0 {
		return ""
	}
	defer pCloseClipboard.Call()
	h, _, _ := pGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer pGlobalUnlock.Call(h)
	u := unsafe.Slice((*uint16)(unsafe.Pointer(p)), 1<<20)
	n := 0
	for n < len(u) && u[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(u[:n])
}

// clipboardWrite 写剪贴板（复制）。
func clipboardWrite(s string) {
	if r, _, _ := pOpenClipboard.Call(0); r == 0 {
		return
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	u := syscall.StringToUTF16(s)
	h, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u))
	copy(dst, u)
	pGlobalUnlock.Call(h)
	pSetClipboardData.Call(cfUnicodeText, h)
}
