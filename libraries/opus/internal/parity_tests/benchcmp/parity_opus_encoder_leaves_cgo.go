//go:build cgo

package benchcmp

/*
#include <stdint.h>
#include <string.h>

struct c_bandwidth_thresholds {
    int32_t mono_voice[8];
    int32_t mono_music[8];
    int32_t stereo_voice[8];
    int32_t stereo_music[8];
};

extern int opus_test_decide_fec(int useInBandFEC, int PacketLoss_perc, int last_fec,
                                int mode, int *bandwidth_io, int32_t rate);
extern int opus_test_compute_silk_rate_for_hybrid(int rate, int bandwidth, int frame20ms,
                                                  int vbr, int fec, int channels);
extern int32_t opus_test_compute_equiv_rate(int32_t bitrate, int channels, int frame_rate,
                                            int vbr, int mode, int complexity, int loss);
extern float opus_test_compute_frame_energy(const float *pcm, int frame_size, int channels, int arch);
extern int opus_test_decide_dtx_mode(int activity, int *nb_no_activity_ms_Q1, int frame_size_ms_Q1);
extern int opus_test_compute_redundancy_bytes(int32_t max_data_bytes, int32_t bitrate_bps,
                                              int frame_rate, int channels);
extern void opus_test_get_bandwidth_thresholds(struct c_bandwidth_thresholds *out);
extern void opus_test_get_stereo_thresholds(int32_t *voice, int32_t *music);
extern void opus_test_get_mode_thresholds(int32_t *out);
extern void opus_test_get_fec_thresholds(int32_t *out);

static int c_decide_fec(int useInBandFEC, int PacketLoss_perc, int last_fec,
                        int mode, int *bandwidth_io, int32_t rate) {
    return opus_test_decide_fec(useInBandFEC, PacketLoss_perc, last_fec,
                                mode, bandwidth_io, rate);
}

static int c_compute_silk_rate_for_hybrid(int rate, int bandwidth, int frame20ms,
                                          int vbr, int fec, int channels) {
    return opus_test_compute_silk_rate_for_hybrid(rate, bandwidth, frame20ms, vbr, fec, channels);
}

static int32_t c_compute_equiv_rate(int32_t bitrate, int channels, int frame_rate,
                                    int vbr, int mode, int complexity, int loss) {
    return opus_test_compute_equiv_rate(bitrate, channels, frame_rate, vbr, mode, complexity, loss);
}

static float c_compute_frame_energy(const float *pcm, int frame_size, int channels, int arch) {
    return opus_test_compute_frame_energy(pcm, frame_size, channels, arch);
}

static int c_decide_dtx_mode(int activity, int *nb_no_activity_ms_Q1, int frame_size_ms_Q1) {
    return opus_test_decide_dtx_mode(activity, nb_no_activity_ms_Q1, frame_size_ms_Q1);
}

static int c_compute_redundancy_bytes(int32_t max_data_bytes, int32_t bitrate_bps,
                                      int frame_rate, int channels) {
    return opus_test_compute_redundancy_bytes(max_data_bytes, bitrate_bps, frame_rate, channels);
}

static void c_get_bandwidth_thresholds(struct c_bandwidth_thresholds *out) {
    opus_test_get_bandwidth_thresholds(out);
}

static void c_get_stereo_thresholds(int32_t *voice, int32_t *music) {
    opus_test_get_stereo_thresholds(voice, music);
}

static void c_get_mode_thresholds(int32_t *out) {
    opus_test_get_mode_thresholds(out);
}

static void c_get_fec_thresholds(int32_t *out) {
    opus_test_get_fec_thresholds(out);
}
*/
import "C"
import "unsafe"

func cDecideFec(useInBandFEC, PacketLossPerc, lastFec, mode, bandwidth int, rate int32) (int, int) {
	var bw C.int = C.int(bandwidth)
	r := int(C.c_decide_fec(C.int(useInBandFEC), C.int(PacketLossPerc), C.int(lastFec),
		C.int(mode), &bw, C.int32_t(rate)))
	return r, int(bw)
}

func cComputeSilkRateForHybrid(rate, bandwidth, frame20ms, vbr, fec, channels int) int {
	return int(C.c_compute_silk_rate_for_hybrid(
		C.int(rate), C.int(bandwidth), C.int(frame20ms),
		C.int(vbr), C.int(fec), C.int(channels)))
}

func cComputeEquivRate(bitrate int32, channels, frameRate, vbr, mode, complexity, loss int) int32 {
	return int32(C.c_compute_equiv_rate(
		C.int32_t(bitrate), C.int(channels), C.int(frameRate),
		C.int(vbr), C.int(mode), C.int(complexity), C.int(loss)))
}

func cComputeFrameEnergy(pcm []float32, frameSize, channels, arch int) float32 {
	if len(pcm) == 0 {
		return 0
	}
	return float32(C.c_compute_frame_energy(
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize), C.int(channels), C.int(arch)))
}

func cDecideDtxMode(activity, nbNoActivityMsQ1, frameSizeMsQ1 int) (int, int) {
	var x C.int = C.int(nbNoActivityMsQ1)
	r := int(C.c_decide_dtx_mode(C.int(activity), &x, C.int(frameSizeMsQ1)))
	return r, int(x)
}

func cComputeRedundancyBytes(maxDataBytes, bitrateBps int32, frameRate, channels int) int {
	return int(C.c_compute_redundancy_bytes(
		C.int32_t(maxDataBytes), C.int32_t(bitrateBps),
		C.int(frameRate), C.int(channels)))
}

func cGetBandwidthThresholds() (monoVoice, monoMusic, stereoVoice, stereoMusic [8]int32) {
	var t C.struct_c_bandwidth_thresholds
	C.c_get_bandwidth_thresholds(&t)
	for i := 0; i < 8; i++ {
		monoVoice[i] = int32(t.mono_voice[i])
		monoMusic[i] = int32(t.mono_music[i])
		stereoVoice[i] = int32(t.stereo_voice[i])
		stereoMusic[i] = int32(t.stereo_music[i])
	}
	return
}

func cGetStereoThresholds() (voice, music int32) {
	var v, m C.int32_t
	C.c_get_stereo_thresholds(&v, &m)
	return int32(v), int32(m)
}

func cGetModeThresholds() [2][2]int32 {
	var arr [4]C.int32_t
	C.c_get_mode_thresholds(&arr[0])
	var out [2][2]int32
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			out[i][j] = int32(arr[2*i+j])
		}
	}
	return out
}

func cGetFecThresholds() [10]int32 {
	var arr [10]C.int32_t
	C.c_get_fec_thresholds(&arr[0])
	var out [10]int32
	for i := 0; i < 10; i++ {
		out[i] = int32(arr[i])
	}
	return out
}
