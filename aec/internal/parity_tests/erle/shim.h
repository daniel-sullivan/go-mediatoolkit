//go:build cgo && aec_oracle

// AEC3 erle parity shim: an extern "C" wrapper that drives a standalone
// webrtc::ErleEstimator with realistic inputs derived from a real
// webrtc::Subtractor + webrtc::RenderDelayBuffer pipeline (the same
// front-end the aecstate slice's shim uses), so that ErleEstimator's
// own math -- including SignalDependentErleEstimator, which is only
// constructed when EchoCanceller3Config::Erle::num_sections > 1 and so
// is never exercised by the aecstate slice's default-config scenarios
// -- can be compared bit-exactly against this port's ErleEstimator in
// isolation from AecState.
//
// EchoRemoverImpl/AecState are not exercised for their own state here:
// an AecState is constructed (Subtractor::Process requires one) but
// its Update() is never called, so it stays at its deterministic
// constructor-default state on both sides -- irrelevant to this
// slice's comparisons, which are all against the standalone
// ErleEstimator instance. The E2/Y2 spectrum-forming glue
// (FormLinearFilterOutput, WindowedPaddedFft) is replicated verbatim
// here exactly as in the aecstate slice's shim, for the same reason
// (internal linkage in echo_remover.cc). The render power spectrum fed
// to ErleEstimator::Update as avg_render_spectrum_with_reverb is the
// render buffer's own spectrum with no reverb term added (reverb
// modeling is the separate "reverb" slice's concern) -- a legitimate,
// simpler real input, since ErleEstimator::Update treats it as an
// opaque array regardless of provenance.
#ifndef AEC_PARITY_ERLE_SHIM_H_
#define AEC_PARITY_ERLE_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_erle aec3_erle;

// AEC3_ERLE_OUTPUT_FLOATS is the flattened ErleEstimator-readable-state
// layout aec3_erle_process writes into out, fixed for
// num_capture_channels == 1, in this exact order:
//   erle_no_onset[65], erle_onset[65], erle_unbounded[65],
//   erle_during_onsets[65], fullband_erle_log2,
//   quality_valid (0/1), quality_value (0 if quality_valid == 0)
#define AEC3_ERLE_OUTPUT_FLOATS (65 * 4 + 3)

// num_sections is EchoCanceller3Config::Erle::num_sections (1 keeps
// the signal-dependent estimator unset, as in the default config; >1
// forces it to be constructed).
aec3_erle* aec3_erle_create(int num_render_channels, int num_sections);
void aec3_erle_destroy(aec3_erle* s);

// samples must have length num_render_channels * 64.
void aec3_erle_insert_render_block(aec3_erle* s, const float* samples);

// Runs one capture block through the real Subtractor (AecState::Update
// is never called -- see shim.h's doc comment), forms the linear
// filter output E and the raw capture Y exactly as echo_remover.cc
// does, then drives ErleEstimator::Update directly with real E2/Y2/
// filter-frequency-response/converged-filter inputs.
//
// capture_samples: 64 floats (num_capture_channels == 1).
// delay_blocks: the fixed echo delay (in blocks) used to read the
//   render buffer's own spectrum as ErleEstimator::Update's
//   avg_render_spectrum_with_reverb input (see shim.h's doc comment).
// out: must have length AEC3_ERLE_OUTPUT_FLOATS.
void aec3_erle_process(aec3_erle* s, const float* capture_samples,
                       int delay_blocks, float* out);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_ERLE_SHIM_H_
