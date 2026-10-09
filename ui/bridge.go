package ui

import (
	"context"
	"fmt"
	"os"
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
	"xiangqi/notation"
	"xiangqi/rules"
)

// 本文件实现「核心功能 A：引擎桥接分析模式」的全部界面与后台逻辑。

// ---------------------------------------------------------------------------
// 右侧面板
// ---------------------------------------------------------------------------

func (a *App) buildBridgeRight() fyne.CanvasObject {
	a.best = NewBestMovePanel(a.copyBestMove, a.copyPV)
	a.think = NewThinkPanel(a.ShowThinkSettings)
	a.kib = NewKibitzerPanel()
	a.think.SetConfig(a.cfg.TimeMode, a.cfg.MoveTimeMS, a.cfg.Depth)
	a.eval = NewEvalBar()
	a.cands = NewCandidatePanel()
	a.params = NewParamPanel(a)
	a.params.OnChanged = a.onParamsChanged

	// 【布局 v1.6.1】右栏**左右对半**切成两块，每块只放一类信息（用户要求）：
	//
	//	左半 · 分析 = 局势条 / 最佳着法 / 变招（变招全部平铺，不滚动）
	//	右半 · 棋谱 = 局势图（宽度即右栏的一半）/ 着法列表
	//
	// 各面板尺寸同步减半：局势条高度 26→18、曲线高度 88→64、变招行高约减半，
	// 省下来的空间用来「少滚动」——分析块只在极端窗口下才出现滚动条。
	a.pasteBox = a.buildPasteBox()

	// 【布局 v1.6.2】右栏重排，目标是「提高利用率、别再留大片空白」：
	//
	//	上半 = 左右对半两栏：左半 分析（局势条 / 最佳着法 / 变招）、右半 棋谱（着法列表）
	//	下半 = **全宽局势图**（宽度 = 整个右栏 ≈ 575 逻辑像素，高度 150）
	//
	// 为什么把局势图从半栏挪到底部横带：走势图是横着看的图，半栏里只有 240 宽、
	// 64 高，用户连着两轮反馈「太小」。放到底部后宽度翻倍、高度约翻 2.3 倍，
	// 同时把两半里原本浪费的垂直空间吃掉——变招卡与着法列表只需自然高度，
	// 不再被拉成一整块空白。
	a.curve.SetMinHeight(sz(160))

	// ⚠ 分析半栏**必须**套一层 VScroll：变招行展开后会带出「全部后续走法」，
	// 那是可以很长的多行文字；没有滚动容器时，内容的最小高度会顺着布局链
	// 把整个窗口顶到比屏幕还高（实测：展开一条变招后窗口 918 逻辑像素 > 可用 816）。
	// 滚动容器把高度钉在 200，超出部分在这半栏里滚。
	analysis := container.NewVScroll(container.NewBorder(
		container.NewVBox(cardBox(a.eval), cardBox(a.think.Root), cardBox(a.best.Root), cardBox(a.kib.Root)),
		nil, nil, nil,
		cardBox(a.cands.Root),
	))
	analysis.SetMinSize(fyne.NewSize(170, 200))
	halves := container.NewHSplit(analysis, a.buildMoveListPanel())
	halves.Offset = 0.5
	// rightSplit 仍然指向「左右两半」这根分栏：拖动比例要记进 config.json
	a.rightSplit = halves

	curveBand := cardBox(a.curve)
	return container.NewBorder(nil, curveBand, nil, nil, halves)
}

// ---------------------------------------------------------------------------
// 参考引擎（Kibitzer）：同一局面两引擎同时分析
// ---------------------------------------------------------------------------

// kibSetIdle 在主线程上更新「参考引擎」对比卡的卡头与提示。
//
// 【并发缺陷修复 / 崩溃根因】startKibitzer 本身是 `go a.startKibitzer()` 起来的，
// 里面的 a.kib.SetIdle 直接改了 canvas.Text 与 widget —— 那是跨线程改 UI。
// Fyne 的文本测量用的是**进程内唯一**的 HarfbuzzShaper（内部复用一个 buffer），
// 主线程首屏排版与后台协程同时整形同一个 buffer，就会把 buffer 的游标写坏，
// 直接 panic：`index out of range [1] with length 1`（harfbuzz buffer.cur）。
// 实测：本次修复前，同样的启动流程 7 次里有 3 次崩在首屏排版（见 tools\xqcase.ps1）。
func (a *App) kibSetIdle(head, msg string) {
	if a.kib == nil {
		return
	}
	fyne.Do(func() { a.kib.SetIdle(head, msg) })
}

// startKibitzer 按配置启动参考引擎客户端（配置里没选引擎就停掉）。
//
// 设计依据 docs/多引擎对比-设计稿.md：线程/哈希默认**跟随主引擎**，
// 取消勾选才用参考引擎自己的；时间语义只有「同步每步时限」一种（同类产品同款）。
func (a *App) startKibitzer() {
	id := a.cfg.KibitzerEngine
	if os.Getenv("XQ_DEBUG") != "" {
		ids := []string{}
		for _, e := range a.Reg().List() {
			ids = append(ids, e.ID+"="+e.Name)
		}
		fmt.Fprintf(os.Stderr, "[kib] cfg.KibitzerEngine=%q  a.kib==nil? %v  已注册引擎=%v\n",
			id, a.kib == nil, ids)
	}
	if id == "" || a.kib == nil {
		a.stopKibitzer()
		a.kibSetIdle("参考引擎：已关闭",
			"菜单「引擎」里的「参考引擎（Kibitzer）」选一个引擎，即可与主引擎同时分析同一局面。")
		return
	}
	e := a.Reg().Find(id)
	if e == nil {
		a.kibSetIdle("参考引擎（找不到）", "配置里的引擎已被移除："+id+"。请在菜单「引擎」里的「参考引擎（Kibitzer）」重新选一个。")
		return
	}
	c := engine.NewClient(e.Path)
	if err := c.Start(8 * time.Second); err != nil {
		a.kibSetIdle("参考引擎（启动失败）", "启动失败："+err.Error())
		return
	}
	th, h := a.cfg.Threads, a.cfg.Hash
	if !a.cfg.KibitzerFollowMain {
		th, h = a.cfg.KibitzerThreads, a.cfg.KibitzerHash
	}
	if _, ok := c.FindOption("Threads"); ok {
		_ = c.SetOption("Threads", fmt.Sprint(th))
	}
	if _, ok := c.FindOption("Hash"); ok {
		_ = c.SetOption("Hash", fmt.Sprint(h))
	}
	a.kibMu.Lock()
	old := a.kibClient
	a.kibClient = c
	a.kibMu.Unlock()
	if old != nil {
		old.Kill()
	}
	a.kibSetIdle("参考引擎：已开启（"+e.Name+"）", "走一步棋，这里就会出现它对同一局面的结论。")
}

