//go:build cgo && aec_oracle

// AEC3 subtractor parity shim: an extern "C" wrapper around the
// oracle's webrtc::Subtractor (scalar path, forced via
// Aec3Optimization::kNone) integrated with webrtc::RenderDelayBuffer
// (Insert + PrepareCaptureProcessing per block, same minimal driving
// as the adaptivefir slice -- no delay controller/estimation is
// exercised, since Subtractor does not depend on delay estimate
// quality), webrtc::RenderSignalAnalyzer (updated with
// std::nullopt for the optional delay-partitions hint on every call,
// since the full AecState::MinDirectPathFilterDelay() this would
// normally come from is out of scope for this port's minimal AecState
// stub -- both sides pass "no value" identically, so this is a
// like-for-like bit-exact comparison, not a correctness claim about
// delay tracking), and webrtc::AecState (constructed for real, but
// only ever touched through the public UpdateCaptureSaturation(bool)
// setter -- the same single knob this port's minimal AecState stub
// exposes; AecState::Update() itself is never called on either side).
#ifndef AEC_PARITY_SUBTRACTOR_SHIM_H_
#define AEC_PARITY_SUBTRACTOR_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_subtractor aec3_subtractor;

// kSubtractorOutputFloats is the flattened per-channel float layout
// aec3_subtractor_process writes into outputs_out, in this exact
// order (matching subtractor_output.h's field order and this port's
// SubtractorOutput field order):
//   s_refined[64], s_coarse[64], e_refined[64], e_coarse[64],
//   E_refined.re[65], E_refined.im[65], E2_refined[65], E2_coarse[65],
//   s2_refined, s2_coarse, e2_refined, e2_coarse, y2,
//   s_refined_max_abs, s_coarse_max_abs
#define AEC3_SUBTRACTOR_OUTPUT_FLOATS (64 * 4 + 65 * 4 + 7)

aec3_subtractor* aec3_subtractor_create(int num_render_channels,
                                        int num_capture_channels);
void aec3_subtractor_destroy(aec3_subtractor* s);

// samples must have length num_render_channels * kBlockSize.
void aec3_subtractor_insert_render_block(aec3_subtractor* s,
                                         const float* samples);

// Updates the RenderSignalAnalyzer from the current render buffer
// content, passing std::nullopt for the delay-partitions hint (see
// shim.h's doc comment).
void aec3_subtractor_update_render_signal_analyzer(aec3_subtractor* s);

// delay_change: 0=kNone, 1=kBufferFlush, 2=kNewDetectedDelay (matches
// this port's DelayAdjustment enum).
void aec3_subtractor_handle_echo_path_change(aec3_subtractor* s,
                                             int gain_change,
                                             int delay_change,
                                             int clock_drift);

void aec3_subtractor_exit_initial_state(aec3_subtractor* s);

void aec3_subtractor_set_saturated_capture(aec3_subtractor* s, int saturated);

// capture_samples must have length num_capture_channels * kBlockSize.
// outputs_out must have length
// num_capture_channels * AEC3_SUBTRACTOR_OUTPUT_FLOATS.
void aec3_subtractor_process(aec3_subtractor* s, const float* capture_samples,
                             float* outputs_out);

int aec3_subtractor_impulse_response_len(aec3_subtractor* s, int channel);
// out must have length >= aec3_subtractor_impulse_response_len(s, channel).
void aec3_subtractor_get_impulse_response(aec3_subtractor* s, int channel,
                                          float* out);

int aec3_subtractor_frequency_response_partitions(aec3_subtractor* s,
                                                  int channel);
// out must have length >=
// aec3_subtractor_frequency_response_partitions(s, channel) * 65.
void aec3_subtractor_get_frequency_response(aec3_subtractor* s, int channel,
                                            float* out);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_SUBTRACTOR_SHIM_H_
