// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC SBR envelope-gain calculation
 * (env_calc.cpp): calculateSbrEnvelope + ResetLimiterBands + the gain/noise/
 * limiter/smoothing/adjustTimeSlot math. This TU provides the extern "C" entry
 * points the Go test calls; it links the GENUINE vendored env_calc.cpp (compiled
 * by env_calc_cgo.cpp) + its setup deps (env_extr.cpp resetFreqBandTables,
 * sbrdec_freq_sca.cpp, sbr_rom.cpp), so the oracle is the real reference.
 *
 * It NEVER imports libraries/aac (no cross-package static-symbol clash); the Go
 * side (cgo.go) MAY and does import internal/nativeaac/sbr.
 *
 * env_calc is exercised as an ISOLATED unit: the bridge resolves the freq band
 * tables (resetFreqBandTables) + a synthetic 1-patch limiter layout
 * (ResetLimiterBands) and a FIX-FIX frame grid, then returns those resolved
 * fixtures so the Go side runs CalculateSbrEnvelope on BYTE-identical inputs. The
 * mutated QMF buffers + scale factors + cal-env state are then asserted EXACT.
 *
 * Integer parity: the whole SBR subsystem is fixed-point (FIXP_DBL == int32
 * Q-format, FIXP_SGL == int16 Q1.15). The gain/noise/limiter/sqrt math is
 * bit-identical regardless of -ffp-contract / vectorization (only the table
 * sqrtFixp_lookup is "transcendental"), so the oracle asserts EXACT int equality.
 *
 * PVC (pvc_mode>0) and the ELD grid are out of HE-AAC v1 scope: pvc_mode is 0 and
 * the ELD flag clear, so those branches fold away exactly as in the Go port.
 */

#include <stdint.h>
#include <string.h>

#include "env_calc.h"
#include "env_extr.h"
#include "sbrdec_freq_sca.h"
#include "sbr_rom.h"
#include "lpp_tran.h" /* PATCH_PARAM, MAX_NUM_PATCHES */
#include "pvc_dec.h"  /* PVC_DYNAMIC_DATA */

