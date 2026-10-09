package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ctrlBufSize 是控制行（id/option/readyok/uciok 等）channel 的缓冲深度。
// 一次握手最多产生数十行，4096 足够；写满时丢弃的行不会影响握手（握手只关心少数几行）。
const ctrlBufSize = 4096

// Client 是一个象棋引擎子进程的通信客户端。
//
// 并发模型：
//
//	主 goroutine（UI） ──send()──▶ 子进程 stdin
//	子进程 stdout ──readLoop goroutine──▶ ctrlCh / infoSig+Snapshot / bestCh
//
// Analyze 是阻塞调用，必须放在独立 goroutine 中执行；UI 线程永远不等待引擎。
type Client struct {
	path string // 引擎 exe 绝对路径
	dir  string // 引擎工作目录（= exe 所在目录，保证引擎能找到同名权重）

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	busy    bool
	closed  bool
	exitErr string

	proto  Protocol
	name   string
	author string
	opts   []Option

	ctrlCh     chan string
	bestCh     chan string
	infoSig    chan struct{}
	exited     chan struct{}
	stdoutDone chan struct{}

	infoMu  sync.Mutex
	curInfo map[int]InfoLine

	stderrMu   sync.Mutex
	stderrTail []string
}

// NewClient 创建一个客户端（此时尚未启动进程）。
func NewClient(path string) *Client {
	abs := path
	if a, err := filepath.Abs(path); err == nil {
		abs = a
	}
	return &Client{
		path:       abs,
		dir:        filepath.Dir(abs),
		proto:      ProtoUnknown,
		ctrlCh:     make(chan string, ctrlBufSize),
		bestCh:     make(chan string, 4),
		infoSig:    make(chan struct{}, 1),
		exited:     make(chan struct{}),
		stdoutDone: make(chan struct{}),
		curInfo:    map[int]InfoLine{},
	}
}

// Path 返回引擎可执行文件路径。
func (c *Client) Path() string { return c.path }

// Dir 返回引擎工作目录。
func (c *Client) Dir() string { return c.dir }

// Name 返回引擎自报名称（握手后有效）。
func (c *Client) Name() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.name == "" {
		return strings.TrimSuffix(filepath.Base(c.path), filepath.Ext(c.path))
	}
	return c.name
}

// Author 返回引擎作者。
func (c *Client) Author() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.author
}

// Protocol 返回探测到的协议。
func (c *Client) Protocol() Protocol {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.proto
}

// Options 返回引擎上报的全部参数（副本）。
func (c *Client) Options() []Option {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Option, len(c.opts))
	copy(out, c.opts)
	return out
}

// FindOption 按名称查找参数（大小写不敏感）。
func (c *Client) FindOption(name string) (Option, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.opts {
		if strings.EqualFold(o.Name, name) {
			return o, true
		}
	}
	return Option{}, false
}

// Alive 判断子进程是否仍在运行。
// PID 返回引擎进程号（0 = 还没启动）。
//
// 供界面侧做「进程优先级 / CPU 亲和性 / CPU 占用统计」——这些都得先拿到进程句柄。
func (c *Client) PID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

func (c *Client) Alive() bool {
	select {
	case <-c.exited:
		return false
	default:
		return true
	}
}

// ExitError 返回进程退出信息（空串表示正常或尚未退出）。
func (c *Client) ExitError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitErr
}

// StderrTail 返回引擎 stderr 的最后若干行（用于崩溃诊断）。
func (c *Client) StderrTail() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	return strings.Join(c.stderrTail, "\n")
}

// Start 启动子进程并完成协议探测（UCI/UCCI 自适应）与参数发现。
//
// 探测流程（对规范的严格实现 + 对真实引擎的兼容）：
//  1. 发送 `uci`，等待 `uciok`（UCI）或 `ucciok`（UCCI）；
//  2. 若第 1 步在 probeTimeout 内无任何响应，再发送 `ucci` 重试一次
//     （部分老 UCCI 引擎不识别 `uci` 命令）；
//  3. 两步都无响应 → 判定「无法识别为象棋引擎」。
func (c *Client) Start(probeTimeout time.Duration) error {
	if err := c.spawn(); err != nil {
		return err
	}
	proto, name, author, opts, err := c.probe(probeTimeout)
	if err != nil {
		c.Kill()
		return err
	}
	c.mu.Lock()
	c.proto = proto
	c.name = name
	c.author = author
	c.opts = opts
	c.mu.Unlock()
	return nil
}

func (c *Client) spawn() error {
	c.mu.Lock()
	if c.cmd != nil {
		c.mu.Unlock()
		return errors.New("引擎进程已启动")
	}
	c.mu.Unlock()

	cmd := exec.Command(c.path)
	cmd.Dir = c.dir
	// 隐藏控制台窗口，避免每启动一个引擎就闪出一个黑框
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建 stdin 管道失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("创建 stderr 管道失败: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动引擎 %s 失败: %w（请检查是否缺少 VC 运行库或文件被删除）", filepath.Base(c.path), err)
	}

	c.mu.Lock()
	c.cmd = cmd
	c.stdin = stdin
	c.closed = false
	c.mu.Unlock()

	go c.readLoop(stdout)
	go c.drainStderr(stderr)
	go func() {
		<-c.stdoutDone
		werr := cmd.Wait() // 回收进程资源
		c.mu.Lock()
		if werr != nil {
			c.exitErr = werr.Error()
		}
		c.mu.Unlock()
		close(c.exited)
	}()
	return nil
}

