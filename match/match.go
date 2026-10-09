// Package match 实现「引擎自动对战」：两个引擎子进程串行交替走子、规则校验、
// 胜负判定、棋谱与报告生成。
//
// 并发模型：
//
//	Runner.Run 运行在一个独立 goroutine 中，通过 Events channel 向 UI 推送事件；
//	两个引擎客户端各自拥有独立的读取 goroutine，互不影响；
//	任一引擎崩溃只判该局负，不会拖垮另一个引擎，也不会让 Runner 卡死。
//
// 判负规则（与需求一致的硬性约束）：
//   - bestmove 非法（格式错误 / 不符合中国象棋规则）→ 该方立即判负；
//   - bestmove 为 none/0000 → 该方立即判负；
//   - 引擎思考超时或进程崩溃 → 该方立即判负；
//   - 无合法着法：被将军为「将死」、未被将军为「困毙」，中国象棋规则下均为走子方负；
//   - 同一局面出现 3 次 → 判和；达到着数上限 → 判和。绝不无限循环。
package match

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xiangqi/analytics"
	"xiangqi/engine"
	"xiangqi/notation"
	"xiangqi/rules"
)

// EventType 是 Runner 推送的事件类型。
type EventType int

const (
	EvMatchStart EventType = iota // 对战开始
	EvGameStart                   // 一局开始
	EvMove                        // 走了一步
	EvGameEnd                     // 一局结束
	EvMatchEnd                    // 全部结束（附报告）
	EvError                       // 出错（不致命）
	EvStatus                      // 状态文本
)

// MoveRecord 记录一步棋的完整信息。
type MoveRecord struct {
	Ply      int             `json:"ply"`
	Side     string          `json:"side"`   // "red" / "black"
	Engine   string          `json:"engine"` // 走子引擎名
	UCI      string          `json:"uci"`
	Chinese  string          `json:"chinese"`
	Score    analytics.Score `json:"score"`
	Depth    int             `json:"depth"`
	Nodes    int64           `json:"nodes"`
	NPS      int64           `json:"nps"`
	EngineMS int64           `json:"engine_ms"` // 引擎自报思考时间
	WallMS   int64           `json:"wall_ms"`   // 本软件实测耗时
	Best     bool            `json:"best"`      // 是否与引擎首选一致
}

// GameRecord 是一局棋的完整记录。
type GameRecord struct {
	Index     int
	RedName   string
	BlackName string
	RedIsSelf bool
	StartFEN  string
	TimeCtrl  string
	Date      string
	Result    rules.Status
	Reason    string
	Moves     []MoveRecord
	PGNPath   string
	EndedAt   time.Time
}

// ResultText 返回 PGN 结果串。
func (g *GameRecord) ResultText() string { return g.Result.PGNResult() }

// ResultCN 返回中文结果。
func (g *GameRecord) ResultCN() string {
	switch g.Result {
	case rules.RedWin:
		if g.RedIsSelf {
			return "己方胜"
		}
		return "对手胜"
	case rules.BlackWin:
		if g.RedIsSelf {
			return "对手胜"
		}
		return "己方胜"
	default:
		return "和棋"
	}
}

// Options 是对战设置。
type Options struct {
	Games           int          // 对局数
	LimitSelf       engine.Limit // 己方引擎思考限制
	LimitOpp        engine.Limit // 对手引擎思考限制
	SelfThreads     int
	SelfHash        int
	OppThreads      int
	OppHash         int
	AlternateColors bool
	OutputDir       string // 棋谱与报告的输出目录
	MoveTimeout     time.Duration
}

// SideStats 是按先后手分列的战绩。
type SideStats struct {
	Games  int
	Wins   int
	Draws  int
	Losses int
}

// Report 是对战报告。
type Report struct {
	Total       int
	SelfWins    int
	Draws       int
	SelfLosses  int
	SelfWinRate float64 // 胜率（胜 + 0.5*和）/ 总局数
	AsRed       SideStats
	AsBlack     SideStats

	AvgDepthSelf float64
	AvgDepthOpp  float64
	AvgNPSSelf   float64
	AvgNPSOpp    float64
	AvgNodesSelf float64
	AvgNodesOpp  float64

	SelfName string
	OppName  string
	LimitCN  string
	Elapsed  time.Duration
	Games    []*GameRecord
	FilePath string
	Dir      string
}

// Event 是 Runner 推送给界面的事件。
type Event struct {
	Type       EventType
	GameIndex  int
	TotalGames int
	SelfWins   int
	Draws      int
	SelfLosses int
	Board      *rules.Board // 当前局面快照（EvMove / EvGameStart）
	Move       MoveRecord
	Game       *GameRecord
	Report     *Report
	Message    string
	Err        error
}

