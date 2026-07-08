// Package r128 holds the pure-Go, cgo-free DSP core of the EBU R128 /
// ITU-R BS.1770-4 loudness meter: a 1:1 bit-exact port of the numerics
// in the vendored libebur128 v1.2.6 (loudness/libebur128/ebur128.c).
//
// # Why this is a separate internal package
//
// Each parity slice under loudness/internal/parity_tests/ compiles its
// OWN copy of the vendored ebur128.c (a self-contained cgo translation
// unit per slice) as its oracle, exposing ebur128_init, ebur128_destroy,
// … as ordinary non-static C symbols. If a slice imported a package that
// ALSO compiled ebur128.c, the linker would see two independent
// compilations of those symbols in one test binary and collide (see the
// smoke slice's cgo.go for the original write-up of this constraint).
//
// Keeping the portable DSP in this cgo-free internal package — it
// contains no `import "C"` — lets every parity slice import it alongside
// its own C oracle copy without any symbol collision, and lets the root
// loudness package delegate to it with no C of its own: loudness.Meter
// simply wraps an *r128.State (and loudness.Limiter an *r128.TruePeaker).
// Importing the root loudness package therefore never builds any C,
// cgo-enabled or not; the vendored C is compiled only by the opt-in
// parity slices. The same pattern is used by libraries/flac, whose
// pure-Go port lives in libraries/flac/internal/nativeflac and is
// imported by the flac parity slices.
//
// This is a deliberate deviation from the original plan, which sketched
// the K-weighting filter living directly in package loudness. Keeping
// the DSP core here is what makes the bit-exact Go-vs-C parity slices
// link at all.
package r128

import "math"

// filterStateSize mirrors libebur128's FILTER_STATE_SIZE: the second-
// order-sections cascade is convolved into one 4th-order biquad, whose
// direct-form-II-transposed-style state needs five taps v[0..4].
const filterStateSize = 5

// dblMin is C's DBL_MIN — the smallest positive *normal* IEEE-754
// double, 2^-1022 ≈ 2.2250738585072014e-308. libebur128's manual
// flush-to-zero denormal guard (FLUSH_MANUALLY) compares |state| against
// this bound, so the port must use exactly the same threshold. Note this
// is NOT math.SmallestNonzeroFloat64 (2^-1074, the smallest *subnormal*)
// — DBL_MIN is the smallest normal, and every subnormal is strictly
// below it, which is precisely what the guard flushes away.
const dblMin = 0x1p-1022

// KFilter is the BS.1770 K-weighting pre-filter: a shelving-boost stage
// (the "K" head-diffraction shelf, f0 ≈ 1681.97 Hz, +4 dB) cascaded with
// an RLB high-pass (f0 ≈ 38.14 Hz), both derived per sample rate by the
// bilinear transform and convolved into a single 4th-order IIR filter.
// It is a direct port of libebur128's ebur128_init_filter (coefficient
// derivation) and the double variant of the EBUR128_FILTER macro (the
// recurrence + manual denormal flush).
//
// One KFilter carries independent state for every channel and filters
// interleaved sample buffers in place-parallel (src -> dst, same
// layout). It owns *only* the filtering: sample/true peak tracking,
// channel weighting, the audio_data ring buffer, and the gating/energy
// accumulation that libebur128's EBUR128_FILTER and ebur128_add_frames
// also perform are deliberately left to the meter that composes this
// filter, matching the plan's seam ("leaving ring/peak logic to the
// meter agent").
//
// Channel handling: FilterInto filters every channel it is given. It
// does NOT skip EBUR128_UNUSED channels the way libebur128's filter loop
// does. That skip only affects the filtered values written for unused
// channels, and the meter's gating sum ignores unused channels entirely
// regardless of what was filtered — so the integrated reading is
// identical either way, and keeping the channel map out of the filter
// keeps this seam free of meter concerns. (The parity slice exercises
// only channel counts whose default map has no unused channel, so C's
// filter and this one filter exactly the same set.)
//
// KFilter is not safe for concurrent use.
type KFilter struct {
	sampleRate int
	channels   int

	// B and A are the convolved 4th-order numerator and denominator
	// coefficients, indexed 0..4 exactly as libebur128's st->d->b[] and
	// st->d->a[]. A[0] is the (unit) normalisation term and is not used
	// in the recurrence; it is retained so the whole coefficient vector
	// can be compared bit-for-bit against the C oracle. Treat these as
	// read-only after construction.
	B [filterStateSize]float64
	A [filterStateSize]float64

	// v holds the per-channel filter state, v[channel][0..4], matching
	// libebur128's filter_state v[ch][5]. Zeroed at construction.
	v [][filterStateSize]float64
}

