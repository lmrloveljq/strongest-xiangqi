package ui

import (
	"math/bits"
	"testing"
)

// 满配档的线程/留核策略：这两条直接决定"引擎能跑多快"与"界面会不会被抢死"，
// 是实测调出来的参数（15 线程只跑到 1318%，12 线程 1185%，且留 4 核给界面）。

func TestMaxStrengthThreadsLeavesRoom(t *testing.T) {
	cases := []struct {
		logical int
		want    int
	}{
		{16, 12}, // 本机：留 4 个核，用 12（用户拍板）
		{8, 6},   // 每 4 核留 1 个
		{4, 3},
		{32, 12}, // 大机器走上限，不再线性加线程
		{2, 1},
		{1, 1},
	}
	for _, c := range cases {
		if got := MaxStrengthThreads(c.logical); got != c.want {
			t.Fatalf("MaxStrengthThreads(%d) = %d，期望 %d", c.logical, got, c.want)
		}
		if c.logical > 1 && MaxStrengthThreads(c.logical) >= c.logical {
			t.Fatalf("%d 逻辑核时没留出任何核给界面：会把整机占满", c.logical)
		}
	}
}

func TestMaxStrengthAffinityMaskKeepsReservedCoresFree(t *testing.T) {
	// 16 逻辑核、引擎 12 线程 → 留 bit0..3，用 bit4..15
	mask := MaxStrengthAffinityMask(16, 12)
	if mask != 0xFFF0 {
		t.Fatalf("掩码 = 0x%X，期望 0xFFF0", mask)
	}
	if bits.OnesCount64(uint64(mask)) != 12 {
		t.Fatalf("掩码里可用核数 = %d，期望 12（与线程数一致，别白绑）", bits.OnesCount64(uint64(mask)))
	}
	if mask&0x1 != 0 {
		t.Fatal("第 1 个逻辑核没留给界面")
	}

	// 引擎线程数 >= 逻辑核时也不能返回「全占」的掩码
	if m := MaxStrengthAffinityMask(4, 99); m == 0xF {
		t.Fatal("线程数超过逻辑核时仍把全部核都给了引擎：界面会被抢死")
	}
	if m := MaxStrengthAffinityMask(2, 1); m != 0 {
		t.Fatalf("2 逻辑核时返回 0x%X，应当返回 0（不改亲和性，交给系统调度）", m)
	}
}
