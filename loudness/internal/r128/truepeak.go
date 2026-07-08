// Package r128 holds the portable, pure-Go DSP core of the loudness
// package: the bit-exact 1:1 port of libebur128 v1.2.6's numerics
// (BS.1770 K-weighting filter, gating, and — here — the true-peak
// oversampling interpolator).
//
// # Why this is a separate internal package (not package loudness)
//
// Every parity slice under loudness/internal/parity_tests/ compiles its
// own copy of the vendored C libebur128 amalgamation as its oracle.
// libebur128's public symbols (ebur128_init, ...) are ordinary
// non-static C symbols, so if a parity slice imported a package that ALSO
// compiled that C, it would link two independent compilations of the same
// symbols into one test binary and collide. Keeping the pure-Go port in
// this cgo-free package lets both the parity slices AND the root loudness
// package import it without pulling any C object code along — importing
// loudness builds no C at all. That is also why the port's types and
// methods are exported: they are consumed from package loudness (meter,
// limiter) and from the parity slices, all of which live outside this
// package. The loudness/internal/ path already scopes that visibility to
// this module.
//
// Everything here is a faithful, width-for-width transliteration of the
// C source: the same operation order, the same float32-vs-float64
// choices at each step, and explicit conversion barriers at
// multiply-accumulate sites so the Go build cannot fuse a multiply-add
// (FMA) where the C — compiled with -ffp-contract=off — does not. See
// the MAC site in TruePeaker.Oversample for the barrier convention.
package r128

import "math"

// almostZero mirrors ebur128.c's ALMOST_ZERO (0.000001): the threshold
// below which a computed coefficient is treated as zero and dropped from
// the polyphase sub-filters, and below which the sinc argument is treated
// as the removable singularity at m == 0 (sinc(0) == 1).
const almostZero = 0.000001

// truePeakTaps is the fixed FIR length libebur128 uses for the true-peak
// interpolator (ebur128_init_resampler always passes 49). An odd tap
// count maximises the number of zero-valued coefficients that get dropped
// (see interp_create's "prefer odd to increase zero coeffs" note).
const truePeakTaps = 49

// interpFilter is one polyphase sub-filter: the subset of the 49-tap
// prototype whose taps fall on this phase of the interpolation factor.
// It mirrors ebur128.c's `interp_filter` struct (index[]/coeff[]/count).
//
// index[t] is the delay-line tap offset for coefficient coeff[t] (the C
// stores this as unsigned int; Go uses int so the delay-index wrap
// arithmetic in Oversample can go negative before wrapping, exactly as
// the C casts to signed int there). coeff[] is stored in float64
// (double), matching interp_create, even though Oversample multiplies it
// against a float32 delay sample.
type interpFilter struct {
	index []int     // delay index of the corresponding coefficient
	coeff []float64 // sub-filter coefficients (double, as in C)
	count int       // number of live taps written into index/coeff
}

