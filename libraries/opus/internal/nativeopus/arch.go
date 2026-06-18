package nativeopus

import "math"

// Port of libopus/celt/arch.h.
//
// Only the float path is ported (FIXED_POINT undefined). The fixed-point
// branch of the header — type aliases, SAT16, and the integer-math macro
// set — is intentionally skipped; this matches a standard FLOAT_API build
// of libopus. Pure forwarding / no-op macros from the float branch are
// preserved as Go inline functions with matching names so every call site
// in the rest of the codebase ports 1:1.

// Audio sample types (float build: all six are plain float).
type (
	opus_val16 = float32
	opus_val32 = float32
	opus_val64 = float32

	celt_sig  = float32
	celt_norm = float32
	celt_ener = float32
	celt_glog = float32

	opus_res  = float32
	celt_coef = float32
)

// Scaling / fixed-point-shift constants (float build values).
const (
	CELT_SIG_SCALE = opus_val32(32768.0)

	Q15ONE   = opus_val16(1.0)
	Q31ONE   = opus_val32(1.0)
	COEF_ONE = celt_coef(1.0)

	NORM_SCALING = opus_val32(1.0)

	EPSILON      = opus_val32(1e-15)
	VERY_SMALL   = opus_val32(1e-30)
	VERY_LARGE16 = opus_val16(1e15)
	Q15_ONE      = opus_val16(1.0)

	NORM_SHIFT         = 24
	SIG_SHIFT          = 12
	SIG_SAT            = 536870911
	DB_SHIFT           = 24
	MAX_ENCODING_DEPTH = 24

	GLOBAL_STACK_SIZE = 120000

	// Set this if opus_int64 is a native type of the CPU.
	// All platforms Go runs on today have fast 64-bit types.
	OPUS_FAST_INT64 = 1
)

// Branch-hint helpers. In float builds these are pure identity — the C
// macro only wraps __builtin_expect when available.
func opus_likely(x bool) bool   { return x }
func opus_unlikely(x bool) bool { return x }

// Assertion helpers. libopus's behavior depends on two independent
// compile-time flags:
//
//   - ENABLE_ASSERTIONS or ENABLE_HARDENING enables celt_assert /
//     celt_assert2 — the macro expands to a check that calls
//     celt_fatal() (prints the failed condition and abort()s) on
//     failure. Our vendored config.h sets ENABLE_HARDENING, so these
//     must actually check the condition, matching the C oracle.
//   - ENABLE_ASSERTIONS alone enables celt_sig_assert. Hardening does
//     not enable it, so in our build it stays a no-op.
//
// Go's panic is the correct analogue of celt_fatal — it unwinds with
// a message and, unless recovered, terminates the program.
func celt_assert(cond bool) {
	if !cond {
		panic("celt_assert failed")
	}
}

func celt_assert2(cond bool, message string) {
	if !cond {
		panic("celt_assert failed: " + message)
	}
}

func celt_sig_assert(cond bool) { _ = cond }

// MUST_SUCCEED is deliberately NOT ported as a Go function. The C macro
// expands to `if (call != OPUS_OK) { RESTORE_STACK; return OPUS_INTERNAL_ERROR; }`
// which performs a caller-local return; that control flow cannot be
// captured in a Go helper. Each call site in a ported C function must
// be written explicitly as:
//
//	if err := someCall(); err != OPUS_OK { return OPUS_INTERNAL_ERROR }

// IMUL32 — integer 32×32 → 32 multiply. Matches the C macro `(a)*(b)`.
func IMUL32(a, b opus_int32) opus_int32 { return a * b }

// Min/max helpers. The C header defines one per type category.
func MIN16(a, b opus_val16) opus_val16 {
	if a < b {
		return a
	}
	return b
}
func MAX16(a, b opus_val16) opus_val16 {
	if a > b {
		return a
	}
	return b
}
func MIN32(a, b opus_val32) opus_val32 {
	if a < b {
		return a
	}
	return b
}
func MAX32(a, b opus_val32) opus_val32 {
	if a > b {
		return a
	}
	return b
}
func IMIN(a, b opus_int) opus_int {
	if a < b {
		return a
	}
	return b
}
func IMAX(a, b opus_int) opus_int {
	if a > b {
		return a
	}
	return b
}
func FMIN(a, b opus_val32) opus_val32 {
	if a < b {
		return a
	}
	return b
}
func FMAX(a, b opus_val32) opus_val32 {
	if a > b {
		return a
	}
	return b
}
func MAXG(a, b celt_glog) celt_glog { return MAX32(a, b) }
func MING(a, b celt_glog) celt_glog { return MIN32(a, b) }

