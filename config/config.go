// Package config 负责 config.json 的读写与默认值。
//
// 约定：
//   - config.json 与 xiangqi.exe 同目录；
//   - 启动时自动读取，文件不存在则按默认值创建；
//   - 任何字段缺失或非法都回退到默认值，绝不因为配置问题导致程序无法启动。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"xiangqi/version"
)

// 时间控制模式
const (
	TimeModeMoveTime = "movetime" // 每步限时
	TimeModeDepth    = "depth"    // 固定深度
	TimeModeGameTime = "gametime" // 每局总时间（仅对战模式）
	TimeModeInfinite = "infinite" // 无限分析：一直算到用户停止（最强引擎模式）
)

// 皮肤标识（对应 ui 包注册的 ThemeProvider）。
//
// 皮肤扩展接口：ui.ThemeProvider + ui.RegisterTheme，本字段只负责选名字，
// 新增皮肤不需要改动 config 之外的任何代码。
const (
	// ThemeDefault 是浅暖米黄的经典护眼皮肤。
	ThemeDefault = "default"
	// ThemeInkGold 是「墨玉金」：深墨底 + 暖金强调。
	ThemeInkGold = "inkgold"
)

// 背景样式：主题自带底纹 / 自定义图片 / 纯色。
const (
	BgStyleTexture = "texture" // 主题自带的细腻底纹（默认，程序生成，任意分辨率都清晰）
	BgStyleImage   = "image"   // 用 background_image 指定的图片
	BgStyleSolid   = "solid"   // 纯色
)

// 人机对弈里「人类执哪一方」的取值。
const (
	SideRed   = "red"
	SideBlack = "black"
)

// 字号档位的取值（可选设置，默认「标准」）。
//
// 历史说明：v1.5 一度把默认值定成「大」并按「适老化＝放大」的思路整轮放大，
// 与真实需求不符——目标用户涵盖各个年龄段，要的是**界面简单好操作**，
// 字号保持正常即可。因此默认回到「标准」，大/特大只作为**用户自己可选**的辅助项，
// 给确实需要更大字的人用（见 ui/a11y.go）。
const (
	FontScaleStandard = "standard" // 标准（1.00，默认）
	FontScaleLarge    = "large"    // 大（1.22）
	FontScaleXLarge   = "xlarge"   // 特大（1.45）
)

// ThinkProfile 是一套「电脑思考」设置（时间模式 / 限时 / 深度）。
//
// 【v1.6.8 / 头脑风暴第 2 条】以前全软件只有一套思考设置，分析时调到 5 秒，
// 接着去人机对弈电脑每步也想 5 秒。现在分析模式与人机对弈各存一套，
// 切模式时自动套用该模式自己那套。对战本来就有自己的 Match* 字段，不参与。
type ThinkProfile struct {
	TimeMode   string `json:"time_mode"`
	MoveTimeMS int    `json:"move_time_ms"`
	Depth      int    `json:"depth"`
}

