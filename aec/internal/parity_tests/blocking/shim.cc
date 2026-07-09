//go:build cgo && aec_oracle

// AEC3 blocking parity shim implementation. Compiled by cgo with the
// include/library paths resolved by ../run.sh (see cgo.go).
#include "shim.h"

#include <algorithm>
#include <vector>

#include "api/array_view.h"
#include "modules/audio_processing/aec3/aec3_common.h"
#include "modules/audio_processing/aec3/block.h"
#include "modules/audio_processing/aec3/block_framer.h"
#include "modules/audio_processing/aec3/frame_blocker.h"

namespace {

using webrtc::kBlockSize;
using webrtc::kSubFrameLength;

// Builds the [band][channel] ArrayView structure the C++ APIs take,
// pointing into a flat [band][channel][kSubFrameLength] array.
std::vector<std::vector<rtc::ArrayView<float>>> SubFrameViews(
    float* sub_frame, int num_bands, int num_channels) {
  std::vector<std::vector<rtc::ArrayView<float>>> views(num_bands);
  for (int band = 0; band < num_bands; ++band) {
    views[band].reserve(num_channels);
    for (int channel = 0; channel < num_channels; ++channel) {
      views[band].emplace_back(
          sub_frame + (band * num_channels + channel) * kSubFrameLength,
          kSubFrameLength);
    }
  }
  return views;
}

void BlockToFlat(const webrtc::Block& block, float* flat) {
  for (int band = 0; band < block.NumBands(); ++band) {
    for (int channel = 0; channel < block.NumChannels(); ++channel) {
      const auto view = block.View(band, channel);
      std::copy(view.begin(), view.end(),
                flat + (band * block.NumChannels() + channel) * kBlockSize);
    }
  }
}

void FlatToBlock(const float* flat, webrtc::Block* block) {
  for (int band = 0; band < block->NumBands(); ++band) {
    for (int channel = 0; channel < block->NumChannels(); ++channel) {
      const float* src =
          flat + (band * block->NumChannels() + channel) * kBlockSize;
      std::copy(src, src + kBlockSize, block->begin(band, channel));
    }
  }
}

}  // namespace

struct aec3_frame_blocker {
  aec3_frame_blocker(int num_bands, int num_channels)
      : impl(num_bands, num_channels),
        block(num_bands, num_channels),
        num_bands(num_bands),
        num_channels(num_channels) {}
  webrtc::FrameBlocker impl;
  webrtc::Block block;
  int num_bands;
  int num_channels;
};

struct aec3_block_framer {
  aec3_block_framer(int num_bands, int num_channels)
      : impl(num_bands, num_channels),
        block(num_bands, num_channels),
        num_bands(num_bands),
        num_channels(num_channels) {}
  webrtc::BlockFramer impl;
  webrtc::Block block;
  int num_bands;
  int num_channels;
};

extern "C" {

aec3_frame_blocker* aec3_frame_blocker_create(int num_bands,
                                              int num_channels) {
  return new aec3_frame_blocker(num_bands, num_channels);
}

void aec3_frame_blocker_destroy(aec3_frame_blocker* fb) { delete fb; }

void aec3_frame_blocker_insert_and_extract(aec3_frame_blocker* fb,
                                           const float* sub_frame,
                                           float* block) {
  // The C++ API takes ArrayView<float> (mutable) but does not write
  // through it; copy to keep the shim API const-correct.
  std::vector<float> sub_frame_copy(
      sub_frame,
      sub_frame + fb->num_bands * fb->num_channels * kSubFrameLength);
  auto views =
      SubFrameViews(sub_frame_copy.data(), fb->num_bands, fb->num_channels);
  fb->impl.InsertSubFrameAndExtractBlock(views, &fb->block);
  BlockToFlat(fb->block, block);
}

int aec3_frame_blocker_block_available(const aec3_frame_blocker* fb) {
  return fb->impl.IsBlockAvailable() ? 1 : 0;
}

void aec3_frame_blocker_extract(aec3_frame_blocker* fb, float* block) {
  fb->impl.ExtractBlock(&fb->block);
  BlockToFlat(fb->block, block);
}

aec3_block_framer* aec3_block_framer_create(int num_bands, int num_channels) {
  return new aec3_block_framer(num_bands, num_channels);
}

void aec3_block_framer_destroy(aec3_block_framer* bf) { delete bf; }

void aec3_block_framer_insert(aec3_block_framer* bf, const float* block) {
  FlatToBlock(block, &bf->block);
  bf->impl.InsertBlock(bf->block);
}

void aec3_block_framer_insert_and_extract(aec3_block_framer* bf,
                                          const float* block,
                                          float* sub_frame) {
  FlatToBlock(block, &bf->block);
  auto views = SubFrameViews(sub_frame, bf->num_bands, bf->num_channels);
  bf->impl.InsertBlockAndExtractSubFrame(bf->block, &views);
}

}  // extern "C"
