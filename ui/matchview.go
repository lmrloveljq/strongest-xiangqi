package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"xiangqi/analytics"
	"xiangqi/config"
	"xiangqi/engine"
	"xiangqi/match"
	"xiangqi/notation"
	"xiangqi/rules"
)

// MatchView 是「核心功能 B：引擎自动对战」的界面。
//
// 它复用棋盘组件显示实时对局，复用曲线组件显示双方胜率，
// 并把 match.Runner 推送的事件流翻译成界面更新。
type MatchView struct {
	app       *App
	Root      fyne.CanvasObject
	BottomBar fyne.CanvasObject

	selfSelect  *widget.Select
	oppSelect   *widget.Select
	gamesSlider *widget.Slider
	gamesVal    *canvas.Text
	timeMode    *widget.RadioGroup
	moveTime    *widget.Slider
	moveVal     *canvas.Text
	gameTime    *widget.Slider
	gameVal     *canvas.Text
	depthS      *widget.Entry // 【v1.6.7】深度不设上限：输入框而不是滑块
	depthVal    *canvas.Text
	// 【双方独立】对手一套自己的每步限时 / 每局总时间 / 固定深度
	oppMoveTime *widget.Slider
	oppMoveVal  *canvas.Text
	oppGameTime *widget.Slider
	oppGameVal  *canvas.Text
	oppDepthS   *widget.Entry
	oppDepthVal *canvas.Text
	altColors   *widget.Check

	// 【R5】对战线程分配控件
	selfThreads    *widget.Slider
	oppThreads     *widget.Slider
	selfThreadsVal *canvas.Text
	oppThreadsVal  *canvas.Text
	// threadWarn 是「线程超订」警告区。
	//
	// 注意：这里刻意用「每行一个 canvas.Text」的 VBox，而不是一个带 \n 的长字符串。
	// Fyne 的 canvas.Text.MinSize() 会把带换行的整段文本当成**一整行**来量宽，
	// 一段上百字的警告会因此报出约 1200 逻辑像素的最小宽度，Fyne 便会强行把
	// 窗口撑到比屏幕还宽（本版实测：窗口被撑到 2371 物理像素，远超 1920 屏幕）。
	// 拆成逐行 canvas.Text 后，最小宽度只取决于最长的那一行。
	threadWarnBox   *fyne.Container
	threadWarnLines []*canvas.Text

	// 【v1.5 简化】开始/暂停/终止按钮与对局日志面板已去掉：
	// 操作走菜单「对局」，日志走主窗口状态栏。
	scoreText    *canvas.Text
	settingsLine *canvas.Text // 「电脑思考：己方 … 对手 …」一行
	redInfo      *canvas.Text // 红方最近一步的引擎实况
	blackInfo    *canvas.Text // 黑方最近一步的引擎实况
	progressText *canvas.Text
	stateText    *canvas.Text

	// logRows 仍然保留事件文本（排查用），只是不再有面板显示它。
	logRows []string

	// settingsBox 是对战设置的滚动容器，放进菜单「对局 → 对局设置…」对话框。
	settingsBox *container.Scroll

	curve *Curve

	cancel  context.CancelFunc
	running bool
	paused  bool // 菜单项「暂停 / 继续」的文字依据
}

