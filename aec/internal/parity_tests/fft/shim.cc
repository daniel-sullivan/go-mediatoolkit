//go:build cgo && aec_oracle

// AEC3 FFT parity shim implementation. Compiled by cgo with the
// include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <algorithm>
#include <array>

#include "api/array_view.h"
#include "common_audio/third_party/ooura/fft_size_128/ooura_fft.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/aec3_fft.h"
#include "modules/audio_processing/aec3/fft_data.h"

namespace {

// The oracle build has NEON disabled and this shim never claims SSE2
// support, so both objects below always take the scalar code path —
// the path fft.go ports.
const webrtc::OouraFft& Ooura() {
  static const webrtc::OouraFft* fft =
      new webrtc::OouraFft(/*sse2_available=*/false);
  return *fft;
}

const webrtc::Aec3Fft& Aec3() {
  static const webrtc::Aec3Fft* fft = new webrtc::Aec3Fft();
  return *fft;
}

void CopyOut(const webrtc::FftData& X, float* re, float* im) {
  std::copy(X.re.begin(), X.re.end(), re);
  std::copy(X.im.begin(), X.im.end(), im);
}

}  // namespace

extern "C" {

void aec3_ooura_fft(float* a) { Ooura().Fft(a); }

void aec3_ooura_ifft(float* a) { Ooura().InverseFft(a); }

void aec3_fft_zero_padded(const float* x, int window, float* re, float* im) {
  webrtc::FftData X;
  Aec3().ZeroPaddedFft(rtc::ArrayView<const float>(x, webrtc::kFftLengthBy2),
                       static_cast<webrtc::Aec3Fft::Window>(window), &X);
  CopyOut(X, re, im);
}

void aec3_fft_padded(const float* x, const float* x_old, int window, float* re,
                     float* im) {
  webrtc::FftData X;
  Aec3().PaddedFft(rtc::ArrayView<const float>(x, webrtc::kFftLengthBy2),
                   rtc::ArrayView<const float>(x_old, webrtc::kFftLengthBy2),
                   static_cast<webrtc::Aec3Fft::Window>(window), &X);
  CopyOut(X, re, im);
}

}  // extern "C"
