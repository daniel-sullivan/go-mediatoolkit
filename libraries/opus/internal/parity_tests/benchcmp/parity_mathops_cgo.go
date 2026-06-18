//go:build cgo

package benchcmp

/*
// config.h MUST be included before any libopus header. It sets
// FLOAT_APPROX, ENABLE_HARDENING, and other macros that gate which
// code paths the static inline functions in the headers expand to.
// Without it, celt_exp2 / celt_log2 fall back to libm-wrapped forms,
// and the parity test ends up comparing the Go polynomial port
// against the wrong C oracle.
#include "config.h"

// FMA contraction is left enabled on both sides. Go's arm64 backend
// auto-fuses `a*b + c` into FMADDS; Apple clang at -O2 does the same.
// The Go port uses explicit fma_add / fma_sub / fma_rsub helpers
// (see internal/libopus/fma.go) to force consistent fusion at every
// site clang would fuse, so the two compilers produce matching
// FMADDS / FMSUBS / FNMSUBS sequences bit-for-bit.

#include "mathops.h"
#include "float_cast.h"

// Thin wrappers: the C entry points are static inline in headers, so
// they need a non-static caller per translation unit.
static float c_celt_log2(float x)       { return celt_log2(x); }
static float c_celt_exp2(float x)       { return celt_exp2(x); }
static float c_celt_cos_norm2(float x)  { return celt_cos_norm2(x); }
static float c_celt_atan_norm(float x)  { return celt_atan_norm(x); }
static float c_celt_atan2p_norm(float y, float x) { return celt_atan2p_norm(y, x); }
static float c_fast_atan2f(float y, float x)      { return fast_atan2f(y, x); }
static short c_FLOAT2INT16(float x)     { return FLOAT2INT16(x); }
static int   c_float2int(float x)       { return float2int(x); }
static unsigned c_isqrt32(unsigned v)   { return isqrt32(v); }

static void c_celt_float2int16_c(const float *in, short *out, int n) {
    celt_float2int16_c(in, out, n);
}
static int c_opus_limit2_checkwithin1_c(float *samples, int n) {
    return opus_limit2_checkwithin1_c(samples, n);
}
*/
import "C"
import "unsafe"

// These are package-level functions rather than methods so they can be
// reused by any parity_*_test.go file. They do not reach into Go's
// libopus package — the test files call the Go versions directly and
// these call the C versions — equality is checked at the test layer.

func cCeltLog2(x float32) float32     { return float32(C.c_celt_log2(C.float(x))) }
func cCeltExp2(x float32) float32     { return float32(C.c_celt_exp2(C.float(x))) }
func cCeltCosNorm2(x float32) float32 { return float32(C.c_celt_cos_norm2(C.float(x))) }
func cCeltAtanNorm(x float32) float32 { return float32(C.c_celt_atan_norm(C.float(x))) }
func cCeltAtan2pNorm(y, x float32) float32 {
	return float32(C.c_celt_atan2p_norm(C.float(y), C.float(x)))
}
func cFastAtan2f(y, x float32) float32 { return float32(C.c_fast_atan2f(C.float(y), C.float(x))) }
func cFloat2Int16(x float32) int16     { return int16(C.c_FLOAT2INT16(C.float(x))) }
func cFloat2Int(x float32) int32       { return int32(C.c_float2int(C.float(x))) }
func cIsqrt32(v uint32) uint32         { return uint32(C.c_isqrt32(C.unsigned(v))) }

func cCeltFloat2int16C(in []float32, out []int16) {
	if len(in) == 0 {
		return
	}
	C.c_celt_float2int16_c(
		(*C.float)(unsafe.Pointer(&in[0])),
		(*C.short)(unsafe.Pointer(&out[0])),
		C.int(len(in)))
}

func cOpusLimit2CheckWithin1C(samples []float32) int {
	if len(samples) == 0 {
		return int(C.c_opus_limit2_checkwithin1_c(nil, 0))
	}
	return int(C.c_opus_limit2_checkwithin1_c(
		(*C.float)(unsafe.Pointer(&samples[0])),
		C.int(len(samples))))
}
