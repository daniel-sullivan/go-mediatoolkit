// Package fvad is a cgo-free, bit-exact 1:1 port of libfvad — the
// WebRTC project's legacy GMM voice-activity detector, as extracted and
// repackaged by github.com/dpirch/libfvad (see ../../libfvad/VERSION for
// the pinned upstream commit).
//
// The reference implementation is pure fixed-point integer arithmetic —
// no float or double anywhere in the VAD path — so this port is
// bit-exact by construction on every platform, with no floating-point
// build-tag or compiler-flag ceremony (unlike the opus/flac/mp3 ports).
// Bit-exactness is pinned by the cgo parity slices under
// vad/internal/parity_tests/fvad_* which compile the vendored C as an
// in-test oracle.
//
// # Porting conventions (C → Go)
//
// The C source leans on two implementation-defined/undefined behaviours
// that gcc/clang on two's-complement targets resolve the same way Go
// defines them, so the port is direct:
//
//   - Arithmetic on int16_t operands is performed in C's promoted `int`
//     (32-bit here) and truncated back to 16 bits on assignment. The
//     port computes in int32 and converts with int16(...) exactly where
//     the C assigns to an int16_t lvalue. (For pure +,-,*,<< chains the
//     truncation point is immaterial — mod-2¹⁶ arithmetic commutes with
//     those ops — but right shifts, divisions and comparisons make the
//     truncation points load-bearing, so they are mirrored everywhere.)
//   - Signed int32 overflow (UB in C, explicitly tolerated upstream via
//     RTC_NO_SANITIZE("signed-integer-overflow"), bugs.webrtc.org/5486)
//     wraps two's-complement on the reference toolchains; Go's int32
//     wraps by definition. Likewise C's arithmetic right shift on
//     negative signed values matches Go's >> on signed integers.
//
// File mapping: signalproc.go ⇄ src/signal_processing/*, sp.go ⇄
// src/vad/vad_sp.c, gmm.go ⇄ src/vad/vad_gmm.c, filterbank.go ⇄
// src/vad/vad_filterbank.c, core.go ⇄ src/vad/vad_core.c, fvad.go ⇄
// src/fvad.c + include/fvad.h.
package fvad

import "math/bits"

// This file ports src/signal_processing/: spl_inl.{h,c},
// division_operations.c, get_scaling_square.c, energy.c,
// resample_by_2_internal.{h,c}, resample_fractional.c, resample_48khz.c.

// splWord16Max mirrors WEBRTC_SPL_WORD16_MAX.
const splWord16Max = 32767

// GetSizeInBits ports WebRtcSpl_GetSizeInBits: the position of the
// highest set bit (32 − clz). bits.LeadingZeros32 matches the C
// (n == 0 ? 32 : __builtin_clz(n)) exactly, including the n == 0 case.
func GetSizeInBits(n uint32) int16 {
	return int16(32 - bits.LeadingZeros32(n))
}

// NormW32 ports WebRtcSpl_NormW32: the number of steps a can be
// left-shifted without overflow, or 0 if a == 0.
func NormW32(a int32) int16 {
	if a == 0 {
		return 0
	}
	x := a
	if a < 0 {
		x = ^a
	}
	return int16(bits.LeadingZeros32(uint32(x)) - 1)
}

// NormU32 ports WebRtcSpl_NormU32: the number of leading zero bits,
// or 0 if a == 0.
func NormU32(a uint32) int16 {
	if a == 0 {
		return 0
	}
	return int16(bits.LeadingZeros32(a))
}

// DivW32W16 ports WebRtcSpl_DivW32W16: truncating (toward zero) integer
// division num/den, or 0x7FFFFFFF when den == 0. Go's / on integers
// truncates toward zero exactly like C99's.
//
// The one C call the port can never reproduce is num == math.MinInt32
// with den == -1 (UB overflow in C, a run-time panic in Go); no call
// site in libfvad can produce that pair (see the parity slice notes).
func DivW32W16(num int32, den int16) int32 {
	if den != 0 {
		return num / int32(den)
	}
	return 0x7FFFFFFF
}

// GetScalingSquare ports WebRtcSpl_GetScalingSquare: the right-shift
// needed so that the sum of `times` squared samples of in cannot
// overflow 32 bits.
//
// Note the faithful quirk: C computes sabs = -(*sptr) in promoted int
// but assigns it to an int16_t, so -(-32768) truncates back to -32768 —
// a buffer of all -32768 samples therefore leaves smax at its initial
// -1. Go's int16 negation wraps identically.
func GetScalingSquare(in []int16, times int) int16 {
	nbits := GetSizeInBits(uint32(times))
	smax := int16(-1)
	for _, v := range in {
		sabs := v
		if v <= 0 {
			sabs = -v // wraps for -32768, matching the C int16_t assignment
		}
		if sabs > smax {
			smax = sabs
		}
	}
	t := NormW32(int32(smax) * int32(smax))
	if smax == 0 {
		return 0 // Since norm(0) returns 0.
	}
	if t > nbits {
		return 0
	}
	return nbits - t
}

