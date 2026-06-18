// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC SBR decode envelope batch
 * "sbr-dec-env": the constant ROM tables (sbr_rom.cpp) and the master/hi/lo/noise
 * frequency band-table builder (sbrdec_freq_sca.cpp). This translation unit
 * provides the extern "C" entry points the Go test calls; it links the GENUINE
 * vendored sbr_rom.cpp / sbrdec_freq_sca.cpp / fixpoint_math.cpp sibling TUs, so
 * the oracle is the real reference, never a hand-twin.
 *
 * It NEVER imports libraries/aac, so there is no cross-package static-symbol
 * clash (the amalgamation-split reasoning the sibling oracles document). It MAY,
 * and does, import internal/nativeaac/sbr on the Go side (cgo.go).
 *
 * Integer parity: the whole SBR decoder is fixed-point — FIXP_DBL == int32
 * (Q-format), FIXP_SGL == int16 (Q1.15 ROM). The ROM narrowing (FL2FXCONST_SGL/
 * DBL), the band-factor multiplies (fMult/fMultDiv2 int64-product kernels), the
 * CalcLdInt log lookups, and the UCHAR band-table arithmetic are bit-identical
 * regardless of -ffp-contract / vectorization, with no transcendental — so the
 * oracle asserts EXACT integer equality.
 */

#include <stdint.h>
#include <string.h>

#include "sbr_rom.h"
#include "env_extr.h"
#include "env_dec.h"
#include "sbrdec_freq_sca.h"
#include "FDK_bitstream.h"

/* sbrdec_mapToStdSampleRate is now provided by the genuine env_extr.cpp TU this
 * oracle compiles (env_extr_cgo.cpp), so no link-only stub is needed. */

