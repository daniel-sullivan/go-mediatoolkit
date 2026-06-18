// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/FDK_bitbuffer.cpp, its own TU: the byte-buffer read/write
// primitives the FDK bit reader/writer (used to build payloads + parse the SBR
// channel element) are built on. See cgo.go.
#include "libfdk/libFDK/src/FDK_bitbuffer.cpp"
