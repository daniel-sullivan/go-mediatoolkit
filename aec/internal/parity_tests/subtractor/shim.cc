//go:build cgo && aec_oracle

// AEC3 subtractor parity shim implementation. Compiled by cgo with the
// include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <optional>
#include <vector>

#include "api/audio/echo_canceller3_config.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/aec_state.h"
#include "modules/audio_processing/aec3/block.h"
#include "modules/audio_processing/aec3/render_delay_buffer.h"
#include "modules/audio_processing/aec3/render_signal_analyzer.h"
#include "modules/audio_processing/aec3/subtractor.h"
#include "modules/audio_processing/aec3/subtractor_output.h"
#include "modules/audio_processing/logging/apm_data_dumper.h"

namespace {
constexpr int kSampleRateHz = 16000;
}  // namespace

struct aec3_subtractor {
  aec3_subtractor(const webrtc::EchoCanceller3Config& config,
                  size_t num_render_channels, size_t num_capture_channels)
      : dumper(0),
        num_render_channels(num_render_channels),
        num_capture_channels(num_capture_channels),
        render_buffer(webrtc::RenderDelayBuffer::Create(
            config, kSampleRateHz, num_render_channels)),
        analyzer(config),
        aec_state(config, num_capture_channels),
        subtractor(config,
                  num_render_channels,
                  num_capture_channels,
                  &dumper,
                  webrtc::Aec3Optimization::kNone),
        outputs(num_capture_channels) {}

  webrtc::ApmDataDumper dumper;
  size_t num_render_channels;
  size_t num_capture_channels;
  std::unique_ptr<webrtc::RenderDelayBuffer> render_buffer;
  webrtc::RenderSignalAnalyzer analyzer;
  webrtc::AecState aec_state;
  webrtc::Subtractor subtractor;
  std::vector<webrtc::SubtractorOutput> outputs;
};

extern "C" {

aec3_subtractor* aec3_subtractor_create(int num_render_channels,
                                        int num_capture_channels) {
  webrtc::EchoCanceller3Config config;
  return new aec3_subtractor(config, static_cast<size_t>(num_render_channels),
                            static_cast<size_t>(num_capture_channels));
}

void aec3_subtractor_destroy(aec3_subtractor* s) { delete s; }

void aec3_subtractor_insert_render_block(aec3_subtractor* s,
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

void aec3_subtractor_update_render_signal_analyzer(aec3_subtractor* s) {
  s->analyzer.Update(*s->render_buffer->GetRenderBuffer(), std::nullopt);
}

void aec3_subtractor_handle_echo_path_change(aec3_subtractor* s,
                                             int gain_change,
                                             int delay_change,
                                             int clock_drift) {
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
  s->subtractor.HandleEchoPathChange(variability);
}

void aec3_subtractor_exit_initial_state(aec3_subtractor* s) {
  s->subtractor.ExitInitialState();
}

void aec3_subtractor_set_saturated_capture(aec3_subtractor* s,
                                           int saturated) {
  s->aec_state.UpdateCaptureSaturation(saturated != 0);
}

namespace {
void PackOutput(const webrtc::SubtractorOutput& o, float* dst) {
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
}  // namespace

void aec3_subtractor_process(aec3_subtractor* s, const float* capture_samples,
                             float* outputs_out) {
  webrtc::Block capture(1, s->num_capture_channels);
  for (size_t ch = 0; ch < s->num_capture_channels; ++ch) {
    auto view = capture.View(0, static_cast<int>(ch));
    for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
      view[i] = capture_samples[ch * webrtc::kBlockSize + i];
    }
  }

  s->subtractor.Process(*s->render_buffer->GetRenderBuffer(), capture,
                        s->analyzer, s->aec_state, s->outputs);

  for (size_t ch = 0; ch < s->num_capture_channels; ++ch) {
    PackOutput(s->outputs[ch],
              outputs_out + ch * AEC3_SUBTRACTOR_OUTPUT_FLOATS);
  }
}

int aec3_subtractor_impulse_response_len(aec3_subtractor* s, int channel) {
  return static_cast<int>(
      s->subtractor.FilterImpulseResponses()[static_cast<size_t>(channel)]
          .size());
}

void aec3_subtractor_get_impulse_response(aec3_subtractor* s, int channel,
                                          float* out) {
  const auto& ir =
      s->subtractor.FilterImpulseResponses()[static_cast<size_t>(channel)];
  for (size_t i = 0; i < ir.size(); ++i) {
    out[i] = ir[i];
  }
}

int aec3_subtractor_frequency_response_partitions(aec3_subtractor* s,
                                                  int channel) {
  return static_cast<int>(
      s->subtractor.FilterFrequencyResponses()[static_cast<size_t>(channel)]
          .size());
}

void aec3_subtractor_get_frequency_response(aec3_subtractor* s, int channel,
                                            float* out) {
  const auto& fr =
      s->subtractor.FilterFrequencyResponses()[static_cast<size_t>(channel)];
  for (size_t p = 0; p < fr.size(); ++p) {
    for (int k = 0; k < webrtc::kFftLengthBy2Plus1; ++k) {
      out[p * webrtc::kFftLengthBy2Plus1 + k] = fr[p][k];
    }
  }
}

}  // extern "C"