// NewMatchView 创建对战界面。
func NewMatchView(app *App) *MatchView {
	m := &MatchView{app: app}
	m.curve = NewCurve("局势图")

	cfg := app.cfg

	m.selfSelect = widget.NewSelect(app.engineLabels(), func(s string) {
		if id := app.engineIDByLabel(s); id != "" {
			app.cfg.SelfEngine = id
			app.SaveConfig()
			app.syncEngineSelects()
		}
	})
	m.oppSelect = widget.NewSelect(app.engineLabels(), func(s string) {
		if id := app.engineIDByLabel(s); id != "" {
			app.cfg.OpponentEngine = id
			app.SaveConfig()
		}
	})

	m.gamesSlider = widget.NewSlider(1, 100)
	m.gamesSlider.Step = 1
	m.gamesSlider.Value = float64(cfg.MatchGames)
	m.gamesVal = canvas.NewText("", colPrimary)
	m.gamesVal.TextSize = textSize(textLabel)
	m.gamesSlider.OnChanged = func(v float64) {
		m.gamesVal.Text = fmt.Sprintf("%.0f 局", v)
		m.gamesVal.Refresh()
		app.cfg.MatchGames = int(v + 0.5)
	}

	m.timeMode = widget.NewRadioGroup([]string{"每步限时", "每局总时间", "固定深度"}, func(s string) {
		switch s {
		case "每局总时间":
			app.cfg.MatchTimeMode = config.TimeModeGameTime
		case "固定深度":
			app.cfg.MatchTimeMode = config.TimeModeDepth
		default:
			app.cfg.MatchTimeMode = config.TimeModeMoveTime
		}
	})
	m.timeMode.Horizontal = true
	switch cfg.MatchTimeMode {
	case config.TimeModeGameTime:
		m.timeMode.SetSelected("每局总时间")
	case config.TimeModeDepth:
		m.timeMode.SetSelected("固定深度")
	default:
		m.timeMode.SetSelected("每步限时")
	}

	m.moveTime = widget.NewSlider(0.1, 60)
	m.moveTime.Step = 0.1
	m.moveTime.Value = float64(cfg.MatchMoveTimeMS) / 1000
	m.moveVal = canvas.NewText("", colPrimary)
	m.moveVal.TextSize = textSize(textLabel)
	m.moveTime.OnChanged = func(v float64) {
		m.moveVal.Text = fmt.Sprintf("%.1f 秒/步", v)
		m.moveVal.Refresh()
		app.cfg.MatchMoveTimeMS = int(v*1000 + 0.5)
	}

	m.gameTime = widget.NewSlider(10, 1800)
	m.gameTime.Step = 10
	m.gameTime.Value = float64(cfg.MatchGameTimeMS) / 1000
	m.gameVal = canvas.NewText("", colPrimary)
	m.gameVal.TextSize = textSize(textLabel)
	m.gameTime.OnChanged = func(v float64) {
		m.gameVal.Text = fmt.Sprintf("%.0f 秒/局（按 60 步均摊）", v)
		m.gameVal.Refresh()
		app.cfg.MatchGameTimeMS = int(v*1000 + 0.5)
	}

	// 【v1.6.7】对手/己方的固定深度都不设上限：用输入框
	m.depthS = widget.NewEntry()
	m.depthS.SetPlaceHolder("层数")
	m.depthS.SetText(fmt.Sprintf("%d", cfg.MatchDepth))
	m.depthVal = canvas.NewText("", colPrimary)
	m.depthVal.TextSize = textSize(textLabel)
	m.depthS.OnChanged = func(sv string) {
		if n, err := strconv.Atoi(strings.TrimSpace(sv)); err == nil && n >= 1 {
			m.depthVal.Text = fmt.Sprintf("%d 层", n)
			m.depthVal.Refresh()
			app.cfg.MatchDepth = n
		}
	}

	// ---------- 对手独立参数 ----------
	oppMoveMS := cfg.MatchOppMoveTimeMS
	if oppMoveMS == 0 {
		oppMoveMS = cfg.MatchMoveTimeMS
	}
	oppGameMS := cfg.MatchOppGameTimeMS
	if oppGameMS == 0 {
		oppGameMS = cfg.MatchGameTimeMS
	}
	oppDepth := cfg.MatchOppDepth
	if oppDepth == 0 {
		oppDepth = cfg.MatchDepth
	}

	m.oppMoveTime = widget.NewSlider(0.1, 60)
	m.oppMoveTime.Step = 0.1
	m.oppMoveTime.Value = float64(oppMoveMS) / 1000
	m.oppMoveVal = canvas.NewText("", colPrimary)
	m.oppMoveVal.TextSize = textSize(textLabel)
	m.oppMoveTime.OnChanged = func(v float64) {
		m.oppMoveVal.Text = fmt.Sprintf("%.1f 秒/步", v)
		m.oppMoveVal.Refresh()
		app.cfg.MatchOppMoveTimeMS = int(v*1000 + 0.5)
	}

	m.oppGameTime = widget.NewSlider(10, 1800)
	m.oppGameTime.Step = 10
	m.oppGameTime.Value = float64(oppGameMS) / 1000
	m.oppGameVal = canvas.NewText("", colPrimary)
	m.oppGameVal.TextSize = textSize(textLabel)
	m.oppGameTime.OnChanged = func(v float64) {
		m.oppGameVal.Text = fmt.Sprintf("%.0f 秒/局（按 60 步均摊）", v)
		m.oppGameVal.Refresh()
		app.cfg.MatchOppGameTimeMS = int(v*1000 + 0.5)
	}

	m.oppDepthS = widget.NewEntry()
	m.oppDepthS.SetPlaceHolder("层数")
	m.oppDepthS.SetText(fmt.Sprintf("%d", oppDepth))
	m.oppDepthVal = canvas.NewText("", colPrimary)
	m.oppDepthVal.TextSize = textSize(textLabel)
	m.oppDepthS.OnChanged = func(sv string) {
		if n, err := strconv.Atoi(strings.TrimSpace(sv)); err == nil && n >= 1 {
			m.oppDepthVal.Text = fmt.Sprintf("%d 层", n)
			m.oppDepthVal.Refresh()
			app.cfg.MatchOppDepth = n
		}
	}

	m.altColors = widget.NewCheck("先后手轮换（奇数局己方执红，偶数局己方执黑）", func(b bool) {
		app.cfg.AlternateColors = b
	})
	m.altColors.SetChecked(cfg.AlternateColors)

	// ---------- 【R5】对战线程分配 ----------
	//
	// 背景：v1.3.0 的默认值是「双方各占满全部核」，本机 16 逻辑处理器上
	// 双方各 16 线程 = 32 线程，超订 2 倍。实测单引擎独占约 927 万 nps，
	// 对战时每方只剩约 181 万 nps，双方合计约为单引擎峰值的 39%——线程越多越慢。
	// 现在：滑块上限 = 逻辑处理器数，超出时给醒目警告，并提供一键平均分配。
	cpu := config.CPUCount()
	m.selfThreads = widget.NewSlider(1, float64(cpu))
	m.selfThreads.Step = 1
	m.selfThreads.Value = float64(cfg.SelfThreads)
	m.oppThreads = widget.NewSlider(1, float64(cpu))
	m.oppThreads.Step = 1
	m.oppThreads.Value = float64(cfg.OppThreads)

	m.selfThreadsVal = canvas.NewText("", colPrimary)
	m.selfThreadsVal.TextSize = textSize(textLabel)
	m.oppThreadsVal = canvas.NewText("", colPrimary)
	m.oppThreadsVal.TextSize = textSize(textLabel)

	// 警告区：固定 5 行，每行一个 canvas.Text（见字段注释：不能用带 \n 的长字符串）
	for i := 0; i < 5; i++ {
		t := canvas.NewText("", colWarn)
		t.TextSize = textSize(textLabel)
		t.TextStyle = fyne.TextStyle{Bold: true}
		m.threadWarnLines = append(m.threadWarnLines, t)
	}
	m.threadWarnBox = container.NewVBox()
	for _, t := range m.threadWarnLines {
		m.threadWarnBox.Add(t)
	}

	m.selfThreads.OnChanged = func(v float64) {
		app.cfg.SelfThreads = int(v + 0.5)
		m.refreshThreadUI()
	}
	m.oppThreads.OnChanged = func(v float64) {
		app.cfg.OppThreads = int(v + 0.5)
		m.refreshThreadUI()
	}

	// 【v1.5 简化】「开始对战 / 暂停 / 终止」三个按钮已从界面取消，
	// 改到菜单「对局」里——点菜单项就直接开始，不必先找按钮再确认。
	// 状态仍然记录在 running/paused 上（见 Start/TogglePause/Stop），
	// 菜单项的文字与可用性由 App.refreshMainMenu() 重建时刷新。

	m.scoreText = canvas.NewText("比分（己方视角）：胜 0   和 0   负 0", colFore)
	m.scoreText.TextSize = textSize(textLabel)
	m.scoreText.TextStyle = fyne.TextStyle{Bold: true}
	m.progressText = canvas.NewText("尚未开始", colForeDim)
	m.progressText.TextSize = textSize(textLabel)
	m.settingsLine = canvas.NewText("", colForeDim)
	m.settingsLine.TextSize = textSize(textSmall)
	m.stateText = canvas.NewText("点工具栏「引擎对战」开始；对战中再点一次即终止", colForeDim)
	m.stateText.TextSize = textSize(textLabel)

	// 对局日志不再上屏（用户要求删掉这个面板），但事件仍留在 logRows 里：
	// 一是方便排查，二是每一条都会同步写到主窗口状态栏，用户照样看得见进度。
	m.Root = m.buildRoot()
	m.BottomBar = m.buildBottomBar()
	m.refreshValues()
	return m
}

