// Package engine 实现象棋引擎子进程的双协议（UCI / UCCI）通信、动态参数发现与引擎库管理。
//
// 设计要点：
//
//   - 每个引擎一个独立子进程 + 独立读取 goroutine，通过 channel 回传结果，
//     绝不阻塞 Fyne 的 UI 线程。
//   - 所有可能阻塞的等待都有超时；进程异常退出通过 exited channel 通知。
//   - 引擎上报的 option 行被动态解析为 Option 列表，界面据此生成参数面板，
//     因此「引擎暴露什么参数，界面就显示什么参数」，不存在硬编码的参数清单。
package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"xiangqi/analytics"
)

// Protocol 表示引擎使用的通信协议。
type Protocol int

const (
	ProtoUnknown Protocol = iota
	ProtoUCI              // UCI：uci → uciok（Pikafish 等）
	ProtoUCCI             // UCCI：ucci → ucciok（旋风、名手等国内引擎）
)

func (p Protocol) String() string {
	switch p {
	case ProtoUCI:
		return "UCI"
	case ProtoUCCI:
		return "UCCI"
	default:
		return "未知"
	}
}

// ProtocolFromString 由字符串还原协议（用于读取 engines.json）。
func ProtocolFromString(s string) Protocol {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "UCI":
		return ProtoUCI
	case "UCCI":
		return ProtoUCCI
	default:
		return ProtoUnknown
	}
}

// Option 是引擎通过 `uci` / `ucci` 命令上报的一个可设置参数。
type Option struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"` // check / spin / combo / button / string
	Default string   `json:"default"`
	Min     int      `json:"min"`
	Max     int      `json:"max"`
	Vars    []string `json:"vars,omitempty"`
}

// IsSpin 判断是否为整数滑块型参数。
func (o Option) IsSpin() bool { return strings.EqualFold(o.Type, "spin") }

// IsCheck 判断是否为开关型参数。
func (o Option) IsCheck() bool { return strings.EqualFold(o.Type, "check") }

// IsString 判断是否为字符串型参数。
func (o Option) IsString() bool { return strings.EqualFold(o.Type, "string") }

// IsCombo 判断是否为枚举型参数。
func (o Option) IsCombo() bool { return strings.EqualFold(o.Type, "combo") }

// IsButton 判断是否为动作型参数（无值）。
func (o Option) IsButton() bool { return strings.EqualFold(o.Type, "button") }

// optionKeywords 是 option 行中可能出现的属性关键字。
func isOptionKeyword(s string) bool {
	switch strings.ToLower(s) {
	case "default", "min", "max", "var":
		return true
	}
	return false
}

// ParseOptionLine 解析一行 `option name ... type ... default ... min ... max ...`。
//
// 注意参数名与默认值都可能含空格（例如 "Move Overhead"、路径），
// 因此必须按关键字切分而不是简单按空格切分。
func ParseOptionLine(line string) (Option, bool) {
	s := strings.TrimSpace(line)
	low := strings.ToLower(s)
	const pfx = "option name "
	if !strings.HasPrefix(low, pfx) {
		return Option{}, false
	}
	s = s[len(pfx):]
	idx := strings.Index(strings.ToLower(s), " type ")
	if idx < 0 {
		return Option{}, false
	}
	o := Option{Name: strings.TrimSpace(s[:idx])}
	rest := s[idx+len(" type "):]

	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return Option{}, false
	}
	o.Type = strings.ToLower(fields[0])
	i := 1
	for i < len(fields) {
		kw := strings.ToLower(fields[i])
		i++
		var vals []string
		for i < len(fields) && !isOptionKeyword(fields[i]) {
			vals = append(vals, fields[i])
			i++
		}
		val := strings.Join(vals, " ")
		switch kw {
		case "default":
			o.Default = val
		case "min":
			if n, err := strconv.Atoi(val); err == nil {
				o.Min = n
			}
		case "max":
			if n, err := strconv.Atoi(val); err == nil {
				o.Max = n
			}
		case "var":
			o.Vars = append(o.Vars, val)
		}
	}
	if o.Name == "" {
		return Option{}, false
	}
	return o, true
}

// LimitMode 是思考限制的方式。
type LimitMode int

const (
	LimitDepth    LimitMode = iota // 固定深度
	LimitMoveTime                  // 每步限时
	LimitGameTime                  // 每局总时间（由对战调度换算成每步限时后下发）
)

// Limit 描述一次思考的限制。
type Limit struct {
	// Infinite 为真时下发 `go infinite`：一直算到用户停止（最强引擎模式用）。
	// 不放 LimitMode 常量里，避免动到既有 iota 顺序。
	Infinite   bool
	Mode       LimitMode
	Depth      int // 固定深度模式使用
	MoveTimeMS int // 每步限时模式使用（毫秒）
	GameTimeMS int // 每局总时间（毫秒），仅对战调度使用
}

