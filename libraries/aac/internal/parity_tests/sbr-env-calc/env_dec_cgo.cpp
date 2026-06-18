// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libSBRdec/src/env_dec.cpp as its own TU (decodeSbrData and friends are
// referenced by env_extr.cpp's translation unit / linkage). Statics stay local.
#include "libfdk/libSBRdec/src/env_dec.cpp"