// stopKibitzer 关闭参考引擎客户端。
func (a *App) stopKibitzer() {
	a.kibMu.Lock()
	c := a.kibClient
	a.kibClient = nil
	a.kibMu.Unlock()
	if c != nil {
		c.Kill()
	}
}

// kibitzerClient 取当前可用的参考引擎客户端（没启用/已死返回 nil）。
func (a *App) kibitzerClient() *engine.Client {
	a.kibMu.Lock()
	c := a.kibClient
	a.kibMu.Unlock()
	if c == nil || !c.Alive() {
		return nil
	}
	return c
}

// requestKibitzer 把同一局面丢给参考引擎，结果回来填对比卡。全程不阻塞主链路。
//
// 【缺陷修复】主引擎每出一版结果（含中间版）都会调到这里，早期实现是「每次直接发」，
// 结果多版请求叠在一起把参考引擎撞成 busy（界面显示「引擎正忙（上一次思考尚未结束）」）。
// 现在**串行**：正忙就只记一个「有新局面的待办」，等这次回来再补一次最新局面——
// 对比功能不需要每一版中间结果，稳定拿到最新结论即可。
func (a *App) requestKibitzer(g *rules.Game, mainRes engine.Result) {
	if g == nil || a.kib == nil || a.kibitzerClient() == nil {
		return
	}
	moves := make([]string, 0, len(g.Moves))
	for _, m := range g.Moves {
		moves = append(moves, m.String())
	}
	engName := "参考引擎"
	if e := a.Reg().Find(a.cfg.KibitzerEngine); e != nil {
		engName = e.Name
	}
	a.kibMu.Lock()
	a.kibLastPos = engine.Position{Startpos: g.StartFEN == rules.StartFEN, FEN: g.StartFEN, Moves: moves}
	a.kibLastBoard = g.Board.Clone()
	a.kibLastMain = mainRes
	a.kibEngName = engName
	if a.kibBusy {
		a.kibPending = true
		a.kibMu.Unlock()
		return
	}
	a.kibBusy = true
	a.kibMu.Unlock()
	a.fireKibitzer()
}

// fireKibitzer 用最近一次记录的局面发一次对比请求（调用方保证不忙）。
func (a *App) fireKibitzer() {
	a.kibMu.Lock()
	pos := a.kibLastPos
	board := a.kibLastBoard
	mainRes := a.kibLastMain
	engName := a.kibEngName
	a.kibMu.Unlock()

	c := a.kibitzerClient()
	if c == nil || board == nil {
		a.kibMu.Lock()
		a.kibBusy = false
		a.kibMu.Unlock()
		return
	}
	limit := engine.Limit{Mode: engine.LimitMoveTime, MoveTimeMS: a.cfg.KibitzerSyncMS}
	if limit.MoveTimeMS <= 0 {
		limit.MoveTimeMS = 2000
	}
	go func() {
		res, err := c.Analyze(context.Background(), pos, limit, nil)
		a.kibMu.Lock()
		a.kibBusy = false
		pending := a.kibPending
		a.kibPending = false
		a.kibMu.Unlock()

		if err == nil {
			pv := ""
			if len(res.Lines) > 0 {
				pv = strings.Join(a.pvChinese(res.Lines[0].PV, board, 12), " ")
			}
			diverged := mainRes.BestMove != "" && res.BestMove != "" && mainRes.BestMove != res.BestMove
			fyne.Do(func() {
				a.kib.Set(engName, scoreText(res.Score), res.Depth, res.TimeMS, res.NPS, pv, diverged)
			})
		} else {
			fyne.Do(func() { a.kib.SetIdle("参考引擎（出错）", "分析出错："+err.Error()) })
		}
		// 忙的这段时间里局面又变了：补一次最新局面
		if pending {
			a.kibMu.Lock()
			a.kibBusy = true
			a.kibMu.Unlock()
			a.fireKibitzer()
		}
	}()
}

// buildPasteBox 构建「粘贴棋谱」对话框的内容（不常驻主界面）。
//
// 【v1.6】原来它是主界面底部左栏的一块多行输入框，和「着法列表」并排占掉整个底栏。
// 同类软件（TCHESS 的「棋谱 → 粘贴棋谱」、象棋巫师）都是把它做成**菜单→一个动作**：
// 要用才弹出来，平时不占地方。这样右栏才能收窄成「分析 / 棋谱」两块。
func (a *App) buildPasteBox() fyne.CanvasObject {
	a.pasteArea = widget.NewMultiLineEntry()
	a.pasteArea.SetPlaceHolder("粘贴棋谱（着法序列），例如：\n" +
		"h2e2 h9g7 c3c4 i9h9　或　1. h2e2 h9g7 2. c3c4\n" +
		"空格 / 换行 / 逗号分隔都行，也支持整行 position startpos moves ...")
	a.pasteArea.SetMinRowsVisible(3)

	btnClear := widget.NewButton("清空", func() { a.pasteArea.SetText("") })

	// 【v1.5 简化】「应用着法序列并立即分析」按钮已删除（用户要求）。
	// 改成**粘贴即分析**：内容变化后停顿 600ms 再解析，能解析成着法序列就立即应用；
	// 解析不出来（用户还在敲）就静静等着，不弹错、不打扰。
	//
	// 为什么要停一下：OnChanged 是逐字符触发的，直接应用会让引擎被反复打断；
	// 等用户停下来再动手，既快又不抖。
	a.pasteArea.OnChanged = func(string) {
		if a.pasteTimer != nil {
			a.pasteTimer.Stop()
		}
		a.pasteTimer = time.AfterFunc(600*time.Millisecond, func() {
			fyne.Do(func() {
				if a.pasteArea == nil {
					return
				}
				txt := a.pasteArea.Text
				if !looksLikeMoveSequence(txt) {
					return
				}
				a.applySequence(txt, true)
			})
		})
	}

	head := container.NewVBox(
		sectionTitle("粘贴棋谱"),
		smallText("粘贴后自动重放并分析（支持空格 / 换行 / 逗号分隔）"),
		container.NewHBox(btnClear),
	)
	return container.NewBorder(head, nil, nil, nil, a.pasteArea)
}

// ShowPasteDialog 打开「粘贴棋谱」对话框（菜单「棋谱 → 粘贴棋谱…」）。
func (a *App) ShowPasteDialog() {
	if a.pasteBox == nil {
		return
	}
	dialog.ShowCustom("粘贴棋谱", "关闭", a.pasteBox, a.win)
}

