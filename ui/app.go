package ui

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"xiangqi/config"
	"xiangqi/engine"
	"xiangqi/match"
	"xiangqi/notation"
	"xiangqi/rules"
	"xiangqi/version"
)

// App 是整个图形界面与全部后台任务的中枢。
//
// 线程约定（非常重要）：
//   - 所有 Fyne 控件只能从主 goroutine 修改，后台 goroutine 一律通过 fyne.Do(...) 回到主线程；
//   - 引擎思考、引擎扫描、自动对战全部跑在独立 goroutine 中，UI 永不阻塞。
type App struct {
	fyneApp fyne.App
	win     fyne.Window

	baseDir string
	cfgPath string
	engPath string
	cfg     *config.Config

	regMu sync.Mutex
	reg   *engine.Registry

	// ---------- 组件 ----------
	board     *Board
	curve     *Curve
	best      *BestMovePanel
	think     *ThinkPanel // 右栏常显的「思考设置 + 实时深度」条
	kib       *KibitzerPanel
	kibClient *engine.Client // 参考引擎客户端（Kibitzer，v1.6.10）
	kibMu     sync.Mutex
	kibSeq    int // 请求代际：只认最新一次的对比结果
	// 思考设置对话框的控件（提升为字段：便于自动化验证走真实路径 + 日志取证）
	thinkMode    *widget.RadioGroup
	thinkTime    *widget.Slider
	thinkDepth   *widget.Entry
	thinkSaveBtn *widget.Button
	kibBusy      bool // 参考引擎正忙：同一时刻只允许一次对比请求
	kibPending   bool // 忙期间又有新局面：等这次回来再补一次最新局面
	kibLastPos   engine.Position
	kibLastBoard *rules.Board
	kibLastMain  engine.Result
	kibEngName   string
	eval         *EvalBar
	cands        *CandidatePanel
	params       *ParamPanel

	moveInput *widget.Entry
	pasteArea *widget.Entry
	moveList  *widget.List
	moveRows  []string
	// moveRounds 是「一回合一行」的记谱模型：一行 = 红方一步 + 黑方一步。
	moveRounds   []moveRound
	moveBox      *fyne.Container
	moveBtns     []moveRowBtns
	moveBuiltFor int // 记谱表当前对应到第几着（增量更新用）
	moveScroll   *container.Scroll

	// 【v1.5 简化】左下角的「棋盘大小」滑块已从界面去掉（改走菜单「设置 → 棋盘：大/中/小」），
	// 因此 boardSlider / boardVal 两个字段一并删除。

	editPalette fyne.CanvasObject
	paletteBtns []*widget.Button

	lastEnding      string
	lastBestUCI     string
	lastBestChinese string
	lastPVText      string
	paramTimer      *time.Timer
	pasteTimer      *time.Timer // 粘贴棋谱的「停顿后再应用」防抖（见 buildPasteBox）
	pasteBox        fyne.CanvasObject

	// 【v1.5 简化】底部页签（参数设置 / 粘贴输入）已取消：
	// 参数面板移进菜单「设置 → 引擎参数…」，底部只留粘贴输入。
	// bottomTabs / bottomStack 两个字段随之删除。
	rightStack *fyne.Container
	leftBottom *fyne.Container
	modeBtns   [3]*widget.Button
	btnSwap    *widget.Button // 局面对调：交换红黑（人机对弈 / 电脑对电脑通用）

	mainSplit  *container.Split
	rightSplit *container.Split
	innerSplit *container.Split

	// 布局诊断用引用（XQ_DEBUG=1 时打印尺寸，便于排查溢出）
	rootStack   *fyne.Container
	mainRoot    *fyne.Container
	leftArea    fyne.CanvasObject
	boardPadded fyne.CanvasObject
	toolbarRef  fyne.CanvasObject
	statusRef   fyne.CanvasObject
	bridgeRight fyne.CanvasObject

	engineSelect *widget.Select
	engMgr       *EngineManager
	engMgrWin    fyne.Window

	// 防止「下拉框 OnChanged → syncEngineSelects → SetSelected → OnChanged」无限递归
	selectMu      sync.Mutex
	selectSyncing bool

	// ---------- 状态栏 ----------
	stTurn   *canvas.Text
	stEngine *canvas.Text
	stDepth  *canvas.Text
	stNodes  *canvas.Text
	stTime   *canvas.Text
	stWin    *canvas.Text
	stInfo   *canvas.Text

	// ---------- 背景 ----------
	bgMid     fyne.CanvasObject // 背景中间层：主题底纹（Raster）或图片（Image）或纯色（Rectangle）
	bgOverlay *canvas.Rectangle
	bgSolid   *canvas.Rectangle
	// titleBarDone 表示系统标题栏配色已按主题设过一次（失败会重试，见 applyTitleBarTheme）
	titleBarDone bool
	bgMissing    bool

	// ---------- 提示（toast） ----------
	toastBox  *fyne.Container
	toastText *canvas.Text
	toastRect *canvas.Rectangle
	toastSeq  atomic.Int64

	// ---------- 分析引擎 ----------
	anaMu     sync.Mutex
	anaClient *engine.Client
	anaState  string

	// anaRestartMu 串行化「重启分析引擎」：并发重启会各起一个进程，
	// 而 anaClient 只留最后一个，先起来的那个就变成没人管的野进程（见 restartAnalysisEngine）。
	anaRestartMu sync.Mutex

	anaReq chan analysisRequest
	anaOut chan analysisResult
	anaGen atomic.Uint64
	// engineOff = 用户从工具栏关掉了分析引擎（不再自动重启、不再发搜索请求）
	engineOff atomic.Bool
	btnEngine *widget.Button
	// 局面分析缓存：键 = 局面 + 影响结果的设置，值 = 引擎原始结果。
	anaCacheMu sync.Mutex
	anaCache   map[string]engine.Result
	anaKeyCur  string
	analyzing  atomic.Bool
	closing    chan struct{}
	closeOnce  sync.Once
	// closed 表示已进入关闭流程（shutdown 开始）：此后不再做任何界面写入
	closed atomic.Bool

	// ---------- 对战 ----------
	matchMu   sync.Mutex
	runner    *match.Runner
	matchView *MatchView

	// ---------- 会话状态 ----------
	game      *rules.Game
	viewing   int // >=0 表示正在查看历史局面（第 viewing 步之后），此时禁止点击走子
	editMode  bool
	editPiece rules.Piece

	// viewGame 是正在查看的那个历史局面（viewing >= 0 时非 nil）。
	//
	// 【局面分析要以局面为准】引擎分析、主变例中文记谱、胜率视角都必须基于
	// 「棋盘上当前显示的局面」，而不是「对局的最新局面」；否则点开历史看棋时，
	// 右侧分析面板给的还是最新局面的结论，对不上。
	viewGame *rules.Game
	// anaGame 是最近一次分析请求所针对的局面，供 renderResult 还原记谱与视角。
	anaGame *rules.Game

	moveSeq string // 当前着法序列（UCI，空格分隔）
	curMode string

	// ready 表示启动流程已走完。启动阶段会按上次的模式调一次 SetMode，
	// 那时不允许触发「切到引擎对战就自动开赛」（否则一开软件就自动下棋）。
	ready bool

	// quickMove 为真时，下一次分析请求改用极短限时（「立即出招」催引擎马上走）。
	quickMove bool

	// ---------- 人机对弈兜底看门狗 ----------
	//
	// 终态定义：「轮到电脑、引擎活着、却没有任何搜索在途」。任何人机对弈里
	// 丢请求的缺陷最后都落在这个终态上，所以在这里统一兜住（见 humanTurnWatchdog）。
	wdTick  atomic.Int64 // 150ms 进度 ticker 的计数
	wdIdle  int          // 连续几次检查都处在上面那个终态（主线程访问）
	wdRetry int          // 已经连续补发了几次（人类走到棋 / 对局结束时清零）

	// matchNoAutoStart 为真时，SetMode("match") 只切界面不开赛
	// （「对局设置…」这类入口用；见 showMatchWithoutStarting）。
	matchNoAutoStart bool
}

