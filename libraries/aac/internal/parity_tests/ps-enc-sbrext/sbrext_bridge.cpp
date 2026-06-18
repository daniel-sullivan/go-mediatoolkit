// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the HE-AAC v2 SBR-extension wrapper carrying ps_data():
 * the genuine FDKsbrEnc_WriteEnvSingleChannelElement (bit_sbr.cpp) with a
 * non-NULL hParametricStereo, which routes through getSbrExtendedDataSize +
 * encodeExtendedData + FDKsbrEnc_PSEnc_WritePSData -> FDKsbrEnc_WritePSBitstream
 * (ps_bitenc.cpp). Builds a full SBR_ENV_DATA + a PS_OUT from flat scenario
 * arrays identical to the Go side, runs the writer, and returns the emitted
 * payload bytes + bit count for byte-exact comparison.
 *
 * FDKsbrEnc_PSEnc_WritePSData (normally in ps_main.cpp, which drags in the whole
 * QMF/hybrid downmix machinery) is stubbed here exactly as ps_main.cpp:453-459
 * does: forward the handle's psOut[0] to FDKsbrEnc_WritePSBitstream. The handle
 * is a tiny surrogate carrying only that PS_OUT. */

#include <stdint.h>
#include <string.h>

#include "sbr_def.h"
#include "qmf.h"
#include "ps_main.h"
#include "bit_sbr.h"
#include "code_env.h"
#include "cmondata.h"
#include "fram_gen.h"
#include "ps_bitenc.h"

/* ps_main.cpp:453-459 surrogate (avoids pulling in the whole QMF/hybrid downmix
 * machinery of ps_main.cpp). bit_sbr.cpp includes ps_main.h, which declares this
 * with C++ linkage and fully defines T_PARAMETRIC_STEREO, so we use the genuine
 * struct and forward its psOut[0] exactly as ps_main.cpp does. */
INT FDKsbrEnc_PSEnc_WritePSData(HANDLE_PARAMETRIC_STEREO h,
                               HANDLE_FDK_BITSTREAM hBitstream) {
  return (h != NULL) ? FDKsbrEnc_WritePSBitstream(&h->psOut[0], hBitstream) : 0;
}

