//go:build cgo && aec_oracle

// AEC3 decimator parity shim: extern "C" wrapper around the oracle's
// webrtc::Decimator (cascaded biquad anti-aliasing + noise-reduction
// filters, followed by stride downsampling).
#ifndef AEC_PARITY_DECIMATOR_SHIM_H_
#define AEC_PARITY_DECIMATOR_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_decimator aec3_decimator;

// down_sampling_factor must be 2, 4, or 8.
aec3_decimator* aec3_decimator_create(int down_sampling_factor);
void aec3_decimator_destroy(aec3_decimator* d);
// in: kBlockSize (64) floats; out: kBlockSize/down_sampling_factor
// floats (out_len).
void aec3_decimator_decimate(aec3_decimator* d, const float* in, float* out,
                             int out_len);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_DECIMATOR_SHIM_H_
