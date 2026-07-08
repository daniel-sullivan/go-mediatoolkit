//go:build cgo

package fvad_e2e

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/fvad"
)

// makeStream builds `seconds` seconds of deterministic audio at rate,
// cycling half-second segments through six signal classes: digital
// silence, quiet noise, AM speech-like harmonic bursts, full-range
// white noise, a steady mid-band tone, and full-scale extremes. The mix
// walks the VAD through both hypotheses, the total-power gate, the
// model-update paths and the saturating resampler paths.
func makeStream(rng *rand.Rand, rate, seconds int) []int16 {
	n := rate * seconds
	out := make([]int16, n)
	seg := rate / 2 // half-second segments
	for i := 0; i < n; i++ {
		switch (i / seg) % 6 {
		case 0: // silence
			out[i] = 0
		case 1: // quiet noise
			out[i] = int16(rng.IntN(129) - 64)
		case 2: // AM speech-like burst
			tt := float64(i)
			f := 150.0 + 100*math.Sin(2*math.Pi*0.3*tt/float64(rate))
			env := 0.5 + 0.5*math.Sin(2*math.Pi*4*tt/float64(rate))
			v := env * 11000 * (math.Sin(2*math.Pi*f*tt/float64(rate)) +
				0.6*math.Sin(2*math.Pi*2*f*tt/float64(rate)) +
				0.3*math.Sin(2*math.Pi*5*f*tt/float64(rate)))
			out[i] = int16(v)
		case 3: // full-range white noise
			out[i] = int16(rng.IntN(65536) - 32768)
		case 4: // steady 1 kHz tone
			out[i] = int16(8000 * math.Sin(2*math.Pi*1000*float64(i)/float64(rate)))
		default: // full-scale extremes
			if (i/32)%2 == 0 {
				out[i] = -32768
			} else {
				out[i] = 32767
			}
		}
	}
	return out
}

// TestProcessParityFullMatrix runs the complete rate × frame-duration ×
// mode matrix over ≥ 60 s streams, requiring frame-by-frame identical
// decisions.
func TestProcessParityFullMatrix(t *testing.T) {
	const seconds = 60
	for _, rate := range []int{8000, 16000, 32000, 48000} {
		rate := rate
		stream := makeStream(rand.New(rand.NewPCG(uint64(rate), 42)), rate, seconds)
		for _, ms := range []int{10, 20, 30} {
			for mode := 0; mode <= 3; mode++ {
				ms, mode := ms, mode
				t.Run(fmt.Sprintf("%dHz/%dms/mode%d", rate, ms, mode), func(t *testing.T) {
					n := rate / 1000 * ms

					cv := newCVAD()
					gv, err := fvad.New()
					require.NoError(t, err)
					require.Equal(t, 0, cv.setSampleRate(rate))
					require.NoError(t, gv.SetSampleRate(rate))
					require.Equal(t, 0, cv.setMode(mode))
					require.NoError(t, gv.SetMode(mode))

					for off := 0; off+n <= len(stream); off += n {
						frame := stream[off : off+n]
						cRes := cv.process(frame)
						require.GreaterOrEqual(t, cRes, 0, "C rejected a valid frame at %d", off)
						gRes, err := gv.Process(frame)
						require.NoError(t, err, "Go rejected a valid frame at %d", off)
						require.Equal(t, cRes == 1, gRes, "decision at sample %d (frame %d)", off, off/n)
					}
				})
			}
		}
	}
}

// TestMidStreamResetParity resets both implementations halfway through
// a stream (fvad_reset resets mode AND sample rate to defaults — the
// port mirrors that, so both sides re-apply their settings) and
// requires identical decisions before and after.
func TestMidStreamResetParity(t *testing.T) {
	const rate, ms, mode = 48000, 30, 2
	n := rate / 1000 * ms
	stream := makeStream(rand.New(rand.NewPCG(7, 77)), rate, 10)

	cv := newCVAD()
	gv, err := fvad.New()
	require.NoError(t, err)

	applySettings := func() {
		require.Equal(t, 0, cv.setSampleRate(rate))
		require.NoError(t, gv.SetSampleRate(rate))
		require.Equal(t, 0, cv.setMode(mode))
		require.NoError(t, gv.SetMode(mode))
	}
	applySettings()

	half := (len(stream) / (2 * n)) * n
	run := func(from, to int) {
		for off := from; off+n <= to; off += n {
			frame := stream[off : off+n]
			cRes := cv.process(frame)
			gRes, err := gv.Process(frame)
			require.NoError(t, err)
			require.Equal(t, cRes == 1, gRes, "decision at sample %d", off)
		}
	}

	run(0, half)
	cv.reset()
	gv.Reset()
	applySettings()
	run(half, len(stream))
}

// TestErrorReturnParity pins the API error surface: invalid frame
// lengths per rate (C -1 ⇄ Go ErrInvalidFrameLength), invalid modes,
// and invalid sample rates.
func TestErrorReturnParity(t *testing.T) {
	for _, rate := range []int{8000, 16000, 32000, 48000} {
		cv := newCVAD()
		gv, err := fvad.New()
		require.NoError(t, err)
		require.Equal(t, 0, cv.setSampleRate(rate))
		require.NoError(t, gv.SetSampleRate(rate))

		valid := map[int]bool{}
		for _, ms := range []int{10, 20, 30} {
			valid[rate/1000*ms] = true
		}
		lengths := []int{1, 79, 80, 81, 160, 240, 320, 480, 481, 960, 1440, 1441}
		for _, n := range lengths {
			frame := make([]int16, n)
			cRes := cv.process(frame)
			_, gErr := gv.Process(frame)
			if valid[n] {
				require.GreaterOrEqual(t, cRes, 0, "C rate=%d len=%d", rate, n)
				require.NoError(t, gErr, "Go rate=%d len=%d", rate, n)
			} else {
				require.Equal(t, -1, cRes, "C rate=%d len=%d", rate, n)
				require.ErrorIs(t, gErr, fvad.ErrInvalidFrameLength, "Go rate=%d len=%d", rate, n)
			}
		}
	}

	cv := newCVAD()
	gv, err := fvad.New()
	require.NoError(t, err)
	for _, mode := range []int{-1, 4, 100} {
		require.Equal(t, -1, cv.setMode(mode), "C mode=%d", mode)
		require.ErrorIs(t, gv.SetMode(mode), fvad.ErrInvalidMode, "Go mode=%d", mode)
	}
	for mode := 0; mode <= 3; mode++ {
		require.Equal(t, 0, cv.setMode(mode))
		require.NoError(t, gv.SetMode(mode))
	}
	for _, rate := range []int{0, 7999, 8001, 44100, 96000, -8000} {
		require.Equal(t, -1, cv.setSampleRate(rate), "C rate=%d", rate)
		require.ErrorIs(t, gv.SetSampleRate(rate), fvad.ErrInvalidSampleRate, "Go rate=%d", rate)
	}
	for _, rate := range []int{8000, 16000, 32000, 48000} {
		require.Equal(t, 0, cv.setSampleRate(rate))
		require.NoError(t, gv.SetSampleRate(rate))
	}
}
