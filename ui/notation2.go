package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"xiangqi/notation"
	"xiangqi/rules"
)

// 本文件实现「记谱」面板（v1.5 增量更新版）。
//
// 需求（用户）：
//   - 每一步走后都要记录下来；
//   - 一行 = 一个回合，左红右黑；
//   - 点击某一步，棋盘跳到那一步之后，右侧分析也以那个局面为准；
//   - 【性能】走一步之后**不能有明显的卡顿**。
//
// 性能设计：
//
//	v1.4 每走一步就把整张记谱表 RemoveAll 再重建，一局 60 步要销毁并新建
//	180+ 个控件（含文字测量与布局）——这正是用户反馈的「走棋后明显卡顿」，
//	而且界面线程被占住还会让走子动画一顿一顿、看起来「走子太慢」。
//
//	现在改成增量更新：
//	  - 走一步 → 只更新/追加最后一行（O(1)）；
//	  - 撤销、重开、粘贴整段序列、摆盘 → 才整表重建；
//	  - 跳转历史局面 → 只切高亮，一个控件都不新建，也不销毁正在被点的按钮。

// moveRound 是一个回合：红方一步 + 黑方一步。
type moveRound struct {
	no        int
	redText   string
	blackText string
	redPly    int
	blackPly  int
	hasBlack  bool
}

// moveRowBtns 保存每一行两个按钮的引用，供高亮与增量更新使用。
type moveRowBtns struct {
	red, black       *moveCell
	redPly, blackPly int
}

// buildMoveListPanel 构建记谱面板。
func (a *App) buildMoveListPanel() fyne.CanvasObject {
	a.moveBox = container.NewVBox()
	a.moveScroll = container.NewVScroll(a.moveBox)
	// 最小高度压小：右栏面板多，谁的最小高度大，分栏比例就被谁钳住
	a.moveScroll.SetMinSize(fyne.NewSize(200, 88))
	a.moveBuiltFor = -1

	// 面板名与按钮名按同类软件（XQWizard 的「着法列表」页签 + 底部的
	// 「开局 / 终局 / 前进 / 后退」按钮组）统一。
	// 【v1.6】「粘贴棋谱…」按钮放在标题行右侧：原来那个常驻的多行输入框已改成对话框，
	// 入口留在这里 + 菜单「棋谱 → 粘贴棋谱…」，两处都能到。
	btnPaste := widget.NewButton("粘贴棋谱…", func() { a.ShowPasteDialog() })
	btnBack := widget.NewButton("终局", func() { a.jumpTo(-1) })
	btnFirst := widget.NewButton("开局", func() { a.jumpTo(0) })
	head := container.NewVBox(
		container.NewBorder(nil, nil, nil, btnPaste, sectionTitle("着法列表")),
		container.NewHBox(btnFirst, btnBack),
	)
	return cardBox(container.NewBorder(head, nil, nil, nil, a.moveScroll))
}

// moveRowText 生成第 ply 着（1-based）的显示文本。
func (a *App) moveRowText(ply int) string {
	i := ply - 1
	if i < 0 || i >= len(a.game.Moves) {
		return ""
	}
	return fmt.Sprintf("%s  %s",
		notation.ToChinese(a.game.BoardAt(i), a.game.Moves[i]), a.game.Moves[i].String())
}

// newMoveRow 创建一个回合行（红黑两个按钮都先建好，黑方没走时显示占位）。
func (a *App) newMoveRow(no int) fyne.CanvasObject {
	noText := canvas.NewText(fmt.Sprintf("%3d.", no), colForeDim)
	noText.TextSize = textSize(textLabel)
	// 序号列的宽度必须跟着字号档位走，否则「特大」档下 4 位数字会被 40px 的栅格切掉。
	// 【v1.6.2】行高 26 → 34：右栏改对半后着法列表这一栏又高又空，
	// 行高加大既填了空白，点击目标也更好点。
	noBox := container.NewGridWrap(fyne.NewSize(sz(40), sz(38)), noText)

	red := newMoveCell(rules.Red)
	black := newMoveCell(rules.Black)

	row := zebra(no, container.NewBorder(nil, nil, noBox, nil,
		container.NewGridWithColumns(2, red, black)))

	idx := len(a.moveBtns)
	a.moveBtns = append(a.moveBtns, moveRowBtns{red: red, black: black})
	// 回调里只做「切局面 + 切高亮」，绝不重建控件，
	// 否则会把自己这个正在被点的按钮销毁掉（v1.4 记谱点不动的根因）。
	// ply <= 0 时宁可不跳，也绝不跳回开局（把「数据没准备好」和
	// 「用户想看开局」区分开，避免再出现整盘乱跳）
	red.OnTap = func() {
		if p := a.moveBtns[idx].redPly; p > 0 {
			a.jumpTo(p)
		}
	}
	black.OnTap = func() {
		if p := a.moveBtns[idx].blackPly; p > 0 {
			a.jumpTo(p)
		}
	}
	return row
}

