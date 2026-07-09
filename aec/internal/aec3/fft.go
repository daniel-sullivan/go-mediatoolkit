// Package aec3 is a 1:1 Go port of the AEC3 foundation types from
// freedesktop's webrtc-audio-processing v2.1 (tracks WebRTC M131),
// fetched (not vendored — see ../../oracle/VERSION) into a per-user
// cache by aec/oracle/fetch.sh and used as the parity oracle for this
// package (see internal/parity_tests/{fft,blocking,bandsplit}).
//
// This file ports:
//   - common_audio/third_party/ooura/fft_size_128/ooura_fft.{h,cc} —
//     Takuya Ooura's 128-point real-valued FFT, scalar (C) path only;
//     the SSE2/NEON/MIPS variants are intentionally not ported (this
//     toolkit's Go build has no equivalent hand-written SIMD intrinsics
//     path for this component and the scalar path is bit-identical to
//     them by construction — they're the same algorithm, just
//     vectorized).
//   - common_audio/third_party/ooura/fft_size_128/ooura_fft_tables_common.h —
//     the rdft_w / rdft_wk3ri_first / rdft_wk3ri_second twiddle tables.
//     These are literal precomputed constants (not derived via runtime
//     sin/cos at init, unlike e.g. this repo's loudness package), so
//     this port needs no libm-transcendental tolerance exception: it
//     is bit-exact against the C oracle by construction.
//   - modules/audio_processing/aec3/aec3_fft.{h,cc} — the Aec3Fft
//     wrapper (windowing, zero-padding, the FftData type from
//     aec3/fft_data.h).
//
// FP discipline (stated once here for the whole package): every
// arithmetic expression is a direct transcription of the corresponding
// C statement, preserving operand order and grouping, under the same
// convention as this repo's other bit-exact ports (opus/flac): the C
// oracle is compiled with `-ffp-contract=off` (no FMA contraction —
// enforced by aec/oracle/fetch.sh), and every a*b+c-shaped expression
// on the Go side goes through the mla/muladd/mulsub helpers
// (fp_default.go / fp_strict.go), because the Go spec permits — and
// the arm64 backend performs — fusing `a + b*c` into a single-rounding
// FMA. Under the aec_strict build tag (which the parity task sets)
// those helpers force separate rounding via //go:noinline primitives
// and match the oracle bit-for-bit; the default build lets the
// compiler fuse for speed and is not a parity target. Pure multiplies,
// adds and subs need no helper: Go rounds each single operation
// exactly like C does.
package aec3

// Constants ported from modules/audio_processing/aec3/aec3_common.h,
// scoped to what this file and this package's other files (block.go,
// bandsplit.go) need.
const (
	// FFTLengthBy2 == kFftLengthBy2: one AEC3 block is 4ms/64 samples
	// at 16kHz.
	FFTLengthBy2 = 64
	// FFTLengthBy2Plus1 == kFftLengthBy2Plus1.
	FFTLengthBy2Plus1 = FFTLengthBy2 + 1
	// FFTLengthBy2Minus1 == kFftLengthBy2Minus1.
	FFTLengthBy2Minus1 = FFTLengthBy2 - 1
	// FFTLength == kFftLength: the 128-point real FFT this package is
	// built around.
	FFTLength = 2 * FFTLengthBy2
	// FFTLengthBy2Log2 == kFftLengthBy2Log2.
	FFTLengthBy2Log2 = 6

	// BlockSize == kBlockSize (block.go); a block is FFTLengthBy2
	// samples, one per 16kHz band.
	BlockSize = FFTLengthBy2
	// BlockSizeLog2 == kBlockSizeLog2.
	BlockSizeLog2 = FFTLengthBy2Log2
	// ExtendedBlockSize == kExtendedBlockSize.
	ExtendedBlockSize = 2 * FFTLengthBy2

	// FrameSize == kFrameSize: a 10ms multiband frame is 160 samples
	// at 16kHz.
	FrameSize = 160
	// SubFrameLength == kSubFrameLength (block.go's FrameBlocker/BlockFramer).
	SubFrameLength = FrameSize / 2

	// NumBlocksPerSecond == kNumBlocksPerSecond.
	NumBlocksPerSecond = 250

	// MaxNumBands == kMaxNumBands: up to 3 bands (48kHz => 16kHz*3).
	MaxNumBands = 3
)

