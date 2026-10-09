package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image/color"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// 适老化（a11y）回归测试。
//
// 为什么这些必须是测试而不是注释：a11y.go 里写着「按钮高约 44px」「对比度约 7:1」，
// 注释会骗人——日后谁改了 Padding 基准或颜色，注释不会跟着变，但测试会红。
// 这里把 WCAG 的硬指标直接变成断言：
//   - 2.5.5（AAA）点击目标 ≥44×44；2.5.8（AA）≥24×24
//   - 1.4.3 正文对比度 ≥4.5:1，大字 ≥3:1（本项目内部按 AAA 的 7:1 要求次要文字）

// withFontTier 在测试期间切到指定档位，结束后恢复。
//
// 字号是**包级全局状态**（主题 Size() 直接读它），漏了恢复会污染同包其它测试。
func withFontTier(t *testing.T, name string) {
	t.Helper()
	old := FontScale()
	SetFontScale(FontScaleFor(name))
	t.Cleanup(func() { SetFontScale(old) })
}

// newTestThemeApp 起一个测试用 Fyne App 并装上本工程的护眼主题。
//
// 不装 defaultTheme{} 的话，量到的是 Fyne 默认主题的尺寸，测不到我们自己的 Size()。
func newTestThemeApp(t *testing.T) fyne.App {
	t.Helper()
	app := test.NewApp()
	app.Settings().SetTheme(defaultTheme{})
	return app
}

func TestFontTierNamesRoundTrip(t *testing.T) {
	for _, name := range []string{fontScaleStandard, fontScaleLarge, fontScaleXLarge} {
		v := FontScaleFor(name)
		if got := FontScaleName(v); got != name {
			t.Fatalf("FontScaleFor(%q) = %v，反查回 %q，档位与倍率不是一一对应", name, v, got)
		}
	}
	// 认不出的档位名必须落到默认「大」，绝不能变成 0 倍（那会让整个界面消失）
	if v := FontScaleFor("天知道"); v != FontScaleFor(fontScaleLarge) {
		t.Fatalf("未知档位名回退成了 %v，应回退到默认大档 %v", v, FontScaleFor(fontScaleLarge))
	}
}

func TestFontScaleClamped(t *testing.T) {
	defer SetFontScale(1.0)
	SetFontScale(0.01)
	if got := FontScale(); got != 0.9 {
		t.Fatalf("倍率下限：SetFontScale(0.01) 后 = %v，应夹到 0.9", got)
	}
	SetFontScale(99)
	if got := FontScale(); got != 1.6 {
		t.Fatalf("倍率上限：SetFontScale(99) 后 = %v，应夹到 1.6", got)
	}
}

// TestTouchTargetsMeetWCAG 量真实的按钮最小高度。
//
// 口径（与需求一致：界面以「密度对齐同类产品」为先，放大是用户自选）：
//   - 三档都必须满足 WCAG 2.5.8（AA）的 24px 底线；
//   - 三档必须严格递增（放大档不能反而更小）；
//   - 默认档倍率必须正好 1.0（默认界面不允许被整体撑大）。
//
// 说明：WCAG 2.5.5（AAA）的 44px 点击目标在本版**不作为默认要求**——
// 用户明确要求「目标人群是普通客户、文字不要太大、面板调小」，
// 实测默认档 30.0px；把字号调到「特大」档可到 40.4px。追求 44px 需要
// 同时调大 baseInnerPadding，属于另一套取向，不做默认。
func TestTouchTargetsMeetWCAG(t *testing.T) {
	newTestThemeApp(t)

	measure := func(tier string) float32 {
		withFontTier(t, tier)
		// 每档都用**新建**的按钮量：Fyne 的控件会缓存最小尺寸，复用会量到上一档的值
		return widget.NewButton("悔棋一步", nil).MinSize().Height
	}

	std := measure(fontScaleStandard)
	large := measure(fontScaleLarge)
	xlarge := measure(fontScaleXLarge)
	t.Logf("按钮最小高度：标准 %.1fpx / 大 %.1fpx / 特大 %.1fpx", std, large, xlarge)

	for _, c := range []struct {
		name string
		h    float32
	}{{"标准", std}, {"大", large}, {"特大", xlarge}} {
		if c.h < 24 {
			t.Errorf("%s档按钮高 %.1fpx < WCAG 2.5.8 的 24px 底线", c.name, c.h)
		}
	}
	if !(std < large && large < xlarge) {
		t.Errorf("三档高度必须严格递增：标准 %.1f / 大 %.1f / 特大 %.1f", std, large, xlarge)
	}
	// 默认档的界面尺寸必须与「不放大」一致：这是需求红线（字体正常就好）
	if v := FontScaleFor(fontScaleStandard); v != 1.0 {
		t.Errorf("标准档倍率 = %v，必须正好是 1.0（默认档不允许把界面撑大）", v)
	}
}

