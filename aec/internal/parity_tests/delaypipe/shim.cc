//go:build cgo && aec_oracle

// AEC3 delay-pipeline parity shim implementation. Compiled by cgo with
// the include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <memory>
#include <optional>

#include "api/audio/echo_canceller3_config.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/block.h"
#include "modules/audio_processing/aec3/delay_estimate.h"
#include "modules/audio_processing/aec3/downsampled_render_buffer.h"
#include "modules/audio_processing/aec3/render_delay_buffer.h"
#include "modules/audio_processing/aec3/render_delay_controller.h"

// Mirrors BlockProcessorImpl's relevant state (block_processor.cc),
// dropping the ApmDataDumper logging and the echo-remover/metrics
// fields this parity slice has no counterpart for.
struct aec3_delaypipe {
  aec3_delaypipe(const webrtc::EchoCanceller3Config& config,
                int sample_rate_hz, int num_render_channels,
                int num_capture_channels)
      : num_bands(static_cast<int>(webrtc::NumBandsForRate(sample_rate_hz))),
        render_buffer(webrtc::RenderDelayBuffer::Create(
            config, sample_rate_hz,
            static_cast<size_t>(num_render_channels))),
        delay_controller(webrtc::RenderDelayController::Create(
            config, sample_rate_hz,
            static_cast<size_t>(num_capture_channels))) {}

  int num_bands;
  std::unique_ptr<webrtc::RenderDelayBuffer> render_buffer;
  std::unique_ptr<webrtc::RenderDelayController> delay_controller;
  bool render_properly_started = false;
  bool capture_properly_started = false;
  webrtc::RenderDelayBuffer::BufferingEvent render_event =
      webrtc::RenderDelayBuffer::BufferingEvent::kNone;
};

extern "C" {

aec3_delaypipe* aec3_delaypipe_create(int sample_rate_hz,
                                      int num_render_channels,
                                      int num_capture_channels) {
  webrtc::EchoCanceller3Config config;
  return new aec3_delaypipe(config, sample_rate_hz, num_render_channels,
                            num_capture_channels);
}

void aec3_delaypipe_destroy(aec3_delaypipe* p) { delete p; }

int aec3_delaypipe_max_delay(aec3_delaypipe* p) {
  return static_cast<int>(p->render_buffer->MaxDelay());
}

void aec3_delaypipe_buffer_render(aec3_delaypipe* p, const float* samples) {
  webrtc::Block block(p->num_bands, 1);
  auto view = block.View(0, 0);
  for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
    view[i] = samples[i];
  }

  // BlockProcessorImpl::BufferRender, minus the metrics-only counter.
  p->render_event = p->render_buffer->Insert(block);
  p->render_properly_started = true;
  p->delay_controller->LogRenderCall();
}

int aec3_delaypipe_process_capture(aec3_delaypipe* p, const float* samples,
                                   int* has_estimate, int* quality,
                                   int* delay_blocks, int* blocks_since_change,
                                   int* blocks_since_update, int* buffer_delay,
                                   int* has_clockdrift) {
  webrtc::Block capture_block(p->num_bands, 1);
  {
    auto view = capture_block.View(0, 0);
    for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
      view[i] = samples[i];
    }
  }

  // BlockProcessorImpl::ProcessCapture, minus the echo-path-variability
  // side channel and echo-remover/metrics/logging calls (no
  // counterparts in this port).
  if (p->render_properly_started) {
    if (!p->capture_properly_started) {
      p->capture_properly_started = true;
      p->render_buffer->Reset();
      p->delay_controller->Reset(true);
    }
  } else {
    p->render_buffer->HandleSkippedCaptureProcessing();
    *has_estimate = 0;
    *buffer_delay = static_cast<int>(p->render_buffer->Delay());
    *has_clockdrift = p->delay_controller->HasClockdrift() ? 1 : 0;
    return 0;
  }

  if (p->render_event ==
          webrtc::RenderDelayBuffer::BufferingEvent::kRenderOverrun &&
      p->render_properly_started) {
    p->delay_controller->Reset(true);
  }
  p->render_event = webrtc::RenderDelayBuffer::BufferingEvent::kNone;

  webrtc::RenderDelayBuffer::BufferingEvent buffer_event =
      p->render_buffer->PrepareCaptureProcessing();
  if (buffer_event ==
      webrtc::RenderDelayBuffer::BufferingEvent::kRenderUnderrun) {
    p->delay_controller->Reset(false);
  }

  std::optional<webrtc::DelayEstimate> estimated_delay =
      p->delay_controller->GetDelay(p->render_buffer->GetDownsampledRenderBuffer(),
                                    p->render_buffer->Delay(), capture_block);

  int delay_changed = 0;
  if (estimated_delay) {
    delay_changed =
        p->render_buffer->AlignFromDelay(estimated_delay->delay) ? 1 : 0;
  }

  *has_clockdrift = p->delay_controller->HasClockdrift() ? 1 : 0;
  *buffer_delay = static_cast<int>(p->render_buffer->Delay());

  if (estimated_delay) {
    *has_estimate = 1;
    *quality = static_cast<int>(estimated_delay->quality);
    *delay_blocks = static_cast<int>(estimated_delay->delay);
    *blocks_since_change =
        static_cast<int>(estimated_delay->blocks_since_last_change);
    *blocks_since_update =
        static_cast<int>(estimated_delay->blocks_since_last_update);
  } else {
    *has_estimate = 0;
  }
  return delay_changed;
}

}  // extern "C"