// matchLimitLabel 把「时间控制方式 + 参数」翻成一句人话。
func matchLimitLabel(mode string, moveMS, depth int) string {
	switch mode {
	case config.TimeModeDepth:
		return fmt.Sprintf("固定深度 %d 层", depth)
	case config.TimeModeGameTime:
		return fmt.Sprintf("每局 %d 秒", moveMS/1000)
	default:
		return fmt.Sprintf("每步 %.1f 秒", float64(moveMS)/1000)
	}
}

func (m *MatchView) refreshValues() {
	m.gamesVal.Text = fmt.Sprintf("%d 局", m.app.cfg.MatchGames)
	m.moveVal.Text = fmt.Sprintf("%.1f 秒/步", float64(m.app.cfg.MatchMoveTimeMS)/1000)
	m.gameVal.Text = fmt.Sprintf("%d 秒/局（按 60 步均摊）", m.app.cfg.MatchGameTimeMS/1000)
	m.depthVal.Text = fmt.Sprintf("%d 层", m.app.cfg.MatchDepth)
	m.refreshOppValues()
	m.refreshThreadUI()
	m.refreshSettingsLine()
}

// refreshValuesFromConfig 重新把配置值灌回线程滑块（不触发回调）。
func (m *MatchView) refreshThreadSliders() {
	if m.selfThreads == nil {
		return
	}
	cpu := float64(config.CPUCount())
	m.selfThreads.Max = cpu
	m.oppThreads.Max = cpu
	selfCb, oppCb := m.selfThreads.OnChanged, m.oppThreads.OnChanged
	m.selfThreads.OnChanged, m.oppThreads.OnChanged = nil, nil
	m.selfThreads.Value = float64(m.app.cfg.SelfThreads)
	m.oppThreads.Value = float64(m.app.cfg.OppThreads)
	m.selfThreads.Refresh()
	m.oppThreads.Refresh()
	m.selfThreads.OnChanged, m.oppThreads.OnChanged = selfCb, oppCb
	m.refreshThreadUI()
}

