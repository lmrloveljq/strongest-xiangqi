package ui

import (
	"xiangqi/config"
	"xiangqi/notation"
	"xiangqi/rules"
)

// 本文件实现「人机对弈」模式与「局面对调」。
//
// 用户定义（原话）：
//
//	局面对调 = 我执红棋和 AI 下了几个回合后觉得劣势太大，改成我执黑棋、电脑执红棋，
//	**局面不变**，也不是新开一局，**下棋次序不变**（该红棋走就是红棋走）。
//	电脑对电脑（引擎对战）也要有这个功能。
//
// 所以「对调」只翻转一个字段（谁执哪一方），绝不碰局面、不碰着法历史、
// 也不碰「轮到谁走」——轮到红方就还是红方走。

// humanPlaysRed 人类是否执红。
func (a *App) humanPlaysRed() bool { return a.cfg.HumanSide != config.SideBlack }

// humanSideCN 人类执哪一方（中文，用于提示）。
func (a *App) humanSideCN() string {
	if a.humanPlaysRed() {
		return "红方"
	}
	return "黑方"
}

// engineSideCN 引擎执哪一方（中文）。
func (a *App) engineSideCN() string {
	if a.humanPlaysRed() {
		return "黑方"
	}
	return "红方"
}

// isHumanTurn 当前是否轮到人类走子（仅人机对弈模式下有意义）。
func (a *App) isHumanTurn() bool {
	if a.curMode != "human" {
		return false
	}
	return (a.game.Board.Side == rules.Red) == a.humanPlaysRed()
}

// canBoardAcceptInput 棋盘当前是否接受点击走子。
//
//	桥接分析：始终可以走（用户手动摆第三方软件的着法）
//	人机对弈：只在轮到人类时接受点击；轮到引擎时锁定，避免抢着走
//	引擎对战：只用于展示
func (a *App) canBoardAcceptInput() bool {
	if a.viewing >= 0 || a.editMode {
		return false
	}
	switch a.curMode {
	case "bridge", "human":
		// 【缺陷修复】引擎关闭时不再限制「只能走自己那方」：
		// 引擎已经不会应招，如果把引擎执手那边也锁住，盘面就彻底走不下去了。
		// 关引擎 = 纯手动摆棋/试走，两边都能动（用户要求）。
		if a.engineOff.Load() {
			return true
		}
		return a.curMode == "bridge" || a.isHumanTurn()
	default:
		return false
	}
}

// refreshBoardInteractive 把「棋盘是否接受点击」同步到棋盘控件。
func (a *App) refreshBoardInteractive() {
	a.board.SetInteractive(a.canBoardAcceptInput())
	a.board.SetBlockedHint(a.boardBlockedReason())
}

// boardBlockedReason 解释「棋盘为什么点不动」。
//
// 用户原话是「棋盘根本动不了」——点不动本身不算缺陷，点不动还不给理由才是：
// 对战模式本来就只用于展示、看历史局面本来就不能走，这两件事必须在状态栏说清楚。
func (a *App) boardBlockedReason() string {
	switch {
	case a.editMode:
		return "" // 摆盘模式下点击有效（走 edit 分支），不需要提示
	case a.viewing >= 0:
		return "正在查看历史局面，不能走子；点记谱最后一行或「回到当前局面」再走"
	case a.curMode == "match":
		return "引擎对战模式下棋盘只用于展示，不能直接走子；想自己走棋请点上方「人机对弈」或「分析模式」"
	case a.curMode == "human" && !a.engineOff.Load():
		return "轮到电脑走棋，请等它应招；想让它马上出招，用菜单「引擎」里的「立即出招」"
	}
	return "现在不能直接走子"
}

// syncFlip 按当前执子方同步棋盘朝向（人执黑 → 棋盘倒转，自己那方在下方）。
func (a *App) syncFlip() {
	if a.curMode == "human" {
		a.board.SetFlipped(!a.humanPlaysRed())
		return
	}
	a.board.SetFlipped(false)
}

