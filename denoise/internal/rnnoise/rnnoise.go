// Package rnnoise is a bit-exact 1:1 pure-Go port of Xiph RNNoise v0.2
// (github.com/xiph/rnnoise @ 904a876, model 0b50c45), the 48 kHz
// fullband recurrent-neural-network noise suppressor. It follows the
// opus/flac/aec3 parity discipline: every floating-point step matches
// the vendored C oracle (compiled with -ffp-contract=off and forced
// onto vec.h's generic scalar branch) bit-for-bit, verified by the cgo
// parity slices under denoise/internal/parity_tests.
//
// The public entry point is the denoise package; this package holds the
// internal engine and is not part of the stable API.
package rnnoise

// Frame and spectral geometry — librnnoise/src/denoise.h.
const (
	FrameSize  = 480           // FRAME_SIZE: samples per 10 ms frame at 48 kHz.
	WindowSize = 2 * FrameSize // WINDOW_SIZE: 960-pt analysis window.
	FreqSize   = FrameSize + 1 // FREQ_SIZE: 481 non-redundant FFT bins.
	NBBands    = 32            // NB_BANDS: ERB-style band count.
	NBFeatures = 2*NBBands + 1 // NB_FEATURES: 65 network inputs.
)

// Pitch-search geometry — librnnoise/src/denoise.h.
const (
	PitchMinPeriod = 60
	PitchMaxPeriod = 768
	PitchFrameSize = 960
	PitchBufSize   = PitchMaxPeriod + PitchFrameSize
)