// analysisRequest / analysisResult 是「局面 → 引擎 → 结果」流水线的消息。
type analysisRequest struct {
	gen   uint64
	pos   engine.Position
	limit engine.Limit
	key   string
}

type analysisResult struct {
	gen   uint64
	res   engine.Result
	err   error
	pos   engine.Position
	moves []string
	key   string
}

// New 创建应用（不阻塞，返回后由 Run 启动事件循环）。
func New() *App {
	a := &App{
		anaReq:   make(chan analysisRequest, 8),
		anaOut:   make(chan analysisResult, 8),
		closing:  make(chan struct{}),
		viewing:  -1,
		game:     rules.NewGame(),
		anaCache: map[string]engine.Result{},
	}
	a.fyneApp = app.NewWithID("com.xiangqi.strong.v13")
	a.win = a.fyneApp.NewWindow(version.APPNAME + " " + version.VERSION)
	a.baseDir = exeDir()
	a.cfgPath = filepath.Join(a.baseDir, "config.json")
	// 【验证钩子】XQ_CONFIG：把配置读写重定向到指定文件。
	//
	// 为什么需要：自动化验证会在运行中改写 config.json（线程 / 哈希 / 模式 / 时间设置），
	// 而用户自己的实例也在跑、每 20 秒回写一次 —— 两边互相覆盖，
	// 既会把用户的设置改掉，也会让验证结果不可复现。指定一份独立配置就互不打扰。
	if p := os.Getenv("XQ_CONFIG"); p != "" {
		a.cfgPath = p
	}
	a.engPath = filepath.Join(a.baseDir, "engines.json")
	return a
}

// Window 返回主窗口（供对话框使用）。
func (a *App) Window() fyne.Window { return a.win }

// exeDir 返回可执行文件所在目录（config.json / engines.json / assets 都相对它）。
func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	dir := filepath.Dir(p)
	// go run 时 exe 在临时目录，回退到工作目录，便于开发调试
	if strings.Contains(strings.ToLower(dir), "go-build") {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
	}
	return dir
}

// Setup 完成初始化：配置、引擎库、主题、界面。
func (a *App) Setup() {
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		cfg = config.Default()
	}
	// 【配置迁移】旧版满配档 = 「逻辑核 - 1」线程（本机 16 核 → 15）。实测 15 线程只跑到
	// 1318%、性价比不如 12 线程（用户拍板降到 12），所以配置里若还是旧值就顺手改掉 ——
	// 否则"默认值改了，用户那儿还是旧线程数"，等于没改。
	if cfg.MaxStrength && cfg.Threads >= config.CPUCount()-1 {
		cfg.Threads = MaxStrengthThreads(config.CPUCount())
	}
	a.cfg = cfg

	// 适老化字号档位：**必须在建立任何控件之前**生效。
	// 主题的 Size() 与全工程 canvas.Text 的字号都是「创建时取值」的，
	// 晚一步设置，第一屏就会用标准档画出来（这正是上一轮留下的半成品）。
	SetFontScale(FontScaleFor(cfg.FontScale))

	// 应用皮肤（config.json 的 theme 字段决定用哪一套）。
	// 顺序要紧：先把调色板写回包级颜色变量，再建控件 —— 控件是「创建时取色」的，
	// 晚一步就会出现第一屏用旧配色画出来的半截效果。
	theme := SetActiveTheme(cfg.Theme)
	a.fyneApp.Settings().SetTheme(theme.Theme())

	a.board = NewBoard()
	a.board.SetSkin(currentSkin())
	a.curve = NewCurve("局势图")
	a.loadEngineRegistry()
	a.buildLayout()
	// 【v1.6.8】窗口置顶（TCHESS 设置菜单里也有这一项）：看棋时常要对照别的软件
	SetAlwaysOnTop(a.cfg.AlwaysOnTop)
	a.restoreWindow()
	a.wireEvents()
	a.startAnalysisLoop()
	a.startProgressTicker()

	a.setStatusInfo("就绪。引擎库：" + a.EngineLibraryDir())
	if a.bgMissing {
		a.setStatusInfo("未找到背景图 " + filepath.Join("assets", "background.jpg") + "，已回退为护眼纯色背景")
	}
	// 菜单栏 / 快捷键 / 界面开关（对齐 TCHESS、swiftxianglong 的菜单结构）
	a.win.SetMainMenu(a.buildMainMenu())
	a.installShortcuts()
	a.board.SetShowCoords(a.cfg.ShowCoords)
	a.board.SetShowLines(a.cfg.ShowLines)
	setSoundEnabled(a.cfg.SoundOn)

	a.refreshEverything()
	go a.restartAnalysisEngine()
	a.startPeriodicSave()
	// 首次运行（还没有 config.json）：写完默认配置后直接把「快速上手」摆出来——
	// 不该指望用户自己去菜单里翻说明书。
	if !fileExists(a.cfgPath) {
		a.SaveConfig()
		a.ShowQuickStart()
	}
	// ready 之后才允许「切到引擎对战就自动开赛」：
	// 启动阶段会按上次模式调一次 SetMode，那时自动开打会变成「一开软件就开始下棋」。
	a.ready = true
	// 配置里选了参考引擎就一并拉起（Kibitzer）
	go a.startKibitzer()
	a.applyVerifyHooks()
}

func (a *App) startPeriodicSave() {
	go func() {
		tk := time.NewTicker(20 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-a.closing:
				return
			case <-tk.C:
				// 【并发缺陷修复】SaveConfig 会读取滑块与输入框的当前值
				// （params.Values()、moveInput.Text），这些都是 Fyne 控件状态，
				// 只能在主线程访问。原来直接在后台协程里调用，属于跨线程读 UI。
				fyne.Do(func() { a.SaveConfig() })
			}
		}
	}()
}