// refreshSwapButton 刷新「交换行棋方」按钮的文案与可用状态。
func (a *App) refreshSwapButton() {
	if a.btnSwap == nil {
		return
	}
	switch a.curMode {
	case "human":
		// 文案直接说明「当前你执哪一方」，点一下就是对调
		a.btnSwap.SetText("交换行棋方（现执" + a.humanSideCN() + "）")
		a.btnSwap.Enable()
	case "match":
		a.btnSwap.SetText("交换行棋方（双方引擎换边）")
		a.btnSwap.Enable()
	default:
		a.btnSwap.SetText("交换行棋方")
		a.btnSwap.Disable()
	}
	a.btnSwap.Refresh()
}

// SwapSides 执行「局面对调」。
//
// 只翻转「谁执哪一方」，局面、着法历史、轮到谁走全部保持不变。
func (a *App) SwapSides() {
	switch a.curMode {
	case "human":
		if a.humanPlaysRed() {
			a.cfg.HumanSide = config.SideBlack
		} else {
			a.cfg.HumanSide = config.SideRed
		}
		a.SaveConfig()
		// 【翻转】不只是换边：棋盘要 180° 倒转，让自己那方始终在下方
		a.board.SetFlipped(!a.humanPlaysRed())
		a.refreshSwapButton()
		a.refreshBoardInteractive()
		a.updateStatusBar()
		msg := "已翻转：现在你执" + a.humanSideCN() + "（棋盘已倒转，你那方在下方）；" +
			"局面与走子次序都没变，轮到谁走还是谁走"
		a.toast(msg)
		a.setStatusInfo(msg)
		// 换边之后可能正好轮到引擎，立刻让它应招
		a.maybeEngineMove()

	case "match":
		a.matchMu.Lock()
		r := a.runner
		a.matchMu.Unlock()
		if r == nil {
			a.toast("对战还没开始，在菜单「对局」里点「开始对战」即可")
			return
		}
		r.SwapSides()
		msg := "已要求双方引擎中途换边：当前局面不变，下一着起由另一边引擎应招"
		a.toast(msg)
		a.setStatusInfo(msg)

	default:
		a.toast("「交换行棋方」只在人机对弈 / 引擎对战模式下有效")
	}
}

// maybeEngineMove 人机对弈里轮到引擎时，请求分析（结果回来后由
// applyEngineBestMove 自动走出最佳着法）。
func (a *App) maybeEngineMove() {
	// 引擎被用户关掉时不要再去请求分析：既不会有人应招，也会让状态栏报错。
	if a.engineOff.Load() {
		return
	}
	if a.curMode != "human" || a.viewing >= 0 || a.editMode {
		return
	}
	a.refreshBoardInteractive()
	if a.isHumanTurn() {
		return
	}
	a.setEngineState("引擎思考中…", colPrimary)
	a.requestAnalysis()
}

// applyEngineBestMove 人机对弈模式下把引擎的最佳着法自动走到棋盘上。
func (a *App) applyEngineBestMove(uci string) {
	if a.curMode != "human" || uci == "" || a.viewing >= 0 || a.editMode {
		return
	}
	if a.isHumanTurn() {
		return // 结果回来时局面可能已经变了（例如人类悔棋/对调了边）
	}
	m, ok := notation.UCIToMove(uci)
	if !ok {
		a.setStatusInfo("引擎返回的着法无法解析：" + uci)
		return
	}
	if err := a.game.TryMove(m); err != nil {
		a.setStatusInfo("引擎返回非法着法：" + err.Error())
		return
	}
	a.board.SetLastMove(m.From, m.To)
	a.afterMoveEffects(m)
	a.afterPositionChange()
}

// afterMoveEffects 走完一步后的动画与音效。
//
// 音效优先级：将军 > 吃子 > 走子（将军时同时提示「被将军」更醒目）。
// 用户要求：将军**只要音效不同**，不要红框闪烁——棋盘那边已经没有将军高亮了。
func (a *App) afterMoveEffects(m rules.Move) {
	a.board.AnimateMove(m.From, m.To)

	captured := false
	if n := len(a.game.Captured); n > 0 {
		captured = !a.game.Captured[n-1].IsEmpty()
	}
	switch {
	case a.game.Board.InCheck(a.game.Board.Side):
		playSound(2) // 将军：专用下行警示音
	case captured:
		playSound(1) // 吃子
	default:
		playSound(0) // 普通走子
	}
}
