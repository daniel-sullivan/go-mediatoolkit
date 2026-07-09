//go:build cgo && aec_oracle

// shim.h declares the minimal extern "C" surface this parity slice
// needs from WebRTC's AEC3 (webrtc::EchoCanceller3). Compiled as C++
// by shim.cc and included as a plain C header by cgo.go's preamble.
//
// Only present/usable once the C++ oracle has been fetched+built via
// aec/oracle/fetch.sh (see aec/oracle/VERSION) — this file itself has
// no dependency on the oracle tree (just stddef.h), but shim.cc does,
// so both carry the aec_oracle build tag.
#ifndef AEC3_SHIM_H_
#define AEC3_SHIM_H_

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_canceller aec3_canceller;

// aec3_create builds an EchoCanceller3 with the library's default
// EchoCanceller3Config at sample_rate_hz / num_channels. Returns NULL
// on failure. Caller must aec3_destroy() the result.
aec3_canceller* aec3_create(int sample_rate_hz, int num_channels);

void aec3_destroy(aec3_canceller* c);

// aec3_process runs one 10ms frame through the full AEC3 render+capture
// pipeline: AnalyzeRender(far) then AnalyzeCapture(near_in) +
// ProcessCapture, writing the echo-cancelled result to out. far,
// near_in and out are interleaved float32 PCM, num_channels*frame_len
// samples each (frame_len samples per channel — for 16kHz mono 10ms
// frames, frame_len == 160). Returns 0 on success, -1 on a null
// argument.
int aec3_process(aec3_canceller* c, const float* far, const float* near_in,
                  float* out, int frame_len);

#ifdef __cplusplus
}
#endif

#endif  // AEC3_SHIM_H_
