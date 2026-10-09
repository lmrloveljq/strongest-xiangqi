package match

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"xiangqi/analytics"
	"xiangqi/version"

	"xiangqi/rules"
)

// savePGN 把一局棋写入 PGN 文件（同时追加到 all_games.pgn）。
func (r *Runner) savePGN(dir string, rec *GameRecord) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.emit(Event{Type: EvError, Err: err, Message: "创建对局目录失败：" + err.Error()})
		return
	}
	text := FormatPGN(rec)
	path := OutputPath(dir, rec.Index)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		r.emit(Event{Type: EvError, Err: err, Message: "保存棋谱失败：" + err.Error()})
		return
	}
	rec.PGNPath = path
	// 汇总文件：便于一次性回看全部对局
	all := filepath.Join(dir, "all_games.pgn")
	f, err := os.OpenFile(all, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		_, _ = f.WriteString(text)
		_, _ = f.WriteString("\n\n")
		_ = f.Close()
	}
}

// FormatPGN 生成一局棋的 PGN 文本。
//
// 中国象棋没有官方 PGN 标准，本格式沿用国际象棋 PGN 的头字段习惯，
// 着法用 UCI 坐标 + 中文记谱注释并列，便于其它软件解析也便于人读。
func FormatPGN(rec *GameRecord) string {
	var sb strings.Builder
	w := func(k, v string) { fmt.Fprintf(&sb, "[%s \"%s\"]\n", k, v) }
	w("Event", "象棋强软 "+version.VERSION+" 引擎自动对战")
	w("Site", "本机 (Windows)")
	w("Date", strings.ReplaceAll(rec.Date, "-", "."))
	w("Round", fmt.Sprint(rec.Index))
	w("Red", rec.RedName)
	w("Black", rec.BlackName)
	w("Result", rec.ResultText())
	w("FEN", rec.StartFEN)
	w("TimeControl", rec.TimeCtrl)
	w("Termination", rec.Reason)
	w("PlyCount", fmt.Sprint(len(rec.Moves)))
	sb.WriteString("\n")

	lineLen := 0
	for i, m := range rec.Moves {
		tok := m.UCI
		if m.Side == "red" {
			tok = fmt.Sprintf("%d. %s", i/2+1, m.UCI)
		}
		tok = fmt.Sprintf("%s {%s}", tok, m.Chinese)
		if lineLen+len(tok) > 100 {
			sb.WriteString("\n")
			lineLen = 0
		}
		sb.WriteString(tok)
		sb.WriteString(" ")
		lineLen += len(tok) + 1
	}
	if len(rec.Moves) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(rec.ResultText())
	sb.WriteString("\n")
	return sb.String()
}

// buildReport 汇总整场对战结果。
func (r *Runner) buildReport(opts Options, elapsed time.Duration) *Report {
	rep := &Report{
		Total:    opts.Games,
		Games:    append([]*GameRecord(nil), r.records...),
		SelfName: r.self.Name,
		OppName:  r.opp.Name,
		LimitCN:  fmt.Sprintf("己方 %s / 对手 %s", opts.LimitSelf.Label(), opts.LimitOpp.Label()),
		Elapsed:  elapsed,
		Dir:      opts.OutputDir,
	}
	var selfDepth, oppDepth, selfNPS, oppNPS, selfNodes, oppNodes float64
	var selfMoves, oppMoves float64

	for _, rec := range r.records {
		selfWin := (rec.Result == rules.RedWin && rec.RedIsSelf) || (rec.Result == rules.BlackWin && !rec.RedIsSelf)
		draw := rec.Result == rules.Draw
		switch {
		case draw:
			rep.Draws++
		case selfWin:
			rep.SelfWins++
		default:
			rep.SelfLosses++
		}
		st := &rep.AsRed
		if !rec.RedIsSelf {
			st = &rep.AsBlack
		}
		st.Games++
		switch {
		case draw:
			st.Draws++
		case selfWin:
			st.Wins++
		default:
			st.Losses++
		}
		for _, m := range rec.Moves {
			isSelf := (m.Side == "red") == rec.RedIsSelf
			if isSelf {
				selfDepth += float64(m.Depth)
				selfNPS += float64(m.NPS)
				selfNodes += float64(m.Nodes)
				selfMoves++
			} else {
				oppDepth += float64(m.Depth)
				oppNPS += float64(m.NPS)
				oppNodes += float64(m.Nodes)
				oppMoves++
			}
		}
	}
	played := len(r.records)
	if played > 0 {
		rep.SelfWinRate = (float64(rep.SelfWins) + 0.5*float64(rep.Draws)) / float64(played) * 100
	}
	if selfMoves > 0 {
		rep.AvgDepthSelf = selfDepth / selfMoves
		rep.AvgNPSSelf = selfNPS / selfMoves
		rep.AvgNodesSelf = selfNodes / selfMoves
	}
	if oppMoves > 0 {
		rep.AvgDepthOpp = oppDepth / oppMoves
		rep.AvgNPSOpp = oppNPS / oppMoves
		rep.AvgNodesOpp = oppNodes / oppMoves
	}
	return rep
}

