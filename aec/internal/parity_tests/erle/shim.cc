//go:build cgo && aec_oracle

// AEC3 erle parity shim implementation. Compiled by cgo with the
// include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <algorithm>
#include <array>
#include <vector>

#include "api/audio/echo_canceller3_config.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/aec3_fft.h"
#include "modules/audio_processing/aec3/aec_state.h"
#include "modules/audio_processing/aec3/block.h"
#include "modules/audio_processing/aec3/erle_estimator.h"
#include "modules/audio_processing/aec3/fft_data.h"
#include "modules/audio_processing/aec3/render_delay_buffer.h"
#include "modules/audio_processing/aec3/render_signal_analyzer.h"
#include "modules/audio_processing/aec3/subtractor.h"
#include "modules/audio_processing/aec3/subtractor_output.h"
#include "modules/audio_processing/aec3/subtractor_output_analyzer.h"
#include "modules/audio_processing/logging/apm_data_dumper.h"

namespace {
constexpr int kSampleRateHz = 16000;

// Replicated verbatim from echo_remover.cc's anonymous namespace (see
// the aecstate slice's shim.cc for the full rationale -- these two
// functions are internal-linkage helpers in echo_remover.cc, needed
// only to shape the E2/Y2 inputs ErleEstimator::Update (via
// AecState::Update in the real orchestration) consumes.

void SignalTransition(rtc::ArrayView<const float> from,
                     rtc::ArrayView<const float> to,
                     rtc::ArrayView<float> out) {
  if (from == to) {
    std::copy(to.begin(), to.end(), out.begin());
  } else {
    constexpr size_t kTransitionSize = 30;
    constexpr float kOneByTransitionSizePlusOne = 1.f / (kTransitionSize + 1);
    for (size_t k = 0; k < kTransitionSize; ++k) {
      float a = (k + 1) * kOneByTransitionSizePlusOne;
      out[k] = a * to[k] + (1.f - a) * from[k];
    }
    std::copy(to.begin() + kTransitionSize, to.end(),
              out.begin() + kTransitionSize);
  }
}

void WindowedPaddedFft(const webrtc::Aec3Fft& fft,
                      rtc::ArrayView<const float> v,
                      rtc::ArrayView<float> v_old,
                      webrtc::FftData* V) {
  fft.PaddedFft(v, v_old, webrtc::Aec3Fft::Window::kSqrtHanning, V);
  std::copy(v.begin(), v.end(), v_old.begin());
}

}  // namespace

struct aec3_erle {
  aec3_erle(const webrtc::EchoCanceller3Config& config,
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
        output_analyzer(num_capture_channels),
        erle_estimator(2 * webrtc::kNumBlocksPerSecond, config,
                      num_capture_channels),
        refined_filter_output_last_selected(true) {
    e_old.fill(0.f);
    y_old.fill(0.f);
  }

  webrtc::ApmDataDumper dumper;
  size_t num_render_channels;
  size_t num_capture_channels;
  std::unique_ptr<webrtc::RenderDelayBuffer> render_buffer;
  webrtc::RenderSignalAnalyzer analyzer;
  webrtc::AecState aec_state;  // never Update()'d -- see shim.h.
  webrtc::Subtractor subtractor;
  std::vector<webrtc::SubtractorOutput> outputs;
  webrtc::SubtractorOutputAnalyzer output_analyzer;
  webrtc::ErleEstimator erle_estimator;
  webrtc::Aec3Fft fft;
  std::array<float, webrtc::kFftLengthBy2> e_old;
  std::array<float, webrtc::kFftLengthBy2> y_old;
  bool refined_filter_output_last_selected;
};

extern "C" {

aec3_erle* aec3_erle_create(int num_render_channels, int num_sections) {
  webrtc::EchoCanceller3Config config;
  config.erle.num_sections = static_cast<size_t>(num_sections);
  return new aec3_erle(config, static_cast<size_t>(num_render_channels));
}

void aec3_erle_destroy(aec3_erle* s) { delete s; }

void aec3_erle_insert_render_block(aec3_erle* s, const float* samples) {
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

namespace {
// C: echo_remover.cc's EchoRemoverImpl::FormLinearFilterOutput,
// specialized to use_coarse_filter_output == false's simpler always-
// refined selection (config.filter.enable_coarse_filter_output_usage
// is false by default and this slice does not vary it -- the aecstate
// slice already covers the coarse/refined switching logic itself).
void FormLinearFilterOutput(aec3_erle* s,
                           const webrtc::SubtractorOutput& subtractor_output,
                           rtc::ArrayView<float> output) {
  SignalTransition(subtractor_output.e_refined, subtractor_output.e_refined,
                  output);
  s->refined_filter_output_last_selected = true;
}

void PackErleOutput(const webrtc::ErleEstimator& erle, float* dst) {
  size_t idx = 0;
  auto erle_no_onset = erle.Erle(false);
  for (float v : erle_no_onset[0]) dst[idx++] = v;
  auto erle_onset = erle.Erle(true);
  for (float v : erle_onset[0]) dst[idx++] = v;
  auto erle_unbounded = erle.ErleUnbounded();
  for (float v : erle_unbounded[0]) dst[idx++] = v;
  auto erle_during_onsets = erle.ErleDuringOnsets();
  for (float v : erle_during_onsets[0]) dst[idx++] = v;
  dst[idx++] = erle.FullbandErleLog2();
  auto quality = erle.GetInstLinearQualityEstimates();
  if (quality[0].has_value()) {
    dst[idx++] = 1.f;
    dst[idx++] = *quality[0];
  } else {
    dst[idx++] = 0.f;
    dst[idx++] = 0.f;
  }
}
}  // namespace

void aec3_erle_process(aec3_erle* s, const float* capture_samples,
                       int delay_blocks, float* out) {
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

  bool any_filter_converged, any_coarse_filter_converged, all_filters_diverged;
  s->output_analyzer.Update(s->outputs, &any_filter_converged,
                            &any_coarse_filter_converged,
                            &all_filters_diverged);

  std::array<float, webrtc::kBlockSize> e;
  FormLinearFilterOutput(s, s->outputs[0], e);

  webrtc::FftData E, Y;
  WindowedPaddedFft(s->fft, capture.View(/*band=*/0, 0), s->y_old, &Y);
  WindowedPaddedFft(s->fft, e, s->e_old, &E);

  std::array<float, webrtc::kFftLengthBy2Plus1> Y2, E2;
  Y.Spectrum(webrtc::Aec3Optimization::kNone, Y2);
  E.Spectrum(webrtc::Aec3Optimization::kNone, E2);

  auto x2_reverb = s->render_buffer->GetRenderBuffer()->Spectrum(delay_blocks);

  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> y2_vec = {Y2};
  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> e2_vec = {E2};

  s->erle_estimator.Update(*s->render_buffer->GetRenderBuffer(),
                           s->subtractor.FilterFrequencyResponses(),
                           x2_reverb[0], y2_vec, e2_vec,
                           s->output_analyzer.ConvergedFilters());

  PackErleOutput(s->erle_estimator, out);
}

}  // extern "C"
