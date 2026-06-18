// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC SBR decode bitstream-extraction batch
 * "sbr-env-extr": env_extr.cpp (sbrGetHeaderData / sbrGetChannelElement /
 * extractFrameInfo / sbrGetEnvelope / sbrGetNoiseFloorData / ...). This
 * translation unit provides the extern "C" entry points the Go test calls; it
 * links the GENUINE vendored env_extr.cpp / sbr_rom.cpp / sbrdec_freq_sca.cpp /
 * huff_dec.cpp / fixpoint_math.cpp sibling TUs, so the oracle is the real
 * reference, never a hand-twin.
 *
 * It NEVER imports libraries/aac (no cross-package static-symbol clash). It MAY,
 * and does, import internal/nativeaac/sbr on the Go side (cgo.go).
 *
 * Strategy: the Go test supplies a flat token stream [(value,nBits),...]. The
 * bridge writes those tokens into a byte buffer with the GENUINE FDK bit WRITER
 * (FDKwriteBits) — so the exact same bytes are produced that the Go test then
 * feeds to the Go reader. The bridge then parses those bytes with the GENUINE
 * sbrGetChannelElement and returns the resulting SBR_FRAME_DATA flat. The Go
 * port parses the identical bytes and the test asserts the two parsed structs
 * are EXACT-integer equal. (The whole SBR subsystem is fixed-point — FIXP_SGL
 * iEnvelope/noise indices, UCHAR grid — so equality is exact, no tolerance.)
 *
 * The header band counts (nSfb / nInvfBands / nNfb) the parser reads come from
 * the GENUINE resetFreqBandTables, run on a header built from the same flat
 * fields the Go driver uses.
 */

#include <stdint.h>
#include <string.h>

#include "sbr_rom.h"
#include "env_extr.h"
#include "sbrdec_freq_sca.h"
#include "FDK_bitstream.h"

extern "C" {

/* qparity_buildPayload writes `nTok` tokens (value[i] for nBits[i] bits, MSB
 * first) into out[] via the genuine FDK bit writer, then byte-aligns and returns
 * the byte length written. out must be a power-of-two-sized scratch buffer
 * (bufBytes). This produces the exact bytes the Go reader will consume. */
int qparity_buildPayload(const uint32_t *value, const uint8_t *nBits, int nTok,
                         uint8_t *out, int bufBytes) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, bufBytes, 0, BS_WRITER);
  int totalBits = 0;
  for (int i = 0; i < nTok; i++) {
    FDKwriteBits(&bs, value[i], nBits[i]);
    totalBits += nBits[i];
  }
  /* flush partial cache to memory, then byte-align */
  FDKsyncCache(&bs);
  int nBytes = (totalBits + 7) >> 3;
  return nBytes;
}

/* Flat copy-out of an SBR_FRAME_DATA for comparison. Mirrors the Go
 * FrameDataResult field-for-field. */
struct frameDataOut {
  int nScaleFactors;
  uint8_t frameClass;
  uint8_t nEnvelopes;
  uint8_t borders[MAX_ENVELOPES + 1];
  uint8_t freqRes[MAX_ENVELOPES];
  int8_t tranEnv;
  uint8_t nNoiseEnvelopes;
  uint8_t bordersNoise[MAX_NOISE_ENVELOPES + 1];
  uint8_t noisePosition;
  uint8_t varLength;
  uint8_t domainVec[MAX_ENVELOPES];
  uint8_t domainVecNoise[MAX_NOISE_ENVELOPES];
  int32_t sbrInvfMode[MAX_INVF_BANDS];
  int coupling;
  int ampResolutionCurrentFrame;
  uint32_t addHarmonics[ADD_HARMONICS_FLAGS_SIZE];
  int16_t iEnvelope[MAX_NUM_ENVELOPE_VALUES];
  int16_t sbrNoiseFloorLevel[MAX_NUM_NOISE_VALUES];
};

