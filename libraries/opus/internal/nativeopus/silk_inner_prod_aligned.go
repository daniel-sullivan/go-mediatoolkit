package nativeopus

// Port of libopus/silk/inner_prod_aligned.c.

// silk_inner_prod_aligned_scale — inner product of two int16 vectors,
// accumulated with a per-sample right shift.
func silk_inner_prod_aligned_scale(inVec1, inVec2 []opus_int16, scale, len_ opus_int) opus_int32 {
	var sum opus_int32
	for i := opus_int(0); i < len_; i++ {
		sum = silk_ADD_RSHIFT32(sum, silk_SMULBB(opus_int32(inVec1[i]), opus_int32(inVec2[i])), scale)
	}
	return sum
}

// silk_inner_prod16_c — int16 inner product accumulated as int64.
// Declared in SigProc_FIX.h but the C implementation lives in a
// separate compilation unit (pitch helpers); port the straightforward
// version here. Used via the silk_inner_prod16 macro.
func silk_inner_prod16_c(inVec1, inVec2 []opus_int16, len_ opus_int) opus_int64 {
	var sum opus_int64
	for i := opus_int(0); i < len_; i++ {
		sum = silk_SMLALBB(sum, inVec1[i], inVec2[i])
	}
	return sum
}
