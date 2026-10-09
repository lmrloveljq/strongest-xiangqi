// Package notation 负责 UCI 坐标着法 <-> 中文记谱的双向转换。
//
// 中文记谱规则（本包实现的子集，覆盖实战与引擎输出所需的全部情形）：
//
//	记谱格式：[前后中]棋子名 + 动作 + 目标   或   棋子名 + 纵线号 + 动作 + 目标
//
//	纵线号：红方以汉字「一」..「九」自红方右侧向左数（红方视角 a 线为九、i 线为一）；
//	        黑方以阿拉伯数字 1..9 自黑方右侧向左数（黑方视角 a 线为 1、i 线为 9）。
//	动作：  平（同行走子）、进（向对方底线方向）、退（向自己底线方向）。
//	目标：  車/炮/兵/卒/帅/将 用「移动的步数」；馬/相/象/仕/士 用「目标纵线号」。
//	同一纵线上有两枚同兵种棋子时，用「前 / 后」代替纵线号（3 枚时中间那枚用「中」，
//	更多枚时依次用 前、二、三、…、后，与通行棋谱写法一致）。
//
// 例：h2e2 → 炮二平五（红炮从 h2 平到 e2）；b0c2 → 馬八进七。
package notation

import (
	"fmt"
	"strconv"
	"strings"

	"xiangqi/rules"
)

// 红方汉字数字（一..九）
var redNumerals = [...]string{"一", "二", "三", "四", "五", "六", "七", "八", "九"}

// num 把 1..9 的数字按阵营转成记谱用字。
func num(side, n int) string {
	if n < 1 || n > 9 {
		return strconv.Itoa(n)
	}
	if side == rules.Red {
		return redNumerals[n-1]
	}
	return strconv.Itoa(n)
}

// fileNumber 把内部文件号（0..8，a=0）转成该方的纵线号（1..9）。
//
//	红方：纵线号 = 9 - file   （红方右侧 i 线 = 一）
//	黑方：纵线号 = file + 1   （黑方右侧 a 线 = 1）
func fileNumber(side, file int) int {
	if side == rules.Red {
		return 9 - file
	}
	return file + 1
}

// UCIToMove 解析 4 字符 UCI 坐标着法（如 "h2e2"）。大小写不敏感。
func UCIToMove(s string) (rules.Move, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) != 4 {
		return rules.Move{}, false
	}
	f1 := int(s[0] - 'a')
	r1 := int(s[1] - '0')
	f2 := int(s[2] - 'a')
	r2 := int(s[3] - '0')
	if !rules.OnBoard(f1, r1) || !rules.OnBoard(f2, r2) {
		return rules.Move{}, false
	}
	return rules.NewMove(rules.Index(f1, r1), rules.Index(f2, r2)), true
}

// MoveToUCI 把着法转成 4 字符 UCI 坐标字符串。
func MoveToUCI(m rules.Move) string { return m.String() }

// IsUCIMove 判断字符串是否是合法的 4 字符 UCI 坐标着法。
func IsUCIMove(s string) bool {
	_, ok := UCIToMove(s)
	return ok
}

// positionNames 返回同一纵线上、按「前→后」排序的同兵种棋子所在格。
//
// 红方的「前」= 行号更大者（更靠近黑方底线）；黑方的「前」= 行号更小者。
func positionNames(b *rules.Board, p rules.Piece, file int) []int {
	var sqs []int
	for r := 0; r < rules.Ranks; r++ {
		sq := rules.Index(file, r)
		if b.Sq[sq] == p {
			sqs = append(sqs, sq)
		}
	}
	// 插入排序：红方按行号降序（前在前），黑方按行号升序
	for i := 1; i < len(sqs); i++ {
		for j := i; j > 0; j-- {
			rj, rj1 := rules.RankOf(sqs[j]), rules.RankOf(sqs[j-1])
			less := false
			if p.Side() == rules.Red {
				less = rj > rj1
			} else {
				less = rj < rj1
			}
			if less {
				sqs[j], sqs[j-1] = sqs[j-1], sqs[j]
			} else {
				break
			}
		}
	}
	return sqs
}