// looksLikeMoveSequence 粗略判断一段文本是不是着法序列。
//
// 判据：至少有一个像 UCI 着法（4 个字符、字母+数字组成）或中文着法（带「进/退/平」）的词。
// 目的只是「别在用户刚敲了两个字符时就动手」，宁可漏判也不要误判。
func looksLikeMoveSequence(s string) bool {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == ',' || r == '，' || r == '、'
	})
	hits := 0
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// 跳过 PGN 的着数序号（"1." "12." 之类）
		if strings.Trim(f, "0123456789.") == "" {
			continue
		}
		if strings.ContainsAny(f, "进退平") || isUCIMove(f) {
			hits++
		}
	}
	return hits > 0
}

// isUCIMove 判断是否是 4 字符的 UCI 着法（如 h2e2、b7b0）。
func isUCIMove(s string) bool {
	if len(s) != 4 {
		return false
	}
	return s[0] >= 'a' && s[0] <= 'i' && s[1] >= '0' && s[1] <= '9' &&
		s[2] >= 'a' && s[2] <= 'i' && s[3] >= '0' && s[3] <= '9'
}

// ---------------------------------------------------------------------------
// 左侧底部操作条
// ---------------------------------------------------------------------------

func (a *App) buildBridgeBottomBar() fyne.CanvasObject {
	a.moveInput = widget.NewEntry()
	a.moveInput.SetPlaceHolder("坐标输入：h2e2 h9g7 c3c4（回车应用，从初始局面重放）")
	a.moveInput.OnSubmitted = func(s string) { a.applySequence(s, true) }

	btnUndo := widget.NewButton("悔棋", func() { a.undo(1) })
	btnUndoAll := widget.NewButton("撤销全部", func() { a.undo(len(a.game.Moves)) })
	btnReset := widget.NewButton("新局面", func() { a.resetGame() })
	btnEdit := widget.NewButton("编辑局面", func() { a.toggleEditMode() })

	// 【v1.6.8】「立即出招」：人机对弈时催引擎马上走一步（同类软件的「立即走棋」）
	btnForce := widget.NewButton("立即出招", func() { a.ForceEngineMove() })
	row2 := container.NewHBox(btnUndo, btnUndoAll, btnReset, btnEdit, btnForce)

	// 摆盘调色板（默认隐藏）
	a.editPalette = a.buildEditPalette()
	a.editPalette.Hide()

	// 【v1.5 简化】这一栏原来有 5 个按钮 + 一行「棋盘大小」滑块，现在只剩 4 个按钮：
	//   · 「复制最佳着法」与右侧结果卡片上那个完全重复 → 去掉这→（结果旁边那个更直观，
	//     而且复制的内容就是它上面那一步）；
	//   · 「棋盘大小」滑块与菜单「设置 → 棋盘：大 / 中 / 小」重复 → 滑块去掉，只留菜单。
	// 界面每少一个控件，第一次用的人就少犹豫一次。
	// moveInput 控件仍保留在内存里（配置持久化/回填要用），只是不放进界面。
	return container.NewVBox(row2, a.editPalette)
}

func (a *App) buildEditPalette() fyne.CanvasObject {
	a.editPiece = rules.MakePiece(rules.Red, rules.PPawn)

	redPieces := []rules.Piece{rules.PKing, rules.PAdvisor, rules.PElephant, rules.PHorse, rules.PRook, rules.PCannon, rules.PPawn}
	blackPieces := []rules.Piece{rules.PKing, rules.PAdvisor, rules.PElephant, rules.PHorse, rules.PRook, rules.PCannon, rules.PPawn}
	a.paletteBtns = nil

	mk := func(p rules.Piece) *widget.Button {
		piece := p
		b := widget.NewButton(piece.Name(), func() { a.setEditPiece(piece) })
		a.paletteBtns = append(a.paletteBtns, b)
		return b
	}
	rowRed := container.NewHBox()
	for _, p := range redPieces {
		rowRed.Add(mk(rules.MakePiece(rules.Red, p)))
	}
	rowBlack := container.NewHBox()
	for _, p := range blackPieces {
		rowBlack.Add(mk(rules.MakePiece(rules.Black, p)))
	}
	// 橡皮擦用空子表示
	eraser := widget.NewButton("橡皮(移除)", func() { a.setEditPiece(rules.PEmpty) })
	a.paletteBtns = append(a.paletteBtns, eraser)

	btnClear := widget.NewButton("清空棋盘", func() {
		for i := 0; i < rules.Squares; i++ {
			a.game.Board.Sq[i] = rules.PEmpty
		}
		a.afterEdit()
	})
	btnStart := widget.NewButton("初始局面", func() {
		a.resetGame()
	})
	btnDone := widget.NewButton("完成摆盘", func() { a.toggleEditMode() })
	btnDone.Importance = widget.HighImportance

	label := smallText("摆盘模式：先选棋子，再点棋盘交叉点放置；「橡皮」用于移除。摆盘后历史着法作废，以当前局面为新起点。")
	return container.NewVBox(label,
		container.NewHBox(append([]fyne.CanvasObject{canvas.NewText("红", colErr)}, rowRed.Objects...)...),
		container.NewHBox(append([]fyne.CanvasObject{canvas.NewText("黑", colFore)}, rowBlack.Objects...)...),
		container.NewHBox(eraser, btnClear, btnStart, btnDone),
	)
}

func (a *App) setEditPiece(p rules.Piece) {
	a.editPiece = p
	a.setStatusInfo("摆盘：已选择「" + p.Name() + "」，点击棋盘交叉点放置")
}

// ---------------------------------------------------------------------------
// 局面变化
// ---------------------------------------------------------------------------

func (a *App) onBoardMove(m rules.Move) {
	if a.viewing >= 0 || a.editMode {
		return
	}
	if err := a.game.TryMove(m); err != nil {
		a.setStatusInfo("非法着法：" + err.Error())
		a.toast("非法着法：" + err.Error())
		a.board.SetGame(a.game)
		return
	}
	a.board.SetLastMove(m.From, m.To)
	a.board.SetGame(a.game)
	a.afterMoveEffects(m)
	a.afterPositionChange()
}

func (a *App) onBoardEdit(sq int) {
	if !a.editMode {
		return
	}
	a.game.Board.Sq[sq] = a.editPiece
	a.afterEdit()
}

func (a *App) afterEdit() {
	// 摆盘后原着法历史不再成立：以当前局面为新的起点
	a.game.StartFEN = a.game.Board.FEN()
	g, err := rules.NewGameFromFEN(a.game.StartFEN)
	if err == nil {
		a.game = g
	}
	a.viewing = -1
	a.viewGame = nil
	a.moveInput.SetText("")
	if a.pasteArea != nil {
		a.pasteArea.SetText("")
	}
	a.curve.SetPoints(nil)
	a.board.SetLastMove(-1, -1)
	a.board.SetGame(a.game)
	a.rebuildMoveList()
	a.updateStatusBar()
	a.requestAnalysis()
}