// Run 进入事件循环（阻塞直到窗口关闭）。
func (a *App) Run() {
	// 窗口显示后反复校正尺寸与位置：Fyne 的 Canvas().Scale()（DPI 缩放）在窗口真正
	// 显示之前可能还是 1.0，只有显示后才会变成真实值（本机 120 DPI 面板 → 1.3），
	// 因此一次校正不够。这里持续观察：
	//   - 前 12 秒每 700ms 校正一次，覆盖窗口显示 / 窗口管理器重排 / 任务栏变化；
	//   - 一旦检测到 content scale 变化（用户改缩放或窗口被拖到另一块屏），
	//     立即重设窗口物理尺寸、重算布局与命中（R1/R2 的要求）。
	// 任何一次校正都不会把用户手动拉的窗口改小，只会纠正越界与不足。
	go func() {
		lastScale := float32(0)
		for i := 0; i < 18; i++ {
			time.Sleep(700 * time.Millisecond)
			fyne.Do(func() {
				// 标题栏配色要等窗口句柄出现（窗口显示之后），所以放进这个校正循环里重试
				a.applyTitleBarTheme()
				if s := a.win.Canvas().Scale(); s != lastScale {
					if lastScale != 0 && os.Getenv("XQ_DEBUG") != "" {
						fmt.Fprintf(os.Stderr, "[scale] content scale 变化：%.3f -> %.3f，重算窗口与布局\n", lastScale, s)
					}
					lastScale = s
					a.board.Refresh()
					a.clampWindow()
					return
				}
			})
		}
	}()
	a.win.ShowAndRun()
	a.shutdown()
}

// ---------------------------------------------------------------------------
// 配置 / 引擎库
// ---------------------------------------------------------------------------

// uiDoWait 在**后台协程**里更新界面：把改动交给主线程并等它做完。
//
// 为什么不直接用 fyne.Do：Fyne 2.8 的 fyne.Do 是「不等」的
// （DoFromGoroutine(fn, false)）—— 后台协程丢进队列就继续往下跑，于是同一个控件
// 可能被「队列里的写入」与「主线程的写入」以不确定的顺序改两次。竞态检测器实测：
// 菜单体检跑一遍（含满配开关）会报 3 条 DATA RACE，两侧都是状态栏文字的写入 ——
// 表现就是状态栏偶尔显示到过期的旧话（用户抱怨过的"状态栏说的和实测不一致"正是这一类）。
// DoAndWait 会等到主线程真正执行完才返回，写入顺序因此确定。
//
// ⚠ 只能在后台协程里调用：本版 Fyne 里从主线程调 DoAndWait 会走
// async.EnsureNotMain 的兼容分支（另起一个协程执行），反而又成了跨线程改 UI。
// 已经进入关闭流程时不再写界面：Fyne 的 DoAndWait 在关闭阶段会退化成
// 「在调用者协程里直接执行」（driver 的 drained 分支），那正好又变成跨线程改控件；
// 而窗口都在拆了，这条状态消息本来也没有意义。
func (a *App) uiDoWait(fn func()) {
	if a.closed.Load() {
		return
	}
	fyne.DoAndWait(fn)
}

// SaveConfig 把当前界面状态写回 config.json。
func (a *App) SaveConfig() {
	if a.cfg == nil || a.win == nil {
		return
	}
	if a.params != nil {
		th, h, d, ms, mp, tp, dm, sz := a.params.Values()
		a.cfg.Threads, a.cfg.Hash, a.cfg.Depth = th, h, d
		a.cfg.MoveTimeMS, a.cfg.MultiPV, a.cfg.Temperature = ms, mp, tp
		// 【缺陷修复】面板只有一个「固定深度」开关，它表达不了 infinite。
		// 原来这里无条件把 TimeMode 重写成 depth/movetime，于是用户选的「无限分析」
		// 会被静默改掉 —— 而且启动时和每 20 秒的自动保存各改一次，根本存不住。
		// （实测：配置里写 infinite，启动 1 秒后诊断行里已经是 movetime。）
		if a.cfg.TimeMode != config.TimeModeInfinite {
			if dm {
				a.cfg.TimeMode = config.TimeModeDepth
			} else {
				a.cfg.TimeMode = config.TimeModeMoveTime
			}
		}
		a.cfg.SyzygyPath = sz
	}
	if sz := a.win.Canvas().Size(); sz.Width > 100 && sz.Height > 100 {
		a.cfg.WindowWidth, a.cfg.WindowHeight = int(sz.Width), int(sz.Height)
	}
	if a.mainSplit != nil {
		a.cfg.MainSplit = a.mainSplit.Offset
	}
	if a.rightSplit != nil {
		a.cfg.RightSplit = a.rightSplit.Offset
	}
	a.cfg.LastMode = a.curMode
	a.cfg.LastMoveInput = a.moveInput.Text
	_ = a.cfg.Save(a.cfgPath)
}

// Reg 返回引擎注册表。
func (a *App) Reg() *engine.Registry {
	a.regMu.Lock()
	defer a.regMu.Unlock()
	return a.reg
}

// EngineLibraryDir 返回引擎库目录（exe 同目录下的 engines\）。
func (a *App) EngineLibraryDir() string { return filepath.Join(a.baseDir, "engines") }

// SelfEngineID 返回当前己方引擎 id。
func (a *App) SelfEngineID() string { return a.cfg.SelfEngine }

// OpponentEngineID 返回当前对手引擎 id。
func (a *App) OpponentEngineID() string { return a.cfg.OpponentEngine }

// skipExeSet 返回扫描时需要跳过的可执行文件集合（本程序自身）。
func (a *App) skipExeSet() map[string]bool {
	m := map[string]bool{}
	if p, err := os.Executable(); err == nil {
		m[strings.ToLower(filepath.Clean(p))] = true
	}
	return m
}

