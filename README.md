# 最强象棋软件系统（Strongest Xiangqi）

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.27%2B-blue.svg)](https://go.dev/)
[![Platform: Windows](https://img.shields.io/badge/Platform-Windows-0078d4.svg)](#从源码构建)

> **Strongest Xiangqi** — 用 Go + [Fyne](https://fyne.io/) 编写的中国象棋**引擎桥接分析 + 引擎自动对战**桌面工具，可构建为单个原生 Windows 可执行文件，零运行时依赖。
>
> **请注意：本软件不是人机对弈游戏。** 它本身不会下棋，而是作为强大的第三方象棋引擎（默认 [Pikafish（皮卡鱼）](https://github.com/official-pikafish/Pikafish)）的图形界面与调度器。

---

## 目录

- [这是什么](#这是什么)
- [界面预览](#界面预览)
- [功能特性](#功能特性)
- [快速开始](#快速开始)
- [使用指南](#使用指南)
- [参数说明](#参数说明)
- [从源码构建](#从源码构建)
- [测试](#测试)
- [项目结构](#项目结构)
- [第三方组件与许可证](#第三方组件与许可证)

---

## 这是什么

本软件解决两个真实场景：

| 场景 | 你在做什么 | 软件在做什么 |
| --- | --- | --- |
| **A. 引擎桥接分析**（核心） | 在手机/电脑上的第三方象棋软件里对弈，同时打开本软件充当"最强参谋" | 你把第三方软件的最新着法同步到本软件棋盘，Pikafish 满配深度思考后给出最强应对；一键复制着法，再走回第三方软件 |
| **B. 引擎自动对战** | 想实测两个引擎的相对实力 | 加载两个引擎批量自动对弈 N 局，自动统计胜/和/负、搜索深度、节点数，保存棋谱（PGN，Portable Game Notation，可移植棋局记法）与对战报告 |

**明确不做**：人机对弈游戏、AI 评语、复盘数据库、杀法/残局训练、联机对战、云同步。软件本身**不含任何网络请求**，不联网、不上传任何数据。

---

## 界面预览

**引擎桥接分析模式** —— 棋盘在左，最佳着法、多候选与胜率曲线在右：

![桥接分析](screenshots/01-bridge.png)

**引擎自动对战模式** —— 实时显示比分、棋盘与对战日志：

![引擎对战](screenshots/03-match.png)

**引擎管理面板** —— 自动扫描、注册、测试引擎连接：

![引擎管理](screenshots/04-engine-manager.png)

> 更多截图见 [`screenshots/`](screenshots/) 目录。

---

## 功能特性

**引擎桥接分析**

- 三种局面输入：点击走子 / UCI 坐标序列输入 / 手动摆盘（支持任意局面编辑）
- 局面一变即自动思考，无需点击"开始"
- 最佳着法同时显示**中文记谱**与 **UCI 坐标**，附胜率、主变例、深度、节点、速度
- 多候选着法（MultiPV，Multi Principal Variation，多主线搜索）列表，含温度 softmax 概率
- 红黑双方胜率随回合变化的实时曲线
- 一键复制最佳着法（4 字符 UCI 坐标），直接粘贴回第三方软件
- 支持粘贴整段着法序列，自动兼容多种分隔符与编号格式

**引擎自动对战**

- 两个引擎独立子进程，串行交替走子，界面永不阻塞
- 内置完整中国象棋规则引擎，逐步校验合法性（非法着法直接判负）
- 自动判定：将死、困毙、重复局面（三次判和）、400 着上限（保证绝不无限循环）
- 每步思考超时或引擎崩溃自动判该方负，不影响另一方
- 先后手自动轮换；支持每步限时 / 每局总时间 / 固定深度三种时间控制
- 自动保存 PGN 棋谱并生成含胜率、平均深度、节点速度的文本报告

**引擎库**

- 自动扫描 `engines/` 目录（最深 3 层），逐个启动探测协议
- 同时支持 **UCI**（Universal Chess Interface，通用象棋接口）与 **UCCI**（Universal Chinese Chess Interface，通用中国象棋接口）引擎
- 自动解析引擎上报的全部参数，按类型生成滑块 / 开关 / 下拉框 / 按钮

---

## 快速开始

### 第 1 步：获取本软件

- 方式一（推荐）：到 [Releases 页面](https://github.com/lmrloveljq/strongest-xiangqi/releases)下载已构建好的 `xiangqi.exe`；
- 方式二：克隆本仓库后自行构建（见[从源码构建](#从源码构建)）。

### 第 2 步：下载 Pikafish（必需）

引擎不随本仓库分发。请通过以下任一官方渠道下载：

- 官网：<https://www.pikafish.com/zh-cn/>（点击"下载"，选择 **Windows 纯引擎文件**，`universal` 版本可自动适配 CPU 指令集）
- GitHub Releases：<https://github.com/official-pikafish/Pikafish/releases>

下载后你会得到（文件名随版本变化）：

```
Pikafish-Windows-x86-64-universal.exe   # 引擎可执行文件
pikafish.nnue                            # 神经网络权重（约 48MB）
```

### 第 3 步：放置引擎

把 **exe 和权重文件放在同一个文件夹中**，再将该文件夹放入软件目录下的 `engines/`：

```
<软件所在目录>\
└── engines\
    └── pikafish\
        ├── Pikafish-Windows-x86-64-universal.exe
        └── pikafish.nnue
```

> exe 与权重必须同目录，否则引擎能启动但棋力会严重退化。详见 [`engines/README.md`](engines/README.md)。

### 第 4 步：启动

双击 `xiangqi.exe`（或源码方式下双击 `start.bat`）。首次运行会自动：

- 扫描并注册 `engines/` 下的 Pikafish，设为默认引擎；
- 生成 `config.json`（参数配置）与 `engines.json`（引擎注册表）；
- 创建 `matches/`（对战输出）目录。

---

## 使用指南

### 场景 A：桥接分析

1. 在第三方象棋软件中走完一步；
2. 在本软件棋盘上用**相同着法**同步局面（点击走子，或在坐标框输入 UCI 着法序列后回车）；
3. 软件自动思考，右栏显示最佳着法，如"炮二平五 `h2e2`"；
4. 点击**「复制最佳着法」**，回到第三方软件粘贴走棋；
5. 重复以上循环。

> 第三方软件的局面非标准（如让子、残局）时，用「编辑局面」手动摆盘。

### 场景 B：引擎对战

1. 顶部切换到**「引擎对战」**；
2. 选择己方引擎与对手引擎（可选同一个引擎进行自我对弈，软件会启动两个独立进程）；
3. 设置对局数量、时间控制，勾选先后手轮换；
4. 点击**「开始对战」**。可随时暂停 / 终止；
5. 结束后在 `matches/<时间戳>/` 查看 PGN 棋谱与 `report.txt` 报告。

### 参数说明

| 参数 | 范围 | 默认 | 说明 |
| --- | --- | --- | --- |
| Threads（线程） | 1–1024 | CPU 逻辑核数 | 引擎搜索线程数；对战双方线程总数建议不超过逻辑核数，界面会给出超订警告 |
| Hash（哈希表） | 1–33554432 MB | 4096 | 引擎置换表大小 |
| MultiPV（多主线） | 1–128 | 分析 8 / 对战 1 | 同时显示的候选着法数量 |
| Depth（固定深度） | 不限 | 20 | 深度优先模式的搜索层数 |
| Movetime（每步限时） | 0.1–60 秒 | 3 秒 | 限时优先模式每步思考时间 |
| Temperature（温度 T） | 40–300 | 120 | 仅影响候选着法的 softmax 概率展示 |

引擎上报的其他动态参数（如 Ponder、EvalFile、NumaPolicy 等）会自动出现在参数面板，修改后立即通过 `setoption` 下发。完整引擎接口说明见 [`docs/ENGINES.md`](docs/ENGINES.md)。

---

## 从源码构建

**环境要求**

- [Go](https://go.dev/dl/) 1.27 或更高；
- mingw-w64 GCC（Fyne 在 Windows 上依赖 cgo），可用 `winget install BrechtSanders.WinLibs.POSIX.UCRT` 安装。

**构建命令**

```powershell
go mod tidy
go build -ldflags "-H windowsgui -s -w" -o xiangqi.exe .
```

- `-H windowsgui`：启动时不弹出控制台窗口；
- `-s -w`：剥离符号表与调试信息，减小可执行文件体积。

也可以直接双击 `start.bat`：脚本检测到 exe 不存在时会自动调用上述命令现场构建。

---

## 测试

```powershell
go vet ./...          # 静态检查
go test ./...         # 全部单元测试
```

- `rules/` 包含基于 perft（性能/正确性遍历）的走法生成测试；
- `engine/` 包含真实引擎集成测试，若 `engines/` 下未放置引擎则自动跳过；
- 其余各包（`notation`、`config`、`analytics`、`ui`）均配有测试文件。

---

## 项目结构

```
strongest-xiangqi/
├── main.go              # 程序入口
├── version/             # 版本常量
├── config/              # 配置读写、默认值与校验
├── rules/               # 象棋规则引擎（棋盘、走法生成、胜负判定，无外部依赖）
├── notation/            # UCI 坐标 ↔ 中文记谱双向转换
├── analytics/           # 胜率公式与温度 softmax
├── engine/              # 引擎子进程通信、协议解析、引擎库扫描
├── match/               # 自动对战调度、PGN 与报告生成
├── ui/                  # Fyne 界面（棋盘自绘、各模式面板、主题）
├── assets/              # 背景图等静态资源
├── engines/             # 引擎存放目录（用户自行放入，见 engines/README.md）
├── tools/               # 构建脚本与开发/取证辅助工具
├── docs/                # 引擎接口文档与历史开发记录
└── screenshots/         # README 展示截图
```

**并发约定**：Fyne 控件只在主 goroutine 读写；每个引擎进程由独立 goroutine 读取输出；自动对战在独立 goroutine 中运行，通过事件 channel 驱动界面。引擎命令带防重入保护、思考硬超时与崩溃恢复。

---

## 第三方组件与许可证

- **本项目代码**：[MIT License](LICENSE) © 2026 lmrloveljq
- **Pikafish（皮卡鱼）引擎**：由用户自行下载放置，本软件不下载、不编译、不修改、不分发。Pikafish 基于 **GPL-3.0** 许可，版权归 Pikafish 开发者所有，仓库：<https://github.com/official-pikafish/Pikafish>
- **Fyne UI 框架**：[BSD-3-Clause](https://github.com/fyne-io/fyne/blob/master/LICENSE)，<https://fyne.io/>

本软件通过 UCI/UCCI 协议以独立子进程方式调用引擎，不链接引擎代码。

### 免责声明

本软件仅用于学习、研究与棋艺分析；不保证分析结果的绝对正确性，不对据此进行的任何对弈结果负责。请遵守你所使用的第三方对弈平台的规则。
