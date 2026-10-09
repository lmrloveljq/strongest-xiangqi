package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xiangqi/analytics"
	"xiangqi/notation"
	"xiangqi/rules"
)

// findPresetEngine 定位预置的皮卡鱼引擎（项目根目录下的 engines/）；找不到则跳过测试。
func findPresetEngine(t *testing.T) string {
	t.Helper()
	presetDir := filepath.Join("..", PresetEngineDir)
	exes := ScanForExes(presetDir, nil)
	if len(exes) == 0 {
		t.Skipf("未找到预置引擎目录 %s，跳过引擎集成测试", presetDir)
	}
	for _, e := range exes {
		if strings.Contains(strings.ToLower(filepath.Base(e)), "pikafish") {
			return e
		}
	}
	return exes[0]
}

// TestParseOptionLine 校验参数行解析（含名称与默认值带空格的情况）。
func TestParseOptionLine(t *testing.T) {
	cases := []struct {
		line string
		name string
		typ  string
		def  string
		min  int
		max  int
	}{
		{"option name Threads type spin default 1 min 1 max 1024", "Threads", "spin", "1", 1, 1024},
		{"option name Hash type spin default 16 min 1 max 33554432", "Hash", "spin", "16", 1, 33554432},
		{"option name Move Overhead type spin default 10 min 0 max 5000", "Move Overhead", "spin", "10", 0, 5000},
		{"option name SyzygyPath type string default <empty>", "SyzygyPath", "string", "<empty>", 0, 0},
		{"option name UCI_Chess960 type check default false", "UCI_Chess960", "check", "false", 0, 0},
	}
	for _, c := range cases {
		o, ok := ParseOptionLine(c.line)
		if !ok {
			t.Fatalf("解析失败：%s", c.line)
		}
		if o.Name != c.name || o.Type != c.typ || o.Default != c.def || o.Min != c.min || o.Max != c.max {
			t.Errorf("解析 %q 得到 %+v，期望 name=%q type=%q default=%q min=%d max=%d",
				c.line, o, c.name, c.typ, c.def, c.min, c.max)
		}
	}
}

// TestParseInfoLine 校验 info 行解析。
func TestParseInfoLine(t *testing.T) {
	line := "info depth 18 seldepth 25 multipv 3 score cp 123 nodes 4567890 nps 1234567 time 3700 hashfull 512 pv h2e2 h9g7 c3c4"
	info, ok := ParseInfoLine(line)
	if !ok {
		t.Fatal("info 行解析失败")
	}
	if info.Depth != 18 || info.SelDepth != 25 || info.MultiPV != 3 {
		t.Errorf("depth/seldepth/multipv 解析错误：%+v", info)
	}
	if !info.Score.Valid || info.Score.CP != 123 || info.Score.Mate {
		t.Errorf("score 解析错误：%+v", info.Score)
	}
	if info.Nodes != 4567890 || info.NPS != 1234567 || info.TimeMS != 3700 {
		t.Errorf("nodes/nps/time 解析错误：%+v", info)
	}
	if len(info.PV) != 3 || info.PV[0] != "h2e2" || info.PV[2] != "c3c4" {
		t.Errorf("pv 解析错误：%v", info.PV)
	}

	mate := "info depth 5 score mate -2 pv e0e1"
	mi, ok := ParseInfoLine(mate)
	if !ok || !mi.Score.Mate || mi.Score.N != -2 {
		t.Errorf("mate 分值解析错误：%+v", mi.Score)
	}
}

// TestLimitCommand 校验 UCI / UCCI 两种协议下 go 命令的下发格式。
func TestLimitCommand(t *testing.T) {
	depth := Limit{Mode: LimitDepth, Depth: 12}
	if got := depth.Command(ProtoUCI); got != "go depth 12" {
		t.Errorf("UCI 深度命令 = %q", got)
	}
	if got := depth.Command(ProtoUCCI); got != "go depth 12" {
		t.Errorf("UCCI 深度命令 = %q", got)
	}
	mt := Limit{Mode: LimitMoveTime, MoveTimeMS: 3000}
	if got := mt.Command(ProtoUCI); got != "go movetime 3000" {
		t.Errorf("UCI 限时命令 = %q", got)
	}
	if got := mt.Command(ProtoUCCI); got != "go time 3000" {
		t.Errorf("UCCI 限时命令 = %q（UCCI 用 go time）", got)
	}
}

