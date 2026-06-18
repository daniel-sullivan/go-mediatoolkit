// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine-vendored oracle bridge for the AAC encoder bandwidth expert
// (bandwidth.cpp). The vendored TU is #included directly here so the static
// GetBandwidthEntry is visible to the extern "C" shims that wrap it; the
// non-static FDKaacEnc_DetermineBandWidth is wrapped the same way. The shims
// fabricate a CHANNEL_MAPPING carrying nChannelsEff/encMode and call the REAL
// FDK functions — no re-derivation (oracle_kind == real_vendored).

#include "channel_map.h"
#include "bandwidth.h"

// Compile the genuine vendored bandwidth.cpp into this TU so the static
// GetBandwidthEntry can be called by the shims below.
#include "bandwidth.cpp"

#include <stdint.h>
#include <string.h>

extern "C" {

// bparity_determine_bandwidth runs the genuine FDKaacEnc_DetermineBandWidth and
// returns the AAC_ENCODER_ERROR (with *bandWidthOut the chosen bandwidth).
int bparity_determine_bandwidth(int proposedBandWidth, int bitrate,
                                int bitrateMode, int sampleRate, int frameLength,
                                int nChannelsEff, int encoderMode,
                                int *bandWidthOut) {
  CHANNEL_MAPPING cm;
  memset(&cm, 0, sizeof(cm));
  cm.nChannelsEff = nChannelsEff;
  cm.encMode = (CHANNEL_MODE)encoderMode;

  INT bandWidth = 0;
  AAC_ENCODER_ERROR err = FDKaacEnc_DetermineBandWidth(
      proposedBandWidth, bitrate, (AACENC_BITRATE_MODE)bitrateMode, sampleRate,
      frameLength, &cm, (CHANNEL_MODE)encoderMode, &bandWidth);
  *bandWidthOut = (int)bandWidth;
  return (int)err;
}

// bparity_get_bandwidth_entry runs the genuine static GetBandwidthEntry.
int bparity_get_bandwidth_entry(int frameLength, int sampleRate, int chanBitRate,
                                int entryNo) {
  return (int)GetBandwidthEntry(frameLength, sampleRate, chanBitRate, entryNo);
}

} // extern "C"