// ordinalPrefix 返回第 idx 枚（0 起，已按前→后排序）棋子的前缀。
func ordinalPrefix(side, idx, total int) string {
	switch {
	case total == 2:
		if idx == 0 {
			return "前"
		}
		return "后"
	case total == 3:
		switch idx {
		case 0:
			return "前"
		case 1:
			return "中"
		default:
			return "后"
		}
	default:
		if idx == 0 {
			return "前"
		}
		if idx == total-1 {
			return "后"
		}
		return num(side, idx+1)
	}
}

// ToChinese 把着法转成中文记谱。b 必须是**走该着法之前**的局面。
//
// 若无法识别起点棋子（例如空盘或坐标越界），回退为 UCI 字符串，保证界面永不崩溃。
func ToChinese(b *rules.Board, m rules.Move) string {
	if b == nil || m.From < 0 || m.From >= rules.Squares || m.To < 0 || m.To >= rules.Squares {
		return m.String()
	}
	p := b.Sq[m.From]
	if p.IsEmpty() {
		return m.String()
	}
	side := p.Side()
	name := p.Name()
	ff, fr := rules.FileOf(m.From), rules.RankOf(m.From)
	tf, tr := rules.FileOf(m.To), rules.RankOf(m.To)

	// 1) 纵线部分：同线同兵种多于一枚时用 前/后/中，否则用纵线号
	same := positionNames(b, p, ff)
	head := ""
	if len(same) > 1 {
		idx := 0
		for i, sq := range same {
			if sq == m.From {
				idx = i
				break
			}
		}
		head = ordinalPrefix(side, idx, len(same)) + name
	} else {
		head = name + num(side, fileNumber(side, ff))
	}

	// 2) 动作与目标
	if tr == fr {
		return head + "平" + num(side, fileNumber(side, tf))
	}
	advancing := (side == rules.Red && tr > fr) || (side == rules.Black && tr < fr)
	action := "退"
	if advancing {
		action = "进"
	}
	switch p.Type() {
	case rules.PHorse, rules.PElephant, rules.PAdvisor:
		// 斜行棋子用目标纵线号
		return head + action + num(side, fileNumber(side, tf))
	default:
		// 直行棋子用移动步数
		d := tr - fr
		if d < 0 {
			d = -d
		}
		return head + action + num(side, d)
	}
}

// FormatMove 返回「中文记谱（UCI）」形式，如 "炮二平五(h2e2)"。
func FormatMove(b *rules.Board, m rules.Move) string {
	return fmt.Sprintf("%s(%s)", ToChinese(b, m), m.String())
}

// ParseMoveList 从一段文本中提取全部 UCI 着法。
//
// 兼容用户从第三方软件复制来的多种格式：
//
//	"h2e2 h9g7 c3c4"
//	"1. h2e2 h9g7 2. c3c4"
//	"position startpos moves h2e2 h9g7"
//	换行、逗号、顿号、分号分隔均可。
//
// 返回成功解析的着法与无法识别的记号（供界面提示）。
func ParseMoveList(text string) ([]rules.Move, []string) {
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")
	for _, sep := range []string{",", "，", "、", ";", "；", "|"} {
		text = strings.ReplaceAll(text, sep, " ")
	}
	// 去掉 "position startpos moves" / "position fen ... moves" 之类的前缀
	fields := strings.Fields(text)
	start := 0
	for i, f := range fields {
		lf := strings.ToLower(f)
		if lf == "moves" {
			start = i + 1
			break
		}
	}
	var moves []rules.Move
	var skipped []string
	for _, f := range fields[start:] {
		tok := strings.TrimSpace(f)
		// 跳过着法序号，如 "1." "1..." "12" "1)"
		trimmed := strings.Trim(tok, ".")
		if trimmed == "" {
			continue
		}
		if _, err := strconv.Atoi(trimmed); err == nil {
			continue
		}
		if m, ok := UCIToMove(tok); ok {
			moves = append(moves, m)
			continue
		}
		skipped = append(skipped, tok)
	}
	return moves, skipped
}

// MoveListToUCI 把着法序列拼成空格分隔的 UCI 串。
func MoveListToUCI(moves []rules.Move) string {
	parts := make([]string, 0, len(moves))
	for _, m := range moves {
		parts = append(parts, m.String())
	}
	return strings.Join(parts, " ")
}

// PieceChar 返回棋子的单字名（用于棋盘绘制）。
func PieceChar(p rules.Piece) string { return p.Name() }