extern "C" {

void sbrext_run(
    /* sbr header / grid / env-data (a minimal but valid FIXFIX SCE) */
    int ampRes, int headerActive, int startFreq, int stopFreq, int xoverBand,
    int noOfEnvelopes, int noOfnoisebands, int bsNumEnv, int numberTimeSlots,
    const int *noScfBands, const int *ienvelopeFlat, const signed char *noiseLevels,
    const int *invfMode,
    /* ps_out fields */
    int psHeader, int enIID, int iidMode, int enICC, int iccMode, int frameClass,
    int psNEnv, const int *frameBorder, const int *deltaIID, const int *deltaICC,
    const int *iidFlat, const int *iccFlat, const int *iidLast, const int *iccLast,
    /* out */
    unsigned char *outBytes, int *nBytes, int *nBits) {

  SBR_CODE_ENVELOPE henv, hnoise;
  struct SBR_ENV_DATA envData;
  memset(&envData, 0, sizeof(envData));

  INT nSfb[2];
  nSfb[FREQ_RES_LOW] = noScfBands[0];
  nSfb[FREQ_RES_HIGH] = noScfBands[0];
  FDKsbrEnc_InitSbrCodeEnvelope(&henv, nSfb, 1, FL2FXCONST_DBL(0.3f),
                               FL2FXCONST_DBL(0.3f));
  FDKsbrEnc_InitSbrCodeEnvelope(&hnoise, nSfb, 1, FL2FXCONST_DBL(0.3f),
                               FL2FXCONST_DBL(0.3f));
  FDKsbrEnc_InitSbrHuffmanTables(&envData, &henv, &hnoise, (AMP_RES)ampRes);

  static SBR_GRID grid;
  memset(&grid, 0, sizeof(grid));
  grid.bufferFrameStart = 0;
  grid.numberTimeSlots = numberTimeSlots;
  grid.frameClass = (FRAME_CLASS)0; /* FIXFIX */
  grid.bs_num_env = bsNumEnv;
  grid.v_f[0] = 0;
  envData.hSbrBSGrid = &grid;

  envData.ldGrid = 0;
  envData.noOfEnvelopes = noOfEnvelopes;
  envData.noOfnoisebands = noOfnoisebands;
  envData.balance = 0;
  envData.currentAmpResFF = (AMP_RES)ampRes;

  for (int i = 0; i < noOfEnvelopes; i++) {
    envData.noScfBands[i] = noScfBands[i];
    envData.domain_vec[i] = 0;
    for (int b = 0; b < MAX_FREQ_COEFFS; b++)
      envData.ienvelope[i][b] = ienvelopeFlat[i * MAX_FREQ_COEFFS + b];
  }
  envData.domain_vec_noise[0] = 0;
  envData.domain_vec_noise[1] = 0;
  for (int i = 0; i < MAX_FREQ_COEFFS; i++)
    envData.sbr_noise_levels[i] = noiseLevels[i];
  for (int i = 0; i < noOfnoisebands; i++)
    envData.sbr_invf_mode_vec[i] = (INVF_MODE)invfMode[i];
  envData.addHarmonicFlag = 0;
  envData.noHarmonics = 0;

  SBR_HEADER_DATA hdr;
  memset(&hdr, 0, sizeof(hdr));
  hdr.sbr_amp_res = (AMP_RES)ampRes;
  hdr.sbr_start_frequency = startFreq;
  hdr.sbr_stop_frequency = stopFreq;
  hdr.sbr_xover_band = xoverBand;
  hdr.freqScale = 0;
  hdr.alterScale = 0;
  hdr.sbr_noise_bands = 0;
  hdr.sbr_limiter_bands = 0;
  hdr.sbr_limiter_gains = 0;
  hdr.sbr_interpol_freq = 0;
  hdr.sbr_smoothing_length = 0;
  hdr.header_extra_1 = 0;
  hdr.header_extra_2 = 0;
  hdr.coupling = 0;

  SBR_BITSTREAM_DATA bsData;
  memset(&bsData, 0, sizeof(bsData));
  bsData.HeaderActive = headerActive;

  /* Build the parametric-stereo handle carrying the PS_OUT to embed. The real
   * PARAMETRIC_STEREO is large; use a static zeroed instance and fill psOut[0]
   * (all FDKsbrEnc_PSEnc_WritePSData reads). */
  static PARAMETRIC_STEREO psInst;
  memset(&psInst, 0, sizeof(psInst));
  PS_OUT *pso = &psInst.psOut[0];
  pso->enablePSHeader = psHeader;
  pso->enableIID = enIID;
  pso->iidMode = iidMode;
  pso->enableICC = enICC;
  pso->iccMode = iccMode;
  pso->enableIpdOpd = 0;
  pso->frameClass = frameClass;
  pso->nEnvelopes = psNEnv;
  for (int e = 0; e < PS_MAX_ENVELOPES; e++) {
    pso->frameBorder[e] = frameBorder[e];
    pso->deltaIID[e] = (PS_DELTA)deltaIID[e];
    pso->deltaICC[e] = (PS_DELTA)deltaICC[e];
    for (int b = 0; b < PS_MAX_BANDS; b++) {
      pso->iid[e][b] = iidFlat[e * PS_MAX_BANDS + b];
      pso->icc[e][b] = iccFlat[e * PS_MAX_BANDS + b];
    }
  }
  for (int b = 0; b < PS_MAX_BANDS; b++) {
    pso->iidLast[b] = iidLast[b];
    pso->iccLast[b] = iccLast[b];
  }
  HANDLE_PARAMETRIC_STEREO ps = &psInst;

  static UCHAR mem[512];
  memset(mem, 0, sizeof(mem));
  COMMON_DATA cmonData;
  memset(&cmonData, 0, sizeof(cmonData));
  FDKinitBitStream(&cmonData.sbrBitbuf, mem, sizeof(mem), 0, BS_WRITER);

  FDKsbrEnc_WriteEnvSingleChannelElement(&hdr, ps, &bsData, &envData,
                                         &cmonData, 0 /*syntaxFlags*/);

  int bits = FDKgetValidBits(&cmonData.sbrBitbuf);
  *nBits = bits;
  int bytes = (bits + 7) >> 3;
  *nBytes = bytes;
  memcpy(outBytes, mem, bytes);
}

} /* extern "C" */