// Runner 驱动整场自动对战。
type Runner struct {
	// swapped：用户点过「交换双方引擎」之后为真（见 SwapSides）
	swapped atomic.Bool
	mu      sync.Mutex
	opts    Options
	self    engine.EngineEntry
	opp     engine.EngineEntry
	events  chan Event
	pauseCh chan struct{} // 暂停闸门：关闭 = 暂停中
	stopCh  chan struct{}
	stopped bool
	paused  bool
	clients [2]*engine.Client // 0=己方 1=对手
	score   [3]int            // 己方胜/和/负
	records []*GameRecord
	startAt time.Time
	onDone  func(*Report)
}

// New 创建 Runner。
func New(self, opp engine.EngineEntry, opts Options) *Runner {
	if opts.Games < 1 {
		opts.Games = 1
	}
	if opts.MoveTimeout <= 0 {
		opts.MoveTimeout = 10 * time.Minute
	}
	return &Runner{
		opts:    opts,
		self:    self,
		opp:     opp,
		events:  make(chan Event, 256),
		pauseCh: make(chan struct{}),
		stopCh:  make(chan struct{}),
	}
}

// Events 返回事件通道（容量 256，消费端必须在独立 goroutine 中持续读取）。
func (r *Runner) Events() <-chan Event { return r.events }

// UpdateOptions 在对战进行中更新设置（对局数、时间控制、线程、哈希在**下一局**生效）。
func (r *Runner) UpdateOptions(fn func(*Options)) {
	r.mu.Lock()
	fn(&r.opts)
	r.mu.Unlock()
}

// Pause 暂停（当前步走完后停在原地）。
func (r *Runner) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.paused {
		r.paused = true
		close(r.pauseCh)
	}
}

// Resume 继续。
func (r *Runner) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.paused {
		r.paused = false
		r.pauseCh = make(chan struct{})
	}
}

// Paused 返回是否暂停中。
func (r *Runner) Paused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}

// Stop 终止对战（已完成的对局与报告会保留）。
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.stopped {
		r.stopped = true
		close(r.stopCh)
	}
}

// Stopped 返回是否已请求终止。
func (r *Runner) Stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}

// waitGate 在暂停闸门处等待；返回 false 表示已被要求终止。
func (r *Runner) waitGate(ctx context.Context) bool {
	for {
		r.mu.Lock()
		paused, stopped := r.paused, r.stopped
		gate := r.pauseCh
		r.mu.Unlock()
		if stopped {
			return false
		}
		if !paused {
			return true
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return false
		}
	}
}

func (r *Runner) emit(ev Event) {
	select {
	case r.events <- ev:
	case <-time.After(3 * time.Second):
		// 消费端异常时不让 Runner 永久阻塞
	}
}

