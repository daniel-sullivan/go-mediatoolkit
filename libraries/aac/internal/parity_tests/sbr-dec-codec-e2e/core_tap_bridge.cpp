// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// core_tap_bridge.cpp implements the parity-local pre-SBR core tap used by the
// PARITY-LOCAL copy of aacdecoder_lib.cpp
// (_fdk_local_aacdecoder_lib_tapped.cpp). The shared vendored libfdk is never
// modified; the tap exists only inside this test slice.
//
// fdk_core_tap is invoked from the tapped aacDecoder_DecodeFrame immediately
// after the core PCM is copied into the SBR `input` buffer and BEFORE
// sbrDecoder_Apply runs. It records the genuine fdk AAC-LC core PCM at the core
// sample rate (planar: channel c at c*frameSize, PCM_AAC == 32-bit int, at
// aacOutDataHeadroom), exactly the SBR INPUT — letting the oracle diff
// native-core vs fdk-core and localize the half-rate core divergence.
//
// FDK-AAC-derived; see libfdk/COPYING. Standalone TU so it links cleanly with
// the tapped lib.

#include <stdlib.h>
#include <string.h>

// PCM_AAC == LONG == INT == 32-bit int (machine_type.h:181 `#define LONG INT`,
// machine_type.h:176 `typedef signed int INT`). Capture as int; avoid pulling in
// the full fdk headers here.
typedef int FDK_CORE_PCM;

// Capture target. The oracle sets these before driving the full decoder; each
// tapped frame appends numChannels*frameSize planar samples into the buffer,
// up to capSamples. frameCount counts every tapped frame; frameSize/numCh
// record the steady core geometry observed.
static FDK_CORE_PCM *g_coreBuf = NULL;
static long g_coreCap = 0;
static long g_coreOff = 0;
static int g_coreFrames = 0;
static int g_coreFrameSize = 0;
static int g_coreNumCh = 0;

extern "C" void fdk_core_tap_reset(int *buf, long capSamples) {
  g_coreBuf = (FDK_CORE_PCM *)buf;
  g_coreCap = capSamples;
  g_coreOff = 0;
  g_coreFrames = 0;
  g_coreFrameSize = 0;
  g_coreNumCh = 0;
}

extern "C" void fdk_core_tap_info(int *frames, int *frameSize, int *numCh) {
  *frames = g_coreFrames;
  *frameSize = g_coreFrameSize;
  *numCh = g_coreNumCh;
}

// fdk_core_tap is called from the tapped aacDecoder_DecodeFrame. The PCM_AAC
// type there is INT (32-bit), matching FDK_CORE_PCM.
extern "C" void fdk_core_tap(const int *core, int numChannels, int frameSize) {
  if (g_coreBuf == NULL) return;
  long n = (long)numChannels * (long)frameSize;
  if (g_coreOff + n > g_coreCap) return; // silently stop; oracle checks frames
  for (long i = 0; i < n; i++) {
    g_coreBuf[g_coreOff + i] = (int)core[i];
  }
  g_coreOff += n;
  g_coreFrames++;
  g_coreFrameSize = frameSize;
  g_coreNumCh = numChannels;
}
