// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU supplying the GENUINE vendored ics-parse parse functions:
 * getSamplingRateInfo, IcsRead and IcsReadMaxSfb. channelinfo.cpp defines ONLY
 * these three (it includes only channelinfo.h / aac_rom.h / aac_ram.h /
 * FDK_bitstream.h), so compiling it whole drags no other decoder module — the
 * sfbOffsetTables ROM it consults comes from the aac_rom.cpp sibling TU. The
 * oracle bridge (oracle_ics_parse_cgo.cpp) links IcsRead WHOLE from here. */
#include "libfdk/libAACdec/src/channelinfo.cpp"
