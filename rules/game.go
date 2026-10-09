package rules

import "fmt"

// Status 表示一局棋的判定结果。
type Status int

const (
	Playing  Status = iota // 对局进行中
	RedWin                 // 红方胜
	BlackWin               // 黑方胜
	Draw                   // 和棋
)

// String 返回中文描述。
func (s Status) String() string {
	switch s {
	case RedWin:
		return "红方胜"
	case BlackWin:
		return "黑方胜"
	case Draw:
		return "和棋"
	default:
		return "进行中"
	}
}

// PGNResult 返回 PGN 结果字符串。
func (s Status) PGNResult() string {
	switch s {
	case RedWin:
		return "1-0"
	case BlackWin:
		return "0-1"
	case Draw:
		return "1/2-1/2"
	default:
		return "*"
	}
}

// Game 在 Board 之上维护一局棋的完整历史，供界面悔棋、曲线记录与重复局面判定使用。
type Game struct {
	Board    *Board
	StartFEN string
	Moves    []Move   // 已走着的着法
	Captured []Piece  // Captured[i] 是 Moves[i] 吃掉的子
	Keys     []string // Keys[i] 是走第 i 步「之前」的局面键；Keys[len(Moves)] 是当前局面键
}

// NewGame 新建一局，从标准初始局面开始。
func NewGame() *Game {
	b := NewStartBoard()
	return &Game{Board: b, StartFEN: StartFEN, Keys: []string{b.Key()}}
}

// NewGameFromFEN 从指定局面新建一局（用于手动摆盘后的对弈/分析）。
func NewGameFromFEN(fen string) (*Game, error) {
	b, err := ParseFEN(fen)
	if err != nil {
		return nil, err
	}
	return &Game{Board: b, StartFEN: b.FEN(), Keys: []string{b.Key()}}, nil
}

// LegalMoves 返回当前局面的完全合法着法。
func (g *Game) LegalMoves() []Move { return g.Board.LegalMoves() }

// LegalTargets 返回 from 格上的棋子所有合法落点（用于点击走子高亮）。
func (g *Game) LegalTargets(from int) []int {
	if from < 0 || from >= Squares {
		return nil
	}
	var out []int
	for _, m := range g.LegalMoves() {
		if m.From == from {
			out = append(out, m.To)
		}
	}
	return out
}

// IsLegal 判断着法是否完全合法。
func (g *Game) IsLegal(m Move) bool {
	for _, lm := range g.LegalMoves() {
		if lm == m {
			return true
		}
	}
	return false
}

// TryMove 校验并执行着法，非法则返回错误且不改变局面。
func (g *Game) TryMove(m Move) error {
	if m.From < 0 || m.From >= Squares || m.To < 0 || m.To >= Squares {
		return fmt.Errorf("着法坐标越界: %s", m)
	}
	if !g.IsLegal(m) {
		p := g.Board.Sq[m.From]
		if p.IsEmpty() {
			return fmt.Errorf("起点 %s 没有棋子", SquareName(m.From))
		}
		if p.Side() != g.Board.Side {
			return fmt.Errorf("该走%s方，但 %s 上是%s方棋子", sideName(g.Board.Side), SquareName(m.From), sideName(p.Side()))
		}
		return fmt.Errorf("非法着法 %s（该着法会导致被将军或将帅照面）", m)
	}
	captured := g.Board.Apply(m)
	g.Moves = append(g.Moves, m)
	g.Captured = append(g.Captured, captured)
	g.Keys = append(g.Keys, g.Board.Key())
	return nil
}

// ForceMove 不做合法性校验直接执行着法（仅供回放外部着法序列的正常路径使用，
// 调用方应先自行用 IsLegal 校验）。
func (g *Game) ForceMove(m Move) {
	captured := g.Board.Apply(m)
	g.Moves = append(g.Moves, m)
	g.Captured = append(g.Captured, captured)
	g.Keys = append(g.Keys, g.Board.Key())
}

// Undo 撤销一步，返回是否成功。
func (g *Game) Undo() bool {
	n := len(g.Moves)
	if n == 0 {
		return false
	}
	m := g.Moves[n-1]
	cap := g.Captured[n-1]
	g.Board.Revert(m, cap)
	g.Moves = g.Moves[:n-1]
	g.Captured = g.Captured[:n-1]
	g.Keys = g.Keys[:n]
	return true
}

// UndoN 撤销最后 n 步。
func (g *Game) UndoN(n int) {
	for i := 0; i < n; i++ {
		if !g.Undo() {
			return
		}
	}
}

// Reset 回到开始局面。
func (g *Game) Reset() {
	b, err := ParseFEN(g.StartFEN)
	if err != nil {
		b = NewStartBoard()
	}
	g.Board = b
	g.Moves = nil
	g.Captured = nil
	g.Keys = []string{b.Key()}
}

// RepetitionCount 返回「当前局面键」在历史中出现的次数。
// 同一局面第 3 次出现即判和（对应长将/长捉循环，避免自动对战死循环）。
func (g *Game) RepetitionCount() int {
	if len(g.Keys) == 0 {
		return 0
	}
	cur := g.Keys[len(g.Keys)-1]
	n := 0
	for _, k := range g.Keys {
		if k == cur {
			n++
		}
	}
	return n
}

// MaxPlies 是自动对战的硬性着数上限；超过即判和，保证永不无限循环。
const MaxPlies = 400

// Adjudicate 判定当前局面结果。返回 Playing 表示棋局继续。
//
// 判定顺序：
//  1. 无合法着法：被将军 → 将死（走子方负）；未被将军 → 困毙（中国象棋规则同样判走子方负）
//  2. 同一局面出现 3 次 → 判和
//  3. 着数达到 MaxPlies → 判和
func (g *Game) Adjudicate() (Status, string) {
	if len(g.LegalMoves()) == 0 {
		loser := g.Board.Side
		if g.Board.InCheck(loser) {
			if loser == Red {
				return BlackWin, "将死"
			}
			return RedWin, "将死"
		}
		// 困毙：中国象棋规则中无子可动同样判负
		if loser == Red {
			return BlackWin, "困毙"
		}
		return RedWin, "困毙"
	}
	if g.RepetitionCount() >= 3 {
		return Draw, "三次重复局面判和"
	}
	if len(g.Moves) >= MaxPlies {
		return Draw, fmt.Sprintf("达到 %d 着上限判和", MaxPlies)
	}
	return Playing, ""
}

// InCheck 当前走子方是否被将军。
func (g *Game) InCheck() bool { return g.Board.InCheck(g.Board.Side) }

// BoardAt 返回走过 ply 步之后的局面（ply=0 即开始局面）。
// 着法列表需要逐行给出「走该步之前的局面」以生成中文记谱，故提供此方法。
func (g *Game) BoardAt(ply int) *Board {
	if ply < 0 {
		ply = 0
	}
	if ply > len(g.Moves) {
		ply = len(g.Moves)
	}
	b, err := ParseFEN(g.StartFEN)
	if err != nil {
		b = NewStartBoard()
	}
	for i := 0; i < ply; i++ {
		b.Apply(g.Moves[i])
	}
	return b
}

func sideName(side int) string {
	if side == Red {
		return "红"
	}
	return "黑"
}

// SideName 导出阵营中文名。
func SideName(side int) string { return sideName(side) }
