package vad

import (
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/resample"
)

// engineAdapter bridges the toolkit's stream convention (interleaved
// float64, any sample rate, any channel count) to a detection engine's
// rigid contract (mono, a fixed engine rate, fixed-size decision
// frames). It owns the full front-end pipeline:
//
//	downmix → resample → frame
//
// plus the position bookkeeping that maps engine-domain sample
// positions back to input-stream frames, and the value-conversion
// helpers (frameToInt16 / frameToFloat32) engines with integer or
// float32 contracts apply per frame.
//
//   - downmix: 1 channel is copied (never aliased — the emitted frames
//     may be mutated by the engine, and Process must not touch caller
//     audio); 2 channels use mutations.DownmixStereoToMono; >2
//     channels use an equal-weight average (no ITU downmix matrix —
//     a VAD wants "is anyone talking on any channel", not a
//     listenable fold-down).
//   - resample: nothing when engineRate == inRate; otherwise a
//     streaming resample.SincFastest converter. SincFastest (not
//     ZOH/Linear) because cheap interpolators alias high frequencies
//     down into the 0–4 kHz bands every engine's features live in;
//     97 dB SNR is far more rejection than any decision needs.
//   - frame: a mutations.StreamChunker accumulates engine-rate mono
//     samples and emits exactly frameSize-sample decision frames.
//     Trailing samples that do not fill a frame stay buffered for the
//     next push — nothing is dropped mid-stream (engines decide on
//     whole frames only; a final partial frame at end-of-stream is
//     simply never decided on).
//
// # Position mapping and latency
//
// The resample package does not export its filter delay, so the
// adapter measures it once at construction with an impulse probe: a
// unit impulse is pushed through a throwaway converter and the delay
// taken as the ENERGY CENTROID of the response (not the argmax — the
// centroid is robust to the near-symmetric ripple around the main
// lobe). Measuring rather than assuming matters: libsamplerate-style
// sinc converters centre the FIR internally and produce time-ALIGNED
// output, so the probe currently reads ≈ 0 — but the mapping stays
// correct if the converter implementation ever changes. Engine sample
// k maps to input frame round(k·inRate/engineRate) − resamplerDelay,
// clamped to ≥ 0; the mapping is monotonic by construction. Positions
// are sample-accurate when no resampling happens and accurate to
// within one engine decision frame otherwise.
//
// An engineAdapter is owned by its engine's audio goroutine and is not
// safe for concurrent use.
type engineAdapter struct {
	inRate     int
	inChannels int
	engineRate int
	frameSize  int // engine samples per decision frame

	conv    resample.Converter // nil when engineRate == inRate
	ratio   resample.Ratio
	chunker *mutations.StreamChunker

	resamplerDelay int64 // measured group delay, in INPUT frames

	framesEmitted int64 // decision frames handed to fn so far

	mono []float64 // downmix scratch
	out  []float64 // resampler output scratch
}

// probeLen is the impulse-probe input length in samples: long enough
// to flush the longest SincFastest FIR history at any supported ratio,
// short enough to keep construction instant.
const probeLen = 4096

// newEngineAdapter builds the front-end pipeline for an engine running
// at engineRate with frameSize-sample decision frames, fed by an input
// stream of inRate Hz / inChannels channels. The caller has already
// validated the input format; frameSize and engineRate are engine
// constants. The only runtime failure is the resampler rejecting the
// rate ratio (outside [1/256, 256]), surfaced as ErrBadSampleRate.
func newEngineAdapter(inRate, inChannels, engineRate, frameSize int) (*engineAdapter, error) {
	a := &engineAdapter{
		inRate:     inRate,
		inChannels: inChannels,
		engineRate: engineRate,
		frameSize:  frameSize,
		chunker:    mutations.NewStreamChunker(frameSize),
	}
	if engineRate != inRate {
		a.ratio = resample.Ratio{InputRate: inRate, OutputRate: engineRate}
		if !resample.IsValidRatio(a.ratio) {
			return nil, ErrBadSampleRate
		}
		delay, err := measureResamplerDelay(inRate, engineRate)
		if err != nil {
			return nil, err
		}
		a.resamplerDelay = delay
		conv, err := resample.New(resample.SincFastest, 1)
		if err != nil {
			return nil, err
		}
		a.conv = conv
	}
	return a, nil
}

// measureResamplerDelay probes a fresh SincFastest converter with a
// unit impulse and returns its group delay as the energy centroid of
// the response, converted to INPUT frames (rounded to nearest).
func measureResamplerDelay(inRate, engineRate int) (int64, error) {
	c, err := resample.New(resample.SincFastest, 1)
	if err != nil {
		return 0, err
	}
	defer c.Close()

	in := make([]float64, probeLen)
	in[0] = 1
	ratio := float64(engineRate) / float64(inRate)
	out := make([]float64, int(math.Ceil(probeLen*ratio))+64)
	d := &resample.Data{
		DataIn:     in,
		DataOut:    out,
		EndOfInput: true,
		Ratio:      resample.Ratio{InputRate: inRate, OutputRate: engineRate},
	}
	if err := c.Process(d); err != nil {
		return 0, err
	}

	num, den := 0.0, 0.0
	for k := 0; k < d.OutputFramesGen; k++ {
		e := out[k] * out[k]
		num += float64(k) * e
		den += e
	}
	if den == 0 {
		// A resampler that annihilates an impulse would be broken;
		// treat it as zero-delay rather than dividing by zero.
		return 0, nil
	}
	centroidEngine := num / den
	return int64(math.Round(centroidEngine * float64(inRate) / float64(engineRate))), nil
}