// NumBandsForRate mirrors aec3_common.h's constexpr NumBandsForRate:
// 16kHz => 1 band, 32kHz => 2 bands, 48kHz => 3 bands.
func NumBandsForRate(sampleRateHz int) int {
	return sampleRateHz / 16000
}

// rdftW, rdftWk3riFirst, rdftWk3riSecond are the literal twiddle
// tables from ooura_fft_tables_common.h, shared by the (in upstream)
// C/MIPS/NEON paths and here backing the sole scalar Go path.
var rdftW = [64]float32{
	1.0000000000, 0.0000000000, 0.7071067691, 0.7071067691, 0.9238795638,
	0.3826834559, 0.3826834559, 0.9238795638, 0.9807852507, 0.1950903237,
	0.5555702448, 0.8314695954, 0.8314695954, 0.5555702448, 0.1950903237,
	0.9807852507, 0.9951847196, 0.0980171412, 0.6343933344, 0.7730104327,
	0.8819212914, 0.4713967443, 0.2902846634, 0.9569403529, 0.9569403529,
	0.2902846634, 0.4713967443, 0.8819212914, 0.7730104327, 0.6343933344,
	0.0980171412, 0.9951847196, 0.7071067691, 0.4993977249, 0.4975923598,
	0.4945882559, 0.4903926253, 0.4850156307, 0.4784701765, 0.4707720280,
	0.4619397819, 0.4519946277, 0.4409606457, 0.4288643003, 0.4157347977,
	0.4016037583, 0.3865052164, 0.3704755902, 0.3535533845, 0.3357794881,
	0.3171966672, 0.2978496552, 0.2777851224, 0.2570513785, 0.2356983721,
	0.2137775421, 0.1913417280, 0.1684449315, 0.1451423317, 0.1214900985,
	0.0975451618, 0.0733652338, 0.0490085706, 0.0245338380,
}

var rdftWk3riFirst = [16]float32{
	1.000000000, 0.000000000, 0.382683456, 0.923879564,
	0.831469536, 0.555570245, -0.195090353, 0.980785251,
	0.956940353, 0.290284693, 0.098017156, 0.995184720,
	0.634393334, 0.773010492, -0.471396863, 0.881921172,
}

var rdftWk3riSecond = [16]float32{
	-0.707106769, 0.707106769, -0.923879564, -0.382683456,
	-0.980785251, 0.195090353, -0.555570245, -0.831469536,
	-0.881921172, 0.471396863, -0.773010492, -0.634393334,
	-0.995184720, -0.098017156, -0.290284693, -0.956940353,
}

// OouraFFT is the scalar-only port of OouraFft (ooura_fft.h/cc). C's
// use_sse2_ selection is dropped entirely: this Go build has one path.
type OouraFFT struct{}

// Fft computes the 128-point real FFT in place, packed as in C:
// a[0]=Re[0], a[1]=Re[N/2], then interleaved Re/Im for bins 1..N/2-1.
// C: OouraFft::Fft.
func (OouraFFT) Fft(a []float32) {
	bitrv2128(a)
	cftfsub128(a)
	rftfsub128(a)
	xi := a[0] - a[1]
	a[0] += a[1]
	a[1] = xi
}

// InverseFft computes the inverse of Fft, in place. C: OouraFft::InverseFft.
func (OouraFFT) InverseFft(a []float32) {
	a[1] = 0.5 * (a[0] - a[1])
	a[0] -= a[1]
	rftbsub128(a)
	bitrv2128(a)
	cftbsub128(a)
}