// afterPositionChange 在局面被改变后统一刷新界面并触发自动思考。
func (a *App) afterPositionChange() {
	a.viewing = -1
	a.viewGame = nil
	a.moveSeq = notation.MoveListToUCI(a.game.Moves)
	a.moveInput.SetText(a.moveSeq)
	a.syncMoveList() // 增量：走一步只动最后一行（避免每步重建整张记谱表造成卡顿）
	a.updateStatusBar()
	a.refreshBoardInteractive()

	if st, reason := a.game.Adjudicate(); st != rules.Playing {
		key := st.String() + reason
		if a.lastEnding != key {
			a.lastEnding = key
			a.toast(fmt.Sprintf("%s（%s）", st.String(), reason))
			dialog.ShowInformation("对局结束",
				fmt.Sprintf("%s —— %s\n共 %d 着", st.String(), reason, len(a.game.Moves)), a.win)
		}
	} else {
		a.lastEnding = ""
	}
	a.requestAnalysis()
}

func (a *App) jumpTo(ply int) {
	if ply < 0 || ply >= len(a.game.Moves) {
		a.viewing = -1
		a.viewGame = nil
		a.board.SetGame(a.game)
		if len(a.game.Moves) > 0 {
			last := a.game.Moves[len(a.game.Moves)-1]
			a.board.SetLastMove(last.From, last.To)
		} else {
			a.board.SetLastMove(-1, -1)
		}
		a.refreshBoardInteractive()
		a.curve.SetMarker(len(a.game.Moves), false)
		a.highlightMoveRows()
		a.updateStatusBar()
		a.requestAnalysis()
		a.setStatusInfo("已回到当前局面")
		return
	}
	g, err := rules.NewGameFromFEN(a.game.StartFEN)
	if err != nil {
		return
	}
	for i := 0; i < ply; i++ {
		g.ForceMove(a.game.Moves[i])
	}
	a.viewing = ply
	a.viewGame = g
	a.board.SetGame(g)
	if os.Getenv("XQ_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[jump] 跳到第 %d 着之后（局面已切换）\n", ply)
	}
	if ply > 0 {
		last := g.Moves[len(g.Moves)-1]
		a.board.SetLastMove(last.From, last.To)
	}
	a.board.SetInteractive(false)
	a.curve.SetMarker(ply, true)
	a.highlightMoveRows()
	a.updateStatusBar()
	// 局面分析以局面为准，但**只用缓存**：算过的局面直接回放，
	// 没算过的就不打扰引擎（用户要求「不要跳转时重新分析」，
	// 跳转因此不会再有等待引擎的卡顿）。
	a.renderAnalysisFromCacheOnly()
	a.setStatusInfo(fmt.Sprintf("正在查看第 %d 着之后的局面（棋盘已锁定，点「回到当前局面」恢复）", ply))
}

func (a *App) undo(n int) {
	if n <= 0 {
		return
	}
	// 【缺陷修复】对战进行中禁止悔棋：对战的着法由 Runner 的事件流驱动，
	// 悔棋只会让显示与对战实际局面不一致（下一着回来就被覆盖）。
	if a.matchRunning() {
		a.toast("对战进行中不能悔棋，请先终止对战")
		return
	}
	a.game.UndoN(n)
	a.viewing = -1
	a.board.SetGame(a.game)
	if len(a.game.Moves) > 0 {
		last := a.game.Moves[len(a.game.Moves)-1]
		a.board.SetLastMove(last.From, last.To)
	} else {
		a.board.SetLastMove(-1, -1)
	}
	// 曲线同步回退到当前着数
	keep := a.curve.Points()[:0]
	for _, p := range a.curve.Points() {
		if p.Ply <= len(a.game.Moves) {
			keep = append(keep, p)
		}
	}
	a.curve.SetPoints(keep)
	a.setStatusInfo(fmt.Sprintf("已撤销 %d 步", n))
	a.afterPositionChange()
}

func (a *App) resetGame() {
	// 同上：对战中重开会把显示局面清空，与正在进行的对战打架。
	if a.matchRunning() {
		a.toast("对战进行中不能开新局，请先终止对战")
		return
	}
	a.game = rules.NewGame()
	a.viewing = -1
	a.lastEnding = ""
	a.moveRows = nil
	a.moveInput.SetText("")
	if a.pasteArea != nil {
		a.pasteArea.SetText("")
	}
	a.curve.SetPoints(nil)
	a.board.SetLastMove(-1, -1)
	a.board.SetGame(a.game)
	if a.moveList != nil {
		a.moveList.Refresh()
	}
	a.setStatusInfo("已重开：局面回到初始局面，曲线已清空")
	a.requestAnalysis()
}

// applySequence 从当前局面的起点（默认初始局面）重放一段 UCI 着法序列。
//
// 这是「坐标输入」与「粘贴输入」共用的实现：永远从头重放，因此无论用户粘贴多少次、
// 中间是否出错，局面都与第三方软件一致。
func (a *App) applySequence(text string, replace bool) {
	moves, skipped := notation.ParseMoveList(text)
	if len(moves) == 0 {
		if len(skipped) > 0 {
			a.setStatusInfo("未识别到 UCI 着法：" + strings.Join(skipped, " "))
			a.toast("未识别到 UCI 着法，请检查输入")
		}
		return
	}
	g, err := rules.NewGameFromFEN(a.game.StartFEN)
	if err != nil {
		g = rules.NewGame()
	}
	applied := 0
	var failMsg string
	for _, m := range moves {
		if err := g.TryMove(m); err != nil {
			failMsg = fmt.Sprintf("第 %d 步 %s 非法：%v", applied+1, m.String(), err)
			break
		}
		applied++
	}
	a.game = g
	a.viewing = -1
	a.lastEnding = ""
	a.board.SetGame(g)
	if len(g.Moves) > 0 {
		last := g.Moves[len(g.Moves)-1]
		a.board.SetLastMove(last.From, last.To)
	} else {
		a.board.SetLastMove(-1, -1)
	}
	if replace {
		a.curve.SetPoints(nil)
	}
	msg := fmt.Sprintf("已应用 %d 步着法", applied)
	if failMsg != "" {
		msg += "；" + failMsg
	}
	if len(skipped) > 0 {
		msg += fmt.Sprintf("；忽略 %d 个无法识别的记号", len(skipped))
	}
	a.setStatusInfo(msg)
	a.afterPositionChange()
}

