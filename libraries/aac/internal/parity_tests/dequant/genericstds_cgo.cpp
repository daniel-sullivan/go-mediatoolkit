// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU supplying the libSYS generic platform shims (FDKmemcpy / memory
 * helpers, etc.) that FDK_bitbuffer.cpp references. */
#include "libfdk/libSYS/src/genericStds.cpp"
