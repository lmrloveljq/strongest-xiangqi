package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// 本文件是界面统一风格层（卡片 / 条 / 斑马纹）。
//
// 目标（用户反馈「布局和侧边栏不美观」「看不出高级感」）：
//   - 右侧信息是一张张**卡片**：层次底色 + 圆角 + 轻投影（深色皮肤下不用硬描边，
//     靠明度差分层更像专业软件；浅色皮肤保留一根极细的描边）；
//   - 记谱行加**斑马纹**，长列表一眼能分清行；
//   - 颜色与圆角**全部**来自当前皮肤（theme.go 的 palette），这里不写死任何一个值 ——
//     换肤时这些卡片会跟着整体变，不会出现半截换肤。

// cardBox 把内容包成一张卡片：层次底色 + 圆角 +（按皮肤）细描边或轻投影。
func cardBox(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(colCardBG)
	bg.CornerRadius = colCardRadius
	if colCardLine.A != 0 {
		bg.StrokeColor = colCardLine
		bg.StrokeWidth = 1
	}
	if colCardShadow.A != 0 {
		// 投影 = 层次感的主要来源（Material 的 elevation 思路）：
		// 半径不大、偏移很小，只为让卡片"浮"起来一点点，不喧宾夺主。
		bg.Shadow = canvas.Shadow{
			Color:      colCardShadow,
			BlurRadius: 12,
			Offset:     fyne.NewPos(0, 2),
			Variant:    canvas.DropShadow,
		}
	}
	return container.NewStack(bg, container.NewPadded(content))
}

// barBox 给内容铺一条底色（用于底部状态栏这类「条」）。
func barBox(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(colBarBG)
	return container.NewStack(bg, container.NewPadded(content))
}

// zebra 按行号给内容垫一层隔行底色（奇数行才有）。
func zebra(no int, content fyne.CanvasObject) fyne.CanvasObject {
	if no%2 == 0 {
		return content
	}
	bg := canvas.NewRectangle(colRowAlt)
	return container.NewStack(bg, content)
}

// scrollPadRight 给滚动内容右侧预留滚动条宽度。
//
// 为什么必须有：面板里「线程 / 哈希 / 候选概率」这些数值都是**右对齐**的，
// 而 container.VScroll 的滚动条浮在内容右缘上——不预留的话，数值会被滚动条
// 压住甚至切掉半个字（用户看到「4 线程」「33.0%」被裁）。
func scrollPadRight(content fyne.CanvasObject) fyne.CanvasObject {
	sp := canvas.NewRectangle(color.Transparent)
	sp.SetMinSize(fyne.NewSize(16, 1))
	return container.NewBorder(nil, nil, nil, sp, content)
}
