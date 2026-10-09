package ui

import (
	"image"
	"image/color"
	"math"
)

// 本文件实现棋盘绘制所需的抗锯齿矢量绘制原语。
//
// 为什么不用 canvas.Line 直接拼棋盘：
//   - canvas.Line 由 OpenGL 光栅化，无法控制线宽与端点，斜线（九宫）锯齿明显；
//   - 用 canvas.Raster 自行光栅化可以得到与分辨率无关、边缘平滑的棋盘，
//     并且在 HiDPI（150% / 200% 缩放）下按物理像素绘制，不会模糊。
//
// 所有原语都基于「像素覆盖率」做 alpha 合成：
// 覆盖率 = 该像素被图形覆盖的面积比例（0~1），再按覆盖率混合颜色。

// toNRGBA 把任意 color.Color 归一化为 color.NRGBA。
func toNRGBA(c color.Color) color.NRGBA {
	if c == nil {
		return color.NRGBA{}
	}
	if n, ok := c.(color.NRGBA); ok {
		return n
	}
	r, g, b, a := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

// blend 把颜色 c 以覆盖率 cov 合成到 img 的 (x,y) 像素上（标准 source-over）。
func blend(img *image.RGBA, x, y int, c color.NRGBA, cov float64) {
	if cov <= 0 || c.A == 0 {
		return
	}
	if cov > 1 {
		cov = 1
	}
	b := img.Bounds()
	if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
		return
	}
	sa := float64(c.A) / 255.0 * cov
	i := img.PixOffset(x, y)
	p := img.Pix
	da := float64(p[i+3]) / 255.0
	oa := sa + da*(1-sa)
	if oa <= 0 {
		return
	}
	sr := float64(c.R) / 255.0 * sa
	sg := float64(c.G) / 255.0 * sa
	sb := float64(c.B) / 255.0 * sa
	dr := float64(p[i]) / 255.0 * da
	dg := float64(p[i+1]) / 255.0 * da
	db := float64(p[i+2]) / 255.0 * da
	p[i] = clamp8((sr + dr*(1-sa)) / oa)
	p[i+1] = clamp8((sg + dg*(1-sa)) / oa)
	p[i+2] = clamp8((sb + db*(1-sa)) / oa)
	p[i+3] = clamp8(oa)
}

// overlap 返回区间 [a0,a1] 与像素格 [p,p+1] 的重叠长度。
func overlap(a0, a1 float64, p int) float64 {
	lo := math.Max(a0, float64(p))
	hi := math.Min(a1, float64(p+1))
	if hi <= lo {
		return 0
	}
	return hi - lo
}

// fillRect 填充矩形（轴对齐，按精确覆盖率抗锯齿）。
func fillRect(img *image.RGBA, x0, y0, x1, y1 float64, c color.NRGBA) {
	if c.A == 0 {
		return
	}
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	ix0 := int(math.Floor(x0 - 1))
	ix1 := int(math.Ceil(x1 + 1))
	iy0 := int(math.Floor(y0 - 1))
	iy1 := int(math.Ceil(y1 + 1))
	for y := iy0; y <= iy1; y++ {
		cy := overlap(y0, y1, y)
		if cy <= 0 {
			continue
		}
		for x := ix0; x <= ix1; x++ {
			cx := overlap(x0, x1, x)
			if cx <= 0 {
				continue
			}
			blend(img, x, y, c, cx*cy)
		}
	}
}

// strokeRect 描边矩形（边框宽度 lw，向内绘制）。
func strokeRect(img *image.RGBA, x0, y0, x1, y1, lw float64, c color.NRGBA) {
	if lw <= 0 {
		return
	}
	fillRect(img, x0, y0, x1, y0+lw, c)
	fillRect(img, x0, y1-lw, x1, y1, c)
	fillRect(img, x0, y0+lw, x0+lw, y1-lw, c)
	fillRect(img, x1-lw, y0+lw, x1, y1-lw, c)
}

// drawLine 绘制抗锯齿线段（任意方向，宽度 width）。
func drawLine(img *image.RGBA, x0, y0, x1, y1, width float64, c color.NRGBA) {
	if c.A == 0 || width <= 0 {
		return
	}
	// 轴对齐线段走矩形快路径，既更快也更锐利
	if math.Abs(x0-x1) < 1e-6 {
		fillRect(img, x0-width/2, math.Min(y0, y1), x0+width/2, math.Max(y0, y1), c)
		return
	}
	if math.Abs(y0-y1) < 1e-6 {
		fillRect(img, math.Min(x0, x1), y0-width/2, math.Max(x0, x1), y0+width/2, c)
		return
	}
	hw := width / 2
	ix0 := int(math.Floor(math.Min(x0, x1) - hw - 1))
	ix1 := int(math.Ceil(math.Max(x0, x1) + hw + 1))
	iy0 := int(math.Floor(math.Min(y0, y1) - hw - 1))
	iy1 := int(math.Ceil(math.Max(y0, y1) + hw + 1))
	dx, dy := x1-x0, y1-y0
	len2 := dx*dx + dy*dy
	for y := iy0; y <= iy1; y++ {
		for x := ix0; x <= ix1; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			t := ((px-x0)*dx + (py-y0)*dy) / len2
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
			ex, ey := x0+t*dx-px, y0+t*dy-py
			d := math.Sqrt(ex*ex + ey*ey)
			if cov := hw + 0.5 - d; cov > 0 {
				blend(img, x, y, c, cov)
			}
		}
	}
}

