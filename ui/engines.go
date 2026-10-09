package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"xiangqi/engine"
)

// EngineManager 是「引擎管理」面板：列出引擎库中的全部引擎，支持重新扫描、
// 添加外部引擎目录、移除、设为己方引擎 / 对手引擎、测试连接。
type EngineManager struct {
	Root fyne.CanvasObject

	app  *App
	box  *fyne.Container
	note *canvas.Text
}

// NewEngineManager 创建引擎管理面板。
func NewEngineManager(app *App) *EngineManager {
	m := &EngineManager{app: app}
	m.note = canvas.NewText("", colForeDim)
	m.note.TextSize = textSize(textSmall)

	btnRescan := widget.NewButton("重新扫描引擎", func() { app.RescanEngines(true) })
	btnRescan.Importance = widget.HighImportance
	btnAdd := widget.NewButton("添加外部引擎目录…", func() { app.AddExternalEngineDir() })
	btnOpenLib := widget.NewButton("打开引擎库目录", func() { app.OpenEngineLibraryDir() })

	head := container.NewVBox(
		sectionTitle("引擎库"),
		canvas.NewText(fmt.Sprintf("引擎库目录（相对 exe）：%s", app.EngineLibraryDir()), colForeDim),
		canvas.NewText("三步追加引擎：① 把引擎文件夹放进引擎库目录 ② 确保 exe 与权重同目录 ③ 点「重新扫描引擎」", colForeDim),
		container.NewHBox(btnRescan, btnAdd, btnOpenLib),
		m.note,
		widget.NewSeparator(),
	)
	m.box = container.NewVBox()
	scroll := container.NewVScroll(m.box)
	scroll.SetMinSize(fyne.NewSize(sz(760), sz(400)))
	m.Root = container.NewBorder(head, nil, nil, nil, scroll)
	m.Refresh()
	return m
}

// Refresh 重建引擎列表。
func (m *EngineManager) Refresh() {
	m.box.RemoveAll()
	reg := m.app.Reg()
	// 【R3】每次刷新都用 os.Stat 重新校验每个引擎的 exe 是否真的还在磁盘上，
	// 绝不读取持久化字段（engines.json 里本来就没有这个字段）。
	reg.RefreshExistence()
	list := reg.List()
	if len(list) == 0 {
		m.box.Add(canvas.NewText("引擎库为空。点击「重新扫描引擎」或「添加外部引擎目录」。", colErr))
		m.note.Text = "提示：首次运行会自动注册预置皮卡鱼目录。"
		m.note.Refresh()
		m.box.Refresh()
		return
	}
	missing := 0
	for i := range list {
		if !list[i].Executable {
			missing++
		}
		m.box.Add(m.row(list[i]))
		m.box.Add(widget.NewSeparator())
	}
	self := m.app.SelfEngineID()
	opp := m.app.OpponentEngineID()
	if missing > 0 {
		m.note.Text = fmt.Sprintf("共 %d 个引擎（%d 个文件已丢失）；己方 id=%s，对手 id=%s",
			len(list), missing, orNone(self), orNone(opp))
	} else {
		m.note.Text = fmt.Sprintf("共 %d 个引擎（全部可用）；己方 id=%s，对手 id=%s", len(list), orNone(self), orNone(opp))
	}
	m.note.Refresh()
	m.box.Refresh()
}

func orNone(s string) string {
	if s == "" {
		return "（未设置）"
	}
	return s
}

// shortPath 把过长的路径截断成「前 3 字符…末尾」，保证行内文本不会把布局顶宽
// （超长路径的 MinSize 曾把右侧按钮整排挤出窗口，见 R4）。
func shortPath(p string, maxRunes int) string {
	r := []rune(p)
	if len(r) <= maxRunes {
		return p
	}
	keep := maxRunes - 1
	head := 3
	if keep <= head {
		return string(r[:keep]) + "…"
	}
	return string(r[:head]) + "…" + string(r[len(r)-(keep-head):])
}