// Command 返回该限制对应的 `go` 命令。
//
// UCI  : go depth N / go movetime N（毫秒）
// UCCI : go depth N / go time N（毫秒）
//
// UCCI 协议没有 movetime，惯用 `go time` 表示可用思考时间，因此两种协议分别下发。
func (l Limit) Command(proto Protocol) string {
	// 【v1.6.11】无限分析：算到用户按停止为止（引擎收到 stop 才回 bestmove）
	if l.Infinite {
		return "go infinite"
	}
	switch l.Mode {
	case LimitDepth:
		d := l.Depth
		if d <= 0 {
			d = 1
		}
		return fmt.Sprintf("go depth %d", d)
	case LimitMoveTime:
		ms := l.MoveTimeMS
		if ms <= 0 {
			ms = 100
		}
		if proto == ProtoUCCI {
			return fmt.Sprintf("go time %d", ms)
		}
		return fmt.Sprintf("go movetime %d", ms)
	default:
		// LimitGameTime 由调用方换算，这里退化为每步限时
		ms := l.MoveTimeMS
		if ms <= 0 {
			ms = 100
		}
		if proto == ProtoUCCI {
			return fmt.Sprintf("go time %d", ms)
		}
		return fmt.Sprintf("go movetime %d", ms)
	}
}

// HardTimeout 返回本限制允许的最长等待时间（超出即判定引擎无响应）。
//
// 规则：限时模式 = 标称限时 + 60 秒宽限；深度模式 = 60 秒 + 每层 3 秒。
func (l Limit) HardTimeout() time.Duration {
	// 【缺陷修复】无限分析以前会掉进下面的 LimitDepth 分支：Infinite 只置了标志位、
	// 没动 Mode，而 LimitDepth 的零值就是 0，于是"无限"被当成"固定深度 1 层"，
	// 硬超时只有 63 秒 —— 引擎会在第 63 秒被强行 stop。
	// 表现：选了无限分析的人机对弈里，电脑"想了半天"才走一步，棋盘看着就是锁死的。
	if l.Infinite {
		// 兜底而非语义：正常由用户按停止/关引擎结束；这里只防"引擎彻底卡住"。
		return 60 * time.Minute
	}
	switch l.Mode {
	case LimitMoveTime:
		return time.Duration(l.MoveTimeMS)*time.Millisecond + 60*time.Second
	case LimitDepth:
		d := l.Depth
		if d <= 0 {
			d = 1
		}
		return time.Duration(60+d*3) * time.Second
	default:
		return 5 * time.Minute
	}
}

// Label 返回面向界面的中文描述。
func (l Limit) Label() string {
	switch l.Mode {
	case LimitDepth:
		return fmt.Sprintf("固定深度 %d 层", l.Depth)
	case LimitMoveTime:
		return fmt.Sprintf("每步限时 %s", msLabel(l.MoveTimeMS))
	default:
		return fmt.Sprintf("每局总时间 %s", msLabel(l.GameTimeMS))
	}
}

// msLabel 把毫秒数格式化成便于阅读的秒/毫秒文本（小于 1 秒时用毫秒，避免 0.1 秒这种精度损失）。
func msLabel(ms int) string {
	if ms < 1000 {
		return fmt.Sprintf("%d 毫秒", ms)
	}
	return fmt.Sprintf("%.1f 秒", float64(ms)/1000)
}

// Position 描述要下发给引擎的局面。
//
// 每次重开 / 悔棋 / 粘贴新着法序列后都必须重新下发完整局面，
// 因为引擎自身不保存也不校验历史。
type Position struct {
	Startpos bool     // true 表示从初始局面开始
	FEN      string   // 局面 FEN（Startpos 为 true 时仍会填充，供 UCCI 使用）
	Moves    []string // 从该局面起要应用的 UCI 着法序列
}

// Command 生成本局面在当前协议下的 `position` 命令。
//
// UCI  : position startpos moves a b c    /  position fen <FEN> moves a b c
// UCCI : position fen <FEN> moves a b c   （UCCI 引擎对 startpos 支持不一，统一用 FEN 形式）
func (p Position) Command(proto Protocol) string {
	var sb strings.Builder
	if p.Startpos && proto == ProtoUCI {
		sb.WriteString("position startpos")
	} else {
		fen := p.FEN
		if strings.TrimSpace(fen) == "" {
			fen = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"
		}
		sb.WriteString("position fen ")
		sb.WriteString(fen)
	}
	if len(p.Moves) > 0 {
		sb.WriteString(" moves ")
		sb.WriteString(strings.Join(p.Moves, " "))
	}
	return sb.String()
}

