// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE bitstream stage:
 * the raw_data_block syntax serializers in libAACenc/src/bitenc.cpp —
 *
 *   - FDKaacEnc_encodeIcsInfo          (bitenc.cpp:180) ics_info syntax
 *   - FDKaacEnc_encodeGlobalGain       (bitenc.cpp:158) global_gain
 *   - FDKaacEnc_encodeSectionData      (bitenc.cpp:239) section_data
 *   - FDKaacEnc_encodeScaleFactorData  (bitenc.cpp:292) scale_factor_data
 *   - FDKaacEnc_encodeMSInfo           (bitenc.cpp:380) ms_mask / ms_used
 *   - FDKaacEnc_encodeTnsDataPresent   (bitenc.cpp:434) tns_data_present
 *   - FDKaacEnc_encodeTnsData          (bitenc.cpp:465) tns_data
 *   - FDKaacEnc_encodeSpectralData     (bitenc.cpp:127) spectral_data
 *
 * Every one of these is `static` in bitenc.cpp. Following the same discipline
 * the sibling enc-stereo-tns oracle uses for the static TNS quantizers, this TU
 * #includes the GENUINE vendored bitenc.cpp directly (NOT a hand-twin) so the
 * extern "C" shims below reach the real static functions from inside their own
 * translation unit — they ARE the genuine reference, not a re-derivation
 * (oracle_kind == real_vendored). Because bitenc.cpp is included here, it must
 * NOT also be compiled as a separate sibling TU (that would doubly-define its
 * public symbols).
 *
 * bitenc.cpp's public functions (FDKaacEnc_ChannelElementWrite /
 * FDKaacEnc_WriteBitstream / FDKaacEnc_writeDataStreamElement) reference the
 * transport encoder (transportEnc_GetBitstream / CrcStartReg / CrcEndReg) and
 * getBitstreamElementList; the shims below never call those public functions, so
 * those symbols are referenced-but-uncalled. getBitstreamElementList is supplied
 * by the genuine FDK_tools_rom.cpp sibling TU; the transportEnc_* references are
 * satisfied by the never-reached stubs at the bottom of this file. The Huffman
 * emitters FDKaacEnc_codeValues / FDKaacEnc_codeScalefactorDelta the spectral /
 * scalefactor serializers call ARE the genuine vendored functions (bit_cnt.cpp
 * sibling TU); the FDK bit WRITER (FDKwriteBits / FDKgetValidBits / FDKbyteAlign
 * + the FDK_put ring store) is the genuine inline reference driven through
 * FDKinitBitStream.
 *
 * This TU NEVER imports libraries/aac, so there is no cross-package
 * static-symbol clash (the same amalgamation-split reasoning the sibling
 * enc-quantize / enc-psy-model / enc-stereo-tns oracles document). The test MAY,
 * and does, import the pure-Go internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — the whole raw_data_block area is
 * a pure INTEGER kernel (bit shifts, masks, table lookups on SHORT/INT operands
 * and a UCHAR ring store). It is bit-identical regardless of -ffp-contract /
 * vectorization, with NO transcendental and NO float, so the assertions are
 * EXACT-byte equality. The gate still runs under -tags 'aac_strict aacfdk' for
 * convention consistency.
 */

#include <stdint.h>
#include <string.h>

#include "common_fix.h"
#include "FDK_bitstream.h"
#include "psy_const.h"
#include "dyn_bits.h"
#include "aacenc_tns.h"

/* Pull in the genuine vendored bitenc.cpp so its static symbols are visible to
 * the shims in this same TU. */
#include "libfdk/libAACenc/src/bitenc.cpp"

