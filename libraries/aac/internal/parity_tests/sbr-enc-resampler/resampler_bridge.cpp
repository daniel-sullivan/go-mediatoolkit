// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Parity bridge for the SBR-encoder 2:1 time-domain downsampler
// (resampler.cpp). Inits the downsampler for a given cutoff + ratio, streams a
// deterministic int16 signal through it in blocks (to exercise the persistent
// biquad delay lines across calls) and returns every output sample for
// exact-integer comparison vs the Go port.

#include <stdint.h>
#include <string.h>

#include "resampler.h"

extern "C" {

// resample_run downsamples nIn int16 samples (in[]) by `ratio`, processing them
// in `blocks` equal chunks across separate FDKaacEnc_Downsample calls. Returns
// the total output count and writes outputs to out[] plus the init delay.
int resample_run(int wc, int ratio, const short *in, int nIn, int blocks,
                 short *out, int *delayOut) {
  DOWNSAMPLER ds;
  memset(&ds, 0, sizeof(ds));
  FDKaacEnc_InitDownsampler(&ds, wc, ratio);
  *delayOut = ds.delay;

  int per = nIn / blocks;        // input samples per block (multiple of ratio)
  int totalOut = 0;
  for (int b = 0; b < blocks; b++) {
    INT nOut = 0;
    FDKaacEnc_Downsample(&ds, (INT_PCM *)(in + b * per), per,
                         (INT_PCM *)(out + totalOut), &nOut);
    totalOut += nOut;
  }
  return totalOut;
}

} /* extern "C" */
