package ui

import (
	"strings"
	"testing"

	"xiangqi/config"
	"xiangqi/engine"
	"xiangqi/notation"
	"xiangqi/rules"
)

// 本次修复的回归测试。
//
// 为什么这几条要写成测试：它们对应的都是**用户实测踩到**的缺陷，而且都不是
// "写错了语法"那种一眼能看出的错——是"看起来合理、实际锁死盘面/参数没生效"的错。
// 注释拦不住下一个改这段代码的人，测试能。

// TestInfiniteAnalysisDegradesInHumanMode 人机对弈里无限分析必须降级成每步限时。
//
// 缺陷原型：无限分析 = 引擎不收到 stop 就不回 bestmove。人机对弈里轮到电脑时，
// 它永远算不完 → 永远不交招 → 永远轮不到人 → 棋盘一直锁着（用户原话"棋盘根本动不了"）。
func TestInfiniteAnalysisDegradesInHumanMode(t *testing.T) {
	newApp := func(mode string) *App {
		c := config.Default()
		c.TimeMode = config.TimeModeInfinite
		c.MoveTimeMS = 1500
		return &App{cfg: c, curMode: mode, viewing: -1, game: rules.NewGame()}
	}

	// 人机对弈：必须收敛成一个有限的每步限时，否则电脑交不出招
	lim := newApp("human").currentLimit()
	if lim.Infinite {
		t.Fatal("人机对弈里仍下发 go infinite：电脑永远不会交招，棋盘会锁死")
	}
	if lim.Mode != engine.LimitMoveTime || lim.MoveTimeMS != 1500 {
		t.Fatalf("人机对弈的降级结果 = %+v，期望每步限时 1500ms", lim)
	}

	// 每步限时太小时兜到 1 秒：总得给引擎一点思考时间
	slow := newApp("human")
	slow.cfg.MoveTimeMS = 100
	if got := slow.currentLimit().MoveTimeMS; got != 1000 {
		t.Fatalf("每步限时被兜底成 %dms，期望最小 1000ms", got)
	}

	// 分析模式：仍然是真无限（用户点「停止」才结束）
	if lim := newApp("bridge").currentLimit(); !lim.Infinite {
		t.Fatal("分析模式里的无限分析被改掉了：用户要的就是算到按停止为止")
	}
}

// TestBoardBlockedReasonAlwaysExplains 棋盘点不动时必须给得出理由。
//
// 缺陷原型：对战模式 / 查看历史局面时，棋盘不接收点击，但**一声不吭**——
// 用户看到的结论就是"棋盘坏了 / 动不了"。
func TestBoardBlockedReasonAlwaysExplains(t *testing.T) {
	cases := []struct {
		name string
		app  *App
		want string
	}{
		{"引擎对战只用于展示", &App{curMode: "match", viewing: -1, game: rules.NewGame()}, "对战模式"},
		{"查看历史局面", &App{curMode: "human", viewing: 3, game: rules.NewGame()}, "历史局面"},
		{"轮到电脑走棋", &App{curMode: "human", viewing: -1, game: rules.NewGame()}, "轮到电脑"},
	}
	for _, c := range cases {
		got := c.app.boardBlockedReason()
		if got == "" {
			t.Fatalf("%s：理由为空，用户只会看到「点不动」", c.name)
		}
		if !strings.Contains(got, c.want) {
			t.Fatalf("%s：理由是 %q，应包含 %q", c.name, got, c.want)
		}
	}
}

// TestHumanTurnGateMatchesBoardInput 人机对弈的「能不能点」必须与「轮到谁」一致。
//
// 缺陷原型：轮到电脑时不锁盘 → 玩家和引擎抢着走；轮到人时误锁 → 玩家走不了
// （后者正是"棋盘根本动不了"的另一种形态）。
func TestHumanTurnGateMatchesBoardInput(t *testing.T) {
	c := config.Default()
	c.HumanSide = config.SideRed
	a := &App{cfg: c, curMode: "human", viewing: -1, game: rules.NewGame()}

	if !a.isHumanTurn() || !a.canBoardAcceptInput() {
		t.Fatal("开局红方走、人执红：应当是人类的回合且棋盘可点")
	}

	m, ok := notation.UCIToMove("h2e2")
	if !ok {
		t.Fatal("测试用着法 h2e2 解析失败")
	}
	if err := a.game.TryMove(m); err != nil {
		t.Fatalf("测试用着法 h2e2 走不了：%v", err)
	}
	if a.isHumanTurn() {
		t.Fatal("红方走完一步后应当轮到黑方（电脑）")
	}
	if a.canBoardAcceptInput() {
		t.Fatal("轮到电脑时棋盘必须锁住，否则玩家会和引擎抢着走")
	}
}
