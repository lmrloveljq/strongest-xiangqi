package ui

import (
	"image/color"

	"fyne.io/fyne/v2/canvas"
)

// 本文件生成「主题自带的细腻底纹」：微颗粒 + 一角柔光 + 四周压暗。
//
// 为什么不用照片背景：
//   - 照片会跟棋盘抢注意力，缩放后必然发虚，深色界面上还会出现色偏；
//   - 程序生成的底纹在任意分辨率/DPI 下都清晰，而且**跟着皮肤走**：
//     换肤时底色、颗粒强度、柔光色一起变，不用再准备第二张图。
//
// 实现用 canvas.NewRasterWithPixels：Fyne 在需要时按实际像素尺寸回调取色，
// 因此不需要缓存文件，也不需要关心窗口大小（缩放时自动重算）。

// newBackgroundRaster 按底纹参数造一个铺满窗口的底纹图层。
func newBackgroundRaster(spec BackgroundSpec) *canvas.Raster {
	r := canvas.NewRasterWithPixels(backgroundPixel(spec))
	r.ScaleMode = canvas.ImageScaleFastest
	return r
}

// backgroundPixel 返回「按像素取色」的函数（单独拿出来是为了能直接断言它的性质）。
func backgroundPixel(spec BackgroundSpec) func(x, y, w, h int) color.Color {
	grain := int(spec.Grain)
	vig := int(spec.Vignette)
	glow := spec.Glow
	base := spec.Base
	gx, gy, gs := spec.GlowX, spec.GlowY, spec.GlowScale
	if gs <= 0 {
		gs = 1.0
	}

	return func(x, y, w, h int) color.Color {
		c := base
		if grain > 0 {
			// 确定性伪随机颗粒（同样的坐标永远得到同样的值，窗口重绘不会闪）。
			// 用两个不同周期的哈希再相减，得到近似零均值的噪声：整体不变亮也不变暗，
			// 只在极小的范围内起伏 —— 这就是"细腻"而不是"噪点"的关键。
			a := hash2(x, y)
			b := hash2(x>>1, y>>1)
			d := int(a) - int(b) // 约 [-255,255]
			c = shade(c, d*grain/512)
		}
		if glow.A > 0 && w > 0 && h > 0 {
			dx := (float64(x)/float64(w) - gx)
			dy := (float64(y)/float64(h) - gy)
			d := (dx*dx + dy*dy) / (gs * gs)
			if d < 1 {
				f := (1 - d) * (1 - d) // 平方衰减，边界处自然消失
				c = mix(c, toNRGBA(glow), f*float64(glow.A)/255.0)
			}
		}
		if vig > 0 && w > 0 && h > 0 {
			// 四周压暗：中心 0、边角最强，让视线自然收到中间的棋盘上。
			cx := float64(x)/float64(w)*2 - 1
			cy := float64(y)/float64(h)*2 - 1
			d := cx*cx + cy*cy // 0（中心）~2（边角）
			if d > 0.35 {
				f := (d - 0.35) / 1.65
				if f > 1 {
					f = 1
				}
				c = mix(c, color.NRGBA{A: 0xFF}, f*float64(vig)/255.0)
			}
		}
		return c
	}
}

// hash2 是坐标 → 0..255 的确定性哈希。
func hash2(x, y int) uint8 {
	h := uint32(x)*0x9E3779B1 ^ uint32(y)*0x85EBCA77
	h ^= h >> 15
	h *= 0x2545F491
	h ^= h >> 13
	return uint8(h >> 8)
}

// shade 把颜色按 delta（-255..255）整体提亮或压暗，保持 alpha 不变。
func shade(c color.NRGBA, delta int) color.NRGBA {
	adj := func(v uint8) uint8 {
		n := int(v) + delta
		if n < 0 {
			return 0
		}
		if n > 255 {
			return 255
		}
		return uint8(n)
	}
	return color.NRGBA{R: adj(c.R), G: adj(c.G), B: adj(c.B), A: c.A}
}

// mix 按比例 f（0~1）把 over 混到 under 上（标准 source-over，保留 under 的 alpha）。
func mix(under, over color.NRGBA, f float64) color.NRGBA {
	if f <= 0 {
		return under
	}
	if f > 1 {
		f = 1
	}
	blendCh := func(u, o uint8) uint8 {
		return uint8(float64(u)*(1-f) + float64(o)*f + 0.5)
	}
	return color.NRGBA{
		R: blendCh(under.R, over.R),
		G: blendCh(under.G, over.G),
		B: blendCh(under.B, over.B),
		A: under.A,
	}
}
