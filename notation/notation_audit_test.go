package notation

import (
	"testing"

	"xiangqi/rules"
)

// 本文件用「已知答案表」钉死中文记谱转换。
//
// 为什么重要：用户反馈过「软件弹出来的走法感觉输出错了」。
// 规则层已经用 perft(1..4) = 44/1920/79666/3290240 证明正确，
// 所以怀疑只能落在记谱层。下面每一行都是**标准棋谱里能查到的写法**，
// 中文写错一个纵线号或者进/退方向，这里立刻红。

type moveCase struct {
	fen  string
	uci  string
	want string
	why  string
}

func TestToChineseKnownTable(t *testing.T) {
	cases := []moveCase{
		// ---- 开局八步（中炮对屏风马），每一步的中文都是标准写法 ----
		{rules.StartFEN, "h2e2", "炮二平五", "红方纵线号 = 9-文件号，h=7 → 二；平到 e → 五"},
		{rules.StartFEN, "b2e2", "炮八平五", "b=1 → 八"},
		{rules.StartFEN, "b0c2", "馬八进七", "红方用繁体馬；马走斜线用**目标**纵线号：c=2 → 七"},
		{rules.StartFEN, "h0g2", "馬二进三", "红方用繁体馬；h=7 → 二，目标 g=6 → 三"},
		{rules.StartFEN, "a0b0", "車九平八", "红方用繁体車；a=0 → 九，目标 b=1 → 八"},
		{rules.StartFEN, "c3c4", "兵七进一", "兵走直线用**步数**：进一"},
		{rules.StartFEN, "g3g4", "兵三进一", "g=6 → 三"},
		{rules.StartFEN, "e0e1", "帅五进一", "帅也走步数：进一"},
		// ---- 黑方：纵线号 = 文件号+1，前进方向是行号减小 ----
		{rules.StartFEN, "h9g7", "马8进7", "黑 h=7 → 8，目标 g=6 → 7"},
		{rules.StartFEN, "b9c7", "马2进3", "黑 b=1 → 2，目标 c=2 → 3"},
		{rules.StartFEN, "a9b9", "车1平2", "黑 a=0 → 1，目标 b=1 → 2"},
		{rules.StartFEN, "i9h9", "车9平8", "黑 i=8 → 9，目标 h=7 → 8"},
		{rules.StartFEN, "e9e8", "将5进1", "黑将在 e=4 → 5；黑方行号减小才是「进」"},
		{rules.StartFEN, "c6c5", "卒3进1", "黑卒纵线号 = 文件号+1，c=2 → 3；过河前只能前进"},
		// ---- 斜行棋子（马/象/士）用目标纵线号，直行棋子用步数 ----
		{rules.StartFEN, "c0e2", "相七进五", "相：c=2 → 七，目标 e=4 → 五"},
		{rules.StartFEN, "g0e2", "相三进五", "相：g=6 → 三，目标 e=4 → 五"},
		{rules.StartFEN, "d0e1", "仕六进五", "仕：d=3 → 六，目标 e=4 → 五"},
		{rules.StartFEN, "f0e1", "仕四进五", "仕：f=5 → 四，目标 e=4 → 五"},
	}
	for _, c := range cases {
		b, err := rules.ParseFEN(c.fen)
		if err != nil {
			t.Fatalf("FEN 解析失败 %q: %v", c.fen, err)
		}
		m, ok := UCIToMove(c.uci)
		if !ok {
			t.Fatalf("%s 不是合法 UCI 坐标", c.uci)
		}
		got := ToChinese(b, m)
		if got != c.want {
			t.Errorf("%s → 得到 %q，期望 %q（%s）", c.uci, got, c.want, c.why)
		}
	}
}

