package ui

import (
	"time"

	"fyne.io/fyne/v2/widget"
)

// 本文件实现「关闭引擎 / 启动引擎」开关。
//
// 用户需求：测试期间要能把引擎关掉（引擎满配 16 线程跑满 CPU，
// 测试界面时既费电又抢时间片）。
//
// 语义：
//   - 关闭：立刻 quit 引擎进程、作废在途结果，并且**不再自动重启、不再发搜索请求**；
//     已经缓存过分析结果的局面仍然照常显示（缓存层在引擎之外，不受影响）。
//   - 启动：重新拉起引擎并立刻对当前局面思考一次。

// CloseEngine 关闭分析引擎。
// refreshBoardInteractiveSafe 关/开引擎后同步一次棋盘可交互状态。
//
// 关引擎会改变「人机对弈里能不能走对面那方」的判定（见 canBoardAcceptInput），
// 所以两个开关收尾都要刷新，否则要等到下一步棋才生效。
func (a *App) refreshBoardInteractiveSafe() {
	if a.board != nil {
		a.refreshBoardInteractive()
	}
}

func (a *App) CloseEngine() {
	a.engineOff.Store(true)

	// 作废所有在途请求：结果回来后 gen 对不上会被丢弃
	a.anaGen.Add(1)

	a.anaMu.Lock()
	c := a.anaClient
	a.anaClient = nil
	a.anaMu.Unlock()

	a.analyzing.Store(false)
	if c != nil {
		c.Quit(1200 * time.Millisecond)
	}

	a.setEngineState("引擎：已关闭（点「启动引擎」恢复）", colWarn)
	a.best.SetStatus("引擎已关闭；已缓存过的局面仍可查看（含记谱跳转）")
	a.refreshEngineButton()
	a.refreshBoardInteractiveSafe()
}

// OpenEngine 重新启动分析引擎。
func (a *App) OpenEngine() {
	a.engineOff.Store(false)
	a.refreshEngineButton()
	a.refreshBoardInteractiveSafe()
	go a.restartAnalysisEngine()
}

// ToggleEngine 工具栏按钮：按当前状态关闭或启动引擎。
func (a *App) ToggleEngine() {
	if a.engineOff.Load() {
		a.OpenEngine()
		return
	}
	a.CloseEngine()
}

// refreshEngineButton 同步按钮文案。
func (a *App) refreshEngineButton() {
	if a.btnEngine == nil {
		return
	}
	if a.engineOff.Load() {
		a.btnEngine.SetText("启动引擎")
	} else {
		a.btnEngine.SetText("关闭引擎")
	}
	a.btnEngine.Refresh()
}

// newEngineToggleButton 创建工具栏上的引擎开关。
func (a *App) newEngineToggleButton() *widget.Button {
	a.btnEngine = widget.NewButton("关闭引擎", func() { a.ToggleEngine() })
	return a.btnEngine
}
