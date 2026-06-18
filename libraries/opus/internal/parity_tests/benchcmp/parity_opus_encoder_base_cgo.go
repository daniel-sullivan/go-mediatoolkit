//go:build cgo

package benchcmp

/*
#include <opus.h>
#include <stdint.h>
#include <string.h>

// Mirror of opus_test_enc_snapshot in libraries/opus/opus_cgo_src.c.
// Field layout MUST match 1:1.
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

// Forward decls — the implementations live in
// libraries/opus/opus_cgo_src.c inside the `opus` package's
// amalgamated C TU. Declared here so cgo produces matching call
// signatures on the Go side.
extern int opus_test_encoder_init_and_snapshot(int32_t Fs, int channels, int application,
                                               struct c_enc_snapshot *snap);
extern int32_t opus_test_user_bitrate_to_bitrate(int32_t Fs, int channels, int application,
                                                 int32_t user_bitrate_bps,
                                                 int frame_size, int max_data_bytes);

static int c_opus_encoder_init_and_snapshot(int32_t Fs, int channels, int application,
                                            struct c_enc_snapshot *snap) {
    // Underlying C helper declares its own struct type; both are
    // ABI-identical so a plain cast is safe.
    return opus_test_encoder_init_and_snapshot(Fs, channels, application,
                                               (struct c_enc_snapshot*)snap);
}

static int c_opus_encoder_get_size(int channels) {
    return opus_encoder_get_size(channels);
}

// gen_toc is static in opus_encoder.c, so we replicate the exact
// packing here. Kept lock-step with the Go gen_toc.
static unsigned char c_gen_toc(int mode, int framerate, int bandwidth, int channels) {
    // Mode and bandwidth constants pulled from opus_private.h /
    // opus_defines.h — the enum values are public ABI.
    int period = 0;
    unsigned char toc;
    while (framerate < 400) {
        framerate <<= 1;
        period++;
    }
    if (mode == 1000) { // MODE_SILK_ONLY
        toc = (bandwidth - OPUS_BANDWIDTH_NARROWBAND) << 5;
        toc |= (period - 2) << 3;
    } else if (mode == 1002) { // MODE_CELT_ONLY
        int tmp = bandwidth - OPUS_BANDWIDTH_MEDIUMBAND;
        if (tmp < 0) tmp = 0;
        toc = 0x80;
        toc |= tmp << 5;
        toc |= period << 3;
    } else { // MODE_HYBRID (1001) or anything else
        toc = 0x60;
        toc |= (bandwidth - OPUS_BANDWIDTH_SUPERWIDEBAND) << 4;
        toc |= (period - 2) << 3;
    }
    toc |= (channels == 2) << 2;
    return toc;
}

// frame_size_select is an extern function in opus_private.h.
extern int32_t frame_size_select(int application, int32_t frame_size,
                                 int variable_duration, int32_t Fs);

static int32_t c_frame_size_select(int application, int32_t frame_size,
                                   int variable_duration, int32_t Fs) {
    return (int32_t)frame_size_select(application, frame_size, variable_duration, Fs);
}

static int32_t c_user_bitrate_to_bitrate(int32_t Fs, int channels, int application,
                                         int32_t user_bitrate_bps,
                                         int frame_size, int max_data_bytes) {
    return opus_test_user_bitrate_to_bitrate(Fs, channels, application,
                                             user_bitrate_bps, frame_size, max_data_bytes);
}
*/
import "C"
import "unsafe"

// cEncSnapshot mirrors nativeopus.OpusEncoderStateSnapshot.
type cEncSnapshot struct {
	CeltEncOffset     int32
	SilkEncOffset     int32
	Application       int32
	Channels          int32
	DelayCompensation int32
	ForceChannels     int32
	SignalType        int32
	UserBandwidth     int32
	MaxBandwidth      int32
	UserForcedMode    int32
	VoiceRatio        int32
	Fs                int32
	UseVBR            int32
	VBRConstraint     int32
	VariableDuration  int32
	BitrateBps        int32
	UserBitrateBps    int32
	LsbDepth          int32
	EncoderBuffer     int32
	Lfe               int32
	Arch              int32
	UseDTX            int32
	FecConfig         int32

	SilkNChannelsAPI              int32
	SilkNChannelsInternal         int32
	SilkAPISampleRate             int32
	SilkMaxInternalSampleRate     int32
	SilkMinInternalSampleRate     int32
	SilkDesiredInternalSampleRate int32
	SilkPayloadSizeMs             int32
	SilkBitRate                   int32
	SilkPacketLossPercentage      int32
	SilkComplexity                int32
	SilkUseInBandFEC              int32
	SilkUseDTX                    int32
	SilkUseCBR                    int32
	SilkReducedDependency         int32

	StreamChannels       int32
	HybridStereoWidthQ14 int32
	VariableHPSmth2Q15   int32
	PrevHBGainBits       uint32
	Mode                 int32
	PrevMode             int32
	PrevChannels         int32
	PrevFramesize        int32
	Bandwidth            int32
	First                int32
	NbNoActivityMsQ1     int32
}

func cOpusEncoderInitSnapshot(Fs int32, channels, application int) (int, cEncSnapshot) {
	var snap C.struct_c_enc_snapshot
	ret := int(C.c_opus_encoder_init_and_snapshot(
		C.int32_t(Fs), C.int(channels), C.int(application),
		(*C.struct_c_enc_snapshot)(unsafe.Pointer(&snap))))
	return ret, cEncSnapshot{
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
}

func cOpusEncoderGetSize(channels int) int {
	return int(C.c_opus_encoder_get_size(C.int(channels)))
}

func cGenToc(mode, framerate, bandwidth, channels int) byte {
	return byte(C.c_gen_toc(C.int(mode), C.int(framerate), C.int(bandwidth), C.int(channels)))
}

func cFrameSizeSelect(application int, frameSize int32, variableDuration int, Fs int32) int32 {
	return int32(C.c_frame_size_select(C.int(application), C.int32_t(frameSize),
		C.int(variableDuration), C.int32_t(Fs)))
}

func cUserBitrateToBitrate(Fs int32, channels, application int, userBitrate int32, frameSize, maxDataBytes int) int32 {
	return int32(C.c_user_bitrate_to_bitrate(C.int32_t(Fs), C.int(channels), C.int(application),
		C.int32_t(userBitrate), C.int(frameSize), C.int(maxDataBytes)))
}