extern "C" {

/* ---- ics_info -------------------------------------------------------------
 *
 * ebparity_ics_info drives FDKaacEnc_encodeIcsInfo into bufBytes of out
 * (pre-zeroed), byte-aligns to anchor 0 and flushes, stores the returned
 * static-bit count into *statBits and returns the produced byte length.
 */
int ebparity_ics_info(int blockType, int windowShape, int groupingMask,
                      int maxSfbPerGroup, unsigned int syntaxFlags,
                      unsigned char *out, int bufBytes, int *statBits) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  *statBits = (int)FDKaacEnc_encodeIcsInfo(blockType, windowShape, groupingMask,
                                           maxSfbPerGroup, &bs,
                                           (UINT)syntaxFlags);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

/* ---- global_gain ---------------------------------------------------------- */
int ebparity_global_gain(int globalGain, int scalefac, int mdctScale,
                         unsigned char *out, int bufBytes, int *statBits) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  *statBits =
      (int)FDKaacEnc_encodeGlobalGain(globalGain, scalefac, &bs, mdctScale);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

/* fillSectionData seeds a genuine SECTION_DATA from the flat section arrays
 * (codeBook[i], sfbStart[i], sfbCnt[i] for i in [0,noOfSections)). */
static void fillSectionData(SECTION_DATA *sd, int blockType, int firstScf,
                            int noOfSections, const int *codeBook,
                            const int *sfbStart, const int *sfbCnt) {
  memset(sd, 0, sizeof(*sd));
  sd->blockType = blockType;
  sd->noOfSections = noOfSections;
  sd->firstScf = firstScf;
  for (int i = 0; i < noOfSections; i++) {
    sd->huffsection[i].codeBook = codeBook[i];
    sd->huffsection[i].sfbStart = sfbStart[i];
    sd->huffsection[i].sfbCnt = sfbCnt[i];
  }
}

/* ---- section_data --------------------------------------------------------- */
int ebparity_section_data(int blockType, int noOfSections, const int *codeBook,
                          const int *sfbStart, const int *sfbCnt,
                          unsigned int useVCB11, unsigned char *out,
                          int bufBytes, int *siBits) {
  SECTION_DATA sd;
  fillSectionData(&sd, blockType, 0, noOfSections, codeBook, sfbStart, sfbCnt);
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  *siBits = (int)FDKaacEnc_encodeSectionData(&sd, &bs, (UINT)useVCB11);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

/* ---- scale_factor_data ----------------------------------------------------
 *
 * maxValueInSfb / scalefac / noiseNrg / isScale are indexed by scalefactor
 * band (length >= max sfbStart+sfbCnt). */
int ebparity_scalefactor_data(int blockType, int firstScf, int noOfSections,
                              const int *codeBook, const int *sfbStart,
                              const int *sfbCnt, const unsigned int *maxValueInSfb,
                              const int *scalefac, const int *noiseNrg,
                              const int *isScale, int globalGain,
                              unsigned char *out, int bufBytes, int *sfBits) {
  SECTION_DATA sd;
  fillSectionData(&sd, blockType, firstScf, noOfSections, codeBook, sfbStart,
                  sfbCnt);
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  /* FDKaacEnc_encodeScaleFactorData takes non-const UINT* / INT* noiseNrg; copy
   * into scratch so the caller's arrays are not mutated. */
  UINT mv[MAX_SFB_LONG];
  INT scf[MAX_SFB_LONG];
  INT nn[MAX_SFB_LONG];
  INT is[MAX_SFB_LONG];
  int maxSfb = firstScf;
  for (int i = 0; i < noOfSections; i++) {
    int end = sfbStart[i] + sfbCnt[i];
    if (end > maxSfb) maxSfb = end;
  }
  if (maxSfb > MAX_SFB_LONG) maxSfb = MAX_SFB_LONG;
  for (int i = 0; i < maxSfb; i++) {
    mv[i] = maxValueInSfb[i];
    scf[i] = scalefac[i];
    nn[i] = noiseNrg[i];
    is[i] = isScale[i];
  }
  *sfBits = (int)FDKaacEnc_encodeScaleFactorData(mv, &sd, scf, &bs, nn, is,
                                                 globalGain);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

/* ---- ms_mask / ms_used ----------------------------------------------------
 *
 * jsFlags is indexed by scalefactor band. */
int ebparity_ms_info(int sfbCnt, int grpSfb, int maxSfb, int msDigest,
                     const int *jsFlags, unsigned char *out, int bufBytes,
                     int *msBits) {
  INT js[MAX_GROUPED_SFB];
  for (int i = 0; i < sfbCnt && i < MAX_GROUPED_SFB; i++) js[i] = jsFlags[i];
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  *msBits =
      (int)FDKaacEnc_encodeMSInfo(sfbCnt, grpSfb, maxSfb, msDigest, js, &bs);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

/* fillTnsInfo seeds a genuine TNS_INFO from flat per-window/filter arrays.
 *   numOfWindows: 1 (long) or TRANS_FAC (short)
 *   coefRes[w], numOfFilters[w]
 *   length/order/direction[w*MAX_NUM_OF_FILTERS + f]
 *   coef[(w*MAX_NUM_OF_FILTERS + f)*TNS_MAX_ORDER + k]   (int32)
 */
static void fillTnsInfo(TNS_INFO *ti, int numOfWindows, const int *coefRes,
                        const int *numOfFilters, const int *length,
                        const int *order, const int *direction,
                        const int *coef) {
  memset(ti, 0, sizeof(*ti));
  for (int w = 0; w < numOfWindows; w++) {
    ti->coefRes[w] = coefRes[w];
    ti->numOfFilters[w] = numOfFilters[w];
    for (int f = 0; f < MAX_NUM_OF_FILTERS; f++) {
      int wf = w * MAX_NUM_OF_FILTERS + f;
      ti->length[w][f] = length[wf];
      ti->order[w][f] = order[wf];
      ti->direction[w][f] = direction[wf];
      for (int k = 0; k < TNS_MAX_ORDER; k++) {
        ti->coef[w][f][k] = coef[wf * TNS_MAX_ORDER + k];
      }
    }
  }
}

/* ---- tns_data_present ----------------------------------------------------- */
int ebparity_tns_data_present(int blockType, int numOfWindows,
                              const int *coefRes, const int *numOfFilters,
                              const int *length, const int *order,
                              const int *direction, const int *coef,
                              unsigned char *out, int bufBytes, int *statBits) {
  TNS_INFO ti;
  fillTnsInfo(&ti, numOfWindows, coefRes, numOfFilters, length, order,
              direction, coef);
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  *statBits = (int)FDKaacEnc_encodeTnsDataPresent(&ti, blockType, &bs);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

/* ---- tns_data ------------------------------------------------------------- */
int ebparity_tns_data(int blockType, int numOfWindows, const int *coefRes,
                      const int *numOfFilters, const int *length,
                      const int *order, const int *direction, const int *coef,
                      unsigned char *out, int bufBytes, int *tnsBits) {
  TNS_INFO ti;
  fillTnsInfo(&ti, numOfWindows, coefRes, numOfFilters, length, order,
              direction, coef);
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  *tnsBits = (int)FDKaacEnc_encodeTnsData(&ti, blockType, &bs);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

/* ---- spectral_data --------------------------------------------------------
 *
 * sfbOffset is indexed by scalefactor band (length >= max sfbStart+sfbCnt + 1);
 * quantSpectrum holds the quantized coefficients (length >= sfbOffset[maxSfb]).
 */
int ebparity_spectral_data(int blockType, int noOfSections, const int *codeBook,
                           const int *sfbStart, const int *sfbCnt,
                           int *sfbOffset, short *quantSpectrum,
                           unsigned char *out, int bufBytes, int *specBits) {
  SECTION_DATA sd;
  fillSectionData(&sd, blockType, 0, noOfSections, codeBook, sfbStart, sfbCnt);
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  /* FDKaacEnc_codeValues takes a non-const SHORT* (sign-extracting books do
   * *values++); copy the spectrum into scratch so the caller's slice is not
   * mutated. The max long-window line count is 1024. */
  short scratch[1024];
  int nlines = 0;
  for (int i = 0; i < noOfSections; i++) {
    int end = sfbStart[i] + sfbCnt[i];
    if (sfbOffset[end] > nlines) nlines = sfbOffset[end];
  }
  if (nlines > 1024) nlines = 1024;
  memcpy(scratch, quantSpectrum, (size_t)nlines * sizeof(short));
  *specBits = (int)FDKaacEnc_encodeSpectralData(sfbOffset, &sd, scratch, &bs);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

} /* extern "C" */

/* ---- never-reached transportEnc_* stubs -----------------------------------
 *
 * The included bitenc.cpp emits FDKaacEnc_ChannelElementWrite /
 * FDKaacEnc_WriteBitstream / FDKaacEnc_writeDataStreamElement, which reference
 * these transport-encoder symbols. The shims above never call those public
 * functions, so these stubs are never executed; they exist only to satisfy the
 * linker without pulling in the whole MpegTPEnc module. They are declared in
 * tpenc_lib.h with C++ linkage (the header has no extern "C"), so they are
 * defined here OUTSIDE the extern "C" block to match. */
HANDLE_FDK_BITSTREAM transportEnc_GetBitstream(HANDLE_TRANSPORTENC hTp) {
  (void)hTp;
  return (HANDLE_FDK_BITSTREAM)0;
}
int transportEnc_CrcStartReg(HANDLE_TRANSPORTENC hTpEnc, int mBits) {
  (void)hTpEnc;
  (void)mBits;
  return 0;
}
void transportEnc_CrcEndReg(HANDLE_TRANSPORTENC hTpEnc, int reg) {
  (void)hTpEnc;
  (void)reg;
}
TRANSPORTENC_ERROR transportEnc_EndAccessUnit(HANDLE_TRANSPORTENC hTp,
                                              int *pBits) {
  (void)hTp;
  (void)pBits;
  return TRANSPORTENC_OK;
}
