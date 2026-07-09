//go:build cgo && aec_oracle

// AEC3 aecstate parity shim implementation. Compiled by cgo with the
// include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <algorithm>
#include <array>
#include <optional>
#include <vector>

#include "api/audio/echo_canceller3_config.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/aec3_fft.h"
#include "modules/audio_processing/aec3/aec_state.h"
#include "modules/audio_processing/aec3/block.h"
#include "modules/audio_processing/aec3/delay_estimate.h"
#include "modules/audio_processing/aec3/echo_path_variability.h"
#include "modules/audio_processing/aec3/fft_data.h"
#include "modules/audio_processing/aec3/render_delay_buffer.h"
#include "modules/audio_processing/aec3/render_signal_analyzer.h"
#include "modules/audio_processing/aec3/subtractor.h"
#include "modules/audio_processing/aec3/subtractor_output.h"
#include "modules/audio_processing/logging/apm_data_dumper.h"

namespace {
constexpr int kSampleRateHz = 16000;

// Replicated verbatim from echo_remover.cc's anonymous namespace (see
// shim.h's doc comment for why these can't be called directly).

// Fades between two input signals using a fix-sized transition. C:
// echo_remover.cc's (anonymous namespace) SignalTransition.
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

// Computes a windowed (square root Hanning) padded FFT and updates the
// related memory. C: echo_remover.cc's (anonymous namespace)
// WindowedPaddedFft.
void WindowedPaddedFft(const webrtc::Aec3Fft& fft,
                      rtc::ArrayView<const float> v,
                      rtc::ArrayView<float> v_old,
                      webrtc::FftData* V) {
  fft.PaddedFft(v, v_old, webrtc::Aec3Fft::Window::kSqrtHanning, V);
  std::copy(v.begin(), v.end(), v_old.begin());
}

}  // namespace

struct aec3_aecstate {
  aec3_aecstate(const webrtc::EchoCanceller3Config& config,
               size_t num_render_channels)
      : dumper(0),
        num_render_channels(num_render_channels),
        num_capture_channels(1),
        use_coarse_filter_output(
            config.filter.enable_coarse_filter_output_usage),
        render_buffer(webrtc::RenderDelayBuffer::Create(config, kSampleRateHz,
                                                        num_render_channels)),
        analyzer(config),
        aec_state(config, num_capture_channels),
        subtractor(config, num_render_channels, num_capture_channels,
                  &dumper, webrtc::Aec3Optimization::kNone),
        outputs(num_capture_channels),
        e_old{{0.f}},
        y_old{{0.f}},
        refined_filter_output_last_selected(true) {}

  webrtc::ApmDataDumper dumper;
  size_t num_render_channels;
  size_t num_capture_channels;
  bool use_coarse_filter_output;
  std::unique_ptr<webrtc::RenderDelayBuffer> render_buffer;
  webrtc::RenderSignalAnalyzer analyzer;
  webrtc::AecState aec_state;
  webrtc::Subtractor subtractor;
  std::vector<webrtc::SubtractorOutput> outputs;
  webrtc::Aec3Fft fft;
  std::array<float, webrtc::kFftLengthBy2> e_old;
  std::array<float, webrtc::kFftLengthBy2> y_old;
  bool refined_filter_output_last_selected;
};