// Config 是全部可持久化的配置项。
type Config struct {
	Version string `json:"version"` // 写入时的程序版本，便于将来做配置迁移

	// ---------- 引擎搜索参数 ----------
	Threads     int    `json:"threads"`      // setoption Threads
	Hash        int    `json:"hash"`         // setoption Hash（MB）
	Depth       int    `json:"depth"`        // 固定深度模式的层数
	MoveTimeMS  int    `json:"move_time_ms"` // 每步限时（毫秒）
	TimeMode    string `json:"time_mode"`    // movetime / depth：思考模式切换
	MultiPV     int    `json:"multipv"`      // 桥接分析模式的候选数
	Temperature int    `json:"temperature"`  // 概率 softmax 温度 T
	SyzygyPath  string `json:"syzygy_path"`  // 残局表目录（引擎支持则启用）

	// ---------- 参考引擎（Kibitzer，v1.6.10）----------
	//
	// 同一局面同时交给两个引擎算、结果上下堆叠对比。KibitzerEngine 为空 = 不启用。
	// 时间语义按同类产品（Arena 自动分析）只做「同步每步时限」：一个值管两个引擎。
	// MaxStrength 是「最强引擎模式」：单引擎满配（线程=逻辑核-1、哈希 4096、
	// MultiPV 1）。它与参考引擎互斥——满配要求独占用核，再多挂一个引擎必然超订，
	// 所以开启时会把参考引擎关掉（见 ui.toggleMaxStrength）。
	MaxStrength bool `json:"max_strength"`
	// MaxStrengthBackup 存开启前的线程/哈希/MultiPV，用于一键退回省资源档。
	MaxStrengthBackup struct {
		Threads int `json:"threads"`
		Hash    int `json:"hash"`
		MultiPV int `json:"multipv"`
	} `json:"max_strength_backup"`

	KibitzerEngine     string `json:"kibitzer_engine"`      // 参考引擎 id，空 = 关闭
	KibitzerFollowMain bool   `json:"kibitzer_follow_main"` // 线程/哈希跟随主引擎
	KibitzerThreads    int    `json:"kibitzer_threads"`
	KibitzerHash       int    `json:"kibitzer_hash"`
	KibitzerSyncMS     int    `json:"kibitzer_sync_ms"` // 同步每步时限（毫秒）

	ThinkBridge ThinkProfile `json:"think_bridge"` // 分析模式自己的思考设置
	ThinkHuman  ThinkProfile `json:"think_human"`  // 人机对弈自己的思考设置

	// ---------- 引擎选择 ----------
	SelfEngine     string `json:"self_engine"`     // 己方引擎 id（默认 = 预置皮卡鱼）
	OpponentEngine string `json:"opponent_engine"` // 最近使用的对手引擎 id

	// HumanSide 是人机对弈模式下**人类**执哪一方："red" / "black"。
	//
	// 「局面对调」就是翻转这个字段：局面不变、该谁走还是谁走，
	// 只是把「哪一方由人下」换了个边。
	HumanSide string `json:"human_side"`

	// ---------- 自动对战 ----------
	MatchGames      int    `json:"match_games"`     // 对局数 1~100
	MatchTimeMode   string `json:"match_time_mode"` // movetime / gametime / depth
	MatchMoveTimeMS int    `json:"match_move_time_ms"`
	MatchGameTimeMS int    `json:"match_game_time_ms"`
	MatchDepth      int    `json:"match_depth"`
	// 对手可以单独设一套思考参数（用户要求「双方独立深度/限时」）。
	// 三者都为 0 表示「跟己方一样」。
	MatchOppMoveTimeMS int    `json:"match_opp_move_time_ms"`
	MatchOppGameTimeMS int    `json:"match_opp_game_time_ms"`
	MatchOppDepth      int    `json:"match_opp_depth"`
	AlternateColors    bool   `json:"alternate_colors"` // 先后手轮换
	SelfThreads        int    `json:"self_threads"`     // 对战模式己方线程（默认拉满）
	SelfHash           int    `json:"self_hash"`
	OppThreads         int    `json:"opp_threads"`
	OppHash            int    `json:"opp_hash"`
	UseOpeningBook     bool   `json:"use_opening_book"`
	OpeningBookPath    string `json:"opening_book_path"`

	// ---------- 界面 ----------
	Theme             string `json:"theme"`        // 皮肤主题，本版固定 "default"
	FontScale         string `json:"font_scale"`   // 字号档位：standard / large / xlarge（默认 large）
	ShowCoords        bool   `json:"show_coords"`  // 是否显示棋盘四周 a~i / 0~9 坐标（默认关）
	ShowLines         bool   `json:"show_lines"`   // 是否显示行棋线路（选中棋子的横线/竖线，默认关）
	SoundOn           bool   `json:"sound_on"`     // 走棋 / 吃子 / 将军音效（默认开）
	WindowWidth       int    `json:"window_width"` // 窗口大小与位置记忆
	WindowHeight      int    `json:"window_height"`
	WindowX           int    `json:"window_x"`
	AlwaysOnTop       bool   `json:"always_on_top"` // 窗口置顶（v1.6.8，参照 TCHESS 设置菜单）
	WindowY           int    `json:"window_y"`
	BackgroundImage   string `json:"background_image"`   // 背景图路径，默认 assets\background.jpg
	BackgroundOverlay int    `json:"background_overlay"` // 背景之上白色遮罩不透明度 0~100
	// BackgroundStyle 决定背景怎么画：texture（主题自带底纹，默认）/ image（用上面的图片）/ solid（纯色）。
	// 默认改成 texture：照片背景会跟棋盘抢注意力、缩放发虚，程序生成的细腻底纹更像专业软件。
	BackgroundStyle string  `json:"background_style"`
	LastMode        string  `json:"last_mode"`   // 上次使用的模式：bridge / match
	RightSplit      float64 `json:"right_split"` // 右侧纵向分栏比例（记忆拖动位置）
	MainSplit       float64 `json:"main_split"`  // 主横向分栏比例

	// ---------- 运行状态记忆 ----------
	LastMoveInput string            `json:"last_move_input"` // 上次粘贴的着法序列
	EngineParams  map[string]string `json:"engine_params"`   // 动态参数面板的取值（key=引擎id/参数名）
}

