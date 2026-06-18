package nativeflac

import "math/bits"

// 1:1 port of libflac/src/libFLAC/bitmath.c plus the inline helpers in
// libflac/src/libFLAC/include/private/bitmath.h.
//
// libFLAC bottoms out on __builtin_clz / _BitScanReverse intrinsics on
// real compilers and a de Bruijn-table fallback on others. Both
// produce the same numeric output for v > 0 — Go's math/bits.Len32 /
// Len64 give us that result directly (Len returns "smallest n such
// that v < 2^n", so ilog2 = Len-1 for v > 0).

// ILog2 — port of FLAC__bitmath_ilog2 (bitmath.h:156).
//
// Returns the index of the highest set bit of v. v MUST be > 0; the C
// version asserts and would otherwise return a garbage value via
// __builtin_clz. The Go port returns 0 for v == 0 to keep callers
// from triggering an out-of-range shift on accident.
func ILog2(v uint32) uint32 {
	if v == 0 {
		return 0
	}
	return uint32(bits.Len32(v)) - 1
}

// ILog2Wide — port of FLAC__bitmath_ilog2_wide (bitmath.h:172).
func ILog2Wide(v uint64) uint32 {
	if v == 0 {
		return 0
	}
	return uint32(bits.Len64(v)) - 1
}

// SILog2 — port of FLAC__bitmath_silog2 (bitmath.c:63). Returns the
// number of bits needed to represent v as a signed two's-complement
// integer; 0 for v == 0; 2 for v == -1.
func SILog2(v int64) uint32 {
	if v == 0 {
		return 0
	}
	if v == -1 {
		return 2
	}
	if v < 0 {
		v = -(v + 1)
	}
	return ILog2Wide(uint64(v)) + 2
}

// ExtraMulbitsUnsigned — port of FLAC__bitmath_extra_mulbits_unsigned
// (bitmath.c:103). Returns ceil(log2(v)) — i.e. how many bits a
// multiply by v adds to a value's storage requirement.
func ExtraMulbitsUnsigned(v uint32) uint32 {
	if v == 0 {
		return 0
	}
	il := ILog2(v)
	// v is a power of two iff dropping the low bits and shifting back
	// reproduces v.
	if (v>>il)<<il == v {
		return il
	}
	return il + 1
}