extern "C" {

aec3_aecstate* aec3_aecstate_create(int num_render_channels) {
  webrtc::EchoCanceller3Config config;
  return new aec3_aecstate(config, static_cast<size_t>(num_render_channels));
}

void aec3_aecstate_destroy(aec3_aecstate* s) { delete s; }

void aec3_aecstate_insert_render_block(aec3_aecstate* s,
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

namespace {
// C: echo_remover.cc's EchoRemoverImpl::FormLinearFilterOutput.
void FormLinearFilterOutput(aec3_aecstate* s,
                           const webrtc::SubtractorOutput& subtractor_output,
                           rtc::ArrayView<float> output) {
  bool use_refined_output = true;
  if (s->use_coarse_filter_output) {
    if (subtractor_output.e2_coarse < 0.9f * subtractor_output.e2_refined &&
        subtractor_output.y2 > 30.f * 30.f * webrtc::kBlockSize &&
        (subtractor_output.s2_refined > 60.f * 60.f * webrtc::kBlockSize ||
         subtractor_output.s2_coarse > 60.f * 60.f * webrtc::kBlockSize)) {
      use_refined_output = false;
    } else {
      if (subtractor_output.e2_coarse < subtractor_output.e2_refined &&
          subtractor_output.y2 < subtractor_output.e2_refined) {
        use_refined_output = false;
      }
    }
  }

  SignalTransition(
      s->refined_filter_output_last_selected ? subtractor_output.e_refined
                                             : subtractor_output.e_coarse,
      use_refined_output ? subtractor_output.e_refined
                        : subtractor_output.e_coarse,
      output);
  s->refined_filter_output_last_selected = use_refined_output;
}

void PackSubtractorOutput(const webrtc::SubtractorOutput& o, float* dst) {
  size_t idx = 0;
  for (float v : o.s_refined) dst[idx++] = v;
  for (float v : o.s_coarse) dst[idx++] = v;
  for (float v : o.e_refined) dst[idx++] = v;
  for (float v : o.e_coarse) dst[idx++] = v;
  for (float v : o.E_refined.re) dst[idx++] = v;
  for (float v : o.E_refined.im) dst[idx++] = v;
  for (float v : o.E2_refined) dst[idx++] = v;
  for (float v : o.E2_coarse) dst[idx++] = v;
  dst[idx++] = o.s2_refined;
  dst[idx++] = o.s2_coarse;
  dst[idx++] = o.e2_refined;
  dst[idx++] = o.e2_coarse;
  dst[idx++] = o.y2;
  dst[idx++] = o.s_refined_max_abs;
  dst[idx++] = o.s_coarse_max_abs;
}

void PackAecState(const webrtc::AecState& aec_state, float* dst) {
  size_t idx = 0;
  auto erle_no_onset = aec_state.Erle(false);
  for (float v : erle_no_onset[0]) dst[idx++] = v;
  auto erle_onset = aec_state.Erle(true);
  for (float v : erle_onset[0]) dst[idx++] = v;
  auto erle_unbounded = aec_state.ErleUnbounded();
  for (float v : erle_unbounded[0]) dst[idx++] = v;
  for (float v : aec_state.Erl()) dst[idx++] = v;
  auto reverb = aec_state.GetReverbFrequencyResponse();
  for (float v : reverb) dst[idx++] = v;
  dst[idx++] = aec_state.FullBandErleLog2();
  dst[idx++] = aec_state.ErlTimeDomain();
  dst[idx++] = static_cast<float>(aec_state.MinDirectPathFilterDelay());
  dst[idx++] = aec_state.SaturatedCapture() ? 1.f : 0.f;
  dst[idx++] = aec_state.SaturatedEcho() ? 1.f : 0.f;
  dst[idx++] = aec_state.TransparentModeActive() ? 1.f : 0.f;
  dst[idx++] = aec_state.ReverbDecay(false);
  dst[idx++] = aec_state.ReverbDecay(true);
  dst[idx++] = aec_state.TransitionTriggered() ? 1.f : 0.f;
  dst[idx++] = static_cast<float>(aec_state.FilterLengthBlocks());
  dst[idx++] = aec_state.UsableLinearEstimate() ? 1.f : 0.f;
  dst[idx++] = aec_state.UseLinearFilterOutput() ? 1.f : 0.f;
  dst[idx++] = aec_state.ActiveRender() ? 1.f : 0.f;
}
}  // namespace

void aec3_aecstate_process(aec3_aecstate* s, const float* capture_samples,
                           int capture_saturation, int gain_change,
                           int delay_change, int clock_drift,
                           int has_external_delay, int external_delay_quality,
                           int external_delay_value, float* outputs_out,
                           float* aec_state_out) {
  webrtc::Block capture(1, s->num_capture_channels);
  {
    auto view = capture.View(0, 0);
    for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
      view[i] = capture_samples[i];
    }
  }

  s->aec_state.UpdateCaptureSaturation(capture_saturation != 0);

  webrtc::EchoPathVariability::DelayAdjustment delay_adjustment;
  switch (delay_change) {
    case 1:
      delay_adjustment =
          webrtc::EchoPathVariability::DelayAdjustment::kBufferFlush;
      break;
    case 2:
      delay_adjustment =
          webrtc::EchoPathVariability::DelayAdjustment::kNewDetectedDelay;
      break;
    default:
      delay_adjustment = webrtc::EchoPathVariability::DelayAdjustment::kNone;
      break;
  }
  webrtc::EchoPathVariability variability(gain_change != 0, delay_adjustment,
                                          clock_drift != 0);
  if (variability.AudioPathChanged()) {
    s->subtractor.HandleEchoPathChange(variability);
    s->aec_state.HandleEchoPathChange(variability);
  }

  s->analyzer.Update(*s->render_buffer->GetRenderBuffer(),
                     s->aec_state.MinDirectPathFilterDelay());

  if (s->aec_state.TransitionTriggered()) {
    s->subtractor.ExitInitialState();
  }

  s->subtractor.Process(*s->render_buffer->GetRenderBuffer(), capture,
                        s->analyzer, s->aec_state, s->outputs);

  PackSubtractorOutput(s->outputs[0], outputs_out);

  std::array<float, webrtc::kBlockSize> e;
  FormLinearFilterOutput(s, s->outputs[0], e);

  webrtc::FftData E, Y;
  WindowedPaddedFft(s->fft, capture.View(/*band=*/0, 0), s->y_old, &Y);
  WindowedPaddedFft(s->fft, e, s->e_old, &E);

  std::array<float, webrtc::kFftLengthBy2Plus1> Y2, E2;
  Y.Spectrum(webrtc::Aec3Optimization::kNone, Y2);
  E.Spectrum(webrtc::Aec3Optimization::kNone, E2);

  std::optional<webrtc::DelayEstimate> external_delay;
  if (has_external_delay) {
    webrtc::DelayEstimate::Quality quality =
        external_delay_quality != 0 ? webrtc::DelayEstimate::Quality::kRefined
                                    : webrtc::DelayEstimate::Quality::kCoarse;
    external_delay.emplace(quality,
                          static_cast<size_t>(external_delay_value));
  }

  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> E2_refined_vec = {
      E2};
  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> Y2_vec = {Y2};

  s->aec_state.Update(external_delay, s->subtractor.FilterFrequencyResponses(),
                      s->subtractor.FilterImpulseResponses(),
                      *s->render_buffer->GetRenderBuffer(), E2_refined_vec,
                      Y2_vec, s->outputs);

  PackAecState(s->aec_state, aec_state_out);
}

}  // extern "C"
