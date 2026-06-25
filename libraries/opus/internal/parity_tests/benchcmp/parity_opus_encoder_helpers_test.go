//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// eqF32BitSlice reports whether two float32 slices match bit-for-bit.
func eqF32BitSlice(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			return false
		}
	}
	return true
}

func firstF32Mismatch(a, b []float32) (int, bool) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			return i, true
		}
	}
	if len(a) != len(b) {
		return n, true
	}
	return 0, false
}

// randRes16 — generate a float sample in the typical int16 range
// exposed by the libopus float API: values in [-32768, 32767].
func randRes16(r *rand.Rand) float32 {
	return float32(r.Intn(65536) - 32768)
}

// TestParity_HpCutoff — sweep sample rates, cutoff values, and
// independent random input chains. Each trial runs >=160 samples
// with carried-over state across a multi-frame chain.
func TestParity_HpCutoff(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	sampleRates := []int32{8000, 12000, 16000, 24000, 48000}
	// Encoder-realistic cutoffs. SILK's variable HP is usually ≤ ~100 Hz.
	// The hp_cutoff assert requires Fc_Q19 = SMULBB(2470, cutoff)/(Fs/1000)
	// to stay < 32768, giving cutoff < ~106 at Fs=8k, < ~636 at Fs=48k.
	// Use values comfortably below the 8 kHz ceiling.
	cutoffs := []int32{40, 50, 60, 80, 100}
	trials := 200

	for trial := 0; trial < trials; trial++ {
		Fs := sampleRates[r.Intn(len(sampleRates))]
		cutoff := cutoffs[r.Intn(len(cutoffs))]
		channels := 1 + r.Intn(2)
		length := 160 + r.Intn(321) // 160..480
		// Multi-frame chain to exercise state evolution.
		numFrames := 2 + r.Intn(3)

		cMem := make([]float32, 4)
		gMem := make([]float32, 4)
		for f := 0; f < numFrames; f++ {
			in_ := make([]float32, length*channels)
			for i := range in_ {
				in_[i] = randRes16(r)
			}
			cOut, cMemOut := cHpCutoff(in_, cutoff, cMem, length, channels, Fs)
			gOut, gMemOut := nativeopus.ExportTestHpCutoff(in_, cutoff, gMem, length, channels, Fs)
			if !eqF32BitSlice(cOut, gOut) {
				idx, _ := firstF32Mismatch(cOut, gOut)
				t.Fatalf("HpCutoff trial=%d frame=%d Fs=%d cutoff=%d ch=%d len=%d mismatch at %d: C=%x Go=%x",
					trial, f, Fs, cutoff, channels, length, idx,
					math.Float32bits(cOut[idx]), math.Float32bits(gOut[idx]))
			}
			if !eqF32BitSlice(cMemOut, gMemOut) {
				t.Fatalf("HpCutoff state mismatch trial=%d frame=%d C=%v Go=%v",
					trial, f, cMemOut, gMemOut)
			}
			cMem = cMemOut
			gMem = gMemOut
		}
	}
}

func TestParity_DcReject(t *testing.T) {
	r := rand.New(rand.NewSource(43))
	sampleRates := []int32{8000, 12000, 16000, 24000, 48000}
	cutoffs := []int32{3, 6, 10}
	trials := 200

	for trial := 0; trial < trials; trial++ {
		Fs := sampleRates[r.Intn(len(sampleRates))]
		cutoff := cutoffs[r.Intn(len(cutoffs))]
		channels := 1 + r.Intn(2)
		length := 160 + r.Intn(321)
		numFrames := 2 + r.Intn(4)

		cMem := make([]float32, 4)
		gMem := make([]float32, 4)
		for f := 0; f < numFrames; f++ {
			in_ := make([]float32, length*channels)
			for i := range in_ {
				in_[i] = randRes16(r)
			}
			cOut, cMemOut := cDcReject(in_, cutoff, cMem, length, channels, Fs)
			gOut, gMemOut := nativeopus.ExportTestDcReject(in_, cutoff, gMem, length, channels, Fs)
			if !eqF32BitSlice(cOut, gOut) {
				idx, _ := firstF32Mismatch(cOut, gOut)
				t.Fatalf("DcReject trial=%d frame=%d Fs=%d cutoff=%d ch=%d len=%d mismatch at %d: C=%x Go=%x",
					trial, f, Fs, cutoff, channels, length, idx,
					math.Float32bits(cOut[idx]), math.Float32bits(gOut[idx]))
			}
			if !eqF32BitSlice(cMemOut, gMemOut) {
				t.Fatalf("DcReject state mismatch trial=%d frame=%d C=%v Go=%v",
					trial, f, cMemOut, gMemOut)
			}
			cMem = cMemOut
			gMem = gMemOut
		}
	}
}

