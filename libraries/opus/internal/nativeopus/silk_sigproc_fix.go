package nativeopus

// Port of libopus/silk/SigProc_FIX.h — macro and inline-function
// portion only. The function *declarations* (silk_resampler_*,
// silk_biquad_alt_*, silk_LPC_analysis_filter, silk_burg_modified_c,
// silk_NLSF_stabilize, silk_A2NLSF, silk_NLSF2A, silk_insertion_sort_*,
// silk_inner_prod_aligned{,_scale}, silk_inner_prod16_c, etc.) will
// land in the respective C-file ports during Phase 6+.
//
// Every macro is a deterministic integer operation, so bit-exact
// parity with the C oracle is guaranteed by matching the exact
// arithmetic (including sign-extension and shift semantics). There is
// no FMA concern here — these are pure integer ops.
//
// Note: Go's signed `>>` is arithmetic (sign-preserving) like the de
// facto C behaviour on every platform we care about, and Go's typed
// integer conversions preserve bit patterns via 2's-complement
// reinterpretation, so `int16(v opus_int32)` takes the low 16 bits
// and sign-extends on subsequent promotion — matching C's `(opus_int16)v`.

const SILK_MAX_ORDER_LPC = 24

// silk_ROR32 rotates a32 right by rot bits. Negative rot rotates left.
// C: OPUS_INLINE opus_int32 at SigProc_FIX.h:394-406.
func silk_ROR32(a32 opus_int32, rot opus_int) opus_int32 {
	x := opus_uint32(a32)
	r := opus_uint32(rot)
	m := opus_uint32(-rot)
	if rot == 0 {
		return a32
	} else if rot < 0 {
		return opus_int32((x << m) | (x >> (32 - m)))
	}
	return opus_int32((x << (32 - r)) | (x >> r))
}

// ── Fixed-point integer multiply helpers ────────────────────────────

func silk_MUL(a32, b32 opus_int32) opus_int32        { return a32 * b32 }
func silk_MUL_uint(a32, b32 opus_uint32) opus_uint32 { return a32 * b32 }

// silk_MLA = a32 + (b32 * c32). C: silk_ADD32((a32),((b32)*(c32))).
func silk_MLA(a32, b32, c32 opus_int32) opus_int32 {
	return silk_ADD32(a32, b32*c32)
}
func silk_MLA_uint(a32, b32, c32 opus_uint32) opus_uint32 {
	return a32 + b32*c32
}

// silk_SMULTT = (a32 >> 16) * (b32 >> 16).
func silk_SMULTT(a32, b32 opus_int32) opus_int32 {
	return (a32 >> 16) * (b32 >> 16)
}

// silk_SMLATT = a32 + (b32 >> 16) * (c32 >> 16).
func silk_SMLATT(a32, b32, c32 opus_int32) opus_int32 {
	return silk_ADD32(a32, (b32>>16)*(c32>>16))
}

// silk_SMLALBB = a64 + (int64)((int16)b * (int16)c). The C macro sign-
// extends both 16-bit multiplicands to int32 via cast, multiplies, and
// widens the product to int64 before adding.
func silk_SMLALBB(a64 opus_int64, b16, c16 opus_int16) opus_int64 {
	return silk_ADD64(a64, opus_int64(opus_int32(b16)*opus_int32(c16)))
}

// silk_SMULL = (int64)a32 * b32.
func silk_SMULL(a32, b32 opus_int32) opus_int64 {
	return opus_int64(a32) * opus_int64(b32)
}

// Overflow-tolerant add/sub via unsigned reinterpretation.
func silk_ADD32_ovflw(a, b opus_int32) opus_int32 {
	return opus_int32(opus_uint32(a) + opus_uint32(b))
}
func silk_SUB32_ovflw(a, b opus_int32) opus_int32 {
	return opus_int32(opus_uint32(a) - opus_uint32(b))
}

