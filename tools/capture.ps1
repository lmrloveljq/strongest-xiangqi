Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

Add-Type @"
using System;
using System.Runtime.InteropServices;
public class WinCap {
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT lpRect);
    [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr hWnd, out RECT lpRect);
    [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr hWnd, ref POINT p);
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
}
"@

$shot     = $args[0]
$procName = if ($args[1]) { $args[1] } else { 'xiangqi' }
$mode     = if ($args[2]) { $args[2] } else { 'client' }   # client | screen

$p = Get-Process -Name $procName -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $p) { Write-Output "process $procName not running"; exit 1 }

# 只把窗口提到前台，**不最大化**（最大化会改变窗口尺寸，干扰布局验证）
if ($p.MainWindowHandle -ne 0) {
    [WinCap]::SetForegroundWindow($p.MainWindowHandle) | Out-Null
    Start-Sleep -Milliseconds 900
}

if ($mode -eq 'client' -and $p.MainWindowHandle -ne 0) {
    $c = New-Object WinCap+RECT
    [WinCap]::GetClientRect($p.MainWindowHandle, [ref]$c) | Out-Null
    $pt = New-Object WinCap+POINT
    $pt.X = 0; $pt.Y = 0
    [WinCap]::ClientToScreen($p.MainWindowHandle, [ref]$pt) | Out-Null
    $w = $c.Right - $c.Left
    $h = $c.Bottom - $c.Top
    $x = $pt.X
    $y = $pt.Y
    Write-Output "client rect: $x,$y ${w}x${h}"
} else {
    $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
    $x = $b.X; $y = $b.Y; $w = $b.Width; $h = $b.Height
    Write-Output "screen rect: $x,$y ${w}x${h}"
}

$bmp = New-Object System.Drawing.Bitmap $w, $h
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen((New-Object System.Drawing.Point $x, $y), [System.Drawing.Point]::Empty, (New-Object System.Drawing.Size $w, $h))
$bmp.Save($shot, [System.Drawing.Imaging.ImageFormat]::Png)
$g.Dispose(); $bmp.Dispose()
Write-Output "saved $shot (${w}x${h})"
