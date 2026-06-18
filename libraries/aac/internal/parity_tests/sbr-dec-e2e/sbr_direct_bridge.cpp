// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// sbr_direct_bridge.cpp drives the genuine vendored fdk SBR decoder
// (sbrDecoder_Open / InitElement / Parse / Apply) over a per-frame core int32
// input + sbr_extension_data bit location, returning the int32 SBR output per
// frame (interleaved, 2048*ch). The pure-Go heaac pipeline is compared against
// this at int32 resolution (pre-narrowing) to localize any divergence. This is a
// standalone C++ translation unit (not inline cgo) so the FDK_INLINE bitstream
// helpers link cleanly. FDK-AAC-derived; see libfdk/COPYING.

#include <stdlib.h>
#include <string.h>
#include "sbrdecoder.h"
#include "FDK_qmf_domain.h"
#include "FDK_bitstream.h"
#include "syslib_channelMapDescr.h"

extern "C" int sbr_direct(int coreRate, int outRate, int ch, int nf,
                          const int *coreInputs,
                          const unsigned char *auFlat, const int *auLens,
                          const int *startBits, const int *countBits, const int *crcFlags,
                          const int *prevElements, int *sbrOut) {
  FDK_QMF_DOMAIN qmfDomain;
  memset(&qmfDomain, 0, sizeof(qmfDomain));
  HANDLE_SBRDECODER self = NULL;
  if (sbrDecoder_Open(&self, &qmfDomain) != SBRDEC_OK) return -1;

  MP4_ELEMENT_ID elementID = (ch == 2) ? ID_CPE : ID_SCE;
  UCHAR configChanged = 0;
  if (sbrDecoder_InitElement(self, coreRate, outRate, 1024, AOT_SBR, elementID, 0,
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

    // Configure the QMF domain (allocate filter-bank state) before applying.
    // The real decoder does this every frame right before sbrDecoder_Apply
    // (aacdecoder_lib.cpp:1440); without it the QMF banks have no state and the
    // SBR output is all zeros.
    if (FDK_QmfDomain_Configure(&qmfDomain) != QMF_DOMAIN_OK) {
      sbrDecoder_Close(&self);
      return -3;
    }

    LONG input[2 * 1024];
    for (int i = 0; i < ch * 1024; i++) input[i] = coreInputs[(size_t)f * ch * 1024 + i];
    LONG timeData[2 * 2048];
    memset(timeData, 0, sizeof(timeData));
    int numCh = ch, sr = outRate, outHr = 0;
    UCHAR psDecoded = 0;
    sbrDecoder_Apply(self, input, timeData, 2 * 2048, &numCh, &sr, &mapDescr, 0,
                     1, &psDecoded, 3, &outHr);
    for (int i = 0; i < ch * 2048; i++) sbrOut[(size_t)f * ch * 2048 + i] = timeData[i];
  }

  sbrDecoder_Close(&self);
  return 0;
}