// Default 返回出厂默认配置。
//
// 默认值按本机配置（Ryzen 7 8845H，8 核 16 逻辑处理器，32GB 内存）拉满：
// 分析线程 = CPU 逻辑核数、哈希 = 4096MB。
//
// 【R5 修复】对战线程默认值改为「双方平摊逻辑核数」。
// v1.3.0 里 SelfThreads 与 OppThreads 都默认成 NumCPU，对战时两方各占满全部核心
// （本机 16+16=32 线程跑在 16 个逻辑处理器上，超订 2 倍），实测双方 nps 合计
// 反而只有单引擎峰值的约 39%。默认平摊后双方各 8 线程，总线程数不超过逻辑核数。
func Default() *Config {
	threads := runtime.NumCPU()
	if threads < 1 {
		threads = 1
	}
	if threads > 16 { // 参数面板上限 16；超线程超过 16 时仍默认 16 以保证稳定
		threads = 16
	}
	// v1.5：分析引擎的默认线程数取逻辑核数的 1/4（2~8），
	// 既够用又不至于把 CPU 占满导致界面卡顿。
	lowThreads := threads / 4
	if lowThreads < 2 {
		lowThreads = 2
	}
	if lowThreads > 8 {
		lowThreads = 8
	}

	// 对战双方平摊；单核机器时至少各 1。
	matchThreads := threads / 2
	if matchThreads < 1 {
		matchThreads = 1
	}
	return &Config{
		Version: version.VERSION,

		// v1.5：默认参数从「拉满」改成「低负载」。
		// 原来线程 = 逻辑核数、MultiPV = 8，引擎满负载跑会把界面挤到卡顿；
		// 现在默认 4 线程 / 512MB / MultiPV 4 / 每步 1 秒，界面顺滑，
		// 而且这些参数在「参数设置」里随时可以调回去。
		Threads:     lowThreads,
		Hash:        512,
		Depth:       20,
		MoveTimeMS:  1000,
		TimeMode:    TimeModeMoveTime,
		MultiPV:     4,
		Temperature: 120,
		SyzygyPath:  "",

		KibitzerEngine:     "",
		KibitzerFollowMain: true,
		KibitzerThreads:    2,
		KibitzerHash:       256,
		KibitzerSyncMS:     2000,

		ThinkBridge: configThinkProfile(TimeModeMoveTime, 1000, 20),
		ThinkHuman:  configThinkProfile(TimeModeMoveTime, 1000, 20),

		SelfEngine:     "",
		OpponentEngine: "",
		HumanSide:      SideRed,

		MatchGames:      10,
		MatchTimeMode:   TimeModeMoveTime,
		MatchMoveTimeMS: 1000,
		MatchGameTimeMS: 60000,
		MatchDepth:      12,
		// 默认与己方相同（0 = 跟随己方），用户想分开调再改
		MatchOppMoveTimeMS: 0,
		MatchOppGameTimeMS: 0,
		MatchOppDepth:      0,
		AlternateColors:    true,
		SelfThreads:        matchThreads,
		SelfHash:           4096,
		OppThreads:         matchThreads,
		OppHash:            1024,
		UseOpeningBook:     false,
		OpeningBookPath:    "",

		Theme:             ThemeDefault,
		FontScale:         FontScaleStandard,
		ShowCoords:        false,
		SoundOn:           true,
		WindowWidth:       1500,
		WindowHeight:      950,
		WindowX:           -1,
		WindowY:           -1,
		BackgroundImage:   filepath.Join("assets", "background.jpg"),
		BackgroundOverlay: 65,
		BackgroundStyle:   BgStyleTexture,
		LastMode:          "bridge",
		RightSplit:        0.52,
		// MainSplit 是左栏（棋盘）占比：0.62 让右栏只占约 38%。
		// 【v1.6】原默认 0.56，用户反馈「右边侧边栏太宽」，同时右栏已重构成
		// 「分析 / 棋谱」两块并收窄了各面板最小宽度，这里同步把默认值调大。
		MainSplit: 0.62,

		LastMoveInput: "",
		EngineParams:  map[string]string{},
	}
}

// configThinkProfile 造一套思考设置（仅用于默认值，避免字面量重复）。
func configThinkProfile(mode string, moveMS, depth int) ThinkProfile {
	return ThinkProfile{TimeMode: mode, MoveTimeMS: moveMS, Depth: depth}
}

