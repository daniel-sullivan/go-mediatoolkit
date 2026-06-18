// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the HE-AAC v2 parametric-stereo bitstream parse
 * ("ps-dec-parse"): ReadPsData + DecodePs (psbitdec.cpp). This TU exposes an
 * extern "C" entry point that allocates a PS_DEC, parses a caller-supplied raw
 * ps_data payload via the GENUINE ReadPsData, runs the GENUINE DecodePs, and
 * returns the dequantized + 34<->20-mapped IID/ICC index arrays (the
 * PS_DEC_COEFFICIENTS aaIidIndexMapped / aaIccIndexMapped) plus the resolved
 * envelope borders. The Go port (internal/nativeaac/sbr ps_bitdec.go) parses the
 * IDENTICAL bytes and must produce EXACTLY the same arrays.
 *
 * It NEVER imports libraries/aac, so there is no cross-package static-symbol
 * clash. Integer parity: the PS parse is pure integer (Huffman walk + SCHAR delta
 * decode + UCHAR border arithmetic + integer 34->20 averaging), bit-identical
 * regardless of -ffp-contract / vectorization, so the oracle asserts EXACT
 * equality.
 */

#include <stdint.h>
#include <string.h>
#include <stdlib.h>

#include "psdec.h"
#include "psbitdec.h"
#include "FDK_bitstream.h"

extern "C" {

/* Flat parse result mirroring the Go ps.ParseResult. */
typedef struct {
  int psProcessFlag;             /* DecodePs return (1 apply / 0 skip)        */
  int bitsRead;                  /* ReadPsData consumed bits                  */
  uint8_t noEnv;                 /* resolved number of envelopes              */
  uint8_t freqResIid;
  uint8_t freqResIcc;
  uint8_t bFineIidQ;
  uint8_t envStartStop[6];       /* MAX_NO_PS_ENV+1                           */
  int8_t iidMapped[5 * 34];      /* aaIidIndexMapped[MAX_NO_PS_ENV][34]       */
  int8_t iccMapped[5 * 34];      /* aaIccIndexMapped[MAX_NO_PS_ENV][34]       */
} psParseOut;

/* qparity_psParse drives the genuine PS parse over the raw payload. noSubSamples
 * selects 30 (960) or 32 (1024). prevDecoded seeds psDecodedPrv so the
 * apply/conceal decision matches a steady-state stream. The PS_DEC is zeroed and
 * its slot indices left at 0 (single-slot, no delay) so ReadPsData writes slot 0
 * and DecodePs reads slot 0 — the same single-frame configuration the Go test
 * uses. */
void qparity_psParse(const uint8_t *payload, int payloadBytes, int validBits,
                     int noSubSamples, int prevDecoded, int frameError,
                     psParseOut *out) {
  struct PS_DEC *h = (struct PS_DEC *)calloc(1, sizeof(struct PS_DEC));
  PS_DEC_COEFFICIENTS *coef =
      (PS_DEC_COEFFICIENTS *)calloc(1, sizeof(PS_DEC_COEFFICIENTS));

  h->noSubSamples = (SCHAR)noSubSamples;
  h->noChannels = NO_QMF_CHANNELS;
  h->psDecodedPrv = (UCHAR)prevDecoded;
  h->bsLastSlot = 0;
  h->bsReadSlot = 0;
  h->processSlot = 0;
  h->bPsDataAvail[0] = ppt_none;
  h->bPsDataAvail[1] = ppt_none;

  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, (UCHAR *)payload, payloadBytes, validBits, BS_READER);

  out->bitsRead = (int)ReadPsData(h, &bs, validBits);
  out->psProcessFlag = DecodePs(h, (UCHAR)frameError, coef);

  MPEG_PS_BS_DATA *bsData = &h->bsData[h->processSlot].mpeg;
  out->noEnv = bsData->noEnv;
  out->freqResIid = bsData->freqResIid;
  out->freqResIcc = bsData->freqResIcc;
  out->bFineIidQ = bsData->bFineIidQ;
  for (int i = 0; i < 6; i++) out->envStartStop[i] = bsData->aEnvStartStop[i];

  for (int e = 0; e < MAX_NO_PS_ENV; e++) {
    for (int b = 0; b < NO_HI_RES_IID_BINS; b++) {
      out->iidMapped[e * 34 + b] = coef->aaIidIndexMapped[e][b];
      out->iccMapped[e * 34 + b] = coef->aaIccIndexMapped[e][b];
    }
  }

  free(coef);
  free(h);
}

} /* extern "C" */
