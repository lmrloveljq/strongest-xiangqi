package ui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2/theme"
)

// 皮肤（主题）相关的回归测试。
//
// 为什么这些必须是测试：
//   - 换肤色最容易"看着还行、其实看不清"——对比度必须由断言守住，而不是靠肉眼；
//   - 换肤必须是**整体**生效（调色板 + 棋盘皮肤 + 标题栏取向一起换），漏一半的表现是
//     "按钮变了、棋盘还是旧配色"，这种半截换肤只有断言能拦住；
//   - 底纹必须不透明且确定性 —— 前者防"露出窗口黑底"，后者防重绘闪烁。

// TestThemePalettesMeetContrast 两套皮肤都要过同一套 WCAG 门槛。
//
// 门槛沿用 a11y_test.go 的既有标准：正文/次要文字对卡片底 ≥7:1（AAA），
// 主色/错误/成功/警告 ≥4.5:1（AA）。此外**文字对面板底与菜单栏底色也要 ≥4.5:1** ——
// 菜单栏是最容易出问题的地方（用户实测的"菜单栏一条黑"就是它）。
func TestThemePalettesMeetContrast(t *testing.T) {
	for _, name := range ThemeNamesOrdered() {
		p := ActiveTheme(name).Palette()
		cases := []struct {
			what string
			fg   color.Color
			bg   color.Color
			want float64
		}{
			{"正文/卡片", p.fore, p.cardBG, 7.0},
			{"次要文字/卡片", p.foreDim, p.cardBG, 7.0},
			{"主色/卡片", p.primary, p.cardBG, 4.5},
			{"错误/卡片", p.err, p.cardBG, 4.5},
			{"成功/卡片", p.ok, p.cardBG, 4.5},
			{"警告/卡片", p.warn, p.cardBG, 4.5},
			{"正文/面板", p.fore, p.panelBG, 4.5},
			{"次要文字/面板", p.foreDim, p.panelBG, 4.5},
			{"正文/菜单栏底色", p.fore, p.windowBG, 4.5},
			{"次要文字/菜单栏底色", p.foreDim, p.windowBG, 4.5},
			{"主按钮文字/主色", p.primaryFg, p.primary, 4.5},
		}
		for _, c := range cases {
			if got := contrastRatio(c.fg, c.bg); got < c.want {
				t.Errorf("皮肤 %s 的「%s」对比度 %.2f:1 < %.1f:1 —— 不合格的取色不能进主题",
					name, c.what, got, c.want)
			}
		}
	}
}

// TestSetActiveThemeSwapsEverything 换肤必须整体生效。
func TestSetActiveThemeSwapsEverything(t *testing.T) {
	defer SetActiveTheme(ThemeClassic) // 恢复默认，避免污染同包其它测试

	classicFore, classicPlate := colFore, currentSkin().Plate
	if p := SetActiveTheme(ThemeInkGold); p.Name() != ThemeInkGold {
		t.Fatalf("SetActiveTheme(%s) 返回了 %q", ThemeInkGold, p.Name())
	}
	if colFore == classicFore {
		t.Fatal("换肤后正文颜色没变：调色板没有写回包级变量（界面代码读的就是这些变量）")
	}
	if currentSkin().Plate == classicPlate {
		t.Fatal("换肤后棋盘底板没变：棋盘皮肤没跟着换（会变成半截换肤）")
	}
	if !currentThemeIsDark() {
		t.Fatal("墨玉金被判定成浅色皮肤：系统标题栏不会切成深色")
	}
	// 主题对象本身也要能取到新颜色（控件重绘走这条路径），而且菜单栏底色必须不透明
	th := ActiveTheme(ThemeInkGold).Theme()
	mb := th.Color(theme.ColorNameBackground, theme.VariantDark)
	if mb == nil {
		t.Fatal("主题的 Color(ColorNameBackground) 返回 nil")
	}
	if _, _, _, a := mb.RGBA(); a == 0 {
		t.Fatal("菜单栏底色是全透明的：菜单栏会画在窗口清屏色上（用户实测的『菜单栏一条黑』）")
	}

	SetActiveTheme(ThemeClassic)
	if colFore != classicFore || currentSkin().Plate != classicPlate {
		t.Fatal("切回经典皮肤后颜色没有还原")
	}
	if currentThemeIsDark() {
		t.Fatal("经典皮肤被判定成深色")
	}
}

// TestBackgroundTextureIsOpaqueAndDeterministic 底纹必须满不透明、同一坐标永远同色、
// 且四周比中心暗（"把视线收到棋盘上"的效果）。
func TestBackgroundTextureIsOpaqueAndDeterministic(t *testing.T) {
	defer SetActiveTheme(ThemeClassic)
	SetActiveTheme(ThemeInkGold)
	px := backgroundPixel(currentBackground())

	a, b := px(10, 20, 400, 300), px(10, 20, 400, 300)
	if a != b {
		t.Fatalf("同一坐标两次取色不同（%v vs %v）：底纹会随重绘闪烁", a, b)
	}
	if _, _, _, al := a.RGBA(); al != 0xFFFF {
		t.Fatalf("底纹像素 alpha = %d，应当完全不透明（否则会露出窗口清屏黑）", al)
	}
	center := px(200, 150, 400, 300)
	corner := px(2, 2, 400, 300)
	cl, _, _, _ := center.RGBA()
	kl, _, _, _ := corner.RGBA()
	if kl > cl {
		t.Fatalf("角落比中心亮（%d > %d）：四周压暗没生效", kl, cl)
	}
}

// TestThemeRegistryHasBothSkins 皮肤表必须同时有经典与墨玉金，且名字与 config 常量一致。
func TestThemeRegistryHasBothSkins(t *testing.T) {
	for _, want := range []string{ThemeClassic, ThemeInkGold} {
		if _, ok := themeRegistry[want]; !ok {
			t.Fatalf("皮肤表里没有 %q（设置菜单里会少一项，config 里写了也会被回退）", want)
		}
		if ActiveTheme(want).Name() != want {
			t.Fatalf("ActiveTheme(%q) 拿到的不是自己", want)
		}
		if ThemeLabel(want) == "" {
			t.Fatalf("皮肤 %q 没有中文名（菜单里会显示英文 id）", want)
		}
	}
	// 未知名字必须回退到经典护眼，而不是返回 nil（历史配置里可能写着已经删掉的皮肤名）
	if ActiveTheme("天知道").Name() != ThemeClassic {
		t.Fatal("未知皮肤名没有回退到经典护眼")
	}
}
