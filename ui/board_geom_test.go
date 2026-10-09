package ui

import (
	"math"
	"testing"

	"fyne.io/fyne/v2"

	"xiangqi/rules"
)

// TestBoardGeomScaledIsExact 守住 v1.3.1 踩过的坑：
// 光栅化底板时「逻辑几何 → 物理几何」必须整体等比放大。
//
// 事故回顾：generatePlate 里手写成 {cell: base.cell, ox: base.ox*sx, oy: base.oy*sy}，
// **漏乘了 cell**。于是底板格距停留在逻辑值、而棋子按物理值摆放，两者差 1.3 倍，
// 表现为「棋子都没落在格线交叉点上、外侧棋子跑出底板」。
//
// 本测试从两侧夹住这个不变量：
//  1. scaled(k) 与直接用放大后的尺寸算出来的几何必须一致（格距、原点都对得上）；
//  2. 交叉点间距必须等于 cell*k —— 也就是棋子摆放用的那套间距。
func TestBoardGeomScaledIsExact(t *testing.T) {
	const (
		lw = 446.9 // 逻辑宽（本机默认窗口实测值）
		lh = 595.8 // 逻辑高
		k  = 1.3   // Fyne content scale
	)
	logical := newBoardGeom(lw, lh)
	scaled := logical.scaled(k)

	// 1. 与「按物理尺寸直接算」的几何一致
	direct := newBoardGeom(lw*k, lh*k)
	if math.Abs(scaled.cell-direct.cell) > 1e-9 {
		t.Fatalf("scaled().cell = %v, 直接按物理尺寸算 = %v，二者必须相等", scaled.cell, direct.cell)
	}
	if math.Abs(scaled.ox-direct.ox) > 1e-9 || math.Abs(scaled.oy-direct.oy) > 1e-9 {
		t.Fatalf("scaled() 原点 = (%v,%v)，直接算 = (%v,%v)",
			scaled.ox, scaled.oy, direct.ox, direct.oy)
	}

	// 2. 格距必须是逻辑格距 × k（这就是事故点：漏乘 k 时这里会差 1.3 倍）
	if math.Abs(scaled.cell-logical.cell*k) > 1e-9 {
		t.Fatalf("scaled().cell = %v，期望 %v（= 逻辑格距 %v × %v）",
			scaled.cell, logical.cell*k, logical.cell, k)
	}

	// 3. 相邻交叉点在两个方向上都必须正好差一个 cell
	//    注意 y 随 rank 增大而**减小**（rank 9 在上），所以取绝对值。
	for file := 0; file < rules.Files-1; file++ {
		got := math.Abs(scaled.x(file+1) - scaled.x(file))
		if math.Abs(got-scaled.cell) > 1e-9 {
			t.Fatalf("file %d→%d 的横向间距 = %v，应等于 cell = %v", file, file+1, got, scaled.cell)
		}
	}
	for rank := 0; rank < rules.Ranks-1; rank++ {
		got := math.Abs(scaled.y(rank+1) - scaled.y(rank))
		if math.Abs(got-scaled.cell) > 1e-9 {
			t.Fatalf("rank %d→%d 的纵向间距 = %v，应等于 cell = %v", rank, rank+1, got, scaled.cell)
		}
	}
}

// TestHitMatchesRenderGeom 守住「点击命中尺寸 = 渲染尺寸」：
// 用渲染几何算出某个交叉点的像素位置，再把它交给 hit()，必须回到同一个格子。
//
// 注意方向换算：几何里的 y(rank) 用的是 rules.ScreenRow(rank)（rank 9 在屏幕最上方），
// 而规则层面的索引是 rules.Index(file, rank)（rank 0 = 红方底线）。
func TestHitMatchesRenderGeom(t *testing.T) {
	size := fyne.NewSize(446.9, 595.8)
	geom := newBoardGeom(float64(size.Width), float64(size.Height))

	for file := 0; file < rules.Files; file++ {
		for rank := 0; rank < rules.Ranks; rank++ {
			// 渲染时棋子圆心所在的位置（逻辑坐标）
			pos := fyne.NewPos(float32(geom.x(file)), float32(geom.y(rank)))
			sq, ok := geom.hit(pos)
			if !ok {
				t.Fatalf("交叉点 file=%d rank=%d 的位置 %v 被判为无效点击", file, rank, pos)
			}
			want := rules.Index(file, rank)
			if sq != want {
				t.Fatalf("交叉点 file=%d rank=%d 命中到 sq=%d（%s），期望 sq=%d（%s）",
					file, rank, sq, rules.SquareName(sq), want, rules.SquareName(want))
			}
		}
	}
}

// TestPlateGeometryCoversAllPieces 守住「所有棋子都在底板范围内」：
// 底板必须完整包住 9×10 个交叉点上的棋子圆（半径 cell*0.435）。
// 这条不变量正是「棋子没落在棋盘上」那个观感的判据。
func TestPlateGeometryCoversAllPieces(t *testing.T) {
	const lw, lh = 446.9, 595.8
	g := newBoardGeom(lw, lh)

	// 底板范围：格线区域外扩 geomPadFrac 格（与 drawBoardPlate 一致）
	x0 := g.x(0) - geomPadFrac*g.cell
	x1 := g.x(8) + geomPadFrac*g.cell
	y0 := g.y(9) - geomPadFrac*g.cell
	y1 := g.y(0) + geomPadFrac*g.cell

	pieceR := g.cell * 0.435 // Layout 里棋子半径
	if x0 > g.x(0)-pieceR {
		t.Fatalf("底板左边界 %.2f 在 a 线棋子左缘 %.2f 之内 —— 棋子会探出底板", x0, g.x(0)-pieceR)
	}
	if x1 < g.x(8)+pieceR {
		t.Fatalf("底板右边界 %.2f 在 i 线棋子右缘 %.2f 之内 —— 棋子会探出底板", x1, g.x(8)+pieceR)
	}
	if y0 > g.y(9)-pieceR {
		t.Fatalf("底板上边界 %.2f 在 rank9 棋子顶缘 %.2f 之内 —— 棋子会探出底板", y0, g.y(9)-pieceR)
	}
	if y1 < g.y(0)+pieceR {
		t.Fatalf("底板下边界 %.2f 在 rank0 棋子底缘 %.2f 之内 —— 棋子会探出底板", y1, g.y(0)+pieceR)
	}
}
