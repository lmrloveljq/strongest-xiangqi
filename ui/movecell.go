package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"xiangqi/rules"
)

// moveCell 是着法列表里的一格「可点的着法」。
//
// 为什么不用 widget.Button：中文象棋棋谱的通行做法是**红方着法用红字、黑方着法用正文色**
// （象棋巫师、TCHESS 的棋谱表都这样分色），而 Fyne 的按钮文字颜色固定取自主题，
// 没法一格一个颜色。所以这里用 canvas.Text + Tappable 自己做一个最小控件，
// 顺带把「当前查看的那一步」做成浅底高亮。
type moveCell struct {
	widget.BaseWidget

	text  *canvas.Text
	side  int
	hot   bool
	OnTap func()
}

func newMoveCell(side int) *moveCell {
	c := &moveCell{side: side, text: canvas.NewText("—", moveTextColor(side))}
	c.text.TextSize = textSize(textLabel)
	c.ExtendBaseWidget(c)
	return c
}

// moveTextColor 返回某一方着法的文字颜色（红方红字、黑方正文色）。
func moveTextColor(side int) color.Color {
	if side == rules.Red {
		return colMoveRed
	}
	return colFore
}

func (c *moveCell) SetText(s string) {
	c.text.Text = s
	c.text.Refresh()
}

// SetHighlight 切换「当前查看的那一步」的浅底高亮。
func (c *moveCell) SetHighlight(on bool) {
	if c.hot == on {
		return
	}
	c.hot = on
	c.Refresh()
}

// Tapped 实现 fyne.Tappable：点一下跳到这一步。
func (c *moveCell) Tapped(*fyne.PointEvent) {
	if c.OnTap != nil {
		c.OnTap()
	}
}

// CreateRenderer 构建渲染器。
func (c *moveCell) CreateRenderer() fyne.WidgetRenderer {
	r := &moveCellRenderer{c: c, bg: canvas.NewRectangle(color.Transparent)}
	r.bg.CornerRadius = 4
	return r
}

type moveCellRenderer struct {
	c  *moveCell
	bg *canvas.Rectangle
}

func (r *moveCellRenderer) Layout(size fyne.Size) {
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)
	ts := r.c.text.MinSize()
	r.c.text.Resize(fyne.NewSize(size.Width, ts.Height))
	r.c.text.Move(fyne.NewPos(sz(4), (size.Height-ts.Height)/2))
}

func (r *moveCellRenderer) MinSize() fyne.Size {
	return r.c.text.MinSize().Add(fyne.NewSize(sz(10), sz(6)))
}

func (r *moveCellRenderer) Refresh() {
	if r.c.hot {
		r.bg.FillColor = colSel
	} else {
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
	// 红黑分色：高亮时只加粗、不改色，红方着法始终是红的
	r.c.text.Color = moveTextColor(r.c.side)
	r.c.text.TextStyle = fyne.TextStyle{Bold: r.c.hot}
	r.c.text.Refresh()
}

func (r *moveCellRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.c.text}
}

func (r *moveCellRenderer) Destroy() {}