// TruePeaker is the pure-Go port of libebur128's polyphase FIR
// true-peak interpolator (interp_create/interp_process) together with the
// per-buffer peak scan that libebur128 performs in
// ebur128_check_true_peak.
//
// # What it computes
//
// It oversamples each channel of an interleaved signal by an integer
// factor (4x below 96 kHz, 2x from 96 kHz up to but not including
// 192 kHz) using a 49-tap Hann-windowed-sinc prototype filter, then
// reports the maximum absolute reconstructed ("inter-sample") amplitude
// per channel. That maximum is a linear amplitude (NOT decibels): 1.0
// corresponds to digital full scale. Callers convert to dBTP with
// 20*log10 downstream. Because a band-limited signal can overshoot its
// sample values between samples, this true peak can exceed the plain
// sample peak — the reason BS.1770 / EBU R128 meter true peak at all.
//
// # State and the meter/limiter seam
//
// A TruePeaker owns exactly the state libebur128's `interpolator` owns:
// the delay line (a single mirrored, channel-interleaved float32 slice
// z[], see the layout section below) and the write cursor (zi). That
// state PERSISTS across Process/Oversample calls, so feeding a
// stream in arbitrarily-sized chunks yields bit-identical results to
// feeding it whole — the delay lines carry filter history across the
// seam. Reset clears them.
//
// Deliberately NOT owned here: the stream-level running maxima
// (libebur128's prev_true_peak/true_peak/sample_peak and the
// max(true_peak, sample_peak) fold in ebur128_true_peak). Process folds
// this buffer's peaks into a caller-supplied accumulator, exactly as
// libebur128's ebur128_check_true_peak folds into prev_true_peak; the
// meter that drives this owns the reset-per-add_frames and
// max-into-true_peak bookkeeping. This mirrors the C structure 1:1: the
// interpolator is a pure per-buffer primitive; the state machine around
// it lives in the meter.
//
// # ≥192 kHz bypass
//
// At sample rates of 192 kHz and above libebur128 does not build an
// interpolator (ebur128_init_resampler sets interp = NULL): there is no
// meaningful oversampling headroom, so BS.1770 treats the true peak as
// equal to the sample peak. NewTruePeaker returns nil for those rates.
// All methods are nil-safe no-ops, so a meter can hold a possibly-nil
// *TruePeaker and, when it is nil, simply report the sample peak as the
// true peak (which is what max(true_peak=0, sample_peak) yields in C).
//
// TruePeaker is not safe for concurrent use; a live meter/limiter owns
// one per stream and drives it from a single goroutine.
//
// # Delay-line layout (mirrored + channel-interleaved)
//
// The per-channel delay lines are stored in ONE flat float32 slice z,
// laid out as z[k*channels + c] for ring slot k and channel c. Two
// optimisations shape this layout, both purely internal (the float32
// values stored, and therefore every output bit, are identical to the
// straightforward per-channel ring libebur128 uses):
//
//   - Channel interleaving. Storing channel c of ring slot k adjacently
//     lets an arm64/amd64 kernel load a stereo pair with one 64-bit load
//     and process both channels as SIMD lanes.
//
//   - Mirror doubling. The ring holds `delay` slots, but z is allocated
//     for 2*delay slots and every incoming sample is written to BOTH slot
//     zi AND slot zi+delay. The read window for a polyphase phase is
//     index[t] taps back from the just-written slot; reading from the
//     high copy at base = zi+delay means base-index[t] lands in
//     [1, 2*delay-1] for every zi and every tap, so the modulo wrap that
//     the C's `if (i < 0) i += delay` performs per tap is eliminated —
//     the read is an unconditional descending walk. Because z[k] and
//     z[k+delay] always hold the same sample (both are written every
//     frame), z[base-index[t]] == z[(zi-index[t]) mod delay], bit for bit.
type TruePeaker struct {
	factor   int            // oversampling factor (2 or 4)
	taps     int            // prototype FIR length (49)
	channels int            // interleaved channel count
	delay    int            // per-channel delay-line length
	filters  []interpFilter // one polyphase sub-filter per phase (len factor)
	z        []float32      // mirrored, interleaved delay line (len 2*delay*channels)
	zi       int            // shared delay-line write cursor (0..delay-1)

	// SIMD fast-path descriptors, computed once in analyzeSIMD. simd is
	// true when the polyphase decomposition matches the shape the
	// channel-lane kernels assume: phase 0 is a single center tap with
	// coefficient exactly 1.0, and every other phase reads a contiguous
	// descending window (index[t] == t) of one fixed length. Both the 4x
	// (49-tap) and 2x (49-tap) libebur128 configurations satisfy this; if
	// a future configuration did not, simd stays false and Oversample
	// falls back to the fully general pure-Go path (which handles any
	// index[] via the same mirror addressing).
	simd        bool      // eligible for the channel-lane kernels
	phase0Index int       // delay-tap offset of phase 0's single center tap
	simdCount   int       // tap count shared by phases 1..factor-1
	coeffFlat   []float64 // phases 1..factor-1 coeffs, concatenated (len (factor-1)*simdCount)

	// Reused scratch for Process, mirroring libebur128's preallocated
	// resampler_buffer_input / resampler_buffer_output. Grown on demand;
	// their contents are fully overwritten each call, so they need no
	// reset. Kept off the hot path's allocator to match the C's
	// zero-allocation steady state.
	in  []float32 // float32 view of the current input buffer
	out []float32 // oversampled output (frames*factor*channels)
}

