// Package ui 实现 Fyne 图形界面：护眼主题、Canvas 自绘棋盘、分析面板、对战面板与引擎管理。
//
// 皮肤扩展接口说明：
//
//	ThemeProvider  —— 一套完整的皮肤 = fyne 主题（控件配色） + 棋盘皮肤 + 背景底纹
//	RegisterTheme  —— 注册皮肤（内置 classic 经典护眼 / inkgold 墨玉金）
//	ActiveTheme    —— 按 config.json 的 theme 字段取当前皮肤
//
// 新增皮肤只需实现一个 ThemeProvider 并在 init 里 RegisterTheme，
// 无需改动棋盘绘制、分析面板等任何代码。
package ui

import (
	"image/color"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// ---------------------------------------------------------------------------
// 调色板：一套皮肤的全部颜色都写在一个 palette 里
// ---------------------------------------------------------------------------
//
// 为什么做成结构体 + 可切换（而不是把颜色散在几十个 var 里）：
// 界面代码要在几百处取色，全是包级变量；换肤时只要把 palette 里的值一次性写回这些
// 变量，所有调用点无需改动就能整体换色 —— 这也是「加一套皮肤不用动界面代码」的前提。
//
// 取色纪律（a11y_test.go 里是硬断言，不是注释）：
//   - 正文/次要文字 对 卡片底 ≥ 7:1（WCAG AAA）
//   - 主色/错误/成功/警告文字 对 卡片底 ≥ 4.5:1（WCAG AA）
//   - 文字与面板底、菜单底也要 ≥ 4.5:1，否则"能看清"就只是运气

// palette 是一套皮肤的全部颜色。
type palette struct {
	windowSolid color.NRGBA // 窗口纯色底（背景层取不到底纹时的回退）
	windowBG    color.NRGBA // fyne 主题的窗口底色（也是**菜单栏**底色，必须不透明）
	panelBG     color.NRGBA // 面板 / 菜单 / 头部底色
	cardBG      color.NRGBA // 卡片底
	cardLine    color.NRGBA // 卡片描边（A=0 表示不描边，纯靠层次区分）
	cardShadow  color.NRGBA // 卡片投影（A=0 表示不投影）
	cardRadius  float32     // 卡片圆角
	rowAlt      color.NRGBA // 记谱隔行底色
	barBG       color.NRGBA // 底部状态条底色
	overlay     color.NRGBA // 对话框底

	fore     color.NRGBA // 正文
	foreDim  color.NRGBA // 次要文字
	disabled color.NRGBA

	button      color.NRGBA
	buttonHov   color.NRGBA
	primary     color.NRGBA
	primaryFg   color.NRGBA
	inputBG     color.NRGBA
	inputBorder color.NRGBA
	sep         color.NRGBA
	sel         color.NRGBA
	scroll      color.NRGBA
	scrollBG    color.NRGBA
	shadow      color.NRGBA

	err  color.NRGBA
	ok   color.NRGBA
	warn color.NRGBA

	moveRed   color.NRGBA // 着法列表里红方着法文字
	evalRed   color.NRGBA // 评估条红方
	evalBlack color.NRGBA // 评估条黑方
	hintArrow color.NRGBA // 最佳着法箭头

	// dark 表示这是深色皮肤：除了配色，还用它决定系统标题栏要不要切成深色。
	dark bool

	board BoardSkin      // 棋盘皮肤
	bg    BackgroundSpec // 背景底纹
}

var (
	// 以下变量是界面代码实际取色的地方（几百处），换肤时由 applyPalette 统一刷新。
	// 初值 = 经典护眼皮肤，保证「没有任何皮肤被激活」时（例如单元测试里）颜色依然正确。
	colWindowSolid = color.NRGBA{R: 0xF6, G: 0xF0, B: 0xE2, A: 0xFF}
	colWindowBG    = color.NRGBA{R: 0xF6, G: 0xF0, B: 0xE2, A: 0xFF}
	colPanelBG     = color.NRGBA{R: 0xFB, G: 0xF7, B: 0xEC, A: 0xFF}
	colCardBG      = color.NRGBA{R: 0xFA, G: 0xF5, B: 0xE9, A: 0xFF}
	colCardLine    = color.NRGBA{R: 0xE0, G: 0xD6, B: 0xC0, A: 0xFF}
	colCardShadow  = color.NRGBA{}
	colCardRadius  = float32(8)
	colRowAlt      = color.NRGBA{R: 0xF2, G: 0xEC, B: 0xDD, A: 0xFF}
	colBarBG       = color.NRGBA{R: 0xFA, G: 0xF5, B: 0xE9, A: 0xF0}
	colOverlay     = color.NRGBA{R: 0xFA, G: 0xF6, B: 0xEC, A: 0xFF}

	colFore      = color.NRGBA{R: 0x3A, G: 0x33, B: 0x2B, A: 0xFF}
	colForeDim   = color.NRGBA{R: 0x5A, G: 0x50, B: 0x42, A: 0xFF}
	colDisabled  = color.NRGBA{R: 0xB8, G: 0xAF, B: 0x9E, A: 0xFF}
	colButton    = color.NRGBA{R: 0xEC, G: 0xE2, B: 0xCC, A: 0xFF}
	colButtonHov = color.NRGBA{R: 0xE2, G: 0xD4, B: 0xB6, A: 0xFF}
	colPrimary   = color.NRGBA{R: 0x8C, G: 0x6A, B: 0x3E, A: 0xFF}
	colPrimaryFg = color.NRGBA{R: 0xFD, G: 0xF8, B: 0xEC, A: 0xFF}
	colInputBG   = color.NRGBA{R: 0xFF, G: 0xFD, B: 0xF8, A: 0xFF}
	colInputBord = color.NRGBA{R: 0xCF, G: 0xC0, B: 0xA4, A: 0xFF}
	colSep       = color.NRGBA{R: 0xDC, G: 0xCF, B: 0xB5, A: 0xFF}
	colSel       = color.NRGBA{R: 0xE9, G: 0xDA, B: 0xB2, A: 0xFF}
	colScroll    = color.NRGBA{R: 0xCB, G: 0xBA, B: 0x98, A: 0xFF}
	colScrollBG  = color.NRGBA{R: 0xEF, G: 0xE7, B: 0xD6, A: 0xFF}
	colErr       = color.NRGBA{R: 0xA8, G: 0x36, B: 0x2A, A: 0xFF}
	colOK        = color.NRGBA{R: 0x46, G: 0x74, B: 0x3C, A: 0xFF}
	colWarn      = color.NRGBA{R: 0x8A, G: 0x5E, B: 0x14, A: 0xFF}
	colMoveRed   = color.NRGBA{R: 0xA8, G: 0x36, B: 0x2A, A: 0xFF}
	colShadow    = color.NRGBA{R: 0x8A, G: 0x7C, B: 0x63, A: 0x50}

	colEvalRed   = color.NRGBA{R: 0xC4, G: 0x5A, B: 0x4A, A: 0xFF}
	colEvalBlack = color.NRGBA{R: 0x4A, G: 0x44, B: 0x3C, A: 0xFF}
	colHintArrow = color.NRGBA{R: 0x2E, G: 0x9E, B: 0x5B, A: 0xB0}
)

// BoardSkin 描述棋盘皮肤的全部可调颜色，供皮肤扩展使用。
type BoardSkin struct {
	Plate       color.Color // 棋盘底板
	PlateEdge   color.Color // 底板描边
	BorderOuter color.Color // 外框（粗）
	BorderInner color.Color // 内框（细）
	GridLine    color.Color // 格线 / 九宫斜线
	RiverText   color.Color // 楚河汉界字样
	CoordText   color.Color // 四周坐标标注

	RedFill, RedEdge, RedText       color.Color // 红方棋子：填充 / 描边 / 文字
	BlackFill, BlackEdge, BlackText color.Color // 黑方棋子
	PieceShadow                     color.Color // 棋子立体阴影
	PieceGloss                      color.Color // 棋子高光（A=0 表示不做高光）

	SelectFill color.Color // 选中棋子底色
	SelectEdge color.Color // 选中棋子描边
	TargetDot  color.Color // 合法落点圆点
	LastFrom   color.Color // 上一步起点高亮
	LastTo     color.Color // 上一步终点高亮
	CheckGlow  color.Color // 将军闪烁高亮
	MarkLine   color.Color // 兵/炮位小十字标记

	// PlateGrain 是底板木纹强度（0 = 纯色底板，越大纹理越明显）。
	PlateGrain uint8
}

// BackgroundSpec 描述「程序生成的背景底纹」。
//
// 为什么不继续用一张照片背景：照片会跟界面抢注意力，而且缩放后必然发虚；
// 细腻的底纹（微颗粒 + 一角柔光 + 四周压暗）反而更像专业软件，且任意分辨率都清晰。
type BackgroundSpec struct {
	Base      color.NRGBA // 底色
	Grain     uint8       // 微颗粒幅度（0~16，肉眼几乎看不出颗粒感，但能去掉"塑料感"）
	Glow      color.NRGBA // 左上柔光颜色（A 为强度）
	GlowX     float64     // 柔光中心（占宽比例）
	GlowY     float64     // 柔光中心（占高比例）
	GlowScale float64     // 柔光半径（占对角线比例）
	Vignette  uint8       // 四周压暗强度（0~120）
}

// DefaultBoardSkin 是「经典护眼」棋盘皮肤。
var DefaultBoardSkin = BoardSkin{
	Plate:       color.NRGBA{R: 0xF0, G: 0xE0, B: 0xBC, A: 0xFF},
	PlateEdge:   color.NRGBA{R: 0xC7, G: 0xAF, B: 0x82, A: 0xFF},
	BorderOuter: color.NRGBA{R: 0x6B, G: 0x4E, B: 0x2F, A: 0xFF},
	BorderInner: color.NRGBA{R: 0x8A, G: 0x6A, B: 0x45, A: 0xFF},
	GridLine:    color.NRGBA{R: 0x8A, G: 0x6A, B: 0x45, A: 0xFF},
	RiverText:   color.NRGBA{R: 0x7A, G: 0x5C, B: 0x3A, A: 0xFF},
	CoordText:   color.NRGBA{R: 0x92, G: 0x82, B: 0x68, A: 0xFF},

	RedFill:   color.NRGBA{R: 0xFD, G: 0xF4, B: 0xDF, A: 0xFF},
	RedEdge:   color.NRGBA{R: 0xAF, G: 0x2A, B: 0x2A, A: 0xFF},
	RedText:   color.NRGBA{R: 0xAF, G: 0x2A, B: 0x2A, A: 0xFF},
	BlackFill: color.NRGBA{R: 0xFD, G: 0xF4, B: 0xDF, A: 0xFF},
	BlackEdge: color.NRGBA{R: 0x2E, G: 0x2A, B: 0x26, A: 0xFF},
	BlackText: color.NRGBA{R: 0x2E, G: 0x2A, B: 0x26, A: 0xFF},

	PieceShadow: color.NRGBA{R: 0x6B, G: 0x55, B: 0x38, A: 0x55},

	SelectFill: color.NRGBA{R: 0xF4, G: 0xCE, B: 0x5A, A: 0xC0},
	SelectEdge: color.NRGBA{R: 0xC0, G: 0x8A, B: 0x1C, A: 0xFF},
	TargetDot:  color.NRGBA{R: 0x2E, G: 0x7D, B: 0x4F, A: 0xD0},
	LastFrom:   color.NRGBA{R: 0x6D, G: 0x9E, B: 0xD8, A: 0x90},
	LastTo:     color.NRGBA{R: 0x46, G: 0x7E, B: 0xC6, A: 0xC8},
	CheckGlow:  color.NRGBA{R: 0xE0, G: 0x3A, B: 0x2A, A: 0xB0},
	MarkLine:   color.NRGBA{R: 0x7A, G: 0x5C, B: 0x3A, A: 0xFF},
}

// classicPalette 是「经典护眼」：浅暖米黄底、深灰棕文字、柔和线条。
//
// ⚠ 这里必须写**字面量**，不能引用包级的 colXxx 变量：那些变量正是本函数要写回的目标，
// 引用它们会让"切回经典"变成"再涂一遍当前皮肤的颜色"（反复换肤会越换越脏）。
// 这条由 TestSetActiveThemeSwapsEverything 守着。
func classicPalette() palette {
	return palette{
		windowSolid: color.NRGBA{R: 0xF6, G: 0xF0, B: 0xE2, A: 0xFF},
		windowBG:    color.NRGBA{R: 0xF6, G: 0xF0, B: 0xE2, A: 0xFF},
		panelBG:     color.NRGBA{R: 0xFB, G: 0xF7, B: 0xEC, A: 0xF2},
		cardBG:      color.NRGBA{R: 0xFA, G: 0xF5, B: 0xE9, A: 0xFF},
		cardLine:    color.NRGBA{R: 0xE0, G: 0xD6, B: 0xC0, A: 0xFF},
		cardShadow:  color.NRGBA{},
		cardRadius:  8,
		rowAlt:      color.NRGBA{R: 0xF2, G: 0xEC, B: 0xDD, A: 0xFF},
		barBG:       color.NRGBA{R: 0xFA, G: 0xF5, B: 0xE9, A: 0xF0},
		overlay:     color.NRGBA{R: 0xFA, G: 0xF6, B: 0xEC, A: 0xFF},

		fore:     color.NRGBA{R: 0x3A, G: 0x33, B: 0x2B, A: 0xFF},
		foreDim:  color.NRGBA{R: 0x5A, G: 0x50, B: 0x42, A: 0xFF},
		disabled: color.NRGBA{R: 0xB8, G: 0xAF, B: 0x9E, A: 0xFF},

		button:      color.NRGBA{R: 0xEC, G: 0xE2, B: 0xCC, A: 0xFF},
		buttonHov:   color.NRGBA{R: 0xE2, G: 0xD4, B: 0xB6, A: 0xFF},
		primary:     color.NRGBA{R: 0x8C, G: 0x6A, B: 0x3E, A: 0xFF},
		primaryFg:   color.NRGBA{R: 0xFD, G: 0xF8, B: 0xEC, A: 0xFF},
		inputBG:     color.NRGBA{R: 0xFF, G: 0xFD, B: 0xF8, A: 0xFF},
		inputBorder: color.NRGBA{R: 0xCF, G: 0xC0, B: 0xA4, A: 0xFF},
		sep:         color.NRGBA{R: 0xDC, G: 0xCF, B: 0xB5, A: 0xFF},
		sel:         color.NRGBA{R: 0xE9, G: 0xDA, B: 0xB2, A: 0xFF},
		scroll:      color.NRGBA{R: 0xCB, G: 0xBA, B: 0x98, A: 0xFF},
		scrollBG:    color.NRGBA{R: 0xEF, G: 0xE7, B: 0xD6, A: 0xFF},
		shadow:      color.NRGBA{R: 0x8A, G: 0x7C, B: 0x63, A: 0x50},

		err:  color.NRGBA{R: 0xA8, G: 0x36, B: 0x2A, A: 0xFF},
		ok:   color.NRGBA{R: 0x46, G: 0x74, B: 0x3C, A: 0xFF},
		warn: color.NRGBA{R: 0x8A, G: 0x5E, B: 0x14, A: 0xFF},

		moveRed:   color.NRGBA{R: 0xA8, G: 0x36, B: 0x2A, A: 0xFF},
		evalRed:   color.NRGBA{R: 0xC4, G: 0x5A, B: 0x4A, A: 0xFF},
		evalBlack: color.NRGBA{R: 0x4A, G: 0x44, B: 0x3C, A: 0xFF},
		hintArrow: color.NRGBA{R: 0x2E, G: 0x9E, B: 0x5B, A: 0xB0},

		board: DefaultBoardSkin,
		bg: BackgroundSpec{
			Base:      color.NRGBA{R: 0xF6, G: 0xF0, B: 0xE2, A: 0xFF},
			Grain:     5,
			Glow:      color.NRGBA{R: 0xFF, G: 0xFC, B: 0xF2, A: 0x50},
			GlowX:     0.28,
			GlowY:     0.12,
			GlowScale: 1.10,
			Vignette:  0,
		},
	}
}

// inkGoldPalette 是「墨玉金」：深墨底 + 暖金强调，棋子象牙白 + 高光，
// 棋盘是压暗的胡桃木 + 金色外框（专业棋软的那种沉稳感）。
//
// 取色理由：底色全部落在同一色相（冷墨灰）上、只用**一个**强调色（金 #C9A961），
// 层次靠 surface 明度差而不是描边 —— 这两条是「看起来贵」的主要来源。
func inkGoldPalette() palette {
	gold := color.NRGBA{R: 0xC9, G: 0xA9, B: 0x61, A: 0xFF}
	return palette{
		windowSolid: color.NRGBA{R: 0x14, G: 0x16, B: 0x1A, A: 0xFF},
		windowBG:    color.NRGBA{R: 0x14, G: 0x16, B: 0x1A, A: 0xFF},
		panelBG:     color.NRGBA{R: 0x1B, G: 0x1E, B: 0x25, A: 0xF7},
		cardBG:      color.NRGBA{R: 0x21, G: 0x25, B: 0x2D, A: 0xFF},
		cardLine:    color.NRGBA{R: 0x2E, G: 0x33, B: 0x3D, A: 0xFF},
		cardShadow:  color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x59},
		cardRadius:  10,
		rowAlt:      color.NRGBA{R: 0x1D, G: 0x21, B: 0x28, A: 0xFF},
		barBG:       color.NRGBA{R: 0x18, G: 0x1B, B: 0x21, A: 0xF2},
		overlay:     color.NRGBA{R: 0x1B, G: 0x1E, B: 0x25, A: 0xFF},

		fore:     color.NRGBA{R: 0xEE, G: 0xEA, B: 0xE1, A: 0xFF},
		foreDim:  color.NRGBA{R: 0xB8, G: 0xB4, B: 0xAB, A: 0xFF},
		disabled: color.NRGBA{R: 0x6C, G: 0x71, B: 0x7A, A: 0xFF},

		button:      color.NRGBA{R: 0x27, G: 0x2C, B: 0x35, A: 0xFF},
		buttonHov:   color.NRGBA{R: 0x31, G: 0x37, B: 0x42, A: 0xFF},
		primary:     gold,
		primaryFg:   color.NRGBA{R: 0x16, G: 0x14, B: 0x0F, A: 0xFF},
		inputBG:     color.NRGBA{R: 0x18, G: 0x1B, B: 0x21, A: 0xFF},
		inputBorder: color.NRGBA{R: 0x3A, G: 0x41, B: 0x4D, A: 0xFF},
		sep:         color.NRGBA{R: 0x2A, G: 0x2F, B: 0x38, A: 0xFF},
		sel:         color.NRGBA{R: 0x3D, G: 0x35, B: 0x22, A: 0xFF},
		scroll:      color.NRGBA{R: 0x3A, G: 0x41, B: 0x4D, A: 0xFF},
		scrollBG:    color.NRGBA{R: 0x1B, G: 0x1E, B: 0x25, A: 0xFF},
		shadow:      color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x66},

		err:  color.NRGBA{R: 0xE8, G: 0x7C, B: 0x74, A: 0xFF},
		ok:   color.NRGBA{R: 0x86, G: 0xC0, B: 0x8F, A: 0xFF},
		warn: color.NRGBA{R: 0xD9, G: 0xAE, B: 0x6A, A: 0xFF},

		moveRed:   color.NRGBA{R: 0xE8, G: 0x8A, B: 0x80, A: 0xFF},
		evalRed:   color.NRGBA{R: 0xC4, G: 0x67, B: 0x5A, A: 0xFF},
		evalBlack: color.NRGBA{R: 0x8C, G: 0x93, B: 0xA0, A: 0xFF},
		hintArrow: color.NRGBA{R: 0x5A, G: 0xC8, B: 0x8C, A: 0xC0},
		dark:      true,

		board: BoardSkin{
			// 压暗的胡桃木：比底纹亮一档，让棋盘成为视觉中心。
			// 木纹强度 9 是调出来的 —— 16 会看出"条纹布"，5 以下又看不出木头质感。
			Plate:       color.NRGBA{R: 0x30, G: 0x28, B: 0x1E, A: 0xFF},
			PlateEdge:   color.NRGBA{R: 0x4A, G: 0x3A, B: 0x22, A: 0xFF},
			BorderOuter: color.NRGBA{R: 0xC9, G: 0xA9, B: 0x61, A: 0xFF},
			BorderInner: color.NRGBA{R: 0x8A, G: 0x74, B: 0x40, A: 0xFF},
			GridLine:    color.NRGBA{R: 0x7A, G: 0x63, B: 0x38, A: 0xE6},
			RiverText:   color.NRGBA{R: 0xB3, G: 0x95, B: 0x54, A: 0xFF},
			CoordText:   color.NRGBA{R: 0x8A, G: 0x74, B: 0x40, A: 0xFF},

			RedFill:   color.NRGBA{R: 0xF7, G: 0xEF, B: 0xDD, A: 0xFF},
			RedEdge:   color.NRGBA{R: 0xC0, G: 0x39, B: 0x2B, A: 0xFF},
			RedText:   color.NRGBA{R: 0xB2, G: 0x31, B: 0x27, A: 0xFF},
			BlackFill: color.NRGBA{R: 0xF7, G: 0xEF, B: 0xDD, A: 0xFF},
			BlackEdge: color.NRGBA{R: 0x22, G: 0x20, B: 0x1D, A: 0xFF},
			BlackText: color.NRGBA{R: 0x22, G: 0x20, B: 0x1D, A: 0xFF},

			PieceShadow: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x8C},
			PieceGloss:  color.NRGBA{R: 0xFF, G: 0xF6, B: 0xE2, A: 0x5A},

			SelectFill: color.NRGBA{R: 0xC9, G: 0xA9, B: 0x61, A: 0x70},
			SelectEdge: color.NRGBA{R: 0xE0, G: 0xC0, B: 0x78, A: 0xFF},
			TargetDot:  color.NRGBA{R: 0x76, G: 0xC1, B: 0x8E, A: 0xD0},
			LastFrom:   color.NRGBA{R: 0xC9, G: 0xA9, B: 0x61, A: 0x55},
			LastTo:     color.NRGBA{R: 0xC9, G: 0xA9, B: 0x61, A: 0xAA},
			CheckGlow:  color.NRGBA{R: 0xE0, G: 0x5A, B: 0x4E, A: 0xC8},
			MarkLine:   color.NRGBA{R: 0x7A, G: 0x63, B: 0x38, A: 0xE6},
			PlateGrain: 9,
		},
		bg: BackgroundSpec{
			Base:      color.NRGBA{R: 0x14, G: 0x16, B: 0x1A, A: 0xFF},
			Grain:     4,
			Glow:      color.NRGBA{R: 0x2E, G: 0x3A, B: 0x58, A: 0x46},
			GlowX:     0.26,
			GlowY:     0.10,
			GlowScale: 1.15,
			Vignette:  70,
		},
	}
}

