package ui

import (
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// 本文件实现「适老化」字号档位。
//
// 目标用户：50~60 岁以上，视力与手指精度都在下降。
// 依据（不是拍脑袋）：
//   - W3C WCAG 2.1 成功标准 1.4.3：正文对比度至少 4.5:1，大号文字至少 3:1
//     https://www.w3.org/Translations/WCAG21-zh/
//   - W3C WCAG 2.2 成功标准 2.5.8：点击目标至少 24×24 CSS 像素；
//     更推荐按 2.5.5（AAA）做到 44×44，并「提供选项放大目标、调节布局密度」
//     https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html
//   - 该文档明确写到：这些准则「使因衰老而使能力有所改变的老年人更容易使用」
//
// 上面的数字都有回归测试守着（a11y_test.go），不是写在注释里就算数：
// 按钮/菜单项的实际最小高度、次要文字的实际对比度都在测试里量过。
//
// 落地方式：**Fyne 的所有控件尺寸都来自主题的 Size()**（按钮高度 = 文字高度 + 2×Padding，
// 输入框、列表行、菜单项同理），所以只要在这一处按档位放大，全界面同时变大——
// 不需要去改每一个控件的字号，也不会漏掉某个角落。

// 字号档位（对应「设置 → 字号」三个菜单项）。
//
// 注意定位：**默认是「标准」，界面尺寸与 v1.5 之前完全一致**。
// 本文件提供的是「想放大的人可以自己放大」的能力，而不是把界面整体做大——
// 需求原话是「目标用户涵盖各个年龄段，界面简单易操作，字体正常就好」。
const (
	fontScaleStandard = "standard" // 标准：默认
	fontScaleLarge    = "large"    // 大：可选
	fontScaleXLarge   = "xlarge"   // 特大：可选
)

// fontScale 保存当前档位的倍率；主题与所有界面字号都从它取。
var fontScale = struct {
	mu  sync.RWMutex
	val float32
}{val: 1.0}

// SetFontScale 设置字号倍率（0.9~1.6，超出会被夹住）。
func SetFontScale(v float32) {
	if v < 0.9 {
		v = 0.9
	}
	if v > 1.6 {
		v = 1.6
	}
	fontScale.mu.Lock()
	fontScale.val = v
	fontScale.mu.Unlock()
}

// FontScale 返回当前字号倍率。
func FontScale() float32 {
	fontScale.mu.RLock()
	defer fontScale.mu.RUnlock()
	return fontScale.val
}

// FontScaleFor 把档位名换算成倍率。
func FontScaleFor(name string) float32 {
	switch name {
	case fontScaleStandard:
		return 1.0
	case fontScaleXLarge:
		return 1.45
	default: // fontScaleLarge
		return 1.22
	}
}

// FontScaleName 把倍率反查成档位名（用于初始化与菜单勾选）。
func FontScaleName(v float32) string {
	switch {
	case v < 1.08:
		return fontScaleStandard
	case v > 1.35:
		return fontScaleXLarge
	default:
		return fontScaleLarge
	}
}

// sz 按当前档位放大一个基准尺寸。
func sz(base float32) float32 { return base * FontScale() }

type textRole int

const (
	textBody textRole = iota
	textSmall
	textLabel
	textTitle
	textBig
)

// allTextRoles 供「换档时按角色重算」遍历（顺序无关）。
var allTextRoles = [...]textRole{textBody, textSmall, textLabel, textTitle, textBig}

// roleBase 是某个角色在「标准档（1.0）」下的基准字号。
//
// 【v1.5 调小】目标用户是「普通客户」，不是特定人群；实测原来的 16/15/17 偏大、
// 界面显得空，同类桌面象棋软件正文普遍在 12~13px、面板标题 14px 上下。
// 现在这一组值就是把界面密度对齐同类产品的结果。
// 需要更大的字，用户自己在「设置 → 字号」里放大（档位机制见本文件开头）。
//
// 之所以把基准值单独抽出来：切换档位时要按「角色基准 × 新倍率」重算，
// 必须能拿到「不含倍率的原始值」，否则会一次一次乘出漂移。
func roleBase(role textRole) float32 {
	switch role {
	case textBody: // 正文、状态栏、列表
		return 13
	case textSmall: // 次要说明
		return 11.5
	case textLabel: // 面板小标题、按钮文字
		return 12.5
	case textTitle: // 区块标题
		return 14
	case textBig: // 最佳着法这种要一眼看到的
		return 22
	}
	return 12.5
}

// textSize 是「正文/普通说明文字」的统一入口。
//
// 全界面凡是要写死字号的 canvas.Text，都应该用 textSize(...) 而不要写死数字——
// 之前工程里有 16 处 11px、25 处 12px，对 50~60 岁用户等于看不清。
func textSize(role textRole) float32 { return sz(roleBase(role)) }

// basePadding 是主题内边距的基准值（布局疏密的总闸门）。
//
// 【v1.5 调小】8 → 6：用户反馈「各个栏的参数界面太大」，加上字号下调，
// 面板四周留白同步收紧，把省下来的空间还给棋盘和着法列表。
const basePadding = 6

// baseInnerPadding 是控件内边距的基准值（决定按钮/输入框的高度）。
//
// 【v1.5 调小】6 → 5，与上面同步收紧，避免按钮在高级字下显得又大又空。
// Fyne 的按钮高度 = 标签高度 + 2×InnerPadding（见 widget/button.go）。
const baseInnerPadding = 5

func (defaultTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNamePadding:
		return sz(basePadding)
	case theme.SizeNameInnerPadding:
		return sz(baseInnerPadding)
	case theme.SizeNameText:
		return textSize(textBody)
	case theme.SizeNameHeadingText:
		return textSize(textTitle)
	case theme.SizeNameSubHeadingText:
		return textSize(textLabel)
	case theme.SizeNameSeparatorThickness:
		// 太细的分隔线在老花眼里等于不存在，加粗到 2
		return 2
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameScrollBar:
		// 滚动条加宽：太细不好拖（同类软件的滚动条也在 12~14px）
		return sz(12)
	case theme.SizeNameScrollBarSmall:
		return sz(5)
	case theme.SizeNameSplitThickness:
		// 分栏手柄：够抓就行，太粗会白占界面宽度
		return sz(10)
	case theme.SizeNameCaptionText:
		return textSize(textSmall)
	}
	return theme.DefaultTheme().Size(n)
}

