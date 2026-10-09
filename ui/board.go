package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"xiangqi/rules"
)

// ============================================================================
// 棋盘几何：所有坐标换算的唯一来源（UI 其它地方不得自行推导）
// ============================================================================
//
// 棋盘按「格」为单位布局，一个格 = 一个交叉点的间距。整体尺寸（单位：格）：
//
//	宽度 = 8（8 个横向间隔） + 2×0.28（底板边距） + 2×0.50（坐标标注带） = 9.56 格
//	高度 = 9（9 个纵向间隔） + 2×0.28（底板边距） + 2×0.50（坐标标注带） = 10.56 格
//
// 交叉点坐标：
//
//	X(file) = 原点X + file × 格宽                  （file：0=a .. 8=i）
//	Y(rank) = 原点Y + (9 - rank) × 格高            （rank：0=红方底线 .. 9=黑方底线）
//
// 即屏幕第 0 行对应 rank 9（黑方底线），这正是 rules.ScreenRow 的换算。

const (
	// geomPadFrac 是底板边距（单位：格）。
	//
	// 必须 **大于棋子半径**（Layout 里 rad = cell*0.435），否则贴在 a/i 线与
	// rank 0/9 上的棋子会有一半探出底板之外——用户看到的「棋子没落在棋盘上」。
	// v1.3.0/v1.3.1 早期都是 0.28，比 0.435 小，所以四边最外侧的棋子本来就会
	// 探出底板约 0.155 格；配合光栅化漏乘 scale 的事故，看起来就更严重。
	// 现在取 0.47，给棋子外缘再留 0.035 格的余量。
	geomPadFrac = 0.47
	// v1.5：用户要求去掉棋盘四周 a~i / 0~9 坐标标注，标注带收成窄边距
	geomLabFrac = 0.10
	geomUnitW   = 8.0 + 2*(geomPadFrac+geomLabFrac)
	geomUnitH   = 9.0 + 2*(geomPadFrac+geomLabFrac)
	geomLabOff  = geomPadFrac + 0.26 // 坐标文字中心相对格线边缘的偏移（单位：格）
	geomAspect  = geomUnitW / geomUnitH
)

type boardGeom struct {
	cell   float64
	ox, oy float64
	// flip：屏幕 180° 翻转（人执黑时把黑方摆到下方）。
	//
	// 棋盘本身（格线/九宫/兵炮位标记）在 180° 旋转下完全对称，因此底板位图
	// 不用重画；需要翻转的只有「棋子落点、高亮、命中换算、四周坐标文字」。
	flip bool
}

func newBoardGeom(w, h float64) boardGeom {
	cell := math.Min(w/geomUnitW, h/geomUnitH)
	if cell < 1 {
		cell = 1
	}
	tw, th := geomUnitW*cell, geomUnitH*cell
	offX, offY := (w-tw)/2, (h-th)/2
	return boardGeom{
		cell: cell,
		ox:   offX + (geomPadFrac+geomLabFrac)*cell,
		oy:   offY + (geomPadFrac+geomLabFrac)*cell,
	}
}

// scaled 返回把每一格长度乘以 k 之后的几何。
//
// 【R1 修复的支点】棋盘绘制与命中测试必须走**同一套**坐标映射：
//
//	布局（逻辑单位）   —— newBoardGeom(逻辑宽, 逻辑高)
//	光栅化（物理像素） —— newBoardGeom(逻辑宽, 逻辑高).scaled(实际物理缩放)
//	命中测试（逻辑单位）—— newBoardGeom(逻辑宽, 逻辑高)
//
// 光栅化不再自己用物理尺寸重新算一遍 cell，而是从逻辑几何派生，因此
// 「棋盘的逻辑布局尺寸 / 物理光栅化尺寸 / 点击命中尺寸」永远严格一致：
// 即便 Fyne 交下来的位图尺寸不是恰好 逻辑×scale（取整、最大纹理尺寸裁剪等），
// 棋盘也不会画到与命中区域不同的位置。
func (g boardGeom) scaled(k float64) boardGeom {
	return boardGeom{cell: g.cell * k, ox: g.ox * k, oy: g.oy * k, flip: g.flip}
}

func (g boardGeom) x(file int) float64 { return g.ox + float64(file)*g.cell }
func (g boardGeom) y(rank int) float64 { return g.oy + float64(rules.ScreenRow(rank))*g.cell }

// withFlip 返回一份带翻转标记的几何副本。
func (g boardGeom) withFlip(f bool) boardGeom { g.flip = f; return g }

// fit 返回某个棋子在屏幕上的落点（自动处理翻转）。
// 所有「按棋子坐标画东西」的地方都必须走这里，不能直接调 x()/y()。
func (g boardGeom) fit(file, rank int) (float64, float64) {
	if g.flip {
		file = rules.Files - 1 - file
		rank = rules.Ranks - 1 - rank
	}
	return g.x(file), g.y(rank)
}

// hit 把像素位置换算成最近的交叉点。超出交叉点 0.48 格的点视为无效点击。
func (g boardGeom) hit(pos fyne.Position) (int, bool) {
	cf := (float64(pos.X) - g.ox) / g.cell
	cr := (float64(pos.Y) - g.oy) / g.cell
	file := int(math.Round(cf))
	row := int(math.Round(cr))
	if file < 0 || file > 8 || row < 0 || row > 9 {
		return 0, false
	}
	if math.Abs(cf-float64(file)) > 0.48 || math.Abs(cr-float64(row)) > 0.48 {
		return 0, false
	}
	if g.flip {
		// 翻转后：屏幕列 f 对应真实文件 8-f，屏幕行 s 对应真实行 s
		return rules.Index(rules.Files-1-file, row), true
	}
	return rules.Index(file, rules.ScreenRow(row)), true
}

// ============================================================================
// Board：Canvas 自绘棋盘控件
// ============================================================================

const (
	boardDotPool    = 24
	boardPiecePool  = 32
	boardCoordCount = 38 // 上/下各 9 个文件标注 + 左/右各 10 个行标注
)

// Board 是棋盘控件。它只持有一个局面指针，着法的应用由上层（App）负责，
// 因此棋盘组件可以被「桥接分析」与「引擎对战」两种模式复用。
type Board struct {
	widget.BaseWidget

	mu          sync.Mutex
	game        *rules.Game
	skin        BoardSkin
	skinVer     int
	interactive bool
	// blockedHint 是「当前不接受点击」时给用户看的一句话（见 SetInteractive 的调用方）
	blockedHint string
	editMode    bool

	selected int
	targets  []int
	lastFrom int
	lastTo   int
	checkSq  int
	checkOn  bool
	hoverSq  int

	onMove    func(m rules.Move)
	onIllegal func(msg string)
	onEdit    func(sq int)

	blinkMu   sync.Mutex
	blinkStop chan struct{}
	blinkRun  bool

	// layoutMu/layoutSz 保存「最近一次布局真正交给棋盘控件的逻辑尺寸」。
	//
	// 【R1】命中测试与光栅化都必须以这个尺寸为准，而不是 b.Size()：
	// 窗口缩放时 Fyne 会先 Resize 再重绘，若命中测试读到的是新尺寸而画面还是
	// 上一帧的，就会出现「点不中」。统一读同一个值可以彻底消除这种错位。
	layoutMu sync.Mutex
	layoutSz fyne.Size

	// flipped：屏幕 180° 翻转（人执黑时黑方在下方）
	flipMu  sync.Mutex
	flipped bool
	// showCoords：是否画四周坐标标注
	coordMu    sync.Mutex
	showCoords bool
	// showLines：是否画「行棋线路」——选中棋子时点亮它所在的横线 / 竖线
	// 【v1.6.9 / 头脑风暴第 18 条】与坐标开关分离（TCHESS 里这是两个独立开关）
	showLines bool

	// 最佳着法箭头（TCHESS 叫「棋步提示」）：在棋盘上画一条带箭头的线
	hintMu   sync.Mutex
	hintFrom int
	hintTo   int
	hintOn   bool
	// 拖动走子：抓住某枚棋子时，把它画在手指/鼠标位置
	dragMu   sync.Mutex
	dragSq   int
	dragPos  fyne.Position
	dragging bool
	// dragOver 是拖动过程中指针所在的格（-1 = 不在任何格上）。
	// 【v1.6.9 / 头脑风暴第 16 条】用来在落点画一枚半透明「预览棋子」。
	dragOver int

	// 走子动画状态：animT 是 0→1 的进度，animOn 为真时渲染器把
	// 「已经落在 animTo 上的那枚棋子」从 animFrom 的位置插值滑过来。
	animMu           sync.Mutex
	animFrom, animTo int
	animT            float64
	animOn           bool
	// animSeq：每次启动动画自增。动画协程只在自己仍是「最新一代」时才写状态，
	// 否则连走两步时旧协程会把新动画的 animOn 提前置 false（表现为动画一闪而过）。
	animSeq uint64

	rend *boardRenderer
}

