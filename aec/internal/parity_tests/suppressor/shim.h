//go:build cgo && aec_oracle

// AEC3 suppressor parity shim: an extern "C" wrapper around the
// oracle's webrtc::ComfortNoiseGenerator, webrtc::SuppressionGain and
// webrtc::SuppressionFilter, driven directly (component-level) with
// caller-supplied synthetic spectra/render/analysis-FFT sequences,
// rather than through a full render/subtractor/AecState pipeline (that
// integration is covered by the echoremover slice). A single,
// never-Updated webrtc::AecState and webrtc::RenderSignalAnalyzer are
// used only to satisfy GetGain's parameter list -- SuppressionGain
// only reads AecState::SaturatedEcho() (always false on a fresh
// instance) and RenderSignalAnalyzer::NarrowPeakBand() (always empty
// on a fresh instance) from them, so their staticness does not limit
// coverage of the three components under test, whose own persistent
// state (CNG's seed/N2/Y2_smoothed, SuppressionGain's
// lastGain/nearendSmoothers/dominant-nearend-detector,
// SuppressionFilter's e_output_old_) evolves across calls purely from
// the supplied per-iteration sequences. Sample rate is fixed at 16000
// Hz (1 band), matching this port's Block/NumBandsForRate convention,
// so SuppressionFilter's upper-band code paths (dead at 1 band) are
// intentionally not exercised here.
#ifndef AEC_PARITY_SUPPRESSOR_SHIM_H_
#define AEC_PARITY_SUPPRESSOR_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_suppressor aec3_suppressor;

// AEC3_SUPPRESSOR_SPECTRUM_FLOATS mirrors kFftLengthBy2Plus1 (65).
#define AEC3_SUPPRESSOR_SPECTRUM_FLOATS 65
// AEC3_SUPPRESSOR_BLOCK_FLOATS mirrors kBlockSize (64).
#define AEC3_SUPPRESSOR_BLOCK_FLOATS 64

aec3_suppressor* aec3_suppressor_create(void);
void aec3_suppressor_destroy(aec3_suppressor* s);

// Runs one iteration through ComfortNoiseGenerator::Compute,
// SuppressionGain::GetGain and SuppressionFilter::ApplyGain, in that
// order (matching EchoRemoverImpl::ProcessCapture's orchestration).
//
// Inputs (all caller-owned, not mutated):
//   render: AEC3_SUPPRESSOR_BLOCK_FLOATS floats (single render channel).
//   capture_spectrum: AEC3_SUPPRESSOR_SPECTRUM_FLOATS floats (CNG's Y2).
//   saturated_capture, clock_drift: 0/1.
//   nearend_spectrum, echo_spectrum, residual_echo_spectrum,
//     residual_echo_spectrum_unbounded: AEC3_SUPPRESSOR_SPECTRUM_FLOATS
//     floats each.
//   e_lowest_re, e_lowest_im: AEC3_SUPPRESSOR_SPECTRUM_FLOATS floats
//     each (synthetic "lowest band" analysis FFT fed to
//     SuppressionFilter::ApplyGain).
//
// Outputs (all caller-allocated):
//   lower_noise_re/im, upper_noise_re/im: AEC3_SUPPRESSOR_SPECTRUM_FLOATS
//     floats each (CNG::Compute's per-channel FftData outputs).
//   noise_spectrum_out: AEC3_SUPPRESSOR_SPECTRUM_FLOATS floats
//     (CNG::NoiseSpectrum()[0]).
//   gain_out: AEC3_SUPPRESSOR_SPECTRUM_FLOATS floats (low_band_gain).
//   filter_out: AEC3_SUPPRESSOR_BLOCK_FLOATS floats (ApplyGain's band-0
//     output).
//   scalar_out: 2 floats: [0]=high_bands_gain, [1]=IsDominantNearend
//     (0/1).
void aec3_suppressor_step(
    aec3_suppressor* s, const float* render, const float* capture_spectrum,
    int saturated_capture, const float* nearend_spectrum,
    const float* echo_spectrum, const float* residual_echo_spectrum,
    const float* residual_echo_spectrum_unbounded, int clock_drift,
    const float* e_lowest_re, const float* e_lowest_im, float* lower_noise_re,
    float* lower_noise_im, float* upper_noise_re, float* upper_noise_im,
    float* noise_spectrum_out, float* gain_out, float* filter_out,
    float* scalar_out);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_SUPPRESSOR_SHIM_H_
