package ui

import (
	"testing"
)

// TestCurveFillsGaps 曲线必须铺满 X 轴：中间没有分析的着数按上一个已知值补齐，
// 第一个点落在中后盘时从开局起补一条水平线。
//
// 事故背景（用户实测）：人机对弈里把引擎关掉那段时间没人分析，那些着数一个点都没有，
// X 轴靠近原点的一半整片空白，看着像「曲线偏右」。
func TestCurveFillsGaps(t *testing.T) {
	c := NewCurve("test")

	// 第一个点落在第 6 着：0~5 着也要有值，否则左边一半空白
	c.AddPoint(6, 60)
	pts := c.Points()
	if len(pts) != 7 {
		t.Fatalf("首个点在第 6 着时应有 0~6 共 7 个点，实际 %d 个：%+v", len(pts), pts)
	}
	if pts[0].Ply != 0 || pts[0].Red != 60 {
		t.Errorf("开局补齐值不对：%+v", pts[0])
	}

	// 跨过一段空档（7~11 着没有分析）直接到第 12 着：中间按上一个值补齐
	c.AddPoint(12, 45)
	pts = c.Points()
	if len(pts) != 13 {
		t.Fatalf("0~12 着应共 13 个点，实际 %d 个", len(pts))
	}
	if pts[9].Ply != 9 || pts[9].Red != 60 {
		t.Errorf("空档应延续上一个已知值 60：%+v", pts[9])
	}
	if pts[12].Red != 45 {
		t.Errorf("第 12 着应是新值 45：%+v", pts[12])
	}

	// 同一步数重复添加是覆盖语义，不应产生重复点
	c.AddPoint(12, 50)
	pts = c.Points()
	if len(pts) != 13 {
		t.Fatalf("同着数覆盖后仍应是 13 个点，实际 %d 个", len(pts))
	}
	if pts[12].Red != 50 {
		t.Errorf("覆盖后第 12 着应是 50：%+v", pts[12])
	}
}