func (a *App) loadEngineRegistry() {
	reg, err := engine.LoadRegistry(a.engPath)
	if err != nil {
		a.setStatusInfo("engines.json 读取异常：" + err.Error())
	}
	a.regMu.Lock()
	a.reg = reg
	a.regMu.Unlock()

	// 首次运行：自动注册预置皮卡鱼目录，并落盘 engines.json
	needSave := false
	if len(reg.Engines) == 0 {
		if _, err := engine.EnsurePreset(reg, a.baseDir, 8*time.Second); err != nil {
			a.setStatusInfo("预置引擎注册失败：" + err.Error())
		} else {
			needSave = true
		}
	}
	if needSave || !fileExists(a.engPath) {
		_ = reg.Save(a.engPath)
	}
	if a.cfg.SelfEngine == "" || reg.Find(a.cfg.SelfEngine) == nil {
		if e := reg.Find(reg.DefaultEngine); e != nil {
			a.cfg.SelfEngine = e.ID
		} else if list := reg.List(); len(list) > 0 {
			a.cfg.SelfEngine = list[0].ID
		}
	}
	if a.cfg.OpponentEngine != "" && reg.Find(a.cfg.OpponentEngine) == nil {
		a.cfg.OpponentEngine = ""
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// OpenEngineLibraryDir 在资源管理器中打开引擎库目录。
func (a *App) OpenEngineLibraryDir() {
	dir := a.EngineLibraryDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = exec.Command("explorer", dir).Start()
}

// OpenMatchDir 打开对局记录目录。
func (a *App) OpenMatchDir() {
	dir := filepath.Join(a.baseDir, "matches")
	_ = os.MkdirAll(dir, 0o755)
	_ = exec.Command("explorer", dir).Start()
}

// ---------------------------------------------------------------------------
// 窗口
// ---------------------------------------------------------------------------

func (a *App) restoreWindow() {
	// 【R2-3】窗口尺寸按「棋盘完整 + 底部操作栏可见 + 右栏完整」反推：
	// 记忆里的尺寸若不足以完整显示这三块内容，就直接换成反推出来的理想尺寸，
	// 保证任何 DPI 缩放下打开即完整，而不是先裁一半再让用户自己去拉。
	want := fyne.NewSize(float32(a.cfg.WindowWidth), float32(a.cfg.WindowHeight))
	minW, minH := a.windowMinimum()
	if want.Width < minW || want.Height < minH {
		want = a.idealWindowSize()
	}
	if os.Getenv("XQ_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[window] stored=%v contentMin=%.1fx%.1f want=%v\n",
			fyne.NewSize(float32(a.cfg.WindowWidth), float32(a.cfg.WindowHeight)), minW, minH, want)
	}
	a.win.Resize(want)
	a.win.CenterOnScreen()
	a.clampWindow()
	// 说明：Fyne v2 的 Window 接口没有提供设置窗口位置的 API（只能 CenterOnScreen），
	// 而 CenterOnScreen 只按客户区居中、不计标题栏，会把标题栏顶出屏幕上沿；
	// 因此这里再用 Win32 SetWindowPos 把整窗夹回可见区域（见 R6）。
	EnforceWindowsOnScreen()
}

// splitHandleWidth 返回分栏拖拽手柄的厚度（逻辑单位），与主题设置保持一致。
func splitHandleWidth() float32 {
	th := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameSplitThickness)
	if th <= 0 {
		th = 5
	}
	return th
}

// windowMinimum 返回「内容真正需要的最小窗口尺寸」（逻辑单位）。
//
// 直接取根容器 mainRoot 的 MinSize：BorderLayout 已经把工具栏、状态栏、
// 左栏（棋盘 + 底部操作栏）、右栏的最小尺寸全部累计进去了。
//
// 【R2-5】v1.3.0 的 clamp 只防「窗口太大」，不防「窗口太小」：窗口小于这个值时
// BorderLayout 会把 bottom（底部操作栏整排）摆到可视区之上，用户看不到也点不到。
// 现在窗口最小尺寸有了权威来源，clamp 会主动把过小的窗口撑回来。
func (a *App) windowMinimum() (float32, float32) {
	if a.mainRoot == nil {
		return 720, 560
	}
	m := a.mainRoot.MinSize()
	w, h := m.Width, m.Height
	if w < 640 {
		w = 640
	}
	if h < 520 {
		h = 520
	}
	return w, h
}

// idealWindowSize 反推「打开即完整」的理想窗口尺寸（逻辑单位）：
//
//	宽 = 棋盘理想宽 + 内边距 + 分栏手柄 + 右栏宽度
//	高 = 工具栏 + （棋盘理想高 + 内边距 + 底部操作栏）+ 状态栏
//
// 棋盘按每格 60 逻辑像素计（130% 缩放下约 78 物理像素/格），
// 这是「棋子看得清、底部操作栏两行都放得下」的舒适值。
func (a *App) idealWindowSize() fyne.Size {
	const idealCell = 60.0
	pad := float32(8) // container.NewPadded：四周各 4 逻辑像素
	leftW := float32(geomUnitW*idealCell) + pad
	leftH := float32(geomUnitH*idealCell) + pad
	if a.leftBottom != nil {
		leftH += a.leftBottom.MinSize().Height
	} else {
		leftH += 60
	}
	// 右栏：取其最小宽度，并额外留 16 逻辑像素余量，
	// 避免参数面板里贴右对齐的数值文本（「16 线程」之类）被窗口边缘切掉半个字。
	rightW := float32(536)
	if a.rightStack != nil {
		if m := a.rightStack.MinSize().Width + 16; m > rightW {
			rightW = m
		}
	}
	var chromeH float32
	if a.toolbarRef != nil {
		chromeH += a.toolbarRef.MinSize().Height
	}
	if a.statusRef != nil {
		chromeH += a.statusRef.MinSize().Height
	}
	size := fyne.NewSize(leftW+splitHandleWidth()+rightW, leftH+chromeH)
	if a.mainRoot != nil {
		if m := a.mainRoot.MinSize(); size.Width < m.Width {
			size.Width = m.Width
		}
		if m := a.mainRoot.MinSize(); size.Height < m.Height {
			size.Height = m.Height
		}
	}
	return size
}

// clampWindow 保证窗口既不超过屏幕可用区域，也不小于内容所需尺寸。
//
// v1.3.0 只做了前半句（而且用 96%/94% 的硬比例），后半句缺失，于是：
//   - 窗口比内容小时，底部操作栏被 BorderLayout 顶到可视区之外（R2）；
//   - CenterOnScreen 只按客户区居中，标题栏顶出屏幕上沿（R6）。
//
// 现在：
//  1. 可用区域取 Win32 工作区（已扣除任务栏），换算成逻辑单位；
//  2. 小于内容所需 → 放大到内容所需；大于可用区域 → 缩小到可用区域；
//  3. 居中后无条件用 Win32 把整窗（含标题栏）夹回可见区域，保证 top >= 0。
func (a *App) clampWindow() {
	sw, sh := ScreenSize()
	if sw <= 0 || sh <= 0 {
		return
	}
	ax, ay, aw, ah := WorkArea()
	if aw <= 0 || ah <= 0 {
		ax, ay, aw, ah = 0, 0, sw, sh
	}
	scale := float64(a.win.Canvas().Scale())
	if scale <= 0 {
		scale = 1
	}
	// 可用区域换算成逻辑单位，并给窗口边框（左右各约 9、标题栏约 38 物理像素，
	// 即合计约 20 逻辑像素）留足余量——否则客户区刚好放下、整窗仍会探出屏幕 1 像素。
	const frameAllowance = 22
	maxW := float32(float64(aw)/scale) - frameAllowance
	maxH := float32(float64(ah)/scale) - frameAllowance
	if maxW < 600 {
		maxW = 600
	}
	if maxH < 480 {
		maxH = 480
	}

	minW, minH := a.windowMinimum()
	cur := a.win.Canvas().Size()
	nw, nh := cur.Width, cur.Height

	// (a) 太小 → 撑到内容所需（这就是 v1.3.0 缺失的那一半）
	if nw < minW {
		nw = minW
	}
	if nh < minH {
		nh = minH
	}
	// (b) 太大 → 缩到可用区域
	if nw > maxW {
		nw = maxW
	}
	if nh > maxH {
		nh = maxH
	}
	// (c) 可用区域本身比内容还小（极低分辨率/超大缩放）时以屏幕为准，
	//     此时棋盘会自动等比缩小并居中，操作栏由 BorderLayout 优先保留。
	if nw > maxW {
		nw = maxW
	}
	if nh > maxH {
		nh = maxH
	}

	if os.Getenv("XQ_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[clamp] screen=%dx%d work=%d,%d %dx%d scale=%.3f max=%.0fx%.0f min=%.0fx%.0f canvas=%.0fx%.0f -> %.0fx%.0f\n",
			sw, sh, ax, ay, aw, ah, scale, maxW, maxH, minW, minH, cur.Width, cur.Height, nw, nh)
	}
	if nw != cur.Width || nh != cur.Height {
		a.win.Resize(fyne.NewSize(nw, nh))
		a.win.CenterOnScreen()
	}
	// (d) R6：把整窗（含标题栏/边框）夹回屏幕可见区域，top 最小值为 0
	if fixed := EnforceWindowsOnScreen(); fixed > 0 && os.Getenv("XQ_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[clamp] 已用 SetWindowPos 修正 %d 个越界窗口，rects=%v\n", fixed, WindowRects())
	}

	s := a.win.Canvas().Size()
	a.cfg.WindowWidth, a.cfg.WindowHeight = int(s.Width), int(s.Height)
	a.dumpLayout()
}

