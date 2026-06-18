package nativeopus

// Port of libopus/silk/resampler_structs.h + resampler_private.h.

const (
	SILK_RESAMPLER_MAX_FIR_ORDER = 36
	SILK_RESAMPLER_MAX_IIR_ORDER = 6

	RESAMPLER_MAX_BATCH_SIZE_MS = 10
	RESAMPLER_MAX_FS_KHZ        = 48
	RESAMPLER_MAX_BATCH_SIZE_IN = RESAMPLER_MAX_BATCH_SIZE_MS * RESAMPLER_MAX_FS_KHZ
)

// silk_resampler_state_struct — resampler state. The C union sFIR is
// split into two independent arrays here; the two fields are never
// used by the same resampler_function, and init() zeroes the full
// struct on reset, so no aliasing behaviour is required.
type silk_resampler_state_struct struct {
	sIIR               [SILK_RESAMPLER_MAX_IIR_ORDER]opus_int32
	sFIR_i32           [SILK_RESAMPLER_MAX_FIR_ORDER]opus_int32
	sFIR_i16           [SILK_RESAMPLER_MAX_FIR_ORDER]opus_int16
	delayBuf           [96]opus_int16
	resampler_function opus_int
	batchSize          opus_int
	invRatio_Q16       opus_int32
	FIR_Order          opus_int
	FIR_Fracs          opus_int
	Fs_in_kHz          opus_int
	Fs_out_kHz         opus_int
	inputDelay         opus_int
	Coefs              []opus_int16
	// Per-call scratch for silk_resampler_private_IIR_FIR, sized to
	// 2*max batchSize + RESAMPLER_ORDER_FIR_12.
	scratch_buf [2*RESAMPLER_MAX_BATCH_SIZE_IN + 8]opus_int16
}

// Resampler function tags used by silk_resampler_init / silk_resampler.
const (
	USE_silk_resampler_copy                   = 0
	USE_silk_resampler_private_up2_HQ_wrapper = 1
	USE_silk_resampler_private_IIR_FIR        = 2
	USE_silk_resampler_private_down_FIR       = 3
)