// Energy ports WebRtcSpl_Energy: the energy of vector, right-shifted by
// the returned scale factor so the accumulation fits 32 bits.
func Energy(vector []int16) (en int32, scaleFactor int) {
	scaling := GetScalingSquare(vector, len(vector))
	for _, v := range vector {
		en += (int32(v) * int32(v)) >> uint(scaling)
	}
	return en, int(scaling)
}

// kResampleAllpass mirrors resample_by_2_internal.c's allpass filter
// coefficients.
var kResampleAllpass = [2][3]int16{
	{821, 6110, 12382},
	{3050, 9368, 15063},
}

// DownBy2IntToShort ports WebRtcSpl_DownBy2IntToShort: decimate n
// int32 samples (Q15-shifted with +16384 offset) to n/2 saturated
// int16 samples. in is OVERWRITTEN (used as scratch), exactly like the
// C. state carries 8 filter states.
func DownBy2IntToShort(in []int32, n int, out []int16, state []int32) {
	half := n >> 1

	// Lower allpass filter (operates on even input samples).
	for i := 0; i < half; i++ {
		tmp0 := in[i<<1]
		diff := tmp0 - state[1]
		// Scale down and round.
		diff = (diff + (1 << 13)) >> 14
		tmp1 := state[0] + diff*int32(kResampleAllpass[1][0])
		state[0] = tmp0
		diff = tmp1 - state[2]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		tmp0 = state[1] + diff*int32(kResampleAllpass[1][1])
		state[1] = tmp1
		diff = tmp0 - state[3]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		state[3] = state[2] + diff*int32(kResampleAllpass[1][2])
		state[2] = tmp0

		// Divide by two and store temporarily.
		in[i<<1] = state[3] >> 1
	}

	// Upper allpass filter (operates on odd input samples).
	for i := 0; i < half; i++ {
		tmp0 := in[(i<<1)+1]
		diff := tmp0 - state[5]
		// Scale down and round.
		diff = (diff + (1 << 13)) >> 14
		tmp1 := state[4] + diff*int32(kResampleAllpass[0][0])
		state[4] = tmp0
		diff = tmp1 - state[6]
		// Scale down and round.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		tmp0 = state[5] + diff*int32(kResampleAllpass[0][1])
		state[5] = tmp1
		diff = tmp0 - state[7]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		state[7] = state[6] + diff*int32(kResampleAllpass[0][2])
		state[6] = tmp0

		// Divide by two and store temporarily.
		in[(i<<1)+1] = state[7] >> 1
	}

	// Combine allpass outputs.
	for i := 0; i < half; i += 2 {
		// Divide by two, add both allpass outputs and round.
		tmp0 := (in[i<<1] + in[(i<<1)+1]) >> 15
		tmp1 := (in[(i<<1)+2] + in[(i<<1)+3]) >> 15
		if tmp0 > 0x00007FFF {
			tmp0 = 0x00007FFF
		}
		if tmp0 < -0x8000 {
			tmp0 = -0x8000
		}
		out[i] = int16(tmp0)
		if tmp1 > 0x00007FFF {
			tmp1 = 0x00007FFF
		}
		if tmp1 < -0x8000 {
			tmp1 = -0x8000
		}
		out[i+1] = int16(tmp1)
	}
}