// dumpLayout 在 XQ_DEBUG=1 时把关键容器的实际尺寸与最小尺寸打到 stderr。
// 仅用于开发期排查布局溢出，正常运行不产生任何输出。
func (a *App) dumpLayout() {
	if os.Getenv("XQ_DEBUG") == "" {
		return
	}
	rep := func(name string, o fyne.CanvasObject) {
		if o == nil {
			return
		}
		m := o.MinSize()
		sz := o.Size()
		p := o.Position()
		fmt.Fprintf(os.Stderr, "[layout] %-11s pos=%6.1f,%6.1f size=%7.1fx%-7.1f min=%7.1fx%-7.1f\n",
			name, p.X, p.Y, sz.Width, sz.Height, m.Width, m.Height)
	}
	rep("canvas", a.win.Content())
	rep("mainRoot", a.mainRoot)
	rep("toolbar", a.toolbarRef)
	rep("statusBar", a.statusRef)
	rep("mainSplit", a.mainSplit)
	rep("leftArea", a.leftArea)
	rep("boardPad", a.boardPadded)
	rep("board", a.board)
	rep("leftBottom", a.leftBottom)
	rep("bridgeRight", a.bridgeRight)
	rep("rightSplit", a.rightSplit)
	if a.mainSplit != nil {
		fmt.Fprintf(os.Stderr, "[layout] mainSplit.Offset = %.4f (左栏占比)\n", a.mainSplit.Offset)
	}
	if a.rightSplit != nil {
		fmt.Fprintf(os.Stderr, "[layout] rightSplit.Offset = %.4f\n", a.rightSplit.Offset)
	}
	// 记谱行取证：确认每一行的红/黑 ply 都写对了（点错会跳到开局）
	if a.moveBox != nil && len(a.moveRounds) > 0 {
		var sb strings.Builder
		for _, r := range a.moveRounds {
			fmt.Fprintf(&sb, "[%d 红ply=%d 黑ply=%d 有黑=%v] ", r.no, r.redPly, r.blackPly, r.hasBlack)
		}
		fmt.Fprintf(os.Stderr, "[notation] %s\n", sb.String())
	}
	if a.matchView != nil {
		rep("matchRoot", a.matchView.Root)
		rep("matchBottom", a.matchView.BottomBar)
		// R5 取证：把线程分配与超订警告的实际文本打到 stderr，
		// 便于在无法滚动到该区域时也能核对（正常运行时 XQ_DEBUG 未设置，不输出）。
		fmt.Fprintf(os.Stderr, "[threads] 己方=%d 对手=%d 逻辑核=%d 警告行=%q\n",
			a.cfg.SelfThreads, a.cfg.OppThreads, config.CPUCount(), a.matchView.threadWarnText())
	}
	// 防呆：内容最小宽度一旦超过屏幕可用区域，Fyne 会强行把窗口撑得比屏幕还宽
	// （本版实测踩过：一段不换行的超长 canvas.Text 就能做到）。
	// 这里直接把「谁最宽」标出来，便于定位。
	if a.mainRoot != nil {
		if m := a.mainRoot.MinSize(); m.Width > 0 {
			if aw := float32(1920); m.Width > aw*0.9 {
				fmt.Fprintf(os.Stderr, "[layout] ⚠ 内容最小宽度 %.0f 逻辑像素偏大，可能把窗口撑出屏幕\n", m.Width)
			}
		}
	}
}

func (a *App) wireEvents() {
	a.win.SetCloseIntercept(func() {
		// 关闭拦截本身就在主线程回调里，直接保存是安全的
		a.SaveConfig()
		a.shutdown()
		a.win.Close()
	})
	// 全局快捷键
	a.win.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		switch ev.Name {
		case fyne.KeyEscape:
			a.board.ClearSelection()
		case fyne.KeyF5:
			a.requestAnalysis()
		}
	})
}

// shutdown 释放资源（可重复调用）。
func (a *App) shutdown() {
	a.closeOnce.Do(func() {
		a.closed.Store(true)
		close(a.closing)
		a.anaMu.Lock()
		c := a.anaClient
		a.anaClient = nil
		a.anaMu.Unlock()
		if c != nil {
			c.Quit(1500 * time.Millisecond)
		}
		a.matchMu.Lock()
		r := a.runner
		a.matchMu.Unlock()
		if r != nil {
			r.Stop()
		}
		a.board.stopBlinkFor()
	})
}

// ---------------------------------------------------------------------------
// 状态栏 / 提示
// ---------------------------------------------------------------------------

func (a *App) setStatusInfo(msg string) {
	if a.stInfo == nil {
		return
	}
	a.stInfo.Text = msg
	a.stInfo.Refresh()
}

func (a *App) setEngineState(msg string, col color.Color) {
	if a.stEngine == nil {
		return
	}
	a.stEngine.Text = msg
	a.stEngine.Color = col
	a.stEngine.Refresh()
}

// toast 在窗口底部弹出一条 2.5 秒的提示。
func (a *App) toast(msg string) {
	if a.toastText == nil {
		return
	}
	seq := a.toastSeq.Add(1)
	a.toastText.Text = msg
	a.toastText.Refresh()
	a.toastBox.Show()
	a.toastRect.Refresh()
	time.AfterFunc(2500*time.Millisecond, func() {
		if a.toastSeq.Load() != seq {
			return
		}
		fyne.Do(func() { a.toastBox.Hide() })
	})
}

// ---------------------------------------------------------------------------
// 布局
// ---------------------------------------------------------------------------

