//go:build cgo && aec_oracle

// AEC3 reverb parity shim implementation. Compiled by cgo with the
// include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <algorithm>
#include <array>
#include <memory>
#include <optional>
#include <vector>

#include "api/audio/echo_canceller3_config.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/aec_state.h"
#include "modules/audio_processing/aec3/block.h"
#include "modules/audio_processing/aec3/filter_analyzer.h"
#include "modules/audio_processing/aec3/render_delay_buffer.h"
#include "modules/audio_processing/aec3/render_signal_analyzer.h"
#include "modules/audio_processing/aec3/reverb_model.h"
#include "modules/audio_processing/aec3/reverb_model_estimator.h"
#include "modules/audio_processing/aec3/subtractor.h"
#include "modules/audio_processing/aec3/subtractor_output.h"
#include "modules/audio_processing/logging/apm_data_dumper.h"

namespace {
constexpr int kSampleRateHz = 16000;
}  // namespace

// ---- Part A: direct ReverbModel unit test ----

struct aec3_reverbmodel {
  webrtc::ReverbModel model;
};

extern "C" {

aec3_reverbmodel* aec3_reverbmodel_create() { return new aec3_reverbmodel(); }

void aec3_reverbmodel_destroy(aec3_reverbmodel* s) { delete s; }

void aec3_reverbmodel_update_no_freq(aec3_reverbmodel* s,
                                     const float* power_spectrum,
                                     float scaling, float decay, float* out) {
  std::array<float, webrtc::kFftLengthBy2Plus1> ps;
  std::copy(power_spectrum, power_spectrum + webrtc::kFftLengthBy2Plus1,
            ps.begin());
  s->model.UpdateReverbNoFreqShaping(ps, scaling, decay);
  auto r = s->model.reverb();
  std::copy(r.begin(), r.end(), out);
}

void aec3_reverbmodel_update_freq(aec3_reverbmodel* s,
                                  const float* power_spectrum,
                                  const float* scaling, float decay,
                                  float* out) {
  std::array<float, webrtc::kFftLengthBy2Plus1> ps, sc;
  std::copy(power_spectrum, power_spectrum + webrtc::kFftLengthBy2Plus1,
            ps.begin());
  std::copy(scaling, scaling + webrtc::kFftLengthBy2Plus1, sc.begin());
  s->model.UpdateReverb(ps, sc, decay);
  auto r = s->model.reverb();
  std::copy(r.begin(), r.end(), out);
}

}  // extern "C"

// ---- Part B: ReverbModelEstimator pipeline ----

struct aec3_reverbest {
  aec3_reverbest(const webrtc::EchoCanceller3Config& config,
                size_t num_render_channels)
      : dumper(0),
        num_render_channels(num_render_channels),
        num_capture_channels(1),
        render_buffer(webrtc::RenderDelayBuffer::Create(config, kSampleRateHz,
                                                        num_render_channels)),
        analyzer(config),
        aec_state(config, num_capture_channels),
        subtractor(config, num_render_channels, num_capture_channels,
                  &dumper, webrtc::Aec3Optimization::kNone),
        outputs(num_capture_channels),
        filter_analyzer(config, num_capture_channels),
        estimator(config, num_capture_channels) {}

  webrtc::ApmDataDumper dumper;
  size_t num_render_channels;
  size_t num_capture_channels;
  std::unique_ptr<webrtc::RenderDelayBuffer> render_buffer;
  webrtc::RenderSignalAnalyzer analyzer;
  webrtc::AecState aec_state;  // never Update()'d -- see shim.h.
  webrtc::Subtractor subtractor;
  std::vector<webrtc::SubtractorOutput> outputs;
  webrtc::FilterAnalyzer filter_analyzer;
  webrtc::ReverbModelEstimator estimator;
};

extern "C" {

aec3_reverbest* aec3_reverbest_create(int num_render_channels,
                                      float default_len, float nearend_len) {
  webrtc::EchoCanceller3Config config;
  config.ep_strength.default_len = default_len;
  config.ep_strength.nearend_len = nearend_len;
  return new aec3_reverbest(config, static_cast<size_t>(num_render_channels));
}

void aec3_reverbest_destroy(aec3_reverbest* s) { delete s; }

void aec3_reverbest_insert_render_block(aec3_reverbest* s,
                                        const float* samples) {
  webrtc::Block block(1, s->num_render_channels);
  for (size_t ch = 0; ch < s->num_render_channels; ++ch) {
    auto view = block.View(0, static_cast<int>(ch));
    for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
      view[i] = samples[ch * webrtc::kBlockSize + i];
    }
  }
  s->render_buffer->Insert(block);
  s->render_buffer->PrepareCaptureProcessing();
}

void aec3_reverbest_process(aec3_reverbest* s, const float* capture_samples,
                            int has_quality, float quality,
                            int stationary_block, float* out) {
  webrtc::Block capture(1, s->num_capture_channels);
  {
    auto view = capture.View(0, 0);
    for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
      view[i] = capture_samples[i];
    }
  }

  int delay = s->aec_state.MinDirectPathFilterDelay();
  s->analyzer.Update(*s->render_buffer->GetRenderBuffer(), delay);

  s->subtractor.Process(*s->render_buffer->GetRenderBuffer(), capture,
                        s->analyzer, s->aec_state, s->outputs);

  bool any_filter_consistent;
  float max_echo_path_gain;
  s->filter_analyzer.Update(s->subtractor.FilterImpulseResponses(),
                            *s->render_buffer->GetRenderBuffer(),
                            &any_filter_consistent, &max_echo_path_gain);

  std::vector<std::optional<float>> linear_filter_qualities(
      s->num_capture_channels);
  if (has_quality) {
    linear_filter_qualities[0] = quality;
  }

  std::vector<bool> usable_linear_estimates(s->num_capture_channels, true);

  // NB: the impulse responses fed to ReverbModelEstimator::Update are
  // the highpass-preprocessed filter (FilterAnalyzer::GetAdjustedFilters),
  // not the raw Subtractor::FilterImpulseResponses -- matching
  // AecState::Update's real call site (aec_state.cc:285-286).
  s->estimator.Update(s->filter_analyzer.GetAdjustedFilters(),
                      s->subtractor.FilterFrequencyResponses(),
                      linear_filter_qualities,
                      s->filter_analyzer.FilterDelaysBlocks(),
                      usable_linear_estimates, stationary_block != 0);

  size_t idx = 0;
  auto freq_response = s->estimator.GetReverbFrequencyResponse();
  for (float v : freq_response) out[idx++] = v;
  out[idx++] = s->estimator.ReverbDecay(false);
  out[idx++] = s->estimator.ReverbDecay(true);
}

}  // extern "C"
