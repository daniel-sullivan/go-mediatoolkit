package vad

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/resample"
)

// adapterFrames pushes sig through a and returns every emitted frame
// (copied) with its start position.
type emittedFrame struct {
	start int64
	data  []float64
}

func adapterFrames(a *engineAdapter, sig []float64, chunkFrames, ch int) []emittedFrame {
	var out []emittedFrame
	step := chunkFrames * ch
	for off := 0; off < len(sig); off += step {
		end := off + step
		if end > len(sig) {
			end = len(sig)
		}
		a.push(sig[off:end], func(frame []float64, start int64) {
			cp := make([]float64, len(frame))
			copy(cp, frame)
			out = append(out, emittedFrame{start: start, data: cp})
		})
	}
	return out
}

// TestAdapterPassthroughNoLoss: mono, no resample — every sample lands
// in a frame or the pending buffer, positions are exact.
func TestAdapterPassthroughNoLoss(t *testing.T) {
	a, err := newEngineAdapter(16000, 1, 16000, 320)
	require.NoError(t, err)

	sig := make([]float64, 1000)
	for i := range sig {
		sig[i] = float64(i)
	}
	frames := adapterFrames(a, sig, 1000, 1)

	require.Len(t, frames, 3)
	assert.Equal(t, 40, a.pendingSamples(), "trailing samples must stay buffered, not vanish")
	for i, f := range frames {
		assert.Equal(t, int64(i*320), f.start)
		for k := 0; k < 320; k++ {
			assert.Equal(t, float64(i*320+k), f.data[k])
		}
	}

	// The buffered tail completes into the next frame seamlessly.
	next := make([]float64, 280)
	for i := range next {
		next[i] = float64(1000 + i)
	}
	frames2 := adapterFrames(a, next, 280, 1)
	require.Len(t, frames2, 1)
	assert.Equal(t, int64(960), frames2[0].start)
	assert.Equal(t, 960.0, frames2[0].data[0])
	assert.Equal(t, 1279.0, frames2[0].data[319])

	// Passthrough position mapping is the identity; latencies are
	// exactly zero resampler + one frame fill.
	assert.Equal(t, int64(12345), a.inputFrame(12345))
	assert.Equal(t, int64(0), a.resamplerDelay)
	assert.Equal(t, mutations.FramesToDuration(320, 16000), a.frameFillLatency())
}

// TestAdapterBufferSizeInvariance: 1-frame, prime, and one-giant-push
// feeding produce byte-identical frames, resampled or not.
func TestAdapterBufferSizeInvariance(t *testing.T) {
	for _, tc := range []struct {
		name               string
		inRate, engineRate int
	}{
		{"passthrough 16k", 16000, 16000},
		{"resampled 44.1k to 16k", 44100, 16000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sig []float64
			sig = appendTone(sig, 440, 0.5, tc.inRate, tc.inRate, 1) // 1 s mono

			run := func(chunkFrames int) []emittedFrame {
				a, err := newEngineAdapter(tc.inRate, 1, tc.engineRate, 320)
				require.NoError(t, err)
				return adapterFrames(a, sig, chunkFrames, 1)
			}

			whole := run(len(sig))
			require.NotEmpty(t, whole)
			assert.Equal(t, whole, run(1), "1-frame pushes must be identical")
			assert.Equal(t, whole, run(997), "prime-sized pushes must be identical")
		})
	}
}

// TestAdapterResampleReference: 44.1 kHz stereo through the adapter
// matches a hand-built reference — manual downmix + one-shot
// resample.Simple to 16 kHz + manual chunking.
func TestAdapterResampleReference(t *testing.T) {
	const (
		inRate     = 44100
		engineRate = 16000
		frameSize  = 320
	)
	// Stereo signal with DIFFERENT channel contents so the downmix
	// path is actually exercised: L = 440 Hz, R = 300 Hz.
	inFrames := inRate * 2 // 2 s
	sig := make([]float64, 0, inFrames*2)
	wl := 2 * math.Pi * 440 / float64(inRate)
	wr := 2 * math.Pi * 300 / float64(inRate)
	for i := 0; i < inFrames; i++ {
		sig = append(sig, 0.4*math.Sin(wl*float64(i)), 0.4*math.Sin(wr*float64(i)))
	}

	// Hand-built reference: downmix, one-shot resample, chunk.
	mono := make([]float64, inFrames)
	mutations.DownmixStereoToMono(sig, mono)
	ref, err := resample.Simple(mono, resample.SincFastest, 1,
		resample.Ratio{InputRate: inRate, OutputRate: engineRate})
	require.NoError(t, err)

	a, err := newEngineAdapter(inRate, 2, engineRate, frameSize)
	require.NoError(t, err)
	frames := adapterFrames(a, sig, 997, 2)
	require.NotEmpty(t, frames)

	// Streaming holds the FIR tail back, so it may trail the flushed
	// one-shot reference by up to the filter history; everything the
	// adapter DID emit must match the reference sample-for-sample.
	emitted := len(frames) * frameSize
	require.LessOrEqual(t, emitted, len(ref))
	expectedEngine := inFrames * engineRate / inRate
	assert.Greater(t, emitted+a.pendingSamples(), expectedEngine-frameSize-256,
		"streaming may only withhold the FIR tail, not lose samples")
	for i, f := range frames {
		for k := 0; k < frameSize; k++ {
			require.InDelta(t, ref[i*frameSize+k], f.data[k], 1e-9,
				"frame %d sample %d diverges from the one-shot reference", i, k)
		}
	}
}