// normalizeThinkProfile 校验一套思考设置；非法值回退到默认。
func normalizeThinkProfile(p, def ThinkProfile) ThinkProfile {
	switch p.TimeMode {
	// 【缺陷修复】这里原来只认 movetime/depth，于是「无限分析」在**每个模式自己的**
	// 思考设置里都算非法值，一读配置就被打回默认 movetime ——
	// 用户选了无限分析，切一次模式（甚至只是重启）就没了。
	// 顶层 time_mode 的校验（见 Normalize）早就放行了 infinite，这里漏了。
	case TimeModeMoveTime, TimeModeDepth, TimeModeInfinite:
	default:
		p.TimeMode = def.TimeMode
	}
	if p.MoveTimeMS == 0 {
		p.MoveTimeMS = def.MoveTimeMS
	}
	if p.MoveTimeMS < 100 {
		p.MoveTimeMS = 100
	}
	if p.MoveTimeMS > 60000 {
		p.MoveTimeMS = 60000
	}
	if p.Depth == 0 {
		p.Depth = def.Depth
	}
	if p.Depth < 1 {
		p.Depth = 1
	}
	if p.Depth > 99999 {
		p.Depth = 99999
	}
	return p
}

// cpuCount 返回逻辑处理器数（对战线程分配的唯一依据，见 R5）。
func cpuCount() int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	return n
}

// CPUCount 导出逻辑处理器数，供界面计算「按核心数自动平均分配」。
func CPUCount() int { return cpuCount() }

// Normalize 修正越界或非法取值，保证界面滑块与引擎 setoption 不会收到离谱数字。
func (c *Config) Normalize() {
	d := Default()
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	// 【缺陷修复】这里的上下限必须与「参数设置」面板里滑块的取值范围一致。
	// 原来允许 1~64 线程 / 1~65536MB，而滑块只有 1~16 / 64~8192：
	// 配置文件里写一个 32，滑块就会拿到越界值，显示与下发都会错位。
	c.Threads = clamp(c.Threads, 1, 16)
	if c.Hash == 0 {
		c.Hash = d.Hash
	}
	c.Hash = clamp(c.Hash, 64, 8192)
	// 【v1.6.7】深度**不设上限**（用户明确要求）：只保留下限 1。
	// 引擎本身按深度搜索，层数想设多深由用户决定（实际会被引擎自身能力与
	// 时间限制约束，不需要软件替用户设天花板）。
	c.Depth = clamp(c.Depth, 1, 99999)
	if c.MoveTimeMS == 0 {
		c.MoveTimeMS = d.MoveTimeMS
	}
	c.MoveTimeMS = clamp(c.MoveTimeMS, 100, 60000)
	c.MultiPV = clamp(c.MultiPV, 1, 12)
	c.Temperature = clamp(c.Temperature, 40, 300)
	switch c.TimeMode {
	case TimeModeMoveTime, TimeModeDepth, TimeModeInfinite:
	default:
		c.TimeMode = d.TimeMode
	}
	c.MatchGames = clamp(c.MatchGames, 1, 100)
	switch c.MatchTimeMode {
	case TimeModeMoveTime, TimeModeGameTime, TimeModeDepth:
	default:
		c.MatchTimeMode = d.MatchTimeMode
	}
	c.MatchMoveTimeMS = clamp(c.MatchMoveTimeMS, 100, 60000)
	c.MatchGameTimeMS = clamp(c.MatchGameTimeMS, 1000, 3600000)
	c.MatchDepth = clamp(c.MatchDepth, 1, 99999) // 【v1.6.7】深度不设上限
	// 0 = 跟随己方；非 0 才夹到合法区间
	if c.MatchOppMoveTimeMS != 0 {
		c.MatchOppMoveTimeMS = clamp(c.MatchOppMoveTimeMS, 100, 60000)
	}
	if c.MatchOppGameTimeMS != 0 {
		c.MatchOppGameTimeMS = clamp(c.MatchOppGameTimeMS, 1000, 3600000)
	}
	if c.MatchOppDepth != 0 {
		c.MatchOppDepth = clamp(c.MatchOppDepth, 1, 99999)
	}
	// 【R5】单方线程数上限 = 逻辑处理器数：一方独占全部核心已属极限，
	// 超过它必然是笔误（旧配置里两方各 16 造成的 2 倍超订就是从这里放行的）。
	c.SelfThreads = clamp(c.SelfThreads, 1, cpuCount())
	c.OppThreads = clamp(c.OppThreads, 1, cpuCount())
	c.SelfHash = clamp(c.SelfHash, 16, 65536)
	c.OppHash = clamp(c.OppHash, 16, 65536)
	c.BackgroundOverlay = clamp(c.BackgroundOverlay, 0, 100)
	if c.WindowWidth < 900 {
		c.WindowWidth = d.WindowWidth
	}
	if c.WindowHeight < 640 {
		c.WindowHeight = d.WindowHeight
	}
	// 皮肤名：认不出的值回退到经典护眼（老配置里 theme 可能是空串）。
	switch c.Theme {
	case ThemeDefault, ThemeInkGold:
	default:
		c.Theme = ThemeDefault
	}
	// 背景样式：老配置没有这个字段（空串）→ 用主题自带底纹。
	// 注意：这里**不是**沿用 background_image —— 用户明确要求把照片背景换成主题底纹；
	// 想用回图片把 background_style 设成 "image" 即可。
	switch c.BackgroundStyle {
	case BgStyleTexture, BgStyleImage, BgStyleSolid:
	default:
		c.BackgroundStyle = BgStyleTexture
	}
	// 字号档位：认不出的值一律回退到默认「大」。
	// 老版本配置里没有这个字段（空串），也会走到这里拿到适老化默认值。
	switch c.FontScale {
	case FontScaleStandard, FontScaleLarge, FontScaleXLarge:
	default:
		c.FontScale = d.FontScale
	}
	if c.LastMode != "bridge" && c.LastMode != "match" && c.LastMode != "human" {
		c.LastMode = "bridge"
	}
	if c.HumanSide != SideRed && c.HumanSide != SideBlack {
		c.HumanSide = SideRed
	}
	if c.RightSplit <= 0 || c.RightSplit >= 1 {
		c.RightSplit = d.RightSplit
	}
	if c.MainSplit <= 0 || c.MainSplit >= 1 {
		c.MainSplit = d.MainSplit
	}
	c.KibitzerThreads = clamp(c.KibitzerThreads, 1, 16)
	c.KibitzerHash = clamp(c.KibitzerHash, 64, 8192)
	if c.KibitzerSyncMS == 0 {
		c.KibitzerSyncMS = d.KibitzerSyncMS
	}
	c.KibitzerSyncMS = clamp(c.KibitzerSyncMS, 100, 60000)

	c.ThinkBridge = normalizeThinkProfile(c.ThinkBridge, d.ThinkBridge)
	c.ThinkHuman = normalizeThinkProfile(c.ThinkHuman, d.ThinkHuman)

	if c.EngineParams == nil {
		c.EngineParams = map[string]string{}
	}
	if c.BackgroundImage == "" {
		c.BackgroundImage = d.BackgroundImage
	}
}

