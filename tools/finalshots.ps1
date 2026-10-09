$env:Path = "C:\Program Files\Go\bin;C:\Users\ASUS\AppData\Local\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin;" + $env:Path
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $root
Get-Process xiangqi,xiangqi-debug -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 600

# 用一个临时调试构建（保留 stderr 通道）驱动最终截图；验证完立即删除
go build -o _shots.exe . 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Output "debug build failed"; exit 1 }

function Shot($name, $envs, $delay) {
    foreach ($k in $envs.Keys) { Set-Item -Path "env:$k" -Value $envs[$k] }
    $env:XQ_SHOT = "$root\screenshots\$name.png"
    $env:XQ_SHOT_DELAY = "$delay"
    $env:XQ_QUIT = '2'
    $spArgs = @{
        FilePath               = "$root\_shots.exe"
        WorkingDirectory       = $root
        RedirectStandardOutput = "$root\_s.out"
        RedirectStandardError  = "$root\_s.err"
        PassThru               = $true
    }
    $p = Start-Process @spArgs
    $p.WaitForExit(200000) | Out-Null
    Select-String -Path "$root\_s.err" -Pattern '\[shot\]|\[stress\]' | Select-Object -ExpandProperty Line
    foreach ($k in $envs.Keys) { Remove-Item -Path "env:$k" -ErrorAction SilentlyContinue }
    Remove-Item -Path "env:XQ_SHOT","env:XQ_SHOT_DELAY","env:XQ_QUIT" -ErrorAction SilentlyContinue
}

# 1) 桥接分析：应用一段着法序列，等引擎出结果
Shot '10-bridge-analysis' @{ XQ_MOVES = 'h2e2 h9g7 c3c4 i9h9 b0c2 b9c7' } 12

# 2) 参数面板 + 着法列表 + 粘贴输入三个页签
Shot '11-bridge-paste-tab' @{ XQ_MOVES = 'h2e2 h9g7' } 10

# 3) 引擎管理面板（已重新扫描，2 个引擎）
Shot '12-engine-manager' @{ XQ_SHOW_ENGINES = '1'; XQ_SHOT_WINDOW = 'engines'; XQ_RESCAN = '1' } 12

Remove-Item "$root\_shots.exe","$root\_s.out","$root\_s.err" -Force -ErrorAction SilentlyContinue
Write-Output "final shots done"
