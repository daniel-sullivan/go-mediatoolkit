//go:build cgo && aec_oracle

// AEC3 FFT parity shim: extern "C" wrappers around the oracle's
// webrtc::OouraFft and webrtc::Aec3Fft (see cgo.go for why this is a
// separate translation unit and how it gets compiled/linked).
#ifndef AEC_PARITY_FFT_SHIM_H_
#define AEC_PARITY_FFT_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

// In-place 128-point forward/inverse real FFT in Ooura's packed
// layout. a points at 128 floats.
void aec3_ooura_fft(float* a);
void aec3_ooura_ifft(float* a);

// Aec3Fft wrappers. Window values match webrtc::Aec3Fft::Window's
// declaration order: 0 = kRectangular, 1 = kHanning, 2 = kSqrtHanning.
// x/x_old point at 64 floats; re/im at 65 floats. x is NOT modified
// (the shim copies it into the scratch array the C++ API mutates).
void aec3_fft_zero_padded(const float* x, int window, float* re, float* im);
void aec3_fft_padded(const float* x, const float* x_old, int window,
                     float* re, float* im);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_FFT_SHIM_H_
