// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC encoder channel-mapping init/config
 * tier — libAACenc/src/channel_map.cpp. This stage decides the per-access-unit
 * element layout before the per-frame psy + rate-control loops:
 *
 *   FDKaacEnc_DetermineEncoderMode  resolve/validate the CHANNEL_MODE from the
 *                                   requested channel count.
 *   FDKaacEnc_InitChannelMapping    zero the CHANNEL_MAPPING, copy the matching
 *                                   channelModeConfig[] entry, build the channel
 *                                   map descriptor (default-table install) and
 *                                   lay out the ELEMENT_INFO list (element type,
 *                                   coder-channel indices, instance tags, the AAC
 *                                   relativeBits split) — this also exercises the
 *                                   static FDKaacEnc_initElement.
 *   FDKaacEnc_InitElementBits       split the bitrate / max-bits budget across the
 *                                   elements into QC_STATE.elementBits[].
 *
 * This TU provides the extern "C" bridge the Go test calls; it links the GENUINE
 * vendored channel_map.cpp + syslib_channelMapDescr.cpp + FDK_tools_rom.cpp +
 * genericStds.cpp (sibling TUs), so the oracle is the real reference, NOT a
 * hand-twin (oracle_kind == real_vendored).
 *
 * It NEVER imports libraries/aac, so there is no cross-package static-symbol
 * clash (the amalgamation-split reasoning the sibling encode-filterbank oracle
 * documents). It MAY, and the test does, import the pure-Go internal/nativeaac.
 *
 * FP-parity: this tier is pure INTEGER (FIXP_DBL == int32 relativeBits; the bit
 * splits are the fMult int64-product>>32 / CountLeadingBits / GetInvInt integer
 * kernels). No floating point, no transcendental -> EXACT int equality (the gate
 * still sets aac_strict for consistency).
 */

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "common_fix.h"
#include "psy_const.h"
#include "qc_data.h"
#include "channel_map.h"

extern "C" {

/* cm_snapshot is a flat, fixed-layout mirror of CHANNEL_MAPPING (qc_data.h:135)
 * the Go test reads to compare every field bit-for-bit. elInfo is flattened into
 * parallel arrays (8 elements). */
typedef struct {
  int32_t encMode;
  int32_t nChannels;
  int32_t nChannelsEff;
  int32_t nElements;
  int32_t elType[8];
  int32_t instanceTag[8];
  int32_t nChannelsInEl[8];
  int32_t channelIndex0[8];
  int32_t channelIndex1[8];
  int32_t relativeBits[8];
} cm_snapshot;

/* eb_snapshot mirrors QC_STATE.elementBits[8] (ELEMENT_BITS, qc_data.h:262). */
typedef struct {
  int32_t chBitrateEl[8];
  int32_t maxBitsEl[8];
  int32_t bitResLevelEl[8];
  int32_t maxBitResBitsEl[8];
  int32_t relativeBitsEl[8];
} eb_snapshot;

static void cm_snap(const CHANNEL_MAPPING *cm, cm_snapshot *o) {
  memset(o, 0, sizeof(*o));
  o->encMode = (int32_t)cm->encMode;
  o->nChannels = (int32_t)cm->nChannels;
  o->nChannelsEff = (int32_t)cm->nChannelsEff;
  o->nElements = (int32_t)cm->nElements;
  for (int i = 0; i < 8; i++) {
    o->elType[i] = (int32_t)cm->elInfo[i].elType;
    o->instanceTag[i] = (int32_t)cm->elInfo[i].instanceTag;
    o->nChannelsInEl[i] = (int32_t)cm->elInfo[i].nChannelsInEl;
    o->channelIndex0[i] = (int32_t)cm->elInfo[i].ChannelIndex[0];
    o->channelIndex1[i] = (int32_t)cm->elInfo[i].ChannelIndex[1];
    o->relativeBits[i] = (int32_t)cm->elInfo[i].relativeBits;
  }
}

/* eparity_determine_encoder_mode runs the genuine FDKaacEnc_DetermineEncoderMode
 * and returns the resolved mode (via *outMode) and the rc. */
int eparity_determine_encoder_mode(int32_t inMode, int nChannels,
                                   int32_t *outMode) {
  CHANNEL_MODE mode = (CHANNEL_MODE)inMode;
  AAC_ENCODER_ERROR rc = FDKaacEnc_DetermineEncoderMode(&mode, nChannels);
  *outMode = (int32_t)mode;
  return (int)rc;
}

/* eparity_init_channel_mapping runs the genuine FDKaacEnc_InitChannelMapping and
 * snapshots the resulting CHANNEL_MAPPING. Returns the kernel rc. */
int eparity_init_channel_mapping(int32_t mode, int co, cm_snapshot *out) {
  CHANNEL_MAPPING cm;
  AAC_ENCODER_ERROR rc = FDKaacEnc_InitChannelMapping(
      (CHANNEL_MODE)mode, (CHANNEL_ORDER)co, &cm);
  cm_snap(&cm, out);
  return (int)rc;
}

/* eparity_init_element_bits builds a CHANNEL_MAPPING (via the genuine init), wires
 * a QC_STATE with 8 ELEMENT_BITS, runs the genuine FDKaacEnc_InitElementBits, and
 * snapshots the elementBits[]. Returns the kernel rc (or -100 if the channel
 * mapping init itself failed). */
int eparity_init_element_bits(int32_t mode, int co, int bitrateTot,
                              int averageBitsTot, int maxChannelBits,
                              eb_snapshot *out) {
  CHANNEL_MAPPING cm;
  if (FDKaacEnc_InitChannelMapping((CHANNEL_MODE)mode, (CHANNEL_ORDER)co, &cm) !=
      AAC_ENC_OK) {
    return -100;
  }

  QC_STATE qc;
  memset(&qc, 0, sizeof(qc));
  ELEMENT_BITS eb[8];
  memset(eb, 0, sizeof(eb));
  for (int i = 0; i < 8; i++) qc.elementBits[i] = &eb[i];

  AAC_ENCODER_ERROR rc = FDKaacEnc_InitElementBits(
      &qc, &cm, bitrateTot, averageBitsTot, maxChannelBits);

  memset(out, 0, sizeof(*out));
  for (int i = 0; i < 8; i++) {
    out->chBitrateEl[i] = (int32_t)eb[i].chBitrateEl;
    out->maxBitsEl[i] = (int32_t)eb[i].maxBitsEl;
    out->bitResLevelEl[i] = (int32_t)eb[i].bitResLevelEl;
    out->maxBitResBitsEl[i] = (int32_t)eb[i].maxBitResBitsEl;
    out->relativeBitsEl[i] = (int32_t)eb[i].relativeBitsEl;
  }
  return (int)rc;
}

} /* extern "C" */