// makeWindow builds a smooth sin-shaped window of the given length
// (mimicking the shape of celt_mode->window but independent of it).
func makeWindow(length int, r *rand.Rand) []float32 {
	w := make([]float32, length)
	for i := range w {
		// Values in [0, 1]. Start with a raised cosine, then jitter
		// lightly so every coefficient is a distinct float32.
		t := float64(i) / float64(length-1)
		base := 0.5 - 0.5*math.Cos(math.Pi*t)
		w[i] = float32(base + 1e-6*(r.Float64()-0.5))
	}
	return w
}

func TestParity_StereoFade(t *testing.T) {
	r := rand.New(rand.NewSource(44))
	sampleRates := []int32{8000, 12000, 16000, 24000, 48000}
	trials := 200

	for trial := 0; trial < trials; trial++ {
		Fs := sampleRates[r.Intn(len(sampleRates))]
		inc := 48000 / int(Fs)
		if inc < 1 {
			inc = 1
		}
		overlap := 120 // typical CELT overlap at 48 kHz
		overlap48 := overlap * inc
		channels := 2
		// Mix of boundary cases: overlap-only, overlap+tail, larger.
		frame_sizes := []int{overlap, overlap + 32, overlap + 128, overlap + 240}
		frame_size := frame_sizes[r.Intn(len(frame_sizes))]
		window := makeWindow(overlap48, r)

		// Boundary cases: g1=0/g2=0, g1=1/g2=1, mid ramps.
		var g1, g2 float32
		switch r.Intn(5) {
		case 0:
			g1, g2 = 0, 0
		case 1:
			g1, g2 = 1, 1
		case 2:
			g1, g2 = 0, 1
		case 3:
			g1, g2 = 1, 0
		default:
			g1 = r.Float32()
			g2 = r.Float32()
		}

		in_ := make([]float32, frame_size*channels)
		for i := range in_ {
			in_[i] = randRes16(r)
		}
		cOut := cStereoFade(in_, g1, g2, overlap48, frame_size, channels, window, Fs)
		gOut := nativeopus.ExportTestStereoFade(in_, g1, g2, overlap48, frame_size, channels, window, Fs)
		if !eqF32BitSlice(cOut, gOut) {
			idx, _ := firstF32Mismatch(cOut, gOut)
			t.Fatalf("StereoFade trial=%d Fs=%d ov48=%d fs=%d g1=%g g2=%g mismatch at %d: C=%x Go=%x",
				trial, Fs, overlap48, frame_size, g1, g2, idx,
				math.Float32bits(cOut[idx]), math.Float32bits(gOut[idx]))
		}
	}
}

