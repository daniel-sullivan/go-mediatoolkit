// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the SBR extension-payload bitstream writer
 * (libSBRenc/src/bit_sbr.cpp). Builds a fully-formed SBR_ENV_DATA + SBR_GRID +
 * SBR_HEADER_DATA from flat scenario arrays (identical to the Go side), inits
 * the huffman tables, runs FDKsbrEnc_WriteEnvSingleChannelElement into a fresh
 * COMMON_DATA bitbuf, and returns the emitted bytes + bit count for byte-exact
 * comparison. HE-AAC v1: no PS, no ldGrid. Fixed-point => byte-identical. */

#include <stdint.h>
#include <string.h>

#include "sbr_def.h"
#include "bit_sbr.h"
#include "code_env.h"
#include "cmondata.h"
#include "fram_gen.h"

/* PS (HE-AAC v2) is out of scope; bit_sbr.cpp references this writer only when
 * hParametricStereo != NULL, which never happens here. Trap stub so the TU
 * links. bit_sbr.cpp includes ps_main.h, which declares this with C++ linkage,
 * so the stub must match (no extern "C"). */
struct T_PARAMETRIC_STEREO;
INT FDKsbrEnc_PSEnc_WritePSData(struct T_PARAMETRIC_STEREO *,
                                HANDLE_FDK_BITSTREAM) {
  return 0;
}

extern "C" {

/* Run the genuine SCE writer.
 *
 * ampRes, headerActive, headerExtra1, headerExtra2 : header controls
 * sbrAmpRes, startFreq, stopFreq, xoverBand, freqScale, alterScale, noiseBands,
 * limiterBands, limiterGains, interpolFreq, smoothingLength : header fields
 * frameClass, bsNumEnv, vf0 : FIXFIX grid (only FIXFIX in scope here)
 * bufferFrameStart, numberTimeSlots : grid
 * noOfEnvelopes, noOfnoisebands : counts
 * noScfBands[noOfEnvelopes], domainVec[noOfEnvelopes] : per-env
 * domainVecNoise[2] : per-noise-env
 * ienvelopeFlat : noOfEnvelopes * MAX_FREQ_COEFFS (row-major) ienvelope deltas
 * noiseLevels[MAX_FREQ_COEFFS] : sbr_noise_levels
 * invfMode[noOfnoisebands] : sbr_invf_mode_vec
 * addHarmonicFlag, noHarmonics, addHarmonic[MAX_FREQ_COEFFS]
 *
 * out: bytes (caller buffer >= 512), *nBytes, *nBits
 */
void bwparity_run_sce(
    int ampRes, int headerActive, int headerExtra1, int headerExtra2,
    int sbrAmpRes, int startFreq, int stopFreq, int xoverBand, int freqScale,
    int alterScale, int noiseBands, int limiterBands, int limiterGains,
    int interpolFreq, int smoothingLength, int frameClass, int bsNumEnv,
    int vf0, int bufferFrameStart, int numberTimeSlots, int noOfEnvelopes,
    int noOfnoisebands, const int *noScfBands, const int *domainVec,
    const int *domainVecNoise, const int *ienvelopeFlat,
    const signed char *noiseLevels, const int *invfMode, int addHarmonicFlag,
    int noHarmonics, const unsigned char *addHarmonic, unsigned char *outBytes,
    int *nBytes, int *nBits) {

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

  /* grid (FIXFIX) */
  static SBR_GRID grid;
  memset(&grid, 0, sizeof(grid));
  grid.bufferFrameStart = bufferFrameStart;
  grid.numberTimeSlots = numberTimeSlots;
  grid.frameClass = (FRAME_CLASS)frameClass;
  grid.bs_num_env = bsNumEnv;
  grid.v_f[0] = vf0;
  envData.hSbrBSGrid = &grid;

  envData.ldGrid = 0;
  envData.noOfEnvelopes = noOfEnvelopes;
  envData.noOfnoisebands = noOfnoisebands;
  envData.balance = 0;
  envData.currentAmpResFF = (AMP_RES)ampRes;

  for (int i = 0; i < noOfEnvelopes; i++) {
    envData.noScfBands[i] = noScfBands[i];
    envData.domain_vec[i] = domainVec[i];
    for (int b = 0; b < MAX_FREQ_COEFFS; b++)
      envData.ienvelope[i][b] = ienvelopeFlat[i * MAX_FREQ_COEFFS + b];
  }
  envData.domain_vec_noise[0] = domainVecNoise[0];
  envData.domain_vec_noise[1] = domainVecNoise[1];
  for (int i = 0; i < MAX_FREQ_COEFFS; i++)
    envData.sbr_noise_levels[i] = noiseLevels[i];
  for (int i = 0; i < noOfnoisebands; i++)
    envData.sbr_invf_mode_vec[i] = (INVF_MODE)invfMode[i];

  envData.addHarmonicFlag = addHarmonicFlag;
  envData.noHarmonics = noHarmonics;
  for (int i = 0; i < MAX_FREQ_COEFFS; i++)
    envData.addHarmonic[i] = addHarmonic[i];

  SBR_HEADER_DATA hdr;
  memset(&hdr, 0, sizeof(hdr));
  hdr.sbr_amp_res = (AMP_RES)sbrAmpRes;
  hdr.sbr_start_frequency = startFreq;
  hdr.sbr_stop_frequency = stopFreq;
  hdr.sbr_xover_band = xoverBand;
  hdr.freqScale = freqScale;
  hdr.alterScale = alterScale;
  hdr.sbr_noise_bands = noiseBands;
  hdr.sbr_limiter_bands = limiterBands;
  hdr.sbr_limiter_gains = limiterGains;
  hdr.sbr_interpol_freq = interpolFreq;
  hdr.sbr_smoothing_length = smoothingLength;
  hdr.header_extra_1 = headerExtra1;
  hdr.header_extra_2 = headerExtra2;
  hdr.coupling = 0;

  SBR_BITSTREAM_DATA bsData;
  memset(&bsData, 0, sizeof(bsData));
  bsData.HeaderActive = headerActive;

  static UCHAR mem[512];
  memset(mem, 0, sizeof(mem));
  COMMON_DATA cmonData;
  memset(&cmonData, 0, sizeof(cmonData));
  FDKinitBitStream(&cmonData.sbrBitbuf, mem, sizeof(mem), 0, BS_WRITER);

  FDKsbrEnc_WriteEnvSingleChannelElement(&hdr, NULL, &bsData, &envData,
                                         &cmonData, 0 /*syntaxFlags*/);

  int bits = FDKgetValidBits(&cmonData.sbrBitbuf);
  *nBits = bits;
  int bytes = (bits + 7) >> 3;
  *nBytes = bytes;
  memcpy(outBytes, mem, bytes);
}

} /* extern "C" */
