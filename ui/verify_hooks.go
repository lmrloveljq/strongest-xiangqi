package ui

import (
	"fmt"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"

	"xiangqi/notation"
)

// 本文件提供「可复现验证钩子」。
//
// 设计原则：这些钩子**只在设置了对应环境变量时生效**；用户双击 xiangqi.exe
// 正常运行时一个都不会触发，因此不影响交付物的行为。
//
//	XQ_MOVES="h2e2 h9g7"     启动后自动应用一段 UCI 着法序列（等价于在「粘贴输入」里粘贴）
//	XQ_MODE=bridge|match     启动后切换到指定模式
//	XQ_PARAMS=1              启动后打开「引擎参数」对话框（v1.5：参数面板已移进菜单）
//	XQ_SHOW_ENGINES=1        启动后打开「引擎管理」窗口
//	XQ_AUTOMATCH=3           启动后自动开始 3 局对战（用当前引擎与时间控制设置）
//	XQ_FONT=standard|large|xlarge  启动后**实时**切换字号档位（默认档是 standard）
//	XQ_HELP=1                启动后打开「新手三步上手」
//	XQ_SHOT=path.png         在 XQ_SHOT_DELAY 秒后把界面自身渲染结果存为 PNG
//	XQ_SHOT_WINDOW=main|engines  截图目标窗口，默认 main
//	XQ_SHOT_DELAY=8          截图延迟秒数，默认 8
//	XQ_QUIT=5                截图后再等 5 秒自动退出（用于脚本化验证）
//	XQ_CONFIG=path.json      用指定配置文件运行（不碰用户的 config.json）
//	XQ_DIAG=1                每秒打印一次状态机快照（模式 / 轮到谁 / 棋盘可否点 / 引擎进程）
//	XQ_MAXSTRENGTH=1         3 秒后调用「最强引擎模式」开关（menu 里同一入口）
//	XQ_BENCH=1               3 秒后跑一次「10 秒基准」
//	XQ_BENCH_DELAY=15        上面那次基准的延迟秒数，默认 3
//	XQ_MOVES2="h2e2"         启动 N 秒后**再**粘贴一次着法序列（复现"同一局面第二次走到"）
//	XQ_MOVES2_DELAY=10       上面那次粘贴的延迟秒数，默认 10
//
// 为什么用 Canvas().Capture() 而不是抓屏：抓屏会经过系统的 DPI 虚拟化，
// 得到的图像与界面真实布局不是 1:1；Capture() 直接取 Fyne 画布，像素级准确。
func (a *App) applyVerifyHooks() {
	// XQ_DIAG=1：每秒一行状态机快照。
	//
	// 为什么需要：「棋盘根本动不了」这句话里藏着三种完全不同的故障——
	// ① 轮到引擎但引擎不交招（引擎/时限层）；② 棋盘被判定为不可交互（界面层）；
	// ③ 界面线程被饿死（进程层，表现为 fyne.Do 排队、这一行不再打印）。
	// 从窗口外面看不出是哪一种，所以把判定结果和引擎进程实况按时间轴打出来。
	if os.Getenv("XQ_DIAG") != "" {
		go func() {
			tk := time.NewTicker(time.Second)
			defer tk.Stop()
			start := time.Now()
			for range tk.C {
				select {
				case <-a.closing:
					return
				default:
				}
				fyne.Do(func() {
					pid, cpu := 0, 0.0
					if c := a.analysisClient(); c != nil {
						pid = c.PID()
						cpu = EngineCPUSeconds(pid)
					}
					fmt.Fprintf(os.Stderr,
						"[diag] t=%.0fs mode=%s engineOff=%v humanTurn=%v interactive=%v viewing=%d edit=%v analyzing=%v moves=%d toMove=%v pid=%d engCPU=%.1fs cfg{t=%d h=%d multi=%d limit=%s depth=%d max=%v}\n",
						time.Since(start).Seconds(), a.curMode, a.engineOff.Load(), a.isHumanTurn(),
						a.canBoardAcceptInput(), a.viewing, a.editMode, a.analyzing.Load(),
						len(a.game.Moves), a.game.Board.Side, pid, cpu,
						a.cfg.Threads, a.cfg.Hash, a.cfg.MultiPV, a.cfg.TimeMode, a.cfg.Depth, a.cfg.MaxStrength)
				})
			}
		}()
	}
	// XQ_MOVES2：延迟一段时间后再粘贴一次同一段着法序列。
	//
	// 为什么需要：人机对弈里"同一个局面第二次走到"（悔棋重走、或重新粘贴同一段序列）
	// 会命中局面分析缓存，而缓存分支只回放结果、不交招 —— 那正是"棋盘动不了"的一种形态。
	// 这个钩子把该路径变成一条可复现的命令。
	if seq2 := os.Getenv("XQ_MOVES2"); seq2 != "" {
		delay := 10
		if v := os.Getenv("XQ_MOVES2_DELAY"); v != "" {
			if k, err := strconv.Atoi(v); err == nil && k >= 0 {
				delay = k
			}
		}
		go func() {
			time.Sleep(time.Duration(delay) * time.Second)
			fyne.Do(func() {
				fmt.Fprintf(os.Stderr, "[hook] 第二次粘贴着法序列 %q（第 %d 秒）\n", seq2, delay)
				a.applySequence(seq2, true)
			})
		}()
	}
	// XQ_MAXSTRENGTH=1：走一遍「最强引擎模式」菜单项背后的同一个函数。
	if os.Getenv("XQ_MAXSTRENGTH") != "" {
		go func() {
			time.Sleep(3 * time.Second)
			fyne.Do(func() {
				fmt.Fprintf(os.Stderr, "[hook] 调用 ToggleMaxStrength（前：max=%v t=%d h=%d）\n",
					a.cfg.MaxStrength, a.cfg.Threads, a.cfg.Hash)
				a.ToggleMaxStrength()
			})
		}()
	}
	// XQ_BENCH=1：走一遍「跑 10 秒基准」菜单项背后的同一个函数（结果自己打到 stderr）。
	if os.Getenv("XQ_BENCH") != "" {
		delay := 3
		if v := os.Getenv("XQ_BENCH_DELAY"); v != "" {
			if k, err := strconv.Atoi(v); err == nil && k >= 0 {
				delay = k
			}
		}
		go func() {
			time.Sleep(time.Duration(delay) * time.Second)
			fyne.Do(func() { a.RunBenchmark() })
		}()
	}
	if seq := os.Getenv("XQ_MOVES"); seq != "" {
		a.applySequence(seq, true)
	}
	if m := os.Getenv("XQ_MODE"); m != "" {
		a.SetMode(m)
	}
	// 【v1.5 简化】原来的 XQ_TAB（切底部页签）随页签一起取消：
	// 底部只剩「粘贴输入」一件事，参数面板已移进菜单「引擎参数…」。
	// 需要脚本化打开参数对话框时用 XQ_PARAMS=1。
	if os.Getenv("XQ_PARAMS") != "" {
		go func() {
			time.Sleep(2 * time.Second)
			fyne.Do(func() { a.ShowEngineParams() })
		}()
	}
	if os.Getenv("XQ_SHOW_ENGINES") != "" {
		a.ShowEngineManager()
	}
	if os.Getenv("XQ_RESCAN") != "" {
		go func() {
			time.Sleep(1200 * time.Millisecond)
			fyne.Do(func() { a.RescanEngines(false) })
		}()
	}
	// XQ_FONT=standard|large|xlarge：启动后**实时**切换字号档位（走的是菜单同一条代码路径），
	// 用于验证「不重启也能换档」。会把切换前后的档位打到 stderr，供脚本取证。
	if v := os.Getenv("XQ_FONT"); v != "" {
		go func() {
			time.Sleep(2500 * time.Millisecond)
			fyne.Do(func() {
				before := describeFontScale()
				a.SetFontTier(v)
				fmt.Fprintf(os.Stderr, "[font] %s -> %s（实时切换，未重启）\n", before, describeFontScale())
			})
		}()
	}
	// XQ_HELP=1：启动后打开「快速上手」，用于人工核对这份说明的可读性。
	if os.Getenv("XQ_HELP") != "" {
		go func() {
			time.Sleep(2 * time.Second)
			fyne.Do(func() { a.ShowQuickStart() })
		}()
	}
	// XQ_MENUSMOKE=1：**逐个点一遍菜单里的每一项**，用于查出「点了没反应」「一点就崩」
	// 这类菜单缺陷。做三件事：
	//   1. 打印菜单结构（顶级项 + 子项数量），核对有没有漏项；
	//   2. 逐项调用 Action（每项之间停 400ms，截图/日志能看出卡在哪一项）；
	//   3. 调完一项就把弹出来的对话框收掉，避免十几层叠在一起互相遮挡。
	// 任何一项 panic 都会带着「刚点的是哪一项」打到 stderr，这就是缺陷定位。
	if os.Getenv("XQ_MENUSMOKE") != "" {
		go func() {
			time.Sleep(3 * time.Second)
			items := a.collectMenuItems()
			fmt.Fprintf(os.Stderr, "[menu] 共 %d 个菜单项\n", len(items))
			for i, it := range items {
				n := i
				item := it
				func() {
					defer func() {
						if r := recover(); r != nil {
							fmt.Fprintf(os.Stderr, "[menu] PANIC #%d %s: %v\n", n+1, item.Label, r)
						}
					}()
					fmt.Fprintf(os.Stderr, "[menu] #%d %s\n", n+1, item.Label)
					fyne.Do(func() {
						if item.Action != nil {
							item.Action()
						}
					})
				}()
				time.Sleep(400 * time.Millisecond)
				fyne.Do(func() { a.closeTopOverlay() })
				time.Sleep(150 * time.Millisecond)
			}
			fmt.Fprintf(os.Stderr, "[menu] 菜单体检结束\n")
		}()
	}
	// XQ_CANDPV=1：展开第 1 条变招的「后续走法」，用于核对点开后的显示。
	if os.Getenv("XQ_CANDPV") != "" {
		go func() {
			time.Sleep(9 * time.Second)
			fyne.Do(func() {
				if a.cands != nil && len(a.cands.rows) > 0 {
					a.cands.rows[0].toggleDetail()
					fmt.Fprintln(os.Stderr, "[candpv] 已展开第 1 条变招的后续走法")
				}
			})
		}()
	}
	// XQ_DEBUG=1：除启动时的版面打印外，12 秒后再打一次——
	// 展开变招后续走法之类的操作会让最小尺寸变化，第二次快照才反映真实状态。
	if os.Getenv("XQ_DEBUG") != "" {
		go func() {
			time.Sleep(12 * time.Second)
			fyne.Do(func() {
				fmt.Fprintln(os.Stderr, "===== 延时版面快照（钩子执行之后）=====")
				a.dumpLayout()
			})
		}()
	}
	// XQ_THINKSET=NN：**用代码走一遍真实路径**——打开思考设置、把输入框改成 NN、
	// 点「保存并立即生效」，并把每一步打到 stderr。用于定位「改了不生效」这类问题。
	if v := os.Getenv("XQ_THINKSET"); v != "" {
		go func() {
			time.Sleep(3 * time.Second)
			fyne.Do(func() {
				fmt.Fprintf(os.Stderr, "[think] 前：cfg.Depth=%d timeMode=%q\n", a.cfg.Depth, a.cfg.TimeMode)
				a.ShowThinkSettings()
			})
			time.Sleep(1 * time.Second)
			fyne.Do(func() {
				if a.thinkDepth == nil {
					fmt.Fprintln(os.Stderr, "[think] 控件为空：对话框没建出来")
					return
				}
				a.thinkDepth.SetText(v)
				fmt.Fprintf(os.Stderr, "[think] 已把输入框设为 %q（对话框内当前值 %q）\n", v, a.thinkDepth.Text)
			})
			time.Sleep(500 * time.Millisecond)
			fyne.Do(func() {
				if a.thinkMode != nil {
					a.thinkMode.SetSelected("固定深度")
				}
				if a.thinkSaveBtn != nil {
					a.thinkSaveBtn.OnTapped()
				} else {
					fmt.Fprintln(os.Stderr, "[think] 保存按钮为空")
				}
				fmt.Fprintf(os.Stderr, "[think] 后：cfg.Depth=%d timeMode=%q\n", a.cfg.Depth, a.cfg.TimeMode)
			})
		}()
	}
	// XQ_THINK=1：打开「思考设置」对话框（核对无上限深度输入框与放大后的按钮）。
	if os.Getenv("XQ_THINK") != "" {
		go func() {
			time.Sleep(2 * time.Second)
			fyne.Do(func() { a.ShowThinkSettings() })
		}()
	}
	// XQ_DETAIL=1：展开「思考细节」，用于核对统计行格式（深度 / 分数 / NPS / 时间）。
	if os.Getenv("XQ_DETAIL") != "" {
		go func() {
			time.Sleep(3 * time.Second)
			fyne.Do(func() {
				if a.best != nil && a.best.btnDetail != nil {
					a.best.btnDetail.OnTapped()
				}
			})
		}()
	}
	if n := os.Getenv("XQ_AUTOMATCH"); n != "" {
		if games, err := strconv.Atoi(n); err == nil && games > 0 {
			a.cfg.MatchGames = games
			if a.matchView != nil {
				a.matchView.gamesSlider.Value = float64(games)
				a.matchView.refreshValues()
			}
			a.setStatusInfo(fmt.Sprintf("验证钩子：%d 秒后自动开始 %d 局对战", 3, games))
			go func() {
				time.Sleep(3 * time.Second)
				fyne.Do(func() { a.matchView.Start() })
			}()
		}
	}
	if v := os.Getenv("XQ_STRESS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			go func() {
				time.Sleep(6 * time.Second)
				fyne.Do(func() { a.runStress(n) })
			}()
		}
	}
	// XQ_AUTOSPLIT=1：启动后调用「按核心数自动平均分配」按钮背后的**同一个函数**，
	// 用于在无法用真实点击触达该按钮（面板需要滚动）时验证 R5 的分配逻辑。
	// 它只改 cfg.SelfThreads / cfg.OppThreads 并 SaveConfig，不触碰任何其它状态。
	if os.Getenv("XQ_AUTOSPLIT") != "" {
		go func() {
			time.Sleep(4 * time.Second)
			fyne.Do(func() {
				if a.matchView != nil {
					a.matchView.autoSplitThreads()
					fmt.Fprintf(os.Stderr, "[hook] XQ_AUTOSPLIT 已执行 autoSplitThreads()\n")
					a.dumpLayout()
				}
			})
		}()
	}
	if path := os.Getenv("XQ_SHOT"); path != "" {
		delay := 8
		if v := os.Getenv("XQ_SHOT_DELAY"); v != "" {
			if k, err := strconv.Atoi(v); err == nil && k >= 0 {
				delay = k
			}
		}
		go func() {
			time.Sleep(time.Duration(delay) * time.Second)
			fyne.Do(func() { a.saveCanvasShot(path) })
			if v := os.Getenv("XQ_QUIT"); v != "" {
				if k, err := strconv.Atoi(v); err == nil && k >= 0 {
					time.Sleep(time.Duration(k) * time.Second)
					a.shutdown()
					fyne.Do(func() { a.fyneApp.Quit() })
				}
			}
		}()
	} else if v := os.Getenv("XQ_QUIT"); v != "" {
		if k, err := strconv.Atoi(v); err == nil && k >= 0 {
			go func() {
				time.Sleep(time.Duration(k) * time.Second)
				a.shutdown()
				fyne.Do(func() { a.fyneApp.Quit() })
			}()
		}
	}
}

