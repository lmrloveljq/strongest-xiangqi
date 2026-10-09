# xqrealmove.ps1 -- real mouse end-to-end: click a red cannon h2->e2 on the real window,
# then check whether the engine answered (diag log says moves=2, humanTurn=true).
#
# Why real clicks: the paste box (XQ_MOVES) goes through applySequence; a real click goes
# through Board.Tapped -> canBoardAcceptInput -> move callbacks. Both must work, and only
# a real click proves the one the user actually uses.
param(
    [string]$Cfg = '_verify\cfg-r0.json',
    [string]$Name = 'realclick',
    [int]$WaitEngine = 20,
    [int]$Quit = 95
)

$root = 'C:\最强象棋软件系统'
Set-Location $root

$env:XQ_CONFIG = $Cfg
$env:XQ_MODE = 'human'
$env:XQ_HIT = '1'
$env:XQ_DIAG = '1'
$env:XQ_DEBUG = '1'
$env:XQ_QUIT = "$Quit"
$env:XQ_SHOT = "$root\_verify\98-realclick.png"
$env:XQ_SHOT_DELAY = "$($Quit - 8)"
Remove-Item env:XQ_MOVES, env:XQ_MOVES2, env:XQ_BENCH, env:XQ_MAXSTRENGTH, env:XQ_MENUSMOKE -ErrorAction SilentlyContinue

$err = "$root\_verify\$Name.err"
Remove-Item $err -Force -ErrorAction SilentlyContinue
$p = Start-Process -FilePath "$root\_shots.exe" -WorkingDirectory $root -PassThru -RedirectStandardError $err -RedirectStandardOutput "$root\_verify\$Name.out"

Write-Output "started pid=$($p.Id); 等引擎就绪 $WaitEngine 秒"
Start-Sleep -Seconds $WaitEngine

# --- 1) 探针点击：拿到「屏幕物理坐标 <-> 棋盘本地逻辑坐标」的对应关系 ---
pwsh -ExecutionPolicy Bypass -File "$root\tools\xqwin.ps1" -Action click -X 500 -Y 500 -ProcName _shots | Out-Null
Start-Sleep -Milliseconds 600
$hit = (Get-Content $err | Select-String -Pattern "\[hit\]" | Select-Object -Last 1).Line
Write-Output "probe: $hit"
if (-not $hit) { Write-Output "!! 没有命中日志，探针点击没落到棋盘上"; Stop-Process -Id $p.Id -Force; exit 1 }

$m = [regex]::Match($hit, 'ev\.Position=\(([\d.]+),([\d.]+)\)\s+boardLogical=([\d.]+) x ([\d.]+) cell=([\d.]+) ox=(-?[\d.]+) oy=(-?[\d.]+)')
if (-not $m.Success) { Write-Output "!! 命中日志格式没解析出来: $hit"; Stop-Process -Id $p.Id -Force; exit 1 }
$lx, $ly = [double]$m.Groups[1].Value, [double]$m.Groups[2].Value
$cell, $ox, $oy = [double]$m.Groups[5].Value, [double]$m.Groups[6].Value, [double]$m.Groups[7].Value

# 探针点击点是客户区物理 (500,500)，对应棋盘本地逻辑 (lx,ly)；DPI 缩放 1.25
$scale = 1.25
$ax = 500 - $lx * $scale
$ay = 500 - $ly * $scale
Write-Output ("映射: cell={0:N2} ox={1:N2} oy={2:N2} 客户区原点补偿=({3:N1},{4:N1})" -f $cell, $ox, $oy, $ax, $ay)

# 交叉点 (file, rank) -> 客户区物理坐标（未翻转：红方在下，ScreenRow(rank)=9-rank）
function Sq2XY([int]$file, [int]$rank) {
    $x = $ax + ($ox + $file * $cell) * $scale
    $y = $ay + ($oy + (9 - $rank) * $cell) * $scale
    return @([int]$x, [int]$y)
}

# --- 2) 真点击走一步：红炮 h2（file7,rank2）-> e2（file4,rank2），即「炮二平五」 ---
$from = Sq2XY 7 2
$to = Sq2XY 4 2
Write-Output "click 炮 h2 @ ($($from[0]),$($from[1]))  ->  e2 @ ($($to[0]),$($to[1]))"
pwsh -ExecutionPolicy Bypass -File "$root\tools\xqwin.ps1" -Action click -X $from[0] -Y $from[1] -ProcName _shots | Out-Null
Start-Sleep -Milliseconds 500
pwsh -ExecutionPolicy Bypass -File "$root\tools\xqwin.ps1" -Action click -X $to[0] -Y $to[1] -ProcName _shots | Out-Null
Start-Sleep -Seconds 12

Write-Output "=== 真点击之后的 diag（moves 应从 0 变 1，再由引擎应招变 2）==="
Get-Content $err | Select-String -Pattern "\[hit\]|\[diag\]" | Select-Object -Last 10 | ForEach-Object { $_.Line }

if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force }
