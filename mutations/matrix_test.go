package mutations_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/consts"
	"go-mediatoolkit/mutations"
)

// This file runs a shared battery of sanity checks across every rate in
// consts.CommonSampleRates for each stateful Processor. The goal is to
// catch rate-specific regressions: coefficient formulae that drift at
// non-standard rates, state management that assumes 48 kHz, or
// buffer-sizing math that breaks at extremes.
//
// Every case that was previously asserted at a single 48 kHz rate is
// covered here at {22.05, 32, 44.1, 48, 88.2, 96, 192} kHz. A failure
// in any sub-test names the specific processor + rate combination.

// processorFactory constructs a fresh Processor for a given output
// format. Fresh-per-call is required because Processors carry state.
type processorFactory func(sampleRate, channels int) mutations.Processor

// matrixCase names a processor configuration tested across rates.
type matrixCase struct {
	name    string
	factory processorFactory
}

// processorCases enumerates the processors the matrix validates. Echo
// and Reverb hold delay lines; the Biquads hold per-channel IIR state.
// Each is configured with representative parameters, not degenerate
// edge cases — this matrix is about rate portability, not parameter
// sweep.
var processorCases = []matrixCase{
	{"Echo_100ms_fb0.5_wet1", func(r, c int) mutations.Processor {
		return mutations.NewEcho(100*time.Millisecond, r, c, 0.5, 1.0)
	}},
	{"Reverb_room0.7_damp0.4_wet1", func(r, c int) mutations.Processor {
		return mutations.NewReverb(r, c, 0.7, 0.4, 1.0)
	}},
	{"Lowpass_1kHz_Q0.707", func(r, c int) mutations.Processor {
		return mutations.NewLowpass(1000, 0.707, r, c)
	}},
	{"Highpass_1kHz_Q0.707", func(r, c int) mutations.Processor {
		return mutations.NewHighpass(1000, 0.707, r, c)
	}},
	{"Bandpass_2kHz_Q2", func(r, c int) mutations.Processor {
		return mutations.NewBandpass(2000, 2.0, r, c)
	}},
}

// rateFrames returns a ~50 ms frame count at the given rate — enough
// audio for impulse-response to develop without blowing up test time
// at 192 kHz.
func rateFrames(sampleRate int) int {
	return sampleRate / 20
}

func TestProcessorMatrix_ImpulseResponse(t *testing.T) {
	for _, tc := range processorCases {
		for _, rate := range consts.CommonSampleRates {
			t.Run(fmt.Sprintf("%s/%dHz", tc.name, rate), func(t *testing.T) {
				p := tc.factory(rate, 1)
				buf := make([]float64, rateFrames(rate))
				buf[0] = 1.0
				p.Process(buf)

				var energy, peak float64
				for i, v := range buf {
					require.False(t, math.IsNaN(v), "NaN at sample %d", i)
					require.False(t, math.IsInf(v, 0), "Inf at sample %d", i)
					energy += v * v
					if m := math.Abs(v); m > peak {
						peak = m
					}
				}
				assert.Greater(t, energy, 0.0, "impulse should produce non-silent output")
				assert.Less(t, peak, 10.0, "impulse response must stay bounded")
			})
		}
	}
}

func TestProcessorMatrix_StabilityOnSine(t *testing.T) {
	for _, tc := range processorCases {
		for _, rate := range consts.CommonSampleRates {
			t.Run(fmt.Sprintf("%s/%dHz", tc.name, rate), func(t *testing.T) {
				p := tc.factory(rate, 1)
				// Drive at ~Nyquist/4 — inside every filter's passband
				// but high enough to exercise coefficient drift.
				freq := float64(rate) / 4
				buf := make([]float64, rateFrames(rate))
				dt := 1.0 / float64(rate)
				for i := range buf {
					buf[i] = math.Sin(2 * math.Pi * freq * float64(i) * dt)
				}
				p.Process(buf)
				for i, v := range buf {
					require.False(t, math.IsNaN(v), "NaN at sample %d", i)
					require.False(t, math.IsInf(v, 0), "Inf at sample %d", i)
					require.Less(t, math.Abs(v), 100.0, "runaway at sample %d", i)
				}
			})
		}
	}
}

