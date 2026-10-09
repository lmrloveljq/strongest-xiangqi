package ui

import (
	"image/color"
	"strings"
	"testing"

	"xiangqi/engine"
	"xiangqi/rules"
)

// 面板格式回归测试。
//
// 这些字符串是「照同类软件平替」的结果，属于对外表现的一部分：
// 一旦有人顺手把格式改回去，用户从别的软件切过来就对不上了，
// 所以格式本身要有测试守着。

// TestThinkDetailLineFormat 思考细节行必须是**中文可读**的完整统计。
//
// 用户要求「显示一部分能看得懂的参数，比如引擎后续计算的深度和计算时间、计算速度」：
// 字段顺序沿用同类软件（深度 → 分数 → 用时 → 速度 → 节点），
// 但不再出现 NPS / K 这类缩写。
func TestThinkDetailLineFormat(t *testing.T) {
	got := thinkDetailLine(18, "+22", 2761505, 2761505, 1000, 380)
	for _, must := range []string{"深度: 18 层", "分数: +22", "用时: 1.0 秒", "速度: 2,761 千节点/秒", "算过 2,761,505 个局面", "哈希占用: 38%"} {
		if !strings.Contains(got, must) {
			t.Errorf("思考细节行缺少 %q：%s", must, got)
		}
	}
	// 字段顺序也照抄：深度 → 分数 → 用时 → 速度
	if strings.Index(got, "深度") > strings.Index(got, "分数") ||
		strings.Index(got, "分数") > strings.Index(got, "用时") ||
		strings.Index(got, "用时") > strings.Index(got, "速度") {
		t.Errorf("字段顺序不对：%s", got)
	}
	// 不许再出现看不懂的缩写
	if strings.Contains(got, "NPS") || strings.Contains(got, "K  时间") {
		t.Errorf("统计行里还留着 NPS/K 这类缩写：%s", got)
	}

	// 将杀：不能同时出现「分数:」，措辞要与同类软件一致
	mate := thinkDetailLine(12, "绝杀 3 步", 0, 0, 500, 0)
	if !strings.Contains(mate, "绝杀") {
		t.Errorf("将杀局面没有用「绝杀」措辞：%s", mate)
	}
	if strings.Contains(mate, "分数:") {
		t.Errorf("将杀局面不应再出现「分数:」：%s", mate)
	}

	// 无分数时必须给占位符，不能留空或出现 "分数: "
	none := thinkDetailLine(0, "", 0, 0, 0, 0)
	if !strings.Contains(none, "分数: —") {
		t.Errorf("无分数时的占位不对：%s", none)
	}
}

// TestCandMetaLineIsPlainChinese 变招行展开后的引擎统计也要说人话。
func TestCandMetaLineIsPlainChinese(t *testing.T) {
	line := engine.InfoLine{Depth: 18, SelDepth: 24, Nodes: 2761505, NPS: 2761505, TimeMS: 1000}
	got := candMetaLine(line)
	for _, must := range []string{"深度 18 层", "最远算到 24 层", "用时 1.0 秒", "速度 2,761 千节点/秒", "算过 2,761,505 个局面"} {
		if !strings.Contains(got, must) {
			t.Errorf("变招统计行缺少 %q：%s", must, got)
		}
	}
	// 没有任何数据时返回空串（调用方会显示占位）
	if s := candMetaLine(engine.InfoLine{}); s != "" {
		t.Errorf("无数据时应返回空串，实际 %q", s)
	}
}

// TestEvalBarTextKeepsTwoDecimals 局势条胜率必须保留两位小数。
//
// 用户要求「胜率精确到两位小数」——曾经是 %.0f（整数），四舍五入后
// 51.56% 会显示成 52%，两边的百分比还会出现 52+48=99 这种对不上的观感。
func TestEvalBarTextKeepsTwoDecimals(t *testing.T) {
	got := evalBarText(51.56, true, "-17")
	if !strings.Contains(got, "红 51.56%") {
		t.Errorf("红方胜率没保留两位小数：%s", got)
	}
	if !strings.Contains(got, "48.44% 黑") {
		t.Errorf("黑方胜率没保留两位小数（应为 100-51.56=48.44）：%s", got)
	}
	if !strings.Contains(got, "-17") {
		t.Errorf("分值没拼进去：%s", got)
	}
	if s := evalBarText(50, false, ""); s != "未分析" {
		t.Errorf("未分析状态应显示「未分析」，实际 %q", s)
	}
}

