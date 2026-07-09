//go:build cgo && aec_oracle

// shim.cc implements shim.h against the fetched webrtc-audio-processing
// v2.1 oracle (see ../../../oracle/VERSION for provenance; not
// vendored -- this file only compiles when the oracle tree has been
// fetched+built and the aec_oracle build tag is passed, since it
// #includes internal AEC3/AudioBuffer headers that only exist in that
// cache directory, not in this repo).
#include "shim.h"

#include <memory>
#include <optional>

#include "api/audio/echo_canceller3_config.h"
#include "modules/audio_processing/aec3/echo_canceller3.h"
#include "modules/audio_processing/audio_buffer.h"

struct aec3_canceller {
  std::unique_ptr<webrtc::EchoCanceller3> ec;
  int sample_rate_hz;
  int num_render_channels;
  int num_capture_channels;
  // render_buf/capture_buf are each constructed ONCE, here, and reused
  // across every subsequent aec3_analyze_render/aec3_analyze_capture/
  // aec3_process_capture call for this canceller's lifetime -- mirroring
  // the real AudioProcessingImpl, which owns exactly one AudioBuffer per
  // direction for the entire stream lifetime, not a fresh one per
  // ProcessStream call. This matters because AudioBuffer::num_bands()>1
  // (32kHz/48kHz) allocates a SplittingFilter with real per-band
  // analysis/synthesis delay-line state that must persist frame to
  // frame; constructing a new AudioBuffer (and thus a new, cold
  // SplittingFilter) on every call silently discards that memory every
  // frame and diverges from the real oracle starting at the second
  // frame processed -- confirmed by exactly this symptom (mismatches
  // appearing at frame 1, not frame 0, only at 32kHz/48kHz) before this
  // fix.
  std::unique_ptr<webrtc::AudioBuffer> render_buf;
  std::unique_ptr<webrtc::AudioBuffer> capture_buf;
};

extern "C" aec3_canceller* aec3_create(int sample_rate_hz,
                                        int num_render_channels,
                                        int num_capture_channels) {
  auto* c = new aec3_canceller();
  c->sample_rate_hz = sample_rate_hz;
  c->num_render_channels = num_render_channels;
  c->num_capture_channels = num_capture_channels;
  webrtc::EchoCanceller3Config config;
  // Disable the runtime, content-adaptive stereo-detection/downmix
  // machinery (MultiChannelContentDetector, driving
  // num_render_channels_to_aec_ and BufferRenderFrameContent's
  // proper_downmix_needed in echo_canceller3.cc): with the default
  // detect_stereo_content=true, a freshly constructed EchoCanceller3
  // starts with num_render_channels_to_aec_ == 1 regardless of the
  // actual render channel count, and only latches to the true channel
  // count (a) temporarily, per-frame, as soon as any inter-channel
  // sample difference exceeds stereo_detection_threshold (0.0f
  // default -- true from frame 0 for any genuinely decorrelated
  // multichannel render), and (b) persistently, only after
  // stereo_detection_hysteresis_seconds (2s = 200 frames) of
  // consecutive detected content, at which point it reinitializes the
  // whole pipeline. This is exactly the mechanism aec/internal/aec3's
  // EchoCanceller3 (this port) documents as an authorized drop (see
  // echo_canceller3.go's header comment, drop (2)): this port fixes
  // the number of channels actually used for AEC at construction time,
  // equal to the caller-supplied channel count, for the object's
  // entire lifetime -- never content-adaptive, never downmixed. Since
  // the real oracle's default behavior differs (starts downmixed,
  // conditionally upgrades), leaving detect_stereo_content at its
  // default would compare this port against a *different* algorithm
  // for any render channel count > 1, not a configuration difference
  // -- confirmed via lldb-free root-cause reading of
  // multi_channel_content_detector.{h,cc}: with detection disabled,
  // MultiChannelContentDetector's constructor sets
  // persistent_multichannel_content_detected_ =
  // (!detect_stereo_content && num_render_input_channels > 1), i.e.
  // immediately and permanently true whenever there is more than one
  // render channel, exactly matching this port's fixed-channel-count
  // assumption. Disabling it here restores a true apples-to-apples
  // comparison instead of silently testing an oracle feature this
  // port deliberately does not implement.
  config.multi_channel.detect_stereo_content = false;
  c->ec = std::make_unique<webrtc::EchoCanceller3>(
      config, std::nullopt, sample_rate_hz,
      static_cast<size_t>(num_render_channels),
      static_cast<size_t>(num_capture_channels));
  c->render_buf = std::make_unique<webrtc::AudioBuffer>(
      sample_rate_hz, num_render_channels, sample_rate_hz,
      num_render_channels, sample_rate_hz, num_render_channels);
  c->capture_buf = std::make_unique<webrtc::AudioBuffer>(
      sample_rate_hz, num_capture_channels, sample_rate_hz,
      num_capture_channels, sample_rate_hz, num_capture_channels);
  return c;
}

extern "C" void aec3_destroy(aec3_canceller* c) { delete c; }

