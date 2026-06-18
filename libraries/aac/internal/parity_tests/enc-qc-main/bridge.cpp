// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine-vendored oracle bridge for the AAC encoder quantization / rate-control
// loop's inner-loop bit counter (qc_main.cpp's FDKaacEnc_QCMain calls
// FDKaacEnc_dynBitCount once per channel per iteration to size the access unit).
//
// The single non-static entry point FDKaacEnc_dynBitCount (linked from the
// sibling dyn_bits.cpp TU compiled into this test binary) drives the ENTIRE
// static helper chain — FDKaacEnc_noiselessCounter, gmStage0/1/2,
// buildBitLookUp, findBestBook/findMinMergeBits/mergeBitLookUp/findMaxMerge/
// CalcMergeGain, getSideInfoBits, scfCount, noiseCount — plus, via
// buildBitLookUp, the real FDKaacEnc_bitCount + the seven per-codebook count
// functions in bit_cnt.cpp. FDKaacEnc_countValues / FDKaacEnc_bitCount are also
// exposed directly (used by crash recovery / the lookup builder). So this e2e
// shim exercises every static the Go port translates, against the real FDK code.
//
// No re-derivation: the oracle is the genuine vendored FDK code, not a
// Go-mirroring hand-twin (oracle_kind == real_vendored). The BITCNTR_STATE and
// its bit-lookup scratch are allocated here with calloc (the vendored BCNew uses
// the encoder RAM pool, which is not part of this slice) and sized exactly to the
// vendored BIT_LOOK_UP_SIZE / MERGE_GAIN_LOOK_UP_SIZE macros.

#include "dyn_bits.h"
#include "bit_cnt.h"
#include "aacEnc_ram.h"
#include "psy_const.h"

#include <stdint.h>
#include <string.h>
#include <stdlib.h>

extern "C" {

// qcm_dyn_bit_count runs the genuine FDKaacEnc_dynBitCount over one channel and
// copies out the returned total bits plus the resulting SECTION_DATA breakdown
// (the per-section codeBook / sfbStart / sfbCnt / sectionBits arrays are written
// up to *noOfSectionsOut entries; the caller sizes them to MAX_SECTIONS).
int qcm_dyn_bit_count(const int16_t *quantSpectrum, const unsigned int *maxValueInSfb,
                      const int *scalefac, int blockType, int sfbCnt,
                      int maxSfbPerGroup, int sfbPerGroup, const int *sfbOffset,
                      const int *noiseNrg, const int *isBook, const int *isScale,
                      unsigned int syntaxFlags,
                      int *noOfSectionsOut, int *huffmanBitsOut,
                      int *sideInfoBitsOut, int *scalefacBitsOut,
                      int *noiseNrgBitsOut, int *firstScfOut,
                      int *sectCodeBookOut, int *sectSfbStartOut,
                      int *sectSfbCntOut, int *sectSectionBitsOut) {
  BITCNTR_STATE bc;
  bc.bitLookUp = (INT *)calloc(1, BIT_LOOK_UP_SIZE);
  bc.mergeGainLookUp = (INT *)calloc(1, MERGE_GAIN_LOOK_UP_SIZE);

  SECTION_DATA *sd = (SECTION_DATA *)calloc(1, sizeof(SECTION_DATA));

  INT total = FDKaacEnc_dynBitCount(
      &bc, quantSpectrum, maxValueInSfb, scalefac, blockType, sfbCnt,
      maxSfbPerGroup, sfbPerGroup, sfbOffset, sd, noiseNrg, isBook, isScale,
      syntaxFlags);

  *noOfSectionsOut = sd->noOfSections;
  *huffmanBitsOut = sd->huffmanBits;
  *sideInfoBitsOut = sd->sideInfoBits;
  *scalefacBitsOut = sd->scalefacBits;
  *noiseNrgBitsOut = sd->noiseNrgBits;
  *firstScfOut = sd->firstScf;
  for (int i = 0; i < sd->noOfSections; i++) {
    sectCodeBookOut[i] = sd->huffsection[i].codeBook;
    sectSfbStartOut[i] = sd->huffsection[i].sfbStart;
    sectSfbCntOut[i] = sd->huffsection[i].sfbCnt;
    sectSectionBitsOut[i] = sd->huffsection[i].sectionBits;
  }

  free(bc.bitLookUp);
  free(bc.mergeGainLookUp);
  free(sd);
  return (int)total;
}

// qcm_count_values runs the genuine FDKaacEnc_countValues. The values buffer is
// not modified (the vendored signature lacks const for SIMD reasons).
int qcm_count_values(int16_t *values, int width, int codeBook) {
  return (int)FDKaacEnc_countValues(values, width, codeBook);
}

// qcm_bit_count runs the genuine FDKaacEnc_bitCount, filling
// bitCountOut[0..CODE_BOOK_ESC_NDX].
void qcm_bit_count(const int16_t *values, int width, int maxVal,
                   int *bitCountOut) {
  FDKaacEnc_bitCount(values, width, maxVal, bitCountOut);
}

} // extern "C"