// cft1st128 is the first-stage complex FFT butterfly. C: cft1st_128_C.
func cft1st128(a []float32) {
	const n = 128
	var wk1r, wk1i, wk2r, wk2i, wk3r, wk3i float32
	var x0r, x0i, x1r, x1i, x2r, x2i, x3r, x3i float32

	x0r = a[0] + a[2]
	x0i = a[1] + a[3]
	x1r = a[0] - a[2]
	x1i = a[1] - a[3]
	x2r = a[4] + a[6]
	x2i = a[5] + a[7]
	x3r = a[4] - a[6]
	x3i = a[5] - a[7]
	a[0] = x0r + x2r
	a[1] = x0i + x2i
	a[4] = x0r - x2r
	a[5] = x0i - x2i
	a[2] = x1r - x3i
	a[3] = x1i + x3r
	a[6] = x1r + x3i
	a[7] = x1i - x3r
	wk1r = rdftW[2]
	x0r = a[8] + a[10]
	x0i = a[9] + a[11]
	x1r = a[8] - a[10]
	x1i = a[9] - a[11]
	x2r = a[12] + a[14]
	x2i = a[13] + a[15]
	x3r = a[12] - a[14]
	x3i = a[13] - a[15]
	a[8] = x0r + x2r
	a[9] = x0i + x2i
	a[12] = x2i - x0i
	a[13] = x0r - x2r
	x0r = x1r - x3i
	x0i = x1i + x3r
	a[10] = wk1r * (x0r - x0i)
	a[11] = wk1r * (x0r + x0i)
	x0r = x3i + x1r
	x0i = x3r - x1i
	a[14] = wk1r * (x0i - x0r)
	a[15] = wk1r * (x0i + x0r)
	k1 := 0
	for j := 16; j < n; j += 16 {
		k1 += 2
		k2 := 2 * k1
		wk2r = rdftW[k1+0]
		wk2i = rdftW[k1+1]
		wk1r = rdftW[k2+0]
		wk1i = rdftW[k2+1]
		wk3r = rdftWk3riFirst[k1+0]
		wk3i = rdftWk3riFirst[k1+1]
		x0r = a[j+0] + a[j+2]
		x0i = a[j+1] + a[j+3]
		x1r = a[j+0] - a[j+2]
		x1i = a[j+1] - a[j+3]
		x2r = a[j+4] + a[j+6]
		x2i = a[j+5] + a[j+7]
		x3r = a[j+4] - a[j+6]
		x3i = a[j+5] - a[j+7]
		a[j+0] = x0r + x2r
		a[j+1] = x0i + x2i
		x0r -= x2r
		x0i -= x2i
		a[j+4] = mulsub(wk2r, x0r, wk2i, x0i)
		a[j+5] = muladd(wk2r, x0i, wk2i, x0r)
		x0r = x1r - x3i
		x0i = x1i + x3r
		a[j+2] = mulsub(wk1r, x0r, wk1i, x0i)
		a[j+3] = muladd(wk1r, x0i, wk1i, x0r)
		x0r = x1r + x3i
		x0i = x1i - x3r
		a[j+6] = mulsub(wk3r, x0r, wk3i, x0i)
		a[j+7] = muladd(wk3r, x0i, wk3i, x0r)
		wk1r = rdftW[k2+2]
		wk1i = rdftW[k2+3]
		wk3r = rdftWk3riSecond[k1+0]
		wk3i = rdftWk3riSecond[k1+1]
		x0r = a[j+8] + a[j+10]
		x0i = a[j+9] + a[j+11]
		x1r = a[j+8] - a[j+10]
		x1i = a[j+9] - a[j+11]
		x2r = a[j+12] + a[j+14]
		x2i = a[j+13] + a[j+15]
		x3r = a[j+12] - a[j+14]
		x3i = a[j+13] - a[j+15]
		a[j+8] = x0r + x2r
		a[j+9] = x0i + x2i
		x0r -= x2r
		x0i -= x2i
		a[j+12] = mulsub(-wk2i, x0r, wk2r, x0i)
		a[j+13] = muladd(-wk2i, x0i, wk2r, x0r)
		x0r = x1r - x3i
		x0i = x1i + x3r
		a[j+10] = mulsub(wk1r, x0r, wk1i, x0i)
		a[j+11] = muladd(wk1r, x0i, wk1i, x0r)
		x0r = x1r + x3i
		x0i = x1i - x3r
		a[j+14] = mulsub(wk3r, x0r, wk3i, x0i)
		a[j+15] = muladd(wk3r, x0i, wk3i, x0r)
	}
}

