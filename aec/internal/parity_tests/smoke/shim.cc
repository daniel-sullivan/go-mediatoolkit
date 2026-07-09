//go:build cgo && aec_oracle

// shim.cc implements shim.h against the fetched webrtc-audio-processing
// v2.1 oracle (see ../../../oracle/VERSION for provenance; not
// vendored — this file only compiles when the oracle tree has been
// fetched+built and the aec_oracle build tag is passed, since it
// #includes internal AEC3/AudioBuffer headers that only exist in that
// cache directory, not in this repo).
#include "shim.h"

#include <memory>
#include <vector>

#include "api/audio/echo_canceller3_config.h"
#include "modules/audio_processing/aec3/echo_canceller3.h"
#include "modules/audio_processing/audio_buffer.h"

struct aec3_canceller {
  std::unique_ptr<webrtc::EchoCanceller3> ec;
  int sample_rate_hz;
  int num_channels;
};

extern "C" aec3_canceller* aec3_create(int sample_rate_hz, int num_channels) {
  auto* c = new aec3_canceller();
  c->sample_rate_hz = sample_rate_hz;
  c->num_channels = num_channels;
  webrtc::EchoCanceller3Config config;
  c->ec = std::make_unique<webrtc::EchoCanceller3>(
      config, std::nullopt, sample_rate_hz, static_cast<size_t>(num_channels),
      static_cast<size_t>(num_channels));
  return c;
}

extern "C" void aec3_destroy(aec3_canceller* c) { delete c; }

extern "C" int aec3_process(aec3_canceller* c, const float* far,
                             const float* near_in, float* out,
                             int frame_len) {
  if (!c || !far || !near_in || !out) return -1;
  const int ch = c->num_channels;
  const int rate = c->sample_rate_hz;

  // Render (far-end) path: AnalyzeRender expects an AudioBuffer built
  // from de-interleaved channel pointers.
  {
    webrtc::AudioBuffer buf(rate, ch, rate, ch, rate, ch);
    std::vector<std::vector<float>> deinterleaved(ch,
                                                    std::vector<float>(frame_len));
    std::vector<float*> chan_ptrs(ch);
    for (int i = 0; i < ch; i++) {
      for (int j = 0; j < frame_len; j++) deinterleaved[i][j] = far[j * ch + i];
      chan_ptrs[i] = deinterleaved[i].data();
    }
    buf.CopyFrom(chan_ptrs.data(), webrtc::StreamConfig(rate, ch));
    c->ec->AnalyzeRender(&buf);
  }

  // Capture (near-end) path: AnalyzeCapture + ProcessCapture mutate the
  // AudioBuffer in place with the echo-cancelled signal.
  webrtc::AudioBuffer cap(rate, ch, rate, ch, rate, ch);
  std::vector<std::vector<float>> deinterleaved(ch, std::vector<float>(frame_len));
  std::vector<float*> chan_ptrs(ch);
  for (int i = 0; i < ch; i++) {
    for (int j = 0; j < frame_len; j++) deinterleaved[i][j] = near_in[j * ch + i];
    chan_ptrs[i] = deinterleaved[i].data();
  }
  cap.CopyFrom(chan_ptrs.data(), webrtc::StreamConfig(rate, ch));

  c->ec->AnalyzeCapture(&cap);
  c->ec->ProcessCapture(&cap, /*level_change=*/false);

  cap.CopyTo(webrtc::StreamConfig(rate, ch), chan_ptrs.data());
  for (int i = 0; i < ch; i++) {
    for (int j = 0; j < frame_len; j++) out[j * ch + i] = deinterleaved[i][j];
  }
  return 0;
}
