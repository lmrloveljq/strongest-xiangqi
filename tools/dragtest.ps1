Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

Add-Type @"
using System;
using System.Runtime.InteropServices;
public class MInput {
    [DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
    [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint dx, uint dy, uint d, IntPtr e);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr hWnd, out RECT r);
    [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr hWnd, ref POINT p);
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
    public const uint LEFTDOWN = 0x0002;
    public const uint LEFTUP   = 0x0004;
}
"@

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$procName = 'xiangqi'

function Get-ClientRect($p) {
    $c = New-Object MInput+RECT
    [MInput]::GetClientRect($p.MainWindowHandle, [ref]$c) | Out-Null
    $pt = New-Object MInput+POINT
    [MInput]::ClientToScreen($p.MainWindowHandle, [ref]$pt) | Out-Null
    return @{ X = $pt.X; Y = $pt.Y; W = ($c.Right - $c.Left); H = ($c.Bottom - $c.Top) }
}

function Capture($path, $p) {
    $r = Get-ClientRect $p
    $bmp = New-Object System.Drawing.Bitmap $r.W, $r.H
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen((New-Object System.Drawing.Point $r.X, $r.Y), [System.Drawing.Point]::Empty, (New-Object System.Drawing.Size $r.W, $r.H))
    $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
    Write-Output "captured $path ($($r.W)x$($r.H))"
}

function DragMouse($x1, $y1, $x2, $y2) {
    [MInput]::SetCursorPos($x1, $y1) | Out-Null
    Start-Sleep -Milliseconds 250
    [MInput]::mouse_event([MInput]::LEFTDOWN, 0, 0, 0, [IntPtr]::Zero)
    Start-Sleep -Milliseconds 200
    $steps = 12
    for ($i = 1; $i -le $steps; $i++) {
        $x = [int]($x1 + ($x2 - $x1) * $i / $steps)
        $y = [int]($y1 + ($y2 - $y1) * $i / $steps)
        [MInput]::SetCursorPos($x, $y) | Out-Null
        Start-Sleep -Milliseconds 40
    }
    Start-Sleep -Milliseconds 200
    [MInput]::mouse_event([MInput]::LEFTUP, 0, 0, 0, [IntPtr]::Zero)
    Start-Sleep -Milliseconds 400
}

$p = Get-Process -Name $procName -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $p) { Write-Output "app not running"; exit 1 }
[MInput]::SetForegroundWindow($p.MainWindowHandle) | Out-Null
Start-Sleep -Milliseconds 800

$r = Get-ClientRect $p
Write-Output "client: $($r.X),$($r.Y) $($r.W)x$($r.H)"
Capture "$root\screenshots\07-split-before.png" $p

# --- 拖动主横向分栏（左棋盘 | 右分析），从 56% 处向左拖 ---
$mainX = [int]($r.X + $r.W * 0.56)
$midY  = [int]($r.Y + $r.H * 0.45)
Write-Output "drag main splitter at x=$mainX y=$midY"
DragMouse $mainX $midY ([int]($r.X + $r.W * 0.38)) $midY
Capture "$root\screenshots\07-split-after-h.png" $p

# --- 拖动右侧纵向分栏 (候选 | 曲线/参数)，从 52% 处向下拖 ---
$r2 = Get-ClientRect $p
$rightX = [int]($r2.X + $r2.W * 0.78)
$vsY    = [int]($r2.Y + 30 + ($r2.H - 30) * 0.52)
Write-Output "drag right vsplit at x=$rightX y=$vsY"
DragMouse $rightX $vsY $rightX ([int]($r2.Y + 30 + ($r2.H - 30) * 0.78))
Capture "$root\screenshots\07-split-after-v.png" $p