// TestToChineseFrontBack 钉死「前/后」次序。
//
// 中国象棋规则：同一纵线上同兵种多于一枚时，用「前 / 后」区分，
// **前 = 更靠近对方底线的那一枚**——红方是行号更大的，黑方是行号更小的。
func TestToChineseFrontBack(t *testing.T) {
	// 红方两车同在 a 线：a0（后）与 a5（前）
	redFEN := "3k5/9/9/9/R8/9/9/9/9/R3K4 w - - 0 1"
	// 黑方两车同在 a 线：a9（后）与 a4（前）
	blackFEN := "r3k4/9/9/9/9/r8/9/9/9/3K5 b - - 0 1"

	cases := []struct {
		fen, uci, want, why string
	}{
		{redFEN, "a5a6", "前車进一", "红方 a5 更靠近黑方底线 → 前（红方繁体車）"},
		{redFEN, "a5b5", "前車平八", "前车横走：a=0 → 九，目标 b=1 → 八"},
		{redFEN, "a0a1", "后車进一", "红方 a0 离对方远 → 后"},
		{blackFEN, "a4a3", "前车进1", "黑方 a4（行号更小）更靠近红方底线 → 前"},
		{blackFEN, "a9a8", "后车进1", "黑方 a9 离对方远 → 后"},
	}
	for _, c := range cases {
		b, err := rules.ParseFEN(c.fen)
		if err != nil {
			t.Fatalf("FEN 解析失败 %q: %v", c.fen, err)
		}
		m, _ := UCIToMove(c.uci)
		if got := ToChinese(b, m); got != c.want {
			t.Errorf("[%s] %s → %q，期望 %q", c.fen, c.uci, got, c.want)
		}
	}
}

// TestToChineseIsLegal 记谱表里出现的每一个着法都必须是**该局面的合法着法**。
//
// 这条防的是「引擎给的着法被照显」的问题：只要引擎吐出一个不在合法着法集合里的
// 坐标，中文记谱就会拿到一个空格子/错棋子。这里把整局走一遍，
// 每个着法都先验合法性再转中文。
func TestMoveListRoundTripStaysLegal(t *testing.T) {
	// 一局真实的开局到中局（红黑交替）
	seq := []string{
		"h2e2", "h9g7", "c3c4", "i9h9", "b0c2", "b9c7",
		"a0b0", "a9b9", "h0g2", "h7h3", "i0h0", "b7b3",
	}
	g := rules.NewGame()
	for i, u := range seq {
		m, ok := UCIToMove(u)
		if !ok {
			t.Fatalf("第 %d 步 %s 不是合法 UCI", i+1, u)
		}
		b := g.BoardAt(i) // 走这一步之前的局面
		if !g.IsLegal(m) {
			t.Fatalf("第 %d 步 %s（%s）不是合法着法", i+1, u, ToChinese(b, m))
		}
		cn := ToChinese(b, m)
		if cn == "" || cn == u {
			t.Fatalf("第 %d 步 %s 没转出中文记谱（得到 %q）", i+1, u, cn)
		}
		// 中文记谱必须是纯中文 + 数字，不能混进坐标字符
		for _, r := range cn {
			if r >= 'a' && r <= 'z' {
				t.Fatalf("第 %d 步 %s 的中文记谱 %q 混进了英文字母", i+1, u, cn)
			}
		}
		if err := g.TryMove(m); err != nil {
			t.Fatalf("第 %d 步 %s 执行失败：%v", i+1, u, err)
		}
	}
	// 走完 12 步后局面必须与 FEN 一致可解析
	if _, err := rules.ParseFEN(g.Board.FEN()); err != nil {
		t.Fatalf("走完 12 步后的 FEN 无法解析：%v", err)
	}
}

// TestUCIToMoveRejectsBad 非法坐标必须被拒（防止把垃圾串当坐标转出乱七八糟的中文）。
func TestUCIToMoveRejectsBad(t *testing.T) {
	bad := []string{"", "h2e", "h2e22", "j2e2", "h2e2x", "0e2", "h2e-"}
	// 只查「格式错误 / 文件号越界」；e9、i9 这些都在盘内，是合法坐标不该拒
	for _, s := range bad {
		if _, ok := UCIToMove(s); ok {
			t.Errorf("%q 不该被当成合法坐标", s)
		}
	}
}