// refreshThreadUI 刷新线程数值文本与超订警告（R5）。
func (m *MatchView) refreshThreadUI() {
	if m.threadWarnBox == nil {
		return
	}
	cpu := config.CPUCount()
	self, opp := m.app.cfg.SelfThreads, m.app.cfg.OppThreads
	m.selfThreadsVal.Text = fmt.Sprintf("%d 线程", self)
	m.oppThreadsVal.Text = fmt.Sprintf("%d 线程", opp)
	m.selfThreadsVal.Refresh()
	m.oppThreadsVal.Refresh()

	// 每行不超过约 30 个汉字：行长直接决定面板最小宽度，进而决定窗口能不能放下。
	sum := self + opp
	var lines []string
	if sum > cpu {
		lines = []string{
			fmt.Sprintf("⚠ 线程超订：己方 %d + 对手 %d = %d", self, opp, sum),
			fmt.Sprintf("超过本机 %d 个逻辑处理器（超订 %.1f 倍）", cpu, float64(sum)/float64(cpu)),
			"两个引擎会互相抢核，总吞吐不升反降",
			"（实测双方合计只有单引擎峰值的约 39%）",
			"建议点下方「按核心数自动平均分配」",
		}
	} else if sum == cpu {
		lines = []string{
			fmt.Sprintf("线程分配：己方 %d + 对手 %d = %d", self, opp, sum),
			fmt.Sprintf("正好占满本机 %d 个逻辑处理器（推荐）", cpu),
		}
	} else {
		lines = []string{
			fmt.Sprintf("线程分配：己方 %d + 对手 %d = %d", self, opp, sum),
			fmt.Sprintf("本机 %d 个逻辑处理器，留有 %d 个空闲", cpu, cpu-sum),
		}
	}
	for i, t := range m.threadWarnLines {
		if i < len(lines) {
			t.Text = lines[i]
			t.Show()
		} else {
			t.Text = ""
			t.Hide()
		}
		t.Refresh()
	}
	m.threadWarnBox.Refresh()
}

// refreshOppValues 刷新对手独立参数的显示文本。
func (m *MatchView) refreshOppValues() {
	cfg := m.app.cfg
	ms := cfg.MatchOppMoveTimeMS
	if ms == 0 {
		ms = cfg.MatchMoveTimeMS
	}
	gs := cfg.MatchOppGameTimeMS
	if gs == 0 {
		gs = cfg.MatchGameTimeMS
	}
	dp := cfg.MatchOppDepth
	if dp == 0 {
		dp = cfg.MatchDepth
	}
	if m.oppMoveVal != nil {
		m.oppMoveVal.Text = fmt.Sprintf("%.1f 秒/步", float64(ms)/1000)
		m.oppMoveVal.Refresh()
	}
	if m.oppGameVal != nil {
		m.oppGameVal.Text = fmt.Sprintf("%d 秒/局（按 60 步均摊）", gs/1000)
		m.oppGameVal.Refresh()
	}
	if m.oppDepthVal != nil {
		m.oppDepthVal.Text = fmt.Sprintf("%d 层", dp)
		m.oppDepthVal.Refresh()
	}
}

// autoSplitThreads 按核心数平均分配双方线程（R5 要求 2）。
func (m *MatchView) autoSplitThreads() {
	cpu := config.CPUCount()
	half := cpu / 2
	if half < 1 {
		half = 1
	}
	m.app.cfg.SelfThreads = half
	m.app.cfg.OppThreads = half
	m.refreshThreadSliders()
	m.app.SaveConfig()
	m.log("线程已按核心数平均分配：己方 %d、对手 %d（共 %d 个逻辑处理器）", half, half, cpu)
}