static void copyFrameData(frameDataOut *o, const SBR_FRAME_DATA *fd) {
  const FRAME_INFO *fi = &fd->frameInfo;
  o->nScaleFactors = fd->nScaleFactors;
  o->frameClass = fi->frameClass;
  o->nEnvelopes = fi->nEnvelopes;
  memcpy(o->borders, fi->borders, sizeof(o->borders));
  memcpy(o->freqRes, fi->freqRes, sizeof(o->freqRes));
  o->tranEnv = fi->tranEnv;
  o->nNoiseEnvelopes = fi->nNoiseEnvelopes;
  memcpy(o->bordersNoise, fi->bordersNoise, sizeof(o->bordersNoise));
  o->noisePosition = fi->noisePosition;
  o->varLength = fi->varLength;
  memcpy(o->domainVec, fd->domain_vec, sizeof(o->domainVec));
  memcpy(o->domainVecNoise, fd->domain_vec_noise, sizeof(o->domainVecNoise));
  for (int i = 0; i < MAX_INVF_BANDS; i++)
    o->sbrInvfMode[i] = (int32_t)fd->sbr_invf_mode[i];
  o->coupling = fd->coupling;
  o->ampResolutionCurrentFrame = fd->ampResolutionCurrentFrame;
  memcpy(o->addHarmonics, fd->addHarmonics, sizeof(o->addHarmonics));
  memcpy(o->iEnvelope, fd->iEnvelope, sizeof(o->iEnvelope));
  memcpy(o->sbrNoiseFloorLevel, fd->sbrNoiseFloorLevel,
         sizeof(o->sbrNoiseFloorLevel));
}

/* Build an SBR_HEADER_DATA from the flat fields and run the genuine
 * resetFreqBandTables so nSfb/nInvfBands/nNfb are populated for the parser. */
static void buildHeader(SBR_HEADER_DATA *hdr, unsigned int sbrProcSmplRate,
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

/* qparity_parseChannelElement parses payload[:nBytes] (validBits valid bits)
 * with the genuine sbrGetChannelElement over a header built from the flat
 * fields, returning ok and the parsed left (+right for CPE) frame-data flat. */
int qparity_parseChannelElement(
    unsigned int sbrProcSmplRate, int startFreq, int stopFreq, int freqScale,
    int alterScale, int noiseBands, int xoverBand, int numberOfAnalysisBands,
    int ampResolution, int numberTimeSlots, int timeStep, int nCh, int overlap,
    uint8_t *payload, int bufBytes, unsigned int validBits, unsigned int flags,
    frameDataOut *outLeft, frameDataOut *outRight) {
  SBR_HEADER_DATA hdr;
  buildHeader(&hdr, sbrProcSmplRate, startFreq, stopFreq, freqScale, alterScale,
              noiseBands, xoverBand, numberOfAnalysisBands, ampResolution,
              numberTimeSlots, timeStep, flags);

  SBR_PREV_FRAME_DATA prev;
  memset(&prev, 0, sizeof(prev));

  SBR_FRAME_DATA fdLeft, fdRight;
  memset(&fdLeft, 0, sizeof(fdLeft));
  memset(&fdRight, 0, sizeof(fdRight));

  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, payload, bufBytes, validBits, BS_READER);

  int ok = sbrGetChannelElement(
      &hdr, &fdLeft, (nCh == 2) ? &fdRight : NULL, &prev, 0, &bs, NULL, flags,
      overlap);

  copyFrameData(outLeft, &fdLeft);
  if (nCh == 2) copyFrameData(outRight, &fdRight);
  return ok;
}

/* qparity_parseHeaderData parses payload[:bufBytes] with the genuine
 * sbrGetHeaderData and writes back the status + bs_data/bs_info fields (11
 * UCHARs in the fixed order the Go HeaderParseResult expects). */
int qparity_parseHeaderData(uint8_t *payload, int bufBytes,
                            unsigned int validBits, int preSyncState,
                            unsigned int flags, int fIsSbrData, int configMode,
                            uint8_t *fields /* [11] */) {
  SBR_HEADER_DATA hdr;
  memset(&hdr, 0, sizeof(hdr));
  hdr.syncState = (SBR_SYNC_STATE)preSyncState;
  hdr.freqBandData.freqBandTable[0] = hdr.freqBandData.freqBandTableLo;
  hdr.freqBandData.freqBandTable[1] = hdr.freqBandData.freqBandTableHi;

  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, payload, bufBytes, validBits, BS_READER);

  SBR_HEADER_STATUS st =
      sbrGetHeaderData(&hdr, &bs, flags, fIsSbrData, (UCHAR)configMode);

  fields[0] = hdr.bs_info.ampResolution;
  fields[1] = hdr.bs_info.xover_band;
  fields[2] = hdr.bs_data.startFreq;
  fields[3] = hdr.bs_data.stopFreq;
  fields[4] = hdr.bs_data.freqScale;
  fields[5] = hdr.bs_data.alterScale;
  fields[6] = hdr.bs_data.noise_bands;
  fields[7] = hdr.bs_data.limiterBands;
  fields[8] = hdr.bs_data.limiterGains;
  fields[9] = hdr.bs_data.interpolFreq;
  fields[10] = hdr.bs_data.smoothingLength;
  return (int)st;
}

} /* extern "C" */