// readLoop 持续读取引擎 stdout。
func (c *Client) readLoop(r io.Reader) {
	defer close(c.stdoutDone)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		c.dispatch(strings.TrimRight(sc.Text(), "\r\n"))
	}
}

func (c *Client) drainStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		c.stderrMu.Lock()
		c.stderrTail = append(c.stderrTail, line)
		if len(c.stderrTail) > 40 {
			c.stderrTail = c.stderrTail[len(c.stderrTail)-40:]
		}
		c.stderrMu.Unlock()
	}
}

// dispatch 把一行引擎输出路由到对应的接收方。
func (c *Client) dispatch(line string) {
	low := strings.ToLower(line)
	switch {
	case strings.HasPrefix(low, "bestmove"):
		f := strings.Fields(line)
		bm := ""
		if len(f) > 1 {
			bm = f[1]
		}
		select {
		case c.bestCh <- bm:
		default:
		}
	case strings.HasPrefix(low, "info"):
		if info, ok := ParseInfoLine(line); ok {
			c.storeInfo(info)
		}
	default:
		select {
		case c.ctrlCh <- line:
		default:
			// 控制通道写满：丢弃。握手阶段输出量极小，不会触发。
		}
	}
}

func (c *Client) storeInfo(info InfoLine) {
	key := info.MultiPV
	if key <= 0 {
		key = 1
	}
	c.infoMu.Lock()
	if old, ok := c.curInfo[key]; !ok || info.Depth >= old.Depth {
		c.curInfo[key] = info
	}
	c.infoMu.Unlock()
	select {
	case c.infoSig <- struct{}{}:
	default:
	}
}

// Snapshot 返回当前 MultiPV 各条候选的最新信息（按 multipv 升序）。
func (c *Client) Snapshot() []InfoLine {
	c.infoMu.Lock()
	out := make([]InfoLine, 0, len(c.curInfo))
	for _, v := range c.curInfo {
		out = append(out, v)
	}
	c.infoMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].MultiPV < out[j].MultiPV })
	return out
}

// InfoSignal 返回「有新 info 到达」的信号 channel（容量 1，可安全重复读取）。
func (c *Client) InfoSignal() <-chan struct{} { return c.infoSig }

func (c *Client) clearInfo() {
	c.infoMu.Lock()
	c.curInfo = map[int]InfoLine{}
	c.infoMu.Unlock()
}

// send 向引擎写入一行命令。
func (c *Client) send(cmd string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil || c.closed {
		return errors.New("引擎未启动或已关闭")
	}
	if _, err := io.WriteString(c.stdin, cmd+"\n"); err != nil {
		return fmt.Errorf("向引擎写入命令失败: %w", err)
	}
	return nil
}

// waitCtrl 等待一条满足 match 的控制行。
func (c *Client) waitCtrl(timeout time.Duration, match func(string) bool) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case line := <-c.ctrlCh:
			if match(line) {
				return line, nil
			}
		case <-c.exited:
			msg := c.ExitError()
			if msg == "" {
				msg = "进程已退出"
			}
			return "", fmt.Errorf("引擎进程异常结束: %s", msg)
		case <-timer.C:
			return "", fmt.Errorf("等待引擎响应超时（%.1f 秒）", timeout.Seconds())
		}
	}
}

// probe 完成协议探测与参数收集。
func (c *Client) probe(timeout time.Duration) (Protocol, string, string, []Option, error) {
	name, author := "", ""
	var opts []Option
	proto := ProtoUnknown

	tryOnce := func(cmd string, budget time.Duration) bool {
		if err := c.send(cmd); err != nil {
			return false
		}
		deadline := time.Now().Add(budget)
		for {
			remain := time.Until(deadline)
			if remain <= 0 {
				return false
			}
			line, err := c.waitCtrl(remain, func(l string) bool {
				ll := strings.ToLower(strings.TrimSpace(l))
				return ll == "uciok" || ll == "ucciok" ||
					strings.HasPrefix(ll, "id ") || strings.HasPrefix(ll, "option ")
			})
			if err != nil {
				return false
			}
			ll := strings.ToLower(strings.TrimSpace(line))
			switch {
			case ll == "uciok":
				proto = ProtoUCI
				return true
			case ll == "ucciok":
				proto = ProtoUCCI
				return true
			case strings.HasPrefix(ll, "id name "):
				name = strings.TrimSpace(line[len("id name "):])
			case strings.HasPrefix(ll, "id author "):
				author = strings.TrimSpace(line[len("id author "):])
			case strings.HasPrefix(ll, "option "):
				if o, ok := ParseOptionLine(line); ok {
					opts = append(opts, o)
				}
			}
		}
	}

	// 第 1 步：标准 UCI 探测
	if !tryOnce("uci", timeout) {
		// 第 2 步：兼容只认 ucci 的老引擎
		if !tryOnce("ucci", timeout) {
			return ProtoUnknown, "", "", nil, fmt.Errorf(
				"%s 无法识别为象棋引擎（发送 uci/ucci 后均无 uciok/ucciok 响应）", filepath.Base(c.path))
		}
	}
	SortOptions(opts)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(c.path), filepath.Ext(c.path))
	}
	return proto, name, author, opts, nil
}

