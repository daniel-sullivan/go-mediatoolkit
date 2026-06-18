// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// sbr_direct_ps_bridge.cpp drives the genuine vendored fdk SBR+PS decoder
// (sbrDecoder_Open / InitElement(AOT_PS) / Parse / Apply with psDecoded=1) over a
// per-frame MONO core int32 input + sbr_extension_data bit location, returning the
// int32 STEREO PS output per frame (interleaved, 2*2048). The pure-Go heaac PS
// pipeline is compared against this at int32 resolution (pre-narrowing) to separate
// a genuine integration divergence from int16-narrowing rounding. Standalone TU so
// the FDK_INLINE bitstream helpers link cleanly. FDK-AAC-derived; see libfdk/COPYING.

#include <stdlib.h>
#include <string.h>
#include "sbrdecoder.h"
#include "FDK_qmf_domain.h"
#include "FDK_bitstream.h"
#include "syslib_channelMapDescr.h"

// sbr_direct_ps drives the genuine fdk SBR decoder with parametric stereo enabled,
// frame-immediate (the SBR/PS payload of frame N applied to the mono core of frame
// N), so the native frame-immediate PS path can be compared at int32 resolution.
// coreInputs is the mono core (1024 int32 per frame); sbrOut receives 2*2048 int32
// (interleaved stereo) per frame.
extern "C" int sbr_direct_ps(int coreRate, int outRate, int nf,
                             const int *coreInputs,
                             const unsigned char *auFlat, const int *auLens,
                             const int *startBits, const int *countBits, const int *crcFlags,
                             const int *prevElements, int *sbrOut) {
  FDK_QMF_DOMAIN qmfDomain;
  memset(&qmfDomain, 0, sizeof(qmfDomain));
  HANDLE_SBRDECODER self = NULL;
  if (sbrDecoder_Open(&self, &qmfDomain) != SBRDEC_OK) return -1;

  UCHAR configChanged = 0;
  // AOT_PS, single ID_SCE element: InitElement promotes to 2 channels + CreatePsDec.
  if (sbrDecoder_InitElement(self, coreRate, outRate, 1024, AOT_PS, ID_SCE, 0,
                             2, 0, 0, &configChanged, 1) != SBRDEC_OK) {
    sbrDecoder_Close(&self);
    return -2;
  }

  FDK_channelMapDescr mapDescr;
  FDK_chMapDescr_init(&mapDescr, NULL, 0, 0);

  UINT acElFlags[8];
  memset(acElFlags, 0, sizeof(acElFlags));
  UCHAR drmBuf[512];

  int auOff = 0;
  for (int f = 0; f < nf; f++) {
    int auLen = auLens[f];
    int bufSize = 1;
    while (bufSize < auLen) bufSize <<= 1;
    unsigned char *buf = (unsigned char *)calloc(bufSize, 1);
    memcpy(buf, auFlat + auOff, auLen);
    auOff += auLen;

    FDK_BITSTREAM bs;
    FDKinitBitStream(&bs, buf, bufSize, auLen * 8, BS_READER);
    FDKpushFor(&bs, startBits[f]);

    int count = countBits[f];
    sbrDecoder_Parse(self, &bs, drmBuf, 512, &count, countBits[f], crcFlags[f],
                     (MP4_ELEMENT_ID)prevElements[f], 0, 0, acElFlags);
    free(buf);

    if (FDK_QmfDomain_Configure(&qmfDomain) != QMF_DOMAIN_OK) {
      sbrDecoder_Close(&self);
      return -3;
    }

    LONG input[1024];
    for (int i = 0; i < 1024; i++) input[i] = coreInputs[(size_t)f * 1024 + i];
    LONG timeData[2 * 2048];
    memset(timeData, 0, sizeof(timeData));
    int numCh = 1, sr = outRate, outHr = 0;
    UCHAR psDecoded = 1; // request PS
    sbrDecoder_Apply(self, input, timeData, 2 * 2048, &numCh, &sr, &mapDescr, 0,
                     1, &psDecoded, 3, &outHr);
    for (int i = 0; i < 2 * 2048; i++) sbrOut[(size_t)f * 2 * 2048 + i] = timeData[i];
  }

  sbrDecoder_Close(&self);
  return 0;
}