func (a *App) toggleEditMode() {
	// 【缺陷修复】对战进行中不允许编辑局面：对战的着法由 Runner 的事件流驱动，
	// 用户在这里摆子只会让显示与对战实际局面不一致（下一着回来就被覆盖），
	// 看着像「摆了没用」。同类软件（象棋巫师）对局中也锁住局面编辑。
	if !a.editMode && a.matchRunning() {
		a.toast("对战进行中不能编辑局面，请先终止对战")
		return
	}
	a.editMode = !a.editMode
	a.board.SetEditMode(a.editMode)
	if a.editPalette != nil {
		if a.editMode {
			a.editPalette.Show()
			a.setStatusInfo("已进入手动摆盘模式")
		} else {
			a.editPalette.Hide()
			a.setStatusInfo("已退出手动摆盘模式")
		}
	}
	// 【缺陷修复】这里原来自己拼了一套交互条件：
	//     a.board.SetInteractive(a.curMode == "bridge" && !a.editMode && a.viewing < 0)
	// 它没有「人机对弈 + 轮到人类」这一支 —— 于人机对弈里进出一次「手动摆盘」之后，
	// 棋盘就永远点不动了（人明明该走棋），只有切模式或再走一步才能恢复。
	// 交互规则只有一处真源（canBoardAcceptInput），这里必须复用它。
	a.refreshBoardInteractive()
	if !a.editMode {
		a.requestAnalysis()
	}
}

// ---------------------------------------------------------------------------
// 引擎生命周期
// ---------------------------------------------------------------------------

func (a *App) restartAnalysisEngine() {
	// 【并发缺陷修复】这个方法会被多处异步调用（启动、切引擎、关/开引擎、满配开关…）。
	// 两次调用重叠时，两边各 NewClient+Start 一个进程，而 anaClient 只留最后一个：
	// 先起来的那个变成没人管的野进程，一直占着 CPU 与内存直到程序退出
	// （满配 12 线程 / 4GB 哈希时尤其明显）。这里串行化：后到的排队，而不是并排再起一个。
	// 调用方全部是 goroutine（没有任何一处从主线程直接调），所以阻塞在这里是安全的。
	a.anaRestartMu.Lock()
	defer a.anaRestartMu.Unlock()

	if a.engineOff.Load() {
		return // 用户已手动关闭引擎，不要偷偷重启
	}
	// 【并发缺陷修复】restartAnalysisEngine 是从 `go a.restartAnalysisEngine()` 启动的，
	// 而 Fyne 要求所有控件改动必须发生在主线程。原来这一行直接改 canvas.Text，
	// 属于跨线程改 UI —— 会随机出现界面撕裂甚至崩溃。
	// 用 uiDoWait（= DoAndWait）而不是 fyne.Do：后者不等，界面写入的先后没有保证。
	a.uiDoWait(func() { a.setEngineState("引擎：正在启动…", colForeDim) })
	a.anaMu.Lock()
	old := a.anaClient
	a.anaClient = nil
	a.anaMu.Unlock()
	if old != nil {
		old.Quit(1500 * time.Millisecond)
	}

	e := a.Reg().Find(a.cfg.SelfEngine)
	if e == nil {
		a.uiDoWait(func() {
			a.setEngineState("引擎：未配置（请到「引擎管理」选择己方引擎）", colErr)
			a.best.SetStatus("引擎未就绪，无法分析")
		})
		return
	}
	entry := *e
	c := engine.NewClient(entry.Path)
	if err := c.Start(8 * time.Second); err != nil {
		c.Kill()
		a.uiDoWait(func() {
			a.setEngineState("引擎：启动失败", colErr)
			a.setStatusInfo("引擎启动失败：" + err.Error())
			dialog.ShowError(fmt.Errorf("己方引擎启动失败：\n%w", err), a.win)
		})
		return
	}
	a.applyEngineParams(c)
	if err := c.IsReady(20 * time.Second); err != nil {
		a.uiDoWait(func() {
			a.setEngineState("引擎：isready 无响应", colErr)
			a.setStatusInfo("引擎 isready 无响应：" + err.Error())
		})
	}
	a.anaMu.Lock()
	a.anaClient = c
	a.anaMu.Unlock()
	a.uiDoWait(func() {
		a.setEngineState(fmt.Sprintf("引擎：%s（%s）就绪", c.Name(), c.Protocol()), colOK)
		a.rebuildDynamicParams()
		a.requestAnalysis()
	})
	// 【缺陷修复】新进程 = 优先级/亲和性回到默认（Normal + 全核）。
	// 引擎因为任何原因重启（程序启动、切引擎、关/开引擎、满配开关）都在这里补一次，
	// 这样不会出现"状态栏说已绑核、实测没绑"，也不会让 15 个线程去抢界面那点时间片。
	if a.cfg.MaxStrength {
		a.ApplyEngineProcessTuning()
	}
}

// applyEngineParams 把配置→引擎参数下发给引擎（不支持的参数自动跳过）。
func (a *App) applyEngineParams(c *engine.Client) {
	set := func(name, val string) {
		if _, ok := c.FindOption(name); ok {
			_ = c.SetOption(name, val)
		}
	}
	set("Threads", fmt.Sprint(a.cfg.Threads))
	set("Hash", fmt.Sprint(a.cfg.Hash))
	set("MultiPV", fmt.Sprint(a.cfg.MultiPV))
	if a.cfg.SyzygyPath != "" {
		set("SyzygyPath", a.cfg.SyzygyPath)
	}
	prefix := a.cfg.SelfEngine + "|"
	for k, v := range a.cfg.EngineParams {
		name := strings.TrimPrefix(k, prefix)
		if name == "" || name == k {
			continue
		}
		set(name, v)
	}
}

func (a *App) rebuildDynamicParams() {
	a.anaMu.Lock()
	c := a.anaClient
	a.anaMu.Unlock()
	if c == nil || a.params == nil {
		return
	}
	opts := c.Options()
	dyn := make([]dynOption, 0, len(opts))
	for _, o := range opts {
		dyn = append(dyn, dynOption{
			Name: o.Name, Type: o.Type, Default: o.Default,
			Min: o.Min, Max: o.Max, Vars: o.Vars,
		})
	}
	prefix := a.cfg.SelfEngine + "|"
	a.params.SetDynamicOptions(dyn,
		func(name string) string { return a.cfg.EngineParams[prefix+name] },
		func(name, value string) {
			a.cfg.EngineParams[prefix+name] = value
			if cl := a.analysisClient(); cl != nil {
				_ = cl.SetOption(name, value)
			}
		})
}

func (a *App) analysisClient() *engine.Client {
	a.anaMu.Lock()
	defer a.anaMu.Unlock()
	return a.anaClient
}

// ---------------------------------------------------------------------------
// 自动思考流水线
// ---------------------------------------------------------------------------