// TestScrollBarWideEnough 滚动条太细就拖不住，量一下主题给的宽度。
func TestScrollBarWideEnough(t *testing.T) {
	newTestThemeApp(t)
	withFontTier(t, fontScaleStandard)
	th := defaultTheme{}
	if w := th.Size(theme.SizeNameScrollBar); w < 10 {
		t.Errorf("标准档滚动条宽 %.1fpx，太细了（拖不住），至少要 10px", w)
	}
	if th.Size(theme.SizeNameSeparatorThickness) < 2 {
		t.Errorf("分隔线太细，几乎看不见")
	}
}

// relativeLuminance 是 WCAG 2.x 的相对亮度公式。
func relativeLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	conv := func(v uint32) float64 {
		s := float64(v) / 65535.0
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*conv(r) + 0.7152*conv(g) + 0.0722*conv(b)
}

// contrastRatio 是 WCAG 定义的对比度（1:1 ~ 21:1）。
func contrastRatio(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestA11yContrast 把「对比度」从注释变成断言。
func TestA11yContrast(t *testing.T) {
	// 面板/卡片底色，也就是绝大多数文字的实际背景
	bg := colCardBG
	cases := []struct {
		name  string
		fg    color.Color
		least float64
	}{
		{"正文 colFore", colFore, 7.0},
		{"次要文字 colForeDim（上一轮从 4.7:1 加深到这里）", colForeDim, 7.0},
		{"主色文字 colPrimary", colPrimary, 4.5},
		{"错误提示 colErr", colErr, 4.5},
		{"成功提示 colOK", colOK, 4.5},
		{"警告提示 colWarn", colWarn, 4.5},
	}
	for _, c := range cases {
		got := contrastRatio(c.fg, bg)
		t.Logf("%-46s 对比度 %.2f:1（要求 ≥%.1f）", c.name, got, c.least)
		if got < c.least {
			t.Errorf("%s 对比度只有 %.2f:1，低于要求的 %.1f:1", c.name, got, c.least)
		}
	}
}

// TestA11yForeDimIsWiredToTheme 颜色必须真的被主题用上——
// 定义了没人用的常量，是上一轮「改了颜色但界面没变」最容易踩的坑。
func TestA11yForeDimIsWiredToTheme(t *testing.T) {
	newTestThemeApp(t)
	th := defaultTheme{}
	got := th.Color(theme.ColorNamePlaceHolder, fyne.ThemeVariant(0))
	if got != color.Color(colForeDim) {
		t.Fatalf("主题的次要文字颜色 = %v，与 colForeDim %v 不一致（颜色没接线）", got, colForeDim)
	}
	if a11yForeDim != colForeDim {
		t.Fatalf("a11yForeDim = %v 与主题里实际使用的 colForeDim %v 不一致", a11yForeDim, colForeDim)
	}
}

// TestFontTierMenuItems 「设置 → 字号」三档必须齐全，且**恰好一档带勾**。
//
// 菜单勾选是老年用户判断「我现在是哪一档」的唯一线索，勾错或一个都不勾
// 等于让他们靠猜。
func TestFontTierMenuItems(t *testing.T) {
	for _, current := range []string{fontScaleStandard, fontScaleLarge, fontScaleXLarge} {
		checked := 0
		for _, name := range []string{fontScaleStandard, fontScaleLarge, fontScaleXLarge} {
			it := newFontTierItem(name, current, func() {})
			if it.Label == "" || !strings.Contains(it.Label, "字号：") {
				t.Errorf("档位 %s 的菜单项标题是 %q，看不出是字号设置", name, it.Label)
			}
			if it.Action == nil {
				t.Errorf("档位 %s 的菜单项点不动（Action 为空）", name)
			}
			if it.Checked {
				checked++
				if name != current {
					t.Errorf("当前档位是 %s，却勾在了 %s 上", current, name)
				}
			}
		}
		if checked != 1 {
			t.Errorf("当前档位 %s 时有 %d 个勾，必须恰好 1 个", current, checked)
		}
	}
}

// TestNoMissingGlyphRunes 守住一条实测教训：本机字体渲染不了某些符号，
// 界面上会变成空心方框（tofu），用户看到的就是一串「□□□」。
//
// 实测复现：状态栏「（设置 → 字号 可再改）」里的 U+2192 显示成方框；
// 新手指南里当作分隔用的 U+00B7（·）同样显示成方框。
// 与其等用户看到方框再回来查，不如让这些字符根本进不了代码里的字符串——
// 注意只管字符串字面量，注释里随便写（注释不上屏）。
func TestNoMissingGlyphRunes(t *testing.T) {
	banned := map[rune]string{
		'\u2192': "→（右箭头）",
		'\u2190': "←（左箭头）",
		'\u2191': "↑（上箭头）",
		'\u2193': "↓（下箭头）",
		'\u00b7': "·（间隔点）",
	}

	fset := token.NewFileSet()
	// 只扫**非测试**文件：测试里的字符串不上屏（本文件自己就要写出这些字符来举例）
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("解析本包源码失败：%v", err)
	}

	found := 0
	for _, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				for _, r := range s {
					if what, bad := banned[r]; bad {
						found++
						t.Errorf("%s:%d 字符串里出现 %s（U+%04X）——该字形在本机字体里缺失，界面上会显示成方框；请改用中文描述",
							fileName, fset.Position(lit.Pos()).Line, what, r)
					}
				}
				return true
			})
		}
	}
	if found > 0 {
		t.Errorf("共发现 %d 处缺失字形，见上面逐条位置", found)
	}
}

