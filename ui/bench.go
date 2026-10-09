package ui

import (
	"context"
	"fmt"
	"os"
	"time"

	"fyne.io/fyne/v2/dialog"

	"xiangqi/config"
	"xiangqi/engine"
	"xiangqi/rules"
)

// 本文件实现「跑 10 秒基准」：用当前设置跑一段固定时间的分析，
// 报出 nps / 深度 / 哈希占用 / **实测 CPU 占用**，用来验证「满配到底吃满没有」。
//
// 为什么需要它：满配（线程 = MaxStrengthThreads(逻辑核)，本机 12、哈希 4096）在纸上好看，
// 实际会被温度墙、
// 电源计划、其它进程偷核拖下来；不看实测数字就不知道有没有真的跑满。

// benchSeconds 是基准时长（固定 10 秒：够长看得出稳定值，又不至于让用户干等）。
const benchSeconds = 10

// RunBenchmark 跑一次 10 秒基准并把结果报到状态栏 + 对话框。
func (a *App) RunBenchmark() {
	c := a.analysisClient()
	if c == nil {
		dialog.ShowInformation("引擎未就绪",
			"先点工具栏「启动引擎」，或菜单「引擎」里的「启动 / 关闭引擎」，然后再跑基准。", a.win)
		return
	}
	pid := c.PID()
	if pid == 0 {
		dialog.ShowInformation("拿不到引擎进程", "引擎客户端没有暴露进程号，无法统计 CPU 占用。", a.win)
		return
	}

	// 基准要**独占**引擎：引擎正忙时 Analyze 会直接回「引擎正忙（上一次思考尚未结束）」，
	// 于是最常见的用法（开着分析就点基准）必然失败。先把在跑的那次思考停掉再跑。
	if !a.stopAnalysisForBench() {
		msg := "引擎没能停下来（上一次思考还在跑）。请稍等几秒再点一次「跑 10 秒基准」，" +
			"或先用菜单「引擎」里的「立即出招」催它交一步。"
		a.setStatusInfo("基准测试取消：" + msg)
		fmt.Fprintln(os.Stderr, "[bench] 取消：引擎正忙且未能停止")
		dialog.ShowInformation("引擎正忙", msg, a.win)
		return
	}

	// 记录基线，10 秒后再采样一次做差分
	cpu0 := EngineCPUSeconds(pid)
	wall0 := time.Now()
	a.setStatusInfo(fmt.Sprintf("基准测试中：初始局面跑 %d 秒（线程 %d / 哈希 %dMB / 候选 %d）…",
		benchSeconds, a.cfg.Threads, a.cfg.Hash, a.cfg.MultiPV))
	if a.toastText != nil {
		a.toast("基准测试开始（10 秒）")
	}

	pos := engine.Position{Startpos: true, FEN: rules.StartFEN}
	limit := engine.Limit{Mode: engine.LimitMoveTime, MoveTimeMS: benchSeconds * 1000}
	cores := config.CPUCount()

	go func() {
		res, err := c.Analyze(context.Background(), pos, limit, nil)
		wall := time.Since(wall0).Seconds()
		cpu := EngineCPUSeconds(pid) - cpu0
		cpuPct := 0.0
		if wall > 0 {
			cpuPct = cpu / wall * 100 // 100% = 一个逻辑核；16 核满载 = 1600%
		}
		a.uiDoWait(func() {
			if err != nil {
				a.setStatusInfo("基准测试失败：" + err.Error())
				fmt.Fprintf(os.Stderr, "[bench] 失败：%v\n", err)
				dialog.ShowError(err, a.win)
				return
			}
			nps := res.NPS
			if nps == 0 && res.TimeMS > 0 {
				nps = res.Nodes * 1000 / res.TimeMS
			}
			msg := fmt.Sprintf(
				"基准结果（初始局面，%d 秒）\n\n深度：%d 层\n节点：%s\n速度：%s nps\n哈希占用：%.0f%%\n"+
					"实测 CPU 占用：%.0f%%（≈ %.1f 个逻辑核 / 共 %d）\n\n设置：线程 %d、哈希 %dMB、候选 %d 条",
				benchSeconds, res.Depth, commas(res.Nodes), commas(nps), float64(res.HashFull)/10,
				cpuPct, cpuPct/100, cores,
				a.cfg.Threads, a.cfg.Hash, a.cfg.MultiPV)
			// 判定：占用低于线程数的 85% 说明没吃满
			want := float64(a.cfg.Threads) * 100
			switch {
			case cpuPct < want*0.85:
				msg += fmt.Sprintf("\n\n⚠ 只跑到 %.0f%%，低于线程数应有的 %.0f%%——"+
					"常见原因：电源计划不是「高性能」、笔记本温度墙降频、别的程序在抢核。", cpuPct, want)
			default:
				msg += "\n\n✅ 占用已接近线程上限，CPU 基本被引擎吃满。"
			}
			a.setStatusInfo(fmt.Sprintf("基准完成：%s nps，深度 %d，实测 CPU %.0f%%", commas(nps), res.Depth, cpuPct))
			dialog.ShowInformation("跑 10 秒基准", msg, a.win)
			fmt.Fprintf(os.Stderr, "[bench] nps=%d depth=%d hashfull=%d cpu=%.1f%%\n", nps, res.Depth, res.HashFull, cpuPct)
		})
	}()
}

