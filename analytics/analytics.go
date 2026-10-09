// Package analytics 提供「引擎分值 → 胜率」与「多候选分值 → 概率分布」的换算。
//
// ====================== 胜率公式及其近似性 ======================
//
// 中国象棋没有国际象棋那样的官方 WDL（胜/和/负）统计模型，也没有公认的
// 「厘兵 → 胜率」标定表。本包采用与国际象棋引擎社区通行做法一致的
// 逻辑斯蒂（logistic）近似模型：
//
//	winrate(cp) = 50 + 50 * ( 2/(1+exp(-K*cp)) - 1 )
//	            = 100 / (1 + exp(-K*cp))                （两式恒等）
//
//	其中 K = 0.00368208，cp 为引擎输出的分值（单位「厘兵」，1 兵 = 100 厘兵）。
//	该 K 值来自国际象棋引擎的常见标定（等价于每 100 厘兵约 +9.6% 胜率），
//	象棋子力价值分布与国象不同，因此：
//
//	  * 该胜率是**近似参考值**，不是统计学意义上的真实胜率；
//	  * cp = 0 时为 50%，cp → +∞ 时趋近 100%，cp → -∞ 时趋近 0%，单调且对称；
//	  * 将死（score mate N）直接取 100% / 0%，不经过该公式。
//
// ====================== 候选概率（温度 softmax） ======================
//
//	p_i = exp(s_i / T) / Σ_j exp(s_j / T)
//
//	s_i 为第 i 条候选的分值，T 为「温度」参数（默认 120，可调 40~300）。
//	T 越大分布越平坦（各候选概率接近），T 越小分布越尖锐（最优着法概率越大）。
//	为保证数值稳定，实现中先减去最大分值：exp((s_i - s_max)/T)，结果不变。
package analytics

import "math"

// WinRateK 是胜率公式中的常数 K。
const WinRateK = 0.00368208

// MateScoreValue 是把「将死分值」折算成厘兵时使用的量级。
// 取 200000 以保证在任意温度下将死候选的概率都趋近 100%。
const MateScoreValue = 200000.0

// Score 表示引擎给出的一个分值。
type Score struct {
	Mate  bool // 是否为将死分值
	N     int  // 将死步数：>0 表示「本方 N 步内将死对方」，<0 表示「被对方将死」
	CP    int  // 厘兵分值（Mate 为 true 时该字段无意义）
	Valid bool // 是否已收到过分值
}

// Value 把分值折算为用于 softmax 的连续数值。
func (s Score) Value() float64 {
	if s.Mate {
		sign := 1.0
		if s.N < 0 {
			sign = -1.0
		}
		// 步数越少分值越大，保证「更快将死」排在前面
		return sign * (MateScoreValue - float64(absInt(s.N))*100)
	}
	return float64(s.CP)
}

// WinRate 由「某方视角的厘兵分值」估算该方胜率（0~100）。
func WinRate(cp float64) float64 {
	return 100.0 / (1.0 + math.Exp(-WinRateK*cp))
}

// WinRateScore 由 Score 估算「该分值所属一方」的胜率。
func WinRateScore(s Score) float64 {
	if s.Mate {
		if s.N > 0 {
			return 100
		}
		if s.N < 0 {
			return 0
		}
		return 50
	}
	return WinRate(float64(s.CP))
}

// RedWinRate 把「走子方视角」的分值换算成红方胜率。
//
// UCI/UCCI 的 score 均以**走子方**为基准，故黑方走子时需要取负号。
func RedWinRate(s Score, sideToMove int) float64 {
	// sideToMove: rules.Red = 0, rules.Black = 1
	if sideToMove == 0 {
		return WinRateScore(s)
	}
	neg := Score{Valid: s.Valid, CP: -s.CP}
	if s.Mate {
		neg.Mate = true
		neg.N = -s.N
	}
	return WinRateScore(neg)
}

// Softmax 把候选分值按温度 T 转成概率（百分比，总和为 100）。
//
// 返回的切片与输入等长；若输入为空或 T <= 0，返回全零。
func Softmax(scores []float64, temp float64) []float64 {
	n := len(scores)
	out := make([]float64, n)
	if n == 0 {
		return out
	}
	if temp <= 0 {
		temp = 120
	}
	maxS := scores[0]
	for _, s := range scores {
		if s > maxS {
			maxS = s
		}
	}
	sum := 0.0
	for i, s := range scores {
		v := math.Exp((s - maxS) / temp)
		out[i] = v
		sum += v
	}
	if sum <= 0 {
		return out
	}
	for i := range out {
		out[i] = out[i] / sum * 100.0
	}
	return out
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
