package denoise

import (
	"errors"
	"fmt"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/gtcrn"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/resample"
)

// modelSampleRate is the only rate the GTCRN graph runs at (16 kHz).
const modelSampleRate = gtcrn.SampleRate

// ErrUnsupportedSampleRate is returned by NewGTCRN when the requested
// input rate exceeds the model's 16 kHz operating rate. Per the resolved
// design decision the engine RESTRICTS rather than resamples down: it
// will not silently discard the high-band content a denoiser is judged
// on. Callers with >16 kHz audio must resample explicitly and own that
// trade-off. Rates below 16 kHz ARE resampled up into the model band.
var ErrUnsupportedSampleRate = errors.New("denoise: GTCRN operates at 16000 Hz; a higher input rate must be resampled by the caller")

// ErrBadChannels is returned by NewGTCRN for a channel count outside 1..64.
var ErrBadChannels = errors.New("denoise: channels must be in 1..64")

// GTCRNConfig configures a GTCRN engine. The zero value is not valid;
// SampleRate and Channels must be set.
type GTCRNConfig struct {
	// SampleRate is the input sample rate in Hz. The model runs at
	// 16000 Hz. A rate ABOVE 16000 is rejected (ErrUnsupportedSampleRate)
	// rather than downsampled; a rate below 16000 is resampled up into
	// the model band and the enhanced audio resampled back.
	SampleRate int

	// Channels is the interleaved input channel count, in 1..64.
	// Multi-channel input is downmixed to mono, enhanced, and the mono
	// result written to every output channel.
	Channels int
}

// GTCRN is the pure-Go GTCRN streaming speech-denoise engine: a hand-port
// of the vendored gtcrn.onnx graph (denoise/internal/gtcrn), parity-gated
// against onnxruntime. It enhances 16 kHz mono speech by masking the
// complex STFT; the STFT/ISTFT front end runs outside the network (see
// the internal package's VERSION spec). One instance serves one stream
// and is not safe for concurrent use.
//
// GTCRN implements the package Engine interface (mutations.Processor plus
// Latency/SampleRate/Channels).
type GTCRN struct {
	sampleRate int
	channels   int
	stream     *gtcrn.Streamer

	up, down resample.Converter // nil when sampleRate == 16000
	outq     []float64          // enhanced samples at input rate, awaiting emission

	mono   []float64 // downmix scratch
	rsIn   []float32 // 16 kHz mono into the streamer
	rsBack []float64 // resampler output scratch
}

// NewGTCRN constructs a GTCRN engine from cfg. It returns
// ErrUnsupportedSampleRate for a rate above 16000, ErrBadChannels for a
// channel count outside 1..64.
func NewGTCRN(cfg GTCRNConfig) (*GTCRN, error) {
	if cfg.SampleRate <= 0 {
		return nil, fmt.Errorf("denoise: sample rate must be positive, got %d", cfg.SampleRate)
	}
	if cfg.SampleRate > modelSampleRate {
		return nil, fmt.Errorf("%w (got %d Hz)", ErrUnsupportedSampleRate, cfg.SampleRate)
	}
	if cfg.Channels < 1 || cfg.Channels > 64 {
		return nil, ErrBadChannels
	}
	model, err := gtcrn.NewModel()
	if err != nil {
		return nil, err
	}
	g := &GTCRN{
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		stream:     gtcrn.NewStreamer(model),
	}
	if cfg.SampleRate != modelSampleRate {
		// Sinc resampling both ways (97 dB SNR), mono.
		if g.up, err = resample.New(resample.SincFastest, 1); err != nil {
			return nil, err
		}
		if g.down, err = resample.New(resample.SincFastest, 1); err != nil {
			g.up.Close()
			return nil, err
		}
	}
	return g, nil
}