// cftmdl128 is the middle-stage complex FFT butterfly. C: cftmdl_128_C.
func cftmdl128(a []float32) {
	const l = 8
	const n = 128
	const m = 32
	var wk1r, wk1i, wk2r, wk2i, wk3r, wk3i float32
	var x0r, x0i, x1r, x1i, x2r, x2i, x3r, x3i float32

	for j0 := 0; j0 < l; j0 += 2 {
		j1 := j0 + 8
		j2 := j0 + 16
		j3 := j0 + 24
		x0r = a[j0+0] + a[j1+0]
		x0i = a[j0+1] + a[j1+1]
		x1r = a[j0+0] - a[j1+0]
		x1i = a[j0+1] - a[j1+1]
		x2r = a[j2+0] + a[j3+0]
		x2i = a[j2+1] + a[j3+1]
		x3r = a[j2+0] - a[j3+0]
		x3i = a[j2+1] - a[j3+1]
		a[j0+0] = x0r + x2r
		a[j0+1] = x0i + x2i
		a[j2+0] = x0r - x2r
		a[j2+1] = x0i - x2i
		a[j1+0] = x1r - x3i
		a[j1+1] = x1i + x3r
		a[j3+0] = x1r + x3i
		a[j3+1] = x1i - x3r
	}
	wk1r = rdftW[2]
	for j0 := m; j0 < l+m; j0 += 2 {
		j1 := j0 + 8
		j2 := j0 + 16
		j3 := j0 + 24
		x0r = a[j0+0] + a[j1+0]
		x0i = a[j0+1] + a[j1+1]
		x1r = a[j0+0] - a[j1+0]
		x1i = a[j0+1] - a[j1+1]
		x2r = a[j2+0] + a[j3+0]
		x2i = a[j2+1] + a[j3+1]
		x3r = a[j2+0] - a[j3+0]
		x3i = a[j2+1] - a[j3+1]
		a[j0+0] = x0r + x2r
		a[j0+1] = x0i + x2i
		a[j2+0] = x2i - x0i
		a[j2+1] = x0r - x2r
		x0r = x1r - x3i
		x0i = x1i + x3r
		a[j1+0] = wk1r * (x0r - x0i)
		a[j1+1] = wk1r * (x0r + x0i)
		x0r = x3i + x1r
		x0i = x3r - x1i
		a[j3+0] = wk1r * (x0i - x0r)
		a[j3+1] = wk1r * (x0i + x0r)
	}
	k1 := 0
	m2 := 2 * m
	for k := m2; k < n; k += m2 {
		k1 += 2
		k2 := 2 * k1
		wk2r = rdftW[k1+0]
		wk2i = rdftW[k1+1]
		wk1r = rdftW[k2+0]
		wk1i = rdftW[k2+1]
		wk3r = rdftWk3riFirst[k1+0]
		wk3i = rdftWk3riFirst[k1+1]
		for j0 := k; j0 < l+k; j0 += 2 {
			j1 := j0 + 8
			j2 := j0 + 16
			j3 := j0 + 24
			x0r = a[j0+0] + a[j1+0]
			x0i = a[j0+1] + a[j1+1]
			x1r = a[j0+0] - a[j1+0]
			x1i = a[j0+1] - a[j1+1]
			x2r = a[j2+0] + a[j3+0]
			x2i = a[j2+1] + a[j3+1]
			x3r = a[j2+0] - a[j3+0]
			x3i = a[j2+1] - a[j3+1]
			a[j0+0] = x0r + x2r
			a[j0+1] = x0i + x2i
			x0r -= x2r
			x0i -= x2i
			a[j2+0] = mulsub(wk2r, x0r, wk2i, x0i)
			a[j2+1] = muladd(wk2r, x0i, wk2i, x0r)
			x0r = x1r - x3i
			x0i = x1i + x3r
			a[j1+0] = mulsub(wk1r, x0r, wk1i, x0i)
			a[j1+1] = muladd(wk1r, x0i, wk1i, x0r)
			x0r = x1r + x3i
			x0i = x1i - x3r
			a[j3+0] = mulsub(wk3r, x0r, wk3i, x0i)
			a[j3+1] = muladd(wk3r, x0i, wk3i, x0r)
		}
		wk1r = rdftW[k2+2]
		wk1i = rdftW[k2+3]
		wk3r = rdftWk3riSecond[k1+0]
		wk3i = rdftWk3riSecond[k1+1]
		for j0 := k + m; j0 < l+(k+m); j0 += 2 {
			j1 := j0 + 8
			j2 := j0 + 16
			j3 := j0 + 24
			x0r = a[j0+0] + a[j1+0]
			x0i = a[j0+1] + a[j1+1]
			x1r = a[j0+0] - a[j1+0]
			x1i = a[j0+1] - a[j1+1]
			x2r = a[j2+0] + a[j3+0]
			x2i = a[j2+1] + a[j3+1]
			x3r = a[j2+0] - a[j3+0]
			x3i = a[j2+1] - a[j3+1]
			a[j0+0] = x0r + x2r
			a[j0+1] = x0i + x2i
			x0r -= x2r
			x0i -= x2i
			a[j2+0] = mulsub(-wk2i, x0r, wk2r, x0i)
			a[j2+1] = muladd(-wk2i, x0i, wk2r, x0r)
			x0r = x1r - x3i
			x0i = x1i + x3r
			a[j1+0] = mulsub(wk1r, x0r, wk1i, x0i)
			a[j1+1] = muladd(wk1r, x0i, wk1i, x0r)
			x0r = x1r + x3i
			x0i = x1i - x3r
			a[j3+0] = mulsub(wk3r, x0r, wk3i, x0i)
			a[j3+1] = muladd(wk3r, x0i, wk3i, x0r)
		}
	}
}

