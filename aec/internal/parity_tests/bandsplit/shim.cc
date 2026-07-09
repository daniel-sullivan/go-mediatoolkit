//go:build cgo && aec_oracle

// AEC3 band-split parity shim implementation. Compiled by cgo with the
// include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <array>

#include "api/array_view.h"
#include "common_audio/include/audio_util.h"
#include "common_audio/signal_processing/include/signal_processing_library.h"
#include "modules/audio_processing/three_band_filter_bank.h"

struct aec3_three_band {
  webrtc::ThreeBandFilterBank impl;
};

extern "C" {

aec3_three_band* aec3_three_band_create(void) { return new aec3_three_band; }

void aec3_three_band_destroy(aec3_three_band* tb) { delete tb; }

void aec3_three_band_analysis(aec3_three_band* tb, const float* in,
                              float* out) {
  constexpr int kNumBands = webrtc::ThreeBandFilterBank::kNumBands;
  constexpr int kSplit = webrtc::ThreeBandFilterBank::kSplitBandSize;
  std::array<rtc::ArrayView<float>, kNumBands> bands;
  for (int band = 0; band < kNumBands; ++band) {
    bands[band] = rtc::ArrayView<float>(out + band * kSplit, kSplit);
  }
  tb->impl.Analysis(
      rtc::ArrayView<const float, webrtc::ThreeBandFilterBank::kFullBandSize>(
          in, webrtc::ThreeBandFilterBank::kFullBandSize),
      rtc::ArrayView<const rtc::ArrayView<float>, kNumBands>(bands.data(),
                                                             kNumBands));
}

void aec3_three_band_synthesis(aec3_three_band* tb, const float* in,
                               float* out) {
  constexpr int kNumBands = webrtc::ThreeBandFilterBank::kNumBands;
  constexpr int kSplit = webrtc::ThreeBandFilterBank::kSplitBandSize;
  std::array<rtc::ArrayView<float>, kNumBands> bands;
  for (int band = 0; band < kNumBands; ++band) {
    // The Synthesis input views are const in effect; the API reuses
    // the mutable ArrayView element type.
    bands[band] = rtc::ArrayView<float>(const_cast<float*>(in) + band * kSplit,
                                        kSplit);
  }
  tb->impl.Synthesis(
      rtc::ArrayView<const rtc::ArrayView<float>, kNumBands>(bands.data(),
                                                             kNumBands),
      rtc::ArrayView<float, webrtc::ThreeBandFilterBank::kFullBandSize>(
          out, webrtc::ThreeBandFilterBank::kFullBandSize));
}

void aec3_analysis_qmf(const int16_t* in_data, int in_len, int16_t* low_band,
                       int16_t* high_band, int32_t* state1, int32_t* state2) {
  WebRtcSpl_AnalysisQMF(in_data, static_cast<size_t>(in_len), low_band,
                        high_band, state1, state2);
}

void aec3_synthesis_qmf(const int16_t* low_band, const int16_t* high_band,
                        int band_len, int16_t* out_data, int32_t* state1,
                        int32_t* state2) {
  WebRtcSpl_SynthesisQMF(low_band, high_band, static_cast<size_t>(band_len),
                         out_data, state1, state2);
}

void aec3_float_s16_to_s16(const float* src, int n, int16_t* dest) {
  for (int i = 0; i < n; ++i) {
    dest[i] = webrtc::FloatS16ToS16(src[i]);
  }
}

void aec3_s16_to_float_s16(const int16_t* src, int n, float* dest) {
  webrtc::S16ToFloatS16(src, static_cast<size_t>(n), dest);
}

}  // extern "C"