var _ fyne.Tappable = (*Board)(nil)
var _ desktop.Hoverable = (*Board)(nil)

// NewBoard 创建棋盘控件。
func NewBoard() *Board {
	b := &Board{
		game:        rules.NewGame(),
		skin:        currentSkin(),
		selected:    -1,
		lastFrom:    -1,
		lastTo:      -1,
		checkSq:     -1,
		hoverSq:     -1,
		dragOver:    -1,
		interactive: true,
	}
	b.ExtendBaseWidget(b)
	return b
}

// SetBlockedHint 设置「棋盘当前不接受点击」时显示给用户的解释。
func (b *Board) SetBlockedHint(msg string) { b.mu.Lock(); b.blockedHint = msg; b.mu.Unlock() }

// SetOnMove 注册「用户走出一步合法着法」的回调。
func (b *Board) SetOnMove(f func(m rules.Move)) { b.mu.Lock(); b.onMove = f; b.mu.Unlock() }

// SetOnIllegal 注册「用户试图走非法着法」的回调（用于状态栏提示）。
func (b *Board) SetOnIllegal(f func(msg string)) { b.mu.Lock(); b.onIllegal = f; b.mu.Unlock() }

// SetOnEdit 注册「摆盘模式下点击交叉点」的回调。
func (b *Board) SetOnEdit(f func(sq int)) { b.mu.Lock(); b.onEdit = f; b.mu.Unlock() }

// SetSkin 切换棋盘皮肤（皮肤扩展接口的落点）。
func (b *Board) SetSkin(s BoardSkin) {
	b.mu.Lock()
	b.skin = s
	b.skinVer++
	b.mu.Unlock()
	if b.rend != nil {
		b.rend.bumpVersion()
	}
	b.Refresh()
}

// skinVersion 返回皮肤版本号，渲染器据此判断是否需要重新光栅化底板。
func (b *Board) skinVersion() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.skinVer
}

// SetInteractive 设置是否允许点击走子。
func (b *Board) SetInteractive(v bool) {
	b.mu.Lock()
	b.interactive = v
	if !v {
		b.selected, b.targets = -1, nil
	}
	b.mu.Unlock()
	b.Refresh()
}

// SetEditMode 设置是否处于手动摆盘模式。
func (b *Board) SetEditMode(v bool) {
	b.mu.Lock()
	b.editMode = v
	b.selected, b.targets = -1, nil
	b.mu.Unlock()
	b.Refresh()
}

// SetGame 更新棋盘显示的局面（会重新计算将军状态与闪烁）。
func (b *Board) SetGame(g *rules.Game) {
	b.mu.Lock()
	b.game = g
	b.selected, b.targets = -1, nil
	b.syncCheckLocked()
	b.mu.Unlock()
	b.Refresh()
}

// SetLastMove 设置「最近一步」的双色高亮（起点/终点）。传 -1,-1 清除。
func (b *Board) SetLastMove(from, to int) {
	b.mu.Lock()
	b.lastFrom, b.lastTo = from, to
	b.mu.Unlock()
	b.Refresh()
}

// ClearSelection 清除选中状态与合法落点标记。
func (b *Board) ClearSelection() {
	b.mu.Lock()
	b.selected, b.targets = -1, nil
	b.mu.Unlock()
	b.Refresh()
}

// Game 返回当前显示的局面。
func (b *Board) Game() *rules.Game {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.game
}

// syncCheckLocked 重新计算将军状态，必要时启动/停止闪烁（调用方需持有 b.mu）。
func (b *Board) syncCheckLocked() {
	sq := -1
	if b.game != nil && b.game.Board.InCheck(b.game.Board.Side) {
		sq = b.game.Board.FindKing(b.game.Board.Side)
	}
	// 【用户要求】将军不要红框闪烁：这里只记录状态，不再开启闪烁循环，
	// checkOn 永远为 false（渲染器因此不会画将军高亮）。
	// 将军的提示改为「专用音效 + 状态栏文字（被将军）」。
	b.checkSq = sq
	if b.checkOn {
		b.checkOn = false
	}
	b.stopBlink()
}

// blinkLoop 是「将军提示闪烁」的后台循环，每 420ms 翻转一次高亮。
func (b *Board) blinkLoop() {
	b.blinkMu.Lock()
	if b.blinkRun {
		b.blinkMu.Unlock()
		return
	}
	ch := make(chan struct{})
	b.blinkStop = ch
	b.blinkRun = true
	b.blinkMu.Unlock()

	tk := time.NewTicker(420 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-ch:
			return
		case <-tk.C:
			stop := false
			fyne.Do(func() {
				b.mu.Lock()
				if b.checkSq < 0 {
					stop = true
				} else {
					b.checkOn = !b.checkOn
				}
				b.mu.Unlock()
				b.Refresh()
			})
			if stop {
				b.blinkMu.Lock()
				if b.blinkStop == ch {
					b.blinkRun = false
					b.blinkStop = nil
				}
				b.blinkMu.Unlock()
				return
			}
		}
	}
}

func (b *Board) stopBlink() {
	b.blinkMu.Lock()
	if b.blinkRun && b.blinkStop != nil {
		close(b.blinkStop)
		b.blinkRun = false
		b.blinkStop = nil
	}
	b.blinkMu.Unlock()
}

// stopBlinkFor 是外部（窗口关闭时）调用的清理入口。
func (b *Board) stopBlinkFor() { b.stopBlink() }

// setLayoutSize 由渲染器在 Layout 时写入权威逻辑尺寸。
func (b *Board) setLayoutSize(s fyne.Size) {
	if s.Width <= 0 || s.Height <= 0 {
		return
	}
	b.layoutMu.Lock()
	b.layoutSz = s
	b.layoutMu.Unlock()
}

// layoutSize 返回权威逻辑尺寸；渲染器尚未布局时回退到控件当前尺寸。
func (b *Board) layoutSize() fyne.Size {
	b.layoutMu.Lock()
	s := b.layoutSz
	b.layoutMu.Unlock()
	if s.Width > 0 && s.Height > 0 {
		return s
	}
	return b.Size()
}

// isFlipped 是否处于 180° 翻转状态。
func (b *Board) isFlipped() bool {
	b.flipMu.Lock()
	defer b.flipMu.Unlock()
	return b.flipped
}

// SetFlipped 设置棋盘是否 180° 翻转（人执黑时用）。
//
// 底板位图在 180° 旋转下完全对称，所以这里只需重画棋子/高亮，
// 不必让底板失效；但为稳妥仍然刷新一次。
func (b *Board) SetFlipped(v bool) {
	b.flipMu.Lock()
	changed := b.flipped != v
	b.flipped = v
	b.flipMu.Unlock()
	if !changed {
		return
	}
	if b.rend != nil {
		b.rend.bumpVersion()
	}
	b.Refresh()
}

// showLinesNow / selectedNow 供渲染器读取（加锁，避免与主线程写冲突）。
func (b *Board) showLinesNow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.showLines
}

func (b *Board) selectedNow() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.selected
}