// rftfsub128 is the forward real-FFT postprocessing step. C: rftfsub_128_C.
func rftfsub128(a []float32) {
	c := rdftW[32:]
	var wkr, wki, xr, xi, yr, yi float32

	for j1, j2 := 1, 2; j2 < 64; j1, j2 = j1+1, j2+2 {
		k2 := 128 - j2
		k1 := 32 - j1
		wkr = 0.5 - c[k1]
		wki = c[j1]
		xr = a[j2+0] - a[k2+0]
		xi = a[j2+1] + a[k2+1]
		yr = mulsub(wkr, xr, wki, xi)
		yi = muladd(wkr, xi, wki, xr)
		a[j2+0] -= yr
		a[j2+1] -= yi
		a[k2+0] += yr
		a[k2+1] -= yi
	}
}

// rftbsub128 is the inverse real-FFT preprocessing step. C: rftbsub_128_C.
func rftbsub128(a []float32) {
	c := rdftW[32:]
	var wkr, wki, xr, xi, yr, yi float32

	a[1] = -a[1]
	for j1, j2 := 1, 2; j2 < 64; j1, j2 = j1+1, j2+2 {
		k2 := 128 - j2
		k1 := 32 - j1
		wkr = 0.5 - c[k1]
		wki = c[j1]
		xr = a[j2+0] - a[k2+0]
		xi = a[j2+1] + a[k2+1]
		yr = muladd(wkr, xr, wki, xi)
		yi = mulsub(wkr, xi, wki, xr)
		a[j2+0] = a[j2+0] - yr
		a[j2+1] = yi - a[j2+1]
		a[k2+0] = yr + a[k2+0]
		a[k2+1] = yi - a[k2+1]
	}
	a[65] = -a[65]
}

// cftfsub128 is the forward complex FFT (radix-4, two stages plus the
// final combine). C: OouraFft::cftfsub_128.
func cftfsub128(a []float32) {
	cft1st128(a)
	cftmdl128(a)
	const l = 32
	var x0r, x0i, x1r, x1i, x2r, x2i, x3r, x3i float32
	for j := 0; j < l; j += 2 {
		j1 := j + l
		j2 := j1 + l
		j3 := j2 + l
		x0r = a[j] + a[j1]
		x0i = a[j+1] + a[j1+1]
		x1r = a[j] - a[j1]
		x1i = a[j+1] - a[j1+1]
		x2r = a[j2] + a[j3]
		x2i = a[j2+1] + a[j3+1]
		x3r = a[j2] - a[j3]
		x3i = a[j2+1] - a[j3+1]
		a[j] = x0r + x2r
		a[j+1] = x0i + x2i
		a[j2] = x0r - x2r
		a[j2+1] = x0i - x2i
		a[j1] = x1r - x3i
		a[j1+1] = x1i + x3r
		a[j3] = x1r + x3i
		a[j3+1] = x1i - x3r
	}
}

