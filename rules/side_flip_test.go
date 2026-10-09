package rules

import "testing"

// TestSideFlipsAfterMove 走一步后走子方必须翻转、FEN 的走子方字段必须同步。
//
// 为什么单独加这条：真机验证时，红兵 c3→c4 之后状态栏仍显示「轮到：红方走棋」、
// FEN 走子方仍是 "w"。若属实，就是严重缺陷——FEN 会被下发给引擎
// （position fen ... moves ...），走子方错了引擎就会按错误的行棋方思考。
// 这条测试把「规则层」钉死，用来区分是规则算错还是界面显示错。
func TestSideFlipsAfterMove(t *testing.T) {
	g := NewGame()

	if g.Board.Side != Red {
		t.Fatalf("初始局面应该红方走棋，实际 Side=%v FEN=%q", g.Board.Side, g.Board.FEN())
	}
	// 红兵 c3(Index(2,3)=29) → c4(Index(2,4)=38)
	from, to := Index(2, 3), Index(2, 4)
	if err := g.TryMove(NewMove(from, to)); err != nil {
		t.Fatalf("c3c4 应该是合法着法，却报错：%v", err)
	}
	if len(g.Moves) != 1 {
		t.Fatalf("走完一步后 Moves 长度应为 1，实际 %d", len(g.Moves))
	}
	if g.Board.Side != Black {
		t.Fatalf("红方走完后应轮到黑方，实际 Side=%v FEN=%q", g.Board.Side, g.Board.FEN())
	}
	fen := g.Board.FEN()
	// FEN 第 2 段是走子方：w=红 b=黑
	sp := -1
	for i := 0; i < len(fen); i++ {
		if fen[i] == ' ' {
			sp = i
			break
		}
	}
	if sp < 0 || sp+1 >= len(fen) || fen[sp+1] != 'b' {
		t.Fatalf("走完红方一步后 FEN 的走子方字段应为 b，实际 FEN=%q", fen)
	}
	// 同时确认局面本身：c4 有红兵、c3 已空
	if g.Board.Sq[to].IsEmpty() || g.Board.Sq[to].Side() != Red {
		t.Fatalf("c4 应该有红方棋子，实际 %v", g.Board.Sq[to])
	}
	if !g.Board.Sq[from].IsEmpty() {
		t.Fatalf("c3 应该已空，实际 %v", g.Board.Sq[from])
	}
}