// Load 读取配置文件；不存在则返回默认配置（不写盘，由调用方决定何时创建）。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			c := Default()
			c.Normalize()
			return c, nil
		}
		return nil, err
	}
	c := Default()
	if err := json.Unmarshal(data, c); err != nil {
		// 配置损坏：不阻断启动，用默认值继续，并把损坏文件备份下来
		_ = os.WriteFile(path+".bak", data, 0o644)
		c = Default()
	}
	c.Normalize()
	// 【一次性迁移】v1.5 之前的默认值是「拉满」（线程=逻辑核数、Hash 4096、
	// MultiPV 8），会让引擎占满 CPU、界面卡顿。如果配置里的值**正好等于旧默认**
	// （说明用户从没手动改过），就换成新的低负载默认；改过的一律尊重不动。
	if c.Version != version.VERSION {
		d := Default()
		legacyThreads := runtime.NumCPU()
		if legacyThreads > 16 {
			legacyThreads = 16
		}
		if c.Threads == legacyThreads && c.Hash == 4096 && c.MultiPV == 8 {
			c.Threads = d.Threads
			c.Hash = d.Hash
			c.MultiPV = d.MultiPV
			c.MoveTimeMS = d.MoveTimeMS
		}
	}
	return c, nil
}

// Save 写回配置文件。
func (c *Config) Save(path string) error {
	c.Normalize()
	// 【缺陷修复】写盘时把版本号更新成当前版本。
	// 原来 Version 只在 Default() 里设置，Load() 又会用文件里的旧值覆盖它，
	// 于是一旦用户从旧版本升级上来，Version 永远是旧的那个 →
	// Load() 里那段「一次性迁移」（线程/哈希/MultiPV 正好等于旧默认就换新默认）
	// 会**每次启动都执行一遍**：用户手动把线程调回 16、哈希调回 4096 后，
	// 下次启动又被悄悄改回去。写盘即认账，迁移就只发生一次。
	c.Version = version.VERSION
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ResolvePath 把配置中的相对路径解析为「相对于 baseDir」的绝对路径。
// 背景图默认写成 assets\background.jpg，就是靠这里与 exe 目录绑定。
func ResolvePath(baseDir, p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(baseDir, p)
}
