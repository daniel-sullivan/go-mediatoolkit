package nativeopus

// Port of libopus/silk/macros.h — SILK's Q-format multiply helpers and
// a couple of bit-count primitives.
//
// Our build sets OPUS_FAST_INT64, so every SMUL*/SMLA* variant takes
// the 64-bit multiply path (widens one operand to opus_int64, does
// the mul, shifts, casts back). The 32-bit-only alternate branch
// (narrowed word splits) is skipped.
//
// Naming follows the C convention: 'W' = wide (shift-by-16 applied),
// 'B' = low-bottom-16-bit operand, 'T' = top-16-bit operand,
// 'M'/'L' = multiply variants. The SM{U|L}A pairs differ by the lead
// letter: SMUL = straight product, SMLA = multiply-accumulate.

// silk_SMULWB = (a32 * (int16)b32) >> 16, 32-bit result.
// C: ((opus_int32)(((a32) * (opus_int64)((opus_int16)(b32))) >> 16))
func silk_SMULWB(a32, b32 opus_int32) opus_int32 {
	return opus_int32((opus_int64(a32) * opus_int64(opus_int16(b32))) >> 16)
}

// silk_SMLAWB = a32 + ((b32 * (int16)c32) >> 16).
func silk_SMLAWB(a32, b32, c32 opus_int32) opus_int32 {
	return opus_int32(opus_int64(a32) + (opus_int64(b32)*opus_int64(opus_int16(c32)))>>16)
}

// silk_SMULWT = (a32 * (b32 >> 16)) >> 16.
func silk_SMULWT(a32, b32 opus_int32) opus_int32 {
	return opus_int32((opus_int64(a32) * opus_int64(b32>>16)) >> 16)
}

// silk_SMLAWT = a32 + ((b32 * (c32 >> 16)) >> 16).
func silk_SMLAWT(a32, b32, c32 opus_int32) opus_int32 {
	return opus_int32(opus_int64(a32) + (opus_int64(b32)*opus_int64(c32>>16))>>16)
}

// silk_SMULBB = (int16)a32 * (int16)b32, 32-bit result. Both operands
// are sign-extended from their low 16 bits to int32 before multiply.
func silk_SMULBB(a32, b32 opus_int32) opus_int32 {
	return opus_int32(opus_int16(a32)) * opus_int32(opus_int16(b32))
}

// silk_SMLABB = a32 + (int16)b32 * (int16)c32.
func silk_SMLABB(a32, b32, c32 opus_int32) opus_int32 {
	return a32 + opus_int32(opus_int16(b32))*opus_int32(opus_int16(c32))
}

// silk_SMULBT = (int16)a32 * (b32 >> 16).
func silk_SMULBT(a32, b32 opus_int32) opus_int32 {
	return opus_int32(opus_int16(a32)) * (b32 >> 16)
}

// silk_SMLABT = a32 + (int16)b32 * (c32 >> 16).
func silk_SMLABT(a32, b32, c32 opus_int32) opus_int32 {
	return a32 + opus_int32(opus_int16(b32))*(c32>>16)
}

// silk_SMLAL = a64 + (int64)b32 * (int64)c32.
// C: silk_ADD64((a64), ((opus_int64)(b32) * (opus_int64)(c32))).
func silk_SMLAL(a64 opus_int64, b32, c32 opus_int32) opus_int64 {
	return silk_ADD64(a64, opus_int64(b32)*opus_int64(c32))
}

// silk_SMULWW = (a32 * b32) >> 16.
func silk_SMULWW(a32, b32 opus_int32) opus_int32 {
	return opus_int32((opus_int64(a32) * opus_int64(b32)) >> 16)
}

// silk_SMLAWW = a32 + ((b32 * c32) >> 16).
func silk_SMLAWW(a32, b32, c32 opus_int32) opus_int32 {
	return opus_int32(opus_int64(a32) + (opus_int64(b32)*opus_int64(c32))>>16)
}

// silk_CLZ16 — count leading zeros in a 16-bit value.
// C: 32 - EC_ILOG(in16<<16 | 0x8000). The `| 0x8000` guarantees a
// non-zero argument so EC_ILOG is well-defined.
func silk_CLZ16(in16 opus_int16) opus_int32 {
	return opus_int32(32 - ec_ilog(opus_uint32(opus_int32(in16)<<16)|0x8000))
}

// silk_CLZ32 — count leading zeros in a 32-bit value. C: the `?:`
// handles the in32==0 case where EC_ILOG would return 0 (so 32-0=32
// is the correct count).
func silk_CLZ32(in32 opus_int32) opus_int32 {
	if in32 == 0 {
		return 32
	}
	return opus_int32(32 - ec_ilog(opus_uint32(in32)))
}

// Matrix indexing helpers. C macros take a base pointer plus row,
// column, dim and compute an offset. In Go we express this as slice
// index helpers returning both the element (matrix_ptr's dereference
// semantics) and the subslice starting at the position (matrix_adr).
// Generic so they port across any element type used in SILK.

// matrix_ptr returns *(base + row*N + column) — dereference form.
func matrix_ptr[T any](base []T, row, column, N opus_int) T {
	return base[row*N+column]
}

// matrix_adr returns base[row*N + column:] — address form. Callers
// that need a mutable element can index the returned slice at [0] or
// assign via matrix_ptr-equivalent indexing on the original slice.
func matrix_adr[T any](base []T, row, column, N opus_int) []T {
	return base[row*N+column:]
}

// matrix_c_ptr — column-major variant from macros.h. *(base + row + M*column).
func matrix_c_ptr[T any](base []T, row, column, M opus_int) T {
	return base[row+M*column]
}