// SetShowLines 设置是否显示「行棋线路」（选中棋子所在的横线 / 竖线）。
func (b *Board) SetShowLines(v bool) {
	b.mu.Lock()
	changed := b.showLines != v
	b.showLines = v
	b.mu.Unlock()
	if changed {
		if r := b.rend; r != nil {
			r.bumpHint()
		}
		b.Refresh()
	}
}

// SetShowCoords 设置是否显示四周坐标标注。
func (b *Board) SetShowCoords(v bool) {
	b.coordMu.Lock()
	changed := b.showCoords != v
	b.showCoords = v
	b.coordMu.Unlock()
	if changed {
		b.Refresh()
	}
}

func (b *Board) coordsVisible() bool {
	b.coordMu.Lock()
	defer b.coordMu.Unlock()
	return b.showCoords
}

// geom 返回当前逻辑尺寸下的棋盘几何（光栅化与命中测试的共同来源）。
func (b *Board) geom() boardGeom {
	s := b.layoutSize()
	return newBoardGeom(float64(s.Width), float64(s.Height)).withFlip(b.isFlipped())
}

// Tapped 处理点击走子 / 摆盘。
func (b *Board) Tapped(ev *fyne.PointEvent) {
	size := b.layoutSize()
	geom := b.geom()
	sq, ok := geom.hit(ev.Position)
	if os.Getenv("XQ_HIT") != "" {
		fmt.Fprintf(os.Stderr, "[hit] ev.Position=(%.1f,%.1f) boardLogical=%.1f x %.1f cell=%.3f ox=%.2f oy=%.2f -> sq=%d ok=%v\n",
			ev.Position.X, ev.Position.Y, size.Width, size.Height, geom.cell, geom.ox, geom.oy, sq, ok)
	}
	if !ok {
		return
	}
	_ = b.geom() // 与光栅化同源：两者都由 layoutSize() 派生

	var moveCb func(rules.Move)
	var editCb func(int)
	var illegalCb func(string)
	var illegalMsg string
	var move rules.Move

	b.mu.Lock()
	switch {
	case b.editMode:
		editCb = b.onEdit
	case !b.interactive:
		// 点不动的时候必须说明**为什么** —— 否则用户看到的就是「棋盘坏了 / 动不了」。
		// 具体原因由 App 通过 SetBlockedHint 给出（对战模式只展示、正在看历史局面…）。
		illegalMsg = b.blockedHint
		if illegalMsg == "" {
			illegalMsg = "现在不能直接走子"
		}
		illegalCb = b.onIllegal
	default:
		p := b.game.Board.Sq[sq]
		targeted := b.selected >= 0 && containsInt(b.targets, sq)
		switch {
		case targeted:
			move = rules.NewMove(b.selected, sq)
			moveCb = b.onMove
			b.selected, b.targets = -1, nil
		case b.selected == sq:
			b.selected, b.targets = -1, nil
		case !p.IsEmpty() && p.Side() == b.game.Board.Side:
			b.selected = sq
			b.targets = b.game.LegalTargets(sq)
		case !p.IsEmpty():
			illegalMsg = fmt.Sprintf("轮到%s方走棋，不能移动%s方的「%s」",
				rules.SideName(b.game.Board.Side), rules.SideName(p.Side()), p.Name())
			illegalCb = b.onIllegal
			b.selected, b.targets = -1, nil
		default:
			b.selected, b.targets = -1, nil
		}
	}
	b.mu.Unlock()

	b.Refresh()
	if editCb != nil {
		editCb(sq)
	}
	if moveCb != nil {
		moveCb(move)
	}
	if illegalCb != nil {
		illegalCb(illegalMsg)
	}
}

// MouseIn / MouseMoved / MouseOut 实现悬停高亮。
func (b *Board) MouseIn(ev *desktop.MouseEvent)    { b.hover(ev.Position) }
func (b *Board) MouseMoved(ev *desktop.MouseEvent) { b.hover(ev.Position) }
func (b *Board) MouseOut() {
	b.mu.Lock()
	changed := b.hoverSq != -1
	b.hoverSq = -1
	b.mu.Unlock()
	if changed {
		b.Refresh()
	}
}

func (b *Board) hover(pos fyne.Position) {
	geom := b.geom()
	sq, ok := geom.hit(pos)
	if !ok {
		sq = -1
	}
	b.mu.Lock()
	changed := b.hoverSq != sq
	b.hoverSq = sq
	b.mu.Unlock()
	if changed {
		b.Refresh()
	}
}

// AnimateMove 让刚走完的那一步「滑」过去（90ms，smoothstep 缓动）。
//
// 实现方式：只在这里记录起点/终点与进度，真正的坐标插值在渲染器 Layout 里做，
// 每 ~16ms 触发一次 Refresh。这样棋子的落点始终由 Layout 统一决定，
// 动画结束后不会残留任何偏移。
func (b *Board) AnimateMove(from, to int) {
	if from < 0 || to < 0 || from == to {
		return
	}
	b.animMu.Lock()
	b.animFrom, b.animTo, b.animT, b.animOn = from, to, 0, true
	b.animSeq++
	mySeq := b.animSeq
	b.animMu.Unlock()
	go func() {
		const dur = 55 * time.Millisecond
		start := time.Now()
		for {
			t := float64(time.Since(start)) / float64(dur)
			done := t >= 1
			if done {
				t = 1
			}
			fyne.Do(func() {
				b.animMu.Lock()
				if b.animSeq != mySeq { // 已经有更新的一次走子动画了，这一代直接退出
					b.animMu.Unlock()
					return
				}
				b.animT = t
				b.animOn = !done
				b.animMu.Unlock()
				// 只挪动「正在滑的那一枚棋子」，不重排整盘 32 枚棋子：
				// 重排一帧要测量并移动 100+ 个画布对象，正是动画发顿的原因。
				if r := b.rend; r != nil {
					r.stepAnim()
				} else {
					b.Refresh()
				}
			})
			if done {
				return
			}
			time.Sleep(8 * time.Millisecond)
		}
	}()
}

// stepAnim 只把「正在滑动的那一枚棋子」挪到插值位置。
//
// 为什么不能整盘重排：一帧 Layout 要重新测量并移动 32 枚棋子 × 3 个画布对象
// （还有河界字、坐标），100+ 次 Move/Resize，界面线程根本跟不上 60fps，
// 表现出来就是「动画一顿一顿、感觉很慢」。这里只动 3 个对象，代价可以忽略。
func (r *boardRenderer) stepAnim() {
	from, to, t, on := r.b.animState()
	g := r.lastGeom
	if !on || r.animIdx < 0 || r.animIdx >= len(r.pieces) || to < 0 || g.cell <= 0 {
		r.b.Refresh()
		return
	}
	e := t * t * (3 - 2*t)
	fx, fy := g.fit(rules.FileOf(from), rules.RankOf(from))
	tx, ty := g.fit(rules.FileOf(to), rules.RankOf(to))
	cx := fx + (tx-fx)*e
	cy := fy + (ty-fy)*e

	rad := g.cell * 0.435
	off := math.Max(1, g.cell*0.045)
	v := r.pieces[r.animIdx]
	v.shadow.Move(fyne.NewPos(float32(cx-rad+off), float32(cy-rad+off)))
	v.body.Move(fyne.NewPos(float32(cx-rad), float32(cy-rad)))
	ts := v.text.MinSize()
	v.text.Move(fyne.NewPos(
		float32(cx-float64(ts.Width)/2),
		float32(cy-float64(ts.Height)/2+g.cell*0.02)))
	canvas.Refresh(v.shadow)
	canvas.Refresh(v.body)
	canvas.Refresh(v.text)
}

// animState 供渲染器读取动画进度。
func (b *Board) animState() (from, to int, t float64, on bool) {
	b.animMu.Lock()
	defer b.animMu.Unlock()
	return b.animFrom, b.animTo, b.animT, b.animOn
}

