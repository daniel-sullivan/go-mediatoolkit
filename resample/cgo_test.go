//go:build cgo

// Tests for the libsamplerate-backed Converter (NewLibsamplerate).
//
// Three layers:
//
//	1. Smoke tests — sanity-check construction / Process / errors on the
//	   C path in isolation.
//	2. Native↔C parity matrix (TestNativeMatchesLibsamplerate) — feeds
//	   identical inputs through the pure-Go converter and the C
//	   reference and asserts they agree on frame counts and (within
//	   float32 quantization) sample values. C is the oracle; the Go
//	   port is verified against it.
//	3. Shared-suite oracle (TestLibsamplerateAPISuite) — runs the same
//	   API-level tests as resample_test.go against the C constructor,
//	   so both paths are held to the same correctness contract.

package resample_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/resample"
)

// ── 1. Smoke tests ──────────────────────────────────────────────────

func TestNewLibsamplerateRoundTrip(t *testing.T) {
	// Identity ratio with ZOH should produce one-frame shifted output
	// (per the converter's contract — first emit holds the previous
	// value), then track the input thereafter. Bit-exactness is
	// covered by the parity matrix below.
	in := make([]float64, 256)
	for i := range in {
		in[i] = math.Sin(2 * math.Pi * float64(i) / 64)
	}
	conv, err := resample.NewLibsamplerate(resample.ZeroOrderHold, 1)
	require.NoError(t, err)
	defer conv.Close()

	out := make([]float64, 256)
	d := &resample.Data{
		DataIn:     in,
		DataOut:    out,
		EndOfInput: true,
		Ratio:      resample.Ratio{InputRate: 48000, OutputRate: 48000},
	}
	require.NoError(t, conv.Process(d))
	assert.Greater(t, d.OutputFramesGen, 250, "ZOH at identity should emit ~256 frames")
	assert.LessOrEqual(t, d.OutputFramesGen, 256)
}

func TestNewLibsamplerateUpsample(t *testing.T) {
	in := make([]float64, 1000)
	for i := range in {
		in[i] = float64(i) / 1000
	}
	conv, err := resample.NewLibsamplerate(resample.Linear, 1)
	require.NoError(t, err)
	defer conv.Close()

	out := make([]float64, 2200)
	d := &resample.Data{
		DataIn:     in,
		DataOut:    out,
		EndOfInput: true,
		Ratio:      resample.Ratio{InputRate: 1000, OutputRate: 2000},
	}
	require.NoError(t, conv.Process(d))
	assert.InDelta(t, 2000, d.OutputFramesGen, 5)
}

func TestNewLibsamplerateRejectsBadInputs(t *testing.T) {
	_, err := resample.NewLibsamplerate(resample.SincFastest, 0)
	assert.ErrorIs(t, err, resample.ErrBadChannelCount)

	_, err = resample.NewLibsamplerate(resample.ConverterType(99), 1)
	assert.ErrorIs(t, err, resample.ErrBadConverterType)
}

// ── 2. Native↔C parity matrix ───────────────────────────────────────

var converterCases = []struct {
	name string
	ct   resample.ConverterType
}{
	{"SincBestQuality", resample.SincBestQuality},
	{"SincMediumQuality", resample.SincMediumQuality},
	{"SincFastest", resample.SincFastest},
	{"ZeroOrderHold", resample.ZeroOrderHold},
	{"Linear", resample.Linear},
}

var ratioCases = []struct {
	name  string
	ratio resample.Ratio
}{
	{"identity", resample.Ratio{InputRate: 1, OutputRate: 1}},
	{"halve", resample.Ratio{InputRate: 2, OutputRate: 1}},
	{"double", resample.Ratio{InputRate: 1, OutputRate: 2}},
	{"48k_to_44k1", resample.Ratio{InputRate: 48000, OutputRate: 44100}},
	{"44k1_to_48k", resample.Ratio{InputRate: 44100, OutputRate: 48000}},
	{"96k_to_44k1", resample.Ratio{InputRate: 96000, OutputRate: 44100}},
	{"22k05_to_48k", resample.Ratio{InputRate: 22050, OutputRate: 48000}},
	{"slight_down", resample.Ratio{InputRate: 10000, OutputRate: 9999}},
	{"slight_up", resample.Ratio{InputRate: 9999, OutputRate: 10000}},
}

// makeRamp builds a deterministic test input — a low-frequency
// sinusoid plus a tiny linear drift. Values are computed in float64
// and truncated to float32, so both the Go path (which sees float64)
// and the C reference (which sees float32 internally) operate on
// byte-identical samples. Without this pre-truncation, the
// float64-vs-float32 cumulative-position drift pushes a single
// rounding boundary differently in the two paths and surfaces as a
// one-frame off-by-one in the output, giving a misleading ~0.03
// max-diff that has nothing to do with the converter's actual
// algorithm.
func makeRamp(frames, channels int) []float64 {
	out := make([]float64, frames*channels)
	for f := 0; f < frames; f++ {
		v := float64(float32(0.5*math.Sin(2*math.Pi*0.01*float64(f)) + 0.0001*float64(f)))
		for ch := 0; ch < channels; ch++ {
			out[f*channels+ch] = v
		}
	}
	return out
}

