//go:build cgo && aec_oracle

// shim.h declares the extern "C" surface this parity slice needs from
// the fetched AEC3 C++ oracle's top-level webrtc::EchoCanceller3 (the
// same class aec/internal/aec3/echo_canceller3.go is a 1:1 port of).
// Unlike the lower-level slices (adaptivefir, subtractor, aecstate,
// echoremover, ...), this slice drives the REAL, unmodified public
// EchoCanceller3/AudioBuffer API end to end -- proving Part 4-6's
// integration of the whole ported pipeline, not just one component in
// isolation.
//
// Compiled as C++ by shim.cc and included as a plain C header by
// cgo.go's preamble. Only present/usable once the C++ oracle has been
// fetched+built via aec/oracle/fetch.sh (see aec/oracle/VERSION) --
// this file itself has no dependency on the oracle tree (just
// stddef.h), but shim.cc does, so both carry the aec_oracle build
// tag.
#ifndef AEC3_EC3_SHIM_H_
#define AEC3_EC3_SHIM_H_

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_canceller aec3_canceller;

// aec3_create builds an EchoCanceller3 with the library's default
// (default-constructed) EchoCanceller3Config -- the same config
// aec.Canceller's buildEngineAndBuffers passes to
// aec3.NewEchoCanceller3 via aec3.DefaultConfig() -- at
// sample_rate_hz, with num_render_channels render channels and
// num_capture_channels capture channels (independently sized, exactly
// as the real constructor allows). Returns NULL on failure. Caller
// must aec3_destroy() the result.
aec3_canceller* aec3_create(int sample_rate_hz, int num_render_channels,
                             int num_capture_channels);

void aec3_destroy(aec3_canceller* c);

// aec3_analyze_render buffers one 10ms render frame (interleaved
// float32 FloatS16-domain PCM, num_render_channels*frame_len samples,
// frame_len samples per channel) via EchoCanceller3::AnalyzeRender.
void aec3_analyze_render(aec3_canceller* c, const float* render_interleaved,
                          int frame_len);

// aec3_analyze_capture reports one 10ms capture frame (interleaved
// float32 FloatS16-domain PCM, num_capture_channels*frame_len samples)
// to EchoCanceller3::AnalyzeCapture (saturation detection only -- does
// not mutate the signal).
void aec3_analyze_capture(aec3_canceller* c, const float* capture_interleaved,
                           int frame_len);

// aec3_process_capture removes the estimated echo from
// capture_interleaved_inout (interleaved float32 FloatS16-domain PCM,
// num_capture_channels*frame_len samples) in place, via
// EchoCanceller3::ProcessCapture(AudioBuffer*, level_change).
void aec3_process_capture(aec3_canceller* c, float* capture_interleaved_inout,
                           int frame_len, int level_change);

// aec3_set_audio_buffer_delay forwards to
// EchoCanceller3::SetAudioBufferDelay.
void aec3_set_audio_buffer_delay(aec3_canceller* c, int delay_ms);

// aec3_get_metrics forwards to EchoCanceller3::GetMetrics. There is no
// corresponding accessor for clockdrift level here: unlike
// echo_return_loss/echo_return_loss_enhancement/delay_ms (all part of
// EchoControl::Metrics), clockdrift is not exposed anywhere on
// EchoCanceller3's or EchoControl's public interface upstream -- see
// this slice's parity_test.go doc comment for why that is not a
// coverage gap in the Go port (clockdrift is already bit-exact-parity
// tested at the RenderDelayController/AecState/EchoRemover slices,
// which do have the necessary internal access).
void aec3_get_metrics(aec3_canceller* c, double* echo_return_loss,
                      double* echo_return_loss_enhancement, int* delay_ms);

#ifdef __cplusplus
}
#endif

#endif  // AEC3_EC3_SHIM_H_