// Run 执行整场对战。必须在独立 goroutine 中调用；结束时关闭 Events。
func (r *Runner) Run(ctx context.Context) {
	defer close(r.events)
	r.startAt = time.Now()

	r.mu.Lock()
	opts := r.opts
	r.mu.Unlock()

	// 启动两个引擎
	name0, err0 := r.startSide(ctx, 0, r.self, opts.SelfThreads, opts.SelfHash)
	if err0 != nil {
		r.emit(Event{Type: EvError, Err: fmt.Errorf("己方引擎启动失败：%w", err0), Message: "己方引擎启动失败"})
		r.emit(Event{Type: EvMatchEnd, Report: r.buildReport(opts, time.Since(r.startAt))})
		return
	}
	name1, err1 := r.startSide(ctx, 1, r.opp, opts.OppThreads, opts.OppHash)
	if err1 != nil {
		r.emit(Event{Type: EvError, Err: fmt.Errorf("对手引擎启动失败：%w", err1), Message: "对手引擎启动失败"})
		r.emit(Event{Type: EvMatchEnd, Report: r.buildReport(opts, time.Since(r.startAt))})
		r.closeClients()
		return
	}
	defer r.closeClients()

	r.emit(Event{
		Type:       EvMatchStart,
		TotalGames: opts.Games,
		Message:    fmt.Sprintf("对战开始：%s  vs  %s，共 %d 局", name0, name1, opts.Games),
	})

	for gi := 1; gi <= opts.Games; gi++ {
		if !r.waitGate(ctx) {
			break
		}
		r.mu.Lock()
		opts = r.opts // 每局重新读取设置，实现「改完下局生效」
		r.mu.Unlock()

		selfIsRed := true
		if opts.AlternateColors {
			selfIsRed = gi%2 == 1 // 奇数局己方执红，偶数局执黑（先后手轮换）
		}
		rec := r.playGame(ctx, gi, opts, selfIsRed)
		r.savePGN(opts.OutputDir, rec)
		r.records = append(r.records, rec)

		// 【缺陷修复】胜负归属必须用 rec.RedIsSelf，而不是本局的基准指派 selfIsRed。
		// 用户在局中点过「翻转（交换双方引擎）」时，playGame 已经按**结束时**的实际
		// 执子方修正过 rec.RedIsSelf；这里若仍用 selfIsRed，胜负会记到错误的引擎头上。
		recSelfIsRed := rec.RedIsSelf
		switch rec.Result {
		case rules.Draw:
			r.score[1]++
		case rules.RedWin:
			if recSelfIsRed {
				r.score[0]++
			} else {
				r.score[2]++
			}
		case rules.BlackWin:
			if recSelfIsRed {
				r.score[2]++
			} else {
				r.score[0]++
			}
		}

		r.emit(Event{
			Type:       EvGameEnd,
			GameIndex:  gi,
			TotalGames: opts.Games,
			SelfWins:   r.score[0],
			Draws:      r.score[1],
			SelfLosses: r.score[2],
			Game:       rec,
			Board:      lastBoard(rec),
			Message: fmt.Sprintf("第 %d/%d 局结束：%s（%s），比分 %d-%d-%d",
				gi, opts.Games, rec.ResultCN(), rec.Reason, r.score[0], r.score[1], r.score[2]),
		})
	}

	rep := r.buildReport(opts, time.Since(r.startAt))
	if rep.Dir != "" && len(rep.Games) > 0 {
		if p, err := WriteReport(rep.Dir, rep); err == nil {
			rep.FilePath = p
		} else {
			r.emit(Event{Type: EvError, Err: err, Message: "写入报告失败：" + err.Error()})
		}
	}
	r.emit(Event{Type: EvMatchEnd, Report: rep, Message: "对战结束"})
}

// startSide 启动一侧引擎并下发基础参数。
// clientAt 取出某一侧的引擎客户端（可能为 nil）。
func (r *Runner) clientAt(idx int) *engine.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	if idx < 0 || idx > 1 {
		return nil
	}
	return r.clients[idx]
}

// restartSide 重启某一侧的引擎进程（用于异常自动降级）。
func (r *Runner) restartSide(ctx context.Context, idx int) (string, error) {
	r.mu.Lock()
	if idx < 0 || idx > 1 {
		r.mu.Unlock()
		return "", fmt.Errorf("引擎下标越界")
	}
	old := r.clients[idx]
	entry := r.self
	threads, hash := r.opts.SelfThreads, r.opts.SelfHash
	if idx == 1 {
		entry = r.opp
		threads, hash = r.opts.OppThreads, r.opts.OppHash
	}
	r.clients[idx] = nil
	r.mu.Unlock()
	if old != nil {
		old.Kill()
	}
	return r.startSide(ctx, idx, entry, threads, hash)
}

func (r *Runner) startSide(ctx context.Context, idx int, entry engine.EngineEntry, threads, hash int) (string, error) {
	c := engine.NewClient(entry.Path)
	if err := c.Start(8 * time.Second); err != nil {
		return entry.Name, err
	}
	// 基础参数：线程 / 哈希（用户可在参数面板调整，下一局重新下发）
	if _, ok := c.FindOption("Threads"); ok && threads > 0 {
		_ = c.SetOption("Threads", fmt.Sprint(threads))
	}
	if _, ok := c.FindOption("Hash"); ok && hash > 0 {
		_ = c.SetOption("Hash", fmt.Sprint(hash))
	}
	if err := c.IsReady(10 * time.Second); err != nil {
		c.Kill()
		return entry.Name, err
	}
	r.mu.Lock()
	r.clients[idx] = c
	r.mu.Unlock()
	return c.Name(), nil
}

func (r *Runner) closeClients() {
	r.mu.Lock()
	cs := r.clients
	r.clients = [2]*engine.Client{}
	r.mu.Unlock()
	for _, c := range cs {
		if c != nil {
			c.Quit(2 * time.Second)
		}
	}
}

// limitFor 把时间控制换算成某一侧的每步限制。
//
// 「每局总时间」按每方约 60 步均摊，下限 200ms，保证不会出现 0 时限导致引擎立即弃权。
func limitFor(base engine.Limit) engine.Limit {
	if base.Mode == engine.LimitGameTime {
		per := base.GameTimeMS / 60
		if per < 200 {
			per = 200
		}
		return engine.Limit{Mode: engine.LimitMoveTime, MoveTimeMS: per}
	}
	return base
}