// silk_MLA_ovflw = silk_ADD32_ovflw(a32, (uint32)b32 * (uint32)c32).
func silk_MLA_ovflw(a32, b32, c32 opus_int32) opus_int32 {
	return silk_ADD32_ovflw(a32, opus_int32(opus_uint32(b32)*opus_uint32(c32)))
}

// silk_SMLABB_ovflw = silk_ADD32_ovflw(a32, (int16)b * (int16)c).
func silk_SMLABB_ovflw(a32, b32, c32 opus_int32) opus_int32 {
	return silk_ADD32_ovflw(a32, opus_int32(opus_int16(b32))*opus_int32(opus_int16(c32)))
}

// Integer division (truncation toward zero, same as C).
func silk_DIV32_16(a32 opus_int32, b16 opus_int32) opus_int32 {
	return a32 / b16
}
func silk_DIV32(a32, b32 opus_int32) opus_int32 { return a32 / b32 }

// Plain add/sub wrappers. silk_ADD* in C are #defines used to enable
// debug-mode overflow checks; without ENABLE_ASSERTIONS they collapse
// to (a)+(b). Signed overflow is implementation-defined in C but
// matches Go's 2's-complement wraparound on every supported platform.
func silk_ADD16(a, b opus_int16) opus_int16 { return a + b }
func silk_ADD32(a, b opus_int32) opus_int32 { return a + b }
func silk_ADD64(a, b opus_int64) opus_int64 { return a + b }
func silk_SUB16(a, b opus_int16) opus_int16 { return a - b }
func silk_SUB32(a, b opus_int32) opus_int32 { return a - b }
func silk_SUB64(a, b opus_int64) opus_int64 { return a - b }

// Saturation to fixed integer widths.
func silk_SAT8(a opus_int32) opus_int32 {
	switch {
	case a > opus_int32(silk_int8_MAX):
		return opus_int32(silk_int8_MAX)
	case a < opus_int32(silk_int8_MIN):
		return opus_int32(silk_int8_MIN)
	}
	return a
}
func silk_SAT16(a opus_int32) opus_int32 {
	switch {
	case a > opus_int32(silk_int16_MAX):
		return opus_int32(silk_int16_MAX)
	case a < opus_int32(silk_int16_MIN):
		return opus_int32(silk_int16_MIN)
	}
	return a
}
func silk_SAT32(a opus_int64) opus_int64 {
	switch {
	case a > opus_int64(silk_int32_MAX):
		return opus_int64(silk_int32_MAX)
	case a < opus_int64(silk_int32_MIN):
		return opus_int64(silk_int32_MIN)
	}
	return a
}

// silk_CHECK_FIT* are identity in non-debug builds.
func silk_CHECK_FIT8(a opus_int32) opus_int32  { return a }
func silk_CHECK_FIT16(a opus_int32) opus_int32 { return a }
func silk_CHECK_FIT32(a opus_int64) opus_int64 { return a }

// Saturating add/sub at specific widths.
func silk_ADD_SAT16(a, b opus_int16) opus_int16 {
	return opus_int16(silk_SAT16(silk_ADD32(opus_int32(a), opus_int32(b))))
}
func silk_SUB_SAT16(a, b opus_int16) opus_int16 {
	return opus_int16(silk_SAT16(silk_SUB32(opus_int32(a), opus_int32(b))))
}

// silk_ADD_SAT32 / silk_SUB_SAT32 — saturating 32-bit add/sub. The
// simplest bit-exact formulation in Go is to compute the sum in int64
// and clamp; this matches the C macro's behaviour for every input.
func silk_ADD_SAT32(a, b opus_int32) opus_int32 {
	r := opus_int64(a) + opus_int64(b)
	if r > opus_int64(silk_int32_MAX) {
		return silk_int32_MAX
	}
	if r < opus_int64(silk_int32_MIN) {
		return silk_int32_MIN
	}
	return opus_int32(r)
}
func silk_SUB_SAT32(a, b opus_int32) opus_int32 {
	r := opus_int64(a) - opus_int64(b)
	if r > opus_int64(silk_int32_MAX) {
		return silk_int32_MAX
	}
	if r < opus_int64(silk_int32_MIN) {
		return silk_int32_MIN
	}
	return opus_int32(r)
}