func (a *App) currentLimit() engine.Limit {
	// 「立即出招」：这一次用极短限时，让引擎把当前最好的一步立刻交出来
	if a.quickMove {
		a.quickMove = false
		return engine.Limit{Mode: engine.LimitMoveTime, MoveTimeMS: 150}
	}
	// 【v1.6.11】无限分析：一直算到用户停止（**分析模式**用）。
	//
	// 【缺陷修复】无限分析不能带进人机对弈：无限 = 引擎不收到 stop 就不回 bestmove，
	// 轮到电脑时它永远算不完 → 永远不交招 → 永远轮不到人 → 棋盘一直锁着。
	// 所以人机对弈里把它降级成「每步限时」（取用户设的每步限时，最少 1 秒），
	// 保证电脑一定会走棋；要纯粹算到底请切回分析模式（菜单里也这么标注）。
	if a.cfg.TimeMode == config.TimeModeInfinite {
		if a.curMode == "human" {
			ms := a.cfg.MoveTimeMS
			if ms < 1000 {
				ms = 1000
			}
			return engine.Limit{Mode: engine.LimitMoveTime, MoveTimeMS: ms}
		}
		return engine.Limit{Infinite: true}
	}
	if a.cfg.TimeMode == config.TimeModeDepth {
		return engine.Limit{Mode: engine.LimitDepth, Depth: a.cfg.Depth}
	}
	return engine.Limit{Mode: engine.LimitMoveTime, MoveTimeMS: a.cfg.MoveTimeMS}
}

// requestAnalysis 请求对当前局面做一次思考（局面输入后自动调用，无需点「开始」）。
func (a *App) requestAnalysis() {
	if a.curMode == "match" || a.editMode {
		return
	}
	if a.analysisClient() == nil {
		return
	}
	// 【局面分析以局面为准】正在查看历史局面时，分析的就是那个局面。
	g := a.game
	if a.viewGame != nil {
		g = a.viewGame
	}
	a.anaGame = g

	// 【缓存】同一局面分析过就直接回放结果，绝不重新开一次引擎搜索。
	// 这是「点击记谱跳转很卡」的正解：跳转只是切局面，不该触发新的思考。
	key := a.anaKey(g)
	a.anaKeyCur = key
	if cached, ok := a.anaCacheGet(key); ok {
		a.renderResult(cached, true)
		a.setEngineState("引擎：该局面已有分析结果（缓存回放，未重新思考）", colForeDim)
		// 【缺陷修复 / 已复现】人机对弈里"缓存回放"同样必须把电脑那一步走出来。
		//
		// 原来这里直接 return：只要轮到电脑走的那一步局面已经算过（悔棋重走、
		// 重新粘贴同一段着法序列、引擎重启后又回到同一局面），就永远不交招——
		// 棋盘永久锁死、引擎还在空转，界面上连"在思考"都没有。
		// 复现命令与前后证据见 _verify\r4-cachelock.err。
		// applyEngineBestMove 内部只对 human 模式生效，别的模式是空操作。
		a.applyEngineBestMove(cached.BestMove)
		return
	}

	// 引擎被用户关掉时不再发搜索请求；缓存层在上面，所以算过的局面照样能看
	if a.engineOff.Load() {
		a.setEngineState("引擎：已关闭（点「启动引擎」恢复）", colWarn)
		return
	}
	if a.analysisClient() == nil {
		return
	}

	uci := make([]string, 0, len(g.Moves))
	for _, m := range g.Moves {
		uci = append(uci, m.String())
	}
	req := analysisRequest{
		gen: a.anaGen.Add(1),
		pos: engine.Position{
			Startpos: g.StartFEN == rules.StartFEN,
			FEN:      g.StartFEN,
			Moves:    uci,
		},
		limit: a.currentLimit(),
		key:   key,
	}
	a.analyzing.Store(true)
	select {
	case a.anaReq <- req:
	default:
		// 队列已满：丢弃最旧请求再放入最新请求（分析永远以最新局面为准）
		select {
		case <-a.anaReq:
		default:
		}
		a.anaReq <- req
	}
}

// startAnalysisLoop 启动分析调度 goroutine：同一时刻只有一个 Analyze 在跑，
// 新请求到达时先取消旧请求并等它真正结束，避免命令重入。
func (a *App) startAnalysisLoop() {
	go func() {
		var pending *analysisRequest
		for {
			if pending == nil {
				select {
				case req := <-a.anaReq:
					pending = &req
				case <-a.closing:
					return
				}
				continue
			}
			cur := *pending
			pending = nil
			next, stop := a.runAnalysisOnce(cur)
			if stop {
				return
			}
			pending = next
		}
	}()
}

// runAnalysisOnce 执行一次思考。返回 (下一个待处理请求, 是否退出)。
//
// 三种出口：
//   - 思考正常结束      → 渲染结果，无下一个请求
//   - 等待期间到达新请求 → 先 cancel 并等旧分析真正返回（释放引擎 busy 标志），
//     把它作为下一个请求返回
//   - 程序退出          → cancel 并等待，通知调用方退出
func (a *App) runAnalysisOnce(cur analysisRequest) (*analysisRequest, bool) {
	client := a.analysisClient()
	if client == nil || !client.Alive() {
		a.analyzing.Store(false)
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan analysisResult, 1)
	go func() {
		res, err := client.Analyze(ctx, cur.pos, cur.limit, nil)
		done <- analysisResult{gen: cur.gen, res: res, err: err, key: cur.key}
	}()

	select {
	case r := <-done:
		a.analyzing.Store(false)
		a.deliverAnalysis(r)
		return nil, false
	case req := <-a.anaReq:
		cancel()
		<-done
		return &req, false
	case <-a.closing:
		cancel()
		<-done
		return nil, true
	}
}

func (a *App) deliverAnalysis(r analysisResult) {
	if r.gen != a.anaGen.Load() {
		return // 过期结果
	}
	fyne.Do(func() {
		if r.err != nil {
			if r.err == context.Canceled {
				return
			}
			a.setEngineState("引擎异常", colErr)
			a.setStatusInfo("引擎分析出错：" + r.err.Error())
			a.best.SetStatus("引擎分析出错：" + r.err.Error())
			return
		}
		a.anaCachePut(r.key, r.res) // 结果进缓存，下次跳回这个局面直接回放
		a.renderResult(r.res, true)
		a.applyEngineBestMove(r.res.BestMove)
	})
}

