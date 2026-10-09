package ui

import (
	"image"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// CurvePoint 是胜率曲线上的一个数据点。
type CurvePoint struct {
	Ply int     // 第几步（0 = 开始局面）
	Red float64 // 红方胜率 0~100
}

// Curve 是「红黑双线」胜率曲线控件（Canvas 自绘）。
//
// X 轴 = 步数，Y 轴 = 胜率 0~100%：
//   - 红线 = 红方胜率
//   - v1.5 起按用户要求**只画红线**（黑方胜率是 100-红方，属于同一信息的镜像）
//
// 引擎思考中会显示一个预览点（空心圆），重开或清空后曲线归零。
type Curve struct {
	widget.BaseWidget

	mu         sync.Mutex
	pts        []CurvePoint
	preview    float64
	hasPreview bool
	markerPly  int
	hasMarker  bool
	title      string
	minH       float32 // 最小高度（0 = 用默认 sz(88)），由调用方按版面需要设置
	ver        uint64  // 数据版本号：渲染器据此决定是否需要重新光栅化位图

	rend *curveRenderer
}

// bumpLocked 递增数据版本号（调用方需持有 c.mu）。
func (c *Curve) bumpLocked() { c.ver++ }

// NewCurve 创建曲线控件。
func NewCurve(title string) *Curve {
	c := &Curve{title: title, markerPly: -1}
	c.ExtendBaseWidget(c)
	return c
}

// SetPoints 整体替换曲线数据。
func (c *Curve) SetPoints(pts []CurvePoint) {
	c.mu.Lock()
	c.pts = append([]CurvePoint(nil), pts...)
	c.bumpLocked()
	c.mu.Unlock()
	c.Refresh()
}

// AddPoint 追加一个数据点（同一步数重复添加会覆盖）。
func (c *Curve) AddPoint(ply int, red float64) {
	if math.IsNaN(red) {
		return
	}
	if red < 0 {
		red = 0
	}
	if red > 100 {
		red = 100
	}
	c.mu.Lock()
	// 【缺陷修复】把「上一个点 → 当前点」之间的着数按上一个值补齐：
	// 引擎关掉/没分析的那些着数原本一个点都没有，X 轴靠近原点的一半就整片空白
	//（用户实测：前十几步均势时曲线什么都不显示，看着像"偏右"）。
	// 补齐用的是**上一个已知值**（信息没变化就保持水平线），不是编造新数据。
	if len(c.pts) > 0 {
		last := c.pts[len(c.pts)-1]
		for p := last.Ply + 1; p < ply; p++ {
			c.pts = append(c.pts, CurvePoint{Ply: p, Red: last.Red})
		}
	} else if ply > 0 {
		// 第一个点本身就落在中后盘（例如粘贴一段着法序列后只分析了最新局面）时，
		// 从开局起用这个值铺一条水平线，否则 X 轴靠近原点的一半永远是空的。
		for p := 0; p < ply; p++ {
			c.pts = append(c.pts, CurvePoint{Ply: p, Red: red})
		}
	}
	replaced := false
	for i := range c.pts {
		if c.pts[i].Ply == ply {
			c.pts[i].Red = red
			replaced = true
			break
		}
	}
	if !replaced {
		c.pts = append(c.pts, CurvePoint{Ply: ply, Red: red})
	}
	c.hasPreview = false
	c.bumpLocked()
	c.mu.Unlock()
	c.Refresh()
}

// SetPreview 设置引擎思考中的预览点。
func (c *Curve) SetPreview(red float64, has bool) {
	c.mu.Lock()
	c.preview, c.hasPreview = red, has
	c.bumpLocked()
	c.mu.Unlock()
	c.Refresh()
}

// SetMarker 设置「当前查看步数」的竖线标记（点击着法列表时使用）。
func (c *Curve) SetMarker(ply int, has bool) {
	c.mu.Lock()
	c.markerPly, c.hasMarker = ply, has
	c.bumpLocked()
	c.mu.Unlock()
	c.Refresh()
}

// Clear 清空曲线。
func (c *Curve) Clear() {
	c.mu.Lock()
	c.pts = nil
	c.hasPreview = false
	c.hasMarker = false
	c.bumpLocked()
	c.mu.Unlock()
	c.Refresh()
}

// 数据版本号：上面每个修改数据的方法都会调用 bumpLocked，
// 渲染器据此判断是否需要重新光栅化位图（否则每次界面重绘都要重画整张图）。
func (c *Curve) version() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ver
}

