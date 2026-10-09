package rules

// 本文件实现走法生成与攻击判定。
//
// 走法生成分两层：
//  1. genPseudo —— 伪合法着法：只考虑棋子自身走子规则（马蹩腿、象塞眼、炮隔子、
//     兵过河、九宫限制等），不检查走完后己方是否被将军、是否形成将帅照面。
//  2. LegalMoves —— 在伪合法着法基础上，逐着试走并剔除「走后被将军」与
//     「走后将帅照面」的着法，即完全合法着法。

// Move 表示一步着法。
type Move struct {
	From int // 起点（线性索引）
	To   int // 终点（线性索引）
}

// NewMove 由起终点构造着法。
func NewMove(from, to int) Move { return Move{From: from, To: to} }

// String 返回 UCI 形式的着法（如 "h2e2"）。
func (m Move) String() string { return SquareName(m.From) + SquareName(m.To) }

// Piece 返回该着法在给定局面下的走子（走子前调用）。
func (m Move) Piece(b *Board) Piece { return b.Sq[m.From] }

// 正交与斜向增量（文件增量, 行增量）
var (
	orthoDirs = [4][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}
	diagDirs  = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
)

// 马的 8 种走法：{文件增量, 行增量, 马腿文件增量, 马腿行增量}
var horseMoves = [8][4]int{
	{1, 2, 0, 1}, {1, -2, 0, -1}, {-1, 2, 0, 1}, {-1, -2, 0, -1},
	{2, 1, 1, 0}, {2, -1, 1, 0}, {-2, 1, -1, 0}, {-2, -1, -1, 0},
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// inPalace 判断 (file,rank) 是否落在 side 方的九宫内。
//
//	红方九宫：文件 3..5，行 0..2
//	黑方九宫：文件 3..5，行 7..9
func inPalace(side, file, rank int) bool {
	if file < 3 || file > 5 {
		return false
	}
	if side == Red {
		return rank >= 0 && rank <= 2
	}
	return rank >= 7 && rank <= 9
}

// inOwnHalf 判断某行是否在 side 方自己的半场内（象/相不可过河）。
//
//	红方半场：行 0..4      黑方半场：行 5..9
func inOwnHalf(side, rank int) bool {
	if side == Red {
		return rank >= 0 && rank <= 4
	}
	return rank >= 5 && rank <= 9
}

// crossedRiver 判断位于 (side, rank) 的兵/卒是否已经过河。
//
//	红兵过河：行 >= 5      黑卒过河：行 <= 4
func crossedRiver(side, rank int) bool { return !inOwnHalf(side, rank) }

// forward 返回 side 方的前进方向（红方向行号增大方向走，黑方相反）。
func forward(side int) int {
	if side == Red {
		return 1
	}
	return -1
}

// addMove 若 (file,rank) 在盘内则追加一个着法。
func addMove(dst []Move, from, file, rank int) []Move {
	if !OnBoard(file, rank) {
		return dst
	}
	return append(dst, Move{From: from, To: Index(file, rank)})
}

// genPseudo 生成 side 方全部伪合法着法。
func (b *Board) genPseudo(side int) []Move {
	out := make([]Move, 0, 64)
	for from := 0; from < Squares; from++ {
		p := b.Sq[from]
		if p.IsEmpty() || p.Side() != side {
			continue
		}
		file, rank := FileOf(from), RankOf(from)
		switch p.Type() {
		case PKing:
			// 帅/将：九宫内直走一步
			for _, d := range orthoDirs {
				nf, nr := file+d[0], rank+d[1]
				if !inPalace(side, nf, nr) {
					continue
				}
				if t := b.Sq[Index(nf, nr)]; t.IsEmpty() || t.Side() != side {
					out = append(out, Move{From: from, To: Index(nf, nr)})
				}
			}
		case PAdvisor:
			// 仕/士：九宫内斜走一步
			for _, d := range diagDirs {
				nf, nr := file+d[0], rank+d[1]
				if !inPalace(side, nf, nr) {
					continue
				}
				if t := b.Sq[Index(nf, nr)]; t.IsEmpty() || t.Side() != side {
					out = append(out, Move{From: from, To: Index(nf, nr)})
				}
			}
		case PElephant:
			// 相/象：斜走两格，不可过河，象眼被占则不可走
			for _, d := range diagDirs {
				nf, nr := file+2*d[0], rank+2*d[1]
				if !OnBoard(nf, nr) || !inOwnHalf(side, nr) {
					continue
				}
				// 塞象眼
				if !b.Sq[Index(file+d[0], rank+d[1])].IsEmpty() {
					continue
				}
				if t := b.Sq[Index(nf, nr)]; t.IsEmpty() || t.Side() != side {
					out = append(out, Move{From: from, To: Index(nf, nr)})
				}
			}
		case PHorse:
			// 馬/马：走日字，蹩马腿则不可走
			for _, hm := range horseMoves {
				nf, nr := file+hm[0], rank+hm[1]
				if !OnBoard(nf, nr) {
					continue
				}
				if !b.Sq[Index(file+hm[2], rank+hm[3])].IsEmpty() {
					continue // 蹩马腿
				}
				if t := b.Sq[Index(nf, nr)]; t.IsEmpty() || t.Side() != side {
					out = append(out, Move{From: from, To: Index(nf, nr)})
				}
			}
		case PRook:
			// 車/车：直线滑行，遇子停止
			for _, d := range orthoDirs {
				nf, nr := file+d[0], rank+d[1]
				for OnBoard(nf, nr) {
					t := b.Sq[Index(nf, nr)]
					if t.IsEmpty() {
						out = append(out, Move{From: from, To: Index(nf, nr)})
					} else {
						if t.Side() != side {
							out = append(out, Move{From: from, To: Index(nf, nr)})
						}
						break
					}
					nf, nr = nf+d[0], nr+d[1]
				}
			}
		case PCannon:
			// 炮：直线滑行不吃子；隔且仅隔一子时可吃子
			for _, d := range orthoDirs {
				nf, nr := file+d[0], rank+d[1]
				// 第一段：空格，可以平移到此处
				for OnBoard(nf, nr) && b.Sq[Index(nf, nr)].IsEmpty() {
					out = append(out, Move{From: from, To: Index(nf, nr)})
					nf, nr = nf+d[0], nr+d[1]
				}
				// 越过炮架
				if !OnBoard(nf, nr) {
					continue
				}
				nf, nr = nf+d[0], nr+d[1]
				// 第二段：找到第一个棋子，若是敌子则可吃
				for OnBoard(nf, nr) {
					if t := b.Sq[Index(nf, nr)]; !t.IsEmpty() {
						if t.Side() != side {
							out = append(out, Move{From: from, To: Index(nf, nr)})
						}
						break
					}
					nf, nr = nf+d[0], nr+d[1]
				}
			}
		case PPawn:
			// 兵/卒：向前一步；过河后可左右一步；不可后退
			fw := forward(side)
			// 向前一步（到达对方底线后不再前移，但仍可横走）
			if OnBoard(file, rank+fw) {
				if t := b.Sq[Index(file, rank+fw)]; t.IsEmpty() || t.Side() != side {
					out = append(out, Move{From: from, To: Index(file, rank+fw)})
				}
			}
			if crossedRiver(side, rank) {
				for _, df := range []int{-1, 1} {
					nf := file + df
					if !OnBoard(nf, rank) {
						continue
					}
					if t := b.Sq[Index(nf, rank)]; t.IsEmpty() || t.Side() != side {
						out = append(out, Move{From: from, To: Index(nf, rank)})
					}
				}
			}
		}
	}
	return out
}

// countBetween 统计同一直线上 from 与 to 之间的棋子数（不含两端）。
// 若两点不在同一行/同一列，返回 -1。
func (b *Board) countBetween(from, to int) int {
	ff, fr := FileOf(from), RankOf(from)
	tf, tr := FileOf(to), RankOf(to)
	df, dr := 0, 0
	switch {
	case fr == tr && ff != tf:
		if tf > ff {
			df = 1
		} else {
			df = -1
		}
	case ff == tf && fr != tr:
		if tr > fr {
			dr = 1
		} else {
			dr = -1
		}
	default:
		return -1
	}
	n := 0
	f, r := ff+df, fr+dr
	for f != tf || r != tr {
		if !OnBoard(f, r) {
			return -1
		}
		if !b.Sq[Index(f, r)].IsEmpty() {
			n++
		}
		f, r = f+df, r+dr
	}
	return n
}

// pieceAttacks 判断 from 格的棋子 p 是否攻击 to 格。
//
// 语义与「to 格上放着一枚 p 的敌子」时的走法生成一致：
// 炮需要恰好一个炮架才能吃子，車需要中间无子，兵需要前进或（过河后）横走。
// 因此调用方必须保证 to 格上是 p 的敌子（本包仅在「判断己方将/帅是否被攻击」时使用）。
func (b *Board) pieceAttacks(from, to int, p Piece) bool {
	if from == to {
		return false
	}
	ff, fr := FileOf(from), RankOf(from)
	tf, tr := FileOf(to), RankOf(to)
	df, dr := tf-ff, tr-fr
	switch p.Type() {
	case PKing:
		if abs(df)+abs(dr) != 1 {
			return false
		}
		return inPalace(p.Side(), tf, tr)
	case PAdvisor:
		if abs(df) != 1 || abs(dr) != 1 {
			return false
		}
		return inPalace(p.Side(), tf, tr)
	case PElephant:
		if abs(df) != 2 || abs(dr) != 2 {
			return false
		}
		if !inOwnHalf(p.Side(), tr) {
			return false
		}
		return b.Sq[Index(ff+df/2, fr+dr/2)].IsEmpty() // 象眼
	case PHorse:
		if abs(df) == 1 && abs(dr) == 2 {
			return b.Sq[Index(ff, fr+dr/2)].IsEmpty() // 马腿
		}
		if abs(df) == 2 && abs(dr) == 1 {
			return b.Sq[Index(ff+df/2, fr)].IsEmpty() // 马腿
		}
		return false
	case PRook:
		if df != 0 && dr != 0 {
			return false
		}
		return b.countBetween(from, to) == 0
	case PCannon:
		if df != 0 && dr != 0 {
			return false
		}
		return b.countBetween(from, to) == 1 // 恰好一个炮架
	case PPawn:
		if df == 0 && dr == forward(p.Side()) {
			return true
		}
		if dr == 0 && abs(df) == 1 && crossedRiver(p.Side(), fr) {
			return true
		}
		return false
	}
	return false
}

// SquareAttackedBy 判断 sq 格是否被 bySide 方攻击。
// sq 格上应放有 bySide 方的敌子（本包只在判断将/帅被攻击时调用）。
func (b *Board) SquareAttackedBy(sq, bySide int) bool {
	for i := 0; i < Squares; i++ {
		p := b.Sq[i]
		if p.IsEmpty() || p.Side() != bySide {
			continue
		}
		if b.pieceAttacks(i, sq, p) {
			return true
		}
	}
	return false
}

// InCheck 判断 side 方是否正被将军。
func (b *Board) InCheck(side int) bool {
	k := b.FindKing(side)
	if k < 0 {
		return false
	}
	return b.SquareAttackedBy(k, 1-side)
}

// KingsFacing 判断双方将/帅是否照面（同一纵线且中间无子）。
// 照面局面在中国象棋中是非法局面，走出该局面的一方着法非法。
func (b *Board) KingsFacing() bool {
	rk := b.FindKing(Red)
	bk := b.FindKing(Black)
	if rk < 0 || bk < 0 {
		return false
	}
	if FileOf(rk) != FileOf(bk) {
		return false
	}
	return b.countBetween(rk, bk) == 0
}

// Apply 在局面上执行着法，返回被吃掉的子（供 Revert 使用），并交换走子方。
func (b *Board) Apply(m Move) Piece {
	captured := b.Sq[m.To]
	b.Sq[m.To] = b.Sq[m.From]
	b.Sq[m.From] = PEmpty
	b.Side = 1 - b.Side
	return captured
}

// Revert 撤销 Apply 执行过的着法。
func (b *Board) Revert(m Move, captured Piece) {
	b.Sq[m.From] = b.Sq[m.To]
	b.Sq[m.To] = captured
	b.Side = 1 - b.Side
}

// LegalMoves 生成当前走子方的全部完全合法着法
// （已剔除「走完后被将军」与「走完后将帅照面」的着法）。
func (b *Board) LegalMoves() []Move {
	side := b.Side
	pseudo := b.genPseudo(side)
	// 原地过滤：写指针不会超过读指针，安全复用同一底层数组
	out := pseudo[:0]
	for _, m := range pseudo {
		captured := b.Apply(m)
		ok := !b.InCheck(side) && !b.KingsFacing()
		b.Revert(m, captured)
		if ok {
			out = append(out, m)
		}
	}
	return out
}
