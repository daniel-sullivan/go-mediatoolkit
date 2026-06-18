// Package libopus is a literal 1:1 Go port of the vendored C libopus
// reference implementation at libraries/opus/libopus/. Identifiers,
// struct field layouts, and file organization mirror the C source so
// that every Go symbol can be trivially cross-referenced against its C
// counterpart. Bit-exact parity with the C implementation is the
// gating criterion for this package — Go-specific optimizations come
// only after parity is verified end-to-end.
package nativeopus

// Port of libopus/include/opus_types.h.
//
// The C header selects between several platform-specific typedef
// blocks. The vendored build defines HAVE_CONFIG_H so the stdint.h
// path is taken, giving the fixed-width aliases below. The two
// #define-based identifiers (opus_int, opus_uint) map to C int and
// unsigned int, which on every platform we target are at least 32 bits
// and functionally interchangeable with Go's int / uint.
//
// All are declared as type *aliases* (`type T = U`), matching C
// typedef semantics: T and U are fully interchangeable with no
// explicit conversion required.

type (
	opus_int8   = int8
	opus_uint8  = uint8
	opus_int16  = int16
	opus_uint16 = uint16
	opus_int32  = int32
	opus_uint32 = uint32
	opus_int64  = int64
	opus_uint64 = uint64

	// opus_int is used for counters etc; at least 16 bits in C. Go's
	// int is ≥32 bits on every platform we target.
	opus_int  = int
	opus_uint = uint
)