// NewTruePeaker builds the true-peak interpolator for the given sample
// rate and channel count, selecting the oversampling factor exactly as
// libebur128's ebur128_init_resampler does:
//
//   - sampleRate < 96000        -> 4x, 49 taps
//   - 96000 <= sampleRate < 192000 -> 2x, 49 taps
//   - sampleRate >= 192000      -> nil (bypass; true peak == sample peak)
//
// A nil return is not an error: it is the documented ≥192 kHz bypass, and
// every method is a nil-safe no-op. channels must be >= 1; the caller
// (the meter constructor) validates channel count before reaching here.
func NewTruePeaker(sampleRate, channels int) *TruePeaker {
	var factor int
	switch {
	case sampleRate < 96000:
		factor = 4
	case sampleRate < 192000:
		factor = 2
	default:
		return nil // ebur128_init_resampler: interp = NULL
	}

	tp := &TruePeaker{
		factor:   factor,
		taps:     truePeakTaps,
		channels: channels,
		// interp->delay = (taps + factor - 1) / factor
		delay: (truePeakTaps + factor - 1) / factor,
	}

	// One polyphase sub-filter per phase; index/coeff sized to delay
	// (calloc(delay, ...) in interp_create), only `count` entries live.
	tp.filters = make([]interpFilter, factor)
	for f := range tp.filters {
		tp.filters[f].index = make([]int, tp.delay)
		tp.filters[f].coeff = make([]float64, tp.delay)
	}

	// Mirrored (2x delay slots), channel-interleaved delay line,
	// zero-initialised (calloc). See the TruePeaker doc comment.
	tp.z = make([]float32, 2*tp.delay*channels)

	tp.buildCoefficients()
	tp.analyzeSIMD()
	return tp
}

// analyzeSIMD inspects the polyphase decomposition buildCoefficients
// produced and, when it matches the shape the channel-lane kernels
// assume, fills in the fast-path descriptors (simd, phase0Index,
// simdCount, coeffFlat). The required shape is:
//
//   - phase 0 has exactly one live tap whose coefficient is exactly 1.0
//     (the prototype's center tap: sinc(0) * Hann(center) == 1.0);
//   - every other phase has the same live-tap count, with index[t] == t
//     for all t — i.e. its read window is the contiguous descending run
//     of the most recent `count` delay slots.
//
// Both configurations libebur128 ever builds (49 taps at 4x: phase 0 =
// {index 6, coeff 1.0}, phases 1-3 = 12 taps each at index [0..11]; 49
// taps at 2x: phase 0 = {index 12, coeff 1.0}, phase 1 = 24 taps at
// index [0..23]) satisfy this. The properties are verified here rather
// than assumed so that if the tap count or factor selection ever
// changed, Oversample would silently fall back to the fully general
// path instead of miscomputing. coeffFlat concatenates phases
// 1..factor-1's live coefficients ((factor-1)*simdCount float64s) so
// the kernels index one flat array.
func (tp *TruePeaker) analyzeSIMD() {
	// factor is always 2 or 4 by construction (see NewTruePeaker's
	// switch); no factor<2 guard is needed here.
	f0 := &tp.filters[0]
	if f0.count != 1 || f0.coeff[0] != 1.0 {
		return
	}
	count := tp.filters[1].count
	if count == 0 {
		return
	}
	for f := 1; f < tp.factor; f++ {
		filt := &tp.filters[f]
		if filt.count != count {
			return
		}
		for t := 0; t < filt.count; t++ {
			if filt.index[t] != t {
				return
			}
		}
	}
	tp.simd = true
	tp.phase0Index = f0.index[0]
	tp.simdCount = count
	tp.coeffFlat = make([]float64, (tp.factor-1)*count)
	for f := 1; f < tp.factor; f++ {
		copy(tp.coeffFlat[(f-1)*count:f*count], tp.filters[f].coeff[:count])
	}
}