// renderResult 把一次思考的结果渲染到界面。final=false 时是思考中的实时预览。
func (a *App) renderResult(res engine.Result, final bool) {
	lines := res.Lines
	if len(lines) == 0 && res.BestMove == "" {
		return
	}
	if len(lines) > a.cfg.MultiPV {
		lines = lines[:a.cfg.MultiPV]
	}

	// 用「实际被分析的局面」，而不是对局最新局面；否则看历史时主变例记谱会对不上
	anaGame := a.anaGame
	if anaGame == nil {
		anaGame = a.game
	}
	board := anaGame.Board
	side := board.Side

	// 概率：温度 softmax
	values := make([]float64, 0, len(lines))
	for _, l := range lines {
		values = append(values, l.Score.Value())
	}
	probs := analytics.Softmax(values, float64(a.cfg.Temperature))

	candChinese := make([]string, 0, len(lines))
	candUCI := make([]string, 0, len(lines))
	candScore := make([]string, 0, len(lines))
	candBar := make([]float64, 0, len(lines))
	candPV := make([]string, 0, len(lines))
	candMeta := make([]string, 0, len(lines))
	for i, l := range lines {
		uci := ""
		chinese := "—"
		if len(l.PV) > 0 {
			uci = l.PV[0]
			if m, ok := notation.UCIToMove(uci); ok {
				chinese = notation.ToChinese(board, m)
			}
		}
		candChinese = append(candChinese, chinese)
		candUCI = append(candUCI, uci)
		candScore = append(candScore, scoreText(l.Score))
		candBar = append(candBar, probs[i]/100.0)
		// 这一招之后的**全部**后续走法（中文）：点开候选行时才显示。
		// 用户要求「把引擎分析的所有后续着法都显示出来」，所以不再截断——
		// 引擎给多长就显示多长（超长时由 Label 自动换行）。
		candPV = append(candPV, strings.Join(a.pvChinese(l.PV, board, len(l.PV)), " "))
		// 这一条候选对应的引擎统计，用大白话写清楚（深度 / 用时 / 速度 / 节点）
		candMeta = append(candMeta, candMetaLine(l))
	}
	// 轮到红方走 → 变招着法用红字（用户要求：红方走时文字变红，黑方走保持正文色）
	a.cands.SetSideToMove(board.Side == rules.Red)
	a.cands.SetCandidates(candChinese, candUCI, probs, candScore, candPV, candMeta)

	// 最佳着法：优先用 bestmove，其次用 multipv 1 的 pv[0]
	bestUCI := res.BestMove
	bestChinese := "—"
	if bestUCI != "" {
		if m, ok := notation.UCIToMove(bestUCI); ok {
			bestChinese = notation.ToChinese(board, m)
		}
	} else if len(candUCI) > 0 {
		bestUCI = candUCI[0]
		bestChinese = candChinese[0]
	}
	// 【自检】引擎返回的首选着法必须在「被分析的那个局面」里合法。
	// 不合法说明引擎/局面/坐标换算哪里对不上——这正是用户反馈的
	// 「弹出来的走法感觉输出错了」最可能的表现形态，必须显式暴露而不是照显。
	bestLegal := true
	if bestUCI != "" {
		if mv, ok := notation.UCIToMove(bestUCI); ok {
			bestLegal = anaGame.IsLegal(mv)
		} else {
			bestLegal = false
		}
	}
	a.lastBestUCI = bestUCI
	a.lastBestChinese = bestChinese

	redWR := analytics.RedWinRate(res.Score, side)
	var pvChinese []string
	pvText := ""
	if len(lines) > 0 {
		// 主变例同样不截断：用户要求把引擎给出的后续着法全部显示出来
		pvChinese = a.pvChinese(lines[0].PV, board, len(lines[0].PV))
		pvText = strings.Join(lines[0].PV, " ")
	}
	if a.think != nil {
		a.think.SetLive(res.Depth, res.TimeMS, res.NPS)
	}
	// 同一局面同时丢给参考引擎（Kibitzer）：结果回来填到下面的对比卡
	a.requestKibitzer(anaGame, res)
	// 哈希占用偏高时给一条可操作提示（v1.6.8 / 头脑风暴第 8 条）
	if res.HashFull >= 900 {
		a.setStatusInfo("哈希表已接近用满（占用 " + fmt.Sprint(res.HashFull/10) + "%），可在「引擎设置」里把哈希（MB）调大")
	}
	a.best.SetBest(bestChinese, bestUCI, redWR, scoreText(res.Score),
		res.Depth, res.Nodes, res.NPS, res.TimeMS, res.HashFull, pvText, pvChinese)
	// 评估条：一眼看出优劣（同类软件分析盘的标配）
	if a.eval != nil {
		scoreTxt := scoreText(res.Score)
		a.eval.Set(redWR, res.Score.Valid, scoreTxt)
	}
	// 最佳着法箭头（TCHESS 的「棋步提示」）：只在着法合法时画
	if bestLegal && bestUCI != "" {
		if mv, ok := notation.UCIToMove(bestUCI); ok {
			a.board.SetHint(mv.From, mv.To)
		}
	} else {
		a.board.SetHint(-1, -1)
	}
	a.lastPVText = pvText
	if !bestLegal {
		a.best.SetStatus(fmt.Sprintf(
			"⚠ 引擎给出的着法 %s 在当前局面不合法（中文记谱 %s）——请把这一行报给开发者",
			bestUCI, bestChinese))
	} else {
		a.best.SetStatus(fmt.Sprintf("思考限制：%s", a.currentLimit().Label()))
	}

	// 状态栏
	if a.stDepth != nil {
		a.stDepth.Text = fmt.Sprintf("深度 %d", res.Depth)
		a.stNodes.Text = "节点 " + commas(res.Nodes)
		a.stTime.Text = fmt.Sprintf("耗时 %.1fs", float64(res.TimeMS)/1000)
		if res.Score.Valid {
			a.stWin.Text = fmt.Sprintf("红方胜率 %.2f%%", redWR)
		} else {
			a.stWin.Text = "红方胜率 —"
		}
		a.stDepth.Refresh()
		a.stNodes.Refresh()
		a.stTime.Refresh()
		a.stWin.Refresh()
	}

	// 【走势曲线】在**被分析的那个着数**上记一个点。
	// AddPoint 对同一着数是覆盖语义，所以：
	//   - 正常走棋 → 每个着数留下一个点，连成一条走势线；
	//   - 点击记谱跳转 → 从缓存回放，同样会在那个着数上补点/更新点，
	//     于是「跳到哪一步，曲线那一步的优劣就显出来」。
	// 原来只在 viewing<0 且着数相等时才加点，跳转时不加点，曲线永远只有几个孤立的点。
	if final {
		a.curve.AddPoint(len(anaGame.Moves), redWR)
		a.setEngineState(fmt.Sprintf("引擎：%s 已完成（深度 %d）", a.engineCurrentName(), res.Depth), colOK)
	}
}

func (a *App) engineCurrentName() string {
	if c := a.analysisClient(); c != nil {
		return c.Name()
	}
	return "引擎"
}

func scoreText(s analytics.Score) string {
	if !s.Valid {
		return "—"
	}
	if s.Mate {
		if s.N > 0 {
			return fmt.Sprintf("将死 %d 步", s.N)
		}
		return fmt.Sprintf("被杀 %d 步", -s.N)
	}
	if s.CP > 0 {
		return fmt.Sprintf("+%d", s.CP)
	}
	return fmt.Sprint(s.CP)
}

