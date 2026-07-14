package denoise

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/rnnoise"
	"github.com/daniel-sullivan/go-mediatoolkit/resample"
)

// rnnoiseNativeRate is RNNoise's fixed operating rate. The network,
// window, and band table are all trained for 48 kHz fullband audio.
const rnnoiseNativeRate = 48000

// RNNoiseConfig configures a RNNoise engine.
//
// RNNoise is a mono, 48 kHz fullband denoiser. A stream at another rate is
// resampled to 48 kHz and back internally (SincBestQuality). Multichannel
// input is not supported: construct one engine per channel, or downmix
// first.
type RNNoiseConfig struct {
	// SampleRate is the interleaved stream rate in Hz (0 defaults to
	// 48000). Any rate is accepted; non-48 kHz streams are resampled.
	SampleRate int
	// Channels must be 1 (0 defaults to 1).
	Channels int
}

// RNNoise is the pure-Go RNNoise v0.2 noise-suppression engine — a
// bit-exact 1:1 port of Xiph RNNoise (verified frame-for-frame against
// the C reference). It implements Engine and additionally exposes the
// network's per-frame voice-activity Probability, a free by-product of
// the denoiser that can drive vad's Gate/Ducker.
type RNNoise struct {
	sampleRate int
	channels   int

	st *rnnoise.State

	up   resample.Converter // native -> 48 kHz (nil when already 48 kHz)
	down resample.Converter // 48 kHz -> native (nil when already 48 kHz)

	in48  []float32 // pending 48 kHz mono samples (±32768) awaiting framing
	out48 []float32 // denoised 48 kHz samples awaiting downsampling
	outN  []float64 // denoised native-rate samples ([-1,1]) awaiting emit

	prob atomic.Uint64 // float64 bits; lock-free reader
}

// NewRNNoise constructs a RNNoise engine for the given configuration.
func NewRNNoise(cfg RNNoiseConfig) (*RNNoise, error) {
	if cfg.SampleRate == 0 {
		cfg.SampleRate = rnnoiseNativeRate
	}
	if cfg.Channels == 0 {
		cfg.Channels = 1
	}
	if cfg.Channels != 1 {
		return nil, fmt.Errorf("denoise: RNNoise is mono (Channels 1, got %d); construct one engine per channel or downmix first", cfg.Channels)
	}
	if cfg.SampleRate < 8000 || cfg.SampleRate > 384000 {
		return nil, fmt.Errorf("denoise: SampleRate %d out of range [8000, 384000]", cfg.SampleRate)
	}
	r := &RNNoise{
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		st:         rnnoise.NewState(),
	}
	if cfg.SampleRate != rnnoiseNativeRate {
		up, err := resample.New(resample.SincBestQuality, 1)
		if err != nil {
			return nil, fmt.Errorf("denoise: resampler: %w", err)
		}
		down, err := resample.New(resample.SincBestQuality, 1)
		if err != nil {
			up.Close()
			return nil, fmt.Errorf("denoise: resampler: %w", err)
		}
		r.up = up
		r.down = down
	}
	return r, nil
}

// latencyFrames is RNNoise's algorithmic delay in 48 kHz frames: one
// frame accumulates before analysis and one frame of the delayed
// spectrum is synthesised, so denoised output lags input by ~2 frames.
const latencyFrames = 2

// Process denoises interleaved mono samples in place, following the
// normalised [-1, 1] float64 convention. Non-48 kHz streams are resampled
// to 48 kHz and back; the engine's Latency of leading output is silence.
func (r *RNNoise) Process(samples []float64) {
	// Ingest: bring the input to 48 kHz mono at ±32768 scale.
	if r.up == nil {
		for _, s := range samples {
			r.in48 = append(r.in48, float32(s*32768.0))
		}
	} else {
		up48 := streamResample(r.up, samples, resample.Ratio{InputRate: r.sampleRate, OutputRate: rnnoiseNativeRate})
		for _, s := range up48 {
			r.in48 = append(r.in48, float32(s*32768.0))
		}
	}

	// Denoise whole 48 kHz frames.
	for len(r.in48) >= rnnoise.FrameSize {
		frame := r.in48[:rnnoise.FrameSize]
		out := make([]float32, rnnoise.FrameSize)
		p := r.st.ProcessFrame(out, frame)
		r.prob.Store(math.Float64bits(float64(p)))
		r.out48 = append(r.out48, out...)
		r.in48 = r.in48[rnnoise.FrameSize:]
	}

	// Egress: back to native rate at [-1,1].
	if r.down == nil {
		for _, s := range r.out48 {
			r.outN = append(r.outN, float64(s)/32768.0)
		}
		r.out48 = r.out48[:0]
	} else if len(r.out48) > 0 {
		norm := make([]float64, len(r.out48))
		for i, s := range r.out48 {
			norm[i] = float64(s) / 32768.0
		}
		r.out48 = r.out48[:0]
		down := streamResample(r.down, norm, resample.Ratio{InputRate: rnnoiseNativeRate, OutputRate: r.sampleRate})
		r.outN = append(r.outN, down...)
	}

	// Emit len(samples), padding the start-up latency with silence.
	for i := range samples {
		if len(r.outN) == 0 {
			samples[i] = 0
			continue
		}
		samples[i] = r.outN[0]
		r.outN = r.outN[1:]
	}
}

// streamResample feeds in through conv at the given ratio and returns all
// output produced so far, looping until every input frame is consumed.
func streamResample(conv resample.Converter, in []float64, ratio resample.Ratio) []float64 {
	if len(in) == 0 {
		return nil
	}
	var out []float64
	off := 0
	for off < len(in) {
		want := int(float64(len(in)-off)*ratio.Float64()) + 64
		buf := make([]float64, want)
		d := &resample.Data{DataIn: in[off:], DataOut: buf, Ratio: ratio}
		if err := conv.Process(d); err != nil {
			break
		}
		out = append(out, buf[:d.OutputFramesGen]...)
		if d.InputFramesUsed == 0 && d.OutputFramesGen == 0 {
			break
		}
		off += d.InputFramesUsed
	}
	return out
}

// Reset clears all internal state for reuse on a fresh stream.
func (r *RNNoise) Reset() {
	r.st.Reset()
	if r.up != nil {
		r.up.Reset()
	}
	if r.down != nil {
		r.down.Reset()
	}
	r.in48 = r.in48[:0]
	r.out48 = r.out48[:0]
	r.outN = r.outN[:0]
	r.prob.Store(0)
}

// Latency reports the engine's approximate algorithmic delay: RNNoise's
// ~2-frame (20 ms) pipeline delay. When the stream is not 48 kHz, the
// resampler adds a small additional group delay not included here.
func (r *RNNoise) Latency() time.Duration {
	return time.Duration(latencyFrames*rnnoise.FrameSize) * time.Second / rnnoiseNativeRate
}

// SampleRate reports the stream rate the engine was constructed for.
func (r *RNNoise) SampleRate() int { return r.sampleRate }

// Channels reports the channel count the engine was constructed for (1).
func (r *RNNoise) Channels() int { return r.channels }

// Probability reports the most recent frame's voice-activity probability
// in [0, 1], safe to read from any goroutine while another drives
// Process. It is a free by-product of the RNNoise network.
func (r *RNNoise) Probability() float64 {
	return math.Float64frombits(r.prob.Load())
}

// Compile-time check: RNNoise satisfies Engine (and mutations.Processor).
var _ Engine = (*RNNoise)(nil)
