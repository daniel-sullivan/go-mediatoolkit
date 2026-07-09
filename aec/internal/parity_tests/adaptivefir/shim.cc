//go:build cgo && aec_oracle

// AEC3 adaptive-FIR-filter parity shim implementation. Compiled by
// cgo with the include/library paths resolved by ../run.sh (see
// cgo.go).
#include "shim.h"

#include <array>
#include <memory>
#include <vector>

#include "api/audio/echo_canceller3_config.h"
#include "modules/audio_processing/aec3/adaptive_fir_filter.h"
#include "modules/audio_processing/aec3/adaptive_fir_filter_erl.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/block.h"
#include "modules/audio_processing/aec3/fft_data.h"
#include "modules/audio_processing/aec3/render_delay_buffer.h"
#include "modules/audio_processing/logging/apm_data_dumper.h"

namespace {
constexpr int kSampleRateHz = 16000;
}  // namespace

struct aec3_adaptivefir {
  aec3_adaptivefir(const webrtc::EchoCanceller3Config& config,
                   size_t max_size_partitions, size_t initial_size_partitions,
                   size_t size_change_duration_blocks,
                   size_t num_render_channels)
      : dumper(0),
        render_buffer(webrtc::RenderDelayBuffer::Create(
            config, kSampleRateHz, num_render_channels)),
        num_render_channels(num_render_channels),
        filter(max_size_partitions,
              initial_size_partitions,
              size_change_duration_blocks,
              num_render_channels,
              webrtc::Aec3Optimization::kNone,
              &dumper) {}

  webrtc::ApmDataDumper dumper;
  std::unique_ptr<webrtc::RenderDelayBuffer> render_buffer;
  size_t num_render_channels;
  webrtc::AdaptiveFirFilter filter;
  std::vector<float> impulse_response;
  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> h2;
};

extern "C" {

aec3_adaptivefir* aec3_adaptivefir_create(int max_size_partitions,
                                          int initial_size_partitions,
                                          int size_change_duration_blocks,
                                          int num_render_channels) {
  webrtc::EchoCanceller3Config config;
  return new aec3_adaptivefir(
      config, static_cast<size_t>(max_size_partitions),
      static_cast<size_t>(initial_size_partitions),
      static_cast<size_t>(size_change_duration_blocks),
      static_cast<size_t>(num_render_channels));
}

void aec3_adaptivefir_destroy(aec3_adaptivefir* f) { delete f; }

void aec3_adaptivefir_insert_render_block(aec3_adaptivefir* f,
                                          const float* samples) {
  webrtc::Block block(1, f->num_render_channels);
  for (size_t ch = 0; ch < f->num_render_channels; ++ch) {
    auto view = block.View(0, static_cast<int>(ch));
    for (size_t i = 0; i < webrtc::kBlockSize; ++i) {
      view[i] = samples[ch * webrtc::kBlockSize + i];
    }
  }
  f->render_buffer->Insert(block);
  f->render_buffer->PrepareCaptureProcessing();
}

void aec3_adaptivefir_handle_echo_path_change(aec3_adaptivefir* f) {
  f->filter.HandleEchoPathChange();
}

int aec3_adaptivefir_size_partitions(aec3_adaptivefir* f) {
  return static_cast<int>(f->filter.SizePartitions());
}

void aec3_adaptivefir_set_size_partitions(aec3_adaptivefir* f, int size,
                                          int immediate_effect) {
  f->filter.SetSizePartitions(static_cast<size_t>(size),
                              immediate_effect != 0);
}

int aec3_adaptivefir_max_filter_size_partitions(aec3_adaptivefir* f) {
  return static_cast<int>(f->filter.max_filter_size_partitions());
}

void aec3_adaptivefir_filter(aec3_adaptivefir* f, float* s_re, float* s_im) {
  webrtc::FftData S;
  f->filter.Filter(*f->render_buffer->GetRenderBuffer(), &S);
  for (int k = 0; k < webrtc::kFftLengthBy2Plus1; ++k) {
    s_re[k] = S.re[k];
    s_im[k] = S.im[k];
  }
}

void aec3_adaptivefir_adapt(aec3_adaptivefir* f, const float* g_re,
                           const float* g_im, float* impulse_response,
                           int* len) {
  webrtc::FftData G;
  for (int k = 0; k < webrtc::kFftLengthBy2Plus1; ++k) {
    G.re[k] = g_re[k];
    G.im[k] = g_im[k];
  }
  f->filter.Adapt(*f->render_buffer->GetRenderBuffer(), G,
                  &f->impulse_response);
  *len = static_cast<int>(f->impulse_response.size());
  for (size_t i = 0; i < f->impulse_response.size(); ++i) {
    impulse_response[i] = f->impulse_response[i];
  }
}

void aec3_adaptivefir_adapt_no_ir(aec3_adaptivefir* f, const float* g_re,
                                 const float* g_im) {
  webrtc::FftData G;
  for (int k = 0; k < webrtc::kFftLengthBy2Plus1; ++k) {
    G.re[k] = g_re[k];
    G.im[k] = g_im[k];
  }
  f->filter.Adapt(*f->render_buffer->GetRenderBuffer(), G);
}

int aec3_adaptivefir_compute_frequency_response(aec3_adaptivefir* f,
                                                float* h2) {
  f->filter.ComputeFrequencyResponse(&f->h2);
  for (size_t p = 0; p < f->h2.size(); ++p) {
    for (int k = 0; k < webrtc::kFftLengthBy2Plus1; ++k) {
      h2[p * webrtc::kFftLengthBy2Plus1 + k] = f->h2[p][k];
    }
  }
  return static_cast<int>(f->h2.size());
}

void aec3_adaptivefir_scale_filter(aec3_adaptivefir* f, float factor) {
  f->filter.ScaleFilter(factor);
}

void aec3_adaptivefir_compute_erl(const float* h2, int num_partitions,
                                 float* erl) {
  std::vector<std::array<float, webrtc::kFftLengthBy2Plus1>> h2_vec(
      static_cast<size_t>(num_partitions));
  for (int p = 0; p < num_partitions; ++p) {
    for (int k = 0; k < webrtc::kFftLengthBy2Plus1; ++k) {
      h2_vec[p][k] = h2[p * webrtc::kFftLengthBy2Plus1 + k];
    }
  }
  rtc::ArrayView<float> erl_view(erl, webrtc::kFftLengthBy2Plus1);
  webrtc::ComputeErl(webrtc::Aec3Optimization::kNone, h2_vec, erl_view);
}

}  // extern "C"