func (a *App) buildLayout() {
	a.matchView = NewMatchView(a)

	// ---- 背景层 ----
	//
	// 三种画法由 config 的 background_style 决定（默认 texture = 主题自带底纹）：
	//   texture —— 程序生成的细腻底纹（微颗粒 + 一角柔光 + 四周压暗），任意分辨率都清晰
	//   image   —— 用 background_image 指定的图片（保留老行为，想用照片背景就切回来）
	//   solid   —— 纯色
	// 为什么默认不再是照片：照片会跟棋盘抢注意力、缩放后发虚，深色皮肤上还会偏色。
	a.rebuildBackground()
	bgPath := config.ResolvePath(a.baseDir, a.cfg.BackgroundImage)
	if a.cfg.BackgroundStyle == config.BgStyleImage && !fileExists(bgPath) {
		a.bgMissing = true
	}
	// 【v1.5 命名】模式按钮用同类软件的通用叫法（TCHESS 用「分析模式」，
	// 「引擎对战 / 人机对弈」是中文象棋软件的通行词）——
	// 上一版改成「分析局面 / 电脑互下 / 我和电脑下」过于口语化，已按用户要求改回。
	a.modeBtns[0] = widget.NewButton("分析模式", func() { a.SetMode("bridge") })
	a.modeBtns[1] = widget.NewButton("引擎对战", func() { a.ToggleMatch() })
	a.modeBtns[2] = widget.NewButton("人机对弈", func() { a.SetMode("human") })
	// 局面对调：局面不变、该谁走还是谁走，只把「哪一方由人下」换个边。
	// 引擎对战模式下同一按钮让两台引擎中途换边，文案见 refreshSwapButton。
	a.btnSwap = widget.NewButton("交换行棋方", func() { a.SwapSides() })

	a.engineSelect = widget.NewSelect(a.engineLabels(), func(s string) {
		if id := a.engineIDByLabel(s); id != "" && id != a.cfg.SelfEngine {
			a.SetSelfEngine(id)
		}
	})
	a.engineSelect.PlaceHolder = "选择己方引擎"

	// 引擎开关：测试期间可以把引擎整个关掉（省 CPU / 省电），随时再启动。
	// 「重启」的语义由「关闭 → 启动」两步覆盖，不再单独占一个按钮。
	btnEngine := a.newEngineToggleButton()

	// 【v1.5 简化】工具栏只留「最常用 + 与当前操作直接相关」的东西：
	// 「引擎管理」和「关于」都已在菜单里（引擎 / 帮助），不必再各占一个按钮。
	// 新手面对一排按钮时的第一反应是「我该点哪个」，少一个就少一次犹豫。
	toolbar := container.NewHBox(
		a.modeBtns[0], a.modeBtns[1], a.modeBtns[2], widget.NewSeparator(),
		a.btnSwap, widget.NewSeparator(),
		canvas.NewText("己方引擎", colFore), a.engineSelect, btnEngine,
		layoutSpacer(),
	)
	mk := func(w float32) *canvas.Text {
		t := canvas.NewText("", colFore)
		t.TextSize = textSize(textLabel)
		_ = w
		return t
	}
	a.stTurn = mk(0)
	a.stEngine = mk(0)
	a.stDepth = mk(0)
	a.stNodes = mk(0)
	a.stTime = mk(0)
	a.stWin = mk(0)
	a.stInfo = canvas.NewText("", colForeDim)
	a.stInfo.TextSize = textSize(textLabel)
	// v1.5：底部状态栏原本与右侧「最佳着法」面板重复显示深度/节点/耗时/胜率，
	// 按用户要求只保留这里独有的信息：轮到谁走、引擎状态、已走着数。
	statusRow := container.NewHBox(a.stTurn, sepDot(), a.stEngine)
	statusBar := barBox(container.NewVBox(statusRow, a.stInfo))
	a.board.SetOnMove(a.onBoardMove)
	a.board.SetOnIllegal(func(msg string) { a.setStatusInfo(msg) })
	a.board.SetOnEdit(a.onBoardEdit)

	leftBottom := container.NewStack(a.buildBridgeBottomBar(), a.matchView.BottomBar)
	a.leftBottom = leftBottom
	a.boardPadded = container.NewPadded(a.board)
	leftArea := container.NewBorder(nil, leftBottom, nil, nil, a.boardPadded)
	a.leftArea = leftArea

	// ---- 右侧：候选 / 曲线 / 参数（纵向分栏，可拖动）----
	bridgeRight := a.buildBridgeRight()
	a.bridgeRight = bridgeRight
	matchRight := a.matchView.Root
	a.rightStack = container.NewStack(bridgeRight, matchRight)
	a.mainSplit = container.NewHSplit(leftArea, a.rightStack)
	a.mainSplit.Offset = a.cfg.MainSplit
	a.toolbarRef = toolbar
	a.statusRef = statusBar

	mainRoot := container.NewBorder(toolbar, statusBar, nil, nil, a.mainSplit)
	a.mainRoot = mainRoot
	a.toastRect = canvas.NewRectangle(colPrimary)
	a.toastRect.CornerRadius = 8
	a.toastText = canvas.NewText("", colPrimaryFg)
	a.toastText.TextSize = textSize(textLabel)
	inner := container.NewStack(a.toastRect, container.NewPadded(a.toastText))
	a.toastBox = container.NewCenter(inner)
	a.toastBox.Hide()
	toastLayer := container.NewBorder(nil, container.NewPadded(a.toastBox), nil, nil, nil)

	root := container.NewStack(a.bgSolid, a.bgMid, a.bgOverlay, mainRoot, toastLayer)
	a.rootStack = root
	a.win.SetContent(root)

	a.syncEngineSelects()
	a.SetMode(a.cfg.LastMode)
}

// rebuildBackground 按 config 的背景样式重建背景层（主窗口用）。
//
// 三种样式的区别只在这一层：底下永远有一张纯色底（bgSolid），
// 中间层放底纹（程序生成）或图片，最上面是遮罩（控制整体明暗）。
func (a *App) rebuildBackground() {
	a.bgSolid = canvas.NewRectangle(colWindowSolid)
	a.bgOverlay = canvas.NewRectangle(a.overlayColor())

	spec := currentBackground()
	switch a.cfg.BackgroundStyle {
	case config.BgStyleImage:
		bgPath := config.ResolvePath(a.baseDir, a.cfg.BackgroundImage)
		if fileExists(bgPath) {
			a.bgMid = canvas.NewImageFromFile(bgPath)
			if img, ok := a.bgMid.(*canvas.Image); ok {
				img.FillMode = canvas.ImageFillStretch
				img.ScaleMode = canvas.ImageScaleSmooth
			}
			return
		}
		// 图片没了就退回底纹，不要留一片纯色
		a.bgMissing = true
		a.bgMid = newBackgroundRaster(spec)
	case config.BgStyleSolid:
		a.bgMid = canvas.NewRectangle(colWindowSolid)
	default:
		a.bgMid = newBackgroundRaster(spec)
	}
}

// refreshBackground 把已有背景层换成新皮肤的样式（换肤时调用，不重建整棵界面树）。
func (a *App) refreshBackground() {
	if a.bgSolid != nil {
		a.bgSolid.FillColor = colWindowSolid
		a.bgSolid.Refresh()
	}
	if a.bgOverlay != nil {
		a.bgOverlay.FillColor = a.overlayColor()
		a.bgOverlay.Refresh()
	}
	if a.bgMid == nil || a.rootStack == nil {
		return
	}
	// 中间层在 stack 里的位置固定是第 1 个，整体替换即可（canvas 不允许同一个对象挂在两处）。
	spec := currentBackground()
	switch a.cfg.BackgroundStyle {
	case config.BgStyleImage, config.BgStyleSolid:
		if r, ok := a.bgMid.(*canvas.Rectangle); ok {
			r.FillColor = colWindowSolid
			r.Refresh()
			return
		}
	}
	if _, ok := a.bgMid.(*canvas.Raster); ok {
		a.rootStack.Objects[1] = newBackgroundRaster(spec)
		a.rootStack.Refresh()
	}
}

