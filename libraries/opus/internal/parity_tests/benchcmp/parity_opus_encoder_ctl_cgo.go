//go:build cgo

package benchcmp

/*
#include <opus.h>
#include <stdint.h>
#include <string.h>

// Mirror of the c_enc_snapshot layout in parity_opus_encoder_base_cgo.go.
// Each .go file's cgo preamble is compiled as its own TU, so we repeat
// the struct here. Layout MUST match 1:1.
struct c_enc_snapshot {
    int32_t celt_enc_offset;
    int32_t silk_enc_offset;
    int32_t application;
    int32_t channels;
    int32_t delay_compensation;
    int32_t force_channels;
    int32_t signal_type;
    int32_t user_bandwidth;
    int32_t max_bandwidth;
    int32_t user_forced_mode;
    int32_t voice_ratio;
    int32_t Fs;
    int32_t use_vbr;
    int32_t vbr_constraint;
    int32_t variable_duration;
    int32_t bitrate_bps;
    int32_t user_bitrate_bps;
    int32_t lsb_depth;
    int32_t encoder_buffer;
    int32_t lfe;
    int32_t arch;
    int32_t use_dtx;
    int32_t fec_config;

    int32_t silk_nChannelsAPI;
    int32_t silk_nChannelsInternal;
    int32_t silk_API_sampleRate;
    int32_t silk_maxInternalSampleRate;
    int32_t silk_minInternalSampleRate;
    int32_t silk_desiredInternalSampleRate;
    int32_t silk_payloadSize_ms;
    int32_t silk_bitRate;
    int32_t silk_packetLossPercentage;
    int32_t silk_complexity;
    int32_t silk_useInBandFEC;
    int32_t silk_useDTX;
    int32_t silk_useCBR;
    int32_t silk_reducedDependency;

    int32_t stream_channels;
    int32_t hybrid_stereo_width_Q14;
    int32_t variable_HP_smth2_Q15;
    uint32_t prev_HB_gain_bits;
    int32_t mode;
    int32_t prev_mode;
    int32_t prev_channels;
    int32_t prev_framesize;
    int32_t bandwidth;
    int32_t first;
    int32_t nb_no_activity_ms_Q1;
};

// Forward decls — implementations live in libraries/opus/opus_cgo_src.c.
struct opus_test_enc_snapshot;

extern int opus_test_encoder_ctl_set_and_snapshot(int32_t Fs, int channels, int application,
                                                  int request, int32_t value,
                                                  struct opus_test_enc_snapshot *snap,
                                                  int *initRet);
extern int opus_test_encoder_ctl_get_i32(int32_t Fs, int channels, int application,
                                         int preSetRequest, int32_t preSetValue,
                                         int getRequest,
                                         int *initRet, int *preSetRet,
                                         int32_t *outValue);
extern int opus_test_encoder_ctl_get_u32(int32_t Fs, int channels, int application,
                                         int preSetRequest, int32_t preSetValue,
                                         int getRequest,
                                         int *initRet, int *preSetRet,
                                         uint32_t *outValue);

// Thin wrappers that cast the snapshot struct.
// The struct layout is defined below in this translation unit as a
// duplicate of opus_test_enc_snapshot (see parity_opus_encoder_base_cgo.go
// for the canonical definition of c_enc_snapshot).
static int c_opus_encoder_ctl_set_and_snapshot(int32_t Fs, int channels, int application,
                                               int request, int32_t value,
                                               struct c_enc_snapshot *snap,
                                               int *initRet) {
    return opus_test_encoder_ctl_set_and_snapshot(Fs, channels, application,
                                                  request, value,
                                                  (struct opus_test_enc_snapshot*)snap,
                                                  initRet);
}

static int c_opus_encoder_ctl_get_i32(int32_t Fs, int channels, int application,
                                      int preSetRequest, int32_t preSetValue,
                                      int getRequest,
                                      int *initRet, int *preSetRet,
                                      int32_t *outValue) {
    return opus_test_encoder_ctl_get_i32(Fs, channels, application,
                                         preSetRequest, preSetValue, getRequest,
                                         initRet, preSetRet, outValue);
}

static int c_opus_encoder_ctl_get_u32(int32_t Fs, int channels, int application,
                                      int preSetRequest, int32_t preSetValue,
                                      int getRequest,
                                      int *initRet, int *preSetRet,
                                      uint32_t *outValue) {
    return opus_test_encoder_ctl_get_u32(Fs, channels, application,
                                         preSetRequest, preSetValue, getRequest,
                                         initRet, preSetRet, outValue);
}
*/
import "C"
import "unsafe"