// rebuildMoveList 整表重建（撤销 / 重开 / 粘贴序列 / 摆盘后用）。
func (a *App) rebuildMoveList() {
	if a.moveBox == nil {
		return
	}
	a.moveBox.RemoveAll()
	a.moveBtns = a.moveBtns[:0]
	a.moveRounds = a.moveRounds[:0]

	// 【缺陷修复】不能再用「着数奇偶」判断红黑。
	// 从手动摆盘 / 自定义 FEN 开始时，可能是**黑方先走**，那时第 1 着是黑方的，
	// 用 i%2 归行会把红黑两列整个错位、跳转 ply 也会指错。
	// 这里的唯一依据是「走第 i 步之前，局面轮到谁」。
	n := len(a.game.Moves)
	row := -1
	for i := 0; i < n; i++ {
		side := a.game.BoardAt(i).Side
		ply := i + 1
		if side == rules.Red || row < 0 {
			// 红方着法开新一回合；若黑方先走，第一行也要先建出来
			no := len(a.moveRounds) + 1
			a.moveBox.Add(a.newMoveRow(no))
			a.moveRounds = append(a.moveRounds, moveRound{no: no})
			row = len(a.moveRounds) - 1
		}
		if side == rules.Red {
			a.moveRounds[row].redPly = ply
		} else {
			a.moveRounds[row].blackPly = ply
			a.moveRounds[row].hasBlack = true
		}
	}
	a.moveBuiltFor = n
	a.refreshMoveRows()
	if a.moveScroll != nil {
		a.moveScroll.ScrollToBottom()
	}
}

// syncMoveList 走一步之后调用：能增量就增量，否则退回整表重建。
func (a *App) syncMoveList() {
	if a.moveBox == nil {
		return
	}
	n := len(a.game.Moves)
	if a.moveBuiltFor < 0 || n != a.moveBuiltFor+1 {
		a.rebuildMoveList() // 撤销 / 跳变 / 批量应用
		return
	}

	ply := n
	side := a.game.BoardAt(ply - 1).Side // 走这一步的是哪一方（按局面判断，不看奇偶）
	if side == rules.Red || len(a.moveRounds) == 0 {
		a.moveBox.Add(a.newMoveRow(len(a.moveRounds) + 1))
		a.moveRounds = append(a.moveRounds, moveRound{no: len(a.moveRounds) + 1})
	}
	round := len(a.moveRounds) - 1
	if round < 0 || round >= len(a.moveBtns) {
		a.rebuildMoveList()
		return
	}
	if side == rules.Red {
		a.moveRounds[round].redPly = ply
		a.moveBtns[round].redPly = ply
	} else {
		a.moveRounds[round].blackPly = ply
		a.moveRounds[round].hasBlack = true
		a.moveBtns[round].blackPly = ply
	}
	a.moveBuiltFor = n
	a.refreshMoveRows()
	if a.moveScroll != nil {
		a.moveScroll.ScrollToBottom()
	}
}

// refreshMoveRows 按 moveRounds 把每行文字与高亮对齐（不新建控件）。
func (a *App) refreshMoveRows() {
	cur := len(a.game.Moves)
	if a.viewing >= 0 {
		cur = a.viewing
	}
	for i := range a.moveRounds {
		if i >= len(a.moveBtns) {
			break
		}
		r := a.moveRounds[i]
		// ★ 必须取指针：moveRowBtns 是结构体，写成 b := a.moveBtns[i] 只是拷贝，
		// 后面 b.redPly = ... 全丢在副本上，按钮里的 ply 永远是 0 ——
		// 表现就是「点记谱根本跳不过去」（ply 0 被回调里的保护挡掉了）。
		b := &a.moveBtns[i]
		if r.redPly > 0 {
			b.red.SetText(a.moveRowText(r.redPly))
			b.redPly = r.redPly
			b.red.SetHighlight(r.redPly == cur)
		}
		if r.hasBlack && r.blackPly > 0 {
			b.black.SetText(a.moveRowText(r.blackPly))
			b.blackPly = r.blackPly
			b.black.SetHighlight(r.blackPly == cur)
		} else {
			b.black.SetText("—")
			b.black.SetHighlight(false)
		}
	}
	a.moveBox.Refresh()
}

// highlightMoveRows 只切当前步的高亮（跳转时用，零控件创建）。
func (a *App) highlightMoveRows() { a.refreshMoveRows() }
