package nativeopus

// Port of libopus/silk/control.h + pitch_est_defines.h.

// ── control.h: decoder API flags ────────────────────────────────────
const (
	FLAG_DECODE_NORMAL = 0
	FLAG_PACKET_LOST   = 1
	FLAG_DECODE_LBRR   = 2
)

// silk_EncControlStruct — C: control.h:42-120.
type silk_EncControlStruct struct {
	nChannelsAPI              opus_int32
	nChannelsInternal         opus_int32
	API_sampleRate            opus_int32
	maxInternalSampleRate     opus_int32
	minInternalSampleRate     opus_int32
	desiredInternalSampleRate opus_int32
	payloadSize_ms            opus_int
	bitRate                   opus_int32
	packetLossPercentage      opus_int
	complexity                opus_int
	useInBandFEC              opus_int
	useDRED                   opus_int
	LBRR_coded                opus_int
	useDTX                    opus_int
	useCBR                    opus_int
	maxBits                   opus_int
	toMono                    opus_int
	opusCanSwitch             opus_int
	reducedDependency         opus_int
	internalSampleRate        opus_int32
	allowBandwidthSwitch      opus_int
	inWBmodeWithoutVariableLP opus_int
	stereoWidth_Q14           opus_int
	switchReady               opus_int
	signalType                opus_int
	offset                    opus_int
}

// silk_DecControlStruct — C: control.h:125-162. ENABLE_OSCE / ENABLE_OSCE_BWE
// are off in our config, so the osce_* fields are omitted.
type silk_DecControlStruct struct {
	nChannelsAPI       opus_int32
	nChannelsInternal  opus_int32
	API_sampleRate     opus_int32
	internalSampleRate opus_int32
	payloadSize_ms     opus_int
	prevPitchLag       opus_int
	enable_deep_plc    opus_int
}

// ── pitch_est_defines.h ─────────────────────────────────────────────
const (
	PE_MAX_FS_KHZ      = 16
	PE_MAX_NB_SUBFR    = 4
	PE_SUBFR_LENGTH_MS = 5

	PE_LTP_MEM_LENGTH_MS = 4 * PE_SUBFR_LENGTH_MS

	PE_MAX_FRAME_LENGTH_MS   = PE_LTP_MEM_LENGTH_MS + PE_MAX_NB_SUBFR*PE_SUBFR_LENGTH_MS
	PE_MAX_FRAME_LENGTH      = PE_MAX_FRAME_LENGTH_MS * PE_MAX_FS_KHZ
	PE_MAX_FRAME_LENGTH_ST_1 = PE_MAX_FRAME_LENGTH >> 2
	PE_MAX_FRAME_LENGTH_ST_2 = PE_MAX_FRAME_LENGTH >> 1

	PE_MAX_LAG_MS = 18
	PE_MIN_LAG_MS = 2
	PE_MAX_LAG    = PE_MAX_LAG_MS * PE_MAX_FS_KHZ
	PE_MIN_LAG    = PE_MIN_LAG_MS * PE_MAX_FS_KHZ

	PE_D_SRCH_LENGTH  = 24
	PE_NB_STAGE3_LAGS = 5

	PE_NB_CBKS_STAGE2     = 3
	PE_NB_CBKS_STAGE2_EXT = 11

	PE_NB_CBKS_STAGE3_MAX = 34
	PE_NB_CBKS_STAGE3_MID = 24
	PE_NB_CBKS_STAGE3_MIN = 16

	PE_NB_CBKS_STAGE3_10MS = 12
	PE_NB_CBKS_STAGE2_10MS = 3

	PE_SHORTLAG_BIAS    = 0.2
	PE_PREVLAG_BIAS     = 0.2
	PE_FLATCONTOUR_BIAS = 0.05

	SILK_PE_MIN_COMPLEX = 0
	SILK_PE_MID_COMPLEX = 1
	SILK_PE_MAX_COMPLEX = 2
)