// SetHint 设置「最佳着法箭头」。传 (-1,-1) 清除。
//
// 参照 TCHESS 的「棋步提示」与 swiftxiangqi 的 best-move arrows：
// 分析结果里光有中文记谱不够直观，棋盘上直接画一条箭头最快看懂。
func (b *Board) SetHint(from, to int) {
	b.hintMu.Lock()
	changed := b.hintFrom != from || b.hintTo != to || !b.hintOn
	b.hintFrom, b.hintTo = from, to
	b.hintOn = from >= 0 && to >= 0 && from != to
	b.hintMu.Unlock()
	if changed && b.rend != nil {
		b.rend.bumpHint()
	}
	if changed {
		b.Refresh()
	}
}

// hintState 供渲染器读取箭头。
func (b *Board) hintState() (int, int, bool) {
	b.hintMu.Lock()
	defer b.hintMu.Unlock()
	return b.hintFrom, b.hintTo, b.hintOn
}

// ---- fyne.Draggable：拖动走子（同类软件都支持拖或点两种方式）----

// Dragged 处理拖动过程：抓子并跟随指针。
func (b *Board) Dragged(e *fyne.DragEvent) {
	b.dragMu.Lock()
	if b.dragSq < 0 {
		// 首次拖动：看按下的位置有没有「该走一方」的棋子
		b.dragMu.Unlock()
		geom := b.geom()
		sq, ok := geom.hit(e.Position.Subtract(e.Dragged))
		if !ok {
			return
		}
		b.mu.Lock()
		interactive := b.interactive && !b.editMode && b.game != nil
		var p rules.Piece
		if interactive {
			p = b.game.Board.Sq[sq]
		}
		b.mu.Unlock()
		if !interactive || p.IsEmpty() || p.Side() != b.game.Board.Side {
			return
		}
		b.mu.Lock()
		b.selected = sq
		b.targets = b.game.LegalTargets(sq)
		b.mu.Unlock()
		b.dragMu.Lock()
		b.dragSq = sq
		b.dragging = true
	} else {
		b.dragMu.Unlock()
		b.dragMu.Lock()
	}
	b.dragPos = e.Position
	// 落点预览：把指针位置换算成格子，供渲染器画半透明棋子
	if sq, ok := b.geom().hit(e.Position); ok {
		b.dragOver = sq
	} else {
		b.dragOver = -1
	}
	b.dragMu.Unlock()
	b.Refresh()
}

// DragEnd 结束拖动：落在合法落点上就走子，否则归位。
func (b *Board) DragEnd() {
	b.dragMu.Lock()
	sq := b.dragSq
	pos := b.dragPos
	b.dragSq = -1
	b.dragging = false
	b.dragOver = -1
	b.dragMu.Unlock()
	if sq < 0 {
		return
	}

	geom := b.geom()
	dst, ok := geom.hit(pos)
	var moveCb func(rules.Move)
	var move rules.Move
	if ok && dst != sq {
		b.mu.Lock()
		if containsInt(b.targets, dst) {
			move = rules.NewMove(sq, dst)
			moveCb = b.onMove
			b.selected, b.targets = -1, nil
		} else {
			b.selected, b.targets = -1, nil
		}
		b.mu.Unlock()
	} else {
		// 原地放下 = 当成普通点击（保留选中态，方便「点一下选中、再点落点」）
		b.mu.Lock()
		b.selected = sq
		b.targets = b.game.LegalTargets(sq)
		b.mu.Unlock()
	}
	b.Refresh()
	if moveCb != nil {
		moveCb(move)
	}
}

// dragState 供渲染器读取拖动位置。
func (b *Board) dragState() (int, fyne.Position, bool, int) {
	b.dragMu.Lock()
	defer b.dragMu.Unlock()
	return b.dragSq, b.dragPos, b.dragging, b.dragOver
}

// MinSize 给出棋盘的最小可操作尺寸。
//
// 【R2-1】返回的宽高严格按棋盘自身长宽比（geomUnitW : geomUnitH）给出，
// 与光栅化时实际使用的比例完全一致——所以「MinSize 描述的形状」和
// 「真实画出来的形状」永远相同，布局不会因为比例不同而多留/少留空间。
func (b *Board) MinSize() fyne.Size {
	const minH = 420
	return fyne.NewSize(float32(minH*geomAspect), minH)
}

// CreateRenderer 构建棋盘渲染器。
func (b *Board) CreateRenderer() fyne.WidgetRenderer {
	r := newBoardRenderer(b)
	b.rend = r
	return r
}

// measureCache 缓存「文本 + 字号 → 尺寸」的测量结果。
//
// 为什么需要：棋盘每次重绘都要为 32 枚棋子 + 38 个坐标 + 2 个河界字测量文本尺寸，
// 单次 fyne.MeasureText 涉及字体解析与字形整形，72 次累计可达数十毫秒。
// 棋盘尺寸不变时这些测量结果完全不变，因此做一层进程内缓存。
var (
	measureMu    sync.Mutex
	measureCache = map[string]fyne.Size{}
)