// buildCoefficients ports interp_create's coefficient generation loop
// verbatim: a Hann-windowed sinc prototype, with |c| < ALMOST_ZERO taps
// dropped, decomposed into per-phase sub-filters. The expression order
// and the double precision match the C exactly so the generated
// coefficients are bit-identical to the oracle's (a coefficient that
// differs even in its last double bit could leak into a float32 output
// ULP after the MAC).
func (tp *TruePeaker) buildCoefficients() {
	factor := float64(tp.factor)
	for j := 0; j < tp.taps; j++ {
		// Calculate sinc.
		//   double m = (double) j - (double) (interp->taps - 1) / 2.0;
		m := float64(j) - float64(tp.taps-1)/2.0
		c := 1.0
		if math.Abs(m) > almostZero {
			// c = sin(m*M_PI/factor) / (m*M_PI/factor)
			c = math.Sin(m*math.Pi/factor) / (m * math.Pi / factor)
		}
		// Apply Hanning window: c *= 0.5 * (1 - cos(2*M_PI*j/(taps-1)))
		c *= 0.5 * (1 - math.Cos(2*math.Pi*float64(j)/float64(tp.taps-1)))

		if math.Abs(c) > almostZero { // Ignore any zero coeffs.
			f := j % tp.factor
			t := tp.filters[f].count
			tp.filters[f].coeff[t] = c
			tp.filters[f].index[t] = j / tp.factor
			tp.filters[f].count++
		}
	}
}

// Factor reports the oversampling factor (2 or 4). Callers sizing an
// Oversample output buffer need frames*Factor*Channels float32 samples.
func (tp *TruePeaker) Factor() int {
	if tp == nil {
		return 1
	}
	return tp.factor
}

// Channels reports the interleaved channel count the interpolator was
// built for.
func (tp *TruePeaker) Channels() int {
	if tp == nil {
		return 0
	}
	return tp.channels
}

// Delay reports the per-channel delay-line length (the interpolator's
// group-delay buffer size). The limiter uses this to size its lookahead.
func (tp *TruePeaker) Delay() int {
	if tp == nil {
		return 0
	}
	return tp.delay
}

// Oversample runs the polyphase FIR interpolator over one interleaved
// float32 buffer, a 1:1 port of libebur128's interp_process. It reads
// len(in)/Channels frames from in and writes frames*Factor output frames
// (frames*Factor*Channels interleaved float32 samples) into out, which
// the caller must size accordingly. It returns the number of OUTPUT
// frames written (frames*Factor), matching interp_process's return value.
//
// The per-channel delay lines and write cursor advance and persist across
// calls, so chunked input is bit-identical to whole-buffer input.
//
// # Precision (load-bearing — this is what makes parity hold)
//
// Mirroring interp_process exactly:
//   - the delay line z is float32;
//   - each coefficient is float64;
//   - the accumulator is float64;
//   - each tap promotes the float32 delay sample to float64, multiplies
//     by the float64 coefficient, and adds into the float64 accumulator;
//   - the finished accumulator is truncated to float32 on output.
//
// The float64(a*b) at the MAC is an explicit rounding barrier: it forces
// the product to round to float64 before the add, so the Go compiler
// cannot contract the multiply-add into a single FMA. libebur128's oracle
// is compiled with -ffp-contract=off, so it never fuses either; matching
// that is what keeps the float32 outputs bit-identical.
//
// # Dispatch
//
// Oversample picks the fastest kernel whose arithmetic is provably
// bit-identical to the reference loop:
//
//   - if the polyphase decomposition is not SIMD-shaped (see analyzeSIMD;
//     never the case for libebur128's configurations), the fully general
//     oversampleGeneric runs;
//   - otherwise channels are processed in pairs by oversamplePair (a
//     2-lane NEON/SSE2 kernel on arm64/amd64, the pure-Go
//     oversamplePairGeneric elsewhere), with oversampleColumnScalar
//     taking the odd trailing channel; mono is a single scalar column.
//
// Each column kernel touches only its own channels' interleaved slots
// and advances a private copy of the cursor identically, so running them
// sequentially over the same frame range is equivalent to the reference
// frame-major order. Every kernel performs, per (channel, phase), the
// same float32->float64 promotion, float64 multiply, float64 add
// sequence in the same order as the reference loop — see each kernel's
// comment for why its output is bit-identical.
func (tp *TruePeaker) Oversample(in []float32, out []float32) int {
	if tp == nil {
		return 0
	}
	ch := tp.channels
	frames := len(in) / ch
	if frames == 0 {
		return 0
	}
	if !tp.simd {
		return tp.oversampleGeneric(in, out)
	}
	var zi int
	c := 0
	for ; c+1 < ch; c += 2 {
		zi = tp.oversamplePair(in, out, c)
	}
	if c < ch {
		zi = tp.oversampleColumnScalar(in, out, c)
	}
	tp.zi = zi
	return frames * tp.factor
}

