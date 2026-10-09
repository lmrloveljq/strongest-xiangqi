package ui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"xiangqi/config"
	"xiangqi/engine"
)

// 本文件提供界面复用的小组件：分区容器、最佳着法面板、多候选列表、参数面板。

// sectionTitle 返回一个粗体小标题。
func sectionTitle(text string) *canvas.Text {
	t := canvas.NewText(text, colFore)
	t.TextSize = textSize(textLabel)
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

// smallText 返回一个次要说明文字。
func smallText(text string) *canvas.Text {
	t := canvas.NewText(text, colForeDim)
	t.TextSize = textSize(textSmall)
	return t
}

// section 把内容包成「标题 + 内容」的卡片式分区。
func section(title string, content fyne.CanvasObject) fyne.CanvasObject {
	head := container.NewHBox(sectionTitle(title), layoutSpacer())
	return container.NewBorder(head, nil, nil, nil, content)
}

// layoutSpacer 是一个可伸缩的空白，用于把同一行的元素推到两端。
func layoutSpacer() fyne.CanvasObject {
	r := canvas.NewRectangle(colPanelBG)
	r.SetMinSize(fyne.NewSize(1, 1))
	return r
}

// commas 把整数格式化为带千分位的字符串（引擎节点数很大，需要可读）。
func commas(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprint(v)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// 最佳着法面板
// ---------------------------------------------------------------------------

// BestMovePanel 展示引擎首选着法：中文记谱、UCI 坐标、胜率、搜索统计与主变例。
type BestMovePanel struct {
	Root fyne.CanvasObject

	chinese  *canvas.Text
	uci      *canvas.Text
	winrate  *canvas.Text
	stats    *widget.Label
	pv       *widget.Label
	status   *canvas.Text
	uciSmall *canvas.Text

	// details 是「引擎详细数据」（深度/节点/速度/耗时、主变例、思考限制），默认收起。
	details   *fyne.Container
	btnDetail *widget.Button

	OnCopy     func()
	OnCopyPV   func()
	currentUCI string
	currentPV  string
}

// NewBestMovePanel 创建最佳着法面板。
func NewBestMovePanel(onCopy, onCopyPV func()) *BestMovePanel {
	p := &BestMovePanel{OnCopy: onCopy, OnCopyPV: onCopyPV}
	p.chinese = canvas.NewText("—", colPrimary)
	p.chinese.TextSize = textSize(textBig)
	p.chinese.TextStyle = fyne.TextStyle{Bold: true}
	p.uci = canvas.NewText("等待局面输入", colForeDim)
	p.uci.TextSize = textSize(textLabel)
	p.uciSmall = canvas.NewText("", colForeDim)
	p.uciSmall.TextSize = textSize(textSmall)
	p.winrate = canvas.NewText("红方胜率 —", colFore)
	p.winrate.TextSize = textSize(textLabel)
	// 统计行改成会自动换行的 Label：现在写成「深度: 17 层  分数: +54  用时: 1.0 秒
	// 速度: 3189 千节点/秒  算过 2,761,505 个局面」，右栏半宽放不下要能折行
	p.stats = widget.NewLabel("深度 —  用时 —  速度 —")
	p.stats.Wrapping = fyne.TextWrapWord
	p.pv = widget.NewLabel("主变例：—")
	p.pv.Wrapping = fyne.TextWrapWord
	p.status = canvas.NewText("", colForeDim)
	p.status.TextSize = textSize(textSmall)

	btnCopy := widget.NewButton("复制最佳着法（UCI）", func() {
		if p.OnCopy != nil {
			p.OnCopy()
		}
	})
	btnCopy.Importance = widget.HighImportance

	// 精简：去掉「UCI 坐标」那一行与「复制主变例」按钮（用户反馈右栏太密集）。
	// 最佳着法的 UCI 依然在「复制最佳着法」里一键可得，不必常驻显示。
	// UCI 坐标加回来，但压成小字灰色附在中文记谱下面：
	// 一是用户核对「弹出来的走法对不对」需要它，
	// 二是出问题时可以直接把这一行报给我，比只报中文精确得多。
	//
	// 【v1.5 简化】首屏只回答两个问题：「引擎让你走哪一步」「现在谁好」。
	// 深度/节点/速度/耗时、主变例、思考限制全部收进默认收起的「引擎详细数据」——
	// 这些是给想深挖的人看的，不该在第一次打开软件时就糊一脸。
	// 【v1.6 精简】「红方胜率 51.9%」这一行删掉：上面的局势条已经写着
	// 「红 52% : 48% 黑 +21」，同一个数字说两遍只会让面板更挤。
	//
	// 【v1.6.2】把「主变例」从折叠区提回常显：右栏现在是对半两栏，
	// 每一栏都比原来窄而高，主变例正好把最佳着法卡片下方的空地填上，
	// 而且它本来就是棋手最常看的一行（TCHESS 也是常显思考明细）。
	left := container.NewVBox(p.chinese, p.uciSmall, p.pv)
	right := container.NewVBox(btnCopy)
	top := container.NewBorder(nil, nil, nil, right, left)

	// 折叠区只放「引擎统计 + 思考限制」；主变例已提到常显（见上），
	// 同一个控件不能挂在两处（Fyne 会把同一对象画两次，表现为文字重影）。
	p.details = container.NewVBox(p.stats, p.status)
	p.details.Hide()
	p.btnDetail = widget.NewButton("思考细节 ▾", func() {
		if p.details.Visible() {
			p.details.Hide()
			p.btnDetail.SetText("思考细节 ▾")
		} else {
			p.details.Show()
			p.btnDetail.SetText("收起思考细节 ▴")
		}
	})
	p.btnDetail.Importance = widget.LowImportance

	p.Root = container.NewVBox(top, p.btnDetail, p.details)
	return p
}

// thinkDetailLine 拼「思考细节」→引擎统计行。
//
// 【v1.6.5】用户要求「显示一部分能看得懂的参数，比如引擎后续计算的深度和计算时间、
// 计算速度等等」——所以字段顺序沿用同类软件（TCHESS 深度/分数/…/时间），
// 但措辞与单位全部改成中文可读：
//
//	深度: 17 层  分数: +54  用时: 1.0 秒  速度: 3189 千节点/秒  算过 2,761,505 个局面
//
// 不再出现 NPS / K 这类缩写（原来写「NPS: 3189K」，非专业用户看不懂）。
func thinkDetailLine(depth int, scoreTxt string, nodes, nps, timeMS int64, hashPermille int) string {
	scorePart := "分数: " + scoreTxt
	if scoreTxt == "" {
		scorePart = "分数: —"
	}
	if strings.Contains(scoreTxt, "绝杀") {
		scorePart = scoreTxt // scoreText() 已经写成「绝杀 N 步」，直接用
	}
	parts := []string{fmt.Sprintf("深度: %d 层", depth), scorePart}
	if timeMS > 0 {
		parts = append(parts, fmt.Sprintf("用时: %.1f 秒", float64(timeMS)/1000))
	}
	if nps > 0 {
		parts = append(parts, fmt.Sprintf("速度: %s 千节点/秒", commas(nps/1000)))
	}
	if nodes > 0 {
		parts = append(parts, "算过 "+commas(nodes)+" 个局面")
	}
	// 【v1.6.8 / 头脑风暴第 8 条】哈希占用：引擎自报的 hashfull（千分比）
	if hashPermille > 0 {
		parts = append(parts, fmt.Sprintf("哈希占用: %.0f%%", float64(hashPermille)/10))
	}
	return strings.Join(parts, "  ")
}

// candMetaLine 把一条候选着法对应的引擎统计写成大白话（点开变招行时才显示）。
//
// 例：深度 18 层（最远算到 24 层）  用时 1.0 秒  速度 3189 千节点/秒  算过 2,761,505 个局面
func candMetaLine(l engine.InfoLine) string {
	parts := make([]string, 0, 4)
	if l.Depth > 0 {
		if l.SelDepth > l.Depth {
			parts = append(parts, fmt.Sprintf("深度 %d 层（最远算到 %d 层）", l.Depth, l.SelDepth))
		} else {
			parts = append(parts, fmt.Sprintf("深度 %d 层", l.Depth))
		}
	}
	if l.TimeMS > 0 {
		parts = append(parts, fmt.Sprintf("用时 %.1f 秒", float64(l.TimeMS)/1000))
	}
	if l.NPS > 0 {
		parts = append(parts, fmt.Sprintf("速度 %s 千节点/秒", commas(l.NPS/1000)))
	}
	if l.Nodes > 0 {
		parts = append(parts, "算过 "+commas(l.Nodes)+" 个局面")
	}
	if l.HashFull > 0 {
		parts = append(parts, fmt.Sprintf("哈希占用 %.0f%%", float64(l.HashFull)/10))
	}
	if len(parts) == 0 {
		return ""
	}
	// 分隔符用空格而不是「」：实测本机字体渲染 U+00B7 是空心方框，
	// 这条规则由 TestNoMissingGlyphRunes 守着（写这一行时就被它抓了一次）。
	return strings.Join(parts, "  ")
}

// SetBest 更新最佳着法显示。
//
// 统计行按同类软件 TCHESS 的「思考细节」格式排版（见 thinkDetailLine）。
func (p *BestMovePanel) SetBest(chinese, uci string, redWinRate float64, scoreTxt string, depth int, nodes, nps, timeMS int64, hashPermille int, pvText string, pvChinese []string) {
	p.currentUCI = uci
	p.currentPV = pvText
	if chinese == "" {
		chinese = "—"
	}
	p.chinese.Text = chinese
	if uci == "" {
		p.uci.Text = "等待引擎输出"
	} else {
		p.uci.Text = "UCI 坐标：" + uci
	}
	if uci == "" {
		p.uciSmall.Text = ""
	} else {
		p.uciSmall.Text = "UCI 坐标 " + uci
	}
	p.uciSmall.Refresh()
	if scoreTxt != "" {
		p.winrate.Text = fmt.Sprintf("红方胜率 %.2f%%", redWinRate)
	} else {
		p.winrate.Text = "红方胜率 —"
	}
	p.stats.SetText(thinkDetailLine(depth, scoreTxt, nodes, nps, timeMS, hashPermille))
	if len(pvChinese) > 0 {
		n := len(pvChinese)
		if n > 5 {
			n = 5
		}
		p.pv.SetText("主变例：" + strings.Join(pvChinese[:n], " "))
	} else if pvText != "" {
		p.pv.SetText("主变例：" + pvText)
	} else {
		p.pv.SetText("主变例：—")
	}
	p.chinese.Refresh()
	p.uci.Refresh()
	p.winrate.Refresh()
	p.stats.Refresh()
}

// SetStatus 在最佳着法面板下方显示一行状态说明。
func (p *BestMovePanel) SetStatus(s string) {
	p.status.Text = s
	p.status.Refresh()
}

// ---------------------------------------------------------------------------
// 多候选着法列表（MultiPV）
// ---------------------------------------------------------------------------

// CandidateRow 是变招列表中的一行。
//
// 【v1.6.2】去掉了每行的概率进度条：一是同类软件（TCHESS 棋谱表 序号/着法/分数、
// 象棋巫师着法列表）都只用「分值/概率」这种文字列，条是多余装饰；
// 二是每条 20px 的条在 12 条候选时会让右栏最小高度多出 240px，
// 直接把窗口顶到屏幕外面去（实测过）。
type CandidateRow struct {
	Root    fyne.CanvasObject
	rank    *canvas.Text
	chinese *canvas.Text
	uci     *canvas.Text
	prob    *canvas.Text
	score   *canvas.Text

	// detail 是这一招之后的后续走法（默认收起，点这一行展开）。
	detail *widget.Label
	// meta 是这一条对应的引擎统计（深度 / 用时 / 速度 / 节点），同样默认收起。
	meta  *widget.Label
	tap   *tapArea
	open  bool
	hasPV bool // 引擎这次没给后续走法时，点开也不展开空行
}

func newCandidateRow() *CandidateRow {
	r := &CandidateRow{}
	r.rank = canvas.NewText("", colForeDim)
	r.rank.TextSize = textSize(textLabel)
	r.chinese = canvas.NewText("", colFore)
	r.chinese.TextSize = textSize(textLabel)
	r.chinese.TextStyle = fyne.TextStyle{Bold: true}
	r.uci = canvas.NewText("", colForeDim)
	r.uci.TextSize = textSize(textSmall)
	r.prob = canvas.NewText("", colPrimary)
	r.prob.TextSize = textSize(textLabel)
	r.prob.TextStyle = fyne.TextStyle{Bold: true}
	r.score = canvas.NewText("", colForeDim)
	r.score.TextSize = textSize(textSmall)

	// 一行放下：排名 | 中文着法 …… 分值 概率
	left := container.NewBorder(nil, nil, r.rank, nil, r.chinese)
	right := container.NewHBox(r.score, r.prob)
	line := container.NewBorder(nil, nil, nil, right, left)

	// 后续走法：**默认收起**（用户要求「后面的分析默认是关闭状态」），点这一行展开。
	r.detail = widget.NewLabel("")
	r.detail.Wrapping = fyne.TextWrapWord
	r.detail.Hide()

	r.tap = newTapArea(line, func() { r.toggleDetail() })
	// 引擎统计（深度/用时/速度/节点）也是一行小字，跟后续走法一起展开
	r.meta = widget.NewLabel("")
	r.meta.Wrapping = fyne.TextWrapWord
	r.meta.Hide()
	r.Root = container.NewVBox(r.tap, r.detail, r.meta)
	return r
}

// toggleDetail 展开 / 收起这一招之后的后续走法。
func (r *CandidateRow) toggleDetail() {
	r.open = !r.open
	show := r.open && r.hasPV
	if show {
		r.detail.Show()
		r.meta.Show()
	} else {
		r.detail.Hide()
		r.meta.Hide()
	}
	r.tap.SetHot(show)
	r.Root.Refresh()
}

// applySideColor 按「轮到谁走」给中文着法上色：红方走 → 红字，黑方走 → 正文色。
func (r *CandidateRow) applySideColor(redToMove bool) {
	if redToMove {
		r.chinese.Color = colMoveRed
	} else {
		r.chinese.Color = colFore
	}
	r.chinese.Refresh()
}

// set 刷新一行内容；pvText 是这一招之后的**全部**后续走法，metaText 是引擎统计。
func (r *CandidateRow) set(shown bool, rank int, chinese, uci string, prob float64, scoreText, pvText, metaText string) {
	if !shown {
		r.Root.Hide()
		return
	}
	r.rank.Text = fmt.Sprint(rank)
	r.chinese.Text = chinese
	r.uci.Text = uci
	r.prob.Text = fmt.Sprintf("%.2f%%", prob)
	r.score.Text = scoreText
	r.rank.Refresh()
	r.chinese.Refresh()
	r.uci.Refresh()
	r.prob.Refresh()
	r.score.Refresh()
	r.hasPV = pvText != ""
	if r.hasPV {
		r.detail.SetText("后续：" + pvText)
	} else {
		r.detail.SetText("后续：引擎本次没有给出更远的走法")
	}
	if metaText != "" {
		r.meta.SetText(metaText)
	} else {
		r.meta.SetText("（本次没有引擎统计）")
	}
	// 数据换了以后保持原来的展开/收起状态，但**没有后续内容时不展开**（空行没意义）
	if !r.open || !r.hasPV {
		r.detail.Hide()
		r.meta.Hide()
	} else {
		r.detail.Show()
		r.meta.Show()
	}
	r.Root.Show()
}

// CandidatePanel 是 MultiPV 候选着法列表。
type CandidatePanel struct {
	Root fyne.CanvasObject

	rows []*CandidateRow
	box  *fyne.Container // 真正挂载可见行的容器（见 SetCandidates 的说明）
	sum  *canvas.Text
	note *canvas.Text
	topN int

	// redToMove 记录当前轮到哪一方走：红方走时变招着法用红字，黑方走保持正文色。
	// 用户要求「该红棋走时文字变成红色，黑棋走时颜色不变」，这也是中文象棋
	// 棋谱的通行分色（象棋巫师、TCHESS 的棋谱表同样按方分色）。
	redToMove bool
}

// SetSideToMove 告知当前轮到哪一方走（只影响文字颜色）。
func (p *CandidatePanel) SetSideToMove(red bool) {
	if p.redToMove == red {
		return
	}
	p.redToMove = red
	for _, r := range p.rows {
		r.applySideColor(red)
	}
}

// NewCandidatePanel 创建候选列表（最多支持 12 条，与 MultiPV 滑块上限一致）。
func NewCandidatePanel() *CandidatePanel {
	p := &CandidatePanel{}
	p.box = container.NewVBox()
	for i := 0; i < 12; i++ {
		row := newCandidateRow()
		p.rows = append(p.rows, row)
	}
	p.sum = canvas.NewText("合计（暂无候选）", colForeDim)
	p.sum.TextSize = textSize(textSmall)
	p.note = canvas.NewText("", colForeDim)
	p.note.TextSize = textSize(textSmall)
	p.topN = 8

	head := container.NewHBox(sectionTitle("变招"), layoutSpacer())
	// 【v1.6】按用户要求「候选着法一次性显示全部，不要滚动查看」：
	// 去掉内部滚动容器，可见行全部平铺（行由 SetCandidates 按需挂载）。
	p.Root = container.NewBorder(container.NewVBox(head, p.sum, p.note), nil, nil, nil,
		scrollPadRight(p.box))
	return p
}

// SetTemperature 只用于在标题下显示当前温度说明。
func (p *CandidatePanel) SetTemperature(t int) {
	p.note.Text = "" // 温度是内部参数，按用户要求不展示
	p.note.Refresh()
}

// SetCandidates 用一批候选刷新列表。
//
//	chinese[i] 中文记谱, uci[i] UCI 坐标, prob[i] 概率百分比, score[i] 分值文本,
//	pv[i] 这一招之后的**全部**后续走法（中文）, meta[i] 这一条对应的引擎统计
//	（深度 / 用时 / 速度 / 节点，点开该行才显示）
func (p *CandidatePanel) SetCandidates(chinese, uci []string, prob []float64, score []string, pv, meta []string) {
	n := len(chinese)
	// 【v1.6.2】只把**真正要显示的**行挂进容器。
	//
	// 为什么不能像以前那样「预先建 12 行、多余的 Hide()」：
	// Fyne 的盒子在算 MinSize 时会把隐藏子项也算进去，于是 12 行候选的
	// 最小高度会顶进整窗的最小高度，把窗口撑到比屏幕还高（实测过）。
	// 改成按需 Add，最小高度就只跟可见行数走。
	p.box.RemoveAll()
	if n == 0 {
		p.sum.Text = "合计（暂无候选）"
		p.sum.Refresh()
		p.box.Refresh()
		return
	}
	sum := 0.0
	for i := 0; i < n && i < len(p.rows); i++ {
		r := p.rows[i]
		sum += prob[i]
		pvText := ""
		if i < len(pv) {
			pvText = pv[i]
		}
		metaText := ""
		if i < len(meta) {
			metaText = meta[i]
		}
		r.set(true, i+1, chinese[i], uci[i], prob[i], score[i], pvText, metaText)
		r.applySideColor(p.redToMove)
		p.box.Add(r.Root)
	}
	p.box.Refresh()
	p.sum.Text = fmt.Sprintf("共 %d 条候选（点任意一条看后续走法）", n)
	p.sum.Refresh()
}

// ---------------------------------------------------------------------------
// 参考引擎（Kibitzer）结果卡
// ---------------------------------------------------------------------------

// colKib 是参考引擎的标识色（与主引擎的 colPrimary 区分开，
// 照 Arena 的做法：引擎名用颜色区分谁是谁）。
var colKib = color.NRGBA{R: 0x2E, G: 0x5E, B: 0x8C, A: 0xFF}

// KibitzerPanel 显示参考引擎对**同一局面**的结论（上下堆叠在最佳着法下面）。
//
// 设计依据见 docs/多引擎对比-设计稿.md：设置三层、时间只做「同步每步时限」、
// 结果显示先做上下堆叠；引擎名带固定色，结论与主引擎不同时加「分歧」标记。
type KibitzerPanel struct {
	Root  fyne.CanvasObject
	head  *canvas.Text // 引擎名 + 状态
	line  *canvas.Text // 分数 / 深度 / 用时 / 速度
	pv    *widget.Label
	empty *canvas.Text
}

// NewKibitzerPanel 创建对比卡。
func NewKibitzerPanel() *KibitzerPanel {
	p := &KibitzerPanel{}
	p.head = canvas.NewText("参考引擎（未启用）", colKib)
	p.head.TextSize = textSize(textLabel)
	p.head.TextStyle = fyne.TextStyle{Bold: true}
	p.line = canvas.NewText("", colFore)
	p.line.TextSize = textSize(textSmall)
	p.pv = widget.NewLabel("")
	p.pv.Wrapping = fyne.TextWrapWord
	p.empty = canvas.NewText("在菜单「引擎」里选一个参考引擎，即可与主引擎同时分析同一局面。", colForeDim)
	p.empty.TextSize = textSize(textSmall)
	p.Root = container.NewVBox(p.head, p.line, p.pv, p.empty)
	return p
}

// SetIdle 显示一行状态（未启用 / 就绪 / 启动中 / 出错）。
//
// 【缺陷修复】原来只改下面那行提示，**卡头标题不动**——于是即使参考引擎已经就绪，
// 卡头还写着「参考引擎（未启用）」，用户看到的结论就是"没启用"（实测踩到）。
// 现在标题与提示一起给。
func (p *KibitzerPanel) SetIdle(head, msg string) {
	if head != "" {
		p.head.Text = head
		p.head.Refresh()
	}
	p.line.Text = ""
	p.line.Refresh()
	p.pv.SetText("")
	p.empty.Text = msg
	p.empty.Show()
	p.Root.Refresh()
}

// Set 填入一次分析结果。
func (p *KibitzerPanel) Set(name, scoreTxt string, depth int, timeMS, nps int64, pvText string, diverged bool) {
	p.head.Text = "参考引擎：已开启（" + name + "）"
	if diverged {
		p.head.Text += "　⚠ 与主引擎结论不同"
	}
	p.head.Color = colKib
	p.head.Refresh()
	p.line.Text = fmt.Sprintf("分数 %s   深度 %d 层   用时 %.1f 秒   速度 %s 千节点/秒",
		scoreTxt, depth, float64(timeMS)/1000, commas(nps/1000))
	p.line.Refresh()
	if pvText != "" {
		p.pv.SetText("主变例：" + pvText)
	} else {
		p.pv.SetText("主变例：—")
	}
	p.empty.Hide()
}

// ---------------------------------------------------------------------------
// 思考设置条（右栏常显：当前设置 + 实时计算深度）
// ---------------------------------------------------------------------------

// ThinkPanel 是右栏常显的一条「电脑思考设置 + 实时计算情况」。
//
// 用户的诉求是两条：
//  1. 不管是人机对弈还是引擎对战，都要能改电脑的**思考时间与计算层数**；
//  2. 改层数、看当前算到多深，都要能**在右侧栏直接看到**；位置不够就给个按钮点开调。
//
// 所以这条做成两行：第一行是当前设置（右边「调整…」点开改），
// 第二行是实时值（这次算到多深、用了多久、多快），不需要展开任何折叠。
type ThinkPanel struct {
	Root fyne.CanvasObject

	profile string // 这套设置属于哪个模式（分析模式 / 人机对弈）
	setting *canvas.Text
	live    *canvas.Text
	btn     *widget.Button

	onEdit func()
}

// NewThinkPanel 创建思考设置条。onEdit 为点「调整…」时的回调。
func NewThinkPanel(onEdit func()) *ThinkPanel {
	p := &ThinkPanel{onEdit: onEdit}
	p.setting = canvas.NewText("", colFore)
	p.setting.TextSize = textSize(textLabel)
	p.live = canvas.NewText("等待引擎输出…", colForeDim)
	p.live.TextSize = textSize(textSmall)

	// 【v1.6.7】用户要求「调整思考设置那个按键调大一点」：
	// 从 LowImportance（扁平小字）改成默认重要性，并加长文案。
	p.btn = widget.NewButton("调整思考设置…", func() {
		if p.onEdit != nil {
			p.onEdit()
		}
	})

	head := container.NewBorder(nil, nil, nil, p.btn, p.setting)
	p.Root = container.NewVBox(head, p.live)
	return p
}

// SetProfileName 标明这套思考设置属于哪个模式（v1.6.8：分析与人机各存一套）。
func (p *ThinkPanel) SetProfileName(name string) {
	p.profile = name
	p.setting.Refresh()
}

// SetConfig 刷新「当前设置」那一行。
func (p *ThinkPanel) SetConfig(mode string, moveMS, depth int) {
	prefix := "电脑思考"
	if p.profile != "" {
		prefix += "（" + p.profile + "）"
	}
	if mode == config.TimeModeDepth {
		p.setting.Text = fmt.Sprintf("%s：固定深度 %d 层", prefix, depth)
	} else {
		p.setting.Text = fmt.Sprintf("%s：每步限时 %.1f 秒（上限 %d 层）", prefix, float64(moveMS)/1000, depth)
	}
	p.setting.Refresh()
}

// SetLive 刷新实时行（这次算到多深、用了多久、多快）。
func (p *ThinkPanel) SetLive(depth int, timeMS, nps int64) {
	if depth <= 0 {
		p.live.Text = "等待引擎输出…"
	} else {
		p.live.Text = fmt.Sprintf("本次：深度 %d 层  用时 %.1f 秒  速度 %s 千节点/秒",
			depth, float64(timeMS)/1000, commas(nps/1000))
	}
	p.live.Refresh()
}

// SetIdle 引擎没在算（该局面没有缓存结果等）时恢复占位文字。
func (p *ThinkPanel) SetIdle(msg string) {
	p.live.Text = msg
	p.live.Refresh()
}

// ---------------------------------------------------------------------------
// 参数面板
// ---------------------------------------------------------------------------

// ParamPanel 是「引擎参数满配」面板。
//
// 固定参数（线程/哈希/深度/限时/MultiPV/温度/SyzygyPath）之外，
// 引擎通过 uci 上报的其余参数会被动态生成为控件（见 SetDynamicOptions）。
type ParamPanel struct {
	Root fyne.CanvasObject

	OnChanged func() // 任意参数变化后的回调（用于持久化与下发）

	Threads    *widget.Slider
	Hash       *widget.Slider
	Depth      *widget.Entry // 【v1.6.7】深度不设上限，用输入框而不是滑块
	MoveTime   *widget.Slider
	MultiPV    *widget.Slider
	Temp       *widget.Slider
	ModeSelect *widget.RadioGroup
	Syzygy     *widget.Entry

	threadsVal, hashVal, depthVal, moveVal, multiVal, tempVal *canvas.Text
	dynBox                                                    *fyne.Container
	dynRows                                                   []*dynOptionRow
	dynNote                                                   *canvas.Text

	suppress bool // 初始化期间屏蔽 OnChanged

	// 高级参数折叠区（本版新增）
	advanced    *fyne.Container
	btnAdvanced *widget.Button

	app *App
}

type dynOptionRow struct {
	opt     dynOption
	root    fyne.CanvasObject
	spin    *widget.Slider
	check   *widget.Check
	entry   *widget.Entry
	select_ *widget.Select
	label   *canvas.Text
}

// dynOption 是界面层对 engine.Option 的轻量封装（避免 ui 直接依赖引擎细节）。
type dynOption struct {
	Name    string
	Type    string
	Default string
	Min     int
	Max     int
	Vars    []string
}

func sliderRow(label string, s *widget.Slider, val *canvas.Text) fyne.CanvasObject {
	val.TextSize = textSize(textLabel)
	val.Color = colPrimary
	head := container.NewBorder(nil, nil, canvas.NewText(label, colFore), val)
	return container.NewVBox(head, s)
}

// entryRow 是「标签 + 数值 + 输入框」的一行（与 sliderRow 同款外观）。
//
// 【v1.6.7】新加：深度这类「不设上限」的参数只能用输入框，
// 否则滑块的最大值就等于偷偷替用户设了上限。
func entryRow(label string, e *widget.Entry, val *canvas.Text) fyne.CanvasObject {
	val.TextSize = textSize(textLabel)
	val.Color = colPrimary
	head := container.NewBorder(nil, nil, canvas.NewText(label, colFore), val)
	return container.NewVBox(head, e)
}

// NewParamPanel 创建参数面板。
func NewParamPanel(app *App) *ParamPanel {
	p := &ParamPanel{app: app}
	p.Threads = widget.NewSlider(1, 16)
	p.Threads.Step = 1
	p.Hash = widget.NewSlider(64, 8192)
	p.Hash.Step = 64
	// 【v1.6.7】深度改成输入框：用户要求「固定深度无上限」。
	// 滑块天生有上限（拖到底是几层就只能填几层），输入框才能真正不设限。
	p.Depth = widget.NewEntry()
	p.Depth.SetPlaceHolder("层数，例如 60")
	p.Depth.Validator = func(sv string) error {
		if sv == "" {
			return nil
		}
		n, err := strconv.Atoi(sv)
		if err != nil || n < 1 {
			return fmt.Errorf("请填 1 以上的整数层数")
		}
		return nil
	}
	p.MoveTime = widget.NewSlider(0.1, 60)
	p.MoveTime.Step = 0.1
	p.MultiPV = widget.NewSlider(1, 12)
	p.MultiPV.Step = 1
	p.Temp = widget.NewSlider(40, 300)
	p.Temp.Step = 5

	p.threadsVal = canvas.NewText("", colPrimary)
	p.hashVal = canvas.NewText("", colPrimary)
	p.depthVal = canvas.NewText("", colPrimary)
	p.moveVal = canvas.NewText("", colPrimary)
	p.multiVal = canvas.NewText("", colPrimary)
	p.tempVal = canvas.NewText("", colPrimary)

	p.ModeSelect = widget.NewRadioGroup([]string{"限时优先", "深度优先"}, func(string) { p.changed() })
	p.ModeSelect.Horizontal = true
	p.Syzygy = widget.NewEntry()
	// 占位文本保持简短：Fyne 的 Entry 在无内容时会用占位文本计算 MinSize，
	// 过长的占位会把参数面板顶宽，导致贴右对齐的数值被窗口右缘切掉。
	p.Syzygy.SetPlaceHolder(`残局表目录，如 D:\syzygy（留空=不用）`)

	wire := func(s *widget.Slider, val *canvas.Text, fmtFn func(float64) string) {
		s.OnChanged = func(v float64) {
			val.Text = fmtFn(v)
			val.Refresh()
			p.changed()
		}
	}
	wire(p.Threads, p.threadsVal, func(v float64) string { return fmt.Sprintf("%.0f 线程", v) })
	wire(p.Hash, p.hashVal, func(v float64) string { return fmt.Sprintf("%.0f MB", v) })
	// 深度输入框：只认整数，合法就立刻生效
	p.Depth.OnChanged = func(sv string) {
		if n, err := strconv.Atoi(strings.TrimSpace(sv)); err == nil && n >= 1 {
			p.depthVal.Text = fmt.Sprintf("%d 层", n)
			p.depthVal.Refresh()
			p.changed()
		}
	}
	wire(p.MoveTime, p.moveVal, func(v float64) string { return fmt.Sprintf("%.1f 秒", v) })
	wire(p.MultiPV, p.multiVal, func(v float64) string { return fmt.Sprintf("%.0f 条", v) })
	wire(p.Temp, p.tempVal, func(v float64) string { return fmt.Sprintf("%.0f", v) })

	p.Syzygy.OnChanged = func(string) { p.changed() }

	p.dynBox = container.NewVBox()
	p.dynNote = canvas.NewText("", colForeDim)
	p.dynNote.TextSize = textSize(textSmall)
	p.dynNote.Text = "引擎连接后自动上报的 UCI 参数"

	// 【本版改动】高级参数折叠：不删除，默认收起。
	// 常看的只留「线程 / 哈希 / 深度 / 思考时间」，其余（思考模式、MultiPV、
	// 温度 T、SyzygyPath、引擎动态参数）收进折叠区，点标题展开。
	//
	// 用词按同类软件（TCHESS 的右栏就是「引擎 + 线程 + 哈希 + MB」）：
	// 线程 / 哈希(MB) / 搜索深度 / 思考时间 是象棋引擎界面的通行叫法，
	// 上一版换成「同时用几个 CPU 核心」这类口语反而不专业，已改回。
	advanced := container.NewVBox(
		container.NewVBox(canvas.NewText("思考模式（切换后下一步生效）", colFore), p.ModeSelect),
		sliderRow("候选变招数（MultiPV）", p.MultiPV, p.multiVal),
		sliderRow("概率温度 T（只影响概率显示）", p.Temp, p.tempVal),
		container.NewVBox(canvas.NewText("残局库路径（SyzygyPath，可留空）", colFore), p.Syzygy),
		widget.NewSeparator(),
		sectionTitle("引擎动态参数（由引擎上报）"),
		p.dynNote,
		p.dynBox,
	)
	advanced.Hide()
	p.advanced = advanced
	p.btnAdvanced = widget.NewButton("显示高级参数 ▾", func() {
		if advanced.Visible() {
			advanced.Hide()
			p.btnAdvanced.SetText("显示高级参数 ▾")
		} else {
			advanced.Show()
			p.btnAdvanced.SetText("收起高级参数 ▴")
		}
	})
	p.btnAdvanced.Importance = widget.LowImportance

	body := container.NewVBox(
		sliderRow("线程", p.Threads, p.threadsVal),
		sliderRow("哈希（MB）", p.Hash, p.hashVal),
		entryRow("搜索深度（层数，可填任意整数）", p.Depth, p.depthVal),
		sliderRow("思考时间（秒）", p.MoveTime, p.moveVal),
		p.btnAdvanced,
		advanced,
	)
	scroll := container.NewVScroll(scrollPadRight(body))
	scroll.SetMinSize(fyne.NewSize(230, 140))
	p.Root = scroll
	return p
}

func (p *ParamPanel) changed() {
	// SetValues 初始化期间必须屏蔽回调：否则第一个滑块赋值就会触发 OnChanged，
	// 而那时其余滑块还停在最小值上，会把 config →正确参数覆盖成最小值。
	if p.suppress {
		return
	}
	if p.OnChanged != nil {
		p.OnChanged()
	}
}

// SetValues 用配置填充控件（初始化时调用，不触发回调）。
func (p *ParamPanel) SetValues(threads, hash, depth, moveMS, multiPV, temp int, depthMode bool, syzygy string) {
	p.suppress = true
	defer func() { p.suppress = false }()
	p.Threads.Value = float64(threads)
	p.Hash.Value = float64(hash)
	p.Depth.SetText(fmt.Sprintf("%d", depth))
	p.MoveTime.Value = float64(moveMS) / 1000.0
	p.MultiPV.Value = float64(multiPV)
	p.Temp.Value = float64(temp)
	if depthMode {
		p.ModeSelect.SetSelected("深度优先")
	} else {
		p.ModeSelect.SetSelected("限时优先")
	}
	p.Syzygy.SetText(syzygy)
	p.threadsVal.Text = fmt.Sprintf("%d 线程", threads)
	p.hashVal.Text = fmt.Sprintf("%d MB", hash)
	p.depthVal.Text = fmt.Sprintf("%d 层", depth)
	p.moveVal.Text = fmt.Sprintf("%.1f 秒", float64(moveMS)/1000)
	p.multiVal.Text = fmt.Sprintf("%d 条", multiPV)
	p.tempVal.Text = fmt.Sprintf("T = %d", temp)
	p.Threads.Refresh()
	p.Hash.Refresh()
	p.Depth.Refresh()
	p.MoveTime.Refresh()
	p.MultiPV.Refresh()
	p.Temp.Refresh()
}

// Values 读出控件当前值。
func (p *ParamPanel) Values() (threads, hash, depth, moveMS, multiPV, temp int, depthMode bool, syzygy string) {
	depthN := 1
	if n, err := strconv.Atoi(strings.TrimSpace(p.Depth.Text)); err == nil && n >= 1 {
		depthN = n
	}
	return int(p.Threads.Value + 0.5), int(p.Hash.Value + 0.5), depthN,
		int(p.MoveTime.Value*1000 + 0.5), int(p.MultiPV.Value + 0.5), int(p.Temp.Value + 0.5),
		p.ModeSelect.Selected == "深度优先", p.Syzygy.Text
}

// SetDynamicOptions 根据引擎暴露的参数动态生成控件。
//
// 这就是「参数满配」的实现方式：引擎暴露什么参数，界面就显示什么参数，
// 不存在硬编码清单。已固定展示的常用参数会被跳过，避免重复。
func (p *ParamPanel) SetDynamicOptions(opts []dynOption, get func(name string) string, set func(name, value string)) {
	p.dynBox.RemoveAll()
	p.dynRows = nil
	skip := map[string]bool{
		"threads": true, "hash": true, "multipv": true, "syzygypath": true,
		"depth": true, "movetime": true, "temperature": true,
	}
	count := 0
	for i := range opts {
		o := opts[i]
		if skip[strings.ToLower(o.Name)] {
			continue
		}
		row := &dynOptionRow{opt: o}
		val := get(o.Name)
		if val == "" {
			val = o.Default
		}
		switch strings.ToLower(o.Type) {
		case "spin":
			lo, hi := float64(o.Min), float64(o.Max)
			if hi <= lo {
				hi = lo + 100
			}
			row.spin = widget.NewSlider(lo, hi)
			row.spin.Step = 1
			if f, err := parseFloat(val); err == nil {
				if f < lo {
					f = lo
				}
				if f > hi {
					f = hi
				}
				row.spin.Value = f
			}
			row.label = canvas.NewText(fmt.Sprintf("%s", o.Name), colFore)
			row.label.TextSize = textSize(textLabel)
			cur := canvas.NewText(fmt.Sprint(int(row.spin.Value)), colPrimary)
			cur.TextSize = textSize(textLabel)
			name := o.Name
			row.spin.OnChanged = func(v float64) {
				cur.Text = fmt.Sprint(int(v + 0.5))
				cur.Refresh()
				set(name, fmt.Sprint(int(v+0.5)))
			}
			head := container.NewBorder(nil, nil, row.label, cur)
			row.root = container.NewVBox(head, row.spin)
		case "check":
			row.check = widget.NewCheck(o.Name, func(b bool) {
				set(o.Name, fmt.Sprint(b))
			})
			row.check.SetChecked(strings.EqualFold(val, "true"))
			row.root = row.check
		case "button":
			// button 型参数没有值，点一下就是下发一次 setoption
			pname := o.Name
			row.root = widget.NewButton(o.Name, func() { set(pname, "") })
		case "combo":
			row.select_ = widget.NewSelect(o.Vars, func(s string) { set(o.Name, s) })
			if val != "" {
				row.select_.SetSelected(val)
			}
			row.label = canvas.NewText(o.Name, colFore)
			row.label.TextSize = textSize(textLabel)
			row.root = container.NewVBox(row.label, row.select_)
		default:
			row.entry = widget.NewEntry()
			row.entry.SetText(val)
			row.entry.OnChanged = func(s string) { set(o.Name, s) }
			row.label = canvas.NewText(o.Name, colFore)
			row.label.TextSize = textSize(textLabel)
			row.root = container.NewVBox(row.label, row.entry)
		}
		p.dynBox.Add(row.root)
		p.dynRows = append(p.dynRows, row)
		count++
	}
	if count == 0 {
		p.dynNote.Text = "引擎参数：该引擎未上报额外参数"
	} else {
		p.dynNote.Text = fmt.Sprintf("引擎参数：自动发现 %d 项，改动立即 setoption 下发", count)
	}
	p.dynNote.Refresh()
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