// FormatReport 生成 report.txt 的文本内容。
func FormatReport(rep *Report) string {
	var sb strings.Builder
	line := strings.Repeat("=", 64)
	sb.WriteString(line + "\n")
	sb.WriteString("            象棋强软 " + version.VERSION + "  引擎自动对战报告\n")
	sb.WriteString(line + "\n")
	fmt.Fprintf(&sb, "生成时间    : %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "己方引擎    : %s\n", rep.SelfName)
	fmt.Fprintf(&sb, "对手引擎    : %s\n", rep.OppName)
	fmt.Fprintf(&sb, "时间控制    : %s\n", rep.LimitCN)
	fmt.Fprintf(&sb, "计划局数    : %d    实际完成: %d\n", rep.Total, len(rep.Games))
	fmt.Fprintf(&sb, "总用时      : %s\n", rep.Elapsed.Round(time.Second))
	sb.WriteString(strings.Repeat("-", 64) + "\n")
	fmt.Fprintf(&sb, "总比分（己方视角）: 胜 %d   和 %d   负 %d\n", rep.SelfWins, rep.Draws, rep.SelfLosses)
	fmt.Fprintf(&sb, "己方胜率（胜 + 0.5×和）/ 已完成局数 = %.2f%%\n", rep.SelfWinRate)
	fmt.Fprintf(&sb, "按先后手分列:  执红 %d 局（胜 %d 和 %d 负 %d）   执黑 %d 局（胜 %d 和 %d 负 %d）\n",
		rep.AsRed.Games, rep.AsRed.Wins, rep.AsRed.Draws, rep.AsRed.Losses,
		rep.AsBlack.Games, rep.AsBlack.Wins, rep.AsBlack.Draws, rep.AsBlack.Losses)
	sb.WriteString(strings.Repeat("-", 64) + "\n")
	fmt.Fprintf(&sb, "平均搜索深度: 己方 %.1f 层    对手 %.1f 层\n", rep.AvgDepthSelf, rep.AvgDepthOpp)
	fmt.Fprintf(&sb, "平均节点/秒 : 己方 %.0f      对手 %.0f\n", rep.AvgNPSSelf, rep.AvgNPSOpp)
	fmt.Fprintf(&sb, "平均节点数  : 己方 %.0f      对手 %.0f\n", rep.AvgNodesSelf, rep.AvgNodesOpp)
	sb.WriteString(strings.Repeat("-", 64) + "\n")
	sb.WriteString("逐局结果:\n")
	for _, g := range rep.Games {
		fmt.Fprintf(&sb, "  #%-3d 红 %-28s vs 黑 %-28s %-7s %-16s 着数 %-4d %s\n",
			g.Index, trunc(g.RedName, 28), trunc(g.BlackName, 28),
			g.ResultText(), trunc(g.Reason, 16), len(g.Moves), g.ResultCN())
	}
	sb.WriteString(strings.Repeat("-", 64) + "\n")
	if rep.Dir != "" {
		fmt.Fprintf(&sb, "棋谱目录    : %s\n", rep.Dir)
	}
	sb.WriteString(line + "\n")
	return sb.String()
}

// WriteScoresCSV 把整场对战每一步的引擎数据写成 CSV（与 report.txt 同目录）。
//
// 【v1.6.8 / 头脑风暴第 15 条】用户要「整局评分导出（CSV / 复盘表）」：
// 对战的每一着本来就带完整数据（分值/深度/节点/耗时），以前只进 report.txt 的汇总，
// 现在导成表格便于复盘与统计。
//
// 两个细节：
//   - 开头写 UTF-8 BOM：否则 Excel 打开中文列名是乱码；
//   - 字段顺序固定，便于再导入别的工具。
func WriteScoresCSV(dir string, rep *Report) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("\ufeff") // UTF-8 BOM：Excel 友好
	b.WriteString("局号,着数,走子方,引擎,中文着法,UCI,分值,深度,节点,NPS,引擎耗时(ms),实际耗时(ms),是否首选\r\n")
	for gi, g := range rep.Games {
		for _, mv := range g.Moves {
			side := "红"
			if mv.Side == "black" {
				side = "黑"
			}
			best := "否"
			if mv.Best {
				best = "是"
			}
			fmt.Fprintf(&b, "%d,%d,%s,%s,%s,%s,%s,%d,%d,%d,%d,%d,%s\r\n",
				gi+1, mv.Ply, side, csvCell(mv.Engine), csvCell(mv.Chinese), mv.UCI,
				scoreCSV(mv.Score), mv.Depth, mv.Nodes, mv.NPS, mv.EngineMS, mv.WallMS, best)
		}
	}
	path := filepath.Join(dir, "scores.csv")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// scoreCSV 把分值列成文字（与界面上的写法一致，方便直接对照）。
func scoreCSV(s analytics.Score) string {
	if !s.Valid {
		return ""
	}
	if s.Mate {
		if s.N > 0 {
			return fmt.Sprintf("将死%d步", s.N)
		}
		return fmt.Sprintf("被杀%d步", -s.N)
	}
	if s.CP > 0 {
		return fmt.Sprintf("+%d", s.CP)
	}
	return fmt.Sprintf("%d", s.CP)
}

// csvCell 给可能含逗号的字段加引号。
func csvCell(s string) string {
	if strings.ContainsAny(s, ",\"\"\r\n") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}

// WriteReport 把报告写入目录下的 report.txt，返回文件路径。
func WriteReport(dir string, rep *Report) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("报告目录为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "report.txt")
	text := FormatReport(rep)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	rep.FilePath = path
	return path, nil
}

// SummaryText 返回弹窗用的简要摘要。
func SummaryText(rep *Report) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "对局完成：%d / %d 局\n", len(rep.Games), rep.Total)
	fmt.Fprintf(&sb, "总比分：胜 %d   和 %d   负 %d\n", rep.SelfWins, rep.Draws, rep.SelfLosses)
	fmt.Fprintf(&sb, "己方胜率：%.2f%%\n", rep.SelfWinRate)
	fmt.Fprintf(&sb, "执红 %d 局（%d 胜）  执黑 %d 局（%d 胜）\n",
		rep.AsRed.Games, rep.AsRed.Wins, rep.AsBlack.Games, rep.AsBlack.Wins)
	fmt.Fprintf(&sb, "平均深度：己方 %.1f / 对手 %.1f\n", rep.AvgDepthSelf, rep.AvgDepthOpp)
	fmt.Fprintf(&sb, "平均 NPS：己方 %.0f / 对手 %.0f\n", rep.AvgNPSSelf, rep.AvgNPSOpp)
	if rep.FilePath != "" {
		fmt.Fprintf(&sb, "\n报告文件：%s", rep.FilePath)
	}
	return sb.String()
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