// applyThemeName 切换皮肤：调色板 → Fyne 主题 → 背景层 → 棋盘皮肤 → 重绘。
//
// 顺序有讲究：先把颜色写回包级变量（applyPalette），再让 Fyne 用新主题重排控件，
// 最后刷新棋盘与背景 —— 否则会出现「按钮换了、卡片还是旧色」的半截换肤。
func (a *App) applyThemeName(name string) {
	p := SetActiveTheme(name)
	a.cfg.Theme = name
	if a.fyneApp != nil {
		a.fyneApp.Settings().SetTheme(p.Theme())
	}
	if a.bgSolid != nil {
		a.refreshBackground()
	} else {
		a.rebuildBackground()
	}
	if a.board != nil {
		a.board.SetSkin(currentSkin())
	}
	if a.rootStack != nil {
		a.rootStack.Refresh()
	}
	// 系统标题栏跟着一起变：深色界面配白标题栏最"露馅"（在 Windows 上生效）
	a.applyTitleBarTheme()
}

// applyTitleBarTheme 把当前皮肤的黑白取向应用到 Windows 标题栏。
//
// 窗口句柄要等窗口真正显示后才存在，所以这里做几次重试（由 Run 的校正循环反复调用），
// 成功一次就记下来不再重复。非 Windows / 老系统上会一直失败，静默忽略即可。
func (a *App) applyTitleBarTheme() {
	if a.titleBarDone {
		return
	}
	hwnd := MainWindowHandle()
	if hwnd == 0 {
		return
	}
	ok := SetDarkTitleBar(hwnd, currentThemeIsDark())
	if os.Getenv("XQ_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[titlebar] hwnd=0x%X dark=%v 设置%s\n", hwnd, currentThemeIsDark(),
			map[bool]string{true: "成功", false: "被系统拒绝（保持默认配色）"}[ok])
	}
	if ok || !currentThemeIsDark() {
		a.titleBarDone = true
	}
}

// overlayColor 计算背景遮罩颜色（不透明度来自 config，颜色取自当前皮肤底色）。
//
// 【缺陷修复】原来这里写死成暖白（0xFA,0xF6,0xEC）：浅色皮肤下它把照片"提亮"是对的，
// 但深色皮肤下同一层白纱会把照片冲成灰白，和深色面板完全不搭。
// 用皮肤底色做遮罩，浅色皮肤仍是提亮、深色皮肤就是压暗 —— 一套逻辑两种观感。
func (a *App) overlayColor() color.Color {
	alpha := uint8(a.cfg.BackgroundOverlay * 255 / 100)
	c := colWindowSolid
	c.A = alpha
	return c
}

func sepDot() *canvas.Text {
	t := canvas.NewText("|", colSep)
	t.TextSize = textSize(textLabel)
	return t
}

// SetMode 切换「分析模式 / 引擎对战 / 人机对弈」三种模式。
//
// 内部模式标识仍是 bridge / match / human（config.json 里存的就是它们，
// 改名只改界面文案，不动数据格式）。
func (a *App) SetMode(mode string) {
	if mode != "match" && mode != "human" {
		mode = "bridge"
	}
	prevMode := a.curMode
	a.switchThinkProfile(prevMode, mode) // 分析 / 人机各用自己那套思考设置
	a.curMode = mode
	inBridge := mode != "match" // 我和电脑下复用分析局面那套右栏/底部面板

	// 模式按钮的选中态
	modeIdx := 0
	switch mode {
	case "match":
		modeIdx = 1
	case "human":
		modeIdx = 2
	}
	for i, b := range a.modeBtns {
		if i == modeIdx {
			b.Importance = widget.HighImportance
		} else {
			b.Importance = widget.MediumImportance
		}
		b.Refresh()
	}
	a.syncFlip()
	a.refreshSwapButton()

	// 右侧与底部面板切换
	if a.rightStack != nil {
		for i, o := range a.rightStack.Objects {
			if (i == 0) == inBridge {
				o.Show()
			} else {
				o.Hide()
			}
		}
		a.rightStack.Refresh()
	}
	if a.leftBottom != nil {
		for i, o := range a.leftBottom.Objects {
			if (i == 0) == inBridge {
				o.Show()
			} else {
				o.Hide()
			}
		}
		a.leftBottom.Refresh()
	}

	a.refreshBoardInteractive()
	switch mode {
	case "human":
		a.setStatusInfo("人机对弈：你执" + a.humanSideCN() + "（在下方），另一方由引擎自动应招；点「交换行棋方」可中途换边（局面不变、棋盘倒转）")
		a.maybeEngineMove()
	case "match":
		// 【v1.5】用户要求「不用先找开始按钮」：切到引擎对战就**直接开赛**，
		// 参数用当前面板/配置里的值。暂停、终止都在菜单「对局」里。
		//
		// 启动阶段不算：程序启动会按上次的模式调一次 SetMode，
		// 若上次停在引擎对战，那样会变成「一开软件就自动开打」，用 ready 挡住。
		switch {
		case a.matchRunning():
			// 对战进行中再切回这个模式：什么都不做，别把状态栏里正在刷新的对战日志顶掉。
		case !a.ready || a.matchNoAutoStart:
			a.setStatusInfo("引擎对战：点工具栏「引擎对战」即开赛；对战中再点同一次即终止")
		default:
			a.matchView.Start()
		}
	default:
		a.setStatusInfo("分析模式：在棋盘上走子，或粘贴棋谱，引擎自动思考并给出应对")
		a.requestAnalysis()
	}
	a.cfg.LastMode = mode
}

// ShowEngineManager 打开引擎管理窗口。
//
// 【R8 修复】v1.3.0 直接把面板塞进窗口，没有铺背景层。护眼主题的
// ColorNameBackground 是全透明的（A=0x00，为的是让主窗口的背景图透出来），
// 主窗口之所以不黑是因为它自己叠了 bgSolid/bgImage/bgOverlay；
// 引擎管理窗口没有这三层，于是窗口底色露成 OpenGL 的清屏色——纯黑。
// 现在两个窗口共用同一套背景层构造，弹窗与主窗口风格完全一致。
//
// 【R4 修复】窗口尺寸按面板真实 MinSize 计算，保证整行按钮（四个）都能放下，
// 不再出现「设为对手引擎 / 移除」被切在窗口外的情况。
func (a *App) ShowEngineManager() {
	if a.engMgrWin == nil {
		a.engMgr = NewEngineManager(a)
		a.engMgrWin = a.fyneApp.NewWindow("引擎管理 — " + version.APPNAME + " " + version.VERSION)
		content := container.NewPadded(a.engMgr.Root)
		a.engMgrWin.SetContent(a.wrapWithBackground(content))
		a.sizeEngineManagerWindow()
	}
	a.engMgr.Refresh()
	a.engMgrWin.Show()
	a.enforceEngineManagerOnScreen()
}

