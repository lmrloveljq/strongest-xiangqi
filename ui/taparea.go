package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// tapArea 把任意内容包成「整块可点」的控件，并在指针悬停时给一层浅底，
// 让用户看得出这里能点。
//
// 为什么要自己写：本工程有几处「点一行/一块」的需求——变招行（点开看后续走法）、
// 记谱格（点着法跳转）——而 Fyne 的 widget.Button 只支持纯文字标签、
// 颜色也固定取自主题（变招行要按红黑分色，按钮做不到）。
type tapArea struct {
	widget.BaseWidget

	content fyne.CanvasObject
	OnTap   func()
	hot     bool
	hover   bool
}

func newTapArea(content fyne.CanvasObject, onTap func()) *tapArea {
	t := &tapArea{content: content, OnTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

// SetHot 设置「当前选中/展开」的常亮底色（变招行展开时用）。
func (t *tapArea) SetHot(on bool) {
	if t.hot == on {
		return
	}
	t.hot = on
	t.Refresh()
}

// Tapped 实现 fyne.Tappable。
func (t *tapArea) Tapped(*fyne.PointEvent) {
	if t.OnTap != nil {
		t.OnTap()
	}
}

// MouseIn / MouseOut 实现悬停反馈。
func (t *tapArea) MouseIn(*fyne.PointEvent) { t.hover = true; t.Refresh() }
func (t *tapArea) MouseOut()                { t.hover = false; t.Refresh() }

func (t *tapArea) CreateRenderer() fyne.WidgetRenderer {
	return &tapAreaRenderer{t: t, bg: canvas.NewRectangle(color.Transparent)}
}

type tapAreaRenderer struct {
	t  *tapArea
	bg *canvas.Rectangle
}

func (r *tapAreaRenderer) Layout(size fyne.Size) {
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)
	r.t.content.Move(fyne.NewPos(0, 0))
	r.t.content.Resize(size)
}

func (r *tapAreaRenderer) MinSize() fyne.Size { return r.t.content.MinSize() }

func (r *tapAreaRenderer) Refresh() {
	switch {
	case r.t.hot:
		r.bg.FillColor = colSel
	case r.t.hover:
		r.bg.FillColor = colRowAlt
	default:
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
	r.t.content.Refresh()
}

func (r *tapAreaRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.t.content}
}

func (r *tapAreaRenderer) Destroy() {}