// 64-bit saturating add/sub — no wider type, so we use the standard
// signed-overflow detection: ((a^sum) & (b^sum)) < 0 exactly when the
// sign of the sum differs from both inputs' signs (classic two's-
// complement overflow test).
func silk_ADD_SAT64(a, b opus_int64) opus_int64 {
	sum := opus_int64(opus_uint64(a) + opus_uint64(b))
	if (a^sum)&(b^sum) < 0 {
		if a < 0 {
			return silk_int64_MIN
		}
		return silk_int64_MAX
	}
	return sum
}
func silk_SUB_SAT64(a, b opus_int64) opus_int64 {
	diff := opus_int64(opus_uint64(a) - opus_uint64(b))
	if (a^b)&(a^diff) < 0 {
		if a < 0 {
			return silk_int64_MIN
		}
		return silk_int64_MAX
	}
	return diff
}

// One-sided positive saturation.
func silk_POS_SAT32(a opus_int32) opus_int32 {
	if a > silk_int32_MAX {
		return silk_int32_MAX
	}
	return a
}
func silk_ADD_POS_SAT8(a, b opus_int32) opus_int32 {
	if (a+b)&0x80 != 0 {
		return opus_int32(silk_int8_MAX)
	}
	return a + b
}
func silk_ADD_POS_SAT16(a, b opus_int32) opus_int32 {
	if (a+b)&0x8000 != 0 {
		return opus_int32(silk_int16_MAX)
	}
	return a + b
}
func silk_ADD_POS_SAT32(a, b opus_int32) opus_int32 {
	if (opus_uint32(a)+opus_uint32(b))&(1<<31) != 0 {
		return silk_int32_MAX
	}
	return a + b
}

// ── Shifts ──────────────────────────────────────────────────────────
//
// C uses unsigned casts to make left shift well-defined under signed
// overflow. Go's left shift on signed ints wraps (defined behaviour)
// but we mirror the unsigned path anyway to match bit-pattern exactly.

func silk_LSHIFT8(a opus_int8, shift opus_int) opus_int8 {
	return opus_int8(opus_uint8(a) << shift)
}
func silk_LSHIFT16(a opus_int16, shift opus_int) opus_int16 {
	return opus_int16(opus_uint16(a) << shift)
}
func silk_LSHIFT32(a opus_int32, shift opus_int) opus_int32 {
	return opus_int32(opus_uint32(a) << shift)
}
func silk_LSHIFT64(a opus_int64, shift opus_int) opus_int64 {
	return opus_int64(opus_uint64(a) << shift)
}
func silk_LSHIFT(a opus_int32, shift opus_int) opus_int32 {
	return silk_LSHIFT32(a, shift)
}

// Arithmetic right shifts: Go's `>>` on signed ints is arithmetic,
// matching the implementation-defined-but-universal C behaviour.
func silk_RSHIFT8(a opus_int8, shift opus_int) opus_int8    { return a >> shift }
func silk_RSHIFT16(a opus_int16, shift opus_int) opus_int16 { return a >> shift }
func silk_RSHIFT32(a opus_int32, shift opus_int) opus_int32 { return a >> shift }
func silk_RSHIFT64(a opus_int64, shift opus_int) opus_int64 { return a >> shift }
func silk_RSHIFT(a opus_int32, shift opus_int) opus_int32   { return silk_RSHIFT32(a, shift) }

// silk_LSHIFT_SAT32 — clamp, then shift. Prevents overflow from the
// shift operation.
func silk_LSHIFT_SAT32(a opus_int32, shift opus_int) opus_int32 {
	return silk_LSHIFT32(
		silk_LIMIT_32(a,
			silk_RSHIFT32(silk_int32_MIN, shift),
			silk_RSHIFT32(silk_int32_MAX, shift)),
		shift)
}

