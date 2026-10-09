// Command xiangqi 是「象棋强软」的入口程序。
//
// 定位（务必先理解，再改动）：
//
//	本软件**不是人机对弈游戏**，而是两件事：
//	  A. 引擎桥接分析 —— 用户把第三方软件的最新着法摆到本软件棋盘上，
//	     内置 Pikafish 引擎深度思考给出最强应对，用户一键复制着法走回第三方软件；
//	  B. 引擎自动对战 —— 加载两个引擎批量对弈，统计胜负与性能并生成报告。
//
// 明确不做：人机对弈游戏、AI 评语、复盘数据库、杀法/残局训练营、联机对战、云同步。
//
// 构建：
//
//	go build -ldflags "-H windowsgui -s -w" -o xiangqi.exe
package main

import "xiangqi/ui"

func main() {
	app := ui.New()
	app.Setup()
	app.Run()
}
