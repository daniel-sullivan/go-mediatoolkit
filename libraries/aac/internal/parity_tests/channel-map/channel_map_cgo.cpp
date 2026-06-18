// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Sibling TU compiling the genuine vendored libfdk/libAACenc/src/channel_map.cpp
// — the AAC encoder channel-mapping init/config tier
// (FDKaacEnc_DetermineEncoderMode / FDKaacEnc_InitChannelMapping /
// FDKaacEnc_InitElementBits, plus the static FDKaacEnc_initElement and the
// channelModeConfig[] ROM they consult). The oracle links these GENUINE symbols
// (oracle_kind == real_vendored), so the parity test compares against the real
// reference, NOT a hand-twin.
//
// See bridge.cpp for the amalgamation-split rationale (each parity package
// compiles its OWN copy of the needed fdk C TUs and never imports
// libraries/aac).
#include "libfdk/libAACenc/src/channel_map.cpp"