// cftbsub128 is the inverse complex FFT. C: OouraFft::cftbsub_128.
func cftbsub128(a []float32) {
	cft1st128(a)
	cftmdl128(a)
	const l = 32
	var x0r, x0i, x1r, x1i, x2r, x2i, x3r, x3i float32
	for j := 0; j < l; j += 2 {
		j1 := j + l
		j2 := j1 + l
		j3 := j2 + l
		x0r = a[j] + a[j1]
		x0i = -a[j+1] - a[j1+1]
		x1r = a[j] - a[j1]
		x1i = -a[j+1] + a[j1+1]
		x2r = a[j2] + a[j3]
		x2i = a[j2+1] + a[j3+1]
		x3r = a[j2] - a[j3]
		x3i = a[j2+1] - a[j3+1]
		a[j] = x0r + x2r
		a[j+1] = x0i - x2i
		a[j2] = x0r - x2r
		a[j2+1] = x0i + x2i
		a[j1] = x1r - x3i
		a[j1+1] = x1i - x3r
		a[j3] = x1r + x3i
		a[j3+1] = x1i + x3r
	}
}

// bitrv2128 is the length-128 bit-reversal permutation. C: OouraFft::bitrv2_128.
func bitrv2128(a []float32) {
	ip := [4]int{0, 64, 32, 96}
	for k := 0; k < 4; k++ {
		for j := 0; j < k; j++ {
			j1 := 2*j + ip[k]
			k1 := 2*k + ip[j]
			a[j1+0], a[k1+0] = a[k1+0], a[j1+0]
			a[j1+1], a[k1+1] = a[k1+1], a[j1+1]
			j1 += 8
			k1 += 16
			a[j1+0], a[k1+0] = a[k1+0], a[j1+0]
			a[j1+1], a[k1+1] = a[k1+1], a[j1+1]
			j1 += 8
			k1 -= 8
			a[j1+0], a[k1+0] = a[k1+0], a[j1+0]
			a[j1+1], a[k1+1] = a[k1+1], a[j1+1]
			j1 += 8
			k1 += 16
			a[j1+0], a[k1+0] = a[k1+0], a[j1+0]
			a[j1+1], a[k1+1] = a[k1+1], a[j1+1]
		}
		j1 := 2*k + 8 + ip[k]
		k1 := j1 + 8
		a[j1+0], a[k1+0] = a[k1+0], a[j1+0]
		a[j1+1], a[k1+1] = a[k1+1], a[j1+1]
	}
}

// FFTData holds imaginary data produced from 128-point real-valued
// FFTs. Port of aec3/fft_data.h's FftData. AVX2/SSE2 Spectrum variants
// are not ported — Spectrum below is always the scalar default branch,
// which is what this Go build uses unconditionally.
type FFTData struct {
	Re [FFTLengthBy2Plus1]float32
	Im [FFTLengthBy2Plus1]float32
}

// Assign copies src into d. C: FftData::Assign.
func (d *FFTData) Assign(src *FFTData) {
	d.Re = src.Re
	d.Im = src.Im
	d.Im[0] = 0
	d.Im[FFTLengthBy2] = 0
}

// Clear zeroes both Re and Im. C: FftData::Clear.
func (d *FFTData) Clear() {
	d.Re = [FFTLengthBy2Plus1]float32{}
	d.Im = [FFTLengthBy2Plus1]float32{}
}

// Spectrum computes the power spectrum of d into powerSpectrum
// (length FFTLengthBy2Plus1). C: FftData::Spectrum (default/scalar case).
func (d *FFTData) Spectrum(powerSpectrum []float32) {
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		powerSpectrum[k] = muladd(d.Re[k], d.Re[k], d.Im[k], d.Im[k])
	}
}

// CopyFromPackedArray unpacks Ooura's packed FFT output (v, length
// FFTLength) into d. C: FftData::CopyFromPackedArray.
func (d *FFTData) CopyFromPackedArray(v *[FFTLength]float32) {
	d.Re[0] = v[0]
	d.Re[FFTLengthBy2] = v[1]
	d.Im[0] = 0
	d.Im[FFTLengthBy2] = 0
	j := 2
	for k := 1; k < FFTLengthBy2; k++ {
		d.Re[k] = v[j]
		d.Im[k] = v[j+1]
		j += 2
	}
}

