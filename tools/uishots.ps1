# uishots.ps1 —— 界面验证截图（当前版本的界面长什么样，逐张存档）。
#
# 抓五张：
#   70-analyze     分析模式（默认首屏）
#   71-match       引擎对战（切模式即开赛，验证「点上方栏目直接开始」）
#   72-human       人机对弈
#   73-engine-set  引擎设置对话框
#   74-quickstart  快速上手
#
# 为什么用临时 exe 而不是 go run：exeDir() 只对 "go-build" 临时目录回退到工作目录，
# 用 go run 时 config.json / engines.json / assets 的解析路径与真实运行不一致。
# 临时 exe 验证完立即删除，工程里不留下任何 exe（用户要求：不许产出 exe）。
#
# 用法：pwsh -File tools\uishots.ps1

$env:Path = "C:\Program Files\Go\bin;C:\Users\ASUS\AppData\Local\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin;" + $env:Path
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $root

Get-Process xiangqi, _shots -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 600

# 截图过程会改写 config.json（窗口尺寸 / 上次模式等），验证完必须还原
$cfgBak = "$root\_verify\config.json.uishotbak"
Copy-Item "$root\config.json" $cfgBak -Force

go build -o _shots.exe . 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Output "构建失败"; exit 1 }

function Shot($name, $envs, $delay) {
    foreach ($k in $envs.Keys) { Set-Item -Path "env:$k" -Value $envs[$k] }
    $env:XQ_SHOT = "$root\_verify\$name.png"
    $env:XQ_SHOT_DELAY = "$delay"
    $env:XQ_QUIT = '2'
    $spArgs = @{
        FilePath               = "$root\_shots.exe"
        WorkingDirectory       = $root
        RedirectStandardOutput = "$root\_verify\_u.out"
        RedirectStandardError  = "$root\_verify\_u.err"
        PassThru               = $true
    }
    $p = Start-Process @spArgs
    $p.WaitForExit(200000) | Out-Null
    Select-String -Path "$root\_verify\_u.err" -Pattern '\[shot\]|\[font\]' |
        Select-Object -ExpandProperty Line
    foreach ($k in $envs.Keys) { Remove-Item -Path "env:$k" -ErrorAction SilentlyContinue }
    Remove-Item -Path "env:XQ_SHOT", "env:XQ_SHOT_DELAY", "env:XQ_QUIT" -ErrorAction SilentlyContinue
}

# 1) 分析模式：摆一段着法，让右栏有内容
Shot '70-analyze' @{ XQ_MOVES = 'h2e2 h9g7 c3c4 i9h9' } 12

# 2) 引擎对战：切模式即开赛，等对战真跑起来再截
Shot '71-match' @{ XQ_MODE = 'match' } 16

# 3) 人机对弈
Shot '72-human' @{ XQ_MOVES = 'h2e2 h9g7'; XQ_MODE = 'human' } 12

# 4) 引擎设置对话框
Shot '73-engine-set' @{ XQ_PARAMS = '1' } 10

# 5) 快速上手
Shot '74-quickstart' @{ XQ_HELP = '1' } 9

Copy-Item $cfgBak "$root\config.json" -Force
Remove-Item "$root\_shots.exe", "$root\_verify\_u.out", "$root\_verify\_u.err" -Force -ErrorAction SilentlyContinue
Write-Output "界面截图完成（config.json 已还原）"
