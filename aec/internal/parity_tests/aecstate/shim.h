//go:build cgo && aec_oracle

// AEC3 aecstate parity shim: an extern "C" wrapper that integrates the
// oracle's webrtc::AecState with webrtc::Subtractor,
// webrtc::RenderDelayBuffer and webrtc::RenderSignalAnalyzer using the
// exact orchestration order EchoRemoverImpl::ProcessCapture uses
// (echo_remover.cc), so that AecState::Update() is driven with
// realistic inputs and its readable state can be compared bit-exactly
// against this port's AecState.
//
// EchoRemoverImpl itself is NOT ported or wrapped here (that is
// Phase 5's scope): the small amount of glue code
// EchoRemoverImpl::ProcessCapture uses just to shape AecState::Update's
// E2/Y2 inputs -- FormLinearFilterOutput, WindowedPaddedFft and
// SignalTransition, all defined as free functions/private methods in
// echo_remover.cc -- is replicated verbatim in shim.cc (they have
// internal linkage in echo_remover.cc so cannot be called directly;
// LinearEchoPower is intentionally omitted, since its result only
// feeds suppression-gain computation in EchoRemoverImpl, which is out
// of scope here and never consulted by AecState::Update). Subtractor
// is forced to Aec3Optimization::kNone (matching this port's
// scalar-only implementation), and FftData::Spectrum is likewise
// always invoked with kNone, exactly as the subtractor slice does.
#ifndef AEC_PARITY_AECSTATE_SHIM_H_
#define AEC_PARITY_AECSTATE_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_aecstate aec3_aecstate;

// kAecstateSubtractorOutputFloats mirrors AEC3_SUBTRACTOR_OUTPUT_FLOATS
// from the subtractor slice's shim.h (same field order): s_refined[64],
// s_coarse[64], e_refined[64], e_coarse[64], E_refined.re[65],
// E_refined.im[65], E2_refined[65], E2_coarse[65], s2_refined,
// s2_coarse, e2_refined, e2_coarse, y2, s_refined_max_abs,
// s_coarse_max_abs.
#define AEC3_AECSTATE_SUBTRACTOR_OUTPUT_FLOATS (64 * 4 + 65 * 4 + 7)

// AEC3_AECSTATE_OUTPUT_FLOATS is the flattened AecState-readable-state
// layout aec3_aecstate_process writes into aec_state_out, fixed for
// num_capture_channels == 1, in this exact order:
//   erle_no_onset[65], erle_onset[65], erle_unbounded[65], erl[65],
//   reverb_freq_response[65],
//   fullband_erle_log2, erl_time_domain, min_direct_path_filter_delay,
//   saturated_capture, saturated_echo, transparent_mode_active,
//   reverb_decay_default, reverb_decay_mild, transition_triggered,
//   filter_length_blocks, usable_linear_estimate,
//   use_linear_filter_output, active_render
#define AEC3_AECSTATE_OUTPUT_FLOATS (65 * 5 + 13)

aec3_aecstate* aec3_aecstate_create(int num_render_channels);
void aec3_aecstate_destroy(aec3_aecstate* s);

// samples must have length num_render_channels * 64.
void aec3_aecstate_insert_render_block(aec3_aecstate* s, const float* samples);

// Runs one full capture block through the same orchestration
// EchoRemoverImpl::ProcessCapture uses (see shim.h's doc comment),
// ending with a call to AecState::Update.
//
// capture_samples: 64 floats (num_capture_channels == 1).
// capture_saturation: 0/1, matches ProcessCapture's caller-supplied
//   capture_signal_saturation parameter.
// gain_change/clock_drift: 0/1. delay_change: 0=kNone, 1=kBufferFlush,
//   2=kNewDetectedDelay.
// has_external_delay: 0/1. external_delay_quality: 0=kCoarse,
//   1=kRefined. external_delay_value: delay in blocks.
// outputs_out: must have length AEC3_AECSTATE_SUBTRACTOR_OUTPUT_FLOATS.
// aec_state_out: must have length AEC3_AECSTATE_OUTPUT_FLOATS.
void aec3_aecstate_process(aec3_aecstate* s, const float* capture_samples,
                           int capture_saturation, int gain_change,
                           int delay_change, int clock_drift,
                           int has_external_delay, int external_delay_quality,
                           int external_delay_value, float* outputs_out,
                           float* aec_state_out);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_AECSTATE_SHIM_H_