// Points 返回曲线数据副本。
func (c *Curve) Points() []CurvePoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]CurvePoint(nil), c.pts...)
}

// SetMinHeight 设置最小高度（0 表示回到默认值）。
//
// 【v1.6.2】用户在「局势图太小」上提了两轮：走势图是横着看的图，
// 给它一条全宽的横带（右栏底部）比塞进半栏里更能看清趋势。
// 高度由调用方按版面给（分析模式给 150，对战模式仍按默认）。
func (c *Curve) SetMinHeight(h float32) {
	c.minH = h
	c.Refresh()
}

// MinSize 曲线最小高度。
func (c *Curve) MinSize() fyne.Size {
	h := c.minH
	if h <= 0 {
		h = sz(88)
	}
	return fyne.NewSize(sz(200), h)
}

// CreateRenderer 构建渲染器。
func (c *Curve) CreateRenderer() fyne.WidgetRenderer {
	r := newCurveRenderer(c)
	c.rend = r
	return r
}

// ---------------------------------------------------------------------------

type curveRenderer struct {
	c      *Curve
	raster *canvas.Raster

	title  *canvas.Text
	legend [2]*canvas.Text
	ylab   [5]*canvas.Text
	xlab   []*canvas.Text
	empty  *canvas.Text

	img        *image.RGBA
	imgW, imgH int
	cachedVer  uint64
	cacheInit  bool
	objects    []fyne.CanvasObject
}

const curveXLabelPool = 14

func newCurveRenderer(c *Curve) *curveRenderer {
	r := &curveRenderer{c: c}
	r.raster = canvas.NewRaster(r.generate)
	r.title = canvas.NewText(c.title, colFore)
	r.title.TextSize = textSize(textLabel)
	r.title.TextStyle = fyne.TextStyle{Bold: true}
	// 图例：按同类软件 TCHESS 的做法**不显示**
	// （`lineChart.setLegendVisible(false)`）。面板标题已经写着「局势图」，
	// 一条线也不需要图例说明是红是黑。
	r.legend[0] = canvas.NewText("", colRedLine)
	// 用户要求：只用一条线看走势（100-红方胜率是同一信息的镜像，纯属干扰）
	r.legend[1] = canvas.NewText("", colBlackLine)
	for _, t := range r.legend {
		t.TextSize = textSize(textSmall)
	}
	for i := range r.ylab {
		r.ylab[i] = canvas.NewText("", colForeDim)
		r.ylab[i].TextSize = textSize(textSmall)
	}
	for i := 0; i < curveXLabelPool; i++ {
		t := canvas.NewText("", colForeDim)
		t.TextSize = textSize(textSmall)
		r.xlab = append(r.xlab, t)
	}
	r.empty = canvas.NewText("暂无数据：走一步棋或开始分析后显示胜率曲线", colForeDim)
	r.empty.TextSize = textSize(textSmall)

	r.objects = append(r.objects, r.raster, r.title, r.legend[0], r.legend[1])
	for i := range r.ylab {
		r.objects = append(r.objects, r.ylab[i])
	}
	for _, t := range r.xlab {
		r.objects = append(r.objects, t)
	}
	r.objects = append(r.objects, r.empty)
	return r
}

var (
	colRedLine   = toNRGBA(DefaultBoardSkin.RedEdge)
	colBlackLine = toNRGBA(DefaultBoardSkin.BlackEdge)
)

// 边距（逻辑像素）
const (
	curveML = 38.0
	curveMR = 10.0
	curveMT = 22.0
	curveMB = 20.0
)