extern "C" {

#define QMF_SLOTS 64

/* Flat result the oracle compares: the resolved freq/limiter fixtures (so the Go
 * side reuses them verbatim) + the mutated QMF buffers + scale factors + the
 * persistent cal-env state calculateSbrEnvelope updates. */
struct envCalcOut {
  int err; /* ResetLimiterBands status */

  /* resolved freq band tables (fixtures fed back to the Go side) */
  uint8_t numMaster;
  uint8_t vKMaster[MAX_FREQ_COEFFS + 1];
  uint8_t nSfb[2];
  uint8_t nNfb;
  uint8_t nInvfBands;
  uint8_t lowSubband;
  uint8_t highSubband;
  uint8_t freqBandLo[MAX_FREQ_COEFFS / 2 + 1];
  uint8_t freqBandHi[MAX_FREQ_COEFFS + 1];
  uint8_t freqBandNoise[MAX_NOISE_COEFFS + 1];
  uint8_t limiterBandTable[MAX_NUM_LIMITERS + 1];
  uint8_t noLimiterBands;

  /* mutated QMF buffers (slot-major nSlots*64) written back */
  /* scale factors after calculateSbrEnvelope */
  int hbScale;
  int ovHbScale;

  /* persistent cal-env state */
  int prevTranEnv;
  uint8_t harmIndex;
  int phaseIndex;
  uint32_t harmFlagsPrev[ADD_HARMONICS_FLAGS_SIZE];
  uint32_t harmFlagsPrevActive[ADD_HARMONICS_FLAGS_SIZE];
};

/* qparity_calculateSbrEnvelope builds the genuine header / patch-limiter / frame
 * fixtures, runs the genuine calculateSbrEnvelope over the slot-major QMF buffers
 * (realFlat/imagFlat, mutated in place), and writes the resolved fixtures + the
 * resulting scale factors + cal-env state into *out. useLP selects the real-only
 * LP path (imag ignored). */
int qparity_calculateSbrEnvelope(
    /* header config */
    unsigned int sbrProcSmplRate, int startFreq, int stopFreq, int freqScale,
    int alterScale, int noiseBands, int xoverBand, int numberOfAnalysisBands,
    int ampResolution, int numberTimeSlots, int timeStep, int interpolFreq,
    int smoothingLength, int limiterBands, int limiterGains, unsigned int flags,
    /* frame config */
    int nEnvelopes, int tranEnv, int iTESactive, int interTempShapeMode0,
    const uint8_t *borders, const uint8_t *freqRes, int nNoiseEnvelopes,
    const uint8_t *bordersNoise,
    const int16_t *iEnvelope, int nIEnv, const int16_t *sbrNoiseFloorLevel,
    int nNoise, const uint32_t *addHarmonics,
    /* scale + run config */
    int hbScale, int ovHbScale, int ovLbScale, int lbScale, int useLP,
    int frameErrorFlag, int nSlots,
    int32_t *realFlat, int32_t *imagFlat, int32_t *degreeAlias,
    envCalcOut *out) {
  memset(out, 0, sizeof(*out));

  /* --- header --- */
  SBR_HEADER_DATA hdr;
  memset(&hdr, 0, sizeof(hdr));
  hdr.syncState = SBR_ACTIVE;
  hdr.sbrProcSmplRate = sbrProcSmplRate;
  hdr.numberOfAnalysisBands = (UCHAR)numberOfAnalysisBands;
  hdr.numberTimeSlots = (UCHAR)numberTimeSlots;
  hdr.timeStep = (UCHAR)timeStep;
  hdr.frameErrorFlag = (UCHAR)frameErrorFlag;
  hdr.bs_data.startFreq = (UCHAR)startFreq;
  hdr.bs_data.stopFreq = (UCHAR)stopFreq;
  hdr.bs_data.freqScale = (UCHAR)freqScale;
  hdr.bs_data.alterScale = (UCHAR)alterScale;
  hdr.bs_data.noise_bands = (UCHAR)noiseBands;
  hdr.bs_data.interpolFreq = (UCHAR)interpolFreq;
  hdr.bs_data.smoothingLength = (UCHAR)smoothingLength;
  hdr.bs_data.limiterBands = (UCHAR)limiterBands;
  hdr.bs_data.limiterGains = (UCHAR)limiterGains;
  hdr.bs_info.xover_band = (UCHAR)xoverBand;
  hdr.bs_info.ampResolution = (UCHAR)ampResolution;
  hdr.freqBandData.freqBandTable[0] = hdr.freqBandData.freqBandTableLo;
  hdr.freqBandData.freqBandTable[1] = hdr.freqBandData.freqBandTableHi;

  resetFreqBandTables(&hdr, flags);

  FREQ_BAND_DATA *f = &hdr.freqBandData;
  f->ov_highSubband = f->highSubband; /* no header change */

  /* --- synthetic single-patch limiter layout --- */
  PATCH_PARAM patch[MAX_NUM_PATCHES + 1];
  memset(patch, 0, sizeof(patch));
  /* one patch covering the whole SBR range: guardStartBand == highSubband */
  patch[0].guardStartBand = f->highSubband;
  int noPatches = 1;

  SBR_ERROR lerr = ResetLimiterBands(
      f->limiterBandTable, &f->noLimiterBands, f->freqBandTableHi, f->nSfb[1],
      patch, noPatches, hdr.bs_data.limiterBands, /*sbrPatchingMode=*/1,
      /*xOverQmf=*/NULL, /*sbrRatio=*/0);
  out->err = (int)lerr;

  /* --- frame data --- */
  SBR_FRAME_DATA fd;
  memset(&fd, 0, sizeof(fd));
  fd.frameInfo.frameClass = 0; /* FIXFIX */
  fd.frameInfo.nEnvelopes = (UCHAR)nEnvelopes;
  for (int i = 0; i <= nEnvelopes; i++) fd.frameInfo.borders[i] = borders[i];
  for (int i = 0; i < nEnvelopes; i++) fd.frameInfo.freqRes[i] = freqRes[i];
  fd.frameInfo.tranEnv = (SCHAR)tranEnv;
  fd.frameInfo.nNoiseEnvelopes = (UCHAR)nNoiseEnvelopes;
  for (int i = 0; i <= nNoiseEnvelopes; i++)
    fd.frameInfo.bordersNoise[i] = bordersNoise[i];
  fd.iTESactive = (UCHAR)iTESactive;
  fd.interTempShapeMode[0] = (UCHAR)interTempShapeMode0;
  for (int i = 0; i < nIEnv; i++) fd.iEnvelope[i] = (FIXP_SGL)iEnvelope[i];
  for (int i = 0; i < nNoise; i++)
    fd.sbrNoiseFloorLevel[i] = (FIXP_SGL)sbrNoiseFloorLevel[i];
  for (int i = 0; i < ADD_HARMONICS_FLAGS_SIZE; i++)
    fd.addHarmonics[i] = addHarmonics[i];

  /* --- scale factors --- */
  QMF_SCALE_FACTOR sf;
  memset(&sf, 0, sizeof(sf));
  sf.hb_scale = hbScale;
  sf.ov_hb_scale = ovHbScale;
  sf.lb_scale = lbScale;
  sf.ov_lb_scale = ovLbScale;

  /* --- cal-env state (fresh, started up) --- */
  SBR_CALCULATE_ENVELOPE hCalEnv;
  memset(&hCalEnv, 0, sizeof(hCalEnv));
  hCalEnv.prevTranEnv = -1;
  resetSbrEnvelopeCalc(&hCalEnv); /* phaseIndex=0, filtBufferNoise_e=0, startUp=1 */

  /* --- PVC (disabled) --- */
  PVC_DYNAMIC_DATA pvc;
  memset(&pvc, 0, sizeof(pvc));
  pvc.pvc_mode = 0;

  /* --- QMF buffers (slot-major) --- */
  FIXP_DBL *re[QMF_SLOTS];
  FIXP_DBL *im[QMF_SLOTS];
  for (int i = 0; i < nSlots; i++) {
    re[i] = (FIXP_DBL *)realFlat + i * 64;
    im[i] = (FIXP_DBL *)imagFlat + i * 64;
  }

  calculateSbrEnvelope(&sf, &hCalEnv, &hdr, &fd, &pvc, re, useLP ? NULL : im,
                       useLP, (FIXP_DBL *)degreeAlias, flags,
                       frameErrorFlag);

  /* --- copy out fixtures + results --- */
  out->numMaster = f->numMaster;
  memcpy(out->vKMaster, f->v_k_master, MAX_FREQ_COEFFS + 1);
  out->nSfb[0] = f->nSfb[0];
  out->nSfb[1] = f->nSfb[1];
  out->nNfb = f->nNfb;
  out->nInvfBands = f->nInvfBands;
  out->lowSubband = f->lowSubband;
  out->highSubband = f->highSubband;
  memcpy(out->freqBandLo, f->freqBandTableLo, MAX_FREQ_COEFFS / 2 + 1);
  memcpy(out->freqBandHi, f->freqBandTableHi, MAX_FREQ_COEFFS + 1);
  memcpy(out->freqBandNoise, f->freqBandTableNoise, MAX_NOISE_COEFFS + 1);
  memcpy(out->limiterBandTable, f->limiterBandTable, MAX_NUM_LIMITERS + 1);
  out->noLimiterBands = f->noLimiterBands;

  out->hbScale = sf.hb_scale;
  out->ovHbScale = sf.ov_hb_scale;
  out->prevTranEnv = hCalEnv.prevTranEnv;
  out->harmIndex = hCalEnv.harmIndex;
  out->phaseIndex = hCalEnv.phaseIndex;
  for (int i = 0; i < ADD_HARMONICS_FLAGS_SIZE; i++) {
    out->harmFlagsPrev[i] = hCalEnv.harmFlagsPrev[i];
    out->harmFlagsPrevActive[i] = hCalEnv.harmFlagsPrevActive[i];
  }
  return (int)lerr;
}

} /* extern "C" */
