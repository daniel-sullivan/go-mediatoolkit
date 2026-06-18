package nativeopus

import "math/bits"

// Port of libopus/celt/entcode.h + entcode.c.
//
// The range-coder state struct ec_ctx is the shared heart of both the
// encoder and decoder; libopus typedefs ec_enc and ec_dec as aliases
// of it so common helpers like ec_tell() work on either role. Go
// aliases give us the same semantics.
//
// EC_CLZ / EC_ILOG: the vendored headers pick up __builtin_clz via
// ecintrin.h at -O2 and compile EC_ILOG into a macro. We route the
// whole codebase through ec_ilog() here, backed by math/bits.Len32 —
// which returns the same value as __builtin_clz-derived EC_ILOG for
// every opus_uint32 input including 0 (EC_ILOG(0)=0, matching the
// careful branchless fallback in entcode.c:41-62).

// ec_window holds the bits pending at the end of the bit-reversed
// raw-bits buffer. C: `typedef opus_uint32 ec_window`. Using
// opus_uint64 here would be permitted by the C comment ("OPT: can be
// wider for speed"), but we keep 32 to match the oracle byte-for-byte.
type ec_window = opus_uint32

const (
	// EC_WINDOW_SIZE — bits per ec_window.
	EC_WINDOW_SIZE = 32
	// EC_UINT_BITS — number of bits of the range-coded part of
	// unsigned-integer encoding.
	EC_UINT_BITS = 8
	// BITRES — resolution of fractional-precision bit usage
	// measurements: 3 => 1/8th bits.
	BITRES = 3
)

// ec_ctx is the entropy coder state. The same struct serves both
// encoder and decoder; ec_enc / ec_dec below are aliases matching
// libopus's `typedef struct ec_ctx ec_enc;` etc.
//
// C: struct ec_ctx at entcode.h:62-91.
type ec_ctx struct {
	// Buffered input/output. C has a plain `unsigned char *buf` with
	// length carried in `storage`; we keep the separate storage field
	// so every C access pattern (`_this->buf[_this->storage-1-...]`,
	// `_this->buf[_this->offs++]`, etc.) ports 1:1 via indexed access
	// on the slice.
	buf []byte

	// Size of the buffer (always equals len(buf); duplicated to match
	// the C layout and all the arithmetic that reads it as uint32).
	storage opus_uint32

	// Offset at which the last byte containing raw bits was
	// read/written (counted from the *end* of buf).
	end_offs opus_uint32

	// Bits that will be read from/written at the end.
	end_window ec_window

	// Number of valid bits in end_window.
	nend_bits int

	// Total number of whole bits read/written. Excludes partial bits
	// currently in the range coder.
	nbits_total int

	// Offset at which the next range-coder byte will be read/written.
	offs opus_uint32

	// Number of values in the current range.
	rng opus_uint32

	// Decoder: difference between top of current range and input value
	// minus one. Encoder: low end of current range.
	val opus_uint32

	// Decoder: saved normalization factor from ec_decode(). Encoder:
	// number of outstanding carry-propagating symbols.
	ext opus_uint32

	// Buffered input/output symbol awaiting carry propagation. -1 when
	// no byte is pending — callers must explicitly initialise to -1
	// (C fields start zeroed but the encoder constructor writes -1).
	rem int

	// Nonzero if an error occurred.
	error int
}

// ec_enc and ec_dec are aliases of ec_ctx. Matches libopus's typedef.
type (
	ec_enc = ec_ctx
	ec_dec = ec_ctx
)

// ec_ilog returns the one-based bit position of the highest set bit of
// v, or 0 for v==0. Equivalent to libopus's EC_ILOG(v) macro and to
// the branchless fallback ec_ilog() in entcode.c:41-62.
//
// Mathops's isqrt32 and silk_macros's silk_CLZ{16,32} previously used
// math/bits.Len32 directly as an interim shim; now that ec_ilog exists
// both will be migrated to route through this function.
func ec_ilog(v opus_uint32) int {
	return bits.Len32(uint32(v))
}

// ec_range_bytes — number of bytes of range-coded output produced so
// far (or consumed, in the decoder).
func ec_range_bytes(_this *ec_ctx) opus_uint32 { return _this.offs }

// ec_get_buffer — the backing byte slice. Matches the C getter.
func ec_get_buffer(_this *ec_ctx) []byte { return _this.buf }

// ec_get_error — nonzero iff an error has occurred.
func ec_get_error(_this *ec_ctx) int { return _this.error }

// ec_tell returns the number of bits "used" so far. Computed the same
// way in encoder and decoder, so it is suitable for coding-decision
// branches. Always slightly overestimates (rounding error is in the
// positive direction).
//
// C: nbits_total - EC_ILOG(rng).
func ec_tell(_this *ec_ctx) int {
	return _this.nbits_total - ec_ilog(_this.rng)
}

// ec_tell_frac — same as ec_tell but scaled by 2^BITRES (i.e., result
// is in 1/8th-bit units). Uses the fast linear-plus-correction-table
// approximation from entcode.c:65-84 (the `#if 1` branch); the
// alternative loop-based exact version in the `#else` is not compiled
// in any libopus build.
func ec_tell_frac(_this *ec_ctx) opus_uint32 {
	var correction = [8]uint32{
		35733, 38967, 42495, 46340,
		50535, 55109, 60097, 65535,
	}
	nbits := opus_uint32(_this.nbits_total) << BITRES
	l := opus_int32(ec_ilog(_this.rng))
	r := _this.rng >> (l - 16)
	b := (r >> 12) - 8
	if r > opus_uint32(correction[b]) {
		b++
	}
	l = (l << 3) + opus_int32(b)
	return nbits - opus_uint32(l)
}

// celt_udiv — unsigned 32-bit division. C uses a small LUT path when
// USE_SMALL_DIV_TABLE is defined (OPUS_ARM_ASM only); our build skips
// that, so the macro collapses to `n/d`.
func celt_udiv(n, d opus_uint32) opus_uint32 {
	celt_sig_assert(d > 0)
	return n / d
}

// celt_sudiv — signed 32-bit division with the same macro behaviour.
func celt_sudiv(n, d opus_int32) opus_int32 {
	celt_sig_assert(d > 0)
	return n / d
}
