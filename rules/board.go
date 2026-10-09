// Package rules 实现中国象棋的棋盘表示、FEN 解析、走法生成、合法性判定与胜负判定。
//
// 本包是纯计算包：不依赖 UI、不启动任何引擎子进程，因此可以独立单元测试，
// 也可以在引擎通信的 goroutine 中安全调用（Board 自身不是并发安全的，
// 每个 goroutine 使用自己的 Board 实例即可）。
//
// ============================ 坐标体系 ============================
//
// 采用与 UCI/UCCI 中国象棋协议完全一致的坐标：文件 a..i，行 0..9。
// 红方在下（行 0 为红方底线），黑方在上（行 9 为黑方底线）。
//
//	rank 9   a9  b9  c9  d9  e9  f9  g9  h9  i9    ← 黑方底线（将 e9）
//	rank 8   a8  b8  c8  d8  e8  f8  g8  h8  i8    ← 黑方九宫
//	rank 7   a7  b7  c7  d7  e7  f7  g7  h7  i7    ← 黑炮 b7 / h7
//	rank 6   a6  b6  c6  d6  e6  f6  g6  h6  i6    ← 黑卒 a6 c6 e6 g6 i6
//	rank 5   ────────────── 楚 河 ──────────────
//	rank 4   ────────────── 汉 界 ──────────────
//	rank 3   a3  b3  c3  d3  e3  f3  g3  h3  i3    ← 红兵 a3 c3 e3 g3 i3
//	rank 2   a2  b2  c2  d2  e2  f2  g2  h2  i2    ← 红炮 b2 / h2
//	rank 1   a1  b1  c1  d1  e1  f1  g1  h1  i1    ← 红方九宫
//	rank 0   a0  b0  c0  d0  e0  f0  g0  h0  i0    ← 红方底线（帅 e0，a0 红方左车）
//	         a   b   c   d   e   f   g   h   i
//
// 内部线性索引：Index = rank*9 + file，取值 0..89。
//
//	帅 e0 → 0*9+4 = 4        将 e9 → 9*9+4 = 85
//	红车 a0 → 0              黑车 i9 → 89
//
// 屏幕绘制换算：屏幕第 0 行在最上方（黑方底线），故
//
//	屏幕行 = 9 - rank        屏幕列 = file
//
// 该换算集中在本文件的 ScreenRow/ScreenCol 中，UI 层不得自行推导。
package rules

import (
	"fmt"
	"strings"
)

// 棋子编码：低 3 位为兵种，第 4 位（值 8）为颜色位。
//
//	bit3 = 0 → 红方     bit3 = 1 → 黑方
//	bits0-2 = 兵种（1..7）
//
// 故 红帅 = 0b0001 = 1，黑将 = 0b1001 = 9。
type Piece uint8

const (
	PEmpty Piece = 0 // 空

	PKing     Piece = 1 // 帅 / 将
	PAdvisor  Piece = 2 // 仕 / 士
	PElephant Piece = 3 // 相 / 象
	PHorse    Piece = 4 // 馬 / 马
	PRook     Piece = 5 // 車 / 车
	PCannon   Piece = 6 // 炮 / 炮
	PPawn     Piece = 7 // 兵 / 卒

	colorBit Piece = 8
)

// 常用棋子常量（供 switch / 比较使用，必须是编译期常量）。
const (
	PRedKing       = PKing                // 1
	PRedAdvisor    = PAdvisor             // 2
	PRedElephant   = PElephant            // 3
	PRedHorse      = PHorse               // 4
	PRedRook       = PRook                // 5
	PRedCannon     = PCannon              // 6
	PRedPawn       = PPawn                // 7
	PBlackKing     = PKing | colorBit     // 9
	PBlackAdvisor  = PAdvisor | colorBit  // 10
	PBlackElephant = PElephant | colorBit // 11
	PBlackHorse    = PHorse | colorBit    // 12
	PBlackRook     = PRook | colorBit     // 13
	PBlackCannon   = PCannon | colorBit   // 14
	PBlackPawn     = PPawn | colorBit     // 15
)

// 阵营
const (
	Red   = 0
	Black = 1
)

// MakePiece 由阵营与兵种构造棋子编码。
func MakePiece(side int, t Piece) Piece {
	p := t & 7
	if side == Black {
		p |= colorBit
	}
	return p
}

