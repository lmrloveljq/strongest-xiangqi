package engine

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PresetEngineDir 是预置引擎目录名，相对于本程序可执行文件所在目录（baseDir）。
// 用户按 README 指引把 Pikafish 放入该目录后，首次运行会自动注册。
const PresetEngineDir = "engines"

// EngineEntry 是引擎库中的一条引擎记录，对应 engines.json 里 engines 数组的一个元素。
type EngineEntry struct {
	ID       string   `json:"id"`               // 稳定唯一 id（由路径哈希得到，形如 eng-1a2b3c4d）
	Name     string   `json:"name"`             // 引擎自报名称（id name）
	Protocol string   `json:"protocol"`         // "UCI" 或 "UCCI"
	Author   string   `json:"author,omitempty"` // 引擎自报作者
	Path     string   `json:"path"`             // exe 绝对路径
	Dir      string   `json:"dir"`              // 引擎工作目录（= exe 所在目录）
	Source   string   `json:"source"`           // preset=预置 / library=引擎库目录 / external=外部添加
	Options  []Option `json:"options_cached"`   // 上次探测缓存到的参数列表

	// Executable 是「本次运行内」的存在性缓存，**不参与序列化**（json:"-"）。
	//
	// 【R3 修复】它以前是界面判断「可用 / 文件已丢失」的唯一依据，于是出现：
	// engines.json 里根本没有这个字段 → 反序列化得到 Go 的零值 false →
	// 明明文件真实存在、主界面正在用该引擎分析，列表却把所有引擎标红「文件已丢失」。
	//
	// 现在的纪律：**任何显示逻辑都不得读取本字段**，必须调用 CheckExecutable()
	// 做实时 os.Stat 校验；本字段只作为扫描结束后的快照，供内部统计使用。
	//
	// Deprecated: 请改用 CheckExecutable()。
	Executable bool `json:"-"`
}

// Availability 描述一个引擎可执行文件在磁盘上的真实状态。
type Availability struct {
	OK     bool   // exe 存在、是普通文件、且当前进程可读
	Reason string // 不可用时的中文原因（可用时为空）
}

// CheckExecutable 实时校验引擎可执行文件：存在性 + 是否普通文件 + 可读权限。
//
// 这是判断「可用 / 文件已丢失」的唯一权威入口（R3）：
//   - 每次引擎列表 Refresh / 扫描时都要重新调用，绝不缓存到 JSON；
//   - 即使 engines.json 里缺失任何字段，也不会影响判断结果。
func (e EngineEntry) CheckExecutable() Availability {
	path := strings.TrimSpace(e.Path)
	if path == "" {
		return Availability{Reason: "引擎记录里没有 exe 路径"}
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Availability{Reason: "文件已丢失"}
		}
		return Availability{Reason: "无法访问：" + err.Error()}
	}
	if info.IsDir() {
		return Availability{Reason: "该路径是目录，不是可执行文件"}
	}
	// 可读权限：真正打开一次。Windows 上被独占锁定的 exe 也会在这里暴露。
	f, err := os.Open(path)
	if err != nil {
		return Availability{Reason: "文件不可读：" + err.Error()}
	}
	_ = f.Close()
	return Availability{OK: true}
}

// RefreshExistence 刷新每条记录的「文件是否仍存在」快照（R3）。
//
// 注意：界面**不**依赖这个快照，它只为扫描报告与内部统计服务；
// 界面显示请一律调用 EngineEntry.CheckExecutable()。
func (r *Registry) RefreshExistence() {
	for i := range r.Engines {
		r.Engines[i].Executable = r.Engines[i].CheckExecutable().OK
	}
}

// Registry 是引擎注册表，整体序列化为 engines.json。
type Registry struct {
	Engines       []EngineEntry `json:"engines"`
	DefaultEngine string        `json:"default_engine"` // 默认己方引擎 id
	SearchDirs    []string      `json:"search_dirs"`    // 额外扫描的「外部引擎目录」
	UpdatedAt     string        `json:"updated_at"`
}

// MakeID 由引擎路径生成稳定 id。
func MakeID(path string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(filepath.Clean(path))))
	return fmt.Sprintf("eng-%08x", h.Sum32())
}

// NewRegistry 返回一个空的引擎注册表。
func NewRegistry() *Registry {
	return &Registry{Engines: []EngineEntry{}}
}