// playGame 走完一局，返回完整记录。
func (r *Runner) playGameInner(ctx context.Context, idx int, opts Options, selfIsRed bool) *GameRecord {
	game := rules.NewGame()
	rec := &GameRecord{
		Index:     idx,
		StartFEN:  game.Board.FEN(),
		RedIsSelf: selfIsRed,
		Date:      time.Now().Format("2006.01.02"),
		Moves:     []MoveRecord{},
	}
	r.mu.Lock()
	selfName, oppName := r.self.Name, r.opp.Name
	cs := r.clients
	r.mu.Unlock()
	if c := cs[0]; c != nil && c.Alive() {
		selfName = c.Name()
	}
	if c := cs[1]; c != nil && c.Alive() {
		oppName = c.Name()
	}
	rec.RedName, rec.BlackName = selfName, oppName
	if !selfIsRed {
		rec.RedName, rec.BlackName = oppName, selfName
	}
	rec.TimeCtrl = opts.LimitSelf.Label() + " / " + opts.LimitOpp.Label()

	r.emit(Event{
		Type:       EvGameStart,
		GameIndex:  idx,
		TotalGames: opts.Games,
		Board:      game.Board.Clone(),
		Message:    fmt.Sprintf("第 %d 局开始：红 %s  vs  黑 %s", idx, rec.RedName, rec.BlackName),
	})

	// 通知引擎开新局
	r.mu.Lock()
	cs = r.clients
	r.mu.Unlock()
	for _, c := range cs {
		if c != nil {
			_ = c.NewGame()
		}
	}

	for {
		if !r.waitGate(ctx) {
			rec.Result = rules.Draw
			rec.Reason = "用户终止对战"
			return rec
		}
		if st, reason := game.Adjudicate(); st != rules.Playing {
			rec.Result, rec.Reason = st, reason
			return rec
		}

		sideToMove := game.Board.Side
		// 【局面对调·电脑对电脑】selfIsRed 是本局的基准指派；用户在窗口里点过
		// 「交换双方引擎」之后，swapped 为真，就让两边引擎中途换边——
		// 局面与走子次序都不动，只是下一着换另一台引擎来应招。
		selfIsRedNow := selfIsRed != r.swapped.Load()
		isSelfTurn := (sideToMove == rules.Red) == selfIsRedNow
		ci := 1
		var limit engine.Limit
		var engName string
		if isSelfTurn {
			ci = 0
			limit = limitFor(opts.LimitSelf)
			if selfIsRed {
				engName = rec.RedName
			} else {
				engName = rec.BlackName
			}
		} else {
			limit = limitFor(opts.LimitOpp)
			if selfIsRed {
				engName = rec.BlackName
			} else {
				engName = rec.RedName
			}
		}

		r.mu.Lock()
		client := r.clients[ci]
		r.mu.Unlock()
		if client == nil || !client.Alive() {
			rec.Result = loseFor(sideToMove)
			rec.Reason = fmt.Sprintf("%s 引擎进程不可用（崩溃或已退出）", engName)
			return rec
		}

		pos := engine.Position{Startpos: game.StartFEN == rules.StartFEN, FEN: game.StartFEN, Moves: uciList(game)}
		wall := time.Now()
		res, err := client.Analyze(ctx, pos, limit, nil)
		wallMS := time.Since(wall).Milliseconds()

		if err != nil {
			if ctx.Err() != nil || r.Stopped() {
				rec.Result = rules.Draw
				rec.Reason = "用户终止对战"
				return rec
			}
			// 【v1.6.8 / 头脑风暴第 14 条】引擎异常**自动降级**：
			// 以前一遇到异常就直接判这一方负；实际上很多异常是临时的
			//（进程卡住、管道半死、引擎自己重启），重来一次往往就好了。
			// 这里三级处理：① 原地重试一次 → ② 重启该引擎进程再试一次 →
			// ③ 仍然失败才判负，并把「已自动重试」写进判负原因，对战继续下一局。
			r.emit(Event{Type: EvStatus, Message: fmt.Sprintf("%s 引擎异常，正在自动重试：%v", engName, err)})
			time.Sleep(400 * time.Millisecond)
			res, err = client.Analyze(ctx, pos, limit, nil)
			if err != nil && ctx.Err() == nil && !r.Stopped() {
				r.emit(Event{Type: EvStatus, Message: fmt.Sprintf("%s 引擎重试仍失败，正在重启引擎进程", engName)})
				if name2, nerr := r.restartSide(ctx, ci); nerr == nil {
					engName = name2
					if c2 := r.clientAt(ci); c2 != nil {
						res, err = c2.Analyze(ctx, pos, limit, nil)
					}
				}
			}
			wallMS = time.Since(wall).Milliseconds()
			if err != nil {
				if ctx.Err() != nil || r.Stopped() {
					rec.Result = rules.Draw
					rec.Reason = "用户终止对战"
					return rec
				}
				rec.Result = loseFor(sideToMove)
				rec.Reason = fmt.Sprintf("%s 引擎异常（已自动重试并重启进程）：%v", engName, err)
				return rec
			}
		}

		bm := strings.TrimSpace(res.BestMove)
		if bm == "" || bm == "none" || bm == "(none)" || bm == "0000" {
			rec.Result = loseFor(sideToMove)
			rec.Reason = fmt.Sprintf("%s 未返回有效着法（bestmove=%q）", engName, res.BestMove)
			return rec
		}
		mv, ok := notation.UCIToMove(bm)
		if !ok {
			rec.Result = loseFor(sideToMove)
			rec.Reason = fmt.Sprintf("%s 返回非法着法坐标 %q", engName, bm)
			return rec
		}
		boardBefore := game.Board.Clone()
		if !game.IsLegal(mv) {
			rec.Result = loseFor(sideToMove)
			rec.Reason = fmt.Sprintf("%s 返回不合法着法 %s", engName, mv)
			return rec
		}

		sideName := "red"
		if sideToMove == rules.Black {
			sideName = "black"
		}
		mr := MoveRecord{
			Ply:      len(game.Moves) + 1,
			Side:     sideName,
			Engine:   engName,
			UCI:      mv.String(),
			Chinese:  notation.ToChinese(boardBefore, mv),
			Score:    res.Score,
			Depth:    res.Depth,
			Nodes:    res.Nodes,
			NPS:      res.NPS,
			EngineMS: res.TimeMS,
			WallMS:   wallMS,
			Best:     true,
		}
		_ = game.TryMove(mv)
		rec.Moves = append(rec.Moves, mr)

		r.emit(Event{
			Type:       EvMove,
			GameIndex:  idx,
			TotalGames: opts.Games,
			SelfWins:   r.score[0],
			Draws:      r.score[1],
			SelfLosses: r.score[2],
			Board:      game.Board.Clone(),
			Move:       mr,
		})
	}
}

