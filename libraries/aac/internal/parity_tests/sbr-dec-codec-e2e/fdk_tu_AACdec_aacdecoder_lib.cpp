// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper for the aacdecoder_lib translation unit of the
// sbr-dec-codec-e2e parity slice. This slice compiles its OWN copy of the fdk
// TUs (it never imports libraries/aac); see libfdk/COPYING.
//
// UNLIKE the other fdk_tu_*.cpp wrappers, this TU does NOT #include the shared
// vendored aacdecoder_lib.cpp. Instead it compiles a PARITY-LOCAL copy
// (fdk_local_aacdecoder_lib_tapped.cpp) that adds a single pre-SBR core tap
// (fdk_core_tap) so the oracle can expose the genuine fdk AAC-LC core PCM at the
// core rate, BEFORE sbrDecoder_Apply. The shared vendored libfdk is never
// modified. Double-compilation of the aacDecoder symbols is avoided because the
// local copy replaces — does not supplement — the shared TU here.
#include "_fdk_local_aacdecoder_lib_tapped.cpp"
