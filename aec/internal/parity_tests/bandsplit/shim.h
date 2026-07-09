//go:build cgo && aec_oracle

// AEC3 band-split parity shim: extern "C" wrappers around the
// oracle's webrtc::ThreeBandFilterBank (48kHz, float), the fixed-point
// two-band QMF (WebRtcSpl_AnalysisQMF/SynthesisQMF), and the
// FloatS16/S16 conversion helpers (see cgo.go).
#ifndef AEC_PARITY_BANDSPLIT_SHIM_H_
#define AEC_PARITY_BANDSPLIT_SHIM_H_

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_three_band aec3_three_band;

aec3_three_band* aec3_three_band_create(void);
void aec3_three_band_destroy(aec3_three_band* tb);
// in: 480 floats; out: 3 bands x 160 floats, flat [band][160].
void aec3_three_band_analysis(aec3_three_band* tb, const float* in,
                              float* out);
// in: 3 bands x 160 floats, flat [band][160]; out: 480 floats.
void aec3_three_band_synthesis(aec3_three_band* tb, const float* in,
                               float* out);

// Two-band QMF. States are 6 int32 words each, carried by the caller
// (mirrors the raw C API, so Go and C can be fed the same state
// lifecycle). in_data: in_len samples (even); low/high: in_len/2 each.
void aec3_analysis_qmf(const int16_t* in_data, int in_len, int16_t* low_band,
                       int16_t* high_band, int32_t* state1, int32_t* state2);
void aec3_synthesis_qmf(const int16_t* low_band, const int16_t* high_band,
                        int band_len, int16_t* out_data, int32_t* state1,
                        int32_t* state2);

// audio_util conversions, applied element-wise over n samples.
void aec3_float_s16_to_s16(const float* src, int n, int16_t* dest);
void aec3_s16_to_float_s16(const int16_t* src, int n, float* dest);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_BANDSPLIT_SHIM_H_