// oversampleGeneric is the fully general reference kernel: it handles
// any polyphase decomposition (arbitrary index[] contents) and any
// channel count, using the mirrored delay line for wrap-free reads but
// otherwise following interp_process's frame->channel->phase->tap nest
// with the exact reference arithmetic. It is the fallback for non-SIMD-
// shaped decompositions and the oracle the equality tests compare the
// optimised kernels against. It updates tp.zi itself and returns the
// output frame count.
func (tp *TruePeaker) oversampleGeneric(in []float32, out []float32) int {
	ch := tp.channels
	frames := len(in) / ch
	outStride := ch * tp.factor
	delay := tp.delay
	z := tp.z
	zi := tp.zi

	for frame := 0; frame < frames; frame++ {
		// Write this frame's samples to both mirrors.
		row := in[frame*ch : frame*ch+ch]
		lo := zi * ch
		copy(z[lo:lo+ch], row)
		hi := (zi + delay) * ch
		copy(z[hi:hi+ch], row)

		base := frame * outStride
		for chn := 0; chn < ch; chn++ {
			// top = flat offset of ring slot zi's high mirror, channel chn;
			// tap index[t] reads slot zi-index[t] (mod delay) == flat
			// top - index[t]*ch, always in range (see layout doc).
			top := hi + chn
			outp := base + chn
			for f := 0; f < tp.factor; f++ {
				acc := 0.0
				filt := &tp.filters[f]
				index := filt.index[:filt.count]
				coeff := filt.coeff[:filt.count]
				for t := range index {
					// acc += (double) z[i] * coeff[t]
					// The inner float64() promotes the float32 delay sample
					// (as C's (double) cast does); the outer float64()
					// around the product is the FMA-suppression barrier.
					acc = float64(float64(z[top-index[t]*ch])*coeff[t]) + acc
				}
				out[outp] = float32(acc) // *outp = (float) acc
				outp += ch               // outp += interp->channels
			}
		}
		// interp->zi++ with wrap at delay.
		zi++
		if zi == delay {
			zi = 0
		}
	}

	tp.zi = zi
	return frames * tp.factor
}

