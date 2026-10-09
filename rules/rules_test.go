package rules

import "testing"

// perft 统计从当前局面出发、走 depth 层的完全合法着法总数。
func perft(b *Board, depth int) int {
	if depth == 0 {
		return 1
	}
	moves := b.LegalMoves()
	if depth == 1 {
		return len(moves)
	}
	n := 0
	for _, m := range moves {
		cap := b.Apply(m)
		n += perft(b, depth-1)
		b.Revert(m, cap)
	}
	return n
}

// TestPerftStartpos 用国际通行的中国象棋 perft 参考值校验走法生成器。
//
// 参考值（初始局面）：
//
//	深度 1 = 44        深度 2 = 1920        深度 3 = 79666        深度 4 = 3290240
//
// 只要这四个数字完全一致，就可以确信走法生成、蹩马腿、塞象眼、炮隔子、
// 兵过河、九宫限制、将帅照面与自将检查全部实现正确。
func TestPerftStartpos(t *testing.T) {
	cases := []struct {
		depth int
		want  int
	}{
		{1, 44},
		{2, 1920},
		{3, 79666},
		{4, 3290240},
	}
	for _, c := range cases {
		b := NewStartBoard()
		got := perft(b, c.depth)
		if got != c.want {
			t.Errorf("perft(%d) = %d，期望 %d", c.depth, got, c.want)
		}
	}
}

func TestStartFENRoundTrip(t *testing.T) {
	b := NewStartBoard()
	fen := b.FEN()
	if fen != StartFEN {
		t.Fatalf("初始局面 FEN 往返失败：\n得到 %s\n期望 %s", fen, StartFEN)
	}
	b2, err := ParseFEN(fen)
	if err != nil {
		t.Fatalf("解析初始 FEN 失败：%v", err)
	}
	if b2.Sq != b.Sq || b2.Side != b.Side {
		t.Fatal("FEN 往返后局面不一致")
	}
}

// TestCoordinates 校验坐标换算（a0 红车、e0 红帅、e9 黑将、i9 黑车）。
func TestCoordinates(t *testing.T) {
	cases := []struct {
		sq   int
		name string
		file int
		rank int
	}{
		{0, "a0", 0, 0},
		{4, "e0", 4, 0},
		{8, "i0", 8, 0},
		{85, "e9", 4, 9},
		{89, "i9", 8, 9},
		{81, "a9", 0, 9},
	}
	for _, c := range cases {
		if got := SquareName(c.sq); got != c.name {
			t.Errorf("SquareName(%d) = %s，期望 %s", c.sq, got, c.name)
		}
		if got := Index(c.file, c.rank); got != c.sq {
			t.Errorf("Index(%d,%d) = %d，期望 %d", c.file, c.rank, got, c.sq)
		}
		if got, ok := ParseSquare(c.name); !ok || got != c.sq {
			t.Errorf("ParseSquare(%s) = %d,%v，期望 %d,true", c.name, got, ok, c.sq)
		}
	}
	// 屏幕行换算：rank 9 在屏幕第 0 行，rank 0 在第 9 行
	if ScreenRow(9) != 0 || ScreenRow(0) != 9 {
		t.Errorf("ScreenRow 换算错误：ScreenRow(9)=%d ScreenRow(0)=%d", ScreenRow(9), ScreenRow(0))
	}
}

// TestInitialPieces 校验初始局面的子力与位置。
func TestInitialPieces(t *testing.T) {
	b := NewStartBoard()
	if b.Sq[Index(4, 0)] != PRedKing {
		t.Error("e0 应为红帅")
	}
	if b.Sq[Index(4, 9)] != PBlackKing {
		t.Error("e9 应为黑将")
	}
	if b.Sq[Index(0, 0)] != PRedRook || b.Sq[Index(8, 0)] != PRedRook {
		t.Error("a0/i0 应为红車")
	}
	if b.Sq[Index(1, 2)] != PRedCannon || b.Sq[Index(7, 2)] != PRedCannon {
		t.Error("b2/h2 应为红炮")
	}
	// 红方子力：車 2×9 + 馬 2×4 + 炮 2×4 + 相 2×2 + 仕 2×2 + 兵 5×1 + 帅 0 = 47
	if got := b.Material(Red); got != 47 {
		t.Errorf("红方子力 = %d，期望 47", got)
	}
	if got := b.Material(Black); got != 47 {
		t.Errorf("黑方子力 = %d，期望 47", got)
	}
}

