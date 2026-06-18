// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/FDK_tools_rom.cpp as its own TU: defines the invCount[80]
// table GetInvInt (used by HFgen_preFlat's gain-vec mean) reads. See cgo.go.
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
