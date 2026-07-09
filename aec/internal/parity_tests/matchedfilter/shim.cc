//go:build cgo && aec_oracle

// AEC3 matched-filter parity shim implementation. Compiled by cgo with
// the include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <optional>

#include "api/array_view.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/downsampled_render_buffer.h"
#include "modules/audio_processing/aec3/matched_filter.h"
#include "modules/audio_processing/aec3/matched_filter_lag_aggregator.h"
#include "modules/audio_processing/logging/apm_data_dumper.h"

// --- DownsampledRenderBuffer ---

struct aec3_drb {
  explicit aec3_drb(size_t size) : impl(size) {}
  webrtc::DownsampledRenderBuffer impl;
};

extern "C" {

aec3_drb* aec3_drb_create(int size) {
  return new aec3_drb(static_cast<size_t>(size));
}

void aec3_drb_destroy(aec3_drb* b) { delete b; }

void aec3_drb_set_buffer(aec3_drb* b, const float* data, int len) {
  for (int i = 0; i < len; ++i) {
    b->impl.buffer[i] = data[i];
  }
}

void aec3_drb_set_read(aec3_drb* b, int read) { b->impl.read = read; }

void aec3_drb_update_read_index(aec3_drb* b, int offset) {
  b->impl.UpdateReadIndex(offset);
}

int aec3_drb_offset_index(aec3_drb* b, int index, int offset) {
  return b->impl.OffsetIndex(index, offset);
}

int aec3_drb_read(aec3_drb* b) { return b->impl.read; }

}  // extern "C"

// --- MatchedFilter ---

struct aec3_matched_filter {
  aec3_matched_filter(size_t sub_block_size, size_t window_size_sub_blocks,
                      int num_matched_filters,
                      size_t alignment_shift_sub_blocks,
                      float excitation_limit, float smoothing_fast,
                      float smoothing_slow, float matching_filter_threshold,
                      bool detect_pre_echo)
      : dumper(0),
        impl(&dumper,
             webrtc::Aec3Optimization::kNone,
             sub_block_size,
             window_size_sub_blocks,
             num_matched_filters,
             alignment_shift_sub_blocks,
             excitation_limit,
             smoothing_fast,
             smoothing_slow,
             matching_filter_threshold,
             detect_pre_echo) {}
  webrtc::ApmDataDumper dumper;
  webrtc::MatchedFilter impl;
};

extern "C" {

aec3_matched_filter* aec3_matched_filter_create(
    int sub_block_size, int window_size_sub_blocks, int num_matched_filters,
    int alignment_shift_sub_blocks, float excitation_limit,
    float smoothing_fast, float smoothing_slow,
    float matching_filter_threshold, int detect_pre_echo) {
  return new aec3_matched_filter(
      static_cast<size_t>(sub_block_size),
      static_cast<size_t>(window_size_sub_blocks), num_matched_filters,
      static_cast<size_t>(alignment_shift_sub_blocks), excitation_limit,
      smoothing_fast, smoothing_slow, matching_filter_threshold,
      detect_pre_echo != 0);
}

void aec3_matched_filter_destroy(aec3_matched_filter* m) { delete m; }

void aec3_matched_filter_update(aec3_matched_filter* m, aec3_drb* render,
                                const float* capture, int capture_len,
                                int use_slow_smoothing) {
  m->impl.Update(render->impl,
                rtc::ArrayView<const float>(capture,
                                            static_cast<size_t>(capture_len)),
                use_slow_smoothing != 0);
}

void aec3_matched_filter_reset(aec3_matched_filter* m, int full_reset) {
  m->impl.Reset(full_reset != 0);
}

int aec3_matched_filter_get_best_lag_estimate(aec3_matched_filter* m,
                                              int* lag, int* pre_echo_lag) {
  std::optional<const webrtc::MatchedFilter::LagEstimate> est =
      m->impl.GetBestLagEstimate();
  if (!est) {
    return 0;
  }
  *lag = static_cast<int>(est->lag);
  *pre_echo_lag = static_cast<int>(est->pre_echo_lag);
  return 1;
}

int aec3_matched_filter_get_max_filter_lag(aec3_matched_filter* m) {
  return static_cast<int>(m->impl.GetMaxFilterLag());
}

}  // extern "C"

// --- MatchedFilterLagAggregator ---

struct aec3_lag_aggregator {
  aec3_lag_aggregator(size_t max_filter_lag,
                      const webrtc::EchoCanceller3Config::Delay& delay_config)
      : dumper(0), impl(&dumper, max_filter_lag, delay_config) {}
  webrtc::ApmDataDumper dumper;
  webrtc::MatchedFilterLagAggregator impl;
};

extern "C" {

aec3_lag_aggregator* aec3_lag_aggregator_create(int max_filter_lag,
                                                int down_sampling_factor,
                                                int delay_headroom_samples,
                                                int thresholds_initial,
                                                int thresholds_converged,
                                                int detect_pre_echo) {
  webrtc::EchoCanceller3Config::Delay delay_config;
  delay_config.down_sampling_factor =
      static_cast<size_t>(down_sampling_factor);
  delay_config.delay_headroom_samples =
      static_cast<size_t>(delay_headroom_samples);
  delay_config.delay_selection_thresholds.initial = thresholds_initial;
  delay_config.delay_selection_thresholds.converged = thresholds_converged;
  delay_config.detect_pre_echo = detect_pre_echo != 0;
  return new aec3_lag_aggregator(static_cast<size_t>(max_filter_lag),
                                 delay_config);
}

void aec3_lag_aggregator_destroy(aec3_lag_aggregator* a) { delete a; }

void aec3_lag_aggregator_reset(aec3_lag_aggregator* a, int hard_reset) {
  a->impl.Reset(hard_reset != 0);
}

int aec3_lag_aggregator_aggregate(aec3_lag_aggregator* a,
                                  int has_lag_estimate, int lag,
                                  int pre_echo_lag, int* quality,
                                  int* delay) {
  std::optional<const webrtc::MatchedFilter::LagEstimate> lag_estimate;
  if (has_lag_estimate) {
    // optional<const T>'s copy/move assignment operator is implicitly
    // deleted (T is const-qualified), so the estimate must be
    // constructed in place rather than assigned.
    lag_estimate.emplace(static_cast<size_t>(lag),
                        static_cast<size_t>(pre_echo_lag));
  }
  std::optional<webrtc::DelayEstimate> result = a->impl.Aggregate(lag_estimate);
  if (!result) {
    return 0;
  }
  *quality = static_cast<int>(result->quality);
  *delay = static_cast<int>(result->delay);
  return 1;
}

int aec3_lag_aggregator_reliable_delay_found(aec3_lag_aggregator* a) {
  return a->impl.ReliableDelayFound() ? 1 : 0;
}

int aec3_lag_aggregator_get_delay_at_highest_peak(aec3_lag_aggregator* a) {
  return a->impl.GetDelayAtHighestPeak();
}

}  // extern "C"