// DownBy2ShortToInt ports WebRtcSpl_DownBy2ShortToInt: decimate n int16
// samples to n/2 int32 samples (Q15-shifted with +16384 offset). state
// carries 8 filter states.
func DownBy2ShortToInt(in []int16, n int, out []int32, state []int32) {
	half := n >> 1

	// Lower allpass filter (operates on even input samples).
	for i := 0; i < half; i++ {
		tmp0 := (int32(in[i<<1]) << 15) + (1 << 14)
		diff := tmp0 - state[1]
		// Scale down and round.
		diff = (diff + (1 << 13)) >> 14
		tmp1 := state[0] + diff*int32(kResampleAllpass[1][0])
		state[0] = tmp0
		diff = tmp1 - state[2]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		tmp0 = state[1] + diff*int32(kResampleAllpass[1][1])
		state[1] = tmp1
		diff = tmp0 - state[3]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		state[3] = state[2] + diff*int32(kResampleAllpass[1][2])
		state[2] = tmp0

		// Divide by two and store temporarily.
		out[i] = state[3] >> 1
	}

	// Upper allpass filter (operates on odd input samples).
	for i := 0; i < half; i++ {
		tmp0 := (int32(in[(i<<1)+1]) << 15) + (1 << 14)
		diff := tmp0 - state[5]
		// Scale down and round.
		diff = (diff + (1 << 13)) >> 14
		tmp1 := state[4] + diff*int32(kResampleAllpass[0][0])
		state[4] = tmp0
		diff = tmp1 - state[6]
		// Scale down and round.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		tmp0 = state[5] + diff*int32(kResampleAllpass[0][1])
		state[5] = tmp1
		diff = tmp0 - state[7]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		state[7] = state[6] + diff*int32(kResampleAllpass[0][2])
		state[6] = tmp0

		// Divide by two and store temporarily.
		out[i] += state[7] >> 1
	}
}

// LPBy2IntToInt ports WebRtcSpl_LPBy2IntToInt: lowpass-filter n int32
// samples (Q15-shifted with +16384 offset) producing n normalized int32
// samples. state carries 16 filter states.
func LPBy2IntToInt(in []int32, n int, out []int32, state []int32) {
	half := n >> 1

	// Lower allpass filter: odd input -> even output samples. The C
	// advances `in` by one, primes tmp0 from the polyphase delay element
	// state[12], and reads the NEXT odd sample at the loop tail — the
	// last read (index 2·half−1) is consumed by the next call via
	// state[12], which the fourth loop updates.
	tmp0 := state[12]
	for i := 0; i < half; i++ {
		diff := tmp0 - state[1]
		// Scale down and round.
		diff = (diff + (1 << 13)) >> 14
		tmp1 := state[0] + diff*int32(kResampleAllpass[1][0])
		state[0] = tmp0
		diff = tmp1 - state[2]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		tmp0 = state[1] + diff*int32(kResampleAllpass[1][1])
		state[1] = tmp1
		diff = tmp0 - state[3]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		state[3] = state[2] + diff*int32(kResampleAllpass[1][2])
		state[2] = tmp0

		// Scale down, round and store.
		out[i<<1] = state[3] >> 1
		tmp0 = in[(i<<1)+1]
	}

	// Upper allpass filter: even input -> even output samples.
	for i := 0; i < half; i++ {
		tmp0 := in[i<<1]
		diff := tmp0 - state[5]
		// Scale down and round.
		diff = (diff + (1 << 13)) >> 14
		tmp1 := state[4] + diff*int32(kResampleAllpass[0][0])
		state[4] = tmp0
		diff = tmp1 - state[6]
		// Scale down and round.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		tmp0 = state[5] + diff*int32(kResampleAllpass[0][1])
		state[5] = tmp1
		diff = tmp0 - state[7]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		state[7] = state[6] + diff*int32(kResampleAllpass[0][2])
		state[6] = tmp0

		// Average the two allpass outputs, scale down and store.
		out[i<<1] = (out[i<<1] + (state[7] >> 1)) >> 15
	}

	// Lower allpass filter: even input -> odd output samples.
	for i := 0; i < half; i++ {
		tmp0 := in[i<<1]
		diff := tmp0 - state[9]
		// Scale down and round.
		diff = (diff + (1 << 13)) >> 14
		tmp1 := state[8] + diff*int32(kResampleAllpass[1][0])
		state[8] = tmp0
		diff = tmp1 - state[10]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		tmp0 = state[9] + diff*int32(kResampleAllpass[1][1])
		state[9] = tmp1
		diff = tmp0 - state[11]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		state[11] = state[10] + diff*int32(kResampleAllpass[1][2])
		state[10] = tmp0

		// Scale down, round and store.
		out[(i<<1)+1] = state[11] >> 1
	}

	// Upper allpass filter: odd input -> odd output samples.
	for i := 0; i < half; i++ {
		tmp0 := in[(i<<1)+1]
		diff := tmp0 - state[13]
		// Scale down and round.
		diff = (diff + (1 << 13)) >> 14
		tmp1 := state[12] + diff*int32(kResampleAllpass[0][0])
		state[12] = tmp0
		diff = tmp1 - state[14]
		// Scale down and round.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		tmp0 = state[13] + diff*int32(kResampleAllpass[0][1])
		state[13] = tmp1
		diff = tmp0 - state[15]
		// Scale down and truncate.
		diff >>= 14
		if diff < 0 {
			diff++
		}
		state[15] = state[14] + diff*int32(kResampleAllpass[0][2])
		state[14] = tmp0

		// Average the two allpass outputs, scale down and store.
		out[(i<<1)+1] = (out[(i<<1)+1] + (state[15] >> 1)) >> 15
	}
}