// LoadRegistry 读取 engines.json；文件不存在或损坏时返回一个按预置规则初始化的新注册表。
func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewRegistry(), nil
		}
		return nil, err
	}
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		// 文件损坏：备份后重建，避免用户直接丢失数据
		_ = os.WriteFile(path+".bak", data, 0o644)
		return NewRegistry(), fmt.Errorf("engines.json 解析失败（已备份为 engines.json.bak，将重新扫描）: %w", err)
	}
	if r.Engines == nil {
		r.Engines = []EngineEntry{}
	}
	// 【R3】加载后立刻用 os.Stat 刷新一次存在性快照。
	// 这样即使 engines.json 是人手编辑的、缺字段的、或由旧版本写出的，
	// 列表也不会把真实存在的引擎误判为「文件已丢失」。
	r.RefreshExistence()
	return &r, nil
}

// Save 写回 engines.json（UTF-8、缩进、便于手工查看）。
func (r *Registry) Save(path string) error {
	r.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Find 按 id 查找引擎。
func (r *Registry) Find(id string) *EngineEntry {
	for i := range r.Engines {
		if r.Engines[i].ID == id {
			return &r.Engines[i]
		}
	}
	return nil
}

// FindByPath 按路径查找引擎。
func (r *Registry) FindByPath(path string) *EngineEntry {
	clean := strings.ToLower(filepath.Clean(path))
	for i := range r.Engines {
		if strings.ToLower(filepath.Clean(r.Engines[i].Path)) == clean {
			return &r.Engines[i]
		}
	}
	return nil
}

// Remove 删除一条引擎记录。
func (r *Registry) Remove(id string) bool {
	for i := range r.Engines {
		if r.Engines[i].ID == id {
			r.Engines = append(r.Engines[:i], r.Engines[i+1:]...)
			if r.DefaultEngine == id {
				r.DefaultEngine = ""
			}
			return true
		}
	}
	return false
}

// AddSearchDir 记录一个外部引擎目录（去重）。
func (r *Registry) AddSearchDir(dir string) bool {
	dir = filepath.Clean(dir)
	for _, d := range r.SearchDirs {
		if strings.EqualFold(filepath.Clean(d), dir) {
			return false
		}
	}
	r.SearchDirs = append(r.SearchDirs, dir)
	return true
}

// RemoveSearchDir 移除一个外部引擎目录，并删除来源于该目录的引擎记录。
func (r *Registry) RemoveSearchDir(dir string) bool {
	dir = filepath.Clean(dir)
	found := false
	var keep []string
	for _, d := range r.SearchDirs {
		if strings.EqualFold(filepath.Clean(d), dir) {
			found = true
			continue
		}
		keep = append(keep, d)
	}
	r.SearchDirs = keep
	if found {
		var engines []EngineEntry
		prefix := strings.ToLower(dir) + string(filepath.Separator)
		for _, e := range r.Engines {
			if strings.HasPrefix(strings.ToLower(filepath.Clean(e.Path)), prefix) {
				continue
			}
			engines = append(engines, e)
		}
		r.Engines = engines
	}
	return found
}

// List 返回按名称排序的引擎列表副本。
func (r *Registry) List() []EngineEntry {
	out := make([]EngineEntry, len(r.Engines))
	copy(out, r.Engines)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source == "preset"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Label 返回界面上显示的「名称 (协议)」文本。
func (e EngineEntry) Label() string {
	return fmt.Sprintf("%s (%s)", e.Name, e.Protocol)
}

// ScanForExes 递归扫描 root 下的所有 .exe（深度上限 3 层）。
func ScanForExes(root string, skip map[string]bool) []string {
	var out []string
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 无权限目录直接跳过，不中断整次扫描
		}
		if d.IsDir() {
			rel, e := filepath.Rel(root, p)
			if e == nil && rel != "." && strings.Count(rel, string(filepath.Separator)) >= 3 {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".exe") {
			return nil
		}
		abs, e := filepath.Abs(p)
		if e != nil {
			abs = p
		}
		if skip != nil && skip[strings.ToLower(filepath.Clean(abs))] {
			return nil
		}
		out = append(out, abs)
		return nil
	})
	sort.Strings(out)
	return out
}

// ScanFailure 记录一个无法识别为象棋引擎的可执行文件。
type ScanFailure struct {
	Path   string
	Reason string
}

// ScanReport 是一次「重新扫描引擎」的结果汇总。
type ScanReport struct {
	Added    []EngineEntry
	Updated  []EngineEntry
	Failed   []ScanFailure
	Scanned  int
	DirCount int
}

