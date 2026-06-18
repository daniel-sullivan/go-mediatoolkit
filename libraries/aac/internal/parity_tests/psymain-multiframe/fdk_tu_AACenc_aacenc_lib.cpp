// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling one vendored Fraunhofer FDK-AAC source as its
// own translation unit. This slice compiles its OWN copy of the fdk TUs (it
// never imports libraries/aac); see libfdk/COPYING. The mf_get_aac_enc accessor
// exposes the internal HANDLE_AAC_ENC (struct AACENCODER is private to this TU)
// so the multi-frame bridge can read the live psyKernel/qcKernel carried state.
#include "../../../libfdk/libAACenc/src/aacenc_lib.cpp"

extern "C" void *mf_get_aac_enc(void *encoder) {
  return (void *)((HANDLE_AACENCODER)encoder)->hAacEnc;
}