// TestAdapterImpulsePosition: an impulse fed through the resampling
// pipeline must map back to its input position via inputFrame within
// one engine decision frame.
func TestAdapterImpulsePosition(t *testing.T) {
	const (
		inRate     = 44100
		engineRate = 16000
		frameSize  = 320
	)
	a, err := newEngineAdapter(inRate, 1, engineRate, frameSize)
	require.NoError(t, err)
	// Empirical note pinned here: libsamplerate (and thus the resample
	// port) time-aligns its sinc output — the FIR is centred inside
	// the converter — so the probe measures a delay of ~0 rather than
	// half the filter length. The probe stays because it MEASURES
	// rather than assumes; this assertion documents the current truth.
	assert.GreaterOrEqual(t, a.resamplerDelay, int64(0))
	assert.LessOrEqual(t, a.resamplerDelay, int64(inRate/1000),
		"a time-aligned converter must probe (near) zero delay")

	const impulseAt = 22050
	sig := make([]float64, inRate) // 1 s
	sig[impulseAt] = 1

	peakSample, peakVal := int64(-1), 0.0
	a.push(sig, func(frame []float64, start int64) {
		for k := 0; k < len(frame); k++ {
			if v := math.Abs(frame[k]); v > peakVal {
				peakVal = v
				peakSample = start + int64(k)
			}
		}
	})
	require.GreaterOrEqual(t, peakSample, int64(0), "the impulse must come out of the pipeline")

	mapped := a.inputFrame(peakSample)
	oneEngineFrameInInput := int64(frameSize * inRate / engineRate)
	assert.InDelta(t, float64(impulseAt), float64(mapped), float64(oneEngineFrameInInput),
		"position mapping must recover the impulse within one engine frame")

	// Early positions clamp to zero rather than going negative.
	assert.Equal(t, int64(0), a.inputFrame(0))
}

// TestAdapterDownmixMulti: >2 channels use an equal-weight average.
func TestAdapterDownmixMulti(t *testing.T) {
	a, err := newEngineAdapter(16000, 4, 16000, 4)
	require.NoError(t, err)

	sig := []float64{
		1, 2, 3, 4, // frame 0 → 2.5
		0, 0, 0, 0, // frame 1 → 0
		-1, -1, 1, 1, // frame 2 → 0
		8, 0, 0, 0, // frame 3 → 2
	}
	frames := adapterFrames(a, sig, 4, 4)
	require.Len(t, frames, 1)
	assert.Equal(t, []float64{2.5, 0, 0, 2}, frames[0].data)
}

// TestAdapterReset: reset restarts positions and clears buffered
// state so a re-run reproduces the first run exactly.
func TestAdapterReset(t *testing.T) {
	a, err := newEngineAdapter(44100, 1, 16000, 320)
	require.NoError(t, err)

	var sig []float64
	sig = appendTone(sig, 440, 0.5, 44100/2, 44100, 1)

	first := adapterFrames(a, sig, 1024, 1)
	a.reset()
	assert.Equal(t, 0, a.pendingSamples())
	second := adapterFrames(a, sig, 1024, 1)
	assert.Equal(t, first, second)
}

// TestFrameToInt16MirrorsMutations: the allocation-free conversion
// must match mutations.Float64ToInt16's byte path bit-for-bit,
// including clamping.
func TestFrameToInt16MirrorsMutations(t *testing.T) {
	src := []float64{0, 0.5, -0.5, 1, -1, 1.5, -1.5, 0.999999, -0.999999,
		1e-9, -1e-9, 0.25, math.Nextafter(1, 2), math.SmallestNonzeroFloat64}

	want := make([]byte, len(src)*2)
	n := mutations.Float64ToInt16(src, want, binary.LittleEndian)
	require.Equal(t, len(src), n)

	got := make([]int16, len(src))
	frameToInt16(src, got)
	for i := 0; i < len(src); i++ {
		ref := int16(binary.LittleEndian.Uint16(want[i*2:]))
		assert.Equal(t, ref, got[i], "sample %d (%v)", i, src[i])
	}
}

// TestFrameToFloat32 is a plain narrowing.
func TestFrameToFloat32(t *testing.T) {
	src := []float64{0, 0.5, -1, 1, 0.123456789, -3}
	got := make([]float32, len(src))
	frameToFloat32(src, got)
	for i := 0; i < len(src); i++ {
		assert.Equal(t, float32(src[i]), got[i])
	}
}