// SampleRate reports the sample rate the coefficients were derived for.
func (f *KFilter) SampleRate() int { return f.sampleRate }

// Channels reports the interleaved channel count FilterInto expects.
func (f *KFilter) Channels() int { return f.channels }

// NewKFilter derives the K-weighting coefficients for sampleRate and
// allocates zeroed per-channel state for channels channels.
//
// It takes no error return by design: it is an unexported-surface
// internal primitive, and the only callers (the root loudness Meter and
// the parity slices) validate sampleRate and channels before reaching
// here. It assumes sampleRate > 0 and channels >= 1; out-of-range values
// produce NaN/Inf coefficients or a zero-length state slice rather than
// an error, exactly as passing garbage to the C ebur128_init_filter
// would. The meter is responsible for rejecting bad inputs up front (see
// loudness.NewMeter and the ErrBadSampleRate / ErrBadChannels
// sentinels).
func NewKFilter(sampleRate, channels int) *KFilter {
	f := &KFilter{
		sampleRate: sampleRate,
		channels:   channels,
		v:          make([][filterStateSize]float64, channels),
	}
	f.B, f.A = deriveCoefficients(sampleRate)
	return f
}

// deriveCoefficients is a 1:1 port of ebur128_init_filter's coefficient
// derivation. It builds the two biquad stages via the bilinear transform
// and convolves them into one 4th-order filter.
//
// FMA policy: libebur128's oracle is compiled with -ffp-contract=off, so
// every multiply and every add rounds separately. Go's compiler may
// contract a*b+c into a single fused-multiply-add on arm64 (and other
// targets), which rounds only once and would diverge from the oracle by
// up to a ULP — and a 1-ULP coefficient error in a feedback IIR filter
// compounds without bound. Per the Go spec an explicit float64()
// conversion forces the intermediate to be rounded to float64, which
// blocks the fusion. So every product that feeds a +/- below is wrapped
// in float64(...). Bare products that feed only a * or / (no add) cannot
// be fused into an FMA and are left unwrapped, matching the C exactly.
func deriveCoefficients(sampleRate int) (b, a [filterStateSize]float64) {
	sr := float64(sampleRate)

	// ---- Stage 1: the +4 dB high-frequency shelving filter. ----
	f0 := 1681.974450955533
	g := 3.999843853973347
	q := 0.7071752369554196

	k := math.Tan(math.Pi * f0 / sr)
	vh := math.Pow(10.0, g/20.0)
	vb := math.Pow(vh, 0.4996667741545416)

	var pb, pa, rb, ra [3]float64
	pa[0] = 1.0
	rb = [3]float64{1.0, -2.0, 1.0}
	ra[0] = 1.0

	a0 := 1.0 + k/q + float64(k*k)
	pb[0] = (vh + vb*k/q + float64(k*k)) / a0
	pb[1] = 2.0 * (float64(k*k) - vh) / a0
	pb[2] = (vh - vb*k/q + float64(k*k)) / a0
	pa[1] = 2.0 * (float64(k*k) - 1.0) / a0
	pa[2] = (1.0 - k/q + float64(k*k)) / a0

	// ---- Stage 2: the RLB high-pass filter. ----
	f0 = 38.13547087602444
	q = 0.5003270373238773
	k = math.Tan(math.Pi * f0 / sr)

	// The denominator normaliser 1 + K/Q + K^2 is computed inline twice
	// in the C; hoisting it into one variable is bit-identical (a
	// deterministic double expression) and avoids repeating the
	// conversion barrier.
	d := 1.0 + k/q + float64(k*k)
	ra[1] = 2.0 * (float64(k*k) - 1.0) / d
	ra[2] = (1.0 - k/q + float64(k*k)) / d

	// ---- Convolve the two stages into one 4th-order filter. ----
	// Products that feed an add are barriered; the pure b[0]/b[4] and
	// a[0]/a[4] terms are single products and need no barrier.
	b[0] = pb[0] * rb[0]
	b[1] = float64(pb[0]*rb[1]) + float64(pb[1]*rb[0])
	b[2] = float64(pb[0]*rb[2]) + float64(pb[1]*rb[1]) + float64(pb[2]*rb[0])
	b[3] = float64(pb[1]*rb[2]) + float64(pb[2]*rb[1])
	b[4] = pb[2] * rb[2]

	a[0] = pa[0] * ra[0]
	a[1] = float64(pa[0]*ra[1]) + float64(pa[1]*ra[0])
	a[2] = float64(pa[0]*ra[2]) + float64(pa[1]*ra[1]) + float64(pa[2]*ra[0])
	a[3] = float64(pa[1]*ra[2]) + float64(pa[2]*ra[1])
	a[4] = pa[2] * ra[2]

	return b, a
}

