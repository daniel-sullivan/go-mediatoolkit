// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libfdk/libSYS/src/syslib_channelMapDescr.cpp so
// the FDK_chMapDescr_init / FDK_chMapDescr_getMapValue routines (and the static
// mapInfoTabDflt[] default channel-map ROM they install) that
// FDKaacEnc_InitChannelMapping / FDKaacEnc_initElement call resolve at link
// time. Genuine vendored source -> the oracle stays real_vendored.
#include "libfdk/libSYS/src/syslib_channelMapDescr.cpp"