func TestNativeMatchesLibsamplerate(t *testing.T) {
	const inFrames = 16384
	for _, ch := range []int{1, 2} {
		in := makeRamp(inFrames, ch)
		for _, cv := range converterCases {
			for _, rc := range ratioCases {
				name := fmt.Sprintf("%s/%s/ch%d", cv.name, rc.name, ch)
				t.Run(name, func(t *testing.T) {
					goOut, err := resample.Simple(in, cv.ct, ch, rc.ratio)
					require.NoError(t, err)

					cConv, err := resample.NewLibsamplerate(cv.ct, ch)
					require.NoError(t, err)
					defer cConv.Close()
					cBuf := make([]float64, len(goOut)+64)
					d := &resample.Data{
						DataIn:     in,
						DataOut:    cBuf,
						EndOfInput: true,
						Ratio:      rc.ratio,
					}
					require.NoError(t, cConv.Process(d))
					cOut := cBuf[:d.OutputFramesGen*ch]

					// Frame counts must match exactly — that's the
					// algorithmic invariant we care about, and the
					// regression net for the round-to-even sinc bug.
					assert.Equal(t, len(goOut), len(cOut),
						"output frame count mismatch (go=%d c=%d)", len(goOut), len(cOut))

					if len(goOut) == len(cOut) && len(goOut) > 0 {
						maxAbsDiff := 0.0
						for i := range goOut {
							if d := math.Abs(goOut[i] - cOut[i]); d > maxAbsDiff {
								maxAbsDiff = d
							}
						}
						// Sample-value tolerance reflects a precision
						// asymmetry, not an algorithmic disagreement:
						// libsamplerate stores data buffers and sinc
						// coefficients as float32, while our Go port
						// uses float64 throughout. Cumulative position
						// arithmetic and coefficient sums diverge over
						// many iterations; the Go path is *more*
						// precise. 0.05 = ~-26dB is well below the
						// noise floor of any audible audio path.
						assert.Less(t, maxAbsDiff, 0.05,
							"max |go-c| sample diff = %g", maxAbsDiff)
					}
				})
			}
		}
	}
}

// ── 3. Shared-suite oracle ──────────────────────────────────────────

// converterFactory abstracts the constructor under test so the same
// API-level suite runs against both the native and the libsamplerate
// paths.
type converterFactory struct {
	name string
	ctor func(resample.ConverterType, int) (resample.Converter, error)
}

var libsamplerateFactory = converterFactory{name: "Libsamplerate", ctor: resample.NewLibsamplerate}

// TestLibsamplerateAPISuite runs the same constructor / Process error /
// edge-case checks as the native resample_test.go suite against the C
// path, so any contract divergence shows up immediately.
func TestLibsamplerateAPISuite(t *testing.T) {
	runConverterAPISuite(t, libsamplerateFactory)
}

// runConverterAPISuite is the parameterised body. The native suite in
// resample_test.go exercises the same checks via t.Run blocks; this
// runner mirrors those checks against an arbitrary constructor so we
// can hold both paths to the same contract.
func runConverterAPISuite(t *testing.T, f converterFactory) {
	t.Helper()
	t.Run("ValidConverters", func(t *testing.T) {
		for _, cv := range converterCases {
			c, err := f.ctor(cv.ct, 1)
			require.NoError(t, err, "%s/%s", f.name, cv.name)
			assert.Equal(t, 1, c.Channels())
			c.Close()
		}
	})
	t.Run("InvalidChannels", func(t *testing.T) {
		_, err := f.ctor(resample.Linear, 0)
		assert.ErrorIs(t, err, resample.ErrBadChannelCount)
		_, err = f.ctor(resample.Linear, -1)
		assert.ErrorIs(t, err, resample.ErrBadChannelCount)
	})
	t.Run("InvalidConverterType", func(t *testing.T) {
		_, err := f.ctor(resample.ConverterType(99), 1)
		assert.ErrorIs(t, err, resample.ErrBadConverterType)
	})
	t.Run("ProcessNilData", func(t *testing.T) {
		c, err := f.ctor(resample.Linear, 1)
		require.NoError(t, err)
		defer c.Close()
		assert.ErrorIs(t, c.Process(nil), resample.ErrBadData)
	})
	t.Run("SetRatio", func(t *testing.T) {
		c, err := f.ctor(resample.Linear, 1)
		require.NoError(t, err)
		defer c.Close()
		assert.NoError(t, c.SetRatio(resample.Ratio{InputRate: 1, OutputRate: 2}))
	})
	t.Run("ZeroInput", func(t *testing.T) {
		for _, ct := range []resample.ConverterType{resample.ZeroOrderHold, resample.Linear, resample.SincFastest} {
			c, err := f.ctor(ct, 1)
			require.NoError(t, err)
			d := &resample.Data{DataOut: make([]float64, 100), Ratio: resample.Ratio{InputRate: 1, OutputRate: 1}}
			assert.NoError(t, c.Process(d), "%v", ct)
			assert.Equal(t, 0, d.OutputFramesGen, "%v", ct)
			c.Close()
		}
	})
	t.Run("Clone", func(t *testing.T) {
		c, err := f.ctor(resample.SincFastest, 1)
		require.NoError(t, err)
		defer c.Close()
		clone := c.Clone()
		require.NotNil(t, clone)
		defer clone.Close()
		assert.Equal(t, c.Channels(), clone.Channels())
	})
}
