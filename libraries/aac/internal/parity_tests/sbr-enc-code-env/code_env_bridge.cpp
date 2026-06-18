// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the SBR-encoder DPCM envelope/noise coder
 * (libSBRenc/src/code_env.cpp). The three public entry points
 * (FDKsbrEnc_InitSbrCodeEnvelope / FDKsbrEnc_InitSbrHuffmanTables /
 * FDKsbrEnc_codeEnvelope) are non-static and defined by the genuine code_env.cpp
 * TU compiled in this package, so this bridge just drives a deterministic
 * scenario through them and exports the mutated state for bit-exact comparison
 * against the Go port. Fixed-point => EXACT int parity. */

#include <stdint.h>
#include <string.h>

#include "code_env.h"
#include "sbr_def.h"

extern "C" {

/* Run a multi-frame codeEnvelope scenario against the genuine reference.
 *
 * ampRes      : 0 == SBR_AMP_RES_1_5, 1 == SBR_AMP_RES_3_0
 * nSfbLo/Hi   : nSfb[FREQ_RES_LOW]/[FREQ_RES_HIGH]
 * deltaTAcross: deltaTAcrossFrames init arg
 * coupling/channel/headerActive : codeEnvelope args (constant across frames)
 * nFrames     : number of successive codeEnvelope calls (exercises upDate)
 * nEnvPerFr   : nEnvelopes per frame
 * freqResIn   : nFrames*nEnvPerFr freq_res values (0/1)
 * sfbNrgIn    : nFrames * (sum of bands) input SCHAR scalefactors, flattened
 * isNoise     : 1 => use noise huffman handle/start-bits, 0 => envelope
 *
 * sfbNrgOut   : delta-coded output (same layout as sfbNrgIn)
 * dirVecOut   : nFrames*nEnvPerFr direction flags
 * prevOut     : final sfb_nrg_prev[MAX_FREQ_COEFFS]
 * upDateOut   : final upDate
 */
void ceparity_run(int ampRes, int nSfbLo, int nSfbHi, int deltaTAcross,
                  int coupling, int channel, int headerActive, int nFrames,
                  int nEnvPerFr, const int *freqResIn, const signed char *sfbNrgIn,
                  int isNoise, signed char *sfbNrgOut, int *dirVecOut,
                  signed char *prevOut, int *upDateOut) {
  SBR_CODE_ENVELOPE henv;
  SBR_CODE_ENVELOPE hnoise;
  struct SBR_ENV_DATA envData;
  memset(&envData, 0, sizeof(envData));

  INT nSfb[2];
  nSfb[FREQ_RES_LOW] = nSfbLo;
  nSfb[FREQ_RES_HIGH] = nSfbHi;

  FDKsbrEnc_InitSbrCodeEnvelope(&henv, nSfb, deltaTAcross, FL2FXCONST_DBL(0.3f),
                               FL2FXCONST_DBL(0.3f));
  FDKsbrEnc_InitSbrCodeEnvelope(&hnoise, nSfb, deltaTAcross, FL2FXCONST_DBL(0.3f),
                               FL2FXCONST_DBL(0.3f));
  FDKsbrEnc_InitSbrHuffmanTables(&envData, &henv, &hnoise, (AMP_RES)ampRes);

  SBR_CODE_ENVELOPE *h = isNoise ? &hnoise : &henv;

  const int *fr = freqResIn;
  const signed char *in = sfbNrgIn;
  signed char *out = sfbNrgOut;
  int *dv = dirVecOut;

  for (int f = 0; f < nFrames; f++) {
    FREQ_RES freqRes[MAX_ENVELOPES];
    int total = 0;
    for (int e = 0; e < nEnvPerFr; e++) {
      freqRes[e] = (FREQ_RES)fr[e];
      total += (fr[e] == FREQ_RES_HIGH) ? nSfbHi : nSfbLo;
    }
    SCHAR buf[MAX_ENVELOPES * MAX_FREQ_COEFFS];
    memcpy(buf, in, total * sizeof(SCHAR));

    INT dirvec[MAX_ENVELOPES];
    FDKsbrEnc_codeEnvelope(buf, freqRes, h, dirvec, coupling, nEnvPerFr, channel,
                           headerActive);

    memcpy(out, buf, total * sizeof(SCHAR));
    for (int e = 0; e < nEnvPerFr; e++) dv[e] = dirvec[e];

    fr += nEnvPerFr;
    in += total;
    out += total;
    dv += nEnvPerFr;
  }

  memcpy(prevOut, h->sfb_nrg_prev, MAX_FREQ_COEFFS * sizeof(SCHAR));
  *upDateOut = h->upDate;
}

} /* extern "C" */