// kCoefficients48To32 mirrors resample_fractional.c's interpolation
// coefficients.
var kCoefficients48To32 = [2][8]int16{
	{778, -2050, 1087, 23285, 12903, -3783, 441, 222},
	{222, 441, -3783, 12903, 23285, 1087, -2050, 778},
}

// Resample48khzTo32khz ports WebRtcSpl_Resample48khzTo32khz: ratio-2/3
// resampling of k blocks (3 input samples -> 2 output samples per
// block). in must hold 3k+6 samples (each block reads two samples past
// its own 3), out 2k. in and out may overlap the way the 48→8 pipeline
// overlaps them.
func Resample48khzTo32khz(in []int32, out []int32, k int) {
	inIdx, outIdx := 0, 0
	for m := 0; m < k; m++ {
		tmp := int32(1 << 14)
		tmp += int32(kCoefficients48To32[0][0]) * in[inIdx+0]
		tmp += int32(kCoefficients48To32[0][1]) * in[inIdx+1]
		tmp += int32(kCoefficients48To32[0][2]) * in[inIdx+2]
		tmp += int32(kCoefficients48To32[0][3]) * in[inIdx+3]
		tmp += int32(kCoefficients48To32[0][4]) * in[inIdx+4]
		tmp += int32(kCoefficients48To32[0][5]) * in[inIdx+5]
		tmp += int32(kCoefficients48To32[0][6]) * in[inIdx+6]
		tmp += int32(kCoefficients48To32[0][7]) * in[inIdx+7]
		out[outIdx] = tmp

		tmp = 1 << 14
		tmp += int32(kCoefficients48To32[1][0]) * in[inIdx+1]
		tmp += int32(kCoefficients48To32[1][1]) * in[inIdx+2]
		tmp += int32(kCoefficients48To32[1][2]) * in[inIdx+3]
		tmp += int32(kCoefficients48To32[1][3]) * in[inIdx+4]
		tmp += int32(kCoefficients48To32[1][4]) * in[inIdx+5]
		tmp += int32(kCoefficients48To32[1][5]) * in[inIdx+6]
		tmp += int32(kCoefficients48To32[1][6]) * in[inIdx+7]
		tmp += int32(kCoefficients48To32[1][7]) * in[inIdx+8]
		out[outIdx+1] = tmp

		inIdx += 3
		outIdx += 2
	}
}

// State48khzTo8khz mirrors WebRtcSpl_State48khzTo8khz — the carried
// filter state of the three-stage 48 kHz → 8 kHz resampler.
type State48khzTo8khz struct {
	S48To24 [8]int32
	S24To24 [16]int32
	S24To16 [8]int32
	S16To8  [8]int32
}

// Reset ports WebRtcSpl_ResetResample48khzTo8khz (all states zeroed).
func (s *State48khzTo8khz) Reset() { *s = State48khzTo8khz{} }

// resample48To8TmpLen is the scratch length WebRtcVad_CalcVad48khz
// allocates for tmpmem: one 10 ms 48 kHz block (480) + 256 extra.
const resample48To8TmpLen = 480 + 256

// Resample48khzTo8khz ports WebRtcSpl_Resample48khzTo8khz: one 10 ms
// block — 480 samples at 48 kHz in, 80 samples at 8 kHz out. tmpmem
// must hold at least resample48To8TmpLen int32s; its prior contents do
// not influence the result (every read location is written first) but
// callers mirror the C and pass zeroed scratch.
func Resample48khzTo8khz(in []int16, out []int16, state *State48khzTo8khz, tmpmem []int32) {
	///// 48 --> 24 /////
	DownBy2ShortToInt(in, 480, tmpmem[256:], state.S48To24[:])

	///// 24 --> 24(LP) /////
	LPBy2IntToInt(tmpmem[256:], 240, tmpmem[16:], state.S24To24[:])

	///// 24 --> 16 /////
	// Copy state to and from the scratch array, exactly as the C does:
	// the 8 samples before the 240-block become the FIR history, and
	// the last 8 lowpassed samples become the next call's history.
	copy(tmpmem[8:16], state.S24To16[:])
	copy(state.S24To16[:], tmpmem[248:256])
	Resample48khzTo32khz(tmpmem[8:], tmpmem, 80)

	///// 16 --> 8 /////
	DownBy2IntToShort(tmpmem, 160, out, state.S16To8[:])
}
