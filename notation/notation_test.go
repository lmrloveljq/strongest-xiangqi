package notation

import (
	"testing"

	"xiangqi/rules"
)

// TestToChineseOpenings 校验开局常见着法的中文记谱。
func TestToChineseOpenings(t *testing.T) {
	cases := []struct {
		uci  string
		want string
	}{
		{"h2e2", "炮二平五"}, // 红炮从 h2 平到 e2（当头炮）
		{"b2e2", "炮八平五"}, // 红炮从 b2 平到 e2
		{"b0c2", "馬八进七"}, // 红馬从 b0 跳到 c2
		{"h0g2", "馬二进三"}, // 红馬从 h0 跳到 g2
		{"a0a1", "車九进一"}, // 红車从 a0 到 a1
		{"i0i1", "車一进一"}, // 红車从 i0 到 i1
		{"a3a4", "兵九进一"}, // 红兵从 a3 到 a4
		{"e3e4", "兵五进一"}, // 红兵从 e3 到 e4
		{"c0e2", "相七进五"}, // 红相从 c0 到 e2
		{"d0e1", "仕六进五"}, // 红仕从 d0 到 e1
		{"e0e1", "帅五进一"}, // 红帅从 e0 到 e1
	}
	g := rules.NewGame()
	for _, c := range cases {
		m, ok := UCIToMove(c.uci)
		if !ok {
			t.Fatalf("%s 解析失败", c.uci)
		}
		got := ToChinese(g.Board, m)
		if got != c.want {
			t.Errorf("%s 的记谱 = %q，期望 %q", c.uci, got, c.want)
		}
	}
}

// TestToChineseBlack 校验黑方用阿拉伯数字纵线号。
//
// 黑方纵线号自黑方右侧（屏幕左侧的 a 线）向左数：a=1、b=2、…、h=8、i=9。
// 因此 h9 → 黑馬在 8 线，跳到 g7（7 线）记作「马8进7」。
func TestToChineseBlack(t *testing.T) {
	g := rules.NewGame()
	// 先走红炮二平五，再走黑馬 h9g7
	m1, _ := UCIToMove("h2e2")
	if err := g.TryMove(m1); err != nil {
		t.Fatalf("红方着法失败：%v", err)
	}
	m2, _ := UCIToMove("h9g7")
	if got := ToChinese(g.Board, m2); got != "马8进7" {
		t.Errorf("h9g7 的记谱 = %q，期望 %q", got, "马8进7")
	}
	m3, _ := UCIToMove("b7b6")
	if got := ToChinese(g.Board, m3); got != "炮2进1" {
		t.Errorf("b7b6 的记谱 = %q，期望 %q", got, "炮2进1")
	}
}

// TestFrontBackPrefix 校验同一纵线上两枚同兵种棋子用「前/后」区分。
func TestFrontBackPrefix(t *testing.T) {
	// 两个红兵同在 e 线：e3 与 e6（e6 更靠近黑方 → 前）
	b, err := rules.ParseFEN("3k5/9/9/4P4/9/9/4P4/9/9/4K4 w - - 0 1")
	if err != nil {
		t.Fatalf("解析 FEN 失败：%v", err)
	}
	front, _ := UCIToMove("e6e7") // 前兵（更靠近黑方）
	back, _ := UCIToMove("e3e4")  // 后兵
	if got := ToChinese(b, front); got != "前兵进一" {
		t.Errorf("前兵记谱 = %q，期望 %q", got, "前兵进一")
	}
	if got := ToChinese(b, back); got != "后兵进一" {
		t.Errorf("后兵记谱 = %q，期望 %q", got, "后兵进一")
	}
}

// TestUCIRoundTrip 校验坐标 ↔ 着法字符串往返。
func TestUCIRoundTrip(t *testing.T) {
	for _, s := range []string{"a0a1", "e0e1", "i9i8", "h2e2", "b0c2"} {
		m, ok := UCIToMove(s)
		if !ok {
			t.Fatalf("%s 解析失败", s)
		}
		if got := MoveToUCI(m); got != s {
			t.Errorf("%s 往返后得到 %s", s, got)
		}
	}
}

// TestParseMoveList 校验从多种粘贴格式中提取 UCI 着法。
func TestParseMoveList(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"h2e2 h9g7 c3c4", 3},
		{"1. h2e2 h9g7 2. c3c4", 3},
		{"position startpos moves h2e2 h9g7", 2},
		{"h2e2,h9g7，c3c4、i0i1", 4},
		{"h2e2\nh9g7\nc3c4", 3},
		{"h2e2  xx  h9g7", 2}, // xx 应被忽略
		{"", 0},
	}
	for _, c := range cases {
		moves, _ := ParseMoveList(c.in)
		if len(moves) != c.want {
			t.Errorf("ParseMoveList(%q) 得到 %d 步，期望 %d 步", c.in, len(moves), c.want)
		}
	}
}

// TestParseMoveListAllLegal 校验解析出的序列能全部合法落到棋盘上。
func TestParseMoveListAllLegal(t *testing.T) {
	moves, skipped := ParseMoveList("h2e2 h9g7 c3c4 i9h9 b0c2 b9c7")
	if len(skipped) != 0 {
		t.Fatalf("不应有无法识别的记号：%v", skipped)
	}
	g := rules.NewGame()
	for i, m := range moves {
		if err := g.TryMove(m); err != nil {
			t.Fatalf("第 %d 步 %s 非法：%v", i+1, m, err)
		}
	}
	if len(g.Moves) != 6 {
		t.Fatalf("应走满 6 步，实际 %d 步", len(g.Moves))
	}
}