// sizeEngineManagerWindow 按引擎管理面板所需尺寸反推窗口大小。
func (a *App) sizeEngineManagerWindow() {
	if a.engMgrWin == nil || a.engMgr == nil {
		return
	}
	m := a.engMgr.Root.MinSize()
	w, h := m.Width+16, m.Height+16
	if w < 1020 {
		w = 1020
	}
	if h < 620 {
		h = 620
	}
	// 不超过屏幕可用区域
	if ax, ay, aw, ah := WorkArea(); aw > 0 && ah > 0 {
		_ = ax
		_ = ay
		scale := float64(a.engMgrWin.Canvas().Scale())
		if scale <= 0 {
			scale = 1
		}
		maxW := float32(float64(aw)/scale) - 6
		maxH := float32(float64(ah)/scale) - 6
		if w > maxW {
			w = maxW
		}
		if h > maxH {
			h = maxH
		}
	}
	a.engMgrWin.Resize(fyne.NewSize(w, h))
}

// enforceEngineManagerOnScreen 把引擎管理窗口夹回可见区域（R6 同理）。
func (a *App) enforceEngineManagerOnScreen() {
	if a.engMgrWin == nil {
		return
	}
	a.engMgrWin.CenterOnScreen()
	EnforceWindowsOnScreen()
}

// wrapWithBackground 给任意内容套上主窗口同款的背景层（纯色 + 遮罩）。
//
// 【R8】弹窗与主窗口共用这一套，白天使用不会再看到刺眼的纯黑底。
//
// 为什么不在这里也铺一张底纹：同一个 CanvasObject 不能挂在两块画布上（Fyne 硬约束），
// 每块画布各生成一张全尺寸底纹又太浪费（每次开窗都要按当前尺寸重算一遍像素）。
// 次要窗口铺主题纯色即可 —— 同色系，观感一致，开销可以忽略。
// 另外：主题的 ColorNameBackground 现在是不透明色（修掉了菜单栏黑条），
// 所以即便这里有哪一层漏了，也不会再露出 OpenGL 的清屏黑。
func (a *App) wrapWithBackground(content fyne.CanvasObject) fyne.CanvasObject {
	solid := canvas.NewRectangle(colWindowSolid)
	overlay := canvas.NewRectangle(a.overlayColor())
	return container.NewStack(solid, overlay, content)
}

// ShowAbout 显示「关于」对话框。
func (a *App) ShowAbout() {
	txt := fmt.Sprintf(`%s %s

本软件是「引擎桥接分析 + 引擎自动对战」工具，不是人机对弈游戏。

一、分析局面：把第三方软件的最新着法摆到本软件棋盘上，内置 Pikafish 引擎立即
    深度思考并给出最强应对，一键复制 UCI 坐标粘贴回第三方软件。
二、电脑互下：加载两个引擎批量对弈，统计胜负与性能并生成报告。

技术栈：Go + Fyne v2（原生单文件 exe，零运行时依赖）
引擎通信：UCI / UCCI 双协议自适应，参数由引擎动态上报
版本：%s

本版明确不做：AI 评语、复盘数据库、杀法/残局训练营、联机对战、云同步。`,
		version.APPNAME, version.VERSION, version.VERSION)
	dialog.ShowInformation("关于 "+version.APPNAME, txt, a.win)
}

// ---------------------------------------------------------------------------
// 引擎选择控件
// ---------------------------------------------------------------------------

func (a *App) engineLabels() []string {
	list := a.Reg().List()
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, e.Label())
	}
	if len(out) == 0 {
		out = append(out, "（引擎库为空，请在菜单「引擎」里点「重新扫描引擎库」）")
	}
	return out
}

func (a *App) engineIDByLabel(label string) string {
	for _, e := range a.Reg().List() {
		if e.Label() == label {
			return e.ID
		}
	}
	return ""
}

// syncEngineSelects 把引擎库的最新状态同步到工具栏与对战面板的下拉框。
//
// 注意：这里必须「静默」地设置选中项——否则 Select.SetSelected 会触发 OnChanged，
// 而 OnChanged 又会回头调用本函数，形成无限递归（本版实测踩过这个坑）。
func (a *App) syncEngineSelects() {
	a.selectMu.Lock()
	if a.selectSyncing {
		a.selectMu.Unlock()
		return
	}
	a.selectSyncing = true
	a.selectMu.Unlock()
	defer func() {
		a.selectMu.Lock()
		a.selectSyncing = false
		a.selectMu.Unlock()
	}()

	if a.engineSelect != nil {
		a.setSelectSilently(a.engineSelect, a.engineLabels(), a.cfg.SelfEngine)
	}
	a.matchMu.Lock()
	mv := a.matchView
	a.matchMu.Unlock()
	if mv != nil {
		mv.RefreshEngines()
	}
}

// setSelectSilently 在临时屏蔽 OnChanged 的前提下更新下拉框选项与选中项。
func (a *App) setSelectSilently(sel *widget.Select, labels []string, id string) {
	cb := sel.OnChanged
	sel.OnChanged = nil
	sel.Options = labels
	if e := a.Reg().Find(id); e != nil {
		sel.SetSelected(e.Label())
	} else if len(labels) > 0 {
		sel.ClearSelected()
	}
	sel.Refresh()
	sel.OnChanged = cb
}

func (a *App) refreshEverything() {
	a.board.SetGame(a.game)
	a.rebuildMoveList()
	a.curve.SetPoints(nil)
	a.cands.SetTemperature(a.cfg.Temperature)
	a.params.SetValues(a.cfg.Threads, a.cfg.Hash, a.cfg.Depth, a.cfg.MoveTimeMS,
		a.cfg.MultiPV, a.cfg.Temperature, a.cfg.TimeMode == config.TimeModeDepth, a.cfg.SyzygyPath)
	a.cands.SetTemperature(a.cfg.Temperature)
	a.updateStatusBar()
	a.moveInput.SetText(a.cfg.LastMoveInput)
	if n, _ := notation.ParseMoveList(a.cfg.LastMoveInput); len(n) > 0 {
		a.applySequence(a.cfg.LastMoveInput, true)
	}
}

func (a *App) updateStatusBar() {
	if a.stTurn == nil {
		return
	}
	side := a.game.Board.Side
	who := "轮到：" + rules.SideName(side) + "方走棋"
	if a.game.InCheck() {
		who += "（被将军）"
	}
	if a.game.StartFEN != rules.StartFEN {
		who += "，自定义局面"
	}
	a.stTurn.Text = who
	a.stTurn.Refresh()
	a.stInfo.Text = fmt.Sprintf("已走 %d 着", len(a.game.Moves))
	a.stInfo.Refresh()
}

// ---------------------------------------------------------------------------
// 对战面板引用（在 matchview.go 中定义）
// ---------------------------------------------------------------------------