// FilterInto filters src into dst, both interleaved with f.Channels()
// channels and the same length (a multiple of the channel count). It is
// a 1:1 port of the double variant of libebur128's EBUR128_FILTER macro:
// the transposed-direct-form recurrence per channel, followed by the
// manual flush-to-zero denormal guard once per channel.
//
// Filter state carries across calls, so a long signal chunked into any
// sequence of FilterInto calls produces bit-identical output to filtering
// it in one call — which is exactly what the parity slice checks against
// the C filter fed the same chunks.
//
// The double variant's scaling_factor is EBUR128_MAX(1.0, 1.0) == 1.0,
// so libebur128's "(double)((double)src / scaling_factor)" is an exact
// IEEE identity on the input; the division is elided here.
//
// FMA policy: as in deriveCoefficients, every product feeding a +/- is
// wrapped in float64() so the arm64 compiler cannot fuse the biquad
// multiply-accumulates into FMAs and diverge from the -ffp-contract=off C
// oracle. This is the hot loop, so the barriers matter most here.
//
// dst and src may not alias unless dst == src (in-place is fine).
func (f *KFilter) FilterInto(dst, src []float64) {
	ch := f.channels
	if ch == 0 {
		return
	}
	frames := len(src) / ch

	a1, a2, a3, a4 := f.A[1], f.A[2], f.A[3], f.A[4]
	b0, b1, b2, b3, b4 := f.B[0], f.B[1], f.B[2], f.B[3], f.B[4]

	for c := 0; c < ch; c++ {
		st := &f.v[c]
		v0, v1, v2, v3, v4 := st[0], st[1], st[2], st[3], st[4]

		for i := 0; i < frames; i++ {
			idx := i*ch + c
			// v[0] = x - a1 v1 - a2 v2 - a3 v3 - a4 v4  (a[0] == 1)
			v0 = src[idx] - float64(a1*v1) - float64(a2*v2) - float64(a3*v3) - float64(a4*v4)
			// y = b0 v0 + b1 v1 + b2 v2 + b3 v3 + b4 v4
			dst[idx] = float64(b0*v0) + float64(b1*v1) + float64(b2*v2) + float64(b3*v3) + float64(b4*v4)
			// Shift the delay line: v4<-v3<-v2<-v1<-v0. v0 keeps its
			// value (v1 == v0 afterwards), matching the C.
			v4, v3, v2, v1 = v3, v2, v1, v0
		}

		// FLUSH_MANUALLY: the non-SSE2 branch that is live on arm64 (see
		// the parity slices' cgo.go for why the manual FTZ path is the one
		// that compiles). It flushes subnormal state taps v1..v4 to zero — deliberately NOT v0, which the C also
		// leaves untouched (it is overwritten on the next sample before
		// it is ever read, so its un-flushed value is unobservable).
		if math.Abs(v1) < dblMin {
			v1 = 0.0
		}
		if math.Abs(v2) < dblMin {
			v2 = 0.0
		}
		if math.Abs(v3) < dblMin {
			v3 = 0.0
		}
		if math.Abs(v4) < dblMin {
			v4 = 0.0
		}

		st[0], st[1], st[2], st[3], st[4] = v0, v1, v2, v3, v4
	}
}

// Reset zeroes the per-channel filter state, as if the KFilter had just
// been constructed. Coefficients are unchanged.
func (f *KFilter) Reset() {
	for c := range f.v {
		f.v[c] = [filterStateSize]float64{}
	}
}
