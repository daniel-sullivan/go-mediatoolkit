// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the parametric-stereo ps_data() bitstream writer
 * (libSBRenc/src/ps_bitenc.cpp, FDKsbrEnc_WritePSBitstream). Builds a PS_OUT
 * from flat scenario arrays identical to the Go side, runs the genuine writer
 * into a fresh FDK bit buffer, and returns the emitted bytes + bit count for
 * byte-exact comparison. GA baseline HE-AAC v2: enableIpdOpd stays 0. */

#include <stdint.h>
#include <string.h>

#include "sbr_def.h"
#include "ps_bitenc.h"
#include "FDK_bitstream.h"

extern "C" {

/* Run the genuine ps_data() writer.
 *
 * enablePSHeader, enableIID, iidMode, enableICC, iccMode : header controls
 * frameClass, nEnvelopes : frame info
 * frameBorder[4] : per-env (used only when frameClass==1)
 * deltaIID[4], deltaICC[4] : per-env DPCM direction (0=FREQ,1=TIME)
 * iidFlat[4*20], iccFlat[4*20] : per-env quantized indices (row-major)
 * iidLast[20], iccLast[20] : previous-frame indices for DELTA_TIME
 *
 * out: bytes (caller buffer >= 512), *nBytes, *nBits
 */
void psbw_run(int enablePSHeader, int enableIID, int iidMode, int enableICC,
              int iccMode, int frameClass, int nEnvelopes,
              const int *frameBorder, const int *deltaIID, const int *deltaICC,
              const int *iidFlat, const int *iccFlat, const int *iidLast,
              const int *iccLast, unsigned char *outBytes, int *nBytes,
              int *nBits) {
  PS_OUT psOut;
  memset(&psOut, 0, sizeof(psOut));

  psOut.enablePSHeader = enablePSHeader;
  psOut.enableIID = enableIID;
  psOut.iidMode = iidMode;
  psOut.enableICC = enableICC;
  psOut.iccMode = iccMode;
  psOut.enableIpdOpd = 0;
  psOut.frameClass = frameClass;
  psOut.nEnvelopes = nEnvelopes;

  for (int e = 0; e < PS_MAX_ENVELOPES; e++) {
    psOut.frameBorder[e] = frameBorder[e];
    psOut.deltaIID[e] = (PS_DELTA)deltaIID[e];
    psOut.deltaICC[e] = (PS_DELTA)deltaICC[e];
    for (int b = 0; b < PS_MAX_BANDS; b++) {
      psOut.iid[e][b] = iidFlat[e * PS_MAX_BANDS + b];
      psOut.icc[e][b] = iccFlat[e * PS_MAX_BANDS + b];
    }
  }
  for (int b = 0; b < PS_MAX_BANDS; b++) {
    psOut.iidLast[b] = iidLast[b];
    psOut.iccLast[b] = iccLast[b];
  }

  static UCHAR mem[512];
  memset(mem, 0, sizeof(mem));
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, mem, sizeof(mem), 0, BS_WRITER);

  FDKsbrEnc_WritePSBitstream(&psOut, &bs);

  int bits = FDKgetValidBits(&bs);
  *nBits = bits;
  int bytes = (bits + 7) >> 3;
  *nBytes = bytes;
  memcpy(outBytes, mem, bytes);
}

} /* extern "C" */