func (m *MatchView) buildRoot() fyne.CanvasObject {
	setting := container.NewVBox(
		sectionTitle("引擎配置"),
		container.NewVBox(canvas.NewText("己方引擎（默认皮卡鱼满配）", colFore), m.selfSelect),
		container.NewVBox(canvas.NewText("对手引擎（引擎库选择）", colFore), m.oppSelect),
		widget.NewSeparator(),
		sectionTitle("对战设置"),
		sliderRow("对局数 N（1~100）", m.gamesSlider, m.gamesVal),
		container.NewVBox(canvas.NewText("时间控制方式（双方共用）", colFore), m.timeMode),
		widget.NewSeparator(),
		sectionTitle("己方思考参数"),
		sliderRow("己方 每步限时", m.moveTime, m.moveVal),
		sliderRow("己方 每局总时间", m.gameTime, m.gameVal),
		entryRow("己方 固定深度（层数，不设上限）", m.depthS, m.depthVal),
		widget.NewSeparator(),
		sectionTitle("对手思考参数（可独立于己方）"),
		smallText("默认与己方相同；拖动任一滑块即表示对手单独使用这一档。"),
		sliderRow("对手 每步限时", m.oppMoveTime, m.oppMoveVal),
		sliderRow("对手 每局总时间", m.oppGameTime, m.oppGameVal),
		entryRow("对手 固定深度（层数，不设上限）", m.oppDepthS, m.oppDepthVal),
		widget.NewSeparator(),
		m.altColors,
		widget.NewSeparator(),
		sectionTitle("对战线程分配（避免超订）"),
		sliderRow("己方线程 Threads", m.selfThreads, m.selfThreadsVal),
		sliderRow("对手线程 Threads", m.oppThreads, m.oppThreadsVal),
		container.NewHBox(widget.NewButton(
			fmt.Sprintf("按核心数自动平均分配（各 %d）", maxInt(1, config.CPUCount()/2)),
			func() { m.autoSplitThreads() })),
		m.threadWarnBox,
		smallText(fmt.Sprintf("本机逻辑处理器：%d", config.CPUCount())),
		smallText("双方线程之和不应超过这个数：超订会让两个引擎"),
		smallText("互相抢核，总 nps 反而下降。"),
		widget.NewSeparator(),
		sectionTitle("说明"),
		smallText("改动的设置与参数会在**下一局**开始时重新下发；对战过程可随时在菜单「对局」里暂停或终止。"),
	)
	// 【v1.5】对战设置整体搬进菜单「对局 → 对局设置…」对话框
	// （同类软件也是这么做的：XQWizard 的双引擎配对在「电脑 → 设置参数」对话框里，
	//  TCHESS 只在右栏内联「引擎 / 线程 / 哈希」三个下拉）。
	// 主界面右栏因此只剩「实时战绩 + 局势图」，一屏看完、不用滚。
	m.settingsBox = container.NewVScroll(setting)
	m.settingsBox.SetMinSize(fyne.NewSize(280, 240))

	// 【v1.6.6】用户要求「对战里也要能改电脑的思考时间与层数，并在右侧栏看得到」：
	// 实时战绩卡里补一行当前设置 + 一个「调整…」按钮（打开对局设置）。
	// 【v1.6.7】用户采纳的头脑风暴第 6 条：对战栏显示**双方最近一步的引擎实况**
	// （深度 / 分数 / 用时 / 节点），数据来自 match.MoveRecord，每步结束刷新一次。
	m.redInfo = canvas.NewText("红方：等待第一步…", colForeDim)
	m.redInfo.TextSize = textSize(textSmall)
	m.blackInfo = canvas.NewText("黑方：等待第一步…", colForeDim)
	m.blackInfo.TextSize = textSize(textSmall)

	btnCfg := widget.NewButton("调整对局设置…", func() { m.ShowSettings() })
	btnCfg.Importance = widget.LowImportance
	settingsRow := container.NewBorder(nil, nil, nil, btnCfg, m.settingsLine)
	status := cardBox(container.NewVBox(
		sectionTitle("实时战绩"),
		m.scoreText,
		m.progressText,
		widget.NewSeparator(),
		sectionTitle("引擎实况"),
		m.redInfo,
		m.blackInfo,
		widget.NewSeparator(),
		settingsRow,
	))
	root := container.NewBorder(status, nil, nil, nil, container.NewPadded(m.curve))
	return root
}

// ShowSettings 打开「对局设置」对话框。
//
// 用同一个 settingsBox 复用控件：滑块取值、立即生效、落盘的行为与原来完全一致，
// 只是从主界面挪进了对话框。
func (m *MatchView) ShowSettings() {
	if m.settingsBox == nil || m.app == nil {
		return
	}
	dialog.ShowCustom("对局设置（改完下一局生效）", "关闭", m.settingsBox, m.app.win)
}