// AudioBuffer::CopyFrom/CopyTo are deliberately NOT used here: they
// operate in "Float" domain ([-1.0, 1.0], via audio_util.h's
// FloatToFloatS16/FloatS16ToFloat, which CLAMP to that range before
// scaling by 32768) -- the contract a real top-level
// AudioProcessingImpl::ProcessStream caller uses. This port, like
// every other parity slice in this repo, operates directly in
// FloatS16 domain (unclamped, [-32768, 32768]) throughout, matching
// aec/internal/aec3.EchoCanceller3's own documented convention. So
// interleaved samples are written straight into AudioBuffer::channels()
// (its FloatS16-domain full-band storage) below, bypassing CopyFrom's
// Float-domain clamp+rescale entirely.
//
// Similarly, EchoCanceller3::AnalyzeRender/AnalyzeCapture/ProcessCapture
// all read/write AudioBuffer::split_bands() (see echo_canceller3.cc),
// not channels() -- upstream expects the caller (AudioProcessingImpl)
// to have already called SplitIntoFrequencyBands()/MergeFrequencyBands()
// around these calls; there is no AudioProcessingImpl in this shim, so
// it must do so explicitly itself, exactly mirroring what this port's
// own EchoCanceller3 (echo_canceller3.go's processCapture/AnalyzeRender)
// does internally (see that file's header comment on this exact point).
//
// At 16kHz, AudioBuffer::SplitIntoFrequencyBands/MergeFrequencyBands
// must NOT be called at all: num_bands()==1 there, so the constructor
// never allocates splitting_filter_/split_data_ (both stay null), and
// both methods unconditionally dereference splitting_filter_ -- calling
// either is a null-pointer crash, not a harmless no-op (split_bands()
// itself safely aliases channels() at num_bands()==1, but the
// Split/Merge *methods* do not check that before dereferencing the
// filter). AudioProcessingImpl itself guards every call site with
// SampleRateSupportsMultiBand(rate) (audio_processing_impl.cc), i.e.
// only 32kHz/48kHz; multiBand(rate) below mirrors that guard exactly.
static bool multiBand(int rate) { return rate == 32000 || rate == 48000; }

extern "C" void aec3_analyze_render(aec3_canceller* c,
                                     const float* render_interleaved,
                                     int frame_len) {
  const int ch = c->num_render_channels;
  const int rate = c->sample_rate_hz;
  webrtc::AudioBuffer* buf = c->render_buf.get();
  float* const* dst = buf->channels();
  for (int i = 0; i < ch; i++) {
    for (int j = 0; j < frame_len; j++) {
      dst[i][j] = render_interleaved[j * ch + i];
    }
  }
  if (multiBand(rate)) {
    buf->SplitIntoFrequencyBands();
  }
  c->ec->AnalyzeRender(buf);
}

extern "C" void aec3_analyze_capture(aec3_canceller* c,
                                      const float* capture_interleaved,
                                      int frame_len) {
  const int ch = c->num_capture_channels;
  webrtc::AudioBuffer* buf = c->capture_buf.get();
  float* const* dst = buf->channels();
  for (int i = 0; i < ch; i++) {
    for (int j = 0; j < frame_len; j++) {
      dst[i][j] = capture_interleaved[j * ch + i];
    }
  }
  // EchoCanceller3::AnalyzeCapture reads only AudioBuffer::channels_const()
  // (full-band data -- see its body in echo_canceller3.cc, which just runs
  // DetectSaturation over each full-band channel), never split_bands(). The
  // real caller, AudioProcessingImpl::ProcessCaptureStreamLocked, calls
  // AnalyzeCapture(capture_buffer) BEFORE the frame's one-and-only
  // capture_buffer->SplitIntoFrequencyBands() call (see
  // audio_processing_impl.cc: submodules_.echo_controller->AnalyzeCapture at
  // line ~1314, SplitIntoFrequencyBands at line ~1334, i.e. strictly later,
  // right before ProcessCapture). This function must NOT split here: doing
  // so was an extra, premature SplitIntoFrequencyBands() call on this same
  // persistent capture_buf, which corrupted the two-band QMF's persistent
  // analysis filter state (advancing it once here and a second time, on the
  // same frame's samples, inside aec3_process_capture's own -- correct --
  // split) whenever num_bands() > 1 (32kHz/48kHz). Confirmed via a temporary
  // debug dump comparing this port's own captureSplit right after its own
  // (single) SplitIntoFrequencyBands call against this shim's: they already
  // diverged by small amounts before any AEC processing ran, at 32kHz only,
  // consistent with a doubled QMF analysis pass, not a genuine Go-port
  // suppression/gain defect.
  c->ec->AnalyzeCapture(buf);
}

extern "C" void aec3_process_capture(aec3_canceller* c,
                                      float* capture_interleaved_inout,
                                      int frame_len, int level_change) {
  const int ch = c->num_capture_channels;
  const int rate = c->sample_rate_hz;
  webrtc::AudioBuffer* buf = c->capture_buf.get();
  float* const* dst = buf->channels();
  for (int i = 0; i < ch; i++) {
    for (int j = 0; j < frame_len; j++) {
      dst[i][j] = capture_interleaved_inout[j * ch + i];
    }
  }
  if (multiBand(rate)) {
    buf->SplitIntoFrequencyBands();
  }

  c->ec->ProcessCapture(buf, level_change != 0);

  if (multiBand(rate)) {
    buf->MergeFrequencyBands();
  }
  const float* const* src = buf->channels();
  for (int i = 0; i < ch; i++) {
    for (int j = 0; j < frame_len; j++) {
      capture_interleaved_inout[j * ch + i] = src[i][j];
    }
  }
}

extern "C" void aec3_set_audio_buffer_delay(aec3_canceller* c, int delay_ms) {
  c->ec->SetAudioBufferDelay(delay_ms);
}

extern "C" void aec3_get_metrics(aec3_canceller* c, double* echo_return_loss,
                                  double* echo_return_loss_enhancement,
                                  int* delay_ms) {
  webrtc::EchoControl::Metrics m = c->ec->GetMetrics();
  *echo_return_loss = m.echo_return_loss;
  *echo_return_loss_enhancement = m.echo_return_loss_enhancement;
  *delay_ms = m.delay_ms;
}
