//go:build cgo && aec_oracle

// AEC3 reverb parity shim: exposes two independent standalone
// surfaces.
//
// Part A (aec3_reverbmodel_*) drives a bare webrtc::ReverbModel
// directly with scripted power-spectrum/scaling/decay inputs. This is
// the only place in this port's parity coverage that exercises
// ReverbModel::UpdateReverb (the per-frequency-bin-scaling variant):
// grepping both this port and the oracle confirms UpdateReverb's only
// real call site is ResidualEchoEstimator::UpdateReverb in
// residual_echo_estimator.cc, which belongs to EchoRemoverImpl's
// suppression-gain pipeline -- explicitly out of this task's Phase 4
// scope (AecState and its estimators only). UpdateReverbNoFreqShaping
// (the single-scalar variant actually used by AecState via
// computeAvgRenderReverb) is exercised here too, side by side on the
// same instance, for a complete direct comparison of both entry
// points against an identical bit-exact oracle instance.
//
// Part B (aec3_reverbest_*) drives a standalone
// webrtc::ReverbModelEstimator (and, through it, ReverbDecayEstimator
// and ReverbFrequencyResponse) using real impulse/frequency responses
// derived from a genuine webrtc::Subtractor + webrtc::FilterAnalyzer +
// webrtc::RenderDelayBuffer pipeline (AecState itself is never
// Update()'d -- only constructed, to satisfy Subtractor::Process's
// signature). Critically, EchoCanceller3Config::ep_strength.default_len
// is forced negative here (DefaultConfig's real default is +0.83,
// which the aecstate slice always uses and which selects
// ReverbDecayEstimator's "not adaptive" constant-decay branch,
// EstimateDecay/AnalyzeFilter's entire adaptive-analysis machinery is
// therefore NEVER exercised by any other slice) -- so this is the only
// slice giving bit-exact coverage of ReverbDecayEstimator's adaptive
// estimation path (AnalyzeFilter, EstimateDecay, BlockAverage,
// AnalyzeBlockGain, SymmetricArithmeticSum, BlockEnergyPeak,
// BlockEnergyAverage, LateReverbLinearRegressor,
// EarlyReverbLengthEstimator).
//
// linear_filter_qualities and usable_linear_estimates are simplified
// inputs here (scripted quality values, a constant "usable" flag)
// rather than driven through a real ErleEstimator/FilterQualityState,
// since ReverbModelEstimator::Update treats them as opaque data --
// FilterQualityState's own logic is already covered bit-exact by the
// aecstate slice's full AecState::Update pipeline. stationary_block is
// likewise passed directly per call (also otherwise never true in any
// other slice, since EchoAudibility.use_stationarity_properties
// defaults false), to cover ReverbFrequencyResponse::Update's and
// ReverbDecayEstimator::Update's stationary-signal early return.
#ifndef AEC_PARITY_REVERB_SHIM_H_
#define AEC_PARITY_REVERB_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_reverbmodel aec3_reverbmodel;

aec3_reverbmodel* aec3_reverbmodel_create(void);
void aec3_reverbmodel_destroy(aec3_reverbmodel* s);

// power_spectrum and out must have length 65 (kFftLengthBy2Plus1).
void aec3_reverbmodel_update_no_freq(aec3_reverbmodel* s,
                                     const float* power_spectrum,
                                     float scaling, float decay, float* out);

// power_spectrum, scaling and out must have length 65.
void aec3_reverbmodel_update_freq(aec3_reverbmodel* s,
                                  const float* power_spectrum,
                                  const float* scaling, float decay,
                                  float* out);

typedef struct aec3_reverbest aec3_reverbest;

// AEC3_REVERBEST_OUTPUT_FLOATS is the flattened
// ReverbModelEstimator-readable-state layout aec3_reverbest_process
// writes into out, in this exact order:
//   frequency_response[65], reverb_decay(mild=false), reverb_decay(mild=true)
#define AEC3_REVERBEST_OUTPUT_FLOATS (65 + 2)

// default_len/nearend_len set EchoCanceller3Config::ep_strength's
// default_len/nearend_len (see shim.h's doc comment for why
// default_len must be negative to exercise the adaptive decay path).
aec3_reverbest* aec3_reverbest_create(int num_render_channels,
                                      float default_len, float nearend_len);
void aec3_reverbest_destroy(aec3_reverbest* s);

// samples must have length num_render_channels * 64.
void aec3_reverbest_insert_render_block(aec3_reverbest* s,
                                        const float* samples);

// Runs one capture block through the real Subtractor+FilterAnalyzer,
// then drives ReverbModelEstimator::Update directly.
//
// capture_samples: 64 floats (num_capture_channels == 1).
// has_quality: 0 or 1 -- whether linear_filter_qualities[0] carries a
//   value (0 mirrors std::optional's empty state, exercising the
//   linear_filter_quality == nullptr skip branch).
// quality: the scripted quality value, used only if has_quality != 0.
// stationary_block: 0 or 1, passed straight through as
//   ReverbModelEstimator::Update's stationary_block argument.
// out: must have length AEC3_REVERBEST_OUTPUT_FLOATS.
void aec3_reverbest_process(aec3_reverbest* s, const float* capture_samples,
                            int has_quality, float quality,
                            int stationary_block, float* out);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_REVERB_SHIM_H_
