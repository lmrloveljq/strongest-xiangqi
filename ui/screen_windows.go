//go:build windows

package ui

import (
	"strings"
	"syscall"
	"unsafe"
)

// 本文件封装「窗口必须在屏幕可见区域内」所需的 Win32 调用（R6）。
//
// 背景：Fyne v2 的 Window 接口只有 CenterOnScreen()，没有设置窗口位置的 API。
// 而 CenterOnScreen 是**按客户区尺寸**居中的，完全不计入标题栏（本机 38 物理像素）
// 与边框（左右各 9 物理像素）。于是居中后窗口框架会上移半个标题栏：
// 屏幕高 1080、客户区高 1016 时居中 y = 32，而窗口实际 top = 32 - 38 = -6，
// 标题栏有 6 像素顶出屏幕之外——这正是 R6 稳定复现的现象。
//
// 修法：拿到本进程的 GLFW 顶层窗口句柄，用 SetWindowPos 把整窗（含标题栏）
// 夹回屏幕可见范围，保证 top >= 0。

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procGetWindowThreadPrc  = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procGetClassNameW       = user32.NewProc("GetClassNameW")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procSystemParametersInf = user32.NewProc("SystemParametersInfoW")
)

const (
	smCXScreen = 0 // SM_CXSCREEN
	smCYScreen = 1 // SM_CYSCREEN

	spiGetWorkArea = 0x0030 // SPI_GETWORKAREA

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

// ScreenSize 返回主显示器物理像素宽高；失败时返回 (0, 0)。
//
// 注意：本进程是 DPI 感知的（GLFW 会显式设置 Per-Monitor v2），
// 因此这里拿到的是真实物理像素，而不是被系统虚拟化过的数值。
func ScreenSize() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	h, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))
	return int(w), int(h)
}

// WorkArea 返回去掉任务栏之后的主显示器可用区域（物理像素）。
// 失败时回退为整屏。
func WorkArea() (x, y, w, h int) {
	var r winRect
	ret, _, _ := procSystemParametersInf.Call(uintptr(spiGetWorkArea), 0,
		uintptr(unsafe.Pointer(&r)), 0)
	if ret == 0 {
		sw, sh := ScreenSize()
		return 0, 0, sw, sh
	}
	return int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// windowHandle 是 EnumWindows 回调里用的候选窗口。
type windowHandle struct {
	hwnd uintptr
}

// MainWindowHandle 返回本进程主窗口的句柄（找不到返回 0）。
//
// 用途：设置 Windows 深色标题栏（SetDarkTitleBar）等系统级外观。
// Fyne v2.8 不再对外暴露窗口句柄，所以只能自己按类名枚举。
func MainWindowHandle() uintptr {
	if ws := ownGlfwWindows(); len(ws) > 0 {
		return ws[0]
	}
	return 0
}

// ownGlfwWindows 枚举本进程所有可见的顶层 GLFW30 窗口句柄。
//
// 只认类名 GLFW30：Fyne 的 GLFW 驱动固定用这个类名创建窗口，
// 用它过滤可以避开对话框、控制台等其它窗口。
func ownGlfwWindows() []uintptr {
	var out []uintptr
	pid := uint32(syscall.Getpid())

	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadPrc.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1 // 继续枚举
		}
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		var buf [64]uint16
		n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return 1
		}
		name := syscall.UTF16ToString(buf[:])
		if strings.EqualFold(name, "GLFW30") {
			out = append(out, hwnd)
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return out
}

// EnforceWindowsOnScreen 把本进程所有 GLFW 顶层窗口夹进屏幕可用区域。
//
// 规则（对应 R6）：
//   - 窗口宽高超过可用区域时先等比缩小到可用区域；
//   - top 最小 0、left 最小 0（标题栏与左边框绝不允许顶出屏幕）；
//   - 右/下边超出时整体左移/上移。
//
// 返回被修正的窗口数量，便于 XQ_DEBUG 输出取证。
// SetAlwaysOnTop 把本软件的所有窗口设为置顶 / 取消置顶。
//
// 为什么用 Win32 而不是 Fyne 的 API：**Fyne v2.8 的 fyne.Window 根本没有 SetPinned**
// （查过源码，全库无此方法）。同类软件（TCHESS）的设置菜单里有「窗口置顶」，
// 这里用 SetWindowPos 的 HWND_TOPMOST / HWND_NOTOPMOST 达到同样效果。
func SetAlwaysOnTop(on bool) int {
	const (
		hwndTopmost    = ^uintptr(0) // (HWND)-1
		hwndNotTopmost = ^uintptr(1) // (HWND)-2
		swpNoSize      = 0x0001
		swpNoMove      = 0x0002
		swpNoActivate  = 0x0010
	)
	after := hwndNotTopmost
	if on {
		after = hwndTopmost
	}
	n := 0
	for _, hwnd := range ownGlfwWindows() {
		ret, _, _ := procSetWindowPos.Call(hwnd, after, 0, 0, 0, 0,
			uintptr(swpNoSize|swpNoMove|swpNoActivate))
		if ret != 0 {
			n++
		}
	}
	return n
}

func EnforceWindowsOnScreen() int {
	ax, ay, aw, ah := WorkArea()
	if aw <= 0 || ah <= 0 {
		return 0
	}
	fixed := 0
	for _, hwnd := range ownGlfwWindows() {
		var r winRect
		ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		if ret == 0 {
			continue
		}
		w := int(r.Right - r.Left)
		h := int(r.Bottom - r.Top)
		if w <= 0 || h <= 0 {
			continue
		}
		nw, nh := w, h
		if nw > aw {
			nw = aw
		}
		if nh > ah {
			nh = ah
		}
		x, y := int(r.Left), int(r.Top)
		if x+nw > ax+aw {
			x = ax + aw - nw
		}
		if y+nh > ay+ah {
			y = ay + ah - nh
		}
		if x < ax {
			x = ax
		}
		if y < ay {
			y = ay
		}
		if x == int(r.Left) && y == int(r.Top) && nw == w && nh == h {
			continue
		}
		procSetWindowPos.Call(hwnd, 0,
			uintptr(x), uintptr(y), uintptr(nw), uintptr(nh),
			uintptr(swpNoZOrder|swpNoActivate))
		fixed++
	}
	return fixed
}

// WindowRects 返回本进程所有 GLFW 窗口的物理矩形（x, y, w, h 依次排列），
// 供 XQ_DEBUG 打印取证。
func WindowRects() []int {
	var out []int
	for _, hwnd := range ownGlfwWindows() {
		var r winRect
		if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret != 0 {
			out = append(out, int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top))
		}
	}
	return out
}