// silk_LSHIFT_ovflw — unsigned left shift, reinterpreted as int32.
func silk_LSHIFT_ovflw(a opus_int32, shift opus_int) opus_int32 {
	return opus_int32(opus_uint32(a) << shift)
}
func silk_LSHIFT_uint(a opus_uint32, shift opus_int) opus_uint32 { return a << shift }
func silk_RSHIFT_uint(a opus_uint32, shift opus_int) opus_uint32 { return a >> shift }

// Combined add/sub + shift helpers.
func silk_ADD_LSHIFT(a, b opus_int32, shift opus_int) opus_int32 {
	return a + silk_LSHIFT(b, shift)
}
func silk_ADD_LSHIFT32(a, b opus_int32, shift opus_int) opus_int32 {
	return silk_ADD32(a, silk_LSHIFT32(b, shift))
}
func silk_ADD_LSHIFT_uint(a, b opus_uint32, shift opus_int) opus_uint32 {
	return a + silk_LSHIFT_uint(b, shift)
}
func silk_ADD_RSHIFT(a, b opus_int32, shift opus_int) opus_int32 {
	return a + silk_RSHIFT(b, shift)
}
func silk_ADD_RSHIFT32(a, b opus_int32, shift opus_int) opus_int32 {
	return silk_ADD32(a, silk_RSHIFT32(b, shift))
}
func silk_ADD_RSHIFT_uint(a, b opus_uint32, shift opus_int) opus_uint32 {
	return a + silk_RSHIFT_uint(b, shift)
}
func silk_SUB_LSHIFT32(a, b opus_int32, shift opus_int) opus_int32 {
	return silk_SUB32(a, silk_LSHIFT32(b, shift))
}
func silk_SUB_RSHIFT32(a, b opus_int32, shift opus_int) opus_int32 {
	return silk_SUB32(a, silk_RSHIFT32(b, shift))
}

// silk_RSHIFT_ROUND — right shift with rounding to nearest. Requires
// shift > 0. The shift==1 case uses a different formula to avoid the
// (a >> 0) + 1 overflow on INT_MIN.
func silk_RSHIFT_ROUND(a opus_int32, shift opus_int) opus_int32 {
	if shift == 1 {
		return (a >> 1) + (a & 1)
	}
	return ((a >> (shift - 1)) + 1) >> 1
}
func silk_RSHIFT_ROUND64(a opus_int64, shift opus_int) opus_int64 {
	if shift == 1 {
		return (a >> 1) + (a & 1)
	}
	return ((a >> (shift - 1)) + 1) >> 1
}

// ── min / max / limit / abs / sign ──────────────────────────────────

