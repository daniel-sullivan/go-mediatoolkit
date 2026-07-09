//go:build cgo && aec_oracle

// AEC3 decimator parity shim implementation. Compiled by cgo with the
// include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include "api/array_view.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/decimator.h"

struct aec3_decimator {
  explicit aec3_decimator(size_t down_sampling_factor)
      : impl(down_sampling_factor) {}
  webrtc::Decimator impl;
};

extern "C" {

aec3_decimator* aec3_decimator_create(int down_sampling_factor) {
  return new aec3_decimator(static_cast<size_t>(down_sampling_factor));
}

void aec3_decimator_destroy(aec3_decimator* d) { delete d; }

void aec3_decimator_decimate(aec3_decimator* d, const float* in, float* out,
                             int out_len) {
  d->impl.Decimate(
      rtc::ArrayView<const float>(in, webrtc::kBlockSize),
      rtc::ArrayView<float>(out, static_cast<size_t>(out_len)));
}

}  // extern "C"