func (m *MatchView) buildBottomBar() fyne.CanvasObject {
	// 【v1.6.4】这一条只有「当前状态 + 对局设置…」。
	//
	// ⚠ 不要在这里再放 scoreText / progressText：它们是右栏「实时战绩」卡片里的
	// 那两个 canvas.Text，同一个控件挂在两处会被 Fyne 画两次（表现为文字重影），
	// 这是本工程踩过两次的坑。
	//
	// 「对局设置…」摆在这里是因为用户反馈「对战中不能改双方引擎参数」——
	// 其实能改（菜单里也有），但入口不够显眼。改完**下一局**生效。
	btnSettings := widget.NewButton("对局设置…", func() { m.ShowSettings() })
	return barBox(container.NewBorder(nil, nil, nil, btnSettings, m.stateText))
}

// updateEngineInfo 记录并显示某一方最近一步的引擎实况。
//
// 数据来自 match.MoveRecord（引擎自报的深度/分值/节点/耗时），
// 每走完一步刷新一次——这就是对战过程中「能看懂的引擎信息」。
func (m *MatchView) updateEngineInfo(mv match.MoveRecord) {
	line := fmt.Sprintf("%s：深度 %d 层  分数 %s  用时 %.1f 秒  节点 %s",
		map[string]string{"red": "红方", "black": "黑方"}[mv.Side],
		mv.Depth, scoreText(mv.Score), float64(mv.EngineMS)/1000, commas(mv.Nodes))
	if mv.Side == "red" {
		if m.redInfo != nil {
			m.redInfo.Text = line
			m.redInfo.Refresh()
		}
		return
	}
	if m.blackInfo != nil {
		m.blackInfo.Text = line
		m.blackInfo.Refresh()
	}
}

// refreshSettingsLine 刷新「电脑思考：己方 … 对手 …」这一行（右侧栏常显）。
func (m *MatchView) refreshSettingsLine() {
	if m.settingsLine == nil {
		return
	}
	cfg := m.app.cfg
	self := matchLimitLabel(cfg.MatchTimeMode, cfg.MatchMoveTimeMS, cfg.MatchDepth)
	opp := "同己方"
	if cfg.MatchOppMoveTimeMS != 0 || cfg.MatchOppDepth != 0 || cfg.MatchOppGameTimeMS != 0 {
		opp = matchLimitLabel(cfg.MatchTimeMode, cfg.MatchOppMoveTimeMS, cfg.MatchOppDepth)
	}
	m.settingsLine.Text = "电脑思考：己方 " + self + " / 对手 " + opp
	m.settingsLine.Refresh()
}

// threadWarnText 返回当前超订警告的全部行文本（供 XQ_DEBUG 取证打印）。
func (m *MatchView) threadWarnText() string {
	out := ""
	for _, t := range m.threadWarnLines {
		if t.Text == "" || !t.Visible() {
			continue
		}
		if out != "" {
			out += " / "
		}
		out += t.Text
	}
	return out
}

// maxInt 返回两个整数中较大的一个。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RefreshEngines 刷新两个引擎下拉框（引擎库变化时调用）。
//
// 必须静默设置选中项：否则 SetSelected 触发 OnChanged，OnChanged 又回头调用
// App.syncEngineSelects，形成无限递归。
func (m *MatchView) RefreshEngines() {
	labels := m.app.engineLabels()
	setSilently := func(sel *widget.Select, id string) {
		if sel == nil {
			return
		}
		cb := sel.OnChanged
		sel.OnChanged = nil
		sel.Options = labels
		if e := m.app.Reg().Find(id); e != nil {
			sel.SetSelected(e.Label())
		} else if len(labels) > 0 {
			sel.ClearSelected()
		}
		sel.Refresh()
		sel.OnChanged = cb
	}
	setSilently(m.selfSelect, m.app.cfg.SelfEngine)
	setSilently(m.oppSelect, m.app.cfg.OpponentEngine)
}

func (m *MatchView) log(format string, args ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	m.logRows = append(m.logRows, line)
	if len(m.logRows) > 500 {
		m.logRows = m.logRows[len(m.logRows)-500:]
	}
	// 日志面板已去掉，但用户仍要看得到进展：每条同步写到主窗口状态栏。
	if m.app != nil {
		m.app.setStatusInfo(line)
	}
}