extern "C" {

/* --- ROM table copies (sbr_rom.cpp) --------------------------------------- */

/* qparity_limGains copies the genuine in-RAM FDK_sbrDecoder_sbr_limGains_m
 * (narrowed FIXP_SGL) + _e (UCHAR) so the Go FL2FXCONST_SGL-narrowed ROM can be
 * verified entry-for-entry. */
void qparity_limGains(int16_t *mOut, uint8_t *eOut, int count) {
  for (int i = 0; i < count; i++) {
    mOut[i] = (int16_t)FDK_sbrDecoder_sbr_limGains_m[i];
    eOut[i] = (uint8_t)FDK_sbrDecoder_sbr_limGains_e[i];
  }
}

/* qparity_smoothFilter copies FDK_sbrDecoder_sbr_smoothFilter (FIXP_SGL). */
void qparity_smoothFilter(int16_t *out, int count) {
  for (int i = 0; i < count; i++)
    out[i] = (int16_t)FDK_sbrDecoder_sbr_smoothFilter[i];
}

/* qparity_limiterBandsPerOctaveDiv4 copies the FIXP_SGL + FIXP_DBL limiter-band
 * count ROM. */
void qparity_limiterBandsPerOctaveDiv4(int16_t *sglOut, int32_t *dblOut,
                                       int count) {
  for (int i = 0; i < count; i++) {
    sglOut[i] = (int16_t)FDK_sbrDecoder_sbr_limiterBandsPerOctaveDiv4[i];
    dblOut[i] = (int32_t)FDK_sbrDecoder_sbr_limiterBandsPerOctaveDiv4_DBL[i];
  }
}

/* qparity_randomPhase copies FDK_sbrDecoder_sbr_randomPhase as flat
 * [re0,im0,...] (pairCount pairs). */
void qparity_randomPhase(int16_t *out, int pairCount) {
  for (int i = 0; i < pairCount; i++) {
    out[2 * i + 0] = (int16_t)FDK_sbrDecoder_sbr_randomPhase[i][0];
    out[2 * i + 1] = (int16_t)FDK_sbrDecoder_sbr_randomPhase[i][1];
  }
}

/* qparity_invTable copies FDK_sbrDecoder_invTable (FIXP_SGL 1/x). */
void qparity_invTable(int16_t *out, int count) {
  for (int i = 0; i < count; i++)
    out[i] = (int16_t)FDK_sbrDecoder_invTable[i];
}

/* --- Frequency-band-mapping driver (sbrdec_freq_sca.cpp) ------------------- */

/* qparity_resetFreqBandTables builds an SBR_HEADER_DATA from the flat header
 * fields, runs the genuine resetFreqBandTables (which itself runs
 * sbrdecUpdateFreqScale + the hi/lo/noise derivation), and writes the full
 * band-table result back out so the Go port can be compared bit-for-bit.
 * Returns the SBR_ERROR code. The freqBandTable[2] alias pointers are wired to
 * the in-struct Lo/Hi arrays exactly as initHeaderData would. */
int qparity_resetFreqBandTables(
    unsigned int sbrProcSmplRate, int startFreq, int stopFreq, int freqScale,
    int alterScale, int noiseBands, int xoverBand, int numberOfAnalysisBands,
    unsigned int flags, unsigned char *numMaster, unsigned char *vKMaster,
    unsigned char *nSfb /*[2]*/, unsigned char *nNfb, unsigned char *nInvfBands,
    unsigned char *lowSubband, unsigned char *highSubband,
    unsigned char *freqBandLo, unsigned char *freqBandHi,
    unsigned char *freqBandNoise) {
  SBR_HEADER_DATA hdr;
  memset(&hdr, 0, sizeof(hdr));

  hdr.sbrProcSmplRate = sbrProcSmplRate;
  hdr.numberOfAnalysisBands = (UCHAR)numberOfAnalysisBands;
  hdr.bs_data.startFreq = (UCHAR)startFreq;
  hdr.bs_data.stopFreq = (UCHAR)stopFreq;
  hdr.bs_data.freqScale = (UCHAR)freqScale;
  hdr.bs_data.alterScale = (UCHAR)alterScale;
  hdr.bs_data.noise_bands = (UCHAR)noiseBands;
  hdr.bs_info.xover_band = (UCHAR)xoverBand;

  /* Wire the freqBandTable[2] alias pointers (env_extr.cpp initHeaderData). */
  hdr.freqBandData.freqBandTable[0] = hdr.freqBandData.freqBandTableLo;
  hdr.freqBandData.freqBandTable[1] = hdr.freqBandData.freqBandTableHi;

  SBR_ERROR err = resetFreqBandTables(&hdr, flags);

  FREQ_BAND_DATA *f = &hdr.freqBandData;
  *numMaster = f->numMaster;
  memcpy(vKMaster, f->v_k_master, MAX_FREQ_COEFFS + 1);
  nSfb[0] = f->nSfb[0];
  nSfb[1] = f->nSfb[1];
  *nNfb = f->nNfb;
  *nInvfBands = f->nInvfBands;
  *lowSubband = f->lowSubband;
  *highSubband = f->highSubband;
  memcpy(freqBandLo, f->freqBandTableLo, MAX_FREQ_COEFFS / 2 + 1);
  memcpy(freqBandHi, f->freqBandTableHi, MAX_FREQ_COEFFS + 1);
  memcpy(freqBandNoise, f->freqBandTableNoise, MAX_NOISE_COEFFS + 1);

  return (int)err;
}

/* --- Envelope dequantization driver (env_dec.cpp) ------------------------- */

/* qparity_buildPayload writes `nTok` tokens (value[i] for nBits[i] bits, MSB
 * first) into out[] via the genuine FDK bit writer, then byte-aligns and returns
 * the byte length written. This produces the exact bytes the Go reader consumes,
 * so both sides parse identical bits. */
int qparity_buildPayload(const uint32_t *value, const uint8_t *nBits, int nTok,
                         uint8_t *out, int bufBytes) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, bufBytes, 0, BS_WRITER);
  int totalBits = 0;
  for (int i = 0; i < nTok; i++) {
    FDKwriteBits(&bs, value[i], nBits[i]);
    totalBits += nBits[i];
  }
  FDKsyncCache(&bs);
  return (totalBits + 7) >> 3;
}

/* Flat copy-out of the dequantized SBR_FRAME_DATA fields decodeSbrData writes. */
struct decodeOut {
  int nScaleFactors;
  uint8_t coupling;
  int16_t iEnvelope[MAX_NUM_ENVELOPE_VALUES];
  int16_t sbrNoiseFloorLevel[MAX_NUM_NOISE_VALUES];
  /* prev-frame state decodeSbrData updates (delta-coding carry) */
  int16_t sfbNrgPrev[MAX_FREQ_COEFFS];
  int16_t prevNoiseLevel[MAX_NOISE_COEFFS];
  uint8_t frameError;
};

static void buildHeaderD(SBR_HEADER_DATA *hdr, unsigned int sbrProcSmplRate,
                         int startFreq, int stopFreq, int freqScale,
                         int alterScale, int noiseBands, int xoverBand,
                         int numberOfAnalysisBands, int ampResolution,
                         int numberTimeSlots, int timeStep, unsigned int flags) {
  memset(hdr, 0, sizeof(*hdr));
  hdr->syncState = SBR_ACTIVE;
  hdr->sbrProcSmplRate = sbrProcSmplRate;
  hdr->numberOfAnalysisBands = (UCHAR)numberOfAnalysisBands;
  hdr->numberTimeSlots = (UCHAR)numberTimeSlots;
  hdr->timeStep = (UCHAR)timeStep;
  hdr->bs_data.startFreq = (UCHAR)startFreq;
  hdr->bs_data.stopFreq = (UCHAR)stopFreq;
  hdr->bs_data.freqScale = (UCHAR)freqScale;
  hdr->bs_data.alterScale = (UCHAR)alterScale;
  hdr->bs_data.noise_bands = (UCHAR)noiseBands;
  hdr->bs_info.xover_band = (UCHAR)xoverBand;
  hdr->bs_info.ampResolution = (UCHAR)ampResolution;
  hdr->freqBandData.freqBandTable[0] = hdr->freqBandData.freqBandTableLo;
  hdr->freqBandData.freqBandTable[1] = hdr->freqBandData.freqBandTableHi;
  resetFreqBandTables(hdr, flags);
}

