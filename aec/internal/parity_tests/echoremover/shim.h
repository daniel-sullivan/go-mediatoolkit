//go:build cgo && aec_oracle

// AEC3 echoremover parity shim: an extern "C" wrapper around the
// oracle's real webrtc::EchoRemover (created via
// webrtc::EchoRemover::Create), driven through
// webrtc::RenderDelayBuffer exactly as EchoRemoverImpl::ProcessCapture
// expects, and compared against this port's now-complete
// aec3.EchoRemover.ProcessCapture. Unlike the aecstate slice (which
// had to hand-reimplement EchoRemoverImpl's private
// FormLinearFilterOutput/WindowedPaddedFft/SignalTransition helpers
// since EchoRemover didn't exist yet at that phase), this slice needs
// no such reimplementation: EchoRemover::Create gives a fully
// orchestrated instance, so the shim is a thin pass-through. Sample
// rate is fixed at 16000 Hz (1 band, 1 render channel, 1 capture
// channel), matching this port's Block/NumBandsForRate convention.
#ifndef AEC_PARITY_ECHOREMOVER_SHIM_H_
#define AEC_PARITY_ECHOREMOVER_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_echoremover aec3_echoremover;

// AEC3_ECHOREMOVER_BLOCK_FLOATS mirrors kBlockSize (64).
#define AEC3_ECHOREMOVER_BLOCK_FLOATS 64

aec3_echoremover* aec3_echoremover_create(void);
void aec3_echoremover_destroy(aec3_echoremover* s);

// samples must have length AEC3_ECHOREMOVER_BLOCK_FLOATS (1 render
// channel).
void aec3_echoremover_insert_render_block(aec3_echoremover* s,
                                          const float* samples);

// Runs one full capture block through webrtc::EchoRemover::
// ProcessCapture followed by GetMetrics, mirroring
// aec3.EchoRemover.ProcessCapture's Go call site.
//
// capture_samples: AEC3_ECHOREMOVER_BLOCK_FLOATS floats, in/out: the
//   microphone block on input, the processed (echo-suppressed) block
//   on output.
// capture_saturation, gain_change, clock_drift: 0/1.
// delay_change: 0=kNone, 1=kBufferFlush, 2=kNewDetectedDelay.
// has_external_delay: 0/1. external_delay_quality: 0=kCoarse,
//   1=kRefined. external_delay_value: delay in blocks.
// linear_output_out: AEC3_ECHOREMOVER_BLOCK_FLOATS floats, the linear
//   filter output block (always requested, unlike production usage
//   where it may be nil, for extra bit-exact coverage).
// metrics_out: 2 doubles (matching EchoControl::Metrics's field
//   widths, so no lossy narrowing is introduced by this shim):
//   [0]=echo_return_loss, [1]=echo_return_loss_enhancement
//   (GetMetrics's output, read after ProcessCapture).
void aec3_echoremover_process(aec3_echoremover* s, float* capture_samples,
                              int capture_saturation, int gain_change,
                              int delay_change, int clock_drift,
                              int has_external_delay,
                              int external_delay_quality,
                              int external_delay_value,
                              float* linear_output_out, double* metrics_out);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_ECHOREMOVER_SHIM_H_