// Process enhances samples in place (interleaved, Channels() wide),
// leaving trailing partial frames untouched. Output is delayed by
// Latency(): the first Latency worth of samples are silent while the
// pipeline fills, and a matching tail remains buffered — feed trailing
// silence at end-of-stream to flush it. Never panics.
func (g *GTCRN) Process(samples []float64) {
	frames := len(samples) / g.channels
	if frames == 0 {
		return
	}

	// Downmix to mono at the input rate.
	g.mono = resizeF64(g.mono, frames)
	downmixMono(samples, g.channels, frames, g.mono)

	// Up-resample to 16 kHz if needed.
	at16k := g.mono
	if g.up != nil {
		at16k = g.resampleTo(g.up, g.mono, modelSampleRate, g.sampleRate)
	}

	// Denoise (streamer returns one 16 kHz sample per input sample).
	g.rsIn = resizeF32(g.rsIn, len(at16k))
	for i, v := range at16k {
		g.rsIn[i] = float32(v)
	}
	den := g.stream.Process(g.rsIn)

	// Down-resample back to the input rate.
	if g.down != nil {
		g.rsBack = resizeF64(g.rsBack, len(den))
		for i, v := range den {
			g.rsBack[i] = float64(v)
		}
		back := g.resampleTo(g.down, g.rsBack, g.sampleRate, modelSampleRate)
		g.outq = append(g.outq, back...)
	} else {
		for _, v := range den {
			g.outq = append(g.outq, float64(v))
		}
	}

	// Emit exactly `frames` samples, broadcast to every channel. A
	// startup underflow (pipeline not yet primed) emits silence.
	emit := frames
	if emit > len(g.outq) {
		emit = len(g.outq)
	}
	for i := 0; i < frames; i++ {
		var v float64
		if i < emit {
			v = g.outq[i]
		}
		base := i * g.channels
		for c := 0; c < g.channels; c++ {
			samples[base+c] = v
		}
	}
	g.outq = g.outq[emit:]
}

// resampleTo streams src through conv at ratio outRate/inRate, returning
// the generated samples (may differ in length from src). It loops until
// every input frame is consumed rather than assuming a single Process
// pass drains all of src, matching streamResample; a converter error is
// treated as end-of-input (consistent with the RNNoise path — Process
// has no error channel).
func (g *GTCRN) resampleTo(conv resample.Converter, src []float64, outRate, inRate int) []float64 {
	ratio := resample.Ratio{InputRate: inRate, OutputRate: outRate}
	var out []float64
	off := 0
	for off < len(src) {
		want := (len(src)-off)*outRate/inRate + 64
		buf := make([]float64, want)
		d := &resample.Data{DataIn: src[off:], DataOut: buf, Ratio: ratio}
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

// Reset clears the carried stream state so the engine can serve a fresh
// stream.
func (g *GTCRN) Reset() {
	g.stream.Reset()
	g.outq = g.outq[:0]
	if g.up != nil {
		g.up.Reset()
	}
	if g.down != nil {
		g.down.Reset()
	}
}

// Latency reports the engine's algorithmic latency: the streaming STFT
// overlap-add fill (one analysis window, 32 ms at 16 kHz). Any
// resampling adds a small filter delay on top; the network itself is
// causal frame to frame.
func (g *GTCRN) Latency() time.Duration {
	return time.Duration(g.stream.LatencySamples()) * time.Second / time.Duration(modelSampleRate)
}

// SampleRate reports the configured input sample rate in Hz.
func (g *GTCRN) SampleRate() int { return g.sampleRate }

// Channels reports the configured interleaved channel count.
func (g *GTCRN) Channels() int { return g.channels }

// downmixMono folds interleaved samples to mono: 1 channel copies, 2
// use the ITU downmix, >2 average equally.
func downmixMono(samples []float64, channels, frames int, dst []float64) {
	switch channels {
	case 1:
		copy(dst, samples[:frames])
	case 2:
		mutations.DownmixStereoToMono(samples[:frames*2], dst)
	default:
		w := 1.0 / float64(channels)
		for i := 0; i < frames; i++ {
			var sum float64
			base := i * channels
			for c := 0; c < channels; c++ {
				sum += samples[base+c]
			}
			dst[i] = sum * w
		}
	}
}

func resizeF64(s []float64, n int) []float64 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]float64, n)
}

func resizeF32(s []float32, n int) []float32 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]float32, n)
}

// GTCRN implements Engine.
var _ Engine = (*GTCRN)(nil)