// TestPositionCommand 校验局面下发格式。
func TestPositionCommand(t *testing.T) {
	pos := Position{Startpos: true, FEN: rules.StartFEN, Moves: []string{"h2e2", "h9g7"}}
	if got := pos.Command(ProtoUCI); got != "position startpos moves h2e2 h9g7" {
		t.Errorf("UCI 局面命令 = %q", got)
	}
	if got := pos.Command(ProtoUCCI); !strings.HasPrefix(got, "position fen ") || !strings.HasSuffix(got, " moves h2e2 h9g7") {
		t.Errorf("UCCI 局面命令 = %q（应统一用 fen 形式）", got)
	}
}

// TestPresetEngineDetected 校验预置皮卡鱼可被探测为 UCI 引擎并能上报参数。
func TestPresetEngineDetected(t *testing.T) {
	exe := findPresetEngine(t)
	c := NewClient(exe)
	if err := c.Start(20 * time.Second); err != nil {
		t.Fatalf("启动引擎失败：%v", err)
	}
	defer c.Quit(2 * time.Second)

	if c.Protocol() != ProtoUCI {
		t.Errorf("协议 = %v，期望 UCI", c.Protocol())
	}
	if !strings.Contains(strings.ToLower(c.Name()), "pikafish") {
		t.Errorf("引擎名 = %q，期望包含 pikafish", c.Name())
	}
	opts := c.Options()
	if len(opts) == 0 {
		t.Fatal("未发现任何引擎参数")
	}
	for _, want := range []string{"Threads", "Hash", "MultiPV"} {
		if _, ok := c.FindOption(want); !ok {
			t.Errorf("引擎未上报参数 %s（已发现 %d 项）", want, len(opts))
		}
	}
	t.Logf("引擎 %s，作者 %s，共动态发现 %d 项参数", c.Name(), c.Author(), len(opts))
	for _, o := range opts {
		t.Logf("  option %-24s type=%-6s default=%s min=%d max=%d", o.Name, o.Type, o.Default, o.Min, o.Max)
	}

	// setoption + isready
	_ = c.SetOption("Threads", "8")
	_ = c.SetOption("Hash", "512")
	_ = c.SetOption("MultiPV", "8")
	if err := c.IsReady(60 * time.Second); err != nil {
		t.Fatalf("isready 失败：%v", err)
	}
}

