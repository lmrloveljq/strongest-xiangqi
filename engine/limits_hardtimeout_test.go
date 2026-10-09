package engine

import (
	"testing"
	"time"
)

// TestInfiniteLimitHardTimeoutIsNotDepthOne 无限分析不能被当成"固定深度 1 层"。
//
// 缺陷原型：Limit.Infinite 只置了标志位、没动 Mode，而 LimitDepth 的零值就是 0，
// 于是 `go infinite` 落进 LimitDepth 分支：d=0→1 → 硬超时 63 秒，
// 引擎在第 63 秒被强行 stop。表现是"无限分析"其实只算一分钟，
// 人机对弈里则是"电脑想了半天才走一步"，棋盘看着就是锁死的。
func TestInfiniteLimitHardTimeoutIsNotDepthOne(t *testing.T) {
	lim := Limit{Infinite: true}
	if got := lim.Command(ProtoUCI); got != "go infinite" {
		t.Fatalf("无限分析的命令 = %q，期望 go infinite", got)
	}
	if got := lim.HardTimeout(); got <= 10*time.Minute {
		t.Fatalf("无限分析的硬超时 = %v，太小：无限分析会被自动打断（旧版是 63 秒）", got)
	}
	if lim.HardTimeout() == (Limit{Mode: LimitDepth, Depth: 1}).HardTimeout() {
		t.Fatal("无限分析的硬超时与「固定深度 1 层」相同：说明又掉进了 LimitDepth 分支")
	}
}

// TestFiniteLimitsStillHaveHardTimeout 兜底不能把有限限时的保护一起抹掉。
func TestFiniteLimitsStillHaveHardTimeout(t *testing.T) {
	if got := (Limit{Mode: LimitMoveTime, MoveTimeMS: 2000}).HardTimeout(); got != 62*time.Second {
		t.Fatalf("每步限时 2 秒的硬超时 = %v，期望 62s（限时 + 60s 宽限）", got)
	}
	if got := (Limit{Mode: LimitDepth, Depth: 20}).HardTimeout(); got != 120*time.Second {
		t.Fatalf("固定深度 20 层的硬超时 = %v，期望 120s", got)
	}
}