// ---------------------------------------------------------------------------
// 皮肤注册与激活
// ---------------------------------------------------------------------------

// ThemeProvider 是皮肤扩展接口：一套皮肤 = 调色板（含棋盘皮肤与背景底纹）。
type ThemeProvider interface {
	Name() string        // 唯一标识，对应 config.json 的 theme 字段
	Description() string // 中文描述（设置菜单里显示）
	Palette() palette    // 全部颜色
	Theme() fyne.Theme   // 控件主题
}

var themeRegistry = map[string]ThemeProvider{}

// RegisterTheme 注册一套皮肤（包初始化时调用）。
func RegisterTheme(p ThemeProvider) { themeRegistry[p.Name()] = p }

// ThemeNames 返回全部已注册皮肤名称（排序）。
func ThemeNames() []string {
	out := make([]string, 0, len(themeRegistry))
	for k := range themeRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ThemeNamesOrdered 按「经典在前、其余按名字」的顺序返回皮肤名（设置菜单用）。
func ThemeNamesOrdered() []string {
	out := make([]string, 0, len(themeRegistry))
	if _, ok := themeRegistry[ThemeClassic]; ok {
		out = append(out, ThemeClassic)
	}
	for _, n := range ThemeNames() {
		if n != ThemeClassic {
			out = append(out, n)
		}
	}
	return out
}

// ThemeLabel 返回皮肤的中文短名（菜单里显示）。
func ThemeLabel(name string) string {
	switch name {
	case ThemeClassic:
		return "经典护眼"
	case ThemeInkGold:
		return "墨玉金"
	}
	return name
}

// ActiveTheme 按名称取皮肤；名称未知时回退到经典护眼。
func ActiveTheme(name string) ThemeProvider {
	if p, ok := themeRegistry[name]; ok {
		return p
	}
	return themeRegistry[ThemeClassic]
}

const (
	// ThemeClassic 是浅暖米黄的护眼皮肤。
	ThemeClassic = "default"
	// ThemeInkGold 是深墨底 + 暖金强调的皮肤。
	ThemeInkGold = "inkgold"
)

// activeProvider 是当前生效的皮肤。界面代码通过 currentSkin()/currentPalette() 取它，
// 而不是每次按名字查表 —— 这样棋盘、背景、对话框永远和当前皮肤一致。
var activeProvider ThemeProvider = classicProvider{}

// applyPalette 把一套皮肤的颜色写回包级变量（界面代码全部读这些变量）。
func applyPalette(p palette) {
	colWindowSolid, colWindowBG = p.windowSolid, p.windowBG
	colPanelBG, colCardBG = p.panelBG, p.cardBG
	colCardLine, colCardShadow, colCardRadius = p.cardLine, p.cardShadow, p.cardRadius
	colRowAlt, colBarBG, colOverlay = p.rowAlt, p.barBG, p.overlay

	colFore, colForeDim, colDisabled = p.fore, p.foreDim, p.disabled
	colButton, colButtonHov = p.button, p.buttonHov
	colPrimary, colPrimaryFg = p.primary, p.primaryFg
	colInputBG, colInputBord = p.inputBG, p.inputBorder
	colSep, colSel = p.sep, p.sel
	colScroll, colScrollBG = p.scroll, p.scrollBG
	colShadow = p.shadow

	colErr, colOK, colWarn = p.err, p.ok, p.warn
	colMoveRed = p.moveRed
	colEvalRed, colEvalBlack, colHintArrow = p.evalRed, p.evalBlack, p.hintArrow
}

// SetActiveTheme 激活一套皮肤：颜色写回包级变量，并记录当前 provider。
//
// 只改了包级颜色与 activeProvider；fyne 主题与界面刷新由调用方负责
// （见 App.applyThemeName）—— 这样单元测试也能单独调用它来校验调色板。
func SetActiveTheme(name string) ThemeProvider {
	p := ActiveTheme(name)
	activeProvider = p
	applyPalette(p.Palette())
	return p
}

// currentSkin 返回当前皮肤（棋盘用）。
func currentSkin() BoardSkin { return activeProvider.Palette().board }

// currentBackground 返回当前背景底纹参数。
func currentBackground() BackgroundSpec { return activeProvider.Palette().bg }

// currentThemeIsDark 报告当前皮肤是不是深色（决定系统标题栏配色）。
func currentThemeIsDark() bool { return activeProvider.Palette().dark }

// ---------------------------------------------------------------------------
// 皮肤实现
// ---------------------------------------------------------------------------

type classicProvider struct{}

func (classicProvider) Name() string     { return ThemeClassic }
func (classicProvider) Palette() palette { return classicPalette() }
func (classicProvider) Theme() fyne.Theme {
	return defaultTheme{}
}
func (classicProvider) Description() string {
	return "浅暖米黄底、深灰棕文字、低饱和线条的护眼配色"
}

type inkGoldProvider struct{}

func (inkGoldProvider) Name() string     { return ThemeInkGold }
func (inkGoldProvider) Palette() palette { return inkGoldPalette() }
func (inkGoldProvider) Theme() fyne.Theme {
	return defaultTheme{}
}
func (inkGoldProvider) Description() string {
	return "深墨底 + 暖金强调色，象牙白棋子、压暗胡桃木棋盘"
}

func init() {
	RegisterTheme(classicProvider{})
	RegisterTheme(inkGoldProvider{})
	// 没有任何皮肤被激活时（单元测试、库被直接引用）也用经典配色，
	// 保证包级颜色变量与 classicPalette() 一致。
	applyPalette(classicPalette())
}

// defaultTheme 是「按当前调色板取色」的 Fyne 主题。
//
// 它读的是 applyPalette 写回的包级变量：这样主题色与界面代码手工取的颜色
// 永远同源，改一处不会出现「按钮变了、卡片没变」这种半截换肤。
type defaultTheme struct{}

var _ fyne.Theme = defaultTheme{}

func (defaultTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		// 【缺陷修复】这里必须是**不透明**色：Fyne 的菜单栏（driver 的 MenuBar）
		// 也用 ColorNameBackground 当自己的底色，给透明值会让菜单栏画在窗口的
		// 黑色清屏色上 —— 用户看到的就是"菜单栏一条黑"。
		// 内容区的背景由 App 自己画（背景底纹/图片层），不受这里影响。
		return colWindowBG
	case theme.ColorNameOverlayBackground, theme.ColorNameMenuBackground, theme.ColorNameHeaderBackground:
		return colOverlay
	case theme.ColorNameForeground:
		return colFore
	case theme.ColorNameDisabled:
		return colDisabled
	case theme.ColorNameButton:
		return colButton
	case theme.ColorNameDisabledButton:
		return colRowAlt
	case theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return colPrimary
	case theme.ColorNameForegroundOnPrimary:
		return colPrimaryFg
	case theme.ColorNameInputBackground:
		return colInputBG
	case theme.ColorNameInputBorder:
		return colInputBord
	case theme.ColorNamePlaceHolder:
		return colForeDim
	case theme.ColorNameHover:
		return colButtonHov
	case theme.ColorNamePressed:
		return colSel
	case theme.ColorNameFocus:
		return colPrimary
	case theme.ColorNameSelection:
		return colSel
	case theme.ColorNameSeparator:
		return colSep
	case theme.ColorNameScrollBar:
		return colScroll
	case theme.ColorNameScrollBarBackground:
		return colScrollBG
	case theme.ColorNameShadow:
		return colShadow
	case theme.ColorNameError:
		return colErr
	case theme.ColorNameSuccess:
		return colOK
	case theme.ColorNameWarning:
		return colWarn
	}
	return theme.DefaultTheme().Color(n, v)
}

func (defaultTheme) Font(s fyne.TextStyle) fyne.Resource {
	// 使用 Fyne 的系统字体解析链（v2.8 会扫描系统字体，
	// 因此中文汉字由 Windows 自带中文字体渲染，无需打包字体文件）。
	return theme.DefaultTheme().Font(s)
}

func (defaultTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(n)
}