// loseFor 返回「走子方判负」对应的终局状态。
func loseFor(sideToMove int) rules.Status {
	if sideToMove == rules.Red {
		return rules.BlackWin
	}
	return rules.RedWin
}

func uciList(g *rules.Game) []string {
	out := make([]string, 0, len(g.Moves))
	for _, m := range g.Moves {
		out = append(out, m.String())
	}
	return out
}

func lastBoard(rec *GameRecord) *rules.Board {
	g, err := rules.NewGameFromFEN(rec.StartFEN)
	if err != nil {
		return rules.NewStartBoard()
	}
	for _, mr := range rec.Moves {
		if m, ok := notation.UCIToMove(mr.UCI); ok {
			g.ForceMove(m)
		}
	}
	return g.Board
}

// OutputPath 返回某局的 PGN 文件路径。
func OutputPath(dir string, idx int) string {
	return filepath.Join(dir, fmt.Sprintf("game_%03d.pgn", idx))
}

// swapped 记录用户是否点过「交换双方引擎」。
//
// 语义与「局面对调」一致：只交换「哪台引擎执哪一方」，
// 局面、着法历史、轮到谁走都不变。
func (r *Runner) SwapSides() {
	r.swapped.Store(!r.swapped.Load())
}

// SwapSides 由界面调用：让两台引擎中途换边。
func (r *Runner) swapNow() bool { return r.swapped.Load() }

// playGame 包一层：跑完一局后，按**结束时**的实际执子方修正记录，
// 免得中途换边之后胜负被算到错误的引擎头上。
func (r *Runner) playGame(ctx context.Context, idx int, opts Options, selfIsRed bool) *GameRecord {
	rec := r.playGameInner(ctx, idx, opts, selfIsRed)
	if r.swapped.Load() {
		rec.RedIsSelf = !selfIsRed
		rec.RedName, rec.BlackName = rec.BlackName, rec.RedName
		rec.TimeCtrl += "（本局中途交换过双方）"
	}
	return rec
}