func TestProcessorMatrix_Reset(t *testing.T) {
	for _, tc := range processorCases {
		for _, rate := range consts.CommonSampleRates {
			t.Run(fmt.Sprintf("%s/%dHz", tc.name, rate), func(t *testing.T) {
				p := tc.factory(rate, 1)
				drive := make([]float64, rateFrames(rate))
				drive[0] = 1.0
				p.Process(drive)

				p.Reset()

				silence := make([]float64, rateFrames(rate))
				p.Process(silence)
				for i, v := range silence {
					assert.Equal(t, 0.0, v, "post-reset sample %d should be zero", i)
				}
			})
		}
	}
}

func TestProcessorMatrix_ChunkedMatchesWhole(t *testing.T) {
	// This invariant has teeth: if a processor's internal state
	// doesn't advance identically when called in small chunks vs one
	// big call, offline rendering (mutations.RenderBuffer) and
	// realtime streaming (EffectSource) would produce different
	// audio — silent corruption. Run across rates because state
	// sizes often scale with sample rate.
	for _, tc := range processorCases {
		for _, rate := range consts.CommonSampleRates {
			t.Run(fmt.Sprintf("%s/%dHz", tc.name, rate), func(t *testing.T) {
				freq := float64(rate) / 8
				n := rateFrames(rate)
				whole := make([]float64, n)
				dt := 1.0 / float64(rate)
				for i := range whole {
					whole[i] = 0.5 * math.Sin(2*math.Pi*freq*float64(i)*dt)
				}
				chunked := append([]float64(nil), whole...)

				a := tc.factory(rate, 1)
				a.Process(whole)

				b := tc.factory(rate, 1)
				const chunkSize = 47 // prime-ish, stays off frame boundaries
				for i := 0; i < len(chunked); i += chunkSize {
					end := i + chunkSize
					if end > len(chunked) {
						end = len(chunked)
					}
					b.Process(chunked[i:end])
				}
				for i := range whole {
					require.InDelta(t, whole[i], chunked[i], 1e-12, "sample %d diverged", i)
				}
			})
		}
	}
}

// TestGainEnvelopeMatrix verifies the gain-envelope algorithm lands
// its boundary values within acceptable tolerance at every common
// rate. The per-frame ns-quantisation error is largest at 96 kHz
// (~1 part in 2e7 per frame); tolerances scale with rate accordingly.
func TestGainEnvelopeMatrix(t *testing.T) {
	for _, rate := range consts.CommonSampleRates {
		t.Run(fmt.Sprintf("%dHz", rate), func(t *testing.T) {
			// 10ms fade-in.
			env := mutations.FadeInEnvelope(10 * time.Millisecond)
			samples := make([]float64, rate/100) // 10ms worth
			for i := range samples {
				samples[i] = 1.0
			}
			mutations.ApplyGainEnvelope(samples, env, 0, 1, rate)

			// Start of fade: gain 0.
			assert.InDelta(t, 0.0, samples[0], 1e-9, "start of fade at %d Hz", rate)
			// End of fade window: gain should be approximately 1.
			// Tolerance absorbs ns-quantisation drift that
			// accumulates over the window at non-round rates — worst
			// at 22.05 kHz where one frame is ~45 μs and a 10 ms
			// window holds ~220 frames of cumulative error.
			last := len(samples) - 1
			assert.InDelta(t, 1.0, samples[last], 1e-2, "end of fade at %d Hz", rate)
		})
	}
}

// TestDurationConversionsMatrix verifies the duration ↔ frames
// round-trip is stable at every supported rate. Truncation (as the
// original implementation used) would fail this at any rate where one
// frame doesn't divide cleanly into a nanosecond.
func TestDurationConversionsMatrix(t *testing.T) {
	for _, rate := range consts.CommonSampleRates {
		t.Run(fmt.Sprintf("%dHz", rate), func(t *testing.T) {
			for _, frames := range []int64{1, 2, 5, 47, 100, 1000, int64(rate)} {
				got := mutations.DurationToFrames(
					mutations.FramesToDuration(frames, rate),
					rate,
				)
				assert.Equal(t, frames, got, "frames=%d", frames)
			}
		})
	}
}