// measureTextCached 是带缓存的 fyne.MeasureText。
func measureTextCached(text string, size float32, style fyne.TextStyle) fyne.Size {
	key := text + "\x00" + string(rune(int(size*4))) + "\x00" + boolKey(style.Bold) + boolKey(style.Italic) + boolKey(style.Monospace)
	measureMu.Lock()
	if v, ok := measureCache[key]; ok {
		measureMu.Unlock()
		return v
	}
	measureMu.Unlock()
	v := fyne.MeasureText(text, size, style)
	measureMu.Lock()
	if len(measureCache) > 4096 {
		measureCache = map[string]fyne.Size{}
	}
	measureCache[key] = v
	measureMu.Unlock()
	return v
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// containsInt 判断切片是否包含某个值。
func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ============================================================================
// 渲染器
// ============================================================================

type pieceView struct {
	shadow *canvas.Circle
	body   *canvas.Circle
	gloss  *canvas.Circle // 高光（皮肤未提供时为 nil）
	text   *canvas.Text
}

// ghostView 是拖动时显示在落点的半透明「预览棋子」（v1.6.9 / 头脑风暴第 16 条）。
type ghostView struct {
	body *canvas.Circle
	text *canvas.Text
}

type boardRenderer struct {
	b *Board

	raster *canvas.Raster
	ghost  *ghostView // 拖动落点的半透明预览棋子
	// annot 是「标注层」：最佳着法箭头画在这里，压在底板之上、棋子之下。
	// 单独一层是为了不把它烤进底板位图（底板按尺寸+皮肤缓存，箭头每步都变）。
	annot    *canvas.Raster
	annotVer int

	plateMu   sync.Mutex
	plate     *image.RGBA
	plateW    int
	plateH    int
	plateVer  int
	plateGeom boardGeom // 生成 plate 时所用的**逻辑**几何（用于与命中测试对齐校验）
	lastGeom  boardGeom // 最近一次布局的几何（动画单步复用它，避免每帧重算）
	animIdx   int       // 正在滑动的棋子在 pieces 里的下标（-1 = 无）
	ver       int
	skinSeen  int
	skinInit  bool

	selRect *canvas.Rectangle
	lfRect  *canvas.Rectangle
	ltRect  *canvas.Rectangle
	hovRect *canvas.Rectangle
	chkRect *canvas.Rectangle

	dots   []*canvas.Circle
	pieces []*pieceView

	river  [2]*canvas.Text
	coords []*canvas.Text

	objects []fyne.CanvasObject
}

func newBoardRenderer(b *Board) *boardRenderer {
	sk := currentSkin()
	r := &boardRenderer{b: b}
	r.raster = canvas.NewRaster(r.generatePlate)
	r.annot = canvas.NewRaster(r.generateAnnot)

	mkRect := func() *canvas.Rectangle {
		c := canvas.NewRectangle(sk.SelectFill)
		c.CornerRadius = 0
		return c
	}
	r.lfRect = mkRect()
	r.ltRect = mkRect()
	r.hovRect = mkRect()
	r.selRect = mkRect()
	r.chkRect = mkRect()
	r.lfRect.FillColor = sk.LastFrom
	r.ltRect.FillColor = sk.LastTo
	r.selRect.FillColor = sk.SelectFill
	r.selRect.StrokeColor = sk.SelectEdge
	r.selRect.StrokeWidth = 2
	r.chkRect.FillColor = sk.CheckGlow
	r.hovRect.FillColor = sk.LastFrom

	r.river[0] = canvas.NewText("楚 河", sk.RiverText)
	r.river[1] = canvas.NewText("汉 界", sk.RiverText)
	for _, t := range r.river {
		t.TextSize = textSize(textTitle)
		t.TextStyle = fyne.TextStyle{Bold: true}
	}

	for i := 0; i < boardCoordCount; i++ {
		t := canvas.NewText("", sk.CoordText)
		t.TextSize = textSize(textSmall)
		r.coords = append(r.coords, t)
	}

	for i := 0; i < boardDotPool; i++ {
		c := canvas.NewCircle(sk.TargetDot)
		r.dots = append(r.dots, c)
	}
	for i := 0; i < boardPiecePool; i++ {
		v := &pieceView{
			shadow: canvas.NewCircle(sk.PieceShadow),
			body:   canvas.NewCircle(sk.RedFill),
			text:   canvas.NewText("", sk.RedText),
		}
		v.body.StrokeWidth = 2
		v.text.TextStyle = fyne.TextStyle{Bold: true}
		// 高光层只在皮肤提供颜色时才建（浅色皮肤下连对象都不生成，零开销）
		if toNRGBA(sk.PieceGloss).A > 0 {
			v.gloss = canvas.NewCircle(sk.PieceGloss)
		}
		r.pieces = append(r.pieces, v)
	}

	// 预览棋子：半透明，画在棋子层的**下面**（这样真棋子压在上面时看着像「放下去」）
	r.ghost = &ghostView{
		body: canvas.NewCircle(colPrimary),
		text: canvas.NewText("", colPrimaryFg),
	}
	r.ghost.body.StrokeWidth = 2
	r.ghost.text.TextStyle = fyne.TextStyle{Bold: true}
	r.ghost.body.Hide()
	r.ghost.text.Hide()

	// 绘制顺序（由下到上）：底板 → 悬停 → 上一步 → 选中 → 将军 → 落点 → 预览棋子 → 河界字 → 坐标 → 棋子
	r.objects = append(r.objects, r.raster, r.annot, r.hovRect, r.lfRect, r.ltRect, r.selRect, r.chkRect)
	r.objects = append(r.objects, r.ghost.body, r.ghost.text)
	for _, d := range r.dots {
		r.objects = append(r.objects, d)
	}
	for _, t := range r.river {
		r.objects = append(r.objects, t)
	}
	for _, t := range r.coords {
		r.objects = append(r.objects, t)
	}
	for _, p := range r.pieces {
		r.objects = append(r.objects, p.shadow, p.body)
		if p.gloss != nil {
			r.objects = append(r.objects, p.gloss)
		}
		r.objects = append(r.objects, p.text)
	}
	return r
}

// ghostTextColor 预览棋子的文字颜色（按棋子阵营取 skin 色）。
func ghostTextColor(p rules.Piece) color.Color {
	sk := currentSkin()
	if p.Side() == rules.Red {
		return sk.RedText
	}
	return sk.BlackText
}

// bumpHint 让箭头层失效（分析结果变了才会调）。
func (r *boardRenderer) bumpHint() {
	r.plateMu.Lock()
	r.annotVer++
	r.plateMu.Unlock()
	if r.annot != nil {
		r.annot.Refresh()
	}
}

// generateAnnot 光栅化标注层：目前只有「最佳着法箭头」。
func (r *boardRenderer) generateAnnot(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return img
	}
	from, to, on := r.b.hintState()
	logical := r.b.layoutSize()
	if logical.Width <= 0 || logical.Height <= 0 {
		return img
	}
	sx := float64(w) / float64(logical.Width)
	sy := float64(h) / float64(logical.Height)
	g := newBoardGeom(float64(logical.Width), float64(logical.Height)).withFlip(r.b.isFlipped())
	scale := (sx + sy) / 2

	// 【v1.6.9 / 头脑风暴第 18 条】「显示线路」：选中棋子时把它所在的横线、竖线点亮，
	// 看棋的人一眼能看出这枚子能沿哪条线走（车/炮的"路"）。
	// 与坐标开关完全独立——TCHESS 里这也是两个分开的设置项。
	if r.b.showLinesNow() {
		if sq := r.b.selectedNow(); sq >= 0 {
			col := colPrimary
			col.A = 0x66
			cx, cy := g.fit(rules.FileOf(sq), rules.RankOf(sq))
			// 整条横线 + 整条竖线（画在棋盘格线之上，半透明）
			drawLine(img, 0, cy*scale, float64(w), cy*scale, math.Max(1, 1.6*scale), toNRGBA(col))
			drawLine(img, cx*scale, 0, cx*scale, float64(h), math.Max(1, 1.6*scale), toNRGBA(col))
		}
	}
	if !on {
		return img
	}
	x0, y0 := g.fit(rules.FileOf(from), rules.RankOf(from))
	x1, y1 := g.fit(rules.FileOf(to), rules.RankOf(to))
	drawHintArrow(img, x0*scale, y0*scale, x1*scale, y1*scale, g.cell*scale, toNRGBA(colHintArrow))
	return img
}

// drawHintArrow 画一条从 (x0,y0) 到 (x1,y1) 的箭头：主干 + 三角箭头。
func drawHintArrow(img *image.RGBA, x0, y0, x1, y1, cell float64, col color.NRGBA) {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	if l < 1 {
		return
	}
	ux, uy := dx/l, dy/l
	head := math.Max(10, cell*0.34) // 箭头长度
	shaft := math.Max(3, cell*0.13) // 主干宽度
	// 主干缩到箭头根部，避免箭头处出现尖角毛刺
	ex, ey := x1-ux*head*0.85, y1-uy*head*0.85
	drawLine(img, x0+ux*head*0.4, y0+uy*head*0.4, ex, ey, shaft, col)
	// 箭头三角
	px, py := -uy, ux
	tipX, tipY := x1, y1
	baseX, baseY := x1-ux*head, y1-uy*head
	half := head * 0.52
	aX, aY := baseX+px*half, baseY+py*half
	bX, bY := baseX-px*half, baseY-py*half
	fillTriangle(img, tipX, tipY, aX, aY, bX, bY, col)
}

// fillTriangle 用重心坐标做抗锯齿填充。
func fillTriangle(img *image.RGBA, x0, y0, x1, y1, x2, y2 float64, col color.NRGBA) {
	minX := int(math.Floor(math.Min(x0, math.Min(x1, x2))) - 1)
	maxX := int(math.Ceil(math.Max(x0, math.Max(x1, x2))) + 1)
	minY := int(math.Floor(math.Min(y0, math.Min(y1, y2))) - 1)
	maxY := int(math.Ceil(math.Max(y0, math.Max(y1, y2))) + 1)
	area := (x1-x0)*(y2-y0) - (x2-x0)*(y1-y0)
	if math.Abs(area) < 1e-9 {
		return
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			w0 := ((x1-px)*(y2-py) - (x2-px)*(y1-py)) / area
			w1 := ((x2-px)*(y0-py) - (x0-px)*(y2-py)) / area
			w2 := 1 - w0 - w1
			cov := math.Min(w0, math.Min(w1, w2)) + 0.5
			if cov > 0 {
				blend(img, x, y, col, cov)
			}
		}
	}
}

func (r *boardRenderer) bumpVersion() {
	r.plateMu.Lock()
	r.ver++
	r.plateVer = -1
	r.plateMu.Unlock()
}