// ---------------------------------------------------------------------------
// 切换档位：不重建界面，直接把全界面文字换成新档位
// ---------------------------------------------------------------------------

// walkCanvasObjects 深度遍历界面对象树：容器进 Objects，
// 自绘控件（棋盘 / 曲线 / 评估条）进它自己的渲染器。
//
// 为什么两样都要走：本工程里有一半文字是直接用 canvas.Text 摆在容器里的，
// 另一半在自绘控件的渲染器里。只走其中一边就会漏掉一半界面。
func walkCanvasObjects(o fyne.CanvasObject, fn func(fyne.CanvasObject)) {
	walkDepth(o, fn, 0)
}

func walkDepth(o fyne.CanvasObject, fn func(fyne.CanvasObject), depth int) {
	if o == nil || depth > 64 { // 深度上限：防御渲染器互相引用的极端情况
		return
	}
	fn(o)
	switch v := o.(type) {
	case *fyne.Container:
		for _, c := range v.Objects {
			walkDepth(c, fn, depth+1)
		}
	case fyne.Widget:
		r := v.CreateRenderer()
		if r == nil {
			return
		}
		for _, c := range r.Objects() {
			walkDepth(c, fn, depth+1)
		}
	}
}

// nearly 判断两个字号是否「就是同一个角色的同一个值」。
//
// canvas.Text 里的字号是 float32 乘出来的，直接 == 在多数情况下能过，
// 但留一点容差，避免某次中间运算精度不同就漏掉一整个角色。
func nearly(a, b float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.01
}

// retargetTextSize 把「旧档位下的某个角色字号」换算成新档位下的字号。
//
// 只认「正好等于旧档位某个角色字号」的文字：Fyne 内建控件（按钮、标签、菜单）
// 的字号本来就由主题给出，改主题它们自己会变；这里只负责那些**写死了字号**
// 的 canvas.Text。两边的目标值一致，所以不会重复放大。
func retargetTextSize(old float32, new float32) func(fyne.CanvasObject) {
	return func(o fyne.CanvasObject) {
		t, ok := o.(*canvas.Text)
		if !ok || t.TextSize <= 0 {
			return
		}
		for _, role := range allTextRoles {
			if nearly(t.TextSize, roleBase(role)*old) {
				t.TextSize = roleBase(role) * new
				t.Refresh()
				return
			}
		}
	}
}

// ApplyFontScale 把全界面切到当前档位，**不重建任何控件**。
//
// old 是切换前的倍率。调用顺序要紧：先把倍率设成新的（SetFontScale），
// 再换主题（Fyne 内建控件跟着主题变），最后走一遍界面把写死字号的
// canvas.Text 换成新档位——两件事各管一半，不会互相打架。
func ApplyFontScale(app fyne.App, old float32) {
	now := FontScale()
	if nearly(now, old) {
		return
	}
	if app != nil {
		for _, w := range app.Driver().AllWindows() {
			if w == nil || w.Content() == nil {
				continue
			}
			walkCanvasObjects(w.Content(), retargetTextSize(old, now))
		}
	}
}

// describeFontScale 给状态栏/菜单显示当前档位的中文名。
func describeFontScale() string {
	switch FontScaleName(FontScale()) {
	case fontScaleStandard:
		return "标准"
	case fontScaleXLarge:
		return "特大"
	default:
		return "大（推荐）"
	}
}

// fontTierLabel 是菜单里显示的名字。
func fontTierLabel(name string) string {
	switch name {
	case fontScaleStandard:
		return "标准（年轻视力）"
	case fontScaleXLarge:
		return "特大（视力明显下降）"
	default:
		return "大（推荐，按 50~60 岁设计）"
	}
}

// a11yForeDim 是「次要文字」的颜色。
//
// colForeDim 原本是 0x776B59，在米黄底 0xFAF6EC 上的对比度约 4.7:1，
// 刚过 WCAG AA 的 4.5:1，但对 60 岁以上用户仍然偏淡。
// 现统一加深到 ≈7.3:1（WCAG AAA），代价只是「次要文字没那么次要」。
// 实际对比度由 a11y_test.go 计算验证。
var a11yForeDim = color.NRGBA{R: 0x5A, G: 0x50, B: 0x42, A: 0xFF}