// applyMaxStrengthToEngine 把「最强引擎模式」的参数真正下发到引擎进程上。
//
// 三步、顺序不能反：
//
//	① 重启分析引擎 —— Start 时才会 setoption Threads/Hash/MultiPV；
//	② 等新进程真的起来；
//	③ 在新进程上做优先级 + 亲和性调优。
//
// 上一版是「只调优、不重启、且在旧进程上调」：状态栏说"已绑定除第 1 核外的
// 15 个逻辑核"，实测进程还是 Normal + 全 16 核，而满配的线程数根本没下发。
func (a *App) applyMaxStrengthToEngine() {
	if a.engineOff.Load() {
		a.uiDoWait(func() {
			a.setStatusInfo(fmt.Sprintf(
				"最强引擎模式：参数已保存（线程 %d / 哈希 %dMB）；引擎当前是关闭状态，下次点「启动引擎」时按此下发",
				a.cfg.Threads, a.cfg.Hash))
		})
		return
	}
	old := 0
	if c := a.analysisClient(); c != nil {
		old = c.PID()
	}
	a.restartAnalysisEngine()
	if a.waitEnginePID(old, 20*time.Second) == 0 {
		a.uiDoWait(func() {
			a.setStatusInfo("最强引擎模式：等引擎重启超时，参数是否生效请点「跑 10 秒基准」核对")
		})
		return
	}
	note := a.ApplyEngineProcessTuning()
	a.uiDoWait(func() {
		state := "已开启"
		if !a.cfg.MaxStrength {
			state = "已关闭"
		}
		msg := fmt.Sprintf("最强引擎模式%s：引擎已按 %d 线程 / %dMB 哈希 / 候选 %d 条重启生效",
			state, a.cfg.Threads, a.cfg.Hash, a.cfg.MultiPV)
		if note != "" {
			msg += "；" + note
		}
		a.setStatusInfo(msg)
	})
}

// waitEnginePID 等一个「不同于 oldPID」的引擎进程出现（重启完成的判据）。
//
// 为什么要等：restartAnalysisEngine 是异步的（Start + isready 最长 20 秒），
// 而"已生效"这句话必须在新进程真的起来之后才说得出口 —— 上一版就是
// 话先说在了进程存在之前，于是状态栏与实测对不上。
func (a *App) waitEnginePID(oldPID int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c := a.analysisClient(); c != nil && c.Alive() {
			if pid := c.PID(); pid != 0 && pid != oldPID {
				return pid
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	return 0
}

// stopAnalysisForBench 为基准测试腾出引擎：停掉正在跑的那次思考并等它真正结束。
//
// 返回 false 表示等不到（引擎卡住或 stop 无效），调用方应当如实告诉用户而不是硬跑。
// 为什么要等：engine.Client 有 busy 标志，一次思考没结束前第二次 Analyze 会被直接拒绝。
func (a *App) stopAnalysisForBench() bool {
	if !a.analyzing.Load() {
		return true
	}
	a.anaGen.Add(1) // 在途结果作废（gen 对不上会被丢弃）
	if c := a.analysisClient(); c != nil {
		_ = c.Stop()
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if !a.analyzing.Load() {
			return true
		}
		time.Sleep(60 * time.Millisecond)
	}
	return false
}

// ApplyEngineProcessTuning 把「进程优先级 + CPU 亲和性」应用到分析引擎。
//
// 只在最强引擎模式下调用：按**实际线程数**留出互补的几个逻辑核给界面
// （12 线程 → 留 4 个核），其余核给引擎并把优先级提到「高于正常」。
func (a *App) ApplyEngineProcessTuning() string {
	c := a.analysisClient()
	if c == nil {
		return ""
	}
	pid := c.PID()
	if pid == 0 {
		return ""
	}
	if !a.cfg.MaxStrength {
		SetEnginePriority(pid, false)
		SetEngineAffinityMask(pid, 0) // 0 会被 API 拒绝，等于"不改亲和性"
		return ""
	}
	logical := config.CPUCount()
	threads := a.cfg.Threads
	reserve := logical - threads
	if reserve < 1 {
		reserve = 1
	}
	high := SetEnginePriority(pid, true)
	mask := MaxStrengthAffinityMask(logical, threads)
	aff := SetEngineAffinityMask(pid, mask)
	bound := fmt.Sprintf("引擎 %d 线程绑定到 %d 个逻辑核，留 %d 个给界面", threads, logical-reserve, reserve)
	switch {
	case high && aff:
		return "引擎进程已设为「高于正常」优先级，" + bound
	case high:
		return "引擎进程已设为「高于正常」优先级（亲和性设置被系统拒绝）"
	case aff:
		return bound + "（优先级设置被系统拒绝）"
	}
	return "进程优先级/亲和性设置都被系统拒绝（可能需要管理员权限）"
}
