package rules

import "testing"

// 本文件只补 rules_test.go 尚未覆盖的两条**系统性**检查。
// （perft 1~4、马腿、象眼、炮架、将帅照面、将死、困毙、兵过河、撤销、坐标
// 已由 rules_test.go 覆盖，不在这里重复。）

// emptyBoard 返回一个空盘（无子），走子方为红。
func emptyBoard() *Board {
	b := NewStartBoard()
	for i := range b.Sq {
		b.Sq[i] = PEmpty
	}
	b.Side = Red
	return b
}

// TestKingAndAdvisorStayInPalace 将/帅与士/仕的任何合法着法都不能离开九宫。
//
// 逐个九宫格点摆放，穷举其合法着法并检查终点仍在九宫内。
func TestKingAndAdvisorStayInPalace(t *testing.T) {
	check := func(side int, typ Piece) {
		for rank := 0; rank < Ranks; rank++ {
			for file := 0; file < Files; file++ {
				if !inPalace(side, file, rank) {
					continue
				}
				b := emptyBoard()
				b.Side = side
				b.Sq[Index(file, rank)] = MakePiece(side, typ)
				// 对方给个帅/将，避免「找不到将」导致 InCheck 分支异常
				if side == Red {
					b.Sq[Index(4, 9)] = MakePiece(Black, PKing)
				} else {
					b.Sq[Index(4, 0)] = MakePiece(Red, PKing)
				}
				for _, m := range b.LegalMoves() {
					if m.From != Index(file, rank) {
						continue
					}
					if !inPalace(side, FileOf(m.To), RankOf(m.To)) {
						t.Fatalf("%s方 %s 在 %s 走到九宫外的 %s",
							SideName(side), typ.Name(), SquareName(m.From), SquareName(m.To))
					}
				}
			}
		}
	}
	check(Red, PKing)
	check(Red, PAdvisor)
	check(Black, PKing)
	check(Black, PAdvisor)
}

// TestNoLegalMoveLeavesSelfInCheckOrFacing 系统性检查：
// **任何**由 LegalMoves 生成的着法，走完之后都不得让己方被将军、也不得形成将帅照面。
//
// 这是走法生成器的最终不变量。用一批有代表性的局面逐着验证。
func TestNoLegalMoveLeavesSelfInCheckOrFacing(t *testing.T) {
	fens := []string{
		StartFEN,
		// 空 e 线、双方各有车的对攻局面
		"4k4/9/9/9/4r4/9/9/9/4R4/4K4 w - - 0 1",
		// 一方被将军
		"4k4/9/9/9/9/9/9/9/4R4/4K4 b - - 0 1",
		// 炮与炮架
		"3k5/9/9/9/4P4/9/9/9/4C4/4K4 w - - 0 1",
		// 马与蹩腿
		"3k5/9/9/4P4/9/2N6/9/9/9/4K4 w - - 0 1",
		// 兵过河
		"3k5/9/4P4/9/9/9/9/9/9/4K4 w - - 0 1",
	}
	for _, fen := range fens {
		b, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("FEN 解析失败 %q: %v", fen, err)
		}
		side := b.Side
		moves := b.LegalMoves()
		if len(moves) == 0 {
			continue
		}
		for _, m := range moves {
			cap := b.Apply(m)
			badCheck := b.InCheck(side)
			badFacing := b.KingsFacing()
			b.Revert(m, cap)
			if badCheck {
				t.Fatalf("[%s] 生成了走后自己被将军的着法：%s", fen, m)
			}
			if badFacing {
				t.Fatalf("[%s] 生成了走后将帅照面的着法：%s", fen, m)
			}
		}
	}
}

// TestLegalMovesIsPure 合法着法生成不得改变局面（Apply/Revert 必须严格互逆）。
func TestLegalMovesIsPure(t *testing.T) {
	b := NewStartBoard()
	before := b.FEN()
	_ = b.LegalMoves()
	_ = b.LegalMoves()
	if after := b.FEN(); after != before {
		t.Fatalf("生成合法着法改变了局面：\n前 %s\n后 %s", before, after)
	}
}
