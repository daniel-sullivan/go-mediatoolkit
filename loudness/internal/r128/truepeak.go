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
// the per-channel float32 delay lines (z[]) and the write cursor (zi).
// That state PERSISTS across Process/Oversample calls, so feeding a
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
type TruePeaker struct {
	factor   int            // oversampling factor (2 or 4)
	taps     int            // prototype FIR length (49)
	channels int            // interleaved channel count
	delay    int            // per-channel delay-line length
	filters  []interpFilter // one polyphase sub-filter per phase (len factor)
	z        [][]float32    // per-channel float32 delay lines (len channels)
	zi       int            // shared delay-line write cursor

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

	// One delay line per channel, zero-initialised (calloc).
	tp.z = make([][]float32, channels)
	for c := range tp.z {
		tp.z[c] = make([]float32, tp.delay)
	}

	tp.buildCoefficients()
	return tp
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
func (tp *TruePeaker) Oversample(in []float32, out []float32) int {
	if tp == nil {
		return 0
	}
	ch := tp.channels
	frames := len(in) / ch
	outStride := ch * tp.factor // interp->channels * interp->factor

	inIdx := 0
	for frame := 0; frame < frames; frame++ {
		base := frame * outStride // out advances by out_stride per input frame
		for chn := 0; chn < ch; chn++ {
			// Add sample to delay buffer: z[chan][zi] = *in++
			tp.z[chn][tp.zi] = in[inIdx]
			inIdx++

			outp := base + chn // outp = out + chan
			for f := 0; f < tp.factor; f++ {
				acc := 0.0
				filt := &tp.filters[f]
				z := tp.z[chn]
				for t := 0; t < filt.count; t++ {
					// i = (int)zi - (int)index[t]; if (i<0) i += delay
					i := tp.zi - filt.index[t]
					if i < 0 {
						i += tp.delay
					}
					// acc += (double) z[i] * coeff[t]
					// The inner float64() promotes the float32 delay sample
					// (as C's (double) cast does); the outer float64()
					// around the product is the FMA-suppression barrier.
					acc = float64(float64(z[i])*filt.coeff[t]) + acc
				}
				out[outp] = float32(acc) // *outp = (float) acc
				outp += ch               // outp += interp->channels
			}
		}
		// interp->zi++ with wrap at delay.
		tp.zi++
		if tp.zi == tp.delay {
			tp.zi = 0
		}
	}

	return frames * tp.factor
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

// Reset clears the per-channel delay lines and the write cursor,
// returning the interpolator to its just-constructed state (all filter
// history zeroed). Coefficients and buffer sizes are retained. It does
// not touch any caller-owned peak accumulators. No-op on a nil bypass
// TruePeaker.
func (tp *TruePeaker) Reset() {
	if tp == nil {
		return
	}
	for c := range tp.z {
		z := tp.z[c]
		for i := range z {
			z[i] = 0
		}
	}
	tp.zi = 0
}
