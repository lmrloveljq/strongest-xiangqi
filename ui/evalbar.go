package ui

import (
	"fmt"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// EvalBar 是「评估条」：一条横向长条，红色部分 = 红方胜率占比。
//
// 参照同类软件的通行做法（swiftxiangqi 的分析盘有 eval bar，TCHESS 有状态栏细节），
// 它的价值是**一眼看出优劣**——看棋谱跳转时，数值要读半天，长条一眼就够。
type EvalBar struct {
	widget.BaseWidget

	mu       sync.Mutex
	red      float64 // 红方胜率 0~100
	valid    bool
	scoreTxt string // 分值文本，例如 "+58" / "-1.2" / "将死 3"
}

// NewEvalBar 创建评估条。
func NewEvalBar() *EvalBar {
	b := &EvalBar{red: 50}
	b.ExtendBaseWidget(b)
	return b
}

// Set 更新评估条。valid=false 时显示为「未分析」的中性状态。
func (b *EvalBar) Set(redWinRate float64, valid bool, scoreText string) {
	b.mu.Lock()
	b.red = redWinRate
	b.valid = valid
	b.scoreTxt = scoreText
	b.mu.Unlock()
	b.Refresh()
}

// CreateRenderer 构建渲染器。
func (b *EvalBar) CreateRenderer() fyne.WidgetRenderer {
	r := &evalBarRenderer{b: b}
	r.bgRed = canvas.NewRectangle(colEvalRed)
	r.bgBlack = canvas.NewRectangle(colEvalBlack)
	r.frame = canvas.NewRectangle(color.Transparent)
	r.frame.StrokeColor = colCardLine
	r.frame.StrokeWidth = 1
	r.frame.CornerRadius = 6
	r.bgRed.CornerRadius = 6
	r.bgBlack.CornerRadius = 6
	r.text = canvas.NewText("", colPrimaryFg)
	r.text.TextSize = textSize(textLabel)
	r.text.TextStyle = fyne.TextStyle{Bold: true}
	r.objects = []fyne.CanvasObject{r.bgBlack, r.bgRed, r.frame, r.text}
	return r
}

// MinSize 给一个紧凑的高度（【v1.6.1】26 → 18：右栏对半切后每一栏都变窄，
// 局势条这种「一条就够」的面板没必要再占一行高度）。
func (b *EvalBar) MinSize() fyne.Size { return fyne.NewSize(sz(110), sz(18)) }

type evalBarRenderer struct {
	b       *EvalBar
	bgRed   *canvas.Rectangle
	bgBlack *canvas.Rectangle
	frame   *canvas.Rectangle
	text    *canvas.Text
	objects []fyne.CanvasObject
}

func (r *evalBarRenderer) Layout(size fyne.Size) {
	r.b.mu.Lock()
	red := r.b.red
	valid := r.b.valid
	score := r.b.scoreTxt
	r.b.mu.Unlock()

	if !valid {
		red = 50
	}
	if red < 0 {
		red = 0
	}
	if red > 100 {
		red = 100
	}
	// 底色整条 = 黑方占比，红色覆盖左侧 = 红方占比，中间留 1px 分界
	r.bgBlack.Move(fyne.NewPos(0, 0))
	r.bgBlack.Resize(size)
	w := float32(float64(size.Width) * red / 100.0)
	r.bgRed.Move(fyne.NewPos(0, 0))
	r.bgRed.Resize(fyne.NewSize(w, size.Height))

	r.frame.Move(fyne.NewPos(0, 0))
	r.frame.Resize(size)

	txt := evalBarText(red, valid, score)
	r.text.Text = txt
	ts := fyne.MeasureText(txt, r.text.TextSize, r.text.TextStyle)
	r.text.Resize(ts)
	r.text.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2))
	r.text.Show()
	r.text.Refresh()
}

func (r *evalBarRenderer) MinSize() fyne.Size { return r.b.MinSize() }

// evalBarText 拼评估条上的文字。
//
// 【v1.6.3】胜率精确到两位小数（用户要求）：
// 「红 51.56%  :  48.44% 黑  -17」。抽成函数是为了能被测试直接断言格式。
func evalBarText(red float64, valid bool, score string) string {
	if !valid {
		return "未分析"
	}
	txt := fmt.Sprintf("红 %.2f%%  :  %.2f%% 黑", red, 100-red)
	if score != "" {
		txt += "    " + score
	}
	return txt
}

func (r *evalBarRenderer) Refresh() {
	r.Layout(r.b.Size())
	canvas.Refresh(r.b)
}

func (r *evalBarRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *evalBarRenderer) Destroy() {}