// Start 开始一场自动对战。
func (m *MatchView) Start() {
	if m.running {
		return
	}
	app := m.app
	reg := app.Reg()
	self := reg.Find(app.cfg.SelfEngine)
	if self == nil {
		dialog.ShowError(fmt.Errorf("请先在「引擎管理」里选择己方引擎"), app.win)
		return
	}
	opp := reg.Find(app.cfg.OpponentEngine)
	if opp == nil {
		dialog.ShowError(fmt.Errorf("请先选择对手引擎（可在「引擎管理」里添加外部引擎目录）"), app.win)
		return
	}

	opts := match.Options{
		Games:           app.cfg.MatchGames,
		SelfThreads:     app.cfg.SelfThreads,
		SelfHash:        app.cfg.SelfHash,
		OppThreads:      app.cfg.OppThreads,
		OppHash:         app.cfg.OppHash,
		AlternateColors: app.cfg.AlternateColors,
		OutputDir:       filepath.Join(app.baseDir, "matches", time.Now().Format("20060102-150405")),
		MoveTimeout:     10 * time.Minute,
	}
	// 【双方独立】对手的限时/深度可以单独设；没设（0）就跟己方一样
	oppMoveMS := app.cfg.MatchOppMoveTimeMS
	if oppMoveMS == 0 {
		oppMoveMS = app.cfg.MatchMoveTimeMS
	}
	oppGameMS := app.cfg.MatchOppGameTimeMS
	if oppGameMS == 0 {
		oppGameMS = app.cfg.MatchGameTimeMS
	}
	oppDepth := app.cfg.MatchOppDepth
	if oppDepth == 0 {
		oppDepth = app.cfg.MatchDepth
	}
	switch app.cfg.MatchTimeMode {
	case config.TimeModeGameTime:
		opts.LimitSelf = engine.Limit{Mode: engine.LimitGameTime, GameTimeMS: app.cfg.MatchGameTimeMS}
		opts.LimitOpp = engine.Limit{Mode: engine.LimitGameTime, GameTimeMS: oppGameMS}
	case config.TimeModeDepth:
		opts.LimitSelf = engine.Limit{Mode: engine.LimitDepth, Depth: app.cfg.MatchDepth}
		opts.LimitOpp = engine.Limit{Mode: engine.LimitDepth, Depth: oppDepth}
	default:
		opts.LimitSelf = engine.Limit{Mode: engine.LimitMoveTime, MoveTimeMS: app.cfg.MatchMoveTimeMS}
		opts.LimitOpp = engine.Limit{Mode: engine.LimitMoveTime, MoveTimeMS: oppMoveMS}
	}

	runner := match.New(*self, *opp, opts)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.running = true
	m.logRows = nil
	m.log("对战开始：%s  vs  %s，共 %d 局，时间控制 %s / %s",
		self.Name, opp.Name, opts.Games, opts.LimitSelf.Label(), opts.LimitOpp.Label())
	// 【R5】开赛前把线程分配如实记录下来：一旦总线程数超过逻辑处理器数，
	// 用户在对局日志里就能看到「为什么双方 nps 都掉了」。
	cpu := config.CPUCount()
	if sum := app.cfg.SelfThreads + app.cfg.OppThreads; sum > cpu {
		m.log("⚠ 线程超订：己方 %d + 对手 %d = %d > %d 个逻辑处理器；两个引擎会互相抢核，总吞吐可能下降。",
			app.cfg.SelfThreads, app.cfg.OppThreads, sum, cpu)
	} else {
		m.log("线程分配：己方 %d、对手 %d（本机 %d 个逻辑处理器）。",
			app.cfg.SelfThreads, app.cfg.OppThreads, cpu)
	}
	m.log("棋谱与报告目录：%s", opts.OutputDir)
	m.log("思考限制：己方 %s / 对手 %s", opts.LimitSelf.Label(), opts.LimitOpp.Label())
	if app.cfg.SelfEngine == app.cfg.OpponentEngine {
		m.log("★ 自我对弈：双方是同一个引擎（%s）。这是允许的——会启动两个独立进程，"+
			"日志里「己方 / 对手」代表两台进程，先后手按上面的轮换规则。", self.Name)
	}
	m.stateText.Text = "对战中…"
	m.stateText.Refresh()
	m.scoreText.Text = "比分（己方视角）：胜 0   和 0   负 0"
	m.scoreText.Refresh()
	m.progressText.Text = fmt.Sprintf("0 / %d 局", opts.Games)
	m.progressText.Refresh()
	m.curve.Clear()
	m.paused = false
	m.app.refreshMainMenu()    // 菜单项「暂停 / 继续」的文字与可用性跟着状态走
	m.app.refreshMatchButton() // 工具栏按钮切成「终止对战」

	app.matchMu.Lock()
	app.runner = runner
	app.matchMu.Unlock()

	go m.consume(runner)
	go runner.Run(ctx)
}

// TogglePause 暂停 / 继续。
func (m *MatchView) TogglePause() {
	m.app.matchMu.Lock()
	r := m.app.runner
	m.app.matchMu.Unlock()
	if r == nil {
		return
	}
	if r.Paused() {
		r.Resume()
		m.paused = false
		m.log("已继续对战")
	} else {
		r.Pause()
		m.paused = true
		m.log("已暂停（当前着法走完后停住）")
	}
	m.app.refreshMainMenu()
}