// TestMoveTextColorBySide 着法分色：红方着法红字，黑方着法正文色。
//
// 用户要求「该红棋走时文字变成红色，黑棋走时颜色不变」；
// 记谱与变招都按这条规则上色，所以颜色映射要单独钉住。
func TestMoveTextColorBySide(t *testing.T) {
	if got := moveTextColor(rules.Red); got != color.Color(colMoveRed) {
		t.Errorf("红方着法颜色 = %v，应为 colMoveRed %v", got, colMoveRed)
	}
	if got := moveTextColor(rules.Black); got != color.Color(colFore) {
		t.Errorf("黑方着法颜色 = %v，应为正文色 colFore %v", got, colFore)
	}
}

// TestCandidateDetailCollapsedByDefault 变招行点开才显示后续走法，且默认是收起的。
//
// 用户要求：「点击进任意一个候选招时，要显示出这步招之后的分析走法，
// 后面的分析默认是关闭状态」。
func TestCandidateDetailCollapsedByDefault(t *testing.T) {
	newTestThemeApp(t)

	row := newCandidateRow()
	row.set(true, 1, "炮二平五", "h2e2", 34.72, "+22",
		"炮二平五 马8进7 马二进三", "深度 18 层（最远算到 24 层） · 用时 1.0 秒 · 速度 3189 千节点/秒")
	if row.detail.Visible() {
		t.Error("后续走法默认必须是收起的")
	}
	if row.meta.Visible() {
		t.Error("引擎统计默认也必须是收起的")
	}
	if !strings.Contains(row.detail.Text, "炮二平五 马8进7") {
		t.Errorf("后续走法内容不对：%q", row.detail.Text)
	}
	if !strings.Contains(row.meta.Text, "深度 18 层") {
		t.Errorf("引擎统计内容不对：%q", row.meta.Text)
	}
	if row.tap == nil || row.tap.OnTap == nil {
		t.Fatal("变招行点不动（没有绑定点击回调）")
	}
	row.tap.OnTap()
	if !row.detail.Visible() {
		t.Error("点一下应当展开后续走法")
	}
	if !row.meta.Visible() {
		t.Error("展开时应当一并显示引擎统计（深度/用时/速度）")
	}
	if !row.tap.hot {
		t.Error("展开后这一行应当有选中底色，否则看不出来在看哪一条")
	}
	row.tap.OnTap()
	if row.detail.Visible() || row.meta.Visible() {
		t.Error("再点一下应当收起")
	}
}

// TestCandidateDetailHiddenWhenNoPV 引擎没给后续走法时不展开空内容。
func TestCandidateDetailHiddenWhenNoPV(t *testing.T) {
	newTestThemeApp(t)
	row := newCandidateRow()
	row.set(true, 1, "炮二平五", "h2e2", 10, "+3", "", "")
	row.tap.OnTap()
	if row.detail.Visible() {
		t.Error("没有后续走法时不应展开空行")
	}
}

// TestFontTierLabelsAreChinese 档位标签必须是中文说明，不能只剩英文档位名。
func TestFontTierLabelsAreChinese(t *testing.T) {
	for _, tier := range []string{fontScaleStandard, fontScaleLarge, fontScaleXLarge} {
		label := fontTierLabel(tier)
		if !strings.HasPrefix(label, "标准") && !strings.HasPrefix(label, "大") && !strings.HasPrefix(label, "特大") {
			t.Errorf("档位 %s 的标签 %q 不是可读的中文说明", tier, label)
		}
	}
}