// TestBestMovePanelDetailsFold 「引擎详细数据」必须默认收起、点了能展开、再点能收起。
//
// 这是「界面简单易操作」的核心改动之一：首屏只留「引擎让你走哪一步」「现在谁好」，
// 深度/节点/速度/耗时、主变例、思考限制都藏在这个折叠后面。
func TestBestMovePanelDetailsFold(t *testing.T) {
	newTestThemeApp(t)

	p := NewBestMovePanel(func() {}, func() {})
	if p.details == nil || p.btnDetail == nil {
		t.Fatal("详细数据折叠区没有建出来")
	}
	if p.details.Visible() {
		t.Error("「引擎详细数据」默认必须是收起的——不该一打开就把这些数字糊在用户脸上")
	}
	if p.btnDetail.OnTapped == nil {
		t.Fatal("「引擎详细数据」按钮点不动")
	}
	p.btnDetail.OnTapped()
	if !p.details.Visible() {
		t.Error("点了「引擎详细数据」之后没有展开")
	}
	p.btnDetail.OnTapped()
	if p.details.Visible() {
		t.Error("再点一次没有收起")
	}
}

// TestApplyFontScaleRescalesCanvasText 换档时必须真的把界面上的 canvas.Text 改掉。
//
// 这是上一轮留下的半成品里最要命的一环：主题只覆盖 Fyne 内建控件，
// 工程里几十处写死字号的 canvas.Text（面板标题、状态栏、棋盘文字）不归主题管，
// 不重新换算就会「按钮变大了、文字没变大」。
func TestApplyFontScaleRescalesCanvasText(t *testing.T) {
	app := newTestThemeApp(t)
	withFontTier(t, fontScaleStandard)

	mk := func(role textRole) *canvas.Text {
		txt := canvas.NewText("测试", colFore)
		txt.TextSize = textSize(role)
		return txt
	}
	title := mk(textTitle)
	small := mk(textSmall)
	body := mk(textBody)

	// 故意造一个「不属于任何角色」的字号：换档时必须原样保留
	// （棋盘棋子文字这类尺寸是跟着棋盘大小算的，不属于字号档位体系）
	custom := canvas.NewText("車", colFore)
	custom.TextSize = 37.5

	w := test.NewWindow(container.NewVBox(title, body, small, custom))
	t.Cleanup(w.Close)

	// —— 切到特大 ——
	old := FontScale()
	SetFontScale(FontScaleFor(fontScaleXLarge))
	ApplyFontScale(app, old)

	if !nearly(title.TextSize, textSize(textTitle)) {
		t.Errorf("标题字号 = %v，应为 %v", title.TextSize, textSize(textTitle))
	}
	if !nearly(body.TextSize, textSize(textBody)) {
		t.Errorf("正文字号 = %v，应为 %v", body.TextSize, textSize(textBody))
	}
	if !nearly(small.TextSize, textSize(textSmall)) {
		t.Errorf("次要文字号 = %v，应为 %v", small.TextSize, textSize(textSmall))
	}
	if !nearly(custom.TextSize, 37.5) {
		t.Errorf("非档位字号（%v）被误改成了 %v", 37.5, custom.TextSize)
	}

	// —— 再切回标准档：必须能原路回去，不能每切一次大一点 ——
	old2 := FontScale()
	SetFontScale(FontScaleFor(fontScaleStandard))
	ApplyFontScale(app, old2)
	if !nearly(title.TextSize, roleBase(textTitle)) {
		t.Errorf("切回标准档后标题字号 = %v，应为 %v（换档出现了漂移）", title.TextSize, roleBase(textTitle))
	}
}
