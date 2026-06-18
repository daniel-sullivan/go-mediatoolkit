// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC SBR-encoder transient detector
 * (libSBRenc/src/tran_det.cpp). These extern "C" entry points link the GENUINE
 * vendored tran_det.cpp (plus its sbr_misc.cpp / libFDK math sibling TUs that
 * this package compiles its own copy of via the *_cgo.cpp shims), so the oracle
 * is the real reference, not a hand-twin. It NEVER imports libraries/aac.
 *
 * The SBR encoder is FIXED-POINT (FIXP_DBL == int32 Q-format), so the oracle
 * asserts EXACT int equality against the Go port in internal/nativeaac/sbr.
 */

#include <stdint.h>
#include <string.h>

#include "tran_det.h"
#include "sbr_encoder.h"

extern "C" {

/* tparity_init_fast_tran inits a FAST_TRAN_DETECTOR and copies out its dBf_m /
 * dBf_e weighting ROM and the derived start/stop bands. */
void tparity_init_fast_tran(int timeSlotsPerFrame, int bandwidthQmfSlot,
                            int noQmfChannels, int sbrQmf1stBand,
                            int32_t *dBfMOut, int *dBfEOut, int *startBand,
                            int *stopBand) {
  FAST_TRAN_DETECTOR h;
  memset(&h, 0, sizeof(h));
  FDKsbrEnc_InitSbrFastTransientDetector(&h, timeSlotsPerFrame, bandwidthQmfSlot,
                                         noQmfChannels, sbrQmf1stBand);
  for (int i = 0; i < 64; i++) {
    dBfMOut[i] = (int32_t)h.dBf_m[i];
    dBfEOut[i] = h.dBf_e[i];
  }
  *startBand = h.startBand;
  *stopBand = h.stopBand;
}

/* tparity_fast_tran inits the fast detector then runs it over `rows` rows of the
 * flat energy matrix (row stride noQmfChannels) and writes tran_vector[0..2]. */
void tparity_fast_tran(const int32_t *energyFlat, int rows, int noQmfChannels,
                       const int *scaleEnergies, int yBufferWriteOffset,
                       int timeSlotsPerFrame, int bandwidthQmfSlot,
                       int sbrQmf1stBand, unsigned char *tranVector) {
  FAST_TRAN_DETECTOR h;
  memset(&h, 0, sizeof(h));
  FDKsbrEnc_InitSbrFastTransientDetector(&h, timeSlotsPerFrame, bandwidthQmfSlot,
                                         noQmfChannels, sbrQmf1stBand);

  const FIXP_DBL *Energies[32 + 2];
  for (int i = 0; i < rows; i++) {
    Energies[i] = (const FIXP_DBL *)energyFlat + (long)i * noQmfChannels;
  }
  FDKsbrEnc_fastTransientDetect(&h, Energies, scaleEnergies, yBufferWriteOffset,
                                tranVector);
}

/* Build a minimal sbrConfiguration with only the fields the transient-detector
 * init reads. */
static void seedConfig(sbrConfiguration *c, int standardBitrate, int nChannels,
                       int codecBitrate, int tran_thr, int tran_det_mode) {
  memset(c, 0, sizeof(*c));
  c->codecSettings.standardBitrate = standardBitrate;
  c->codecSettings.nChannels = nChannels;
  c->codecSettings.bitRate = codecBitrate;
  c->tran_thr = tran_thr;
  c->tran_det_mode = tran_det_mode;
}

/* tparity_init_tran inits a SBR_TRANSIENT_DETECTOR and copies out tran_thr,
 * split_thr_m, split_thr_e. lowDelay sets the SBR_SYNTAX_LOW_DELAY flag. */
void tparity_init_tran(int lowDelay, int frameSize, int sampleFreq,
                       int standardBitrate, int nChannels, int codecBitrate,
                       int tran_thr, int tran_det_mode, int tran_fc, int no_cols,
                       int no_rows, int frameShift, int tran_off,
                       int32_t *tranThrOut, int32_t *splitThrMOut,
                       int *splitThrEOut) {
  sbrConfiguration cfg;
  seedConfig(&cfg, standardBitrate, nChannels, codecBitrate, tran_thr,
             tran_det_mode);
  SBR_TRANSIENT_DETECTOR h;
  unsigned int flags = lowDelay ? SBR_SYNTAX_LOW_DELAY : 0;
  FDKsbrEnc_InitSbrTransientDetector(&h, flags, frameSize, sampleFreq, &cfg,
                                     tran_fc, no_cols, no_rows, 0, 0, frameShift,
                                     tran_off);
  *tranThrOut = (int32_t)h.tran_thr;
  *splitThrMOut = (int32_t)h.split_thr_m;
  *splitThrEOut = h.split_thr_e;
}

/* tparity_tran inits a standard detector then runs FDKsbrEnc_transientDetect
 * over `rows` rows of the flat energy matrix (row stride rowStride). It writes
 * transient_info[0..2] and copies out the mutated thresholds[64] + transients
 * ring. */
void tparity_tran(const int32_t *energyFlat, int rows, int rowStride,
                  const int *scaleEnergies, int lowDelay, int frameSize,
                  int sampleFreq, int standardBitrate, int nChannels,
                  int codecBitrate, int tran_thr, int tran_det_mode, int tran_fc,
                  int no_cols, int no_rows, int frameShift, int tran_off,
                  int yBufferWriteOffset, int yBufferSzShift, int timeStep,
                  int frameMiddleBorder, unsigned char *transientInfo,
                  int32_t *thresholdsOut, int32_t *transientsOut) {
  sbrConfiguration cfg;
  seedConfig(&cfg, standardBitrate, nChannels, codecBitrate, tran_thr,
             tran_det_mode);
  SBR_TRANSIENT_DETECTOR h;
  unsigned int flags = lowDelay ? SBR_SYNTAX_LOW_DELAY : 0;
  FDKsbrEnc_InitSbrTransientDetector(&h, flags, frameSize, sampleFreq, &cfg,
                                     tran_fc, no_cols, no_rows,
                                     yBufferWriteOffset, yBufferSzShift,
                                     frameShift, tran_off);

  FIXP_DBL *Energies[32 + 2];
  for (int i = 0; i < rows; i++) {
    Energies[i] = (FIXP_DBL *)energyFlat + (long)i * rowStride;
  }
  int scEn[2] = {scaleEnergies[0], scaleEnergies[1]};

  FDKsbrEnc_transientDetect(&h, Energies, scEn, transientInfo,
                            yBufferWriteOffset, yBufferSzShift, timeStep,
                            frameMiddleBorder);

  for (int i = 0; i < 64; i++) thresholdsOut[i] = (int32_t)h.thresholds[i];
  for (int i = 0; i < 32 + (32 / 2); i++)
    transientsOut[i] = (int32_t)h.transients[i];
}

/* tparity_frame_splitter inits a standard detector, seeds prevLowBandEnergy,
 * runs FDKsbrEnc_frameSplitter, and reports tran_vector + updated prev energies
 * + tonality. */
void tparity_frame_splitter(const int32_t *energyFlat, int rows, int rowStride,
                            const int *scaleEnergies, int lowDelay, int frameSize,
                            int sampleFreq, int standardBitrate, int nChannels,
                            int codecBitrate, int tran_thr, int tran_det_mode,
                            int tran_fc, int no_cols, int no_rows, int frameShift,
                            int tran_off, int32_t prevLowBandEnergy,
                            const unsigned char *freqBandTable,
                            unsigned char *tranVector, int yBufferWriteOffset,
                            int yBufferSzShift, int nSfb, int timeStep,
                            int32_t tonalityIn, int32_t *prevLowOut,
                            int32_t *prevHighOut, int32_t *tonalityOut) {
  sbrConfiguration cfg;
  seedConfig(&cfg, standardBitrate, nChannels, codecBitrate, tran_thr,
             tran_det_mode);
  SBR_TRANSIENT_DETECTOR h;
  unsigned int flags = lowDelay ? SBR_SYNTAX_LOW_DELAY : 0;
  FDKsbrEnc_InitSbrTransientDetector(&h, flags, frameSize, sampleFreq, &cfg,
                                     tran_fc, no_cols, no_rows,
                                     yBufferWriteOffset, yBufferSzShift,
                                     frameShift, tran_off);
  h.prevLowBandEnergy = (FIXP_DBL)prevLowBandEnergy;

  FIXP_DBL *Energies[32 + 2];
  for (int i = 0; i < rows; i++) {
    Energies[i] = (FIXP_DBL *)energyFlat + (long)i * rowStride;
  }
  int scEn[2] = {scaleEnergies[0], scaleEnergies[1]};
  FIXP_DBL tonality = (FIXP_DBL)tonalityIn;

  FDKsbrEnc_frameSplitter(Energies, scEn, &h, (UCHAR *)freqBandTable, tranVector,
                          yBufferWriteOffset, yBufferSzShift, nSfb, timeStep,
                          no_cols, &tonality);

  *prevLowOut = (int32_t)h.prevLowBandEnergy;
  *prevHighOut = (int32_t)h.prevHighBandEnergy;
  *tonalityOut = (int32_t)tonality;
}

} /* extern "C" */
