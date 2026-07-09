//go:build cgo && aec_oracle

// AEC3 echoremover parity shim implementation. Compiled by cgo with
// the include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <memory>
#include <optional>

#include "api/audio/echo_canceller3_config.h"
#include "api/audio/echo_control.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/block.h"
#include "modules/audio_processing/aec3/delay_estimate.h"
#include "modules/audio_processing/aec3/echo_path_variability.h"
#include "modules/audio_processing/aec3/echo_remover.h"
#include "modules/audio_processing/aec3/render_delay_buffer.h"

namespace {
constexpr int kSampleRateHz = 16000;
constexpr int kNumRenderChannels = 1;
constexpr int kNumCaptureChannels = 1;
}  // namespace

struct aec3_echoremover {
  aec3_echoremover()
      : config(),
        render_buffer(webrtc::RenderDelayBuffer::Create(
            config, kSampleRateHz, kNumRenderChannels)),
        remover(webrtc::EchoRemover::Create(config, kSampleRateHz,
                                            kNumRenderChannels,
                                            kNumCaptureChannels)) {}

  webrtc::EchoCanceller3Config config;
  std::unique_ptr<webrtc::RenderDelayBuffer> render_buffer;
  std::unique_ptr<webrtc::EchoRemover> remover;
};

extern "C" {

aec3_echoremover* aec3_echoremover_create(void) {
  return new aec3_echoremover();
}

void aec3_echoremover_destroy(aec3_echoremover* s) { delete s; }

void aec3_echoremover_insert_render_block(aec3_echoremover* s,
                                          const float* samples) {
  webrtc::Block block(1, kNumRenderChannels);
  auto view = block.View(/*band=*/0, /*channel=*/0);
  for (size_t i = 0; i < webrtc::kBlockSize; ++i) view[i] = samples[i];
  s->render_buffer->Insert(block);
  s->render_buffer->PrepareCaptureProcessing();
}

void aec3_echoremover_process(aec3_echoremover* s, float* capture_samples,
                              int capture_saturation, int gain_change,
                              int delay_change, int clock_drift,
                              int has_external_delay,
                              int external_delay_quality,
                              int external_delay_value,
                              float* linear_output_out, double* metrics_out) {
  webrtc::Block capture(1, kNumCaptureChannels);
  {
    auto view = capture.View(/*band=*/0, /*channel=*/0);
    for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
      view[i] = capture_samples[i];
    }
  }

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

  std::optional<webrtc::DelayEstimate> external_delay;
  if (has_external_delay) {
    webrtc::DelayEstimate::Quality quality =
        external_delay_quality != 0 ? webrtc::DelayEstimate::Quality::kRefined
                                    : webrtc::DelayEstimate::Quality::kCoarse;
    external_delay.emplace(quality,
                          static_cast<size_t>(external_delay_value));
  }

  webrtc::Block linear_output(1, kNumCaptureChannels);

  s->remover->ProcessCapture(variability, capture_saturation != 0,
                             external_delay, s->render_buffer->GetRenderBuffer(),
                             &linear_output, &capture);

  auto capture_view = capture.View(/*band=*/0, /*channel=*/0);
  for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
    capture_samples[i] = capture_view[i];
  }
  auto linear_view = linear_output.View(/*band=*/0, /*channel=*/0);
  for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
    linear_output_out[i] = linear_view[i];
  }

  webrtc::EchoControl::Metrics metrics;
  s->remover->GetMetrics(&metrics);
  metrics_out[0] = metrics.echo_return_loss;
  metrics_out[1] = metrics.echo_return_loss_enhancement;
}

}  // extern "C"