// row 渲染引擎库中的一行。
//
// 【R4 修复】v1.3.0 把四个操作按钮挂在 BorderLayout 的 right，而这一行又被超长
// 路径撑出很大的 MinSize，于是一行放不下时右侧按钮被推出窗口（实测：「测试连接」
// 可见、「设为己方引擎」被切一半、「设为对手引擎」「移除」完全在窗口外，
// 且容器只有纵向滚动，被裁的按钮无法到达）。
//
// 现在改为纵向排布：标题行、路径+状态行、参数行、按钮行各自成行——按钮行
// 不强制与其他内容同排，因此无论窗口多窄，四个按钮都完整可见可点。
func (m *EngineManager) row(e engine.EngineEntry) fyne.CanvasObject {
	name := canvas.NewText(e.Name, colFore)
	name.TextSize = textSize(textTitle)
	name.TextStyle = fyne.TextStyle{Bold: true}

	proto := e.Protocol
	tag := canvas.NewText("["+proto+"]", colPrimary)
	tag.TextSize = textSize(textLabel)

	title := container.NewHBox(name, tag)

	// 【R3】实时校验磁盘状态，绝不读取持久化布尔字段
	av := e.CheckExecutable()
	state := "可用"
	stateCol := colOK
	if !av.OK {
		state, stateCol = "文件已丢失", colErr
		if av.Reason != "" && av.Reason != "文件已丢失" {
			state = "不可用（" + av.Reason + "）"
		}
	}
	if e.Source == "preset" {
		state += "（预置皮卡鱼）"
	}
	st := canvas.NewText(state, stateCol)
	st.TextSize = textSize(textSmall)
	st.TextStyle = fyne.TextStyle{Bold: !av.OK}

	pathTxt := canvas.NewText(shortPath(e.Path, 76), colForeDim)
	pathTxt.TextSize = textSize(textSmall)
	pathLine := container.NewHBox(pathTxt, layoutSpacer(), st)

	id := e.ID
	btnTest := widget.NewButton("测试连接", func() { m.app.TestEngine(id) })
	btnSelf := widget.NewButton("设为己方引擎", func() {
		m.app.SetSelfEngine(id)
		m.Refresh()
	})
	btnOpp := widget.NewButton("设为对手引擎", func() {
		m.app.SetOpponentEngine(id)
		m.Refresh()
	})
	btnDel := widget.NewButton("移除", func() {
		dialog.ShowConfirm("确认移除", "确定从引擎库移除 "+e.Name+" 吗？（不会删除磁盘上的文件）", func(ok bool) {
			if ok {
				m.app.RemoveEngine(id)
				m.Refresh()
			}
		}, m.app.Window())
	})
	btns := container.NewHBox(btnTest, btnSelf, btnOpp, btnDel)

	optCount := canvas.NewText(fmt.Sprintf("已缓存引擎参数 %d 项", len(e.Options)), colForeDim)
	optCount.TextSize = textSize(textSmall)
	if !av.OK {
		// 丢失时把完整路径原样显示出来，方便用户核对到底缺哪个文件
		optCount.Text = fmt.Sprintf("已缓存引擎参数 %d 项，完整路径：%s", len(e.Options), e.Path)
	}

	return container.NewVBox(title, pathLine, optCount, btns)
}

// ---------------------------------------------------------------------------
// App 侧的引擎库操作
// ---------------------------------------------------------------------------