// pvChinese 把主变例的前 n 步转成中文记谱。
func (a *App) pvChinese(pv []string, board *rules.Board, n int) []string {
	b := board.Clone()
	out := make([]string, 0, n)
	for _, u := range pv {
		if len(out) >= n {
			break
		}
		m, ok := notation.UCIToMove(u)
		if !ok {
			break
		}
		out = append(out, notation.ToChinese(b, m))
		b.Apply(m)
	}
	return out
}

// startProgressTicker 每 150ms 把引擎的最新 info 快照推到界面，
// 实现「思考中显示实时深度与胜率预览」。
// humanTurnWatchdog 兜底：人机对弈里「轮到电脑、引擎在跑、却没有任何搜索在途」必须自愈。
//
// 为什么需要：用户实测过棋盘永久锁死（引擎空转、棋盘点不动）。已定位的根因是
// 「命中局面缓存就只回放、不交招」，但同类路径不止一条（引擎还在启动时请求被丢弃、
// 结果因换代被判过期…）。这些路径的终态是同一个，所以在这里统一兜住：
// 连续 3 次检查都成立就补发一次分析请求 —— 把"永久锁死"降级成"最多多等 3 秒"。
func (a *App) humanTurnWatchdog() {
	if a.wdTick.Add(1)%7 != 0 { // 150ms × 7 ≈ 每秒判一次
		return
	}
	// 用等待版：一是让「判定 → 补发请求」这段逻辑与主线程的顺序确定，二是天然限流
	// （主线程忙的时候 ticker 自然慢下来，不会把闭包堆在队列里）。
	a.uiDoWait(func() {
		if a.curMode != "human" || a.viewing >= 0 || a.editMode || a.engineOff.Load() {
			a.wdIdle, a.wdRetry = 0, 0
			return
		}
		// 人类能走了 / 对局结束 = 真的前进了，补发次数清零
		if a.isHumanTurn() || a.lastEnding != "" {
			a.wdIdle, a.wdRetry = 0, 0
			return
		}
		if a.analyzing.Load() || a.analysisClient() == nil {
			a.wdIdle = 0
			return
		}
		a.wdIdle++
		if a.wdIdle < 3 {
			return
		}
		a.wdIdle = 0
		// 补发也要有上限：引擎真坏了（客户端活着但每次都报错）时，
		// 无限重试只会把状态栏刷满，不如停下来把该做什么说清楚。
		if a.wdRetry >= 5 {
			if a.wdRetry == 5 {
				a.wdRetry++
				a.setStatusInfo("看门狗：已连续 5 次重新发起思考仍未取得着法——请检查引擎（菜单「引擎」里的引擎管理，或重新启动引擎），也可用「立即出招」催一步")
			}
			return
		}
		a.wdRetry++
		a.setStatusInfo("看门狗：轮到电脑走棋但分析没在跑，已自动重新发起思考")
		a.requestAnalysis()
	})
}

func (a *App) startProgressTicker() {
	go func() {
		tk := time.NewTicker(150 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-a.closing:
				return
			case <-tk.C:
				a.humanTurnWatchdog()
				// 人机对弈也要实时预览（原来只判 bridge，人机模式下深度/胜率一直不动）
				if !a.analyzing.Load() || (a.curMode != "bridge" && a.curMode != "human") {
					continue
				}
				c := a.analysisClient()
				if c == nil {
					continue
				}
				lines := c.Snapshot()
				if len(lines) == 0 {
					continue
				}
				res := engine.Aggregate("", lines)
				fyne.Do(func() {
					if a.analyzing.Load() {
						a.renderResult(res, false)
						// 【缺陷修复】必须用「被分析的那个局面」的走子方。
						// 原来固定取 a.game（对局最新局面），当用户点开历史局面查看时，
						// 被分析的是历史局面，两者走子方可能相反 —— 胜率预览会红黑颠倒。
						side := a.game.Board.Side
						if a.anaGame != nil {
							side = a.anaGame.Board.Side
						}
						a.curve.SetPreview(analytics.RedWinRate(res.Score, side), true)
					}
				})
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// 复制
// ---------------------------------------------------------------------------

func (a *App) copyBestMove() {
	if a.lastBestUCI == "" {
		a.toast("还没有最佳着法：请先输入局面并等待引擎返回")
		return
	}
	a.win.Clipboard().SetContent(a.lastBestUCI)
	a.toast("已复制：" + a.lastBestChinese + "（" + a.lastBestUCI + "）")
	a.setStatusInfo("已复制最佳着法 " + a.lastBestUCI + " 到剪贴板，可直接粘贴回第三方软件")
}

func (a *App) copyPV() {
	pv := a.lastPVText
	if pv == "" {
		a.toast("还没有主变例")
		return
	}
	a.win.Clipboard().SetContent(pv)
	a.toast("已复制主变例：" + pv)
}

// onParamsChanged 在参数面板任意改动后调用：保存 + 立即 setoption + 重新分析。
func (a *App) onParamsChanged() {
	th, h, d, ms, mp, tp, dm, sz := a.params.Values()
	a.cfg.Threads, a.cfg.Hash, a.cfg.Depth = th, h, d
	a.cfg.MoveTimeMS, a.cfg.MultiPV, a.cfg.Temperature = ms, mp, tp
	a.cfg.SyzygyPath = sz
	// 【缺陷修复】面板只有一个「固定深度」勾选，它表达不了"无限分析"：
	// 原来这里无条件用勾选值覆盖 TimeMode，于是「无限分析」只要被这块面板碰一下
	// （哪怕只是拖线程/哈希滑块）就被改回每步限时。无限/非无限的切换只由
	// 「思考设置」那个三选一负责（那里有明确的三项），这里不越权。
	if a.cfg.TimeMode != config.TimeModeInfinite {
		if dm {
			a.cfg.TimeMode = config.TimeModeDepth
		} else {
			a.cfg.TimeMode = config.TimeModeMoveTime
		}
	}
	a.cands.SetTemperature(tp)

	if c := a.analysisClient(); c != nil {
		if _, ok := c.FindOption("Threads"); ok {
			_ = c.SetOption("Threads", fmt.Sprint(th))
		}
		if _, ok := c.FindOption("Hash"); ok {
			_ = c.SetOption("Hash", fmt.Sprint(h))
		}
		if _, ok := c.FindOption("MultiPV"); ok {
			_ = c.SetOption("MultiPV", fmt.Sprint(mp))
		}
		if sz != "" {
			if _, ok := c.FindOption("SyzygyPath"); ok {
				_ = c.SetOption("SyzygyPath", sz)
			}
		}
	}
	// 拖动滑块会高频触发，这里做 350ms 防抖后重新分析
	if a.paramTimer != nil {
		a.paramTimer.Stop()
	}
	a.paramTimer = time.AfterFunc(350*time.Millisecond, func() {
		fyne.Do(func() { a.requestAnalysis() })
	})
}
