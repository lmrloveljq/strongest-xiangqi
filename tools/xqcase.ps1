param(
    [Parameter(Mandatory=$true)][string]$Cfg,
    [Parameter(Mandatory=$true)][string]$Name,
    [int]$Quit = 50,
    [string]$Mode = 'human',
    [string]$Moves = 'h2e2',
    [string]$MaxStrength = '',
    [string]$Bench = '',
    [int]$BenchDelay = 3,
    [string]$Moves2 = '',
    [int]$Moves2Delay = 12,
    [switch]$Diag
)

$root = 'C:\最强象棋软件系统'
Set-Location $root

$env:XQ_CONFIG = $Cfg
$env:XQ_MODE = $Mode
$env:XQ_MOVES = $Moves
$env:XQ_QUIT = "$Quit"
if ($Diag) { $env:XQ_DIAG = '1' } else { Remove-Item env:XQ_DIAG -ErrorAction SilentlyContinue }
if ($MaxStrength) { $env:XQ_MAXSTRENGTH = $MaxStrength } else { Remove-Item env:XQ_MAXSTRENGTH -ErrorAction SilentlyContinue }
if ($Bench) { $env:XQ_BENCH = $Bench; $env:XQ_BENCH_DELAY = "$BenchDelay" } else { Remove-Item env:XQ_BENCH, env:XQ_BENCH_DELAY -ErrorAction SilentlyContinue }
if ($Moves2) { $env:XQ_MOVES2 = $Moves2; $env:XQ_MOVES2_DELAY = "$Moves2Delay" } else { Remove-Item env:XQ_MOVES2, env:XQ_MOVES2_DELAY -ErrorAction SilentlyContinue }

$err = Join-Path $root "_verify\$Name.err"
$out = Join-Path $root "_verify\$Name.out"
Remove-Item $err, $out -Force -ErrorAction SilentlyContinue

$t0 = Get-Date
$p = Start-Process -FilePath "$root\_shots.exe" -WorkingDirectory $root -PassThru -RedirectStandardError $err -RedirectStandardOutput $out
Write-Output "started _shots.exe pid=$($p.Id) cfg=$Cfg quit=${Quit}s"

$samples = New-Object System.Collections.ArrayList
$deadline = $t0.AddSeconds($Quit)
while ((Get-Date) -lt $deadline -and -not $p.HasExited) {
    Start-Sleep -Milliseconds 1500
    $kids = @(Get-CimInstance Win32_Process -Filter "ParentProcessId=$($p.Id)" -ErrorAction SilentlyContinue |
              Where-Object { $_.Name -like 'Pikafish*' })
    foreach ($k in $kids) {
        $pr = Get-Process -Id $k.ProcessId -ErrorAction SilentlyContinue
        if ($pr) {
            $aff = 0
            try { $aff = $pr.ProcessorAffinity.ToInt64() } catch { }
            $null = $samples.Add(("t={0,3:N0}s pid={1} thr={2,2} pri={3,-12} aff=0x{4:X} cpu={5,7:N1}s ws={6,5}MB" -f `
                ((Get-Date) - $t0).TotalSeconds, $pr.Id, $pr.Threads.Count, $pr.PriorityClass, $aff, $pr.CPU, [int]($pr.WorkingSet64 / 1MB)))
        }
    }
}

if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force }
Start-Sleep -Milliseconds 500

Write-Output "=== _shots engine process samples (every row) ==="
foreach ($s in $samples) { Write-Output $s }
Write-Output "=== stderr tail ==="
if (Test-Path $err) { Get-Content $err | Select-Object -Last 40 }