// SetOption 下发一个引擎参数。
func (c *Client) SetOption(name, value string) error {
	return c.send(fmt.Sprintf("setoption name %s value %s", name, value))
}

// SetOptions 批量下发参数（按名称排序，保证结果可复现）。
func (c *Client) SetOptions(m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := c.SetOption(k, m[k]); err != nil {
			return err
		}
	}
	return nil
}

// IsReady 发送 isready 并等待 readyok。
func (c *Client) IsReady(timeout time.Duration) error {
	// 清掉残留控制行，避免读到上一次的 readyok
	for {
		select {
		case <-c.ctrlCh:
			continue
		default:
		}
		break
	}
	if err := c.send("isready"); err != nil {
		return err
	}
	_, err := c.waitCtrl(timeout, func(l string) bool {
		return strings.EqualFold(strings.TrimSpace(l), "readyok")
	})
	return err
}

// Analyze 同步执行一次思考。必须放在独立 goroutine 中调用。
//
// 参数：
//
//	ctx      —— 取消即向引擎发送 stop 并尽快返回（用于「局面已变化，放弃本次思考」）
//	pos      —— 完整局面（引擎不保存历史，每次都要重新下发）
//	lim      —— 思考限制
//	onUpdate —— 每次收到新 info 行时回调（可为 nil，回调里不要做重活）
//
// 返回的 Result.Lines 为该次思考结束时的 MultiPV 快照。
func (c *Client) Analyze(ctx context.Context, pos Position, lim Limit, onUpdate func()) (Result, error) {
	c.mu.Lock()
	if c.busy {
		c.mu.Unlock()
		return Result{}, errors.New("引擎正忙（上一次思考尚未结束）")
	}
	if c.closed || c.stdin == nil {
		c.mu.Unlock()
		return Result{}, errors.New("引擎未启动")
	}
	c.busy = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.busy = false
		c.mu.Unlock()
	}()

	c.clearInfo()
	drainStr(c.bestCh)
	drainSig(c.infoSig)

	if err := c.send(pos.Command(c.proto)); err != nil {
		return Result{}, err
	}
	if err := c.send(lim.Command(c.proto)); err != nil {
		return Result{}, err
	}

	hard := time.NewTimer(lim.HardTimeout())
	defer hard.Stop()

	finish := func(bm string) Result {
		return Aggregate(bm, c.Snapshot())
	}

	for {
		select {
		case bm := <-c.bestCh:
			return finish(bm), nil
		case <-c.infoSig:
			if onUpdate != nil {
				onUpdate()
			}
		case <-hard.C:
			_ = c.send("stop")
			select {
			case bm := <-c.bestCh:
				return finish(bm), nil
			case <-time.After(3 * time.Second):
				return Result{}, fmt.Errorf("引擎思考超时（超过 %.0f 秒）且 stop 后无 bestmove 响应", lim.HardTimeout().Seconds())
			case <-c.exited:
				return Result{}, fmt.Errorf("引擎进程在思考中退出: %s", c.ExitError())
			}
		case <-c.exited:
			return Result{}, fmt.Errorf("引擎进程在思考中退出: %s", c.ExitError())
		case <-ctx.Done():
			_ = c.send("stop")
			select {
			case bm := <-c.bestCh:
				return finish(bm), ctx.Err()
			case <-time.After(2 * time.Second):
				return Result{}, ctx.Err()
			case <-c.exited:
				return Result{}, ctx.Err()
			}
		}
	}
}

// Stop 请求引擎停止当前思考（异步，结果仍通过 bestmove 返回）。
func (c *Client) Stop() error { return c.send("stop") }

// NewGame 通知引擎开始新的一局（部分引擎用该命令清空置换表与历史启发）。
func (c *Client) NewGame() error { return c.send("ucinewgame") }

// Quit 优雅关闭：发送 quit，最多等待 wait，超时则强杀。
func (c *Client) Quit(wait time.Duration) {
	c.mu.Lock()
	if c.stdin != nil && !c.closed {
		_, _ = io.WriteString(c.stdin, "quit\n")
	}
	c.closed = true
	c.mu.Unlock()

	select {
	case <-c.exited:
		return
	case <-time.After(wait):
		c.Kill()
	}
}

// Kill 强制结束子进程。
func (c *Client) Kill() {
	c.mu.Lock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	cmd := c.cmd
	c.closed = true
	c.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func drainStr(ch chan string) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func drainSig(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