// push runs samples (interleaved, inChannels wide) through the
// pipeline and invokes fn once per completed decision frame. frame is
// exactly frameSize engine-rate mono samples; startSample is the
// engine-domain sample position of frame[0] (map it to input frames
// with inputFrame). fn MAY mutate frame in place — it is a view into
// adapter-owned scratch, never into the caller's buffer — but must not
// retain it across calls.
//
// A trailing partial input frame (len(samples) not a multiple of
// inChannels — which well-formed interleaved audio never has) is
// ignored.
func (a *engineAdapter) push(samples []float64, fn func(frame []float64, startSample int64)) {
	frames := len(samples) / a.inChannels
	if frames == 0 {
		return
	}

	a.mono = mutations.ResizeScratch(a.mono, frames)
	switch a.inChannels {
	case 1:
		copy(a.mono, samples[:frames])
	case 2:
		mutations.DownmixStereoToMono(samples[:frames*2], a.mono)
	default:
		ch := a.inChannels
		w := 1.0 / float64(ch)
		for i := 0; i < frames; i++ {
			sum := 0.0
			base := i * ch
			for c := 0; c < ch; c++ {
				sum += samples[base+c]
			}
			a.mono[i] = sum * w
		}
	}

	emit := func(chunk []float64) error {
		start := a.framesEmitted * int64(a.frameSize)
		a.framesEmitted++
		fn(chunk, start)
		return nil
	}

	if a.conv == nil {
		// The chunker's callback never errors here.
		_, _ = a.chunker.Write(a.mono, emit)
		return
	}

	// Streaming resample: loop until the converter has consumed the
	// whole downmixed buffer (it may take input in several bites when
	// the output scratch fills).
	pending := a.mono
	outCap := int(math.Ceil(float64(frames)*a.ratio.Float64())) + 64
	a.out = mutations.ResizeScratch(a.out, outCap)
	for len(pending) > 0 {
		d := &resample.Data{
			DataIn:  pending,
			DataOut: a.out,
			Ratio:   a.ratio,
		}
		// The only Process errors are nil-data and invalid-ratio,
		// both excluded by construction.
		if err := a.conv.Process(d); err != nil {
			return
		}
		if d.OutputFramesGen > 0 {
			_, _ = a.chunker.Write(a.out[:d.OutputFramesGen], emit)
		}
		if d.InputFramesUsed == 0 && d.OutputFramesGen == 0 {
			// No forward progress — cannot happen with a healthy
			// converter; bail rather than spin.
			return
		}
		pending = pending[d.InputFramesUsed:]
	}
}

// inputFrame maps an engine-domain sample position (as passed to a
// push callback) back to an input-stream frame position:
// round(k·inRate/engineRate) − resamplerDelay, clamped to ≥ 0. The
// mapping is monotonic. Sample-accurate when the adapter does not
// resample; within one engine decision frame otherwise.
func (a *engineAdapter) inputFrame(engineSample int64) int64 {
	if a.conv == nil {
		return engineSample
	}
	f := int64(math.Round(float64(engineSample)*float64(a.inRate)/float64(a.engineRate))) - a.resamplerDelay
	if f < 0 {
		return 0
	}
	return f
}

// resamplerLatency reports the measured resampler group delay as a
// duration at the input rate (zero when not resampling).
func (a *engineAdapter) resamplerLatency() time.Duration {
	return mutations.FramesToDuration(a.resamplerDelay, a.inRate)
}

// frameFillLatency reports how long one decision frame takes to fill:
// frameSize samples at the engine rate.
func (a *engineAdapter) frameFillLatency() time.Duration {
	return mutations.FramesToDuration(int64(a.frameSize), a.engineRate)
}

// latencyBase reports the engine-agnostic floor every DecisionLatency
// implementation builds on: the resampler group delay plus one decision
// frame fill. Each engine adds its own onset debounce on top (Energy's
// and WebRTC's hangover/onset frames; Silero's MinSpeech).
func (a *engineAdapter) latencyBase() time.Duration {
	return a.resamplerLatency() + a.frameFillLatency()
}

// pendingSamples reports how many engine-rate samples are buffered
// toward the next decision frame (excludes samples still inside the
// resampler's FIR history).
func (a *engineAdapter) pendingSamples() int { return a.chunker.Pending() }

// reset returns the pipeline to its just-constructed state: buffered
// chunk samples are discarded, the resampler history is cleared, and
// positions restart at zero. The measured resampler delay is a
// property of the converter design, not of stream state, so it is
// retained.
func (a *engineAdapter) reset() {
	a.chunker.Reset()
	if a.conv != nil {
		a.conv.Reset()
	}
	a.framesEmitted = 0
}

// frameToInt16 converts one decision frame to int16 sample values,
// mirroring mutations.Float64ToInt16 exactly (clamp to [-1, 1], scale
// by 32767, truncate toward zero) without the byte round-trip. dst
// must be at least len(src) long.
func frameToInt16(src []float64, dst []int16) {
	for i := 0; i < len(src); i++ {
		v := src[i]
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		dst[i] = int16(v * 32767.0)
	}
}

// frameToFloat32 converts one decision frame to float32 (plain
// narrowing, no clamp — engines with a float32 contract expect the
// same nominal [-1, 1] range as the input). dst must be at least
// len(src) long.
func frameToFloat32(src []float64, dst []float32) {
	for i := 0; i < len(src); i++ {
		dst[i] = float32(src[i])
	}
}