// oversampleColumnScalar processes ONE channel column (c0) of the
// interleaved stream through the SIMD-shaped fast path in pure Go: the
// mirrored delay line makes every phase's read window the contiguous
// descending run of slots ending at the just-written one, so the tap
// loop is a branch-free pointer walk. It reads its private cursor from
// tp.zi, does NOT write it back (Oversample commits the cursor once
// after all columns), and returns the final cursor value.
//
// Bit-exactness vs the reference loop, per output sample:
//
//   - phases 1..factor-1 run the identical per-tap sequence
//     float64(float64(z)*coeff) + acc in the same ascending-tap order —
//     only the address computation differs;
//   - phase 0 (single tap, coefficient exactly 1.0) skips the multiply:
//     float64(v)*1.0 == float64(v) for every value a float32 can
//     promote to (multiplication by one is exact), and the reference's
//     `+ acc` with acc == 0.0 is kept because +0.0 canonicalises a
//     -0.0 product to +0.0, which a bare copy would miss.
func (tp *TruePeaker) oversampleColumnScalar(in []float32, out []float32, c0 int) int {
	ch := tp.channels
	frames := len(in) / ch
	outStride := ch * tp.factor
	factor := tp.factor
	delay := tp.delay
	count := tp.simdCount
	coeff := tp.coeffFlat
	p0 := tp.phase0Index * ch
	z := tp.z
	zi := tp.zi

	for frame := 0; frame < frames; frame++ {
		s := in[frame*ch+c0]
		top := (zi+delay)*ch + c0 // high-mirror slot zi, channel c0
		z[zi*ch+c0] = s
		z[top] = s

		outp := frame*outStride + c0
		// Phase 0: single center tap, coeff exactly 1.0 (see doc above).
		out[outp] = float32(float64(z[top-p0]) + 0.0)
		outp += ch

		cb := 0
		for f := 1; f < factor; f++ {
			acc := 0.0
			// Per-phase coefficient window: reslicing to exactly `count`
			// elements lets the compiler prove t < len(phaseCoeff) from the
			// loop bound alone, eliminating the coeff[cb+t] bounds check
			// that a raw coeff[cb+t] index carries on every tap. z's own
			// tap index (ptr, strided by the runtime channel count ch)
			// can't be proven this way — a non-constant stride defeats the
			// compiler's induction-variable range check — so it keeps its
			// per-tap check.
			phaseCoeff := coeff[cb : cb+count]
			ptr := top
			for t := 0; t < count; t++ {
				acc = float64(float64(z[ptr])*phaseCoeff[t]) + acc
				ptr -= ch
			}
			out[outp] = float32(acc)
			outp += ch
			cb += count
		}

		zi++
		if zi == delay {
			zi = 0
		}
	}
	return zi
}

// oversamplePairGeneric is the pure-Go 2-lane pair kernel: channels c0
// and c0+1 are processed together as the two lanes the NEON/SSE2
// kernels vectorise, tap-major so the per-tap address and coefficient
// work is shared between the lanes. Each lane's arithmetic is the exact
// per-channel sequence of oversampleColumnScalar (and therefore of the
// reference loop) — the lanes never mix, they only share addressing. On
// arm64/amd64 the assembly oversamplePair shadows this function;
// keeping it compiled everywhere lets the equality tests run asm vs
// pure Go on the same machine. Cursor protocol matches
// oversampleColumnScalar: reads tp.zi, returns the final cursor,
// never writes tp.zi.
func (tp *TruePeaker) oversamplePairGeneric(in []float32, out []float32, c0 int) int {
	ch := tp.channels
	frames := len(in) / ch
	outStride := ch * tp.factor
	factor := tp.factor
	delay := tp.delay
	count := tp.simdCount
	coeff := tp.coeffFlat
	p0 := tp.phase0Index * ch
	z := tp.z
	zi := tp.zi

	for frame := 0; frame < frames; frame++ {
		s0 := in[frame*ch+c0]
		s1 := in[frame*ch+c0+1]
		top := (zi+delay)*ch + c0
		lo := zi*ch + c0
		z[lo] = s0
		z[lo+1] = s1
		z[top] = s0
		z[top+1] = s1

		outp := frame*outStride + c0
		// Phase 0 (see oversampleColumnScalar for the 1.0-coeff proof).
		out[outp] = float32(float64(z[top-p0]) + 0.0)
		out[outp+1] = float32(float64(z[top-p0+1]) + 0.0)
		outp += ch

		cb := 0
		for f := 1; f < factor; f++ {
			acc0 := 0.0
			acc1 := 0.0
			// See oversampleColumnScalar for why this reslice eliminates
			// the coeff[cb+t] bounds check but z's stride-ch tap index
			// cannot be proven the same way.
			phaseCoeff := coeff[cb : cb+count]
			ptr := top
			for t := 0; t < count; t++ {
				c := phaseCoeff[t]
				acc0 = float64(float64(z[ptr])*c) + acc0
				acc1 = float64(float64(z[ptr+1])*c) + acc1
				ptr -= ch
			}
			out[outp] = float32(acc0)
			out[outp+1] = float32(acc1)
			outp += ch
			cb += count
		}

		zi++
		if zi == delay {
			zi = 0
		}
	}
	return zi
}

