// Package denoise provides streaming single-channel noise suppression
// for interleaved float64 audio, as pluggable engines behind one
// interface — mirroring the multi-engine structure of the vad package.
//
// An engine is a mutations.Processor that MUTATES the stream in place:
// Process replaces the samples it is given with their denoised version.
// Because suppression is spectral (overlap-add on windowed frames) and,
// for engines whose native rate differs from the stream, resampled, an
// engine introduces latency; Latency reports it honestly so callers can
// compensate (e.g. as timeline lookahead).
//
// # Engines
//
//   - RNNoise (NewRNNoise) — a bit-exact 1:1 pure-Go port of Xiph
//     RNNoise v0.2, a 48 kHz fullband recurrent-network denoiser. It
//     also exposes a voice-activity Probability as a free by-product,
//     so it can drive vad's Gate/Ducker without a separate detector.
//   - GTCRN (NewGTCRN) — a pure-Go hand-port of the GTCRN streaming
//     speech-enhancement network, parity-gated against onnxruntime; a
//     compact 16 kHz neural denoiser. Input rates above 16 kHz are
//     rejected (the caller resamples); lower rates are resampled
//     internally.
//
// # One feeder per stream
//
// An Engine is a mutations.Processor and inherits its contract: a single
// instance is bound to the stream it was constructed for and is not safe
// to share across logical streams or goroutines without external
// synchronisation. Tuning that is safe to change mid-stream is exposed
// through lock-free atomics on the concrete type.
package denoise

import (
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Engine is a streaming noise-suppression effect. It is a
// mutations.Processor (in-place Process + Reset) plus stream-geometry
// and latency reporting.
type Engine interface {
	mutations.Processor

	// Latency is the effect's algorithmic delay: the time between a
	// sample entering Process and its denoised value leaving. Feed this
	// much trailing silence at end-of-stream to flush the tail.
	Latency() time.Duration

	// SampleRate is the interleaved stream sample rate the engine was
	// constructed for, in Hz.
	SampleRate() int

	// Channels is the interleaved channel count the engine was
	// constructed for.
	Channels() int
}
