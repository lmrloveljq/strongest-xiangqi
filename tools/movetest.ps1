# movetest.ps1 —— 用真实鼠标完成「选子 → 落子」两步点击，并从 XQ_HIT 日志核对命中格。
# 用法: pwsh -File movetest.ps1 -From "b0" -To "c2"
param(
	[string]$From = 'b0',
	[string]$To = 'c2',
	[string]$Log = '',
	[int]$ProbeX = -1,
	[int]$ProbeY = -1,
	[switch]$Probe
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
if ($Log -eq '') { $Log = Join-Path $root '_verify\bat-fix.log' }

function Read-Log {
	$fs = [System.IO.File]::Open($Log, 'Open', 'Read', 'ReadWrite')
	$sr = New-Object System.IO.StreamReader($fs, (New-Object System.Text.UTF8Encoding($false)))
	$t = $sr.ReadToEnd(); $sr.Close(); $fs.Close(); return $t
}
function Last-Hit {
	$t = Read-Log
	$m = [regex]::Matches($t, '\[hit\] ev\.Position=\(([-\d.]+),([-\d.]+)\) boardLogical=([\d.]+) x ([\d.]+) cell=([\d.]+) ox=([\d.]+) oy=([-\d.]+) -> sq=(-?\d+) ok=(\w+)')
	if ($m.Count -eq 0) { return $null }
	$g = $m[$m.Count - 1].Groups
	return [pscustomobject]@{
		px = [double]$g[1].Value; py = [double]$g[2].Value
		cell = [double]$g[5].Value; ox = [double]$g[6].Value; oy = [double]$g[7].Value
		sq = [int]$g[8].Value; ok = $g[9].Value
	}
}
function Sq2FileRank($s) {
	$f = [int][char]([string]$s).ToCharArray()[0] - [int][char]'a'
	$r = [int]([string]$s).Substring(1, 1)
	return @($f, $r)
}

$geo = Last-Hit
if (-not $geo) { Write-Output 'ERROR: 日志里没有 [hit] 记录，先用 -Probe 点一次'; exit 1 }

# 棋盘原点（客户区逻辑坐标）= 上次探测点击的客户区物理坐标 / scale − 该次点击在棋盘内的逻辑位置
#   注意：必须用「探测点击自己的客户区坐标」，不能用 ox —— 后者是格线原点，不是控件原点。
$scale = 1.3
if ($ProbeX -lt 0 -or $ProbeY -lt 0) {
	Write-Output 'ERROR: 需要 -ProbeX / -ProbeY 指定上一次探测点击的客户区物理坐标'
	exit 1
}
$boardAbsX = $ProbeX / $scale - $geo.px
$boardAbsY = $ProbeY / $scale - $geo.py

function CellPx($cell) {
	$fr = Sq2FileRank $cell
	$lx = $boardAbsX + $geo.ox + $fr[0] * $geo.cell
	$ly = $boardAbsY + $geo.oy + (9 - $fr[1]) * $geo.cell
	return @([int][math]::Round($lx * $scale), [int][math]::Round($ly * $scale))
}

$hwndArg = @()
$p1 = CellPx $From
$p2 = CellPx $To
Write-Output ("boardAbs=(%.1f,%.1f) cell=%.3f scale=%.2f" -f $boardAbsX, $boardAbsY, $geo.cell, $scale)
Write-Output ("click {0} at client=({1},{2}) ; {3} at client=({4},{5})" -f $From, $p1[0], $p1[1], $To, $p2[0], $p2[1])

& pwsh -NoProfile -File (Join-Path $root 'tools\xqwin.ps1') -Action click -X $p1[0] -Y $p1[1] | Select-Object -Last 1
Start-Sleep -Milliseconds 900
& pwsh -NoProfile -File (Join-Path $root 'tools\xqwin.ps1') -Action click -X $p2[0] -Y $p2[1] | Select-Object -Last 1
Start-Sleep -Seconds 2

$t = Read-Log
Write-Output '=== 最近 4 条 [hit] ==='
($t -split "`r?`n") | Where-Object { $_ -match '^\[hit\]' } | Select-Object -Last 4
