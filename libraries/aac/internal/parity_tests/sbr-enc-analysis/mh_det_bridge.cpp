// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC SBR-encoder missing-harmonics
 * detector (libSBRenc/src/mh_det.cpp). Links the GENUINE vendored mh_det.cpp
 * (+ sbr_misc.cpp + sbrenc_ram.cpp for the GetRam pools + the libFDK math TUs),
 * so the oracle is the real reference. Fixed-point => EXACT int parity. */

#include <stdint.h>
#include <string.h>

#include "mh_det.h"
#include "fram_gen.h"

extern "C" {

/* mhparity_run inits a missing-harmonics detector (Create+Init for channel 0)
 * and runs nFrames frames. Each frame supplies:
 *   - quotaFlat[f]: totNoEst*qmfChannels FIXP_DBL tonality values
 *   - signFlat[f] : totNoEst*qmfChannels INT signs
 *   - indexVector : qmfChannels SCHAR (same every frame)
 *   - frameInfoPacked[f]: the 26-int packed SBR_FRAME_INFO (the fram_gen layout,
 *       first 17 ints are the frame-info fields we reconstruct)
 *   - tranInfo[f*3..]
 *   - nrgFlat[f]: qmfChannels FIXP_DBL energy
 *   - freqBandTable: nSfb+1 UCHAR (same every frame)
 * It writes, per frame:
 *   - addHarmFlagOut[f]
 *   - addHarmSfbOut[f*nSfb..]    (pAddHarmonicsScaleFactorBands)
 *   - envCompOut[f*nSfb..]       (envelopeCompensation)
 */
void mhparity_run(int lowDelay, int sampleFreq, int frameSize, int nSfb,
                  int qmfChannels, int totNoEst, int move, int noEstPerFrame,
                  int nFrames, const int32_t *quotaFlat, const int32_t *signFlat,
                  const signed char *indexVector, const int *frameInfoPacked,
                  const unsigned char *tranInfo, const int32_t *nrgFlat,
                  const unsigned char *freqBandTable, int *addHarmFlagOut,
                  unsigned char *addHarmSfbOut, unsigned char *envCompOut) {
  SBR_MISSING_HARMONICS_DETECTOR det;
  memset(&det, 0, sizeof(det));
  FDKsbrEnc_CreateSbrMissingHarmonicsDetector(&det, 0);
  unsigned int flags = lowDelay ? SBR_SYNTAX_LOW_DELAY : 0;
  FDKsbrEnc_InitSbrMissingHarmonicsDetector(&det, sampleFreq, frameSize, nSfb,
                                            qmfChannels, totNoEst, move,
                                            noEstPerFrame, flags);

  const int FI_STRIDE = 26;

  for (int f = 0; f < nFrames; f++) {
    FIXP_DBL *quota[MAX_NO_OF_ESTIMATES];
    INT *sign[MAX_NO_OF_ESTIMATES];
    for (int e = 0; e < totNoEst; e++) {
      quota[e] = (FIXP_DBL *)quotaFlat + ((long)f * totNoEst + e) * qmfChannels;
      sign[e] = (INT *)signFlat + ((long)f * totNoEst + e) * qmfChannels;
    }

    /* Reconstruct the minimal SBR_FRAME_INFO the detector reads (nEnvelopes +
     * borders). */
    const int *fi = frameInfoPacked + (long)f * FI_STRIDE;
    SBR_FRAME_INFO frameInfo;
    memset(&frameInfo, 0, sizeof(frameInfo));
    frameInfo.nEnvelopes = fi[0];
    for (int b = 0; b < 6; b++) frameInfo.borders[b] = fi[1 + b];

    int addHarmFlag = 0;
    FDKsbrEnc_SbrMissingHarmonicsDetectorQmf(
        &det, quota, sign, (SCHAR *)indexVector, &frameInfo, tranInfo + f * 3,
        &addHarmFlag, addHarmSfbOut + (long)f * nSfb, freqBandTable, nSfb,
        envCompOut + (long)f * nSfb,
        (FIXP_DBL *)nrgFlat + (long)f * qmfChannels);

    addHarmFlagOut[f] = addHarmFlag;
  }

  FDKsbrEnc_DeleteSbrMissingHarmonicsDetector(&det);
}

} /* extern "C" */
