// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC sfb-offset / sampling-rate-info ROM
 * (libAACdec/src/aac_rom.cpp sfbOffsetTables + channelinfo.cpp
 * getSamplingRateInfo). This translation unit provides the extern "C" bridge the
 * Go test calls. It uses the GENUINE vendored data and a VERBATIM copy of the
 * lookup function:
 *
 *   - sfbOffsetTables (aac_rom.cpp:445) and the static sfb_* offset arrays it
 *     points at come from aac_rom.cpp, compiled as a sibling TU
 *     (aac_rom_cgo.cpp). aac_rom.h (included here) declares the table extern and
 *     the SFB_INFO struct, so this oracle reads the same ROM the decoder does.
 *   - SamplingRateInfo (channelinfo.h:152) and getSamplingRateInfo
 *     (channelinfo.cpp:225) live in channelinfo.{h,cpp}, which drag in the
 *     entire per-channel decoder struct and the whole decode path at link time
 *     (the same cross-module drag the inverse-quant oracle documents). Per the
 *     add-audio-format parity discipline ("reimplement the static C parser on
 *     the C public API inside the oracle TU; document the hand-twin"), the
 *     SamplingRateInfo struct and the getSamplingRateInfo body are copied
 *     VERBATIM below (channelinfo.h:152 / channelinfo.cpp:225, byte-for-byte the
 *     vendored source — only the function symbol is suffixed _oracle and the two
 *     debug-only FDK_ASSERTs are dropped, exactly as they compile away in a
 *     release build). This is the genuine reference code compiled, without
 *     dragging the rest of the decoder.
 *
 * No other libfdk module is linked (only aac_rom.cpp + the HCR link stubs its
 * unused dispatch table demands), so there is no cross-package static-symbol
 * clash. This file NEVER imports libraries/aac; it stands alone alongside
 * nativeaac.
 *
 * FP-parity: this is a pure integer ROM lookup; the -ffp-contract / vectorize
 * flags from the mise env are irrelevant. See aac_rom.cpp / channelinfo.cpp.
 */

#include <stdint.h>
#include <string.h>

#include "aac_rom.h"

#include "oracle_bridge.h"

/* Verbatim copy of SamplingRateInfo (libAACdec/src/channelinfo.h:152). The
 * vendored struct, decoupled from the rest of channelinfo.h so the link does not
 * pull the whole decoder. */
typedef struct {
  const SHORT *ScaleFactorBands_Long;
  const SHORT *ScaleFactorBands_Short;
  UCHAR NumberOfScaleFactorBands_Long;
  UCHAR NumberOfScaleFactorBands_Short;
  UINT samplingRateIndex;
  UINT samplingRate;
} SamplingRateInfo_oracle;

/* Verbatim copy of getSamplingRateInfo (libAACdec/src/channelinfo.cpp:225),
 * renamed _oracle and typed against SamplingRateInfo_oracle. Identical body —
 * the genuine reference, decoupled from the rest of channelinfo.cpp so the link
 * does not pull the whole decoder. The two trailing FDK_ASSERTs
 * (channelinfo.cpp:289/291) are debug-only and compile away in release; they are
 * omitted here. AAC_DECODER_ERROR is replaced with int and its OK / unsupported
 * codes inlined (AAC_DEC_OK == 0x0000, AAC_DEC_UNSUPPORTED_FORMAT == 0x2003,
 * aacdecoder_lib.h:443/466) so the TU links without aacdecoder_lib.h. */
static int getSamplingRateInfo_oracle(SamplingRateInfo_oracle *t,
                                      UINT samplesPerFrame,
                                      UINT samplingRateIndex,
                                      UINT samplingRate) {
  int index = 0;

  /* Search closest samplerate according to ISO/IEC 13818-7:2005(E) 8.2.4 (Table
   * 38): */
  if ((samplingRateIndex >= 15) || (samplesPerFrame == 768)) {
    const UINT borders[] = {(UINT)-1, 92017, 75132, 55426, 46009, 37566,
                            27713,    23004, 18783, 13856, 11502, 9391};
    UINT i, samplingRateSearch = samplingRate;

    if (samplesPerFrame == 768) {
      samplingRateSearch = (samplingRate * 4) / 3;
    }

    for (i = 0; i < 11; i++) {
      if (borders[i] > samplingRateSearch &&
          samplingRateSearch >= borders[i + 1]) {
        break;
      }
    }
    samplingRateIndex = i;
  }

  t->samplingRateIndex = samplingRateIndex;
  t->samplingRate = samplingRate;

  switch (samplesPerFrame) {
    case 1024:
      index = 0;
      break;
    case 960:
      index = 1;
      break;
    case 768:
      index = 2;
      break;
    case 512:
      index = 3;
      break;
    case 480:
      index = 4;
      break;

    default:
      return 0x2003; /* AAC_DEC_UNSUPPORTED_FORMAT */
  }

  t->ScaleFactorBands_Long =
      sfbOffsetTables[index][samplingRateIndex].sfbOffsetLong;
  t->ScaleFactorBands_Short =
      sfbOffsetTables[index][samplingRateIndex].sfbOffsetShort;
  t->NumberOfScaleFactorBands_Long =
      sfbOffsetTables[index][samplingRateIndex].numberOfSfbLong;
  t->NumberOfScaleFactorBands_Short =
      sfbOffsetTables[index][samplingRateIndex].numberOfSfbShort;

  if (t->ScaleFactorBands_Long == NULL ||
      t->NumberOfScaleFactorBands_Long == 0) {
    t->samplingRate = 0;
    return 0x2003; /* AAC_DEC_UNSUPPORTED_FORMAT */
  }

  return 0x0000; /* AAC_DEC_OK */
}

extern "C" {

void fparity_get_sampling_rate_info(unsigned int samplesPerFrame,
                                    unsigned int samplingRateIndex,
                                    unsigned int samplingRate,
                                    fparity_sri_result *out) {
  SamplingRateInfo_oracle t;
  memset(&t, 0, sizeof(t));
  memset(out, 0, sizeof(*out));

  int err = getSamplingRateInfo_oracle(&t, (UINT)samplesPerFrame,
                                       (UINT)samplingRateIndex,
                                       (UINT)samplingRate);

  out->err = err;
  out->number_of_sfb_long = (unsigned char)t.NumberOfScaleFactorBands_Long;
  out->number_of_sfb_short = (unsigned char)t.NumberOfScaleFactorBands_Short;
  out->sampling_rate_index = (unsigned int)t.samplingRateIndex;
  out->sampling_rate = (unsigned int)t.samplingRate;

  out->long_is_null = (t.ScaleFactorBands_Long == NULL) ? 1 : 0;
  out->short_is_null = (t.ScaleFactorBands_Short == NULL) ? 1 : 0;

  /* Copy the resolved offset tables [0 .. count] inclusive (the terminating
   * transform length sits at index count). Bounded by the fixed buffers. */
  if (!out->long_is_null) {
    int n = (int)t.NumberOfScaleFactorBands_Long + 1;
    if (n > FPARITY_MAX_LONG) n = FPARITY_MAX_LONG;
    for (int k = 0; k < n; k++) out->long_offsets[k] = t.ScaleFactorBands_Long[k];
  }
  if (!out->short_is_null) {
    int n = (int)t.NumberOfScaleFactorBands_Short + 1;
    if (n > FPARITY_MAX_SHORT) n = FPARITY_MAX_SHORT;
    for (int k = 0; k < n; k++)
      out->short_offsets[k] = t.ScaleFactorBands_Short[k];
  }
}

} /* extern "C" */
