//go:build cgo && aec_oracle

// AEC3 suppressor parity shim implementation. Compiled by cgo with the
// include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <algorithm>
#include <array>
#include <vector>

#include "api/audio/echo_canceller3_config.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/aec_state.h"
#include "modules/audio_processing/aec3/block.h"
#include "modules/audio_processing/aec3/comfort_noise_generator.h"
#include "modules/audio_processing/aec3/fft_data.h"
#include "modules/audio_processing/aec3/render_signal_analyzer.h"
#include "modules/audio_processing/aec3/suppression_filter.h"
#include "modules/audio_processing/aec3/suppression_gain.h"

namespace {
constexpr int kSampleRateHz = 16000;
}  // namespace

struct aec3_suppressor {
  aec3_suppressor()
      : config(),
        aec_state(config, 1),
        analyzer(config),
        cng(config, webrtc::Aec3Optimization::kNone, 1),
        gain(config, webrtc::Aec3Optimization::kNone, kSampleRateHz, 1),
        filter(webrtc::Aec3Optimization::kNone, kSampleRateHz, 1) {}

  webrtc::EchoCanceller3Config config;
  webrtc::AecState aec_state;
  webrtc::RenderSignalAnalyzer analyzer;
  webrtc::ComfortNoiseGenerator cng;
  webrtc::SuppressionGain gain;
  webrtc::SuppressionFilter filter;
};

extern "C" {

aec3_suppressor* aec3_suppressor_create(void) { return new aec3_suppressor(); }

void aec3_suppressor_destroy(aec3_suppressor* s) { delete s; }

void aec3_suppressor_step(
    aec3_suppressor* s, const float* render, const float* capture_spectrum,
    int saturated_capture, const float* nearend_spectrum,
    const float* echo_spectrum, const float* residual_echo_spectrum,
    const float* residual_echo_spectrum_unbounded, int clock_drift,
    const float* e_lowest_re, const float* e_lowest_im, float* lower_noise_re,
    float* lower_noise_im, float* upper_noise_re, float* upper_noise_im,
    float* noise_spectrum_out, float* gain_out, float* filter_out,
    float* scalar_out) {
  webrtc::Block render_block(1, 1);
  {
    auto view = render_block.View(/*band=*/0, /*channel=*/0);
    for (size_t i = 0; i < webrtc::kBlockSize; ++i) view[i] = render[i];
  }

  std::array<float, webrtc::kFftLengthBy2Plus1> Y2;
  std::copy(capture_spectrum, capture_spectrum + webrtc::kFftLengthBy2Plus1,
           Y2.begin());
  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> Y2_vec = {Y2};

  std::vector<webrtc::FftData> lower_noise(1), upper_noise(1);
  s->cng.Compute(saturated_capture != 0, Y2_vec, lower_noise, upper_noise);

  std::copy(lower_noise[0].re.begin(), lower_noise[0].re.end(), lower_noise_re);
  std::copy(lower_noise[0].im.begin(), lower_noise[0].im.end(), lower_noise_im);
  std::copy(upper_noise[0].re.begin(), upper_noise[0].re.end(), upper_noise_re);
  std::copy(upper_noise[0].im.begin(), upper_noise[0].im.end(), upper_noise_im);
  std::copy(s->cng.NoiseSpectrum()[0].begin(), s->cng.NoiseSpectrum()[0].end(),
           noise_spectrum_out);

  std::array<float, webrtc::kFftLengthBy2Plus1> nearend, echo, residual,
      residual_unbounded;
  std::copy(nearend_spectrum, nearend_spectrum + webrtc::kFftLengthBy2Plus1,
           nearend.begin());
  std::copy(echo_spectrum, echo_spectrum + webrtc::kFftLengthBy2Plus1,
           echo.begin());
  std::copy(residual_echo_spectrum,
           residual_echo_spectrum + webrtc::kFftLengthBy2Plus1,
           residual.begin());
  std::copy(residual_echo_spectrum_unbounded,
           residual_echo_spectrum_unbounded + webrtc::kFftLengthBy2Plus1,
           residual_unbounded.begin());

  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> nearend_vec = {
      nearend};
  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> echo_vec = {echo};
  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> residual_vec = {
      residual};
  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>>
      residual_unbounded_vec = {residual_unbounded};

  float high_bands_gain = 0.f;
  std::array<float, webrtc::kFftLengthBy2Plus1> low_band_gain;
  s->gain.GetGain(nearend_vec, echo_vec, residual_vec, residual_unbounded_vec,
                  s->cng.NoiseSpectrum(), s->analyzer, s->aec_state,
                  render_block, clock_drift != 0, &high_bands_gain,
                  &low_band_gain);

  std::copy(low_band_gain.begin(), low_band_gain.end(), gain_out);
  scalar_out[0] = high_bands_gain;
  scalar_out[1] = s->gain.IsDominantNearend() ? 1.f : 0.f;

  webrtc::FftData E;
  std::copy(e_lowest_re, e_lowest_re + webrtc::kFftLengthBy2Plus1,
           E.re.begin());
  std::copy(e_lowest_im, e_lowest_im + webrtc::kFftLengthBy2Plus1,
           E.im.begin());
  std::vector<webrtc::FftData> e_lowest_vec = {E};

  webrtc::Block e(1, 1);
  s->filter.ApplyGain(lower_noise, upper_noise, low_band_gain, high_bands_gain,
                      e_lowest_vec, &e);

  auto e_view = e.View(/*band=*/0, /*channel=*/0);
  for (size_t i = 0; i < webrtc::kBlockSize; ++i) filter_out[i] = e_view[i];
}

}  // extern "C"