// CopyToPackedArray packs d into v (length FFTLength) in Ooura's
// packed layout, ready for InverseFft. C: FftData::CopyToPackedArray.
func (d *FFTData) CopyToPackedArray(v *[FFTLength]float32) {
	v[0] = d.Re[0]
	v[1] = d.Re[FFTLengthBy2]
	j := 2
	for k := 1; k < FFTLengthBy2; k++ {
		v[j] = d.Re[k]
		v[j+1] = d.Im[k]
		j += 2
	}
}

// Window selects the time-domain window applied before a padded FFT.
// C: Aec3Fft::Window.
type Window int

const (
	WindowRectangular Window = iota
	WindowHanning
	WindowSqrtHanning
)

// kHanning64 is a plain (non-square-rooted) Hanning window of length
// FFTLengthBy2 (64), used by ZeroPaddedFft. C: aec3_fft.cc's kHanning64.
var kHanning64 = [FFTLengthBy2]float32{
	0, 0.00248461, 0.00991376, 0.0222136, 0.03926189,
	0.06088921, 0.08688061, 0.11697778, 0.15088159, 0.1882551,
	0.22872687, 0.27189467, 0.31732949, 0.36457977, 0.41317591,
	0.46263495, 0.51246535, 0.56217185, 0.61126047, 0.65924333,
	0.70564355, 0.75, 0.79187184, 0.83084292, 0.86652594,
	0.89856625, 0.92664544, 0.95048443, 0.96984631, 0.98453864,
	0.99441541, 0.99937846, 0.99937846, 0.99441541, 0.98453864,
	0.96984631, 0.95048443, 0.92664544, 0.89856625, 0.86652594,
	0.83084292, 0.79187184, 0.75, 0.70564355, 0.65924333,
	0.61126047, 0.56217185, 0.51246535, 0.46263495, 0.41317591,
	0.36457977, 0.31732949, 0.27189467, 0.22872687, 0.1882551,
	0.15088159, 0.11697778, 0.08688061, 0.06088921, 0.03926189,
	0.0222136, 0.00991376, 0.00248461, 0,
}

// kSqrtHanning128 is sqrt(hanning(128)) (Matlab: sqrt(hanning(128))),
// used by PaddedFft's kSqrtHanning branch. C: aec3_fft.cc's kSqrtHanning128.
var kSqrtHanning128 = [FFTLength]float32{
	0.00000000000000, 0.02454122852291, 0.04906767432742, 0.07356456359967,
	0.09801714032956, 0.12241067519922, 0.14673047445536, 0.17096188876030,
	0.19509032201613, 0.21910124015687, 0.24298017990326, 0.26671275747490,
	0.29028467725446, 0.31368174039889, 0.33688985339222, 0.35989503653499,
	0.38268343236509, 0.40524131400499, 0.42755509343028, 0.44961132965461,
	0.47139673682600, 0.49289819222978, 0.51410274419322, 0.53499761988710,
	0.55557023301960, 0.57580819141785, 0.59569930449243, 0.61523159058063,
	0.63439328416365, 0.65317284295378, 0.67155895484702, 0.68954054473707,
	0.70710678118655, 0.72424708295147, 0.74095112535496, 0.75720884650648,
	0.77301045336274, 0.78834642762661, 0.80320753148064, 0.81758481315158,
	0.83146961230255, 0.84485356524971, 0.85772861000027, 0.87008699110871,
	0.88192126434835, 0.89322430119552, 0.90398929312344, 0.91420975570353,
	0.92387953251129, 0.93299279883474, 0.94154406518302, 0.94952818059304,
	0.95694033573221, 0.96377606579544, 0.97003125319454, 0.97570213003853,
	0.98078528040323, 0.98527764238894, 0.98917650996478, 0.99247953459871,
	0.99518472667220, 0.99729045667869, 0.99879545620517, 0.99969881869620,
	1.00000000000000, 0.99969881869620, 0.99879545620517, 0.99729045667869,
	0.99518472667220, 0.99247953459871, 0.98917650996478, 0.98527764238894,
	0.98078528040323, 0.97570213003853, 0.97003125319454, 0.96377606579544,
	0.95694033573221, 0.94952818059304, 0.94154406518302, 0.93299279883474,
	0.92387953251129, 0.91420975570353, 0.90398929312344, 0.89322430119552,
	0.88192126434835, 0.87008699110871, 0.85772861000027, 0.84485356524971,
	0.83146961230255, 0.81758481315158, 0.80320753148064, 0.78834642762661,
	0.77301045336274, 0.75720884650648, 0.74095112535496, 0.72424708295147,
	0.70710678118655, 0.68954054473707, 0.67155895484702, 0.65317284295378,
	0.63439328416365, 0.61523159058063, 0.59569930449243, 0.57580819141785,
	0.55557023301960, 0.53499761988710, 0.51410274419322, 0.49289819222978,
	0.47139673682600, 0.44961132965461, 0.42755509343028, 0.40524131400499,
	0.38268343236509, 0.35989503653499, 0.33688985339222, 0.31368174039889,
	0.29028467725446, 0.26671275747490, 0.24298017990326, 0.21910124015687,
	0.19509032201613, 0.17096188876030, 0.14673047445536, 0.12241067519922,
	0.09801714032956, 0.07356456359967, 0.04906767432742, 0.02454122852291,
}