// Summary 返回面向界面的中文摘要。
func (sr ScanReport) Summary() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "扫描 %d 个目录、%d 个 exe：新增 %d 个引擎，更新 %d 个",
		sr.DirCount, sr.Scanned, len(sr.Added), len(sr.Updated))
	if len(sr.Failed) > 0 {
		fmt.Fprintf(&sb, "，%d 个文件无法识别为象棋引擎", len(sr.Failed))
	}
	return sb.String()
}

// ProbeExe 启动一个 exe 并做协议探测，成功则返回引擎记录。
//
// 无法识别（非象棋引擎、缺少运行库、启动失败）时返回错误，调用方据此提示用户，
// 但绝不能因此崩溃或中断扫描。
func ProbeExe(path string, timeout time.Duration, source string) (*EngineEntry, error) {
	c := NewClient(path)
	if err := c.Start(timeout); err != nil {
		return nil, err
	}
	defer c.Quit(2 * time.Second)
	return &EngineEntry{
		ID:       MakeID(path),
		Name:     c.Name(),
		Protocol: c.Protocol().String(),
		Author:   c.Author(),
		Path:     path,
		Dir:      filepath.Dir(path),
		Source:   source,
		Options:  c.Options(),
	}, nil
}

// sourceFor 判断某个 exe 属于哪类来源。
func sourceFor(path string, presetDirs []string) string {
	lp := strings.ToLower(filepath.Clean(path))
	for _, d := range presetDirs {
		if strings.HasPrefix(lp, strings.ToLower(filepath.Clean(d))+string(filepath.Separator)) {
			return "preset"
		}
	}
	return "library"
}

// EnsurePreset 首次运行时把预置引擎目录（baseDir\engines）注册进引擎库。
// 已注册或目录不存在时不做任何事（不报错）。
func EnsurePreset(r *Registry, baseDir string, timeout time.Duration) (*EngineEntry, error) {
	presetDir := filepath.Join(baseDir, PresetEngineDir)
	exes := ScanForExes(presetDir, nil)
	if len(exes) == 0 {
		return nil, fmt.Errorf("未在预置目录找到引擎可执行文件：%s", presetDir)
	}
	// 优先选择文件名含 pikafish 的 exe
	pick := exes[0]
	for _, e := range exes {
		if strings.Contains(strings.ToLower(filepath.Base(e)), "pikafish") {
			pick = e
			break
		}
	}
	if exist := r.FindByPath(pick); exist != nil {
		exist.Executable = true
		if r.DefaultEngine == "" {
			r.DefaultEngine = exist.ID
		}
		return exist, nil
	}
	entry, err := ProbeExe(pick, timeout, "preset")
	if err != nil {
		return nil, err
	}
	r.Engines = append(r.Engines, *entry)
	if r.DefaultEngine == "" {
		r.DefaultEngine = entry.ID
	}
	return entry, nil
}

// Rescan 扫描「exe 同目录的 engines\ 子目录」与全部已登记的外部引擎目录。
//
// baseDir 一般是本程序 exe 所在目录。skip 集合用于排除本程序自身。
// progress 可为 nil；每个候选文件探测前调用一次，供界面显示进度。
func Rescan(r *Registry, baseDir string, timeout time.Duration, skip map[string]bool, progress func(string)) ScanReport {
	var rep ScanReport
	dirs := []string{filepath.Join(baseDir, "engines")}
	dirs = append(dirs, r.SearchDirs...)
	presetDirs := []string{filepath.Join(baseDir, PresetEngineDir)}

	seen := map[string]bool{}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		rep.DirCount++
		for _, exe := range ScanForExes(dir, skip) {
			key := strings.ToLower(filepath.Clean(exe))
			if seen[key] {
				continue
			}
			seen[key] = true
			rep.Scanned++
			if progress != nil {
				progress(filepath.Base(exe))
			}
			entry, err := ProbeExe(exe, timeout, sourceFor(exe, presetDirs))
			if err != nil {
				rep.Failed = append(rep.Failed, ScanFailure{Path: exe, Reason: err.Error()})
				continue
			}
			if old := r.FindByPath(exe); old != nil {
				old.Name = entry.Name
				old.Protocol = entry.Protocol
				old.Author = entry.Author
				old.Options = entry.Options
				old.ID = entry.ID
				old.Executable = true
				rep.Updated = append(rep.Updated, *old)
			} else {
				entry.Executable = true
				r.Engines = append(r.Engines, *entry)
				rep.Added = append(rep.Added, *entry)
			}
		}
	}
	r.RefreshExistence()
	return rep
}
