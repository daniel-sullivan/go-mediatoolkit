// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package channelmap pins the Go port of the Fraunhofer FDK-AAC encoder
// channel-mapping init/config tier — FDKaacEnc_DetermineEncoderMode /
// FDKaacEnc_InitChannelMapping / FDKaacEnc_InitElementBits
// (libAACenc/src/channel_map.cpp), plus the static FDKaacEnc_initElement they
// drive — against the vendored C, compiled into this test binary via cgo. The
// tier resolves the encoder CHANNEL_MODE, lays out the per-access-unit
// ELEMENT_INFO list (element types, coder-channel indices, instance tags, the
// AAC relativeBits split) and splits the bit budget across QC_STATE.elementBits.
//
// This package compiles its OWN copy of the needed vendored C source
// (channel_map.cpp + syslib_channelMapDescr.cpp for the channel-map descriptor +
// FDK_tools_rom.cpp for invCount/GetInvInt + genericStds.cpp for FDKmemclear) and
// NEVER imports libraries/aac — importing it would link a second copy of the FDK
// reference and clash on static symbols. It MAY, and does, import the pure-Go
// internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: this tier is a pure INTEGER fixed-point kernel (FIXP_DBL ==
// int32 relativeBits; the bit splits are fMult int64-product>>32 /
// CountLeadingBits / GetInvInt integer kernels) with no transcendental, so it
// asserts EXACT int equality unconditionally. The oracle is the genuine
// FDKaacEnc_* symbols (oracle_kind == real_vendored).
package channelmap

/*
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>
#include <stdlib.h>

// cm_snapshot / eb_snapshot must match the bridge.cpp layout exactly.
typedef struct {
  int32_t encMode;
  int32_t nChannels;
  int32_t nChannelsEff;
  int32_t nElements;
  int32_t elType[8];
  int32_t instanceTag[8];
  int32_t nChannelsInEl[8];
  int32_t channelIndex0[8];
  int32_t channelIndex1[8];
  int32_t relativeBits[8];
} cm_snapshot;

typedef struct {
  int32_t chBitrateEl[8];
  int32_t maxBitsEl[8];
  int32_t bitResLevelEl[8];
  int32_t maxBitResBitsEl[8];
  int32_t relativeBitsEl[8];
} eb_snapshot;

extern int eparity_determine_encoder_mode(int32_t inMode, int nChannels,
                                          int32_t *outMode);
extern int eparity_init_channel_mapping(int32_t mode, int co, cm_snapshot *out);
extern int eparity_init_element_bits(int32_t mode, int co, int bitrateTot,
                                     int averageBitsTot, int maxChannelBits,
                                     eb_snapshot *out);
*/
import "C"

// cCM mirrors the C cm_snapshot.
type cCM struct {
	encMode       int32
	nChannels     int32
	nChannelsEff  int32
	nElements     int32
	elType        [8]int32
	instanceTag   [8]int32
	nChannelsInEl [8]int32
	channelIndex0 [8]int32
	channelIndex1 [8]int32
	relativeBits  [8]int32
}

// cEB mirrors the C eb_snapshot.
type cEB struct {
	chBitrateEl     [8]int32
	maxBitsEl       [8]int32
	bitResLevelEl   [8]int32
	maxBitResBitsEl [8]int32
	relativeBitsEl  [8]int32
}

// cDetermineEncoderMode runs the genuine FDKaacEnc_DetermineEncoderMode and
// returns (resolvedMode, rc).
func cDetermineEncoderMode(inMode int32, nChannels int) (int32, int) {
	var outMode C.int32_t
	rc := int(C.eparity_determine_encoder_mode(C.int32_t(inMode), C.int(nChannels), &outMode))
	return int32(outMode), rc
}

// cInitChannelMapping runs the genuine FDKaacEnc_InitChannelMapping and returns
// (snapshot, rc).
func cInitChannelMapping(mode int32, co int) (cCM, int) {
	var snap C.cm_snapshot
	rc := int(C.eparity_init_channel_mapping(C.int32_t(mode), C.int(co), &snap))
	var o cCM
	o.encMode = int32(snap.encMode)
	o.nChannels = int32(snap.nChannels)
	o.nChannelsEff = int32(snap.nChannelsEff)
	o.nElements = int32(snap.nElements)
	for i := 0; i < 8; i++ {
		o.elType[i] = int32(snap.elType[i])
		o.instanceTag[i] = int32(snap.instanceTag[i])
		o.nChannelsInEl[i] = int32(snap.nChannelsInEl[i])
		o.channelIndex0[i] = int32(snap.channelIndex0[i])
		o.channelIndex1[i] = int32(snap.channelIndex1[i])
		o.relativeBits[i] = int32(snap.relativeBits[i])
	}
	return o, rc
}

// cInitElementBits runs the genuine FDKaacEnc_InitElementBits over the genuinely
// built CHANNEL_MAPPING and returns (snapshot, rc).
func cInitElementBits(mode int32, co, bitrateTot, averageBitsTot, maxChannelBits int) (cEB, int) {
	var snap C.eb_snapshot
	rc := int(C.eparity_init_element_bits(C.int32_t(mode), C.int(co),
		C.int(bitrateTot), C.int(averageBitsTot), C.int(maxChannelBits), &snap))
	var o cEB
	for i := 0; i < 8; i++ {
		o.chBitrateEl[i] = int32(snap.chBitrateEl[i])
		o.maxBitsEl[i] = int32(snap.maxBitsEl[i])
		o.bitResLevelEl[i] = int32(snap.bitResLevelEl[i])
		o.maxBitResBitsEl[i] = int32(snap.maxBitResBitsEl[i])
		o.relativeBitsEl[i] = int32(snap.relativeBitsEl[i])
	}
	return o, rc
}
