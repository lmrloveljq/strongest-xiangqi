//go:build windows

package ui

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// 本文件实现走子 / 吃子 / 将军三种音效。
//
// 设计取舍：
//   - **不引入任何第三方音频库**（本例要交付单文件 exe、零运行时依赖）；
//   - 三个音效在首次使用时用代码合成 16bit PCM WAV，落到 %TEMP% 下一次，
//     之后用 Win32 的 PlaySound(SND_ASYNC) 异步播放，绝不阻塞界面；
//   - 播放失败（设备忙 / 无音频设备）一律静默忽略，不影响下棋。
//
// 用户要求：将军**只要不同的音效**，不要「被将军的将/帅红框闪烁」——
// 所以棋盘那边已经不画任何将军高亮了，提示只靠声音 + 状态栏文字。

var (
	winmm          = syscall.NewLazyDLL("winmm.dll")
	procPlaySoundW = winmm.NewProc("PlaySoundW")
)

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndFilename  = 0x00020000
)

var (
	soundOnce  sync.Once
	soundPaths [3]string // 0=走子 1=吃子 2=将军
	// soundOff：用户在「设置 → 走棋音效」里关掉后为真
	soundOff atomic.Bool
)

// setSoundEnabled 设置音效总开关。
func setSoundEnabled(on bool) { soundOff.Store(!on) }

// playSound 异步播放第 idx 个音效（0=走子 1=吃子 2=将军）。
func playSound(idx int) {
	if soundOff.Load() {
		return
	}
	soundOnce.Do(prepareSounds)
	if idx < 0 || idx >= len(soundPaths) || soundPaths[idx] == "" {
		return
	}
	p, err := syscall.UTF16PtrFromString(soundPaths[idx])
	if err != nil {
		return
	}
	procPlaySoundW.Call(uintptr(unsafe.Pointer(p)), 0,
		uintptr(sndAsync|sndFilename|sndNoDefault))
}

// prepareSounds 合成三个 WAV 并写到临时目录。
func prepareSounds() {
	dir := filepath.Join(os.TempDir(), "xiangqi-sound")
	_ = os.MkdirAll(dir, 0o755)

	// 走子：短促干净的「嗒」
	move := synthTone([]toneSeg{{freq: 1000, ms: 38, gain: 0.38, decay: 30}})
	// 吃子：清脆的「咔」。
	// 注意：原来用的是 240Hz —— 那是笔记本喇叭几乎放不出来的低频，
	// 所以用户反馈「吃子无音效」。改成 620Hz 主音 + 300Hz 尾音，
	// 前 25ms 混入噪声做「撞击头」，和走子声在音色上区分得很明显。
	capture := synthTone([]toneSeg{
		{freq: 620, ms: 45, gain: 0.62, decay: 34, noise: 0.62},
		{freq: 300, ms: 70, gain: 0.40, decay: 22, noise: 0.18},
	})
	// 将军：两段下行警示音，与上面两个明显不同（1240Hz→820Hz，共 180ms）
	check := synthTone([]toneSeg{
		{freq: 1240, ms: 90, gain: 0.42, decay: 12},
		{freq: 820, ms: 90, gain: 0.42, decay: 12},
	})

	names := [3]string{"move.wav", "capture.wav", "check.wav"}
	data := [3][]byte{move, capture, check}
	for i := range names {
		p := filepath.Join(dir, names[i])
		if err := os.WriteFile(p, data[i], 0o644); err == nil {
			soundPaths[i] = p
		}
	}
}

type toneSeg struct {
	freq  float64 // 基频 Hz
	ms    int     // 时长
	gain  float64 // 振幅 0~1
	decay float64 // 指数衰减系数（越大衰减越快）
	noise float64 // 噪声混入比例 0~1
}

const soundRate = 22050

// synthTone 把若干音段合成为 16bit 单声道 WAV 字节流。
func synthTone(segs []toneSeg) []byte {
	var pcm []int16
	phase := 0.0
	// 固定种子的简单 LCG，保证每次生成的噪声完全一致（便于复现）
	seed := uint32(20261007)
	for _, s := range segs {
		n := soundRate * s.ms / 1000
		for i := 0; i < n; i++ {
			t := float64(i) / float64(soundRate)
			env := math.Exp(-s.decay * t)
			v := math.Sin(phase)
			phase += 2 * math.Pi * s.freq / float64(soundRate)
			if s.noise > 0 {
				seed = seed*1664525 + 1013904223
				nz := float64(int32(seed>>8)%20001-10000) / 10000.0
				v = v*(1-s.noise) + nz*s.noise
			}
			sample := v * env * s.gain
			if sample > 1 {
				sample = 1
			} else if sample < -1 {
				sample = -1
			}
			pcm = append(pcm, int16(sample*32000))
		}
	}

	// WAV 头（44 字节）
	dataLen := len(pcm) * 2
	buf := make([]byte, 0, 44+dataLen)
	le := binary.LittleEndian
	put32 := func(v uint32) { b := make([]byte, 4); le.PutUint32(b, v); buf = append(buf, b...) }
	put16 := func(v uint16) { b := make([]byte, 2); le.PutUint16(b, v); buf = append(buf, b...) }

	buf = append(buf, 'R', 'I', 'F', 'F')
	put32(uint32(36 + dataLen))
	buf = append(buf, 'W', 'A', 'V', 'E')
	buf = append(buf, 'f', 'm', 't', ' ')
	put32(16)            // fmt 块长度
	put16(1)             // PCM
	put16(1)             // 单声道
	put32(soundRate)     // 采样率
	put32(soundRate * 2) // 字节率
	put16(2)             // 块对齐
	put16(16)            // 位深
	buf = append(buf, 'd', 'a', 't', 'a')
	put32(uint32(dataLen))
	for _, s := range pcm {
		put16(uint16(s))
	}
	return buf
}