// generatePlate 光栅化棋盘底板。
//
// Fyne 传入的 w、h 是**物理像素**（内部已经乘过 DPI 缩放），因此在 125%/150%/200%
// 缩放下棋盘都是按物理像素绘制，不会模糊。
//
// 【R1 修复】这里不再用物理尺寸重新推导一套几何，而是从**逻辑布局几何**派生：
//
//	k = 实际物理尺寸 / 逻辑布局尺寸
//	物理几何 = 逻辑几何.scaled(k)
//
// 这样即使 w、h 因为取整或纹理尺寸上限而与「逻辑×scale」略有出入，棋盘底板
// 依然与棋子、命中区域完全同源，绝不会出现"画得比分配空间大"或"画在别处"。
func (r *boardRenderer) generatePlate(w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	logical := r.b.layoutSize()
	if logical.Width <= 0 || logical.Height <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	// 分别取 x/y 方向的实际缩放。Fyne 的 w、h 是各自取整后的整数，两者只会差
	// 不到 0.1%，而真实的画布缩放是统一的，因此取平均作为光栅缩放系数。
	sx := float64(w) / float64(logical.Width)
	sy := float64(h) / float64(logical.Height)
	k := (sx + sy) / 2
	base := newBoardGeom(float64(logical.Width), float64(logical.Height)).withFlip(r.b.isFlipped())

	// ★ 必须用 scaled()：cell、ox、oy **三者一起**放大。
	// v1.3.1 第一版这里手写成 {cell: base.cell, ox: base.ox*sx, oy: base.oy*sy}，
	// 漏乘了 cell —— 底板格距仍是逻辑值、而棋子按物理值摆放，两者差 1.3 倍，
	// 于是棋子全部落在格线之间、外侧棋子跑出底板（已修，并加了回归测试
	// ui/board_geom_test.go 守住这一点）。
	physGeom := base.scaled(k)

	r.plateMu.Lock()
	defer r.plateMu.Unlock()
	if r.plate != nil && r.plateW == w && r.plateH == h && r.plateVer == r.ver {
		return r.plate
	}
	if os.Getenv("XQ_DEBUG") != "" {
		fmt.Fprintf(os.Stderr,
			"[raster] logical=%.2fx%.2f physical=%dx%d scale=%.4f/%.4f cellLogical=%.3f cellPhysical=%.3f\n",
			logical.Width, logical.Height, w, h, sx, sy, base.cell, physGeom.cell)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	drawBoardPlate(img, physGeom, r.b.currentSkin())
	r.plate, r.plateW, r.plateH, r.plateVer = img, w, h, r.ver
	r.plateGeom = physGeom
	return img
}

func (b *Board) currentSkin() BoardSkin {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.skin
}

// drawBoardPlate 绘制棋盘底板：圆角底板、双线外框、四角装饰、格线、九宫斜线、兵炮位标记。
//
// g 由调用方给出（物理像素几何，从逻辑几何派生），本函数不再自行推导坐标。
func drawBoardPlate(img *image.RGBA, g boardGeom, sk BoardSkin) {
	cell := g.cell

	plate := toNRGBA(sk.Plate)
	plateEdge := toNRGBA(sk.PlateEdge)
	outer := toNRGBA(sk.BorderOuter)
	inner := toNRGBA(sk.BorderInner)
	grid := toNRGBA(sk.GridLine)
	mark := toNRGBA(sk.MarkLine)

	// 底板范围（格线区域外扩 geomPadFrac 格）
	x0 := g.x(0) - geomPadFrac*cell
	x1 := g.x(8) + geomPadFrac*cell
	y0 := g.y(9) - geomPadFrac*cell
	y1 := g.y(0) + geomPadFrac*cell
	radius := cell * 0.20

	fillRoundRect(img, x0, y0, x1, y1, radius, plate)
	strokeRoundRect(img, x0+0.5, y0+0.5, x1-0.5, y1-0.5, radius, 1, plateEdge)

	// 双线外框：粗线 + 内侧细线
	lwOuter := math.Max(1.8, cell*0.055)
	lwInner := math.Max(1.0, cell*0.020)
	strokeRoundRect(img, x0, y0, x1, y1, radius, lwOuter, outer)
	gap := math.Max(2.5, cell*0.13)
	strokeRoundRect(img, x0+gap, y0+gap, x1-gap, y1-gap, math.Max(1, radius-gap*0.5), lwInner, inner)

	// 四角装饰：内框四个角上的小方块
	ds := math.Max(1.5, cell*0.055)
	for _, c := range [][2]float64{
		{x0 + gap, y0 + gap}, {x1 - gap, y0 + gap},
		{x0 + gap, y1 - gap}, {x1 - gap, y1 - gap},
	} {
		fillRect(img, c[0]-ds, c[1]-ds, c[0]+ds, c[1]+ds, outer)
	}

	// 格线
	lw := math.Max(1.0, cell*0.024)
	gx0, gx1 := g.x(0), g.x(8)
	gyTop, gyBottom := g.y(9), g.y(0)
	for rank := 0; rank < rules.Ranks; rank++ {
		yy := g.y(rank)
		drawLine(img, gx0, yy, gx1, yy, lw, grid)
	}
	for file := 0; file < rules.Files; file++ {
		xx := g.x(file)
		if file == 0 || file == rules.Files-1 {
			// 两侧边线贯通（传统棋盘边框）
			drawLine(img, xx, gyTop, xx, gyBottom, lw, grid)
			continue
		}
		// 中间竖线在河界处断开
		drawLine(img, xx, gyTop, xx, g.y(5), lw, grid)
		drawLine(img, xx, g.y(4), xx, gyBottom, lw, grid)
	}

	// 九宫斜线
	drawLine(img, g.x(3), g.y(0), g.x(5), g.y(2), lw, grid)
	drawLine(img, g.x(5), g.y(0), g.x(3), g.y(2), lw, grid)
	drawLine(img, g.x(3), g.y(9), g.x(5), g.y(7), lw, grid)
	drawLine(img, g.x(5), g.y(9), g.x(3), g.y(7), lw, grid)

	// 兵位与炮位的小十字标记
	marked := [][2]int{
		{0, 3}, {2, 3}, {4, 3}, {6, 3}, {8, 3},
		{0, 6}, {2, 6}, {4, 6}, {6, 6}, {8, 6},
		{1, 2}, {7, 2}, {1, 7}, {7, 7},
	}
	for _, m := range marked {
		drawPositionMark(img, g, m[0], m[1], lw*0.8, mark)
	}

	// 木纹（皮肤给了强度才画；浅色皮肤 PlateGrain=0，底板与从前一模一样）
	if sk.PlateGrain > 0 {
		drawWoodGrain(img, x0, y0, x1, y1, radius, cell, sk)
	}
}

// drawWoodGrain 在底板上叠一层低对比度木纹。
//
// 为什么用程序生成而不是贴图：
//   - 任意 DPI 下都清晰（贴图缩放必然发虚）；
//   - 强度由皮肤的一个数字控制，浅色皮肤设 0 就完全不变，不会污染已有观感；
//   - 只画"疏密不均的竖纹 + 偶发节疤"两件事 —— 木纹的像不像全在这两点上，
//     画成等距条纹反而会变成"条纹布"。
func drawWoodGrain(img *image.RGBA, x0, y0, x1, y1, radius, cell float64, sk BoardSkin) {
	base := toNRGBA(sk.Plate)
	amp := int(sk.PlateGrain)
	dark := shade(base, -amp*3)
	light := shade(base, amp*3)
	dark.A = uint8(math.Min(255, float64(amp)*4))
	light.A = uint8(math.Min(255, float64(amp)*3))

	step := math.Max(2, cell*0.045)
	for x := x0 + radius; x < x1-radius; x += step {
		h := hash2(int(x), 977)
		// 每 5 条里画 1 条：既看不出等距，又足以形成走向
		if h%5 != 0 {
			continue
		}
		c := dark
		if h%10 == 0 {
			c = light
		}
		w := math.Max(1, cell*0.028)
		// 轻微弯曲：一条竖线拆成两段错位的斜线，避免"直尺画出来"的机械感
		midY := (y0 + y1) / 2
		shift := (float64(h%7) - 3) * cell * 0.01
		drawLineClippedRR(img, x, y0+radius, x+shift, midY, w, c, x0, y0, x1, y1, radius)
		drawLineClippedRR(img, x+shift, midY, x, y1-radius, w, c, x0, y0, x1, y1, radius)
	}
	// 节疤：稀疏的小圆，纹理因此不会像条纹
	for i := 0; i < 6; i++ {
		hx := hash2(i*37+11, 5)
		hy := hash2(i*91+7, 3)
		cx := x0 + radius + float64(hx)/255*(x1-x0-2*radius)
		cy := y0 + radius + float64(hy)/255*(y1-y0-2*radius)
		c := dark
		c.A = uint8(math.Min(255, float64(amp)*3))
		fillCircleClippedRR(img, cx, cy, cell*0.10, c, x0, y0, x1, y1, radius)
	}
}

// insideRoundRect 判断像素是否落在圆角矩形内（用于把纹理裁在底板上）。
func insideRoundRect(px, py, x0, y0, x1, y1, r float64) bool {
	if px < x0 || px > x1 || py < y0 || py > y1 {
		return false
	}
	if r <= 0 {
		return true
	}
	cx := math.Min(math.Max(px, x0+r), x1-r)
	cy := math.Min(math.Max(py, y0+r), y1-r)
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

// drawLineClippedRR 画一条被圆角矩形裁剪的线段（木纹不能画到圆角外面去）。
func drawLineClippedRR(img *image.RGBA, x0, y0, x1, y1, w float64, c color.NRGBA,
	cx0, cy0, cx1, cy1, r float64) {
	tmp := image.NewRGBA(img.Bounds())
	drawLine(tmp, x0+float64(img.Bounds().Min.X), y0, x1, y1, w, c)
	blitClippedRR(img, tmp, cx0, cy0, cx1, cy1, r)
}

// fillCircleClippedRR 画一个被圆角矩形裁剪的实心圆。
func fillCircleClippedRR(img *image.RGBA, cx, cy, rad float64, c color.NRGBA,
	cx0, cy0, cx1, cy1, r float64) {
	tmp := image.NewRGBA(img.Bounds())
	fillCircle(tmp, cx, cy, rad, c)
	blitClippedRR(img, tmp, cx0, cy0, cx1, cy1, r)
}

// blitClippedRR 把 src 中落在圆角矩形内的像素合成到 dst 上。
func blitClippedRR(dst, src *image.RGBA, x0, y0, x1, y1, r float64) {
	b := src.Bounds()
	ix0 := int(math.Max(float64(b.Min.X), math.Floor(x0-1)))
	ix1 := int(math.Min(float64(b.Max.X-1), math.Ceil(x1+1)))
	iy0 := int(math.Max(float64(b.Min.Y), math.Floor(y0-1)))
	iy1 := int(math.Min(float64(b.Max.Y-1), math.Ceil(y1+1)))
	for y := iy0; y <= iy1; y++ {
		for x := ix0; x <= ix1; x++ {
			if !insideRoundRect(float64(x)+0.5, float64(y)+0.5, x0, y0, x1, y1, r) {
				continue
			}
			i := src.PixOffset(x, y)
			if src.Pix[i+3] == 0 {
				continue
			}
			sr, sg, sb, sa := src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3]
			blend(dst, x, y, color.NRGBA{R: sr, G: sg, B: sb, A: sa}, 1)
		}
	}
}

// drawPositionMark 在交叉点四周画四个小 L 形标记（棋盘边缘只画内侧的一半）。
func drawPositionMark(img *image.RGBA, g boardGeom, file, rank int, lw float64, c color.NRGBA) {
	cell := g.cell
	gap := cell * 0.09
	size := cell * 0.17
	cx, cy := g.x(file), g.y(rank)
	for _, sx := range []float64{-1, 1} {
		for _, sy := range []float64{-1, 1} {
			if file == 0 && sx < 0 {
				continue
			}
			if file == rules.Files-1 && sx > 0 {
				continue
			}
			px := cx + sx*gap
			py := cy + sy*gap
			drawLine(img, px, py, px+sx*size, py, lw, c)
			drawLine(img, px, py, px, py+sy*size, lw, c)
		}
	}
}

// ---------------------------------------------------------------------------
// WidgetRenderer 接口
// ---------------------------------------------------------------------------

func (r *boardRenderer) Layout(size fyne.Size) {
	// 【R1/R2】先登记权威逻辑尺寸：命中测试与光栅化都读它，三者同源。
	r.b.setLayoutSize(size)
	geom := newBoardGeom(float64(size.Width), float64(size.Height)).withFlip(r.b.isFlipped())
	cell := geom.cell
	b := r.b

	r.raster.Move(fyne.NewPos(0, 0))
	r.raster.Resize(size)
	r.annot.Move(fyne.NewPos(0, 0))
	r.annot.Resize(size)

	// --- 高亮方块 ---
	setSquare := func(o *canvas.Rectangle, sq int, pad float64, visible bool) {
		if sq < 0 || !visible {
			o.Hide()
			return
		}
		file, rank := rules.FileOf(sq), rules.RankOf(sq)
		side := cell - 2*pad
		fx, fy := geom.fit(file, rank)
		o.Move(fyne.NewPos(float32(fx-side/2), float32(fy-side/2)))
		o.Resize(fyne.NewSize(float32(side), float32(side)))
		o.Show()
	}

	b.mu.Lock()
	sel, lf, lt, hov, chk := b.selected, b.lastFrom, b.lastTo, b.hoverSq, b.checkSq
	chkOn := b.checkOn
	targets := append([]int(nil), b.targets...)
	b.mu.Unlock()

	setSquare(r.hovRect, hov, cell*0.44, true)
	setSquare(r.lfRect, lf, cell*0.40, true)
	setSquare(r.ltRect, lt, cell*0.40, true)
	setSquare(r.chkRect, chk, cell*0.46, chkOn)
	if sel >= 0 {
		r.selRect.StrokeWidth = float32(math.Max(1.5, cell*0.035))
		setSquare(r.selRect, sel, cell*0.36, true)
	} else {
		r.selRect.Hide()
	}

	// --- 合法落点圆点 ---
	dotR := cell * 0.115
	for i, d := range r.dots {
		if i >= len(targets) {
			d.Hide()
			continue
		}
		file, rank := rules.FileOf(targets[i]), rules.RankOf(targets[i])
		dfx, dfy := geom.fit(file, rank)
		d.Move(fyne.NewPos(float32(dfx-dotR), float32(dfy-dotR)))
		d.Resize(fyne.NewSize(float32(2*dotR), float32(2*dotR)))
		d.Show()
	}

	// --- 楚河汉界 ---
	riverY := (geom.y(5) + geom.y(4)) / 2
	riverSize := float32(math.Max(12, cell*0.42))
	for i, t := range r.river {
		t.TextSize = riverSize
		ts := measureTextCached(t.Text, riverSize, t.TextStyle)
		fx := geom.x(2) - float64(ts.Width)/2
		if i == 1 {
			fx = geom.x(6) - float64(ts.Width)/2
		}
		t.Resize(ts)
		t.Move(fyne.NewPos(float32(fx), float32(riverY-float64(ts.Height)/2)))
	}

	// --- 四周坐标标注 ---
	// v1.5：不再绘制四周坐标标注（用户要求去掉 a~i / 0~9）
	if len(r.coords) > 0 {
		for _, c := range r.coords {
			c.Hide()
		}
	}
	coordSize := float32(math.Max(9, cell*0.26))
	labOff := geomLabOff * cell
	idx := 0
	skipCoords := !b.coordsVisible() // 默认不显示，可在「设置 → 显示棋盘坐标」打开
	put := func(s string, cx, cy float64) {
		if skipCoords {
			return
		}
		if idx >= len(r.coords) {
			return
		}
		t := r.coords[idx]
		idx++
		t.Text = s
		t.TextSize = coordSize
		ts := measureTextCached(s, coordSize, t.TextStyle)
		t.Resize(ts)
		t.Move(fyne.NewPos(float32(cx-float64(ts.Width)/2), float32(cy-float64(ts.Height)/2)))
		t.Show()
	}
	// 四周坐标标注：翻转时列字母与行号都要跟着换（棋子已经翻过去了，标注不能留在原地）
	fileLabel := func(f int) string {
		if geom.flip {
			f = rules.Files - 1 - f
		}
		return string(rune('a' + f))
	}
	rankLabel := func(rank int) string {
		if geom.flip {
			return fmt.Sprint(rules.Ranks - 1 - rank)
		}
		return fmt.Sprint(rank)
	}
	// 下边（靠近屏幕底部的文件字母）
	for f := 0; f < rules.Files; f++ {
		put(fileLabel(f), geom.x(f), geom.y(0)+labOff)
	}
	// 上边
	for f := 0; f < rules.Files; f++ {
		put(fileLabel(f), geom.x(f), geom.y(9)-labOff)
	}
	// 左边行号
	for rank := 0; rank < rules.Ranks; rank++ {
		put(rankLabel(rank), geom.x(0)-labOff, geom.y(rank))
	}
	// 右边行号
	for rank := 0; rank < rules.Ranks; rank++ {
		put(rankLabel(rank), geom.x(8)+labOff, geom.y(rank))
	}
	for ; idx < len(r.coords); idx++ {
		r.coords[idx].Hide()
	}

	// --- 棋子 ---
	sk := b.currentSkin()
	rad := cell * 0.435
	shadowOff := math.Max(1, cell*0.045)
	textSize := float32(math.Max(11, cell*0.55))

	b.mu.Lock()
	game := b.game
	b.mu.Unlock()

	r.lastGeom = geom
	r.animIdx = -1
	// 走子动画：读一次进度，循环里给「刚落在 animTo 上的那枚棋子」做位置插值
	animFrom, animTo, animT, animOn := b.animState()
	animEase := animT * animT * (3 - 2*animT) // smoothstep 缓动
	dragSq, dragPos, dragging, dragOver := b.dragState()
	// 预览棋子需要「落点是否合法」，锁内取一次快照
	b.mu.Lock()
	targetsSnap := append([]int(nil), b.targets...)
	b.mu.Unlock()

	pi := 0
	if game != nil {
		for sq := 0; sq < rules.Squares && pi < len(r.pieces); sq++ {
			p := game.Board.Sq[sq]
			if p.IsEmpty() {
				continue
			}
			v := r.pieces[pi]
			pi++
			file, rank := rules.FileOf(sq), rules.RankOf(sq)
			cx, cy := geom.fit(file, rank)
			if dragging && sq == dragSq {
				// 拖动中：棋子直接画在指针位置（拖到哪看到哪）
				cx, cy = float64(dragPos.X), float64(dragPos.Y)
			}
			if animOn && sq == animTo && animFrom >= 0 {
				r.animIdx = pi - 1
				x0, y0 := geom.fit(rules.FileOf(animFrom), rules.RankOf(animFrom))
				cx = x0 + (cx-x0)*animEase
				cy = y0 + (cy-y0)*animEase
			}

			v.shadow.Move(fyne.NewPos(float32(cx-rad+shadowOff), float32(cy-rad+shadowOff)))
			v.shadow.Resize(fyne.NewSize(float32(2*rad), float32(2*rad)))
			v.shadow.Show()

			v.body.Move(fyne.NewPos(float32(cx-rad), float32(cy-rad)))
			v.body.Resize(fyne.NewSize(float32(2*rad), float32(2*rad)))
			if p.Side() == rules.Red {
				v.body.FillColor = sk.RedFill
				v.body.StrokeColor = sk.RedEdge
				v.text.Color = sk.RedText
			} else {
				v.body.FillColor = sk.BlackFill
				v.body.StrokeColor = sk.BlackEdge
				v.text.Color = sk.BlackText
			}
			v.body.StrokeWidth = float32(math.Max(1.2, cell*0.035))
			v.body.Show()

			// 棋子高光：左上角一小片柔光，让棋子看起来是"有厚度的圆片"而不是平涂圆。
			// 皮肤给 PieceGloss 的 A=0 时整层不显示（浅色皮肤保持原样）。
			if v.gloss != nil {
				if gA := toNRGBA(sk.PieceGloss).A; gA > 0 {
					gr := rad * 0.62
					v.gloss.FillColor = sk.PieceGloss
					v.gloss.Move(fyne.NewPos(float32(cx-rad*0.34), float32(cy-rad*0.42)))
					v.gloss.Resize(fyne.NewSize(float32(2*gr), float32(2*gr)))
					v.gloss.Show()
				} else {
					v.gloss.Hide()
				}
			}

			v.text.Text = p.Name()
			v.text.TextSize = textSize
			ts := measureTextCached(v.text.Text, textSize, v.text.TextStyle)
			v.text.Resize(ts)
			// 汉字在等宽方块中视觉重心略偏上，向下微调 2% 格宽
			v.text.Move(fyne.NewPos(
				float32(cx-float64(ts.Width)/2),
				float32(cy-float64(ts.Height)/2+cell*0.02)))
			v.text.Show()
		}
	}
	for ; pi < len(r.pieces); pi++ {
		v := r.pieces[pi]
		v.shadow.Hide()
		v.body.Hide()
		v.text.Hide()
		if v.gloss != nil {
			v.gloss.Hide()
		}
	}

	// 【v1.6.9 / 头脑风暴第 16 条】拖动落点预览：指针停在合法落点上时，
	// 就在那个格子里画一枚半透明棋子——松手之前已经看得出「放下去是什么样」。
	if dragging && dragOver >= 0 && game != nil && containsInt(targetsSnap, dragOver) {
		src := game.Board.Sq[dragSq]
		if dragSq >= 0 && !src.IsEmpty() {
			gx, gy := geom.fit(rules.FileOf(dragOver), rules.RankOf(dragOver))
			r.ghost.body.Move(fyne.NewPos(float32(gx-rad), float32(gy-rad)))
			r.ghost.body.Resize(fyne.NewSize(float32(2*rad), float32(2*rad)))
			r.ghost.body.FillColor = color.NRGBA{R: 0xF6, G: 0xF0, B: 0xE2, A: 0x96}
			r.ghost.body.StrokeColor = color.NRGBA{R: 0x8A, G: 0x7C, B: 0x63, A: 0xC8}
			r.ghost.body.Show()
			r.ghost.text.Text = src.Name()
			r.ghost.text.TextSize = textSize
			r.ghost.text.Color = ghostTextColor(src)
			gts := measureTextCached(r.ghost.text.Text, textSize, r.ghost.text.TextStyle)
			r.ghost.text.Resize(gts)
			r.ghost.text.Move(fyne.NewPos(
				float32(gx-float64(gts.Width)/2),
				float32(gy-float64(gts.Height)/2+cell*0.02)))
			r.ghost.text.Show()
		} else {
			r.ghost.body.Hide()
			r.ghost.text.Hide()
		}
	} else {
		r.ghost.body.Hide()
		r.ghost.text.Hide()
	}
}

func (r *boardRenderer) MinSize() fyne.Size { return r.b.MinSize() }

func (r *boardRenderer) Refresh() {
	sk := r.b.currentSkin()
	r.selRect.FillColor = sk.SelectFill
	r.selRect.StrokeColor = sk.SelectEdge
	r.lfRect.FillColor = sk.LastFrom
	r.ltRect.FillColor = sk.LastTo
	r.hovRect.FillColor = sk.LastFrom
	r.chkRect.FillColor = sk.CheckGlow
	for _, d := range r.dots {
		d.FillColor = sk.TargetDot
	}
	for _, t := range r.river {
		t.Color = sk.RiverText
	}
	for _, t := range r.coords {
		t.Color = sk.CoordText
	}
	for _, p := range r.pieces {
		p.shadow.FillColor = sk.PieceShadow
	}
	// 仅当皮肤真正变化时才让底板重新光栅化（否则每次悬停/点击都重画整块位图）
	if sv := r.b.skinVersion(); !r.skinInit || sv != r.skinSeen {
		r.skinSeen, r.skinInit = sv, true
		r.plateMu.Lock()
		r.plateVer = -1
		r.plateMu.Unlock()
		r.raster.Refresh()
	}

	r.Layout(r.b.layoutSize())
	canvas.Refresh(r.b)
}

func (r *boardRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *boardRenderer) Destroy() { r.b.stopBlink() }
