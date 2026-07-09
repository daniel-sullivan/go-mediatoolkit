//go:build cgo && aec_oracle

// AEC3 delay-pipeline parity shim: an extern "C" wrapper that drives
// the oracle's webrtc::RenderDelayBuffer + webrtc::RenderDelayController
// through the exact BufferRender/ProcessCapture orchestration sequence
// used by BlockProcessorImpl (block_processor.cc), minus the
// ApmDataDumper logging and echo-remover/metrics side channels this
// port doesn't have counterparts for.
#ifndef AEC_PARITY_DELAYPIPE_SHIM_H_
#define AEC_PARITY_DELAYPIPE_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_delaypipe aec3_delaypipe;

aec3_delaypipe* aec3_delaypipe_create(int sample_rate_hz,
                                      int num_render_channels,
                                      int num_capture_channels);
void aec3_delaypipe_destroy(aec3_delaypipe* p);
int aec3_delaypipe_max_delay(aec3_delaypipe* p);

// samples must have length kBlockSize (64) -- single band, single
// channel (sample_rate_hz is expected to be 16000 so NumBandsForRate
// == 1).
void aec3_delaypipe_buffer_render(aec3_delaypipe* p, const float* samples);

// Runs ProcessCapture's orchestration for one 64-sample mono capture
// block. Returns 1 if AlignFromDelay reported the buffer delay
// changed this call, else 0. *has_estimate is 1 iff GetDelay produced
// an estimate this call (quality/delay_blocks/blocks_since_change/
// blocks_since_update are only meaningful then); *buffer_delay (in
// blocks) and *has_clockdrift are always filled.
int aec3_delaypipe_process_capture(aec3_delaypipe* p, const float* samples,
                                   int* has_estimate, int* quality,
                                   int* delay_blocks, int* blocks_since_change,
                                   int* blocks_since_update, int* buffer_delay,
                                   int* has_clockdrift);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_DELAYPIPE_SHIM_H_