// silk_min / silk_max — generic for any ordered type, matching the C
// macro's use across int types and silk_float.
func silk_min[T intOrFloat](a, b T) T {
	if a < b {
		return a
	}
	return b
}
func silk_max[T intOrFloat](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// intOrFloat is the type set used by silk_min / silk_max / silk_LIMIT.
// It covers every integer and float type the C macros are ever
// invoked with. (We avoid constraints.Ordered from the stdlib to keep
// the port standalone.)
type intOrFloat interface {
	~opus_int8 | ~opus_int16 | ~opus_int32 | ~opus_int64 |
		~opus_uint8 | ~opus_uint16 | ~opus_uint32 | ~opus_uint64 |
		~int | ~uint | ~float32 | ~float64
}

// silk_min_int / silk_min_16 / silk_min_32 / silk_min_64 — per-type
// variants from SigProc_FIX.h. They exist in C for call-site
// disambiguation; Go generics could replace them but we preserve the
// names so ports transcribe 1:1.
func silk_min_int(a, b opus_int) opus_int    { return silk_min(a, b) }
func silk_min_16(a, b opus_int16) opus_int16 { return silk_min(a, b) }
func silk_min_32(a, b opus_int32) opus_int32 { return silk_min(a, b) }
func silk_min_64(a, b opus_int64) opus_int64 { return silk_min(a, b) }
func silk_max_int(a, b opus_int) opus_int    { return silk_max(a, b) }
func silk_max_16(a, b opus_int16) opus_int16 { return silk_max(a, b) }
func silk_max_32(a, b opus_int32) opus_int32 { return silk_max(a, b) }
func silk_max_64(a, b opus_int64) opus_int64 { return silk_max(a, b) }

// silk_LIMIT — clamp a to [l1, l2] if l1 < l2, otherwise to [l2, l1].
// The C macro's double-conditional handles the swapped-bounds case.
func silk_LIMIT[T intOrFloat](a, limit1, limit2 T) T {
	if limit1 > limit2 {
		if a > limit1 {
			return limit1
		}
		if a < limit2 {
			return limit2
		}
		return a
	}
	if a > limit2 {
		return limit2
	}
	if a < limit1 {
		return limit1
	}
	return a
}
func silk_LIMIT_int(a, limit1, limit2 opus_int) opus_int {
	return silk_LIMIT(a, limit1, limit2)
}
func silk_LIMIT_16(a, limit1, limit2 opus_int16) opus_int16 {
	return silk_LIMIT(a, limit1, limit2)
}
func silk_LIMIT_32(a, limit1, limit2 opus_int32) opus_int32 {
	return silk_LIMIT(a, limit1, limit2)
}

// silk_abs returns abs(a). Returns wrong answer for silk_int*_MIN
// (matches the C comment at SigProc_FIX.h:584).
func silk_abs[T intOrFloat](a T) T {
	if a > 0 {
		return a
	}
	return -a
}

// silk_abs_int — branchless abs. C: (a ^ (a>>sign)) - (a>>sign). On
// our 64-bit platforms `int` is 64 bits, so the sign shift is 63.
func silk_abs_int(a opus_int) opus_int {
	s := a >> 63
	return (a ^ s) - s
}
func silk_abs_int32(a opus_int32) opus_int32 {
	s := a >> 31
	return (a ^ s) - s
}
func silk_abs_int64(a opus_int64) opus_int64 {
	if a > 0 {
		return a
	}
	return -a
}

// silk_sign returns -1, 0, or +1.
func silk_sign[T intOrFloat](a T) opus_int {
	if a > 0 {
		return 1
	}
	if a < 0 {
		return -1
	}
	return 0
}

// ── PRNG ────────────────────────────────────────────────────────────

// LCG used throughout SILK for dither, PLC, and CNG. Deterministic;
// the bit-pattern identical to the C macro's expansion is required
// for bit-exact output.
const (
	RAND_MULTIPLIER opus_int32 = 196314165
	RAND_INCREMENT  opus_int32 = 907633515
)

// silk_RAND(seed) = silk_MLA_ovflw(RAND_INCREMENT, seed, RAND_MULTIPLIER).
func silk_RAND(seed opus_int32) opus_int32 {
	return silk_MLA_ovflw(RAND_INCREMENT, seed, RAND_MULTIPLIER)
}

// silk_SMMUL — signed top-word multiply. The fastest variant on x86.
// C: (opus_int32)silk_RSHIFT64(silk_SMULL((a32), (b32)), 32).
func silk_SMMUL(a32, b32 opus_int32) opus_int32 {
	return opus_int32(silk_RSHIFT64(silk_SMULL(a32, b32), 32))
}

// SILK_FIX_CONST — convert a float constant to its Q-domain integer
// representation. Used throughout SILK to encode tables inline at
// source level: SILK_FIX_CONST(0.25, 16) == (int)(0.25 * 2^16 + 0.5).
func SILK_FIX_CONST(c float64, q opus_int) opus_int32 {
	return opus_int32(c*float64(opus_int64(1)<<q) + 0.5)
}