// fillCircle 填充抗锯齿圆。
func fillCircle(img *image.RGBA, cx, cy, r float64, c color.NRGBA) {
	if c.A == 0 || r <= 0 {
		return
	}
	ix0 := int(math.Floor(cx - r - 1))
	ix1 := int(math.Ceil(cx + r + 1))
	iy0 := int(math.Floor(cy - r - 1))
	iy1 := int(math.Ceil(cy + r + 1))
	for y := iy0; y <= iy1; y++ {
		for x := ix0; x <= ix1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if cov := r + 0.5 - math.Sqrt(dx*dx+dy*dy); cov > 0 {
				blend(img, x, y, c, cov)
			}
		}
	}
}

// strokeCircle 描边抗锯齿圆。
func strokeCircle(img *image.RGBA, cx, cy, r, lw float64, c color.NRGBA) {
	if c.A == 0 || r <= 0 || lw <= 0 {
		return
	}
	outer := r + lw/2
	ix0 := int(math.Floor(cx - outer - 1))
	ix1 := int(math.Ceil(cx + outer + 1))
	iy0 := int(math.Floor(cy - outer - 1))
	iy1 := int(math.Ceil(cy + outer + 1))
	for y := iy0; y <= iy1; y++ {
		for x := ix0; x <= ix1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := math.Sqrt(dx*dx + dy*dy)
			if cov := lw/2 + 0.5 - math.Abs(d-r); cov > 0 {
				blend(img, x, y, c, cov)
			}
		}
	}
}

// fillRoundRect 填充圆角矩形（四角用圆覆盖率近似，视觉平滑）。
func fillRoundRect(img *image.RGBA, x0, y0, x1, y1, r float64, c color.NRGBA) {
	if c.A == 0 {
		return
	}
	if r <= 0 {
		fillRect(img, x0, y0, x1, y1, c)
		return
	}
	if maxR := math.Min(x1-x0, y1-y0) / 2; r > maxR {
		r = maxR
	}
	fillRect(img, x0+r, y0, x1-r, y1, c)
	fillRect(img, x0, y0+r, x1, y1-r, c)
	fillCircle(img, x0+r, y0+r, r, c)
	fillCircle(img, x1-r, y0+r, r, c)
	fillCircle(img, x0+r, y1-r, r, c)
	fillCircle(img, x1-r, y1-r, r, c)
}

// strokeRoundRect 描边圆角矩形。
func strokeRoundRect(img *image.RGBA, x0, y0, x1, y1, r, lw float64, c color.NRGBA) {
	if c.A == 0 || lw <= 0 {
		return
	}
	if r <= 0 {
		strokeRect(img, x0, y0, x1, y1, lw, c)
		return
	}
	if maxR := math.Min(x1-x0, y1-y0) / 2; r > maxR {
		r = maxR
	}
	// 四条直边
	fillRect(img, x0+r, y0, x1-r, y0+lw, c)
	fillRect(img, x0+r, y1-lw, x1-r, y1, c)
	fillRect(img, x0, y0+r, x0+lw, y1-r, c)
	fillRect(img, x1-lw, y0+r, x1, y1-r, c)
	// 四个圆角
	strokeCircleArc(img, x0+r, y0+r, r, lw, c, math.Pi, math.Pi*1.5)
	strokeCircleArc(img, x1-r, y0+r, r, lw, c, math.Pi*1.5, math.Pi*2)
	strokeCircleArc(img, x1-r, y1-r, r, lw, c, 0, math.Pi*0.5)
	strokeCircleArc(img, x0+r, y1-r, r, lw, c, math.Pi*0.5, math.Pi)
}

// strokeCircleArc 描边圆弧（角度为弧度，屏幕坐标系 y 向下）。
func strokeCircleArc(img *image.RGBA, cx, cy, r, lw float64, c color.NRGBA, a0, a1 float64) {
	outer := r + lw/2
	ix0 := int(math.Floor(cx - outer - 1))
	ix1 := int(math.Ceil(cx + outer + 1))
	iy0 := int(math.Floor(cy - outer - 1))
	iy1 := int(math.Ceil(cy + outer + 1))
	for y := iy0; y <= iy1; y++ {
		for x := ix0; x <= ix1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := math.Sqrt(dx*dx + dy*dy)
			cov := lw/2 + 0.5 - math.Abs(d-r)
			if cov <= 0 {
				continue
			}
			ang := math.Atan2(dy, dx)
			if ang < 0 {
				ang += 2 * math.Pi
			}
			if ang < a0 || ang > a1 {
				continue
			}
			blend(img, x, y, c, cov)
		}
	}
}
