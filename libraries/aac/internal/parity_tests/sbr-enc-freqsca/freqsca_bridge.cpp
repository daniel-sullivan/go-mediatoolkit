// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Parity bridge for the SBR-encoder frequency band-table construction
// (sbrenc_freq_sca.cpp). Drives the public entry points (FindStartAndStopBand,
// UpdateFreqScale, UpdateHiRes, UpdateLoRes, getSbrStart/StopFreqRAW) and returns
// the full band tables for exact-integer comparison vs the Go port.

#include <stdint.h>
#include <string.h>

#include "sbrenc_freq_sca.h"
#include "sbr_def.h"

extern "C" {

int freqsca_startstop(int srSbr, int srCore, int noChannels, int startFreq,
                      int stopFreq, int *k0Out, int *k2Out) {
  INT k0 = 0, k2 = 0;
  INT err = FDKsbrEnc_FindStartAndStopBand(srSbr, srCore, noChannels, startFreq,
                                           stopFreq, &k0, &k2);
  *k0Out = k0;
  *k2Out = k2;
  return err;
}

// Returns numBands (or -1 on error) and fills vkOut (numBands+1 UCHARs).
int freqsca_updatefreqscale(int k0, int k2, int freqScale, int alterScale,
                            unsigned char *vkOut) {
  UCHAR vk[MAX_FREQ_COEFFS + 1];
  memset(vk, 0, sizeof(vk));
  INT numBands = 0;
  INT err = FDKsbrEnc_UpdateFreqScale(vk, &numBands, k0, k2, freqScale, alterScale);
  if (err) return -1;
  for (int i = 0; i <= numBands; i++) vkOut[i] = vk[i];
  return numBands;
}

// Drives UpdateHiRes + UpdateLoRes given a master table. Returns numHires; fills
// hiresOut (numHires+1), loresOut (numLores+1), and *numLoresOut, *xoverOut.
int freqsca_hires_lores(const unsigned char *vk, int numMaster, int xoverBand,
                        unsigned char *hiresOut, unsigned char *loresOut,
                        int *numLoresOut, int *xoverOut) {
  UCHAR vkLocal[MAX_FREQ_COEFFS + 1];
  for (int i = 0; i <= numMaster; i++) vkLocal[i] = vk[i];

  UCHAR hires[MAX_FREQ_COEFFS + 1];
  UCHAR lores[MAX_FREQ_COEFFS + 1];
  memset(hires, 0, sizeof(hires));
  memset(lores, 0, sizeof(lores));

  INT numHires = 0;
  INT xb = xoverBand;
  FDKsbrEnc_UpdateHiRes(hires, &numHires, vkLocal, numMaster, &xb);

  INT numLores = 0;
  FDKsbrEnc_UpdateLoRes(lores, &numLores, hires, numHires);

  for (int i = 0; i <= numHires; i++) hiresOut[i] = hires[i];
  for (int i = 0; i <= numLores; i++) loresOut[i] = lores[i];
  *numLoresOut = numLores;
  *xoverOut = xb;
  return numHires;
}

int freqsca_startfreq_raw(int startFreq, int fsCore) {
  return FDKsbrEnc_getSbrStartFreqRAW(startFreq, fsCore);
}

int freqsca_stopfreq_raw(int stopFreq, int fsCore) {
  return FDKsbrEnc_getSbrStopFreqRAW(stopFreq, fsCore);
}

} /* extern "C" */