// Aec3FFT provides 128-point real-valued FFT functionality with the
// FFTData type: windowing, zero-padding, and the plain forward/inverse
// transform. Port of aec3/aec3_fft.{h,cc}'s Aec3Fft.
type Aec3FFT struct {
	ooura OouraFFT
}

// NewAec3FFT constructs an Aec3FFT. C: Aec3Fft::Aec3Fft (the SSE2
// detection this ctor did — IsSse2Available()/GetCPUInfo — has no
// effect here since OouraFFT is scalar-only).
func NewAec3FFT() *Aec3FFT {
	return &Aec3FFT{}
}

// Fft computes the FFT of x (destructively) into X. C: Aec3Fft::Fft.
func (f *Aec3FFT) Fft(x *[FFTLength]float32, X *FFTData) {
	f.ooura.Fft(x[:])
	X.CopyFromPackedArray(x)
}

// Ifft computes the inverse FFT of X into x. C: Aec3Fft::Ifft.
func (f *Aec3FFT) Ifft(X *FFTData, x *[FFTLength]float32) {
	X.CopyToPackedArray(x)
	f.ooura.InverseFft(x[:])
}

// ZeroPaddedFft windows x (length FFTLengthBy2) with the requested
// window, prepends FFTLengthBy2 zeros, and computes the FFT into X.
// window must be WindowRectangular or WindowHanning (WindowSqrtHanning
// is invalid here, matching the C RTC_DCHECK_NOTREACHED). C:
// Aec3Fft::ZeroPaddedFft.
func (f *Aec3FFT) ZeroPaddedFft(x []float32, window Window, X *FFTData) {
	var fft [FFTLength]float32
	for i := 0; i < FFTLengthBy2; i++ {
		fft[i] = 0
	}
	switch window {
	case WindowRectangular:
		copy(fft[FFTLengthBy2:], x)
	case WindowHanning:
		for i := 0; i < FFTLengthBy2; i++ {
			fft[FFTLengthBy2+i] = x[i] * kHanning64[i]
		}
	default:
		panic("aec3: ZeroPaddedFft: invalid window")
	}
	f.Fft(&fft, X)
}

// PaddedFft concatenates xOld and x (each length FFTLengthBy2) — with
// window applied for WindowSqrtHanning — and computes the FFT into X.
// C: Aec3Fft::PaddedFft (windowed overload).
func (f *Aec3FFT) PaddedFft(x, xOld []float32, window Window, X *FFTData) {
	var fft [FFTLength]float32
	switch window {
	case WindowRectangular:
		copy(fft[:], xOld)
		copy(fft[len(xOld):], x)
	case WindowSqrtHanning:
		for i, v := range xOld {
			fft[i] = v * kSqrtHanning128[i]
		}
		for i, v := range x {
			fft[len(xOld)+i] = v * kSqrtHanning128[len(xOld)+i]
		}
	default:
		panic("aec3: PaddedFft: invalid window")
	}
	f.Fft(&fft, X)
}

// PaddedFftRectangular is the unwindowed convenience overload. C:
// Aec3Fft::PaddedFft (the non-windowed overload, which calls the
// windowed one with Window::kRectangular).
func (f *Aec3FFT) PaddedFftRectangular(x, xOld []float32, X *FFTData) {
	f.PaddedFft(x, xOld, WindowRectangular, X)
}