// TestHorseLeg 校验蹩马腿。
func TestHorseLeg(t *testing.T) {
	// 空盘 + 红馬在 e4(4,4) + 红帅/黑将放在不同纵线（避免将帅照面导致局面本身非法）
	b := &Board{}
	b.Sq[Index(4, 4)] = PRedHorse
	b.Sq[Index(3, 0)] = PRedKing
	b.Sq[Index(5, 9)] = PBlackKing
	b.Side = Red

	targets := map[int]bool{}
	for _, m := range b.LegalMoves() {
		if m.From == Index(4, 4) {
			targets[m.To] = true
		}
	}
	// 馬走日共 8 个落点，全部在盘内
	if len(targets) != 8 {
		t.Fatalf("空盘馬的落点数 = %d，期望 8", len(targets))
	}
	// 在 (4,5) 放一枚子 → 蹩住向上两条腿
	b.Sq[Index(4, 5)] = PBlackPawn
	targets = map[int]bool{}
	for _, m := range b.LegalMoves() {
		if m.From == Index(4, 4) {
			targets[m.To] = true
		}
	}
	if targets[Index(3, 6)] || targets[Index(5, 6)] {
		t.Error("馬腿被蹩住后仍能走到 (3,6)/(5,6)")
	}
	if len(targets) != 6 {
		t.Errorf("蹩一条腿后落点数 = %d，期望 6", len(targets))
	}
}

// TestElephantEye 校验塞象眼与象不过河。
func TestElephantEye(t *testing.T) {
	b := &Board{}
	b.Sq[Index(2, 0)] = PRedElephant
	b.Sq[Index(3, 0)] = PRedKing // 红帅放 d0，与黑将不同纵线
	b.Sq[Index(5, 9)] = PBlackKing
	b.Side = Red
	moves := map[int]bool{}
	for _, m := range b.LegalMoves() {
		if m.From == Index(2, 0) {
			moves[m.To] = true
		}
	}
	// 象从 (2,0) 只能走 (0,2) 与 (4,2)
	if !moves[Index(0, 2)] || !moves[Index(4, 2)] {
		t.Error("相 (2,0) 应能走 (0,2)/(4,2)")
	}
	if moves[Index(0, 4)] || moves[Index(4, 4)] {
		t.Error("相不能连走两步")
	}
	// 塞象眼 (1,1)
	b.Sq[Index(1, 1)] = PBlackPawn
	moves = map[int]bool{}
	for _, m := range b.LegalMoves() {
		if m.From == Index(2, 0) {
			moves[m.To] = true
		}
	}
	if moves[Index(0, 2)] {
		t.Error("象眼被塞后仍能走到 (0,2)")
	}
	if !moves[Index(4, 2)] {
		t.Error("象眼被塞不应影响另一侧走法")
	}
}

// TestCannonScreen 校验炮必须隔一个子才能吃子。
func TestCannonScreen(t *testing.T) {
	b := &Board{}
	b.Sq[Index(0, 0)] = PRedCannon
	b.Sq[Index(3, 0)] = PRedKing // 红帅 d0，与黑将不同纵线
	b.Sq[Index(5, 9)] = PBlackKing
	b.Sq[Index(0, 9)] = PBlackRook // 炮的直线上有敌車
	b.Side = Red

	// 无炮架：只能平移到空格，不能吃 (0,9) 的車
	canCapture := false
	for _, m := range b.LegalMoves() {
		if m.From == Index(0, 0) && m.To == Index(0, 9) {
			canCapture = true
		}
	}
	if canCapture {
		t.Error("无炮架时炮不应能吃到 (0,9) 的車")
	}
	// 加一个炮架在 (0,4)
	b.Sq[Index(0, 4)] = PBlackPawn
	canCapture = false
	for _, m := range b.LegalMoves() {
		if m.From == Index(0, 0) && m.To == Index(0, 9) {
			canCapture = true
		}
	}
	if !canCapture {
		t.Error("有一个炮架时炮应能吃 (0,9) 的車")
	}
	// 再加一个炮架 → 两个炮架不能吃
	b.Sq[Index(0, 6)] = PBlackPawn
	canCapture = false
	for _, m := range b.LegalMoves() {
		if m.From == Index(0, 0) && m.To == Index(0, 9) {
			canCapture = true
		}
	}
	if canCapture {
		t.Error("有两个炮架时炮不应能吃子")
	}
}

// TestKingsFacing 校验将帅照面（走出照面的一方着法非法）。
//
// 局面：红帅 e0、黑将 e9、黑車 e5 挡在中间 → 当前不照面。
// 黑車若横走出 e 线，将帅即照面，因此「車 e5 → a5」必须是非法着法；
// 而「車 e5 → e6」仍挡在中间，必须合法。
func TestKingsFacing(t *testing.T) {
	b := &Board{}
	b.Sq[Index(4, 0)] = PRedKing
	b.Sq[Index(4, 9)] = PBlackKing
	b.Sq[Index(4, 5)] = PBlackRook
	b.Side = Black

	if b.KingsFacing() {
		t.Fatal("构造局面不应照面")
	}
	legal := map[int]bool{}
	for _, m := range b.LegalMoves() {
		if m.From == Index(4, 5) {
			legal[m.To] = true
		}
	}
	if legal[Index(0, 5)] {
		t.Error("黑車从 e5 走到 a5 会造成将帅照面，应为非法着法")
	}
	if !legal[Index(4, 6)] {
		t.Error("黑車从 e5 走到 e6 仍挡在两将之间，应为合法着法")
	}
}

