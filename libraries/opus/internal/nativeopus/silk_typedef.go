package nativeopus

// Port of libopus/silk/typedef.h.
//
// The C header defines SILK's integer limit constants, the silk_float
// alias (used only in the !FIXED_POINT branch; our build matches), and
// the silk_assert macro. Our config sets ENABLE_HARDENING, which also
// enables SILK assertions (the C header keys off ENABLE_ASSERTIONS
// specifically — but for porting purposes we follow arch.go's policy
// and make silk_assert a real check). The fatal helper becomes a Go
// panic.

// silk_float mirrors `#define silk_float float` in the !FIXED_POINT
// branch. Kept as an alias so every silk/float/*.c port reads 1:1.
type silk_float = float32

// silk_float_MAX — FLT_MAX. In Go this is math.MaxFloat32.
const silk_float_MAX = 3.40282346638528859811704183484516925440e+38

// SILK integer range constants. The C macros cast to the matching
// opus_intN type; Go's type-checker infers from the typed assignment
// so explicit casts at use-sites are not required.
const (
	silk_int64_MAX opus_int64 = 0x7FFFFFFFFFFFFFFF
	silk_int64_MIN opus_int64 = -0x8000000000000000
	silk_int32_MAX opus_int32 = 0x7FFFFFFF
	silk_int32_MIN opus_int32 = -0x80000000
	silk_int16_MAX opus_int16 = 0x7FFF
	silk_int16_MIN opus_int16 = -0x8000
	silk_int8_MAX  opus_int8  = 0x7F
	silk_int8_MIN  opus_int8  = -0x80
	silk_uint8_MAX opus_uint8 = 0xFF
)

// silk_TRUE / silk_FALSE are integer constants in C. Go has a real
// bool type, but the C source uses these as `opus_int` sentinels (e.g.
// the `forEnc` argument to silk_resampler_init). Keeping them as
// typed ints makes the ports compile as-is.
const (
	silk_TRUE  opus_int = 1
	silk_FALSE opus_int = 0
)

// silk_assert panics if cond is false. The C macro is a no-op unless
// ENABLE_ASSERTIONS is set, which our config.h does not set directly
// — but the same ENABLE_HARDENING policy that makes celt_assert
// active applies here, and catching SILK invariant violations in the
// Go port matters for debugging the 1:1 transcription.
//
// C: silk_assert(COND) {if (!(COND)) {silk_fatal("assertion failed: " #COND);}}
func silk_assert(cond bool) {
	if !cond {
		panic("silk_assert failed")
	}
}
