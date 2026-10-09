package config

import "testing"

// TestThinkProfileKeepsInfinite 每个模式自己的思考设置必须认「无限分析」。
//
// 缺陷原型：顶层 time_mode 认得 infinite，但每个模式自己的 ThinkProfile
// 只认 movetime/depth，读配置时被打回默认 movetime —— 用户选了无限分析，
// 切一次模式（甚至只是重启程序）就没了。用户看到的结论是"我明明选了，它自己变回去了"。
func TestThinkProfileKeepsInfinite(t *testing.T) {
	def := configThinkProfile(TimeModeMoveTime, 1000, 20)
	p := ThinkProfile{TimeMode: TimeModeInfinite, MoveTimeMS: 1000, Depth: 20}
	if got := normalizeThinkProfile(p, def); got.TimeMode != TimeModeInfinite {
		t.Fatalf("无限分析被打回 %q：per-mode 思考设置不认 infinite", got.TimeMode)
	}

	// 仍然要拦住真正的非法值（拼错的模式名不能进内存）
	bad := ThinkProfile{TimeMode: "天知道", MoveTimeMS: 1000, Depth: 20}
	if got := normalizeThinkProfile(bad, def); got.TimeMode != TimeModeMoveTime {
		t.Fatalf("非法模式名 = %q，期望回退到默认 %q", got.TimeMode, TimeModeMoveTime)
	}
}

// TestNormalizeKeepsInfiniteTimeMode 顶层 time_mode 同样要保住 infinite。
func TestNormalizeKeepsInfiniteTimeMode(t *testing.T) {
	c := Default()
	c.TimeMode = TimeModeInfinite
	c.Normalize()
	if c.TimeMode != TimeModeInfinite {
		t.Fatalf("顶层 time_mode = %q，期望 %q", c.TimeMode, TimeModeInfinite)
	}
}