func (a *App) saveCanvasShot(path string) {
	target := a.win
	if os.Getenv("XQ_SHOT_WINDOW") == "engines" && a.engMgrWin != nil {
		target = a.engMgrWin
	}
	if target == nil {
		return
	}
	img := target.Canvas().Capture()
	if img == nil {
		fmt.Fprintln(os.Stderr, "[shot] canvas capture returned nil")
		return
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[shot] create failed: %v\n", err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintf(os.Stderr, "[shot] encode failed: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "[shot] saved %s %v\n", path, img.Bounds().Size())
}

// runStress 连续 n 次「从粘贴区应用着法序列 + 一键复制最佳着法」，
// 用于验证桥接模式的高频输入不会卡住界面（对应需求「连续粘贴/复制 50 次无卡顿」）。
//
// 每次迭代都在主 goroutine 上同步执行（与用户真实操作一致），
// 因此单次耗时的最大值就是界面可能被阻塞的最长时间。
func (a *App) runStress(n int) {
	const full = "h2e2 h9g7 c3c4 i9h9 b0c2 b9c7 a0b0 a9b9 h0g2 h7h3 i0h0 b7b3"
	base, _ := notation.ParseMoveList(full)
	if len(base) == 0 {
		return
	}
	seq := make([]string, 0, len(base))
	var worst time.Duration
	start := time.Now()
	for i := 0; i < n; i++ {
		k := (i % len(base)) + 1
		seq = seq[:0]
		for _, m := range base[:k] {
			seq = append(seq, m.String())
		}
		t0 := time.Now()
		a.applySequence(strings.Join(seq, " "), true)
		a.copyBestMove()
		if d := time.Since(t0); d > worst {
			worst = d
		}
	}
	elapsed := time.Since(start)
	msg := fmt.Sprintf("压力测试：连续 %d 次「粘贴着法序列 + 复制最佳着法」总用时 %s，单次最慢 %s（平均 %s）",
		n, elapsed.Round(time.Millisecond), worst.Round(time.Millisecond),
		(elapsed / time.Duration(n)).Round(time.Microsecond))
	a.setStatusInfo(msg)
	a.toast(msg)
	fmt.Fprintf(os.Stderr, "[stress] %s\n", msg)
}