func (r *curveRenderer) generate(w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	c := r.c
	// 位图缓存：尺寸与数据版本都没变时直接复用，避免每次界面重绘都重画整张曲线图
	if ver := c.version(); r.cacheInit && r.img != nil && r.imgW == w && r.imgH == h && r.cachedVer == ver {
		return r.img
	}
	size := c.Size()
	lw, lh := float64(size.Width), float64(size.Height)
	if lw < 1 || lh < 1 {
		lw, lh = float64(w), float64(h)
	}
	sx, sy := float64(w)/lw, float64(h)/lh

	c.mu.Lock()
	pts := append([]CurvePoint(nil), c.pts...)
	preview, hasPrev := c.preview, c.hasPreview
	marker, hasMarker := c.markerPly, c.hasMarker
	c.mu.Unlock()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := toNRGBA(colPanelBG)
	grid := toNRGBA(colSep)
	mid := toNRGBA(colForeDim)
	fillRoundRect(img, 0, 0, float64(w), float64(h), 6*sx, bg)

	x0 := curveML * sx
	x1 := float64(w) - curveMR*sx
	y0 := curveMT * sy
	y1 := float64(h) - curveMB*sy
	if x1 <= x0+4 || y1 <= y0+4 {
		return img
	}
	plotW, plotH := x1-x0, y1-y0

	mapX := func(ply int, maxPly int) float64 {
		if maxPly <= 0 {
			return x0
		}
		return x0 + float64(ply)/float64(maxPly)*plotW
	}
	mapY := func(wr float64) float64 { return y1 - wr/100.0*plotH }

	// 横向网格线（0/25/50/75/100）
	lwid := math.Max(1, sx)
	for i := 0; i <= 4; i++ {
		wr := float64(i) * 25
		yy := mapY(wr)
		if i == 2 {
			// 50% 中线压成网格灰、并变细：开局双方胜率就在 50% 附近，
			// 中线太抢眼会让数据线「糊」在中线上看不见（用户反馈的问题）。
			drawLine(img, x0, yy, x1, yy, lwid*0.7, grid)
		} else {
			drawLine(img, x0, yy, x1, yy, lwid, grid)
		}
	}
	drawLine(img, x0, y0, x0, y1, lwid, grid)
	drawLine(img, x0, y1, x1, y1, lwid, grid)

	// 最大步数：至少 20，按实际着数扩展
	maxPly := 20
	if len(pts) > 0 {
		if last := pts[len(pts)-1].Ply; last > maxPly {
			maxPly = last
		}
	}
	if hasPrev && marker > maxPly {
		maxPly = marker
	}

	// 纵向网格线：**不画**。
	// 依据同类软件 TCHESS 的局势图配置（Controller.initLineChart：
	// `lineChart.setVerticalGridLinesVisible(false)`、`setLegendVisible(false)`、
	// `setCreateSymbols(false)`）——纵向网格与图例对读走势没有帮助，只增加噪声。

	// 当前查看步数的竖线标记（这条是功能性的：告诉你现在看的是第几步）
	if hasMarker && marker >= 0 {
		xx := mapX(marker, maxPly)
		drawLine(img, xx, y0, xx, y1, lwid*1.5, mid)
	}

	// 走势线：**只画线，不画采样点**
	// （TCHESS `setCreateSymbols(false)`；点在密集时会糊成一条粗线，反而看不出走势）
	lwLine := math.Max(1.4, 1.8*sx)
	if len(pts) >= 2 {
		for i := 1; i < len(pts); i++ {
			xa, ya := mapX(pts[i-1].Ply, maxPly), mapY(pts[i-1].Red)
			xb, yb := mapX(pts[i].Ply, maxPly), mapY(pts[i].Red)
			drawLine(img, xa, ya, xb, yb, lwLine, colRedLine)
		}
	} else if len(pts) == 1 {
		// 只有一个点时画**一个**圆点。
		// 原来这里红黑各画一个点（100-红 的镜像），用户看到的就是「两个点」，
		// 既看不出走势也分不清哪条是哪条。
		fillCircle(img, mapX(pts[0].Ply, maxPly), mapY(pts[0].Red), lwLine*1.8, colRedLine)
	}

	// 引擎思考中的预览点（空心圆，同样只画一个）
	if hasPrev {
		px := mapX(maxPly, maxPly)
		if len(pts) > 0 {
			px = mapX(pts[len(pts)-1].Ply+1, maxPly)
		}
		strokeCircle(img, px, mapY(preview), lwLine*2.2, lwLine, colRedLine)
	}

	r.img, r.imgW, r.imgH = img, w, h
	r.cachedVer, r.cacheInit = c.version(), true
	return img
}

