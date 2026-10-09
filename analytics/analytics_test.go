package analytics

import (
	"math"
	"testing"
)

// TestWinRateFormula 校验需求指定的胜率公式：
//
//	winrate% = 50 + 50*(2/(1+exp(-0.00368208*cp)) - 1)
func TestWinRateFormula(t *testing.T) {
	cases := []struct {
		cp   float64
		want float64
	}{
		{0, 50},
		{100, 50 + 50*(2/(1+math.Exp(-0.00368208*100))-1)},
		{-100, 50 + 50*(2/(1+math.Exp(0.00368208*100))-1)},
		{500, 50 + 50*(2/(1+math.Exp(-0.00368208*500))-1)},
	}
	for _, c := range cases {
		got := WinRate(c.cp)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("WinRate(%.0f) = %.6f，期望 %.6f", c.cp, got, c.want)
		}
	}
	// 单调性与值域
	if WinRate(-100000) < 0 || WinRate(100000) > 100 {
		t.Error("胜率超出 0~100 值域")
	}
	if !(WinRate(-300) < 50 && WinRate(300) > 50) {
		t.Error("胜率不满足单调性")
	}
	// 需求验收点：红方多一車的典型分值（约 +900 厘兵）胜率应远高于 70%
	if WinRate(900) <= 70 {
		t.Errorf("+900 厘兵的胜率 = %.2f%%，应 > 70%%", WinRate(900))
	}
	if WinRate(-900) >= 30 {
		t.Errorf("-900 厘兵的胜率 = %.2f%%，应 < 30%%", WinRate(-900))
	}
}

// TestRedWinRatePerspective 校验「走子方视角」到「红方视角」的换算。
func TestRedWinRatePerspective(t *testing.T) {
	redGood := Score{CP: 500, Valid: true}
	if got := RedWinRate(redGood, 0); got <= 70 {
		t.Errorf("红方走子且 +500 时红方胜率 = %.2f%%，应 > 70%%", got)
	}
	// 同一分值若由黑方走子给出，则对红方而言是劣势
	if got := RedWinRate(redGood, 1); got >= 30 {
		t.Errorf("黑方走子且 +500 时红方胜率 = %.2f%%，应 < 30%%", got)
	}
	// 将死分值直接取 100 / 0
	if got := RedWinRate(Score{Mate: true, N: 3, Valid: true}, 0); got != 100 {
		t.Errorf("红方 3 步将死时红方胜率 = %.1f%%，应为 100%%", got)
	}
	if got := RedWinRate(Score{Mate: true, N: -3, Valid: true}, 0); got != 0 {
		t.Errorf("红方被杀时红方胜率 = %.1f%%，应为 0%%", got)
	}
}

// TestSoftmaxSumIs100 校验温度 softmax 的概率和恒为 100%（需求验收点：误差 < 1%）。
func TestSoftmaxSumIs100(t *testing.T) {
	scores := []float64{35, 20, 10, 5, 0, -10, -30, -60}
	for _, temp := range []float64{40, 120, 300} {
		p := Softmax(scores, temp)
		sum := 0.0
		for _, v := range p {
			sum += v
		}
		if math.Abs(sum-100) > 0.001 {
			t.Errorf("T=%.0f 时概率和 = %.6f%%，误差超过 0.001%%", temp, sum)
		}
		if len(p) != len(scores) {
			t.Errorf("T=%.0f 时返回条数 = %d，期望 %d", temp, len(p), len(scores))
		}
	}
}

// TestTemperatureChangesDistribution 校验 T=40 与 T=120 的概率分布明显不同，
// 且 T 越小分布越集中（最优着法概率更大）。
func TestTemperatureChangesDistribution(t *testing.T) {
	scores := []float64{35, 20, 10, 5, 0, -10, -30, -60}
	p40 := Softmax(scores, 40)
	p120 := Softmax(scores, 120)
	if math.Abs(p40[0]-p120[0]) < 0.01 {
		t.Errorf("T=40 与 T=120 的首位概率几乎相同（%.4f vs %.4f），温度未生效", p40[0], p120[0])
	}
	if p40[0] <= p120[0] {
		t.Errorf("T 越小最优着法概率应越大：T=40 → %.4f%%，T=120 → %.4f%%", p40[0], p120[0])
	}
	// 差值应“明显”：首位概率差至少 1 个百分点
	if p40[0]-p120[0] < 1.0 {
		t.Errorf("温度对分布影响过小：T=40 → %.4f%%，T=120 → %.4f%%", p40[0], p120[0])
	}
}

// TestSoftmaxMateDominates 校验将死候选几乎独占概率。
func TestSoftmaxMateDominates(t *testing.T) {
	values := []float64{
		Score{Mate: true, N: 3, Valid: true}.Value(),
		Score{CP: 100, Valid: true}.Value(),
		Score{CP: -50, Valid: true}.Value(),
	}
	p := Softmax(values, 120)
	if p[0] < 99.9 {
		t.Errorf("将死候选概率 = %.4f%%，应接近 100%%", p[0])
	}
}

// TestSoftmaxDegenerate 校验空输入与零温度的健壮性。
func TestSoftmaxDegenerate(t *testing.T) {
	if got := Softmax(nil, 120); len(got) != 0 {
		t.Errorf("空输入应返回空切片，实际 %v", got)
	}
	p := Softmax([]float64{1, 2, 3}, 0)
	sum := 0.0
	for _, v := range p {
		sum += v
	}
	if math.Abs(sum-100) > 0.001 {
		t.Errorf("T=0 时应回退到默认温度并使概率和为 100%%，实际 %.4f", sum)
	}
}