// Type 返回兵种（1..7，空子返回 0）。
func (p Piece) Type() Piece { return p & 7 }

// Side 返回阵营（Red / Black）。对空子调用返回 Red，调用前应先用 IsEmpty 判断。
func (p Piece) Side() int {
	if p&colorBit != 0 {
		return Black
	}
	return Red
}

// IsEmpty 判断是否为空子。
func (p Piece) IsEmpty() bool { return p == 0 }

// Name 返回棋子的中文名（红方用「車」「馬」，黑方用「车」「马」）。
func (p Piece) Name() string {
	if p.IsEmpty() {
		return "空"
	}
	red := [...]string{"", "帅", "仕", "相", "馬", "車", "炮", "兵"}
	black := [...]string{"", "将", "士", "象", "马", "车", "炮", "卒"}
	if p.Side() == Red {
		return red[p.Type()]
	}
	return black[p.Type()]
}

// 常量：棋盘尺寸
const (
	Files   = 9             // 纵线数（a..i）
	Ranks   = 10            // 横线数（0..9）
	Squares = Files * Ranks // 90
)

// Index 由文件与行计算线性索引。
func Index(file, rank int) int { return rank*Files + file }

// FileOf 取线性索引对应的文件（0..8）。
func FileOf(sq int) int { return sq % Files }

// RankOf 取线性索引对应的行（0..9）。
func RankOf(sq int) int { return sq / Files }

// OnBoard 判断文件/行是否在棋盘内。
func OnBoard(file, rank int) bool {
	return file >= 0 && file < Files && rank >= 0 && rank < Ranks
}

// ScreenRow 返回该行在屏幕上的行号（屏幕第 0 行 = 黑方底线 = rank 9）。
func ScreenRow(rank int) int { return Ranks - 1 - rank }

// ScreenCol 返回该文件在屏幕上的列号（与文件号相同，红方视角 a 在左）。
func ScreenCol(file int) int { return file }

// SquareName 把线性索引转成 UCI 坐标字符串，如 4 → "e0"。
func SquareName(sq int) string {
	if sq < 0 || sq >= Squares {
		return "??"
	}
	return fmt.Sprintf("%c%d", 'a'+FileOf(sq), RankOf(sq))
}

// ParseSquare 把 UCI 坐标字符串（如 "e0"）解析为线性索引。
func ParseSquare(s string) (int, bool) {
	if len(s) < 2 {
		return 0, false
	}
	f := int(s[0] - 'a')
	r := int(s[1] - '0')
	if !OnBoard(f, r) {
		return 0, false
	}
	return Index(f, r), true
}

// Board 表示一个中国象棋局面。
type Board struct {
	Sq   [Squares]Piece // 90 格，Index(rank,file) 定位
	Side int            // 轮到哪一方走：Red / Black
}

// StartFEN 是中国象棋标准初始局面 FEN。
// 行顺序为 rank 9 → rank 0，大写为红方、小写为黑方。
const StartFEN = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

// NewStartBoard 返回初始局面。
func NewStartBoard() *Board {
	b, err := ParseFEN(StartFEN)
	if err != nil {
		// StartFEN 是编译期常量，永不出错；此处仅为签名完整性。
		panic("rules: 内置初始局面 FEN 非法: " + err.Error())
	}
	return b
}

// Clone 深拷贝一个局面。
func (b *Board) Clone() *Board {
	nb := *b
	return &nb
}

// fenCharToPiece 把 FEN 字符转成棋子编码。
func fenCharToPiece(c rune) (Piece, bool) {
	var side int
	var t Piece
	switch c {
	case 'K', 'k':
		t = PKing
	case 'A', 'a':
		t = PAdvisor
	case 'B', 'b', 'E', 'e': // E/e 为部分软件使用的象的别名
		t = PElephant
	case 'N', 'n', 'H', 'h': // H/h 为部分软件使用的马的别名
		t = PHorse
	case 'R', 'r':
		t = PRook
	case 'C', 'c':
		t = PCannon
	case 'P', 'p':
		t = PPawn
	default:
		return PEmpty, false
	}
	if c >= 'A' && c <= 'Z' {
		side = Red
	} else {
		side = Black
	}
	return MakePiece(side, t), true
}