// Process scans one interleaved float64 buffer for inter-sample true
// peaks and folds this buffer's per-channel maximum absolute
// reconstructed amplitude into peaks, exactly as libebur128's
// ebur128_check_true_peak folds into prev_true_peak.
//
// It is the composition of the two steps libebur128 performs in its
// double-input filter path:
//
//  1. Convert each input sample to float32. In libebur128 this is
//     resampler_buffer_input[k] = (float)((double)src[k] / scaling_factor);
//     for double-typed input scaling_factor is 1.0, so it is exactly a
//     float32 narrowing — replicated here as float32(samples[k]).
//  2. Oversample, then take, per channel, the maximum of |float32
//     output| promoted to float64, folded into the running peaks.
//
// peaks is a caller-owned accumulator of length Channels; each element is
// updated to max(peaks[c], thisBufferPeak[c]) and never lowered. The
// caller (meter) zeroes it per add_frames call and folds it into the
// stream-level true_peak, owning all cross-buffer bookkeeping. samples
// must be interleaved with len a multiple of Channels; any trailing
// partial frame is ignored (the meter feeds whole frames).
//
// The returned peaks are linear amplitudes (full scale == 1.0), matching
// libebur128's true_peak[] units.
//
// On a nil (≥192 kHz bypass) TruePeaker this is a no-op: peaks is left
// untouched, so the meter's max(true_peak, sample_peak) reduces to the
// sample peak — the correct bypass behaviour.
func (tp *TruePeaker) Process(samples []float64, peaks []float64) {
	if tp == nil {
		return
	}
	ch := tp.channels
	frames := len(samples) / ch
	if frames == 0 {
		return
	}

	// Step 1: fill the float32 input scratch (resampler_buffer_input).
	if cap(tp.in) < frames*ch {
		tp.in = make([]float32, frames*ch)
	}
	tp.in = tp.in[:frames*ch]
	for k := 0; k < frames*ch; k++ {
		tp.in[k] = float32(samples[k])
	}

	// Step 2: oversample into the output scratch (resampler_buffer_output).
	outLen := frames * tp.factor * ch
	if cap(tp.out) < outLen {
		tp.out = make([]float32, outLen)
	}
	tp.out = tp.out[:outLen]
	framesOut := tp.Oversample(tp.in, tp.out)

	// Fold the per-channel max |output| into peaks, exactly as
	// ebur128_check_true_peak does:
	//   val = (double) out[i*ch+c];
	//   if (EBUR128_MAX(val,-val) > prev_true_peak[c])
	//       prev_true_peak[c] = EBUR128_MAX(val,-val);
	// EBUR128_MAX(val,-val) is abs(val); it is spelled out as the C's
	// branch (not math.Abs) so the -0.0 / boundary behaviour is identical.
	for i := 0; i < framesOut; i++ {
		row := i * ch
		for c := 0; c < ch; c++ {
			val := float64(tp.out[row+c])
			mx := val
			if -val > mx {
				mx = -val
			}
			if mx > peaks[c] {
				peaks[c] = mx
			}
		}
	}
}

// Reset clears the delay line (both mirrors of every channel column)
// and the write cursor, returning the interpolator to its
// just-constructed state (all filter history zeroed). Coefficients and
// buffer sizes are retained. It does not touch any caller-owned peak
// accumulators. No-op on a nil bypass TruePeaker.
func (tp *TruePeaker) Reset() {
	if tp == nil {
		return
	}
	for i := range tp.z {
		tp.z[i] = 0
	}
	tp.zi = 0
}