// InfoLine 是一条 `info` 行的解析结果。
type InfoLine struct {
	Depth    int
	SelDepth int
	MultiPV  int
	Score    analytics.Score
	Nodes    int64
	NPS      int64
	TimeMS   int64
	HashFull int
	PV       []string
	PVString string // 原始 pv 文本（空格分隔的 UCI 着法）
}

// ParseInfoLine 解析一行 info 输出。
//
// 支持字段：depth / seldepth / multipv / score cp|mate / nodes / nps / time /
// hashfull / pv / currmove / currmovenumber / tbhits / cpuload / string。
// 未知字段被安全跳过，因此对不常见的引擎输出也具备容错性。
func ParseInfoLine(line string) (InfoLine, bool) {
	f := strings.Fields(line)
	if len(f) == 0 || !strings.EqualFold(f[0], "info") {
		return InfoLine{}, false
	}
	info := InfoLine{MultiPV: 1}
	i := 1
	for i < len(f) {
		key := strings.ToLower(f[i])
		i++
		nextInt := func() (int64, bool) {
			if i >= len(f) {
				return 0, false
			}
			n, err := strconv.ParseInt(f[i], 10, 64)
			if err != nil {
				return 0, false
			}
			i++
			return n, true
		}
		switch key {
		case "depth":
			if n, ok := nextInt(); ok {
				info.Depth = int(n)
			}
		case "seldepth":
			if n, ok := nextInt(); ok {
				info.SelDepth = int(n)
			}
		case "multipv":
			if n, ok := nextInt(); ok {
				info.MultiPV = int(n)
			}
		case "nodes":
			if n, ok := nextInt(); ok {
				info.Nodes = n
			}
		case "nps":
			if n, ok := nextInt(); ok {
				info.NPS = n
			}
		case "time":
			if n, ok := nextInt(); ok {
				info.TimeMS = n
			}
		case "hashfull":
			if n, ok := nextInt(); ok {
				info.HashFull = int(n)
			}
		case "score":
			if i < len(f) {
				switch strings.ToLower(f[i]) {
				case "cp":
					i++
					if n, ok := nextInt(); ok {
						info.Score = analytics.Score{CP: int(n), Valid: true}
					}
				case "mate":
					i++
					if n, ok := nextInt(); ok {
						info.Score = analytics.Score{Mate: true, N: int(n), Valid: true}
					}
				case "lowerbound", "upperbound":
					i++
				}
			}
		case "pv":
			info.PV = append([]string(nil), f[i:]...)
			info.PVString = strings.Join(info.PV, " ")
			i = len(f)
		case "currmove", "currmovenumber", "tbhits", "cpuload", "hashfull2":
			if i < len(f) {
				i++
			}
		case "string":
			// info string 后面的内容长度不定，直接丢弃剩余部分
			i = len(f)
		default:
			// 未知字段：跳过其后的一个 token（保守处理，避免死循环）
		}
	}
	if info.Depth == 0 && info.PV == nil && !info.Score.Valid && info.Nodes == 0 {
		return InfoLine{}, false
	}
	return info, true
}

// Result 是一次思考的完整结果。
type Result struct {
	BestMove string
	Lines    []InfoLine      // 按 multipv 升序，Lines[0] 即最佳着法（若引擎给出）
	Depth    int             // 主变例达到的深度
	Nodes    int64           // 节点数
	NPS      int64           // 每秒节点数
	TimeMS   int64           // 思考耗时（引擎自报）
	HashFull int             // 哈希表占用（千分比 0~1000，引擎自报的 hashfull；0 = 引擎没报）
	Score    analytics.Score // 主变例分值（走子方视角）
}

// Aggregate 由多候选信息汇总出一个 Result。
func Aggregate(best string, lines []InfoLine) Result {
	r := Result{BestMove: best, Lines: lines}
	if len(lines) > 0 {
		// 取 multipv 最小者作为主变例
		main := lines[0]
		for _, l := range lines {
			if l.MultiPV < main.MultiPV {
				main = l
			}
		}
		r.Depth = main.Depth
		r.Score = main.Score
		r.HashFull = main.HashFull
		for _, l := range lines {
			if l.Nodes > r.Nodes {
				r.Nodes = l.Nodes
			}
			if l.NPS > r.NPS {
				r.NPS = l.NPS
			}
			if l.TimeMS > r.TimeMS {
				r.TimeMS = l.TimeMS
			}
		}
		r.Depth = main.Depth
	}
	return r
}

// SortOptions 按名称排序参数，保证界面顺序稳定。
func SortOptions(opts []Option) {
	sort.SliceStable(opts, func(i, j int) bool { return opts[i].Name < opts[j].Name })
}
