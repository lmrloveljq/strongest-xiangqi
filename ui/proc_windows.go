package ui

import (
	"syscall"
	"unsafe"
)

// 本文件提供引擎进程层面的三个能力（Windows）：
//   - 进程优先级：满配时把引擎提到「高于正常」，减少被界面/系统抢占；
//   - CPU 亲和性：把引擎绑到指定核心上，抖动更小；
//   - CPU 占用实测：用 GetProcessTimes 差分算引擎真正吃了多少核（比任务管理器直观）。
//
// 为什么放 UI 层而不是 engine 包：这些是「跑分/满配」这类界面功能的辅助，
// engine 包只管 UCI 通信，不该依赖 Win32。

var (
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess            = kernel32.NewProc("OpenProcess")
	procCloseHandle            = kernel32.NewProc("CloseHandle")
	procSetPriorityClass       = kernel32.NewProc("SetPriorityClass")
	procSetProcessAffinityMask = kernel32.NewProc("SetProcessAffinityMask")
	procGetProcessTimes        = kernel32.NewProc("GetProcessTimes")

	dwmapi                    = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

// ---------------------------------------------------------------------------
// 深色标题栏：深色界面配一条系统白标题栏，是最容易"露馅"的地方
// ---------------------------------------------------------------------------

// SetDarkTitleBar 让 Windows 的标题栏跟随深色界面（Win10 1809+ 支持）。
//
// 属性号有两个历史版本：20 是 20H1 之后的，19 是 1809~1909 的；
// 先试 20，失败再试 19。老系统（或非 Windows）上两个都会失败 —— 那就保持系统默认，
// 只影响观感、不影响功能，所以调用方不需要处理错误。
//
// 窗口句柄用 screen_windows.go 里已有的 ownGlfwWindows()（认类名 GLFW30）。
func SetDarkTitleBar(hwnd uintptr, dark bool) bool {
	if hwnd == 0 {
		return false
	}
	if err := procDwmSetWindowAttribute.Find(); err != nil {
		return false
	}
	var val uintptr
	if dark {
		val = 1
	}
	for _, attr := range []uintptr{20, 19} {
		ret, _, _ := procDwmSetWindowAttribute.Call(hwnd, attr,
			uintptr(unsafe.Pointer(&val)), unsafe.Sizeof(val))
		if ret == 0 {
			return true
		}
	}
	return false
}

const (
	processSetInformation   = 0x0200
	processQueryInformation = 0x0400
	// 【缺陷修复】原来用的是 HIGH_PRIORITY_CLASS（"高"），对外却说"高于正常"：
	// 名字不对，更要紧的是"高"在 15 个搜索线程满载时足以把界面线程饿死
	// （表现就是"象棋软件点不动"）。真正要的是 ABOVE_NORMAL（"高于正常"）：
	// 比界面高一级、拿得到 CPU，又不会把整个桌面压住。
	aboveNormalPriorityClass = 0x00008000
	normalPriorityClass      = 0x0020
)

type filetimeWin struct {
	Low  uint32
	High uint32
}

func ftSeconds(ft filetimeWin) float64 {
	v := uint64(ft.High)<<32 | uint64(ft.Low)
	return float64(v) / 1e7 // 100ns 单位
}

func openProc(pid int) uintptr {
	if pid <= 0 {
		return 0
	}
	h, _, _ := procOpenProcess.Call(
		uintptr(processSetInformation|processQueryInformation), 0, uintptr(pid))
	return h
}

// SetEnginePriority 把引擎进程设为「高于正常」/「正常」优先级。
func SetEnginePriority(pid int, high bool) bool {
	h := openProc(pid)
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)
	cls := uintptr(normalPriorityClass)
	if high {
		cls = aboveNormalPriorityClass
	}
	ret, _, _ := procSetPriorityClass.Call(h, cls)
	return ret != 0
}

// SetEngineAffinityMask 设置引擎进程可用的 CPU 逻辑核掩码（位 0 = 第 1 个逻辑核）。
func SetEngineAffinityMask(pid int, mask uintptr) bool {
	h := openProc(pid)
	if h == 0 || mask == 0 {
		return false
	}
	defer procCloseHandle.Call(h)
	ret, _, _ := procSetProcessAffinityMask.Call(h, mask)
	return ret != 0
}

// EngineCPUSeconds 返回引擎进程累计占用 CPU 的秒数（内核 + 用户）。
func EngineCPUSeconds(pid int) float64 {
	h := openProc(pid)
	if h == 0 {
		return 0
	}
	defer procCloseHandle.Call(h)
	var create, exit, kernel, user filetimeWin
	ret, _, _ := procGetProcessTimes.Call(h,
		uintptr(unsafe.Pointer(&create)), uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ret == 0 {
		return 0
	}
	return ftSeconds(kernel) + ftSeconds(user)
}

// MaxStrengthAffinityMask 造一个「把前面 reserve 个逻辑核留给界面」的掩码。
//
// 例：16 逻辑核、引擎 12 线程 → reserve = 4 → 0xFFF0
// （bit0..3 留给界面/系统，引擎只用 bit4..15）。
//
// 【为什么按实际线程数留核】原来是「只留第 1 核、其余 15 个全给引擎」，而引擎只开
// 12 个线程时这等于白绑：12 个线程在 15 个核之间乱跳，界面还是可能被挤。
// 现在留出的核数与引擎线程数互补，界面拿到的是**确定的** 4 个核。
func MaxStrengthAffinityMask(logical, engineThreads int) uintptr {
	if logical <= 2 {
		return 0
	}
	reserve := logical - engineThreads
	if reserve < 1 {
		reserve = 1 // 至少留 1 个核，绝不把整机占满
	}
	if reserve >= logical {
		return 0
	}
	mask := uintptr(0)
	for i := reserve; i < logical; i++ {
		mask |= 1 << uint(i)
	}
	return mask
}