// Unsigned 32-bit add/sub (modular wrapping).
func UADD32(a, b opus_uint32) opus_uint32 { return a + b }
func USUB32(a, b opus_uint32) opus_uint32 { return a - b }

// celt_isnan — the float build uses the self-inequality trick. NaN is
// the only IEEE 754 value that is not equal to itself.
func celt_isnan(x opus_val32) int {
	if x != x {
		return 1
	}
	return 0
}

// Absolute value helpers. C: `((float)fabs(x))`. math.Abs widens to
// float64 internally, which is bit-exact for any finite float32 input
// and correctly handles negative zero.
func ABS16(x opus_val16) opus_val16 { return opus_val16(math.Abs(float64(x))) }
func ABS32(x opus_val32) opus_val32 { return opus_val32(math.Abs(float64(x))) }

// Q-constant constructors. In the float build the bit-count argument
// is ignored and the literal is passed through unchanged.
func QCONST16(x opus_val16, bits int) opus_val16 { _ = bits; return x }
func QCONST32(x opus_val32, bits int) opus_val32 { _ = bits; return x }
func GCONST(x celt_glog) celt_glog               { return x }

// Sign-flip and extraction. All identity in the float build.
func NEG16(x opus_val16) opus_val16       { return -x }
func NEG32(x opus_val32) opus_val32       { return -x }
func NEG32_ovflw(x opus_val32) opus_val32 { return -x }
func EXTRACT16(x opus_val32) opus_val16   { return x }
func EXTEND32(x opus_val16) opus_val32    { return x }

// Shifts are no-ops on floats.
func SHR16(a opus_val16, shift int) opus_val16        { _ = shift; return a }
func SHL16(a opus_val16, shift int) opus_val16        { _ = shift; return a }
func SHR32(a opus_val32, shift int) opus_val32        { _ = shift; return a }
func SHL32(a opus_val32, shift int) opus_val32        { _ = shift; return a }
func PSHR32(a opus_val32, shift int) opus_val32       { _ = shift; return a }
func VSHR32(a opus_val32, shift int) opus_val32       { _ = shift; return a }
func SHR64(a opus_val64, shift int) opus_val64        { _ = shift; return a }
func PSHR(a opus_val32, shift int) opus_val32         { _ = shift; return a }
func SHR(a opus_val32, shift int) opus_val32          { _ = shift; return a }
func SHL(a opus_val32, shift int) opus_val32          { _ = shift; return a }
func SHL32_ovflw(a opus_val32, shift int) opus_val32  { _ = shift; return a }
func PSHR32_ovflw(a opus_val32, shift int) opus_val32 { _ = shift; return a }

// Saturation — identity in float mode.
func SATURATE(x opus_val32, a opus_val32) opus_val32 { _ = a; return x }
func SATURATE16(x opus_val32) opus_val16             { return x }

// Rounding — identity in float mode; HALF variants scale by 0.5.
func ROUND16(a opus_val16, shift int) opus_val16  { _ = shift; return a }
func SROUND16(a opus_val32, shift int) opus_val16 { _ = shift; return a }
func HALF16(x opus_val16) opus_val16              { return 0.5 * x }
func HALF32(x opus_val32) opus_val32              { return 0.5 * x }

// Basic arithmetic aliases.
func ADD16(a, b opus_val16) opus_val16       { return a + b }
func SUB16(a, b opus_val16) opus_val16       { return a - b }
func ADD32(a, b opus_val32) opus_val32       { return a + b }
func SUB32(a, b opus_val32) opus_val32       { return a - b }
func ADD32_ovflw(a, b opus_val32) opus_val32 { return a + b }
func SUB32_ovflw(a, b opus_val32) opus_val32 { return a - b }

// Multiply aliases. In the float build every *_Qxx / *_Pxx / MAC
// variant collapses to ordinary floating-point multiply-or-fma.
func MULT16_16_16(a, b opus_val16) opus_val16 { return a * b }
func MULT16_16(a, b opus_val16) opus_val32    { return opus_val32(a) * opus_val32(b) }
func MAC16_16(c opus_val32, a, b opus_val16) opus_val32 {
	return c + opus_val32(a)*opus_val32(b)
}