func cOpusEncoderCtlSetAndSnapshot(Fs int32, channels, application, request int, value int32) (int, int, cEncSnapshot) {
	var snap C.struct_c_enc_snapshot
	var initRet C.int
	ctlRet := int(C.c_opus_encoder_ctl_set_and_snapshot(
		C.int32_t(Fs), C.int(channels), C.int(application),
		C.int(request), C.int32_t(value),
		(*C.struct_c_enc_snapshot)(unsafe.Pointer(&snap)),
		&initRet))
	out := cEncSnapshot{
		CeltEncOffset:     int32(snap.celt_enc_offset),
		SilkEncOffset:     int32(snap.silk_enc_offset),
		Application:       int32(snap.application),
		Channels:          int32(snap.channels),
		DelayCompensation: int32(snap.delay_compensation),
		ForceChannels:     int32(snap.force_channels),
		SignalType:        int32(snap.signal_type),
		UserBandwidth:     int32(snap.user_bandwidth),
		MaxBandwidth:      int32(snap.max_bandwidth),
		UserForcedMode:    int32(snap.user_forced_mode),
		VoiceRatio:        int32(snap.voice_ratio),
		Fs:                int32(snap.Fs),
		UseVBR:            int32(snap.use_vbr),
		VBRConstraint:     int32(snap.vbr_constraint),
		VariableDuration:  int32(snap.variable_duration),
		BitrateBps:        int32(snap.bitrate_bps),
		UserBitrateBps:    int32(snap.user_bitrate_bps),
		LsbDepth:          int32(snap.lsb_depth),
		EncoderBuffer:     int32(snap.encoder_buffer),
		Lfe:               int32(snap.lfe),
		Arch:              int32(snap.arch),
		UseDTX:            int32(snap.use_dtx),
		FecConfig:         int32(snap.fec_config),

		SilkNChannelsAPI:              int32(snap.silk_nChannelsAPI),
		SilkNChannelsInternal:         int32(snap.silk_nChannelsInternal),
		SilkAPISampleRate:             int32(snap.silk_API_sampleRate),
		SilkMaxInternalSampleRate:     int32(snap.silk_maxInternalSampleRate),
		SilkMinInternalSampleRate:     int32(snap.silk_minInternalSampleRate),
		SilkDesiredInternalSampleRate: int32(snap.silk_desiredInternalSampleRate),
		SilkPayloadSizeMs:             int32(snap.silk_payloadSize_ms),
		SilkBitRate:                   int32(snap.silk_bitRate),
		SilkPacketLossPercentage:      int32(snap.silk_packetLossPercentage),
		SilkComplexity:                int32(snap.silk_complexity),
		SilkUseInBandFEC:              int32(snap.silk_useInBandFEC),
		SilkUseDTX:                    int32(snap.silk_useDTX),
		SilkUseCBR:                    int32(snap.silk_useCBR),
		SilkReducedDependency:         int32(snap.silk_reducedDependency),

		StreamChannels:       int32(snap.stream_channels),
		HybridStereoWidthQ14: int32(snap.hybrid_stereo_width_Q14),
		VariableHPSmth2Q15:   int32(snap.variable_HP_smth2_Q15),
		PrevHBGainBits:       uint32(snap.prev_HB_gain_bits),
		Mode:                 int32(snap.mode),
		PrevMode:             int32(snap.prev_mode),
		PrevChannels:         int32(snap.prev_channels),
		PrevFramesize:        int32(snap.prev_framesize),
		Bandwidth:            int32(snap.bandwidth),
		First:                int32(snap.first),
		NbNoActivityMsQ1:     int32(snap.nb_no_activity_ms_Q1),
	}
	return int(initRet), ctlRet, out
}

func cOpusEncoderCtlGetI32(Fs int32, channels, application, preSetRequest int, preSetValue int32, getRequest int) (int, int, int, int32) {
	var initRet, preSetRet C.int
	var outValue C.int32_t
	getRet := int(C.c_opus_encoder_ctl_get_i32(
		C.int32_t(Fs), C.int(channels), C.int(application),
		C.int(preSetRequest), C.int32_t(preSetValue), C.int(getRequest),
		&initRet, &preSetRet, &outValue))
	return int(initRet), int(preSetRet), getRet, int32(outValue)
}

func cOpusEncoderCtlGetU32(Fs int32, channels, application, preSetRequest int, preSetValue int32, getRequest int) (int, int, int, uint32) {
	var initRet, preSetRet C.int
	var outValue C.uint32_t
	getRet := int(C.c_opus_encoder_ctl_get_u32(
		C.int32_t(Fs), C.int(channels), C.int(application),
		C.int(preSetRequest), C.int32_t(preSetValue), C.int(getRequest),
		&initRet, &preSetRet, &outValue))
	return int(initRet), int(preSetRet), getRet, uint32(outValue)
}
