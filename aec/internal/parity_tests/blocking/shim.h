//go:build cgo && aec_oracle

// AEC3 blocking parity shim: extern "C" wrappers around the oracle's
// webrtc::FrameBlocker and webrtc::BlockFramer (see cgo.go).
//
// Flat layouts: a sub-frame is [band][channel][80] floats and a block
// is [band][channel][64] floats, both flattened in that index order —
// matching webrtc::Block's own (band * num_channels + channel) * 64
// internal layout.
#ifndef AEC_PARITY_BLOCKING_SHIM_H_
#define AEC_PARITY_BLOCKING_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

typedef struct aec3_frame_blocker aec3_frame_blocker;
typedef struct aec3_block_framer aec3_block_framer;

aec3_frame_blocker* aec3_frame_blocker_create(int num_bands, int num_channels);
void aec3_frame_blocker_destroy(aec3_frame_blocker* fb);
void aec3_frame_blocker_insert_and_extract(aec3_frame_blocker* fb,
                                           const float* sub_frame,
                                           float* block);
int aec3_frame_blocker_block_available(const aec3_frame_blocker* fb);
void aec3_frame_blocker_extract(aec3_frame_blocker* fb, float* block);

aec3_block_framer* aec3_block_framer_create(int num_bands, int num_channels);
void aec3_block_framer_destroy(aec3_block_framer* bf);
void aec3_block_framer_insert(aec3_block_framer* bf, const float* block);
void aec3_block_framer_insert_and_extract(aec3_block_framer* bf,
                                          const float* block,
                                          float* sub_frame);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_BLOCKING_SHIM_H_
