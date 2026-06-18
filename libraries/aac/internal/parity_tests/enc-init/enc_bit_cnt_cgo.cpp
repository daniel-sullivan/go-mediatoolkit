// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Sibling TU: compiles the genuine vendored libfdk/libAACenc/src/bit_cnt.cpp as its
// own translation unit (file-static helpers stay file-local). Linked by the
// enc-init oracle bridge (oracle_kind == real_vendored).
#include "libfdk/libAACenc/src/bit_cnt.cpp"