/* qparity_decodeChannelElement parses payload[:bufBytes] with the genuine
 * sbrGetChannelElement, then runs the genuine decodeSbrData over the parsed
 * frame data + a fresh (zero) previous-frame whose stopPos is set so the start
 * border matches (no concealment). It returns the dequantized iEnvelope /
 * sbrNoiseFloorLevel (and the updated prev-frame carry) so the Go port can be
 * compared bit-for-bit. nCh selects SCE (1) or CPE (2). */
int qparity_decodeChannelElement(
    unsigned int sbrProcSmplRate, int startFreq, int stopFreq, int freqScale,
    int alterScale, int noiseBands, int xoverBand, int numberOfAnalysisBands,
    int ampResolution, int numberTimeSlots, int timeStep, int nCh, int overlap,
    uint8_t *payload, int bufBytes, unsigned int validBits, unsigned int flags,
    decodeOut *outLeft, decodeOut *outRight) {
  SBR_HEADER_DATA hdr;
  buildHeaderD(&hdr, sbrProcSmplRate, startFreq, stopFreq, freqScale, alterScale,
               noiseBands, xoverBand, numberOfAnalysisBands, ampResolution,
               numberTimeSlots, timeStep, flags);

  SBR_PREV_FRAME_DATA prevL, prevR;
  memset(&prevL, 0, sizeof(prevL));
  memset(&prevR, 0, sizeof(prevR));
  /* Make the previous stop position match the current FIX-FIX start border (0)
   * so decodeEnvelope takes the normal (non-concealment) delta-decode path. */
  prevL.stopPos = (UCHAR)numberTimeSlots;
  prevR.stopPos = (UCHAR)numberTimeSlots;

  SBR_FRAME_DATA fdLeft, fdRight;
  memset(&fdLeft, 0, sizeof(fdLeft));
  memset(&fdRight, 0, sizeof(fdRight));

  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, payload, bufBytes, validBits, BS_READER);

  int ok = sbrGetChannelElement(
      &hdr, &fdLeft, (nCh == 2) ? &fdRight : NULL, &prevL, 0, &bs, NULL, flags,
      overlap);
  if (!ok) return 0;

  decodeSbrData(&hdr, &fdLeft, &prevL, (nCh == 2) ? &fdRight : NULL,
                (nCh == 2) ? &prevR : NULL);

  outLeft->nScaleFactors = fdLeft.nScaleFactors;
  outLeft->coupling = (uint8_t)fdLeft.coupling;
  memcpy(outLeft->iEnvelope, fdLeft.iEnvelope, sizeof(outLeft->iEnvelope));
  memcpy(outLeft->sbrNoiseFloorLevel, fdLeft.sbrNoiseFloorLevel,
         sizeof(outLeft->sbrNoiseFloorLevel));
  memcpy(outLeft->sfbNrgPrev, prevL.sfb_nrg_prev, sizeof(outLeft->sfbNrgPrev));
  memcpy(outLeft->prevNoiseLevel, prevL.prevNoiseLevel,
         sizeof(outLeft->prevNoiseLevel));
  outLeft->frameError = hdr.frameErrorFlag;

  if (nCh == 2) {
    outRight->nScaleFactors = fdRight.nScaleFactors;
    outRight->coupling = (uint8_t)fdRight.coupling;
    memcpy(outRight->iEnvelope, fdRight.iEnvelope, sizeof(outRight->iEnvelope));
    memcpy(outRight->sbrNoiseFloorLevel, fdRight.sbrNoiseFloorLevel,
           sizeof(outRight->sbrNoiseFloorLevel));
    memcpy(outRight->sfbNrgPrev, prevR.sfb_nrg_prev,
           sizeof(outRight->sfbNrgPrev));
    memcpy(outRight->prevNoiseLevel, prevR.prevNoiseLevel,
           sizeof(outRight->prevNoiseLevel));
    outRight->frameError = hdr.frameErrorFlag;
  }
  return ok;
}

} /* extern "C" */
