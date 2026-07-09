//go:build cgo && aec_oracle

// AEC3 adaptive-FIR-filter parity shim: an extern "C" wrapper around
// the oracle's webrtc::AdaptiveFirFilter (scalar path, forced via
// Aec3Optimization::kNone) and webrtc::ComputeErl, fed by a real
// webrtc::RenderDelayBuffer driven the same way
// RenderDelayBuffer/PrepareCaptureProcessing are driven in the
// delaypipe slice (Insert then PrepareCaptureProcessing per block; no
// delay controller/estimation is exercised here, since the filter
// itself does not depend on delay estimation quality).
#ifndef AEC_PARITY_ADAPTIVEFIR_SHIM_H_
#define AEC_PARITY_ADAPTIVEFIR_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_adaptivefir aec3_adaptivefir;

aec3_adaptivefir* aec3_adaptivefir_create(int max_size_partitions,
                                          int initial_size_partitions,
                                          int size_change_duration_blocks,
                                          int num_render_channels);
void aec3_adaptivefir_destroy(aec3_adaptivefir* f);

// samples must have length num_render_channels * kBlockSize (channel
// 0's 64 samples, then channel 1's, ...). Drives the underlying
// RenderDelayBuffer one block (Insert + PrepareCaptureProcessing) so
// the filter's RenderBuffer has fresh spectra/ffts to read.
void aec3_adaptivefir_insert_render_block(aec3_adaptivefir* f,
                                          const float* samples);

void aec3_adaptivefir_handle_echo_path_change(aec3_adaptivefir* f);
int aec3_adaptivefir_size_partitions(aec3_adaptivefir* f);
void aec3_adaptivefir_set_size_partitions(aec3_adaptivefir* f, int size,
                                          int immediate_effect);
int aec3_adaptivefir_max_filter_size_partitions(aec3_adaptivefir* f);

// s_re/s_im must each have length kFftLengthBy2Plus1 (65).
void aec3_adaptivefir_filter(aec3_adaptivefir* f, float* s_re, float* s_im);

// g_re/g_im (each length 65) form the adaptation gain G (im[0] and
// im[64] must be 0, matching FftData's invariant). impulse_response
// must have capacity >= GetTimeDomainLength(max_size_partitions); on
// return *len holds the vector's actual size (the entries beyond *len
// are untouched/undefined and must not be compared).
void aec3_adaptivefir_adapt(aec3_adaptivefir* f, const float* g_re,
                           const float* g_im, float* impulse_response,
                           int* len);

// Same as aec3_adaptivefir_adapt but without the impulse-response
// output (exercises AdaptiveFirFilter::Adapt's 2-arg overload).
void aec3_adaptivefir_adapt_no_ir(aec3_adaptivefir* f, const float* g_re,
                                 const float* g_im);

// h2 must have capacity >= max_size_partitions * kFftLengthBy2Plus1
// (65) floats, laid out partition-major. Returns the number of
// partitions actually written (the filter's current size_partitions).
int aec3_adaptivefir_compute_frequency_response(aec3_adaptivefir* f,
                                                float* h2);

void aec3_adaptivefir_scale_filter(aec3_adaptivefir* f, float factor);

// Standalone: h2 (partition-major, num_partitions * 65 floats) -> erl
// (65 floats). Exercises ComputeErl(kNone, ...) directly, with no
// filter/render-buffer state involved.
void aec3_adaptivefir_compute_erl(const float* h2, int num_partitions,
                                 float* erl);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_ADAPTIVEFIR_SHIM_H_