// TestCheckmate 校验将死判定（红双車杀黑将）。
//
// 局面：红車 a9 控 rank 9，红車 b8 控 rank 8，黑将 e9，红帅 d0（避免照面）。
// 黑将三个可走点 d9/f9/e8 全部被攻击 → 将死。
func TestCheckmate(t *testing.T) {
	b, err := ParseFEN("R3k4/1R7/9/9/9/9/9/9/9/3K5 b - - 0 1")
	if err != nil {
		t.Fatalf("解析 FEN 失败：%v", err)
	}
	if !b.InCheck(Black) {
		t.Fatal("构造局面应判定黑方被将军")
	}
	if n := len(b.LegalMoves()); n != 0 {
		t.Fatalf("构造局面下黑方应无合法着法，实际 %d 步", n)
	}
	st, reason := (&Game{Board: b}).Adjudicate()
	if st != RedWin {
		t.Fatalf("黑方被将死应判红方胜，实际 %v（%s）", st, reason)
	}
	if reason != "将死" {
		t.Errorf("判定原因 = %q，期望 %q", reason, "将死")
	}
}

// TestStalemateIsLoss 校验困毙（无子可动但未被将军）在中国象棋中同样判负。
//
// 局面：黑将 d9；红車 a8 控 rank 8（封 d8），红車 e1 控 e 线（封 e9）；
// 黑将既未被将军又无处可走（c9 在九宫之外不可去）→ 困毙，判黑方负。
func TestStalemateIsLoss(t *testing.T) {
	b, err := ParseFEN("3k5/R8/9/9/9/9/9/9/4R4/4K4 b - - 0 1")
	if err != nil {
		t.Fatalf("解析 FEN 失败：%v", err)
	}
	if b.InCheck(Black) {
		t.Fatal("构造局面下黑方不应被将军（否则不是困毙）")
	}
	if n := len(b.LegalMoves()); n != 0 {
		t.Fatalf("构造局面下黑方应无合法着法，实际 %d 步", n)
	}
	st, reason := (&Game{Board: b}).Adjudicate()
	if st != RedWin || reason != "困毙" {
		t.Errorf("困毙应判走子方负（红方胜/困毙），实际 %v/%s", st, reason)
	}
}

// TestPawnRiver 校验兵过河后才能横走。
func TestPawnRiver(t *testing.T) {
	b := &Board{}
	b.Sq[Index(0, 3)] = PRedPawn // 未过河
	b.Sq[Index(3, 0)] = PRedKing // 红帅 d0，与黑将不同纵线
	b.Sq[Index(5, 9)] = PBlackKing
	b.Side = Red
	n := 0
	for _, m := range b.LegalMoves() {
		if m.From == Index(0, 3) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("未过河的兵应有 1 步可走，实际 %d", n)
	}
	b.Sq[Index(0, 3)] = PEmpty
	b.Sq[Index(0, 6)] = PRedPawn // 已过河
	n = 0
	side := map[int]bool{}
	for _, m := range b.LegalMoves() {
		if m.From == Index(0, 6) {
			n++
			side[m.To] = true
		}
	}
	if n != 2 {
		t.Errorf("过河后的兵应有 2 步可走（前 1 + 横 1），实际 %d", n)
	}
	if !side[Index(1, 6)] {
		t.Error("过河后的兵应能横走到 (1,6)")
	}
}

// TestUndoRestores 校验悔棋能完整还原局面。
func TestUndoRestores(t *testing.T) {
	g := NewGame()
	before := g.Board.FEN()
	m, ok := parseMove("h2e2")
	if !ok {
		t.Fatal("解析 h2e2 失败")
	}
	if err := g.TryMove(m); err != nil {
		t.Fatalf("走炮二平五失败：%v", err)
	}
	if g.Board.FEN() == before {
		t.Fatal("走子后局面未变化")
	}
	if !g.Undo() {
		t.Fatal("悔棋失败")
	}
	if g.Board.FEN() != before {
		t.Fatalf("悔棋后局面未还原：\n得到 %s\n期望 %s", g.Board.FEN(), before)
	}
}

func parseMove(s string) (Move, bool) {
	if len(s) != 4 {
		return Move{}, false
	}
	f1, r1 := int(s[0]-'a'), int(s[1]-'0')
	f2, r2 := int(s[2]-'a'), int(s[3]-'0')
	if !OnBoard(f1, r1) || !OnBoard(f2, r2) {
		return Move{}, false
	}
	return NewMove(Index(f1, r1), Index(f2, r2)), true
}