func MULT16_32_Q15(a opus_val16, b opus_val32) opus_val32 { return opus_val32(a) * b }
func MULT16_32_Q16(a opus_val16, b opus_val32) opus_val32 { return opus_val32(a) * b }

func MULT32_32_Q16(a, b opus_val32) opus_val32       { return a * b }
func MULT32_32_Q31(a, b opus_val32) opus_val32       { return a * b }
func MULT32_32_P31(a, b opus_val32) opus_val32       { return a * b }
func MULT32_32_P31_ovflw(a, b opus_val32) opus_val32 { return a * b }

func MAC16_32_Q15(c opus_val32, a opus_val16, b opus_val32) opus_val32 {
	return c + opus_val32(a)*b
}
func MAC16_32_Q16(c opus_val32, a opus_val16, b opus_val32) opus_val32 {
	return c + opus_val32(a)*b
}
func MAC_COEF_32_ARM(c opus_val32, a opus_val16, b opus_val32) opus_val32 {
	return c + opus_val32(a)*b
}

func MULT16_16_Q11_32(a, b opus_val16) opus_val32         { return opus_val32(a) * opus_val32(b) }
func MULT16_16_Q11(a, b opus_val16) opus_val32            { return opus_val32(a) * opus_val32(b) }
func MULT16_16_Q13(a, b opus_val16) opus_val32            { return opus_val32(a) * opus_val32(b) }
func MULT16_16_Q14(a, b opus_val16) opus_val32            { return opus_val32(a) * opus_val32(b) }
func MULT16_16_Q15(a, b opus_val16) opus_val32            { return opus_val32(a) * opus_val32(b) }
func MULT16_16_P15(a, b opus_val16) opus_val32            { return opus_val32(a) * opus_val32(b) }
func MULT16_16_P13(a, b opus_val16) opus_val32            { return opus_val32(a) * opus_val32(b) }
func MULT16_16_P14(a, b opus_val16) opus_val32            { return opus_val32(a) * opus_val32(b) }
func MULT16_32_P16(a opus_val16, b opus_val32) opus_val32 { return opus_val32(a) * b }

func MULT_COEF_32(a celt_coef, b opus_val32) opus_val32   { return opus_val32(a) * b }
func MULT_COEF(a celt_coef, b opus_val16) opus_val16      { return a * b }
func MULT_COEF_TAPS(a celt_coef, b opus_val16) opus_val16 { return a * b }
func COEF2VAL16(x celt_coef) opus_val16                   { return x }

// DIV32 / DIV32_16 — C: `((opus_val32)(a))/(opus_val32)(b)`.
func DIV32_16(a opus_val32, b opus_val16) opus_val32 { return a / opus_val32(b) }
func DIV32(a, b opus_val32) opus_val32               { return a / b }

// Resolution/signal/int16 conversions (float build).
// FLOAT2INT16 / float2int live in float_cast.h and land in that port.
func SIG2RES(a celt_sig) opus_res  { return opus_res(1.0/float32(CELT_SIG_SCALE)) * opus_res(a) }
func RES2FLOAT(a opus_res) float32 { return float32(a) }
func INT16TORES(a opus_int16) opus_res {
	return opus_res(float32(a) * (1.0 / float32(CELT_SIG_SCALE)))
}
func INT24TORES(a opus_int32) opus_res {
	return opus_res(float32(a) * (1.0 / 32768.0 / 256.0))
}
func ADD_RES(a, b opus_res) opus_res { return ADD32(a, b) }
func FLOAT2RES(a float32) opus_res   { return opus_res(a) }
func RES2SIG(a opus_res) celt_sig    { return celt_sig(CELT_SIG_SCALE * opus_val32(a)) }
func MULT16_RES_Q15(a opus_val16, b opus_res) opus_res {
	return opus_res(MULT16_16_Q15(a, opus_val16(b)))
}
func RES2VAL16(a opus_res) opus_val16  { return opus_val16(a) }
func FLOAT2SIG(a float32) celt_sig     { return celt_sig(a * float32(CELT_SIG_SCALE)) }
func INT16TOSIG(a opus_int16) celt_sig { return celt_sig(a) }
func INT24TOSIG(a opus_int32) celt_sig { return celt_sig(float32(a) * (1.0 / 256.0)) }
