//go:build cgo && aec_oracle

// AEC3 matched-filter parity shim: extern "C" wrappers around the
// oracle's webrtc::DownsampledRenderBuffer, webrtc::MatchedFilter
// (scalar path, forced via Aec3Optimization::kNone) and
// webrtc::MatchedFilterLagAggregator.
#ifndef AEC_PARITY_MATCHEDFILTER_SHIM_H_
#define AEC_PARITY_MATCHEDFILTER_SHIM_H_

#ifdef __cplusplus
extern "C" {
#endif

// --- DownsampledRenderBuffer ---

typedef struct aec3_drb aec3_drb;

aec3_drb* aec3_drb_create(int size);
void aec3_drb_destroy(aec3_drb* b);
// data must have len == size (the buffer's fixed capacity).
void aec3_drb_set_buffer(aec3_drb* b, const float* data, int len);
void aec3_drb_set_read(aec3_drb* b, int read);
void aec3_drb_update_read_index(aec3_drb* b, int offset);
int aec3_drb_offset_index(aec3_drb* b, int index, int offset);
int aec3_drb_read(aec3_drb* b);

// --- MatchedFilter (scalar path: Aec3Optimization::kNone) ---

typedef struct aec3_matched_filter aec3_matched_filter;

aec3_matched_filter* aec3_matched_filter_create(
    int sub_block_size, int window_size_sub_blocks, int num_matched_filters,
    int alignment_shift_sub_blocks, float excitation_limit,
    float smoothing_fast, float smoothing_slow,
    float matching_filter_threshold, int detect_pre_echo);
void aec3_matched_filter_destroy(aec3_matched_filter* m);
void aec3_matched_filter_update(aec3_matched_filter* m, aec3_drb* render,
                                const float* capture, int capture_len,
                                int use_slow_smoothing);
void aec3_matched_filter_reset(aec3_matched_filter* m, int full_reset);
// Returns 1 and fills *lag/*pre_echo_lag if a lag estimate is present,
// else returns 0.
int aec3_matched_filter_get_best_lag_estimate(aec3_matched_filter* m,
                                              int* lag, int* pre_echo_lag);
int aec3_matched_filter_get_max_filter_lag(aec3_matched_filter* m);

// --- MatchedFilterLagAggregator ---

typedef struct aec3_lag_aggregator aec3_lag_aggregator;

aec3_lag_aggregator* aec3_lag_aggregator_create(int max_filter_lag,
                                                int down_sampling_factor,
                                                int delay_headroom_samples,
                                                int thresholds_initial,
                                                int thresholds_converged,
                                                int detect_pre_echo);
void aec3_lag_aggregator_destroy(aec3_lag_aggregator* a);
void aec3_lag_aggregator_reset(aec3_lag_aggregator* a, int hard_reset);
// has_lag_estimate selects std::nullopt (0) vs a populated
// MatchedFilter::LagEstimate{lag, pre_echo_lag}. Returns 1 and fills
// *quality (0=kCoarse,1=kRefined)/*delay if an estimate is produced.
int aec3_lag_aggregator_aggregate(aec3_lag_aggregator* a,
                                  int has_lag_estimate, int lag,
                                  int pre_echo_lag, int* quality, int* delay);
int aec3_lag_aggregator_reliable_delay_found(aec3_lag_aggregator* a);
int aec3_lag_aggregator_get_delay_at_highest_peak(aec3_lag_aggregator* a);

#ifdef __cplusplus
}
#endif

#endif  // AEC_PARITY_MATCHEDFILTER_SHIM_H_