func TestParity_GainFade(t *testing.T) {
	r := rand.New(rand.NewSource(45))
	sampleRates := []int32{8000, 12000, 16000, 24000, 48000}
	trials := 200

	for trial := 0; trial < trials; trial++ {
		Fs := sampleRates[r.Intn(len(sampleRates))]
		inc := 48000 / int(Fs)
		if inc < 1 {
			inc = 1
		}
		overlap := 120
		overlap48 := overlap * inc
		channels := 1 + r.Intn(2)
		frame_sizes := []int{overlap, overlap + 32, overlap + 160}
		frame_size := frame_sizes[r.Intn(len(frame_sizes))]
		window := makeWindow(overlap48, r)

		// Mix gain combos including 0, 1, and mid ramps.
		var g1, g2 float32
		switch r.Intn(6) {
		case 0:
			g1, g2 = 0, 1
		case 1:
			g1, g2 = 1, 0
		case 2:
			g1, g2 = 0.5, 0.5
		case 3:
			g1, g2 = 1, 1
		case 4:
			g1, g2 = 0, 0
		default:
			g1 = r.Float32()
			g2 = r.Float32()
		}

		in_ := make([]float32, frame_size*channels)
		for i := range in_ {
			in_[i] = randRes16(r)
		}
		cOut := cGainFade(in_, g1, g2, overlap48, frame_size, channels, window, Fs)
		gOut := nativeopus.ExportTestGainFade(in_, g1, g2, overlap48, frame_size, channels, window, Fs)
		if !eqF32BitSlice(cOut, gOut) {
			idx, _ := firstF32Mismatch(cOut, gOut)
			t.Fatalf("GainFade trial=%d Fs=%d ov48=%d fs=%d ch=%d g1=%g g2=%g mismatch at %d: C=%x Go=%x",
				trial, Fs, overlap48, frame_size, channels, g1, g2, idx,
				math.Float32bits(cOut[idx]), math.Float32bits(gOut[idx]))
		}
	}
}

func TestParity_ComputeStereoWidth(t *testing.T) {
	r := rand.New(rand.NewSource(46))
	sampleRates := []int32{8000, 12000, 16000, 24000, 48000}
	trials := 200

	for trial := 0; trial < trials; trial++ {
		Fs := sampleRates[r.Intn(len(sampleRates))]
		// frame_size must be a multiple of 4 for the compute_stereo_width
		// unrolled loop to cover the whole frame (C does this except at
		// 12 kHz 2.5 ms, which we avoid).
		frameSizes := []int{int(Fs) / 50, int(Fs) / 100, 160, 240, 480}
		// Align to multiples of 4.
		frame_size := frameSizes[r.Intn(len(frameSizes))]
		frame_size = (frame_size / 4) * 4
		if frame_size < 4 {
			frame_size = 4
		}
		pcm := make([]float32, frame_size*2)
		for i := range pcm {
			pcm[i] = randRes16(r)
		}
		// Randomise initial state modestly so the MAX(...,QCONST(8e-4))
		// branch fires about half the time.
		var xx, xy, yy float32
		var sm, mx float32
		if r.Intn(2) == 0 {
			xx = r.Float32() * 1e-2
			xy = (r.Float32()*2 - 1) * 1e-3
			yy = r.Float32() * 1e-2
			sm = r.Float32() * 0.1
			mx = sm + r.Float32()*0.1
		}

		cRet, cXX, cXY, cYY, cSm, cMax := cComputeStereoWidth(pcm, frame_size, Fs, xx, xy, yy, sm, mx)
		gRet, gXX, gXY, gYY, gSm, gMax := nativeopus.ExportTestComputeStereoWidth(pcm, frame_size, Fs, xx, xy, yy, sm, mx)

		if math.Float32bits(cRet) != math.Float32bits(gRet) {
			t.Fatalf("ComputeStereoWidth trial=%d ret mismatch: C=%x Go=%x",
				trial, math.Float32bits(cRet), math.Float32bits(gRet))
		}
		if math.Float32bits(cXX) != math.Float32bits(gXX) ||
			math.Float32bits(cXY) != math.Float32bits(gXY) ||
			math.Float32bits(cYY) != math.Float32bits(gYY) ||
			math.Float32bits(cSm) != math.Float32bits(gSm) ||
			math.Float32bits(cMax) != math.Float32bits(gMax) {
			t.Fatalf("ComputeStereoWidth trial=%d state mismatch:\n"+
				"C=%+v\nGo=%+v",
				trial,
				[]float32{cXX, cXY, cYY, cSm, cMax},
				[]float32{gXX, gXY, gYY, gSm, gMax})
		}
	}
}