func (r *curveRenderer) Layout(size fyne.Size) {
	r.raster.Move(fyne.NewPos(0, 0))
	r.raster.Resize(size)

	w := float64(size.Width)
	// 标题：必须**避开左侧刻度列**。
	// 原来放在 x=8，而最上面的「100」刻度在 x=4 起，标题一长就压在刻度上
	// （用户看到的「标题和刻度糊在一起」）。左侧刻度列宽约 26，标题从 30 起。
	ts := fyne.MeasureText(r.title.Text, r.title.TextSize, r.title.TextStyle)
	r.title.Resize(ts)
	r.title.Move(fyne.NewPos(30, 2))

	// 图例：右对齐（原来硬编码 -160/-92，文字一改就错位或者压到边上）
	legendX := float32(w) - 8
	for i := len(r.legend) - 1; i >= 0; i-- {
		lt := r.legend[i]
		s := fyne.MeasureText(lt.Text, lt.TextSize, lt.TextStyle)
		lt.Resize(s)
		if lt.Text == "" {
			continue
		}
		legendX -= s.Width
		lt.Move(fyne.NewPos(legendX, 3))
		legendX -= 10
	}

	// Y 轴刻度
	h := float64(size.Height)
	y0, y1 := curveMT, h-curveMB
	for i := 0; i <= 4; i++ {
		wr := float64(i) * 25
		yy := y1 - wr/100.0*(y1-y0)
		t := r.ylab[4-i]
		s := fyne.MeasureText(t.Text, t.TextSize, t.TextStyle)
		t.Resize(s)
		t.Move(fyne.NewPos(4, float32(yy-float64(s.Height)/2)))
	}

	// X 轴刻度
	r.c.mu.Lock()
	pts := r.c.pts
	r.c.mu.Unlock()
	maxPly := 20
	if len(pts) > 0 && pts[len(pts)-1].Ply > maxPly {
		maxPly = pts[len(pts)-1].Ply
	}
	step := 10
	for maxPly/step > 8 {
		step *= 2
	}
	idx := 0
	x0 := curveML
	x1 := w - curveMR
	for p := step; p <= maxPly && idx < len(r.xlab); p += step {
		xx := x0 + float64(p)/float64(maxPly)*(x1-x0)
		t := r.xlab[idx]
		idx++
		t.Text = itoa(p)
		s := fyne.MeasureText(t.Text, t.TextSize, t.TextStyle)
		t.Resize(s)
		t.Move(fyne.NewPos(float32(xx-float64(s.Width)/2), float32(h-curveMB+3)))
		t.Show()
	}
	for ; idx < len(r.xlab); idx++ {
		r.xlab[idx].Hide()
	}

	r.empty.Move(fyne.NewPos(float32(x0+10), float32((y0+y1)/2)))
	if len(pts) == 0 {
		r.empty.Show()
	} else {
		r.empty.Hide()
	}
}

func (r *curveRenderer) MinSize() fyne.Size { return r.c.MinSize() }

func (r *curveRenderer) Refresh() {
	for i := 0; i <= 4; i++ {
		r.ylab[i].Text = itoa((4 - i) * 25)
	}
	r.raster.Refresh()
	r.Layout(r.c.Size())
	canvas.Refresh(r.c)
}

func (r *curveRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *curveRenderer) Destroy() {}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [8]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