// Stop 终止对战，保留已完成的对局与报告。
func (m *MatchView) Stop() {
	m.app.matchMu.Lock()
	r := m.app.runner
	m.app.matchMu.Unlock()
	if r == nil {
		return
	}
	r.Stop()
	if m.cancel != nil {
		m.cancel()
	}
	m.log("已请求终止对战（已完成的对局与报告会保留）")
	m.app.refreshMainMenu()
	m.app.refreshMatchButton()
}

// consume 消费 Runner 的事件流并更新界面。
func (m *MatchView) consume(r *match.Runner) {
	app := m.app
	for ev := range r.Events() {
		switch ev.Type {
		case match.EvMatchStart:
			fyne.Do(func() {
				m.log("%s", ev.Message)
				m.stateText.Text = ev.Message
				m.stateText.Refresh()
			})
		case match.EvGameStart:
			board := ev.Board
			fyne.Do(func() {
				m.updateScore(ev)
				m.progressText.Text = fmt.Sprintf("第 %d / %d 局", ev.GameIndex, ev.TotalGames)
				m.progressText.Refresh()
				m.stateText.Text = ev.Message
				m.stateText.Refresh()
				m.log("%s", ev.Message)
				m.showBoard(board, -1, -1)
			})
		case match.EvMove:
			mv := ev.Move
			board := ev.Board
			fyne.Do(func() {
				m.updateScore(ev)
				m.updateEngineInfo(mv)
				from, to := -1, -1
				if mm, ok := notation.UCIToMove(mv.UCI); ok {
					from, to = mm.From, mm.To
				}
				m.showBoard(board, from, to)
				side := rules.Red
				if mv.Side == "black" {
					side = rules.Black
				}
				m.curve.AddPoint(mv.Ply, analytics.RedWinRate(mv.Score, side))
				app.board.SetInteractive(false)
			})
		case match.EvGameEnd:
			rec := ev.Game
			fyne.Do(func() {
				m.updateScore(ev)
				m.log("第 %d 局结束：%s（%s）%s，着数 %d，棋谱 %s",
					ev.GameIndex, rec.ResultCN(), rec.Reason, rec.ResultText(), len(rec.Moves), filepath.Base(rec.PGNPath))
				m.progressText.Text = fmt.Sprintf("已完成 %d / %d 局", ev.GameIndex, ev.TotalGames)
				m.progressText.Refresh()
			})
		case match.EvError:
			msg := ev.Message
			if ev.Err != nil {
				msg = ev.Err.Error()
			}
			fyne.Do(func() {
				m.log("错误：%s", msg)
				app.setStatusInfo("对战错误：" + msg)
			})
		case match.EvStatus:
			fyne.Do(func() { m.stateText.Text = ev.Message; m.stateText.Refresh() })
		case match.EvMatchEnd:
			rep := ev.Report
			fyne.Do(func() {
				m.running = false
				m.paused = false
				m.app.refreshMainMenu()
				m.app.refreshMatchButton()
				if rep == nil {
					m.stateText.Text = "对战结束"
					m.stateText.Refresh()
					return
				}
				if p, err := match.WriteScoresCSV(rep.Dir, rep); err == nil {
					m.log("评分表已导出：%s", p)
				}
				m.log("对战结束：胜 %d 和 %d 负 %d，胜率 %.2f%%", rep.SelfWins, rep.Draws, rep.SelfLosses, rep.SelfWinRate)
				if rep.FilePath != "" {
					m.log("报告文件：%s", rep.FilePath)
				}
				m.stateText.Text = "对战结束"
				m.stateText.Refresh()
				if len(rep.Games) == 0 {
					dialog.ShowInformation("对战结束", "没有完成任何对局（引擎启动失败或已被终止）。", app.win)
					return
				}
				dialog.ShowInformation("对战报告（摘要）", match.SummaryText(rep), app.win)
			})
		}
	}
}

func (m *MatchView) updateScore(ev match.Event) {
	m.scoreText.Text = fmt.Sprintf("比分（己方视角）：胜 %d   和 %d   负 %d", ev.SelfWins, ev.Draws, ev.SelfLosses)
	m.scoreText.Refresh()
}

// showBoard 把一局的实时局面显示到共享棋盘上。
func (m *MatchView) showBoard(b *rules.Board, from, to int) {
	if b == nil {
		return
	}
	g, err := rules.NewGameFromFEN(b.FEN())
	if err != nil {
		return
	}
	m.app.board.SetGame(g)
	m.app.board.SetLastMove(from, to)
	m.app.board.SetInteractive(false)
}