// RescanEngines 重新扫描引擎库与外部引擎目录。
func (a *App) RescanEngines(showDialog bool) {
	go func() {
		// 【并发缺陷修复】下面三处都在后台扫描协程里，必须回到主线程再改控件
		fyne.Do(func() { a.setStatusInfo("正在扫描引擎库…") })
		reg := a.Reg()
		rep := engine.Rescan(reg, a.baseDir, 6*time.Second, a.skipExeSet(), func(name string) {
			fyne.Do(func() { a.setStatusInfo("正在探测 " + name + " …") })
		})
		if err := reg.Save(a.engPath); err != nil {
			fyne.Do(func() { a.setStatusInfo("保存 engines.json 失败：" + err.Error()) })
		}
		msg := rep.Summary()
		if len(rep.Failed) > 0 {
			for i, f := range rep.Failed {
				if i >= 3 {
					msg += fmt.Sprintf("；另有 %d 个未列出", len(rep.Failed)-3)
					break
				}
				msg += fmt.Sprintf("；%s：无法识别为象棋引擎", baseName(f.Path))
			}
		}
		fyne.Do(func() {
			a.setStatusInfo(msg)
			if a.engMgr != nil {
				a.engMgr.Refresh()
			}
			a.syncEngineSelects()
			if showDialog {
				detail := msg
				if len(rep.Failed) > 0 {
					detail += "\n\n无法识别为象棋引擎的文件：\n"
					for _, f := range rep.Failed {
						detail += "• " + f.Path + "\n  原因：" + f.Reason + "\n"
					}
				}
				dialog.ShowInformation("引擎扫描完成", detail, a.win)
			}
		})
	}()
}

// AddExternalEngineDir 让用户选择一个外部引擎目录并加入扫描列表。
func (a *App) AddExternalEngineDir() {
	dialog.ShowFolderOpen(func(lu fyne.ListableURI, err error) {
		if err != nil || lu == nil {
			return
		}
		dir := lu.Path()
		reg := a.Reg()
		if !reg.AddSearchDir(dir) {
			dialog.ShowInformation("已在列表中", "该目录已在引擎库扫描列表中："+dir, a.win)
			return
		}
		if err := reg.Save(a.engPath); err != nil {
			fyne.Do(func() { a.setStatusInfo("保存 engines.json 失败：" + err.Error()) })
		}
		a.RescanEngines(false)
	}, a.win)
}

// TestEngine 测试与某个引擎的连接（启动 + isready）。
func (a *App) TestEngine(id string) {
	e := a.Reg().Find(id)
	if e == nil {
		return
	}
	entry := *e
	a.setStatusInfo("正在测试 " + entry.Name + " …")
	go func() {
		c := engine.NewClient(entry.Path)
		if err := c.Start(8 * time.Second); err != nil {
			c.Kill()
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("测试失败：%w", err), a.win)
				a.setStatusInfo("测试失败：" + entry.Name)
			})
			return
		}
		err := c.IsReady(8 * time.Second)
		name, proto, opts := c.Name(), c.Protocol().String(), len(c.Options())
		c.Quit(2 * time.Second)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(fmt.Errorf("引擎启动成功，但 isready 无响应：%w", err), a.win)
				a.setStatusInfo("测试失败：" + entry.Name)
				return
			}
			a.setStatusInfo(fmt.Sprintf("测试通过：%s（%s，%d 项参数）", name, proto, opts))
			dialog.ShowInformation("连接测试通过",
				fmt.Sprintf("引擎：%s\n协议：%s\n参数：%d 项\n路径：%s", name, proto, opts, entry.Path), a.win)
		})
	}()
}

// RemoveEngine 从引擎库移除一个引擎。
func (a *App) RemoveEngine(id string) {
	reg := a.Reg()
	if reg.Remove(id) {
		if a.cfg.SelfEngine == id {
			a.cfg.SelfEngine = ""
		}
		if a.cfg.OpponentEngine == id {
			a.cfg.OpponentEngine = ""
		}
		_ = reg.Save(a.engPath)
		a.SaveConfig()
		a.syncEngineSelects()
		a.setStatusInfo("已移除引擎：" + id)
	}
}

// SetSelfEngine 设置己方（分析）引擎。
func (a *App) SetSelfEngine(id string) {
	a.cfg.SelfEngine = id
	a.SaveConfig()
	a.syncEngineSelects()
	e := a.Reg().Find(id)
	if e != nil {
		a.setStatusInfo("己方引擎已设为：" + e.Name)
		go a.restartAnalysisEngine()
	}
}

// SetOpponentEngine 设置对手引擎。
func (a *App) SetOpponentEngine(id string) {
	a.cfg.OpponentEngine = id
	a.SaveConfig()
	a.syncEngineSelects()
	if e := a.Reg().Find(id); e != nil {
		a.setStatusInfo("对手引擎已设为：" + e.Name)
	}
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '\\' || p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
