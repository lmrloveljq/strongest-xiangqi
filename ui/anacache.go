package ui

import (
	"fmt"

	"xiangqi/engine"
	"xiangqi/rules"
)

// 本文件实现「局面分析缓存」。
//
// 用户要求（原话）：
//
//	点击跳转局面时，局面分析要回退到刚才那个位置，但是用缓存记录全局分析，
//	不要跳转时重新分析。
//
// 之前每次点记谱里的某一步，都会重新向引擎发一次搜索请求：既卡（要等引擎
// 重新算），又乱（结果回来时局面可能已经又变了，主变例/胜率对不上，
// 还会往走势曲线上多塞点）。现在按局面缓存整局的分析结果：
//   - 跳到一个算过的局面 → 直接回放缓存，引擎一动不动；
//   - 跳到没算过的局面 → 才真正请求引擎；
//   - 引擎算完 → 结果写进缓存，之后再跳回同一局面就是瞬时的。

// anaKey 计算局面缓存键。
//
// 键必须包含所有「会影响结果内容」的输入：局面本身 + MultiPV + 思考限制 +
// 温度（温度只影响 softmax 概率的展示，但展示也属于结果的一部分）。
func (a *App) anaKey(g *rules.Game) string {
	mode := a.cfg.TimeMode
	limitVal := a.cfg.MoveTimeMS
	if mode == "depth" {
		limitVal = a.cfg.Depth
	}
	// 【缺陷修复】键里必须带上**引擎身份**：换引擎之后同一个局面的结果完全不同，
	// 原来不带 SelfEngine，切了引擎还会命中旧引擎的缓存，右侧显示的是别的引擎的结论。
	return fmt.Sprintf("%s|eng%s|pv%d|%s%d|T%d",
		g.Board.FEN(), a.cfg.SelfEngine, a.cfg.MultiPV, mode, limitVal, a.cfg.Temperature)
}

const anaCacheMax = 512

// anaCacheGet 取缓存结果。
func (a *App) anaCacheGet(key string) (engine.Result, bool) {
	if key == "" {
		return engine.Result{}, false
	}
	a.anaCacheMu.Lock()
	defer a.anaCacheMu.Unlock()
	r, ok := a.anaCache[key]
	return r, ok
}

// anaCachePut 写缓存（超过上限时整体清空，避免无限增长）。
func (a *App) anaCachePut(key string, res engine.Result) {
	if key == "" {
		return
	}
	a.anaCacheMu.Lock()
	defer a.anaCacheMu.Unlock()
	if a.anaCache == nil {
		a.anaCache = map[string]engine.Result{}
	}
	if len(a.anaCache) >= anaCacheMax {
		a.anaCache = map[string]engine.Result{}
	}
	a.anaCache[key] = res
}

// anaCacheClear 清空缓存（换引擎 / 改参数时调用）。
func (a *App) anaCacheClear() {
	a.anaCacheMu.Lock()
	a.anaCache = map[string]engine.Result{}
	a.anaCacheMu.Unlock()
}

// anaCacheLen 当前缓存条目数（XQ_DEBUG 取证用）。
func (a *App) anaCacheLen() int {
	a.anaCacheMu.Lock()
	defer a.anaCacheMu.Unlock()
	return len(a.anaCache)
}

// renderAnalysisFromCacheOnly 只用缓存刷新分析面板，命中不了就不打扰引擎。
//
// 用于「点击记谱跳转局面」：用户明确要求「不要跳转时重新分析」。
// 每一个真正走过的局面在走子后都会自动分析并进缓存，所以正常对局中
// 跳转永远能命中缓存，跳转因此没有任何引擎等待。
func (a *App) renderAnalysisFromCacheOnly() {
	g := a.game
	if a.viewGame != nil {
		g = a.viewGame
	}
	a.anaGame = g
	key := a.anaKey(g)
	a.anaKeyCur = key
	if res, ok := a.anaCacheGet(key); ok {
		a.renderResult(res, true)
		a.setEngineState("引擎：缓存回放（未重新思考）", colForeDim)
		return
	}
	a.best.SetStatus("该局面还没有分析结果（走一步后会自动分析并进缓存）")
	a.cands.SetCandidates(nil, nil, nil, nil, nil, nil)
	a.setEngineState("引擎：该局面未分析", colForeDim)
}
