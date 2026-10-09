# xqwin.ps1 —— 真实窗口取证工具（PrintWindow + 真实鼠标点击），用于 v1.3.1 验证。
#
# 为什么必须用它，而不能用 GDI CopyFromScreen / Canvas().Capture()：
#   * 本软件窗口是 GLFW30 + OpenGL 硬件加速，普通 BitBlt 抓不到内容，只会拍到桌面；
#     必须用 PrintWindow(hwnd, hdc, PW_RENDERFULLCONTENT=2) 走 DWM 重定向表面。
#   * Canvas().Capture() 按 Fyne 理想状态成像，绕开真实 GLFW 窗口，会得到"假通过"。
#   * 本脚本所在进程默认是 DPI 不感知的，所有窗口坐标会被系统虚拟化（÷1.25），
#     因此脚本第一件事就是 SetThreadDpiAwarenessContext(PER_MONITOR_AWARE_V2)，
#     让 GetWindowRect / ClientToScreen / SetCursorPos 全部工作在同一套**物理像素**坐标系里。
#
# 用法：
#   pwsh -File xqwin.ps1 -Action info
#   pwsh -File xqwin.ps1 -Action shot  -Out shot.png
#   pwsh -File xqwin.ps1 -Action click -X 300 -Y 250          # 相对客户区左上角（物理像素）
#   pwsh -File xqwin.ps1 -Action drag  -X 700 -Y 400 -X2 500 -Y2 400
#   pwsh -File xqwin.ps1 -Action move  -X 300 -Y 250
param(
	[string]$Action = 'info',
	[string]$Out = 'shot.png',
	[int]$X = 0,
	[int]$Y = 0,
	[int]$X2 = 0,
	[int]$Y2 = 0,
	[string]$ProcName = '',
	[int]$Index = 0
)

Add-Type -AssemblyName System.Drawing

Add-Type @"
using System;
using System.Runtime.InteropServices;
using System.Text;

public class XQW {
    [DllImport("user32.dll")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr ctx);
    [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr ctx);
    [DllImport("user32.dll")] public static extern int GetSystemMetrics(int i);

    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT r);
    [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr hWnd, out RECT r);
    [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr hWnd, ref POINT p);
    [DllImport("user32.dll")] public static extern bool ScreenToClient(IntPtr hWnd, ref POINT p);
    [DllImport("user32.dll")] public static extern int GetWindowTextLength(IntPtr hWnd);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder s, int n);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr hWnd, StringBuilder s, int n);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")] public static extern bool GetCursorPos(out POINT p);
    [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint dx, uint dy, uint d, IntPtr e);
    [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr hWnd, IntPtr hdc, uint flags);
    [DllImport("user32.dll")] public static extern IntPtr GetWindowDC(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern int ReleaseDC(IntPtr hWnd, IntPtr hdc);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int cmd);
    [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern IntPtr GetParent(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr hWnd, IntPtr after, int x, int y, int cx, int cy, uint flags);
    [DllImport("user32.dll")] public static extern void keybd_event(byte vk, byte scan, uint flags, IntPtr extra);

    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }

    public const uint LEFTDOWN = 0x0002;
    public const uint LEFTUP   = 0x0004;
    public const uint MOVE     = 0x0001;

    public static string Text(IntPtr h) {
        int n = GetWindowTextLength(h);
        StringBuilder sb = new StringBuilder(n + 2);
        GetWindowText(h, sb, sb.Capacity);
        return sb.ToString();
    }
    public static string Cls(IntPtr h) {
        StringBuilder sb = new StringBuilder(256);
        GetClassName(h, sb, sb.Capacity);
        return sb.ToString();
    }
}
"@

# ---- 关键第一步：本线程切到 Per-Monitor v2 DPI 感知，坐标全部变成物理像素 ----
[void][XQW]::SetThreadDpiAwarenessContext([IntPtr](-4))

