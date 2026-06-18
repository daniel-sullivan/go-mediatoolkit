package nativeopus

// Port of libopus/celt/mfrngcod.h — the shared constants the range
// encoder/decoder use to configure their state registers.

const (
	// EC_SYM_BITS — bits to output at a time (bytes).
	EC_SYM_BITS = 8
	// EC_CODE_BITS — total bits in each of the state registers.
	EC_CODE_BITS = 32
	// EC_SYM_MAX — maximum symbol value.
	EC_SYM_MAX opus_uint32 = (1 << EC_SYM_BITS) - 1
	// EC_CODE_SHIFT — bits to shift to move a symbol into the high-
	// order position.
	EC_CODE_SHIFT = EC_CODE_BITS - EC_SYM_BITS - 1
	// EC_CODE_TOP — carry bit of the high-order range symbol.
	EC_CODE_TOP opus_uint32 = 1 << (EC_CODE_BITS - 1)
	// EC_CODE_BOT — low-order bit of the high-order range symbol.
	EC_CODE_BOT opus_uint32 = EC_CODE_TOP >> EC_SYM_BITS
	// EC_CODE_EXTRA — bits available for the last, partial symbol in
	// the code field.
	EC_CODE_EXTRA = ((EC_CODE_BITS-2)%EC_SYM_BITS + 1)
)

// EC_MINI — C: ecintrin.h:46, branchless integer min. In Go the plain
// conditional compiles just as tightly on arm64 (CSEL) and gives the
// same bit-for-bit result, so we keep it readable. `unsigned` in C is
// at least 16 bits but ports as int in our usage because ec_decode's
// return type is `unsigned`; mapping to opus_uint32 matches the
// widest consumer.
func EC_MINI(a, b opus_uint32) opus_uint32 {
	if b < a {
		return b
	}
	return a
}