// TestAnalyzeMultiPV8 校验 MultiPV=8 时能返回 8 条候选、bestmove 合法、概率和为 100%。
func TestAnalyzeMultiPV8(t *testing.T) {
	exe := findPresetEngine(t)
	c := NewClient(exe)
	if err := c.Start(20 * time.Second); err != nil {
		t.Fatalf("启动引擎失败：%v", err)
	}
	defer c.Quit(2 * time.Second)

	_ = c.SetOption("Threads", "16")
	_ = c.SetOption("Hash", "1024")
	_ = c.SetOption("MultiPV", "8")
	if err := c.IsReady(90 * time.Second); err != nil {
		t.Fatalf("isready 失败：%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	res, err := c.Analyze(ctx,
		Position{Startpos: true, FEN: rules.StartFEN},
		Limit{Mode: LimitMoveTime, MoveTimeMS: 3000}, nil)
	if err != nil {
		t.Fatalf("分析失败：%v", err)
	}
	if res.BestMove == "" {
		t.Fatal("未返回 bestmove")
	}
	t.Logf("bestmove=%s 深度=%d 节点=%d nps=%d 耗时=%dms 候选=%d 条",
		res.BestMove, res.Depth, res.Nodes, res.NPS, res.TimeMS, len(res.Lines))
	for _, l := range res.Lines {
		t.Logf("  multipv=%d depth=%d score=%+d pv=%s", l.MultiPV, l.Depth, l.Score.CP, l.PVString)
	}

	// bestmove 必须能被本软件的规则引擎接受
	m, ok := notation.UCIToMove(res.BestMove)
	if !ok {
		t.Fatalf("bestmove %q 不是合法坐标", res.BestMove)
	}
	g := rules.NewGame()
	if !g.IsLegal(m) {
		t.Fatalf("bestmove %s 不符合中国象棋规则", res.BestMove)
	}

	if len(res.Lines) != 8 {
		t.Errorf("MultiPV=8 时应返回 8 条候选，实际 %d 条", len(res.Lines))
	}
	// 概率（温度 softmax）和应为 100%
	values := make([]float64, 0, len(res.Lines))
	for _, l := range res.Lines {
		values = append(values, l.Score.Value())
	}
	probs := analytics.Softmax(values, 120)
	sum := 0.0
	for _, p := range probs {
		sum += p
	}
	if sum < 99 || sum > 101 {
		t.Errorf("概率和 = %.4f%%，应在 100%% ±1%% 内", sum)
	}
}

// TestWinRateLargeAdvantage 校验「红方多一車 → 红方胜率 >70%」「黑方多一車 → 红方胜率 <30%」。
func TestWinRateLargeAdvantage(t *testing.T) {
	exe := findPresetEngine(t)
	c := NewClient(exe)
	if err := c.Start(20 * time.Second); err != nil {
		t.Fatalf("启动引擎失败：%v", err)
	}
	defer c.Quit(2 * time.Second)
	_ = c.SetOption("Threads", "16")
	_ = c.SetOption("Hash", "1024")
	_ = c.SetOption("MultiPV", "1")
	if err := c.IsReady(90 * time.Second); err != nil {
		t.Fatalf("isready 失败：%v", err)
	}

	// 红方多一車：去掉黑方 a9 的車
	redUp := "1nbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"
	// 黑方多一車：去掉红方 a0 的車
	blackUp := "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/1NBAKABNR w - - 0 1"

	measure := func(fen, label string) float64 {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		res, err := c.Analyze(ctx, Position{FEN: fen}, Limit{Mode: LimitMoveTime, MoveTimeMS: 3000}, nil)
		if err != nil {
			t.Fatalf("%s 分析失败：%v", label, err)
		}
		// FEN 中以 w 表示轮到红方
		wr := analytics.RedWinRate(res.Score, 0)
		t.Logf("%s：bestmove=%s score=%+d（mate=%v）红方胜率=%.2f%%", label, res.BestMove, res.Score.CP, res.Score.Mate, wr)
		return wr
	}

	wrRed := measure(redUp, "红方多一車")
	if wrRed <= 70 {
		t.Errorf("红方多一車时红方胜率 = %.2f%%，应 > 70%%", wrRed)
	}
	wrBlack := measure(blackUp, "黑方多一車")
	if wrBlack >= 30 {
		t.Errorf("黑方多一車时红方胜率 = %.2f%%，应 < 30%%", wrBlack)
	}
}

// TestBadEngineRejected 校验非象棋引擎被明确拒绝而不是挂起或崩溃。
func TestBadEngineRejected(t *testing.T) {
	notEngine := filepath.Join(os.Getenv("SystemRoot"), "notepad.exe")
	if _, err := os.Stat(notEngine); err != nil {
		t.Skipf("找不到 %s，跳过非引擎测试", notEngine)
	}
	entry, err := ProbeExe(notEngine, 3*time.Second, "library")
	if err == nil {
		t.Fatalf("notepad.exe 不应被识别为象棋引擎，却得到 %+v", entry)
	}
	if !strings.Contains(err.Error(), "无法识别为象棋引擎") {
		t.Errorf("错误信息应包含「无法识别为象棋引擎」，实际：%v", err)
	}
	t.Logf("正确拒绝：%v", err)
}

// TestScanForExes 校验引擎库扫描能找到预置目录下的 exe。
func TestScanForExes(t *testing.T) {
	presetDir := filepath.Join("..", PresetEngineDir)
	exes := ScanForExes(presetDir, nil)
	if len(exes) == 0 {
		t.Skipf("预置目录不存在：%s", presetDir)
	}
	found := false
	for _, e := range exes {
		if strings.HasSuffix(strings.ToLower(e), ".exe") {
			found = true
		}
		t.Logf("发现候选可执行文件：%s", e)
	}
	if !found {
		t.Error("扫描结果中没有 .exe")
	}
}