function Get-TargetWindow {
	# 统一用 EnumWindows 枚举屏幕上全部可见的 GLFW30 顶层窗口，按 -Index 选择。
	Add-Type @"
using System;using System.Runtime.InteropServices;using System.Text;using System.Collections.Generic;
public class XQE {
  [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc cb, IntPtr l);
  [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
  delegate bool EnumProc(IntPtr h, IntPtr l);
  public static List<IntPtr> Find(string cls) {
    List<IntPtr> r = new List<IntPtr>();
    EnumWindows((h,l) => { if (IsWindowVisible(h)) { StringBuilder s=new StringBuilder(256); GetClassName(h,s,256); if (s.ToString()==cls) r.Add(h);} return true; }, IntPtr.Zero);
    return r;
  }
  public static string Title(IntPtr h){ StringBuilder s=new StringBuilder(512); GetWindowText(h,s,512); return s.ToString(); }
}
"@ -ErrorAction SilentlyContinue
	$cands = @()
	foreach ($h in [XQE]::Find('GLFW30')) {
		$cands += [pscustomobject]@{ Hwnd = $h; Proc = 'xiangqi'; Title = [XQE]::Title($h); Cls = 'GLFW30' }
	}
	if ($ProcName -ne '') {
		$cands = @($cands | Where-Object { (Get-Process -Id (Get-Process -Name $ProcName -ErrorAction SilentlyContinue | Select-Object -First 1).Id -ErrorAction SilentlyContinue) -ne $null })
	}
	if ($cands.Count -eq 0) { return $null }
	return $cands[[Math]::Min($Index, $cands.Count - 1)]
}

function List-Windows {
	Add-Type @"
using System;using System.Runtime.InteropServices;using System.Text;using System.Collections.Generic;
public class XQL {
  [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc cb, IntPtr l);
  [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
  delegate bool EnumProc(IntPtr h, IntPtr l);
  public static List<string> All() {
    List<string> r = new List<string>();
    EnumWindows((h,l) => { if (IsWindowVisible(h)) { StringBuilder c=new StringBuilder(256); GetClassName(h,c,256); if (c.ToString()=="GLFW30") { StringBuilder s=new StringBuilder(512); GetWindowText(h,s,512); r.Add(h.ToInt64()+"|"+s.ToString()); } } return true; }, IntPtr.Zero);
    return r;
  }
}
"@ -ErrorAction SilentlyContinue
	[XQL]::All()
}

function Get-Geom($hwnd) {
	$wr = New-Object XQW+RECT; [void][XQW]::GetWindowRect($hwnd, [ref]$wr)
	$cr = New-Object XQW+RECT; [void][XQW]::GetClientRect($hwnd, [ref]$cr)
	$pt = New-Object XQW+POINT; $pt.X = 0; $pt.Y = 0
	[void][XQW]::ClientToScreen($hwnd, [ref]$pt)
	return [pscustomobject]@{
		WinX = $wr.Left; WinY = $wr.Top
		WinW = $wr.Right - $wr.Left; WinH = $wr.Bottom - $wr.Top
		CliX = $pt.X; CliY = $pt.Y
		CliW = $cr.Right - $cr.Left; CliH = $cr.Bottom - $cr.Top
		Dpi  = [XQW]::GetDpiForWindow($hwnd)
	}
}

$t = Get-TargetWindow
if (-not $t) { Write-Output 'ERROR: no GLFW30 window found'; exit 1 }
$hwnd = $t.Hwnd

if ($Action -eq 'list') {
	Write-Output 'visible GLFW30 windows (hwnd|title):'
	List-Windows | ForEach-Object { Write-Output "  $_" }
	exit 0
}

if ($Action -eq 'info' -or $Action -eq 'shot' -or $Action -eq 'click' -or $Action -eq 'drag' -or $Action -eq 'move') {
	$g = Get-Geom $hwnd
	Write-Output ("window title : {0}" -f $t.Title)
	Write-Output ("class        : {0}" -f $t.Cls)
	Write-Output ("hwnd         : {0}" -f $hwnd)
	Write-Output ("dpi/scale    : {0} / {1}" -f $g.Dpi, [math]::Round($g.Dpi / 96.0, 4))
	Write-Output ("window rect  : x={0} y={1} w={2} h={3}   (physical)" -f $g.WinX, $g.WinY, $g.WinW, $g.WinH)
	Write-Output ("client rect  : x={0} y={1} w={2} h={3}   (physical, screen origin)" -f $g.CliX, $g.CliY, $g.CliW, $g.CliH)
	Write-Output ("titlebar h   : {0}" -f ($g.CliY - $g.WinY))
}

switch ($Action) {
	'info' { }
	'resize' {
		# 用真实 Win32 改变窗口大小（物理像素），模拟用户手动拉窗口
		[void][XQW]::SetWindowPos($hwnd, [IntPtr]::Zero, $X, $Y, $X2, $Y2, 0x0004)
		Start-Sleep -Milliseconds 1400
		$g2 = Get-Geom $hwnd
		Write-Output ("resized      : window rect x={0} y={1} w={2} h={3}" -f $g2.WinX, $g2.WinY, $g2.WinW, $g2.WinH)
		Write-Output ("client       : x={0} y={1} w={2} h={3}" -f $g2.CliX, $g2.CliY, $g2.CliW, $g2.CliH)
	}
	'shot' {
		$bmp = New-Object System.Drawing.Bitmap $g.WinW, $g.WinH
		$gfx = [System.Drawing.Graphics]::FromImage($bmp)
		$hdc = $gfx.GetHdc()
		# PW_RENDERFULLCONTENT = 2 —— 抓 GLFW/OpenGL 窗口内容的唯一可行方式
		$ok = [XQW]::PrintWindow($hwnd, $hdc, 2)
		$gfx.ReleaseHdc($hdc)
		$gfx.Dispose()
		$bmp.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)
		$bmp.Dispose()
		Write-Output ("shot         : {0} ok={1}" -f $Out, $ok)
	}
	'wheel' {
		# $X,$Y = 客户区坐标（滚轮位置），$Y2 = 滚动量（正数向上 / 负数向下）
		$sx = $g.CliX + $X; $sy = $g.CliY + $Y
		[void][XQW]::ShowWindow($hwnd, 9)
		for ($i = 0; $i -lt 6; $i++) {
			[void][XQW]::SetForegroundWindow($hwnd); Start-Sleep -Milliseconds 220
			if ([XQW]::GetForegroundWindow() -eq $hwnd) { break }
			[XQW]::keybd_event(0x12, 0, 0, [IntPtr]::Zero); [XQW]::keybd_event(0x12, 0, 2, [IntPtr]::Zero)
		}
		[void][XQW]::SetCursorPos($sx, $sy); Start-Sleep -Milliseconds 300
		$ticks = [int]($Y2 / 120)
		for ($k = 0; $k -lt [Math]::Abs($ticks); $k++) {
			# ticks < 0（Y2 为负）= 向下滚 = wheel delta -120
			$delta = if ($ticks -lt 0) { -120 } else { 120 }
			$u = [uint32]([int64]$delta -band 0xFFFFFFFF)
			[XQW]::mouse_event(0x0800, 0, 0, $u, [IntPtr]::Zero)
			Start-Sleep -Milliseconds 90
		}
		Start-Sleep -Milliseconds 500
		Write-Output ("wheel        : at client=({0},{1}) ticks={2}" -f $X, $Y, $ticks)
	}
	'move' {
		$sx = $g.CliX + $X; $sy = $g.CliY + $Y
		[void][XQW]::SetForegroundWindow($hwnd); Start-Sleep -Milliseconds 250
		[void][XQW]::SetCursorPos($sx, $sy); Start-Sleep -Milliseconds 350
		$cp = New-Object XQW+POINT; [void][XQW]::GetCursorPos([ref]$cp)
		Write-Output ("move         : asked screen=({0},{1}) actual=({2},{3})" -f $sx, $sy, $cp.X, $cp.Y)
	}
	'click' {
		$sx = $g.CliX + $X; $sy = $g.CliY + $Y
		# 先确保窗口可见并提到前台（Windows 会限制非前台进程抢焦点，故重试几次）
		[void][XQW]::ShowWindow($hwnd, 9)  # SW_RESTORE
		for ($i = 0; $i -lt 6; $i++) {
			[void][XQW]::SetForegroundWindow($hwnd)
			Start-Sleep -Milliseconds 220
			if ([XQW]::GetForegroundWindow() -eq $hwnd) { break }
			# 用一次 Alt 键轻敲解除前台锁定，再试
			[XQW]::keybd_event(0x12, 0, 0, [IntPtr]::Zero)
			[XQW]::keybd_event(0x12, 0, 2, [IntPtr]::Zero)
		}
		$fg = [XQW]::GetForegroundWindow()
		Start-Sleep -Milliseconds 200
		[void][XQW]::SetCursorPos($sx, $sy); Start-Sleep -Milliseconds 400
		$cp = New-Object XQW+POINT; [void][XQW]::GetCursorPos([ref]$cp)
		[void][XQW]::SetCursorPos($sx, $sy); Start-Sleep -Milliseconds 200
		[XQW]::mouse_event([XQW]::MOVE, 0, 0, 0, [IntPtr]::Zero)
		Start-Sleep -Milliseconds 150
		[XQW]::mouse_event([XQW]::LEFTDOWN, 0, 0, 0, [IntPtr]::Zero)
		Start-Sleep -Milliseconds 110
		[XQW]::mouse_event([XQW]::LEFTUP, 0, 0, 0, [IntPtr]::Zero)
		Start-Sleep -Milliseconds 450
		Write-Output ("click        : client=({0},{1}) screen=({2},{3}) cursor=({4},{5}) foreground={6} target={7} fgOK={8}" -f `
			$X, $Y, $sx, $sy, $cp.X, $cp.Y, $fg, $hwnd, ($fg -eq $hwnd))
	}
	'drag' {
		$sx = $g.CliX + $X; $sy = $g.CliY + $Y
		$ex = $g.CliX + $X2; $ey = $g.CliY + $Y2
		[void][XQW]::SetForegroundWindow($hwnd); Start-Sleep -Milliseconds 300
		[void][XQW]::SetCursorPos($sx, $sy); Start-Sleep -Milliseconds 300
		[XQW]::mouse_event([XQW]::LEFTDOWN, 0, 0, 0, [IntPtr]::Zero)
		Start-Sleep -Milliseconds 200
		$steps = 16
		for ($i = 1; $i -le $steps; $i++) {
			$ix = [int]($sx + ($ex - $sx) * $i / $steps)
			$iy = [int]($sy + ($ey - $sy) * $i / $steps)
			[void][XQW]::SetCursorPos($ix, $iy)
			Start-Sleep -Milliseconds 45
		}
		Start-Sleep -Milliseconds 200
		[XQW]::mouse_event([XQW]::LEFTUP, 0, 0, 0, [IntPtr]::Zero)
		Start-Sleep -Milliseconds 500
		Write-Output ("drag         : from client=({0},{1}) to client=({2},{3})" -f $X, $Y, $X2, $Y2)
	}
}