// pieceToFENChar 把棋子编码转成 FEN 字符（红大写、黑小写）。
func pieceToFENChar(p Piece) byte {
	var c byte
	switch p.Type() {
	case PKing:
		c = 'K'
	case PAdvisor:
		c = 'A'
	case PElephant:
		c = 'B'
	case PHorse:
		c = 'N'
	case PRook:
		c = 'R'
	case PCannon:
		c = 'C'
	case PPawn:
		c = 'P'
	default:
		return '?'
	}
	if p.Side() == Black {
		c += 'a' - 'A'
	}
	return c
}

// ParseFEN 解析中国象棋 FEN。
//
// 形如 "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"，
// 第一段共 10 行（rank 9 → rank 0），数字表示连续空格数。
func ParseFEN(fen string) (*Board, error) {
	fields := strings.Fields(fen)
	if len(fields) == 0 {
		return nil, fmt.Errorf("FEN 为空")
	}
	rows := strings.Split(fields[0], "/")
	if len(rows) != Ranks {
		return nil, fmt.Errorf("FEN 行数应为 %d，实际 %d", Ranks, len(rows))
	}
	b := &Board{Side: Red}
	for i, row := range rows {
		rank := Ranks - 1 - i // 第 0 段是 rank 9
		file := 0
		for _, c := range row {
			if c >= '1' && c <= '9' {
				file += int(c - '0')
				continue
			}
			if file >= Files {
				return nil, fmt.Errorf("FEN 第 %d 段超出 9 列", i+1)
			}
			p, ok := fenCharToPiece(c)
			if !ok {
				return nil, fmt.Errorf("FEN 含非法棋子字符 %q", c)
			}
			b.Sq[Index(file, rank)] = p
			file++
		}
		if file != Files {
			return nil, fmt.Errorf("FEN 第 %d 段列数为 %d，应为 %d", i+1, file, Files)
		}
	}
	if len(fields) > 1 {
		switch strings.ToLower(fields[1]) {
		case "w", "r":
			b.Side = Red
		case "b":
			b.Side = Black
		default:
			return nil, fmt.Errorf("FEN 走子方字段非法: %q", fields[1])
		}
	}
	return b, nil
}

// FEN 输出当前局面的 FEN（含走子方；着法计数固定为 " - - 0 1"，
// 因为象棋引擎不依赖该计数，重复局面判定由本软件自己完成）。
func (b *Board) FEN() string {
	var sb strings.Builder
	for i := 0; i < Ranks; i++ {
		rank := Ranks - 1 - i
		empty := 0
		for file := 0; file < Files; file++ {
			p := b.Sq[Index(file, rank)]
			if p.IsEmpty() {
				empty++
				continue
			}
			if empty > 0 {
				sb.WriteByte(byte('0' + empty))
				empty = 0
			}
			sb.WriteByte(pieceToFENChar(p))
		}
		if empty > 0 {
			sb.WriteByte(byte('0' + empty))
		}
		if i != Ranks-1 {
			sb.WriteByte('/')
		}
	}
	sb.WriteByte(' ')
	if b.Side == Red {
		sb.WriteByte('w')
	} else {
		sb.WriteByte('b')
	}
	sb.WriteString(" - - 0 1")
	return sb.String()
}

// Key 返回用于重复局面判定的键（棋盘 + 走子方，忽略计数器）。
func (b *Board) Key() string {
	f := b.FEN()
	if i := strings.IndexByte(f, ' '); i >= 0 {
		return f[:i+2]
	}
	return f
}

// Material 统计某一方的子力（仅用于界面展示与调试，不影响规则判定）。
func (b *Board) Material(side int) int {
	v := map[Piece]int{PKing: 0, PAdvisor: 2, PElephant: 2, PHorse: 4, PRook: 9, PCannon: 4, PPawn: 1}
	sum := 0
	for i := 0; i < Squares; i++ {
		p := b.Sq[i]
		if p.IsEmpty() || p.Side() != side {
			continue
		}
		sum += v[p.Type()]
	}
	return sum
}

// FindKing 返回某一方将/帅所在格的线性索引，找不到返回 -1。
func (b *Board) FindKing(side int) int {
	want := MakePiece(side, PKing)
	for i := 0; i < Squares; i++ {
		if b.Sq[i] == want {
			return i
		}
	}
	return -1
}
