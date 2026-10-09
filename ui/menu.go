package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"xiangqi/config"
	"xiangqi/notation"
	"xiangqi/rules"
)

// 本文件实现菜单栏 + 快捷键 + 界面开关。
//
// 结构参照同类软件（TCHESS 的 引擎/连线/开局库/局面/设置 五个顶级菜单；
// swiftxiangqi 的 Game / Analysis 菜单 + 一整套快捷键）：
//
//	引擎  引擎管理 / 重新扫描 / 关闭·启动引擎 / 参数设置
//	局面  翻转棋盘 / 回到开局 / 回到当前局面 / 悔棋 / 重开 / 摆盘 / 复制·粘贴 FEN
//	设置  显示坐标 / 走棋音效 / 棋盘大小（大中小）/ 显示高级参数
//	帮助  使用说明 / 关于
//
// 工具栏只留最常用的：模式切换 + 引擎选择 + 翻转 + 引擎开关 + 引擎管理 + 关于。

// buildMainMenu 构建菜单栏。
//
// 【v1.5 命名对齐】菜单项名称按同类软件（重点参考 TCHESS / public-Xiangqi 的
// 文件|局面|棋谱|开局库|引擎|连线|设置|帮助 结构）改成通行叫法：
//
//	引擎  引擎管理 / 重新扫描 / 启动·关闭 / 引擎设置
//	局面  新局面 / 编辑局面 / 交换行棋方 / 开局 / 终局 / 悔棋 / 复制·粘贴 FEN
//	对局  开始对战 / 暂停·继续 / 终止对战
//	棋谱  复制棋谱 PGN / 打开棋谱目录
//	设置  字号 / 显示坐标 / 走棋音效 / 棋盘大小 / 引擎设置
//	帮助  快速上手 / 使用说明 / 引擎接口文档 / 关于
//
// 用词依据：「局面」（不是「棋盘状态」）、「棋谱」（不是「着法记录」）、
// 「交换行棋方」（TCHESS 棋盘右键即此词）、「思考细节」（TCHESS 的引擎输出面板）、
// 「变招」（TCHESS 的候选着法列表）、「局势图」（TCHESS 的走势图页签）。
func (a *App) buildMainMenu() *fyne.MainMenu {
	// ---- 引擎 ----
	engineMenu := fyne.NewMenu("引擎",
		fyne.NewMenuItem("引擎管理…", func() { a.ShowEngineManager() }),
		fyne.NewMenuItem("重新扫描引擎库", func() { a.RescanEngines(true) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("启动 / 关闭引擎", func() { a.ToggleEngine() }),
		fyne.NewMenuItem("思考设置…", func() { a.ShowThinkSettings() }),
		fyne.NewMenuItem("参考引擎（Kibitzer）…", func() { a.ShowKibitzerSettings() }),
		a.kibStateItem(),
		a.checkItem("最强引擎模式（单引擎满配）", a.cfg.MaxStrength, func() { a.ToggleMaxStrength() }),
		fyne.NewMenuItem("跑 10 秒基准（nps / CPU 占用）…", func() { a.RunBenchmark() }),
		fyne.NewMenuItem("引擎设置…", func() { a.ShowEngineParams() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("打开引擎库目录", func() { a.OpenEngineLibraryDir() }),
	)

	// ---- 局面 ----
	positionMenu := fyne.NewMenu("局面",
		fyne.NewMenuItem("新局面", func() { a.resetGame() }),
		fyne.NewMenuItem("编辑局面", func() { a.toggleEditMode() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("交换行棋方", func() { a.SwapSides() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("开局", func() { a.jumpTo(0) }),
		fyne.NewMenuItem("终局", func() { a.jumpTo(-1) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("悔棋", func() { a.undo(1) }),
		fyne.NewMenuItem("撤销全部", func() { a.undo(len(a.game.Moves)) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("复制局面 FEN", func() { a.copyFEN() }),
		fyne.NewMenuItem("粘贴局面 FEN…", func() { a.pasteFEN() }),
	)

	// ---- 对局 ----
	//
	// 【v1.5 简化】「开始对战 / 暂停 / 终止」原来是主界面底部的一排按钮，
	// 现在收到这里：点菜单就直接开始，不用先找按钮、再回到界面确认。
	// 另外，点工具栏的「引擎对战」也会直接开赛（见 SetMode）。
	matchMenu := fyne.NewMenu("对局",
		fyne.NewMenuItem("开始对战", func() { a.StartMatch() }),
		a.matchPauseItem(),
		a.matchStopItem(),
		fyne.NewMenuItemSeparator(),
		// 【缺陷修复】这里原来直接 SetMode("match")，而 SetMode 会「切到引擎对战
		// 就自动开赛」——于是「对局设置…」一点就开打。改成只切视图、不开赛。
		fyne.NewMenuItem("立即出招（催引擎走一步）", func() { a.ForceEngineMove() }),
		fyne.NewMenuItem("对局设置…", func() {
			if a.matchView != nil {
				a.showMatchWithoutStarting()
				a.matchView.ShowSettings()
			}
		}),
		fyne.NewMenuItem("查看最近对战报告", func() { a.OpenLatestReport() }),
	)

	// ---- 棋谱 ----
	scoreMenu := fyne.NewMenu("棋谱",
		fyne.NewMenuItem("粘贴棋谱…", func() { a.ShowPasteDialog() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("复制棋谱 PGN", func() { a.copyPGN() }),
		fyne.NewMenuItem("打开棋谱目录", func() { a.OpenMatchDir() }),
	)

	// ---- 设置 ----
	//
	// 字号三档平铺（不做二级子菜单），当前档位用 ✓ 标出来。
	settingsMenu := fyne.NewMenu("设置",
		a.fontTierItem(fontScaleStandard),
		a.fontTierItem(fontScaleLarge),
		a.fontTierItem(fontScaleXLarge),
		fyne.NewMenuItemSeparator(),
		a.themeItem(config.ThemeDefault),
		a.themeItem(config.ThemeInkGold),
		a.checkItem("背景：主题底纹", a.cfg.BackgroundStyle == config.BgStyleTexture,
			func() { a.setBackgroundStyle(config.BgStyleTexture) }),
		a.checkItem("背景：纯色", a.cfg.BackgroundStyle == config.BgStyleSolid,
			func() { a.setBackgroundStyle(config.BgStyleSolid) }),
		a.checkItem("背景：自定义图片", a.cfg.BackgroundStyle == config.BgStyleImage,
			func() { a.setBackgroundStyle(config.BgStyleImage) }),
		fyne.NewMenuItemSeparator(),
		a.checkItem("显示棋盘坐标", a.cfg.ShowCoords, func() { a.toggleCoords() }),
		a.checkItem("窗口置顶", a.cfg.AlwaysOnTop, func() { a.toggleAlwaysOnTop() }),
		a.checkItem("显示行棋线路", a.cfg.ShowLines, func() { a.toggleShowLines() }),
		a.checkItem("走棋音效", a.cfg.SoundOn, func() { a.toggleSound() }),
		fyne.NewMenuItemSeparator(),
		a.checkItem("棋盘：大", a.boardScaleIs(boardScaleLarge), func() { a.setBoardScale(boardScaleLarge) }),
		a.checkItem("棋盘：中", a.boardScaleIs(boardScaleMedium), func() { a.setBoardScale(boardScaleMedium) }),
		a.checkItem("棋盘：小", a.boardScaleIs(boardScaleSmall), func() { a.setBoardScale(boardScaleSmall) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("思考设置…", func() { a.ShowThinkSettings() }),
		fyne.NewMenuItem("引擎设置…", func() { a.ShowEngineParams() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("备份配置…", func() { a.BackupConfig() }),
		fyne.NewMenuItem("还原最近一次备份…", func() { a.RestoreLatestBackup() }),
	)

	// ---- 帮助 ----
	helpMenu := fyne.NewMenu("帮助",
		fyne.NewMenuItem("快速上手", func() { a.ShowQuickStart() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("使用说明（README）", func() { a.openDoc("README.md") }),
		fyne.NewMenuItem("引擎接口文档（ENGINES.md）", func() { a.openDoc("docs\\ENGINES.md") }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("关于 象棋强软", func() { a.ShowAbout() }),
	)

	return fyne.NewMainMenu(engineMenu, positionMenu, matchMenu, scoreMenu, settingsMenu, helpMenu)
}

// ---------------------------------------------------------------------------
// 对战开关（菜单「对局」）
// ---------------------------------------------------------------------------

// refreshMainMenu 重建主菜单。
//
// Fyne 的菜单项是**静态**的：勾选态、文字（暂停 / 继续）、可用性一旦变化，
// 只能整体重建才会显示出来。重建很便宜（就是新建几个 MenuItem 对象）。
func (a *App) refreshMainMenu() {
	if a.win == nil {
		return
	}
	a.win.SetMainMenu(a.buildMainMenu())
}

// collectMenuItems 收集主菜单里所有可点的菜单项（跳过分隔线与会拉起外部程序的那几项）。
//
// 供 XQ_MENUSMOKE 做「逐个点一遍」的菜单体检：外部程序类（打开目录 / 打开文档）
// 不属于界面缺陷范围，跳过可以避免体检时弹出一堆资源管理器窗口。
func (a *App) collectMenuItems() []*fyne.MenuItem {
	skip := []string{"打开引擎库目录", "打开棋谱目录", "使用说明", "引擎接口文档", "查看最近对战报告"}
	var out []*fyne.MenuItem
	for _, top := range a.buildMainMenu().Items {
		for _, it := range top.Items {
			if it == nil || it.Label == "" {
				continue // 分隔线
			}
			skipIt := false
			for _, s := range skip {
				if strings.Contains(it.Label, s) {
					skipIt = true
					break
				}
			}
			if !skipIt {
				out = append(out, it)
			}
		}
	}
	return out
}

// closeTopOverlay 关掉当前最上层弹窗（菜单体检用，避免十几个对话框叠在一起）。
func (a *App) closeTopOverlay() {
	if a.win == nil {
		return
	}
	ov := a.win.Canvas().Overlays()
	if top := ov.Top(); top != nil {
		ov.Remove(top)
	}
}

// StartMatch 切到「引擎对战」模式并立即开始对战。
//
// 用户要求「点菜单就直接开始」，所以这里不做二次确认，
// 参数一律用当前界面/配置里的值（引擎、局数、时间控制都在对战设置里调）。
func (a *App) StartMatch() {
	if a.matchView == nil {
		return
	}
	a.SetMode("match")
	a.matchView.Start()
}

// showMatchWithoutStarting 切到「引擎对战」的界面，但**不开赛**。
//
// 供「对局设置…」这类只想看设置的入口使用：SetMode("match") 的语义是
// 「用户点了引擎对战按钮 = 要开打」，而打开设置对话框显然不是这个意思。
func (a *App) showMatchWithoutStarting() {
	a.matchNoAutoStart = true
	a.SetMode("match")
	a.matchNoAutoStart = false
}

// ToggleMatch 是工具栏「引擎对战」按钮的行为：没在打就开打，正在打就**终止**。
//
// 依据同类软件：TCHESS 的「引擎执红 / 引擎执黑」是 toggle 按钮，再点一次同一个按钮
// 就停；象棋巫师同理（「电脑执红 / 电脑执黑」+「停止思考」）。
// 上一版只能从菜单里终止，界面上没有停止入口——用户反馈「点击引擎对弈后取消不了」。
func (a *App) ToggleMatch() {
	if a.matchRunning() {
		a.matchView.Stop()
		a.refreshMatchButton()
		a.setStatusInfo("已终止对战（已完成的对局与报告都保留在 matches 目录）")
		return
	}
	a.SetMode("match")
}

// refreshMatchButton 让工具栏按钮反映对战状态：进行中显示「终止对战」。
//
// 文案随状态变是刻意的——用户不需要去菜单里找停止入口，按钮自己就是开关。
func (a *App) refreshMatchButton() {
	if len(a.modeBtns) < 2 || a.modeBtns[1] == nil {
		return
	}
	if a.matchRunning() {
		a.modeBtns[1].SetText("终止对战")
	} else {
		a.modeBtns[1].SetText("引擎对战")
	}
	a.modeBtns[1].Refresh()
}

// matchRunning 报告引擎对战是否正在进行。
func (a *App) matchRunning() bool {
	return a.matchView != nil && a.matchView.running
}

// BackupConfig 把 config.json / engines.json 备份到 backup\<时间戳>\。
//
// 【v1.6.9 / 头脑风暴第 20 条】配置一键备份：这两个文件是全部「软件记住了什么」，
// 换机器、试参数试乱了、或者升级前，先按一下这份保险。
func (a *App) BackupConfig() {
	stamp := time.Now().Format("20060102-150405")
	dir := filepath.Join(a.baseDir, "backup", stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dialog.ShowError(err, a.win)
		return
	}
	saved := 0
	for _, name := range []string{"config.json", "engines.json"} {
		src := filepath.Join(a.baseDir, name)
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		saved++
	}
	if saved == 0 {
		dialog.ShowInformation("没有可备份的配置",
			"还没找到 config.json / engines.json；先正常用一次软件，配置会自动生成。", a.win)
		return
	}
	a.setStatusInfo("配置已备份：" + dir)
	dialog.ShowInformation("备份完成",
		fmt.Sprintf("已备份 %d 个文件到：\n%s\n\n还原时用「设置」里的「还原最近一次备份」。", saved, dir), a.win)
}

// RestoreLatestBackup 还原最近一次配置备份（会先确认，再覆盖现用配置）。
func (a *App) RestoreLatestBackup() {
	root := filepath.Join(a.baseDir, "backup")
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) == 0 {
		dialog.ShowInformation("没有备份",
			"backup 目录里还没有备份。先用「设置」里的「备份配置」存一份。", a.win)
		return
	}
	latest := ""
	for _, e := range entries {
		if e.IsDir() && e.Name() > latest {
			latest = e.Name()
		}
	}
	if latest == "" {
		dialog.ShowInformation("没有备份", "backup 目录里没有可用的备份文件夹。", a.win)
		return
	}
	dir := filepath.Join(root, latest)
	msg := fmt.Sprintf("将用备份 %s 覆盖当前的 config.json / engines.json。\n\n"+
		"窗口大小、引擎参数、思考设置、参考引擎等都会回到备份时的状态。\n"+
		"**界面要重启软件后才会完全生效。**\n\n确定还原吗？", latest)
	dialog.ShowConfirm("还原配置", msg, func(ok bool) {
		if !ok {
			return
		}
		restored := 0
		for _, name := range []string{"config.json", "engines.json"} {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			if err := os.WriteFile(filepath.Join(a.baseDir, name), data, 0o644); err != nil {
				dialog.ShowError(err, a.win)
				return
			}
			restored++
		}
		if restored == 0 {
			dialog.ShowInformation("备份里没有配置文件", dir, a.win)
			return
		}
		a.setStatusInfo("已还原配置，重启软件后完全生效")
		dialog.ShowInformation("还原完成",
			fmt.Sprintf("已还原 %d 个文件。\n\n请关闭并重新打开软件，让新配置完全生效。", restored), a.win)
	}, a.win)
}

// OpenLatestReport 用系统默认程序打开最近一次对战的报告（matches\<时间戳>\report.txt）。
//
// 目录名是时间戳，所以字典序最大的就是最新一次；报告不在时给提示而不是静默失败。
func (a *App) OpenLatestReport() {
	dir := filepath.Join(a.baseDir, "matches")
	entries, err := os.ReadDir(dir)
	if err != nil {
		dialog.ShowInformation("还没有对战报告",
			"先跑一场引擎对战，棋谱与报告会写到：\n"+dir, a.win)
		return
	}
	latest := ""
	for _, e := range entries {
		if e.IsDir() && e.Name() > latest {
			latest = e.Name()
		}
	}
	if latest == "" {
		dialog.ShowInformation("还没有对战报告",
			"matches 目录里还没有对局记录。\n先跑一场引擎对战即可。", a.win)
		return
	}
	p := filepath.Join(dir, latest, "report.txt")
	if !fileExists(p) {
		dialog.ShowInformation("找不到报告文件", p, a.win)
		return
	}
	_ = exec.Command("cmd", "/c", "start", "", p).Start()
	a.setStatusInfo("已打开最近一次对战报告：" + p)
}

// matchPauseItem 生成「暂停 / 继续」菜单项，文字与可用性跟随当前状态。
func (a *App) matchPauseItem() *fyne.MenuItem {
	label := "暂停对战"
	if a.matchView != nil && a.matchView.paused {
		label = "继续对战"
	}
	it := fyne.NewMenuItem(label, func() {
		if a.matchView != nil {
			a.matchView.TogglePause()
		}
	})
	it.Disabled = a.matchView == nil || !a.matchView.running
	return it
}

// matchStopItem 生成「终止对战」菜单项。
func (a *App) matchStopItem() *fyne.MenuItem {
	it := fyne.NewMenuItem("终止对战", func() {
		if a.matchView != nil {
			a.matchView.Stop()
		}
	})
	it.Disabled = a.matchView == nil || !a.matchView.running
	return it
}

// checkItem 生成带勾选的开关菜单项：勾选态直接反映当前设置。
//
// 对用户尤其重要——菜单项没有勾的话，点一下到底开没开、现在是什么状态，
// 只能靠界面别处去猜。
func (a *App) checkItem(label string, checked bool, tapped func()) *fyne.MenuItem {
	it := fyne.NewMenuItem(label, tapped)
	it.Checked = checked
	return it
}

// boardScaleIs 判断当前分栏比例是否就是某个棋盘档位（容差 1%，因为分栏可拖动）。
func (a *App) boardScaleIs(v float64) bool {
	d := a.cfg.MainSplit - v
	if d < 0 {
		d = -d
	}
	return d < 0.01
}

// fontTierItem 生成一档字号菜单项，当前档位带勾。
func (a *App) fontTierItem(name string) *fyne.MenuItem {
	return newFontTierItem(name, FontScaleName(FontScale()), func() { a.SetFontTier(name) })
}

// newFontTierItem 是纯函数版本（不依赖 *App），便于单测断言勾选态。
func newFontTierItem(name, current string, tapped func()) *fyne.MenuItem {
	it := fyne.NewMenuItem("字号："+fontTierLabel(name), tapped)
	it.Checked = current == name
	return it
}

// ---------------------------------------------------------------------------
// 适老化：字号档位切换
// ---------------------------------------------------------------------------

// SetFontTier 切换字号档位（standard / large / xlarge），立即生效并落盘。
//
// 「立即生效」分两半，缺一不可：
//  1. 换主题 —— Fyne 内建控件（按钮、菜单、输入框、列表行）的高度与字号都由主题给出，
//     重新 SetTheme 会让它们全部按新档位重排，顺带把点击目标一起放大；
//  2. 走一遍界面树 —— 工程里写死字号的 canvas.Text（面板标题、状态栏、棋盘文字…）
//     不归主题管，由 ApplyFontScale 按角色换算。
//
// 全程不重建任何控件，所以正在下的棋、正在跑的分析都不会被打断。
func (a *App) SetFontTier(name string) {
	old := FontScale()
	SetFontScale(FontScaleFor(name))
	tier := FontScaleName(FontScale()) // 夹取后的真实档位，保证勾选与实际一致
	a.cfg.FontScale = tier

	a.fyneApp.Settings().SetTheme(ActiveTheme(a.cfg.Theme).Theme())
	ApplyFontScale(a.fyneApp, old)

	// 菜单里的 ✓ 要跟着挪（顺便刷新「显示高级参数」的勾选态）
	a.refreshMainMenu()
	// 记谱面板的序号列是固定宽度栅格，换档后重建一次才不会切字
	if a.moveBox != nil {
		a.rebuildMoveList()
	}
	a.SaveConfig()

	msg := "字号已切换为：" + describeFontScale()
	a.toast(msg)
	// 提示文案里不要用箭头符号：实测本机字体渲染 U+2192 会出空心方框（缺字形），
	// 对老年用户更是一串看不懂的符号。一律用中文说清楚在哪改。
	a.setStatusInfo(msg + "（在「设置」菜单的「字号」里可以再改）")
}

// ShowQuickStart 是给第一次上手的人（尤其是不熟悉象棋软件的老年用户）看的
// 「三步上手」说明。为什么不做成让他去读 README：README 有 600 多行，
// 老人打开就关了；这里只讲三件事——选引擎、摆局面、看结果。
func (a *App) ShowQuickStart() {
	const md = `# 快速上手

## 一、选择引擎

菜单「**引擎**」里的「**引擎管理**」，确认列表里有引擎（本软件已内置皮卡鱼）。
回到主界面，在工具栏「己方引擎」下拉框里选中它。
**状态栏出现「引擎：……已完成」即表示就绪。**

## 二、三种模式

| 模式 | 用途 |
|---|---|
| **分析模式** | 拆棋。你在棋盘上走子，或把棋谱粘到下面的「粘贴棋谱」，引擎立即给出应对 |
| **人机对弈** | 你和引擎下。点这个模式后直接用鼠标把棋子拖到目标格 |
| **引擎对战** | 两个引擎互下。点这个模式**立刻开始**，暂停 / 终止在菜单「对局」里 |

## 三、看引擎的结论

- 右侧最大的那行中文（例如「炮二进二」）就是引擎推荐的一步。
- 它上面那条横条是**局势**：红色越长，红方越好。
- 点「复制最佳着法（UCI）」，这一步的坐标就复制走了，可以粘到别的软件里。
- 想看深度、节点、主变例这些，点「**思考细节**」。

---

## 字太小看不清？

菜单「**设置**」里的「**字号**」有三档：**标准 / 大 / 特大**。点一下立刻生效，不用重启。

## 找不到功能？

菜单栏从左到右是：**引擎 / 局面 / 对局 / 棋谱 / 设置 / 帮助**。
界面上只留最常用的按钮；引擎参数在「**引擎设置**」里（平时不用管它）。

## 键盘快捷键

| 键 | 作用 |
|---|---|
| F | 交换行棋方 |
| E | 开 / 关引擎 |
| N | 新局面 |
| Z | 悔棋 |
| 左右方向键 | 前进 / 后退一步 |
| 上下方向键 | 开局 / 终局 |`

	body := container.NewVScroll(widget.NewRichTextFromMarkdown(md))
	body.SetMinSize(fyne.NewSize(600, 430))
	dialog.ShowCustom("新手三步上手", "知道了", body, a.win)
}

// openDoc 用系统默认程序打开工程内的文档。
func (a *App) openDoc(rel string) {
	p := a.baseDir + "\\" + rel
	if !fileExists(p) {
		dialog.ShowInformation("找不到文档", p, a.win)
		return
	}
	_ = exec.Command("cmd", "/c", "start", "", p).Start()
}

// ---------------------------------------------------------------------------
// 界面开关
// ---------------------------------------------------------------------------

// 棋盘大小档位：直接换算成左栏占比（棋盘控件本身始终等比填满左栏）。
// 【v1.6】整组右移，配合「右栏收窄」：大 0.68 / 中 0.58 / 小 0.46。
const (
	boardScaleLarge  = 0.68
	boardScaleMedium = 0.58
	boardScaleSmall  = 0.46
)

// setBoardScale 设置棋盘大小档位（菜单「设置 → 棋盘：大 / 中 / 小」）。
//
// 【v1.5 简化】原来这里还要同步一个界面滑块；滑块已去掉，
// 现在只改分栏比例——棋盘控件始终等比填满左栏，所以棋子/格线会跟着一起缩放。
func (a *App) setBoardScale(v float64) {
	a.cfg.MainSplit = v
	if a.mainSplit != nil {
		a.mainSplit.SetOffset(v)
	}
	a.SaveConfig()
	a.setStatusInfo(fmt.Sprintf("棋盘大小已切换：%s", boardScaleName(v)))
}

// boardScaleName 把分栏比例反查成菜单里的档位名（状态栏提示用）。
func boardScaleName(v float64) string {
	switch {
	case v >= boardScaleLarge-0.001:
		return "大"
	case v <= boardScaleSmall+0.001:
		return "小"
	default:
		return "中"
	}
}

// toggleCoords 切换棋盘四周坐标标注。
func (a *App) toggleCoords() {
	a.cfg.ShowCoords = !a.cfg.ShowCoords
	a.board.SetShowCoords(a.cfg.ShowCoords)
	a.SaveConfig()
	// 【缺陷修复】菜单项带 ✓，而 Fyne 的菜单是静态的：不重建菜单，
	// 勾就停在旧状态上（用户会以为没生效）。下同。
	a.refreshMainMenu()
}

// toggleShowLines 切换「显示行棋线路」（选中棋子时点亮它所在的横线/竖线）。
//
// 【v1.6.9 / 头脑风暴第 18 条】与「显示棋盘坐标」分开成两个开关——
// 同类软件（TCHESS）里这是两件事：坐标是刻度标注，线路是看棋的辅助线。
func (a *App) toggleShowLines() {
	a.cfg.ShowLines = !a.cfg.ShowLines
	a.board.SetShowLines(a.cfg.ShowLines)
	a.SaveConfig()
	a.refreshMainMenu()
	a.setStatusInfo(map[bool]string{true: "已显示行棋线路（选中棋子时点亮所在横线与竖线）", false: "已关闭行棋线路"}[a.cfg.ShowLines])
}

// toggleAlwaysOnTop 切换窗口置顶（v1.6.8，参照 TCHESS 设置菜单里的同名项）。
func (a *App) toggleAlwaysOnTop() {
	a.cfg.AlwaysOnTop = !a.cfg.AlwaysOnTop
	if SetAlwaysOnTop(a.cfg.AlwaysOnTop) == 0 {
		a.setStatusInfo("置顶失败：没找到本软件的窗口句柄")
	}
	a.SaveConfig()
	a.refreshMainMenu()
	a.setStatusInfo(map[bool]string{true: "窗口已置顶（不会被其它窗口盖住）", false: "已取消置顶"}[a.cfg.AlwaysOnTop])
}

// thinkProfileName 把内部模式名翻成界面用词。
func thinkProfileName(mode string) string {
	if mode == "human" {
		return "人机对弈"
	}
	return "分析模式"
}

// switchThinkProfile 切模式时把该模式自己的思考设置套上去（v1.6.8 / 头脑风暴第 2 条）。
func (a *App) switchThinkProfile(prev, next string) {
	a.saveThinkProfile(prev)
	if next == "match" || prev == next || a.cfg == nil {
		return
	}
	p := a.cfg.ThinkBridge
	if next == "human" {
		p = a.cfg.ThinkHuman
	}
	a.cfg.TimeMode, a.cfg.MoveTimeMS, a.cfg.Depth = p.TimeMode, p.MoveTimeMS, p.Depth
	if a.think != nil {
		a.think.SetProfileName(thinkProfileName(next))
		a.think.SetConfig(a.cfg.TimeMode, a.cfg.MoveTimeMS, a.cfg.Depth)
	}
}

// saveThinkProfile 把当前生效的思考设置存回它所属模式的那一格。
func (a *App) saveThinkProfile(mode string) {
	if a.cfg == nil {
		return
	}
	p := config.ThinkProfile{TimeMode: a.cfg.TimeMode, MoveTimeMS: a.cfg.MoveTimeMS, Depth: a.cfg.Depth}
	switch mode {
	case "human":
		a.cfg.ThinkHuman = p
	case "match":
		// 对战有自己的 Match* 设置，不参与
	default:
		a.cfg.ThinkBridge = p
	}
}

// ForceEngineMove 「立即出招」：人机对弈时不等满思考时间，让引擎马上走一步。
//
// 参照同类软件（象棋巫师的「立即走棋」/ TCHESS 的立即出招）：只有在轮到电脑时才有意义。
func (a *App) ForceEngineMove() {
	if a.curMode != "human" {
		a.toast("「立即出招」只在人机对弈模式下有效")
		return
	}
	if a.isHumanTurn() {
		a.toast("现在轮到你走棋")
		return
	}
	a.quickMove = true
	a.setStatusInfo("立即出招：引擎马上给出当前最好的一步")
	a.requestAnalysis()
}

// themeItem 是「设置 → 主题」里的一项：勾选当前皮肤，点一下立刻换肤。
//
// 换肤不只是换 fyne 主题：调色板、背景底纹、棋盘皮肤要一起换（见 applyThemeName），
// 否则会出现「按钮变了、棋盘还是旧配色」的半截效果。
func (a *App) themeItem(name string) *fyne.MenuItem {
	desc := ""
	if p, ok := themeRegistry[name]; ok {
		desc = p.Description()
	}
	label := "主题：" + ThemeLabel(name)
	it := fyne.NewMenuItem(label, func() {
		if a.cfg.Theme == name {
			return
		}
		a.applyThemeName(name)
		a.SaveConfig()
		a.refreshMainMenu()
		a.setStatusInfo("已切换主题：" + ThemeLabel(name) + "（" + desc + "）")
	})
	it.Checked = a.cfg.Theme == name
	return it
}

// setBackgroundStyle 切换背景画法（主题底纹 / 纯色 / 自定义图片）。
func (a *App) setBackgroundStyle(style string) {
	if a.cfg.BackgroundStyle == style {
		return
	}
	a.cfg.BackgroundStyle = style
	a.rebuildBackground()
	if a.rootStack != nil {
		a.rootStack.Objects[0] = a.bgSolid
		a.rootStack.Objects[1] = a.bgMid
		a.rootStack.Objects[2] = a.bgOverlay
		a.rootStack.Refresh()
	}
	a.SaveConfig()
	a.refreshMainMenu()
	a.setStatusInfo(map[string]string{
		config.BgStyleTexture: "背景已切换为：主题自带底纹",
		config.BgStyleSolid:   "背景已切换为：纯色",
		config.BgStyleImage:   "背景已切换为：自定义图片（" + a.cfg.BackgroundImage + "）",
	}[style])
}

// toggleSound 切换走棋音效。
func (a *App) toggleSound() {
	a.cfg.SoundOn = !a.cfg.SoundOn
	setSoundEnabled(a.cfg.SoundOn)
	a.SaveConfig()
	a.refreshMainMenu()
}

// kibStateItem 是「参考引擎：已开启 / 已关闭」的状态菜单项（点它打开设置）。
//
// 用户要求「参考引擎的界面要显示什么时候开着、什么时候关着」：
// 菜单里一眼可见，右侧对比卡的卡头也同步显示（见 KibitzerPanel.SetIdle 的标题）。
func (a *App) kibStateItem() *fyne.MenuItem {
	label := "参考引擎：已关闭"
	if a.cfg.KibitzerEngine != "" {
		name := a.cfg.KibitzerEngine
		if e := a.Reg().Find(a.cfg.KibitzerEngine); e != nil {
			name = e.Name
		}
		label = "参考引擎：已开启（" + name + "）"
	}
	if a.cfg.MaxStrength {
		label += "　—　最强引擎模式下已停用"
	}
	it := fyne.NewMenuItem(label, func() { a.ShowKibitzerSettings() })
	return it
}

// maxStrengthThreadCap 是满配档的线程上限。
//
// 【实测调参】原来是「逻辑核 - 1」（本机 16 核 → 15 线程）。实测 15 线程只跑到
// 1318%（≈13.2 核），离名义 1500% 差一大截：多出来的线程抢的是同样的功耗与睿频
// 预算，还额外产热。降到 12 线程后留出 4 个逻辑核给界面与系统，占用反而更接近
// 名义值，界面也更稳（用户拍板：降到 12）。
const maxStrengthThreadCap = 12

// maxReservedThreads 是「留给界面/系统」的逻辑核上限。
// 小机器按比例留（每 4 个核留 1 个），大机器没必要把前 20 个核都空着。
const maxReservedThreads = 4

// MaxStrengthThreads 返回满配档该用多少线程：留出足够核给界面，但不超过上限。
//
// 留核规则：每 4 个逻辑核留 1 个（16 → 留 4 用 12；8 → 留 2 用 6；4 → 留 1 用 3），
// 最多留 maxReservedThreads 个，且至少留 1 个（绝不把整机占满）。
func MaxStrengthThreads(logical int) int {
	reserve := logical / 4
	if reserve < 1 {
		reserve = 1
	}
	if reserve > maxReservedThreads {
		reserve = maxReservedThreads
	}
	n := logical - reserve
	if n > maxStrengthThreadCap {
		n = maxStrengthThreadCap
	}
	if n < 1 {
		n = 1
	}
	return n
}

// ToggleMaxStrength 开关「最强引擎模式」：单引擎满配。
//
// 满配 = 线程 MaxStrengthThreads(逻辑核) / 哈希 4096 / MultiPV 1。
// 与参考引擎**互斥**：满配的前提是独占用核，再挂一个参考引擎必然线程超订
// （本机 16 逻辑核，12+2 也挤），两个都会变慢——所以这里直接把参考引擎关掉，
// 并在状态栏说明原因（用户要求）。
func (a *App) ToggleMaxStrength() {
	if !a.cfg.MaxStrength {
		// 记下当前值，供一键退回
		a.cfg.MaxStrengthBackup.Threads = a.cfg.Threads
		a.cfg.MaxStrengthBackup.Hash = a.cfg.Hash
		a.cfg.MaxStrengthBackup.MultiPV = a.cfg.MultiPV

		cpu := config.CPUCount()
		threads := MaxStrengthThreads(cpu)
		a.cfg.Threads = threads
		a.cfg.Hash = 4096
		a.cfg.MultiPV = 1
		a.cfg.MaxStrength = true

		// 互斥：停掉参考引擎。
		// ⚠ 这里只调 stopKibitzer + 自己写卡头，**不要再调 startKibitzer()**——
		// 它在「未选引擎」分支会把卡头改回通用的「已关闭」提示，把下面这句
		// 说明原因的话覆盖掉（第一版就是这么写的，顺序错了）。
		hadKib := a.cfg.KibitzerEngine != ""
		a.cfg.KibitzerEngine = ""
		a.stopKibitzer()
		if a.kib != nil {
			a.kib.SetIdle("参考引擎：已关闭（最强引擎模式）",
				"满配要求独占用核，参考引擎已停用：两个引擎同时跑会互相抢核，双方都变慢。")
		}
		if a.params != nil {
			a.params.SetValues(a.cfg.Threads, a.cfg.Hash, a.cfg.Depth, a.cfg.MoveTimeMS,
				a.cfg.MultiPV, a.cfg.Temperature, a.cfg.TimeMode == config.TimeModeDepth, a.cfg.SyzygyPath)
		}
		a.SaveConfig()
		a.refreshMainMenu()
		if a.think != nil {
			a.think.SetConfig(a.cfg.TimeMode, a.cfg.MoveTimeMS, a.cfg.Depth)
		}
		// 【缺陷修复 / 用户实测 387%】满配必须**真的送到引擎上**。
		//
		// Threads / Hash / MultiPV 都是引擎启动时才读的 setoption：只改配置值，
		// 对已经在跑的引擎一点作用都没有。用户开满配后跑基准只有 387%
		// （≈ 4 核，正是改之前的旧线程数）——"15 线程"从来没生效过。
		// 所以这里显式重启分析引擎重新下发参数，并在**新进程**上做进程层调优；
		// 顺序不能反（上一版就是在旧进程上调的，新引擎一起来就丢了）。
		go a.applyMaxStrengthToEngine()
		msg := fmt.Sprintf("已开启最强引擎模式：线程 %d（共 %d 逻辑核，留 %d 个给界面）、哈希 4096MB、候选 1 条",
			threads, cpu, cpu-threads)
		if hadKib {
			msg += "；并已停用参考引擎（满配需独占用核，同时跑会互相抢核）"
		}
		a.toast("最强引擎模式已开启")
		a.setStatusInfo(msg)
		return
	}

	// 关闭：退回省资源档
	b := a.cfg.MaxStrengthBackup
	if b.Threads > 0 {
		a.cfg.Threads = b.Threads
	}
	if b.Hash > 0 {
		a.cfg.Hash = b.Hash
	}
	if b.MultiPV > 0 {
		a.cfg.MultiPV = b.MultiPV
	}
	a.cfg.MaxStrength = false
	if a.params != nil {
		a.params.SetValues(a.cfg.Threads, a.cfg.Hash, a.cfg.Depth, a.cfg.MoveTimeMS,
			a.cfg.MultiPV, a.cfg.Temperature, a.cfg.TimeMode == config.TimeModeDepth, a.cfg.SyzygyPath)
	}
	a.SaveConfig()
	a.refreshMainMenu()
	a.toast("最强引擎模式已关闭")
	a.setStatusInfo(fmt.Sprintf("已关闭最强引擎模式，退回省资源档：线程 %d、哈希 %dMB、候选 %d 条",
		a.cfg.Threads, a.cfg.Hash, a.cfg.MultiPV))
	// 同理：退回省资源档也必须重启引擎，否则引擎还在按满配的线程/哈希跑
	// （"关掉满配 CPU 还是满的"是同一类缺陷的镜像）。
	go a.applyMaxStrengthToEngine()
}

// ShowKibitzerSettings 打开「参考引擎（Kibitzer）」设置对话框。
//
// 设置排布照同类产品（见 docs/多引擎对比-设计稿.md）：**每个引擎一套参数 +
// 一层可选的公共默认值**；本软件里主引擎那套就是「引擎设置」，这里只管参考引擎，
// 并提供「线程/哈希跟随主引擎」的勾选（Arena 的 UCI 全局 Tab 同款思路）。
// 时间语义按调研只做「同步每步时限」：一个输入框管两个引擎。
func (a *App) ShowKibitzerSettings() {
	labelToID := map[string]string{}
	labels := []string{"（不启用）"}
	labelToID["（不启用）"] = ""
	for _, e := range a.Reg().List() {
		lbl := e.Name
		if lbl == "" {
			lbl = e.Path
		}
		labels = append(labels, lbl)
		labelToID[lbl] = e.ID
	}
	cur := "（不启用）"
	for lbl, id := range labelToID {
		if id != "" && id == a.cfg.KibitzerEngine {
			cur = lbl
		}
	}

	sel := widget.NewSelect(labels, nil)
	sel.SetSelected(cur)

	follow := widget.NewCheck("线程 / 哈希跟随主引擎（推荐）", nil)
	follow.SetChecked(a.cfg.KibitzerFollowMain)

	threads := widget.NewSlider(1, 16)
	threads.Step = 1
	threads.Value = float64(a.cfg.KibitzerThreads)
	threadsVal := canvas.NewText(fmt.Sprintf("%d 线程", a.cfg.KibitzerThreads), colPrimary)
	hash := widget.NewSlider(64, 8192)
	hash.Step = 64
	hash.Value = float64(a.cfg.KibitzerHash)
	hashVal := canvas.NewText(fmt.Sprintf("%d MB", a.cfg.KibitzerHash), colPrimary)
	threads.OnChanged = func(v float64) {
		threadsVal.Text = fmt.Sprintf("%.0f 线程", v)
		threadsVal.Refresh()
	}
	hash.OnChanged = func(v float64) {
		hashVal.Text = fmt.Sprintf("%.0f MB", v)
		hashVal.Refresh()
	}

	syncVal := canvas.NewText("", colPrimary)
	syncEntry := widget.NewEntry()
	syncEntry.SetText(fmt.Sprintf("%.1f", float64(a.cfg.KibitzerSyncMS)/1000))
	syncVal.Text = "秒（两个引擎各算这么久）"

	save := func() {
		prev := a.cfg.KibitzerEngine
		a.cfg.KibitzerEngine = labelToID[sel.Selected]
		a.cfg.KibitzerFollowMain = follow.Checked
		a.cfg.KibitzerThreads = int(threads.Value + 0.5)
		a.cfg.KibitzerHash = int(hash.Value + 0.5)
		if sec, err := strconv.ParseFloat(strings.TrimSpace(syncEntry.Text), 64); err == nil && sec > 0 {
			a.cfg.KibitzerSyncMS = int(sec*1000 + 0.5)
		}
		a.SaveConfig()
		if a.cfg.KibitzerEngine != prev || !follow.Checked {
			a.startKibitzer() // 换引擎 / 改参数都重启一次客户端
		}
		a.setStatusInfo("参考引擎设置已保存" + map[bool]string{true: "（已启用：" + sel.Selected + "）", false: "（已关闭）"}[a.cfg.KibitzerEngine != ""])
	}

	body := container.NewVBox(
		smallText("参考引擎与主引擎**同时**分析同一局面，结果上下排布便于对比。\n"+
			"两个引擎共享 CPU：同类产品都提示「每多挂一个引擎，分析质量都会下降」，\n"+
			"建议线程各设「核数 ÷ 引擎数」，或改用单引擎的 Multi-PV（变招条数）。"),
		widget.NewSeparator(),
		container.NewBorder(nil, nil, canvas.NewText("参考引擎", colFore), nil, sel),
		follow,
		container.NewBorder(nil, nil, canvas.NewText("参考引擎线程", colFore), threadsVal, threads),
		container.NewBorder(nil, nil, canvas.NewText("参考引擎哈希", colFore), hashVal, hash),
		widget.NewSeparator(),
		container.NewBorder(nil, nil, canvas.NewText("同步每步时限", colFore), syncVal, syncEntry),
		smallText("时间语义只做这一种（同类产品同款）：一个值同时管两个引擎，各自算完就交招。"),
		container.NewHBox(widget.NewButton("保存", func() { save() })),
	)
	dialog.ShowCustom("参考引擎（Kibitzer）", "关闭", body, a.win)
}

// newThinkSaveButton 造「保存并立即生效」按钮，并把它记到 a.thinkSaveBtn，
// 供自动化验证直接触发（XQ_THINSET 钩子走的就是这条真实路径）。
func (a *App) newThinkSaveButton(apply func(bool)) *widget.Button {
	a.thinkSaveBtn = widget.NewButton("保存并立即生效", func() {
		if os.Getenv("XQ_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[think] 点击保存：输入框=%q\n", a.thinkDepth.Text)
		}
		apply(true)
		mode := "每步限时"
		detail := fmt.Sprintf("%.1f 秒（上限 %d 层）", float64(a.cfg.MoveTimeMS)/1000, a.cfg.Depth)
		if a.cfg.TimeMode == config.TimeModeDepth {
			mode = "固定深度"
			detail = fmt.Sprintf("%d 层", a.cfg.Depth)
		}
		a.setStatusInfo("思考设置已保存：" + mode + " " + detail)
	})
	return a.thinkSaveBtn
}

// ShowThinkSettings 打开「思考设置」对话框：电脑每步想多久、算多深。
//
// 为什么单独做一个（而不是只留「引擎设置」）：用户明确要求
// 「不管人机对弈还是引擎对战，都要能改电脑的思考时间和计算层数，
// 而且要在右侧栏能看到、能点开调」。线程/哈希属于引擎调优，
// 日常只需要「时间」和「层数」这两个值，所以给一个一眼能看懂的对话框。
func (a *App) ShowThinkSettings() {
	mode := widget.NewRadioGroup([]string{"每步限时", "固定深度", "无限分析（仅分析模式）"}, nil)
	a.thinkMode = mode
	mode.Horizontal = true
	switch a.cfg.TimeMode {
	case config.TimeModeDepth:
		mode.SetSelected("固定深度")
	case config.TimeModeInfinite:
		mode.SetSelected("无限分析（仅分析模式）")
	default:
		mode.SetSelected("每步限时")
	}

	timeVal := canvas.NewText("", colFore)
	timeVal.TextSize = textSize(textLabel)
	timeSlider := widget.NewSlider(0.1, 30)
	timeSlider.Step = 0.1
	timeSlider.Value = float64(a.cfg.MoveTimeMS) / 1000
	a.thinkTime = timeSlider

	// 【v1.6.7】深度用输入框：用户要求「固定深度无上限」。
	// 滑块必须有最大值，输入框才能真正不设限（引擎自己会按能力与时间停）。
	depthVal := canvas.NewText("", colFore)
	depthVal.TextSize = textSize(textLabel)
	depthEntry := widget.NewEntry()
	a.thinkDepth = depthEntry
	depthEntry.SetPlaceHolder("层数，例如 60")
	depthEntry.SetText(fmt.Sprintf("%d", a.cfg.Depth))
	depthEntry.Validator = func(sv string) error {
		if sv == "" {
			return nil
		}
		n, err := strconv.Atoi(sv)
		if err != nil || n < 1 {
			return fmt.Errorf("请填 1 以上的整数层数")
		}
		return nil
	}

	// 只在**松手**时触发重新分析：拖动过程中每一格都重开一次搜索纯属浪费
	depthOf := func() int {
		if n, err := strconv.Atoi(strings.TrimSpace(depthEntry.Text)); err == nil && n >= 1 {
			return n
		}
		return a.cfg.Depth
	}
	apply := func(reanalyze bool) {
		a.cfg.MoveTimeMS = int(timeSlider.Value*1000 + 0.5)
		a.cfg.Depth = depthOf()
		if os.Getenv("XQ_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[think] apply: 输入框=%q 解析后depth=%d timeMode=%q reanalyze=%v\n",
				depthEntry.Text, a.cfg.Depth, mode.Selected, reanalyze)
		}
		switch mode.Selected {
		case "固定深度":
			a.cfg.TimeMode = config.TimeModeDepth
		case "无限分析（仅分析模式）":
			a.cfg.TimeMode = config.TimeModeInfinite
		default:
			a.cfg.TimeMode = config.TimeModeMoveTime
		}
		// ⚠ 顺序要紧：**先**把新值同步进「引擎设置」面板，**再** SaveConfig。
		// 原因：SaveConfig 会读面板里滑块/输入框的当前值落盘（这是防丢改动的设计），
		// 反过来先保存的话，面板里的旧层数/旧时间会立刻把这里刚改的值覆盖回去——
		// 表现就是「改了固定思考层数但没生效」（用户实测踩到）。
		if a.params != nil {
			a.params.SetValues(a.cfg.Threads, a.cfg.Hash, a.cfg.Depth, a.cfg.MoveTimeMS,
				a.cfg.MultiPV, a.cfg.Temperature, a.cfg.TimeMode == config.TimeModeDepth, a.cfg.SyzygyPath)
		}
		a.SaveConfig()
		if a.think != nil {
			a.think.SetConfig(a.cfg.TimeMode, a.cfg.MoveTimeMS, a.cfg.Depth)
		}
		if reanalyze {
			a.requestAnalysis()
		}
	}
	// setEnabled 把当前**不生效**的那个控件置灰：单选在「每步限时」时层数输入框置灰，
	// 反之时间滑块置灰。这样"我改的这个到底算不算数"一眼可见。
	setEnabled := func() {
		switch mode.Selected {
		case "固定深度":
			timeSlider.Disable()
			depthEntry.Enable()
		case "无限分析（仅分析模式）":
			// 无限分析只由「停止」结束，时间和层数都不参与，两个都置灰才诚实
			timeSlider.Disable()
			depthEntry.Disable()
		default:
			timeSlider.Enable()
			depthEntry.Disable()
		}
	}
	refresh := func() {
		timeVal.Text = fmt.Sprintf("%.1f 秒", timeSlider.Value)
		depthVal.Text = fmt.Sprintf("%d 层", depthOf())
		timeVal.Refresh()
		depthVal.Refresh()
	}
	timeSlider.OnChanged = func(float64) {
		if mode.Selected != "每步限时" {
			mode.SetSelected("每步限时")
		}
		refresh()
		setEnabled()
		apply(false)
	}
	timeSlider.OnChangeEnded = func(float64) { apply(true) }
	// 【缺陷修复】用户实测「改了固定思考层数没生效」的真实原因：
	// 上面那个「每步限时 / 固定深度」是单选，只填层数而不把单选切过去时，
	// 生效的仍是时间限时——看起来就是改了没用。现在**改哪个就自动切到哪个**，
	// 并把当前不生效的控件置灰，界面上再也造不出"改了不生效"的状态。
	depthEntry.OnChanged = func(string) {
		if mode.Selected != "固定深度" {
			mode.SetSelected("固定深度")
		}
		refresh()
		setEnabled()
		apply(false)
	}
	depthEntry.OnSubmitted = func(string) { apply(true) }
	mode.OnChanged = func(string) { refresh(); setEnabled(); apply(true) }
	refresh()
	setEnabled()

	body := container.NewVBox(
		smallText("电脑每步思考多久、算多深。改完立即生效：人机对弈下一步、引擎对战下一局就用新设置。"),
		widget.NewSeparator(),
		mode,
		container.NewBorder(nil, nil, canvas.NewText("每步限时", colFore), timeVal, timeSlider),
		container.NewBorder(nil, nil, canvas.NewText("固定深度", colFore), depthVal, depthEntry),
		smallText("上面选「每步限时」就按时间算，选「固定深度」就按层数算；\n选「无限分析」则一直算到你按停止（跑满配、看深度用，只在**分析模式**生效）。\n注意：人机对弈里电脑必须有一步可走，所以无限分析会按「每步限时」执行，不会把棋盘锁住。"),
		container.NewHBox(
			a.newThinkSaveButton(apply),
		),
	)
	dialog.ShowCustom("思考设置", "关闭", body, a.win)
}

// ShowEngineParams 打开「引擎设置」对话框。
//
// 【v1.5 简化】参数面板原来占着主界面底部的一整个页签，和棋盘同屏抢注意力；
// 现在整体搬进这个对话框——想调的人找得到（引擎 / 设置两个菜单里都有），
// 不想调的人永远不会被它打扰。控件本身还是同一个 ParamPanel，
// 所以滑块取值、立即生效、落盘这些行为一个都没变。
func (a *App) ShowEngineParams() {
	if a.params == nil {
		return
	}
	// 参数面板自己就是一个 VScroll，直接把它的最小尺寸放大到对话框合适的大小，
	// 然后整个交给对话框——不要再套一层 VBox，否则内容只占顶上一条、下面全空。
	if sc, ok := a.params.Root.(*container.Scroll); ok {
		sc.SetMinSize(fyne.NewSize(sz(480), sz(300)))
	}
	dialog.ShowCustom("引擎设置（改完立即生效）", "关闭", a.params.Root, a.win)
}

// toggleAdvancedParams 展开 / 收起参数面板的高级区。
//
// 【v1.5 简化】参数面板已搬进「引擎参数…」对话框，菜单里不再有这一项；
// 保留函数是因为对话框里的「显示高级参数」按钮走的是同一段逻辑
// （btnAdvanced.OnTapped），将来要加菜单项也能直接复用。
func (a *App) toggleAdvancedParams() {
	if a.params != nil && a.params.btnAdvanced != nil {
		a.params.btnAdvanced.OnTapped()
	}
}

// ---------------------------------------------------------------------------
// FEN 复制 / 粘贴（TCHESS 的「局面」菜单里也有这一对）
// ---------------------------------------------------------------------------

func (a *App) copyFEN() {
	fen := a.game.Board.FEN()
	a.win.Clipboard().SetContent(fen)
	a.toast("已复制当前局面 FEN")
	a.setStatusInfo("已复制局面 FEN：" + fen)
}

func (a *App) pasteFEN() {
	clip := a.win.Clipboard().Content()
	if clip == "" {
		dialog.ShowInformation("剪贴板为空", "剪贴板里没有内容。先复制一段 FEN 再试。", a.win)
		return
	}
	b, err := rules.ParseFEN(clip)
	if err != nil {
		// 也允许粘贴整行 "position fen ... moves ..." 或纯着法序列
		a.applySequence(clip, true)
		return
	}
	g, err := rules.NewGameFromFEN(b.FEN())
	if err != nil {
		dialog.ShowError(fmt.Errorf("这段 FEN 无法解析：\n%w", err), a.win)
		return
	}
	a.game = g
	a.viewing = -1
	a.viewGame = nil
	a.board.SetHint(-1, -1)
	a.board.SetGame(a.game)
	a.board.SetLastMove(-1, -1)
	a.afterEdit() // 复用「局面被整体替换」的收尾：清历史、刷新记谱、重新分析
	a.setStatusInfo("已从剪贴板载入局面")
}

// ---------------------------------------------------------------------------
// 快捷键（对齐 swiftxiangqi 的一套）
// ---------------------------------------------------------------------------

// installShortcuts 注册键盘快捷键。
func (a *App) installShortcuts() {
	c := a.win.Canvas()
	// 单键：不走菜单栏加速键，而是在画布上监听（Fyne 的菜单快捷键在无菜单栏窗口里不生效）
	c.SetOnTypedKey(func(ev *fyne.KeyEvent) {
		switch ev.Name {
		case fyne.KeyEscape:
			a.board.ClearSelection()
		case fyne.KeyF5:
			a.requestAnalysis()
		case fyne.KeyF:
			a.SwapSides() // 翻转
		case fyne.KeyE:
			a.ToggleEngine() // 引擎开关
		case fyne.KeyN:
			a.resetGame() // 新局
		case fyne.KeyZ:
			a.undo(1) // 悔棋
		case fyne.KeyLeft:
			a.jumpPrev() // 上一步
		case fyne.KeyRight:
			a.jumpNext() // 下一步
		case fyne.KeyUp:
			a.jumpTo(0) // 回到开局
		case fyne.KeyDown:
			a.jumpTo(-1) // 回到当前
		}
	})
}

// jumpPrev 查看上一步之后的局面。
func (a *App) jumpPrev() {
	cur := len(a.game.Moves)
	if a.viewing >= 0 {
		cur = a.viewing
	}
	if cur <= 0 {
		return
	}
	a.jumpTo(cur - 1)
}

// jumpNext 查看下一步之后的局面；已经在最后就回到当前局面。
func (a *App) jumpNext() {
	cur := len(a.game.Moves)
	if a.viewing >= 0 {
		cur = a.viewing
	}
	if cur >= len(a.game.Moves) {
		a.jumpTo(-1)
		return
	}
	a.jumpTo(cur + 1)
}

// copyPGN 把当前着法历史导出成 PGN 复制到剪贴板。
//
// 参照 swiftxiangqi：PGN 导入导出是同类软件的标配（对局存档、发给别人复盘都用它）。
// 这里只导出「当前局面」这一份，内容与自动对战写入 matches\ 的 PGN 同格式。
func (a *App) copyPGN() {
	if len(a.game.Moves) == 0 {
		a.toast("还没有着法，无法导出棋谱")
		return
	}
	var sb strings.Builder
	sb.WriteString("[Event \"象棋强软 手工对局\"]\r\n")
	sb.WriteString("[Site \"本机 (Windows)\"]\r\n")
	sb.WriteString("[Date \"" + time.Now().Format("2006.01.02") + "\"]\r\n")
	sb.WriteString("[Red \"红方\"]\r\n[Black \"黑方\"]\r\n")
	sb.WriteString("[Result \"*\"]\r\n")
	sb.WriteString("[FEN \"" + a.game.StartFEN + "\"]\r\n")
	sb.WriteString("[TimeControl \"每步 " + fmt.Sprintf("%.1f", float64(a.cfg.MoveTimeMS)/1000) + " 秒\"]\r\n\r\n")

	// 着法体：一行一回合，红黑并列；同时附上中文记谱作为注释
	for i := 0; i < len(a.game.Moves); i += 2 {
		fmt.Fprintf(&sb, "%d. ", i/2+1)
		sb.WriteString(a.game.Moves[i].String())
		sb.WriteString(" {")
		sb.WriteString(notation.ToChinese(a.game.BoardAt(i), a.game.Moves[i]))
		sb.WriteString("} ")
		if i+1 < len(a.game.Moves) {
			sb.WriteString(a.game.Moves[i+1].String())
			sb.WriteString(" {")
			sb.WriteString(notation.ToChinese(a.game.BoardAt(i+1), a.game.Moves[i+1]))
			sb.WriteString("} ")
		}
	}
	sb.WriteString("*\r\n")

	a.win.Clipboard().SetContent(sb.String())
	a.toast("已复制 PGN 棋谱到剪贴板")
	a.setStatusInfo(fmt.Sprintf("已复制 PGN（%d 着）", len(a.game.Moves)))
}
