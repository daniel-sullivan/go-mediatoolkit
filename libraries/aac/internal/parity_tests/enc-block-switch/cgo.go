// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encblockswitch pins the Go port of the Fraunhofer FDK-AAC fixed-point
// ENCODE block-switch decision kernel — FDKaacEnc_BlockSwitching /
// FDKaacEnc_InitBlockSwitching / FDKaacEnc_SyncBlockSwitching
// (libAACenc/src/block_switch.cpp) — against the vendored C, compiled into this
// test binary via cgo. block_switch chooses the per-channel window sequence
// (long/start/short/stop) and window shape from the time-domain energy/attack
// measure, carrying its own persistent BLOCK_SWITCHING_CONTROL state (the
// per-window energies and the IIR delay-line) across frames. The whole state is
// compared field-for-field, bit-for-bit, across a stateful sequence of frames.
//
// This package compiles its OWN copy of the needed vendored C source
// (block_switch.cpp + genericStds.cpp for FDKmemclear/FDKmemcpy) and NEVER
// imports libraries/aac — importing it would link a second copy of the FDK
// reference and clash on static symbols (the same amalgamation-split reason the
// sibling encode-filterbank parity package documents). It MAY, and does, import
// the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the block-switch kernel is a pure INTEGER fixed-point kernel
// (INT_PCM == int16 time samples since SAMPLE_BITS == 16, FIXP_DBL == int32
// Q-format energies/states). The IIR filter, the fPow2Div2 + arithmetic-shift
// energy accumulation, and the attack comparisons are integer shifts + the int32
// fixmul int64-product>>32 kernels — bit-identical regardless of -ffp-contract /
// vectorization, with no transcendental. So this slice asserts EXACT int32
// equality unconditionally (no aac_strict gate is needed — every AAC kernel is
// fixed-point): the integer
// kernel matches in any build. The oracle is the genuine FDKaacEnc_BlockSwitching
// / FDKaacEnc_InitBlockSwitching / FDKaacEnc_SyncBlockSwitching symbols
// (oracle_kind == real_vendored).
package encblockswitch

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/enc-block-switch).
// block_switch.cpp pulls common_fix.h / psy_const.h (libAACenc/src + libFDK +
// libSYS) and genericStds.h (libSYS).
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo LDFLAGS: -lm

#include <stdint.h>
#include <stdlib.h>

// bs_snapshot must match the bridge.cpp layout exactly. MAX_NO_OF_GROUPS == 4,
// BLOCK_SWITCH_WINDOWS == 8, BLOCK_SWITCHING_IIR_LEN == 2 (psy_const.h /
// block_switch.h). Hard-coded here so cgo can size the struct without pulling C++
// headers into the Go preamble.
typedef struct {
  int32_t lastWindowSequence;
  int32_t windowShape;
  int32_t lastWindowShape;
  uint32_t nBlockSwitchWindows;
  int32_t attack;
  int32_t lastattack;
  int32_t attackIndex;
  int32_t lastAttackIndex;
  int32_t allowShortFrames;
  int32_t allowLookAhead;
  int32_t noOfGroups;
  int32_t groupLen[4];
  int32_t maxWindowNrg;
  int32_t windowNrg[2][8];
  int32_t windowNrgF[2][8];
  int32_t accWindowNrg;
  int32_t iirStates[2];
} bs_snapshot;

extern void *eparity_new(void);
extern void  eparity_free(void *st);
extern void  eparity_init(void *st, int isLowDelay, bs_snapshot *out);
extern int   eparity_block_switch(void *st, int granuleLength, int isLFE,
                                  const int16_t *pTimeSignal, bs_snapshot *out);
extern int   eparity_sync(void *left, void *right, int nChannels,
                          int commonWindow, bs_snapshot *outL, bs_snapshot *outR);
extern void  eparity_consts(int32_t *out);
extern int   eparity_sinetable_16bit(void);
*/
import "C"

import "unsafe"

// cSnapshot is the Go-side mirror of the C bs_snapshot, used to compare every
// BLOCK_SWITCHING_CONTROL state field bit-for-bit.
type cSnapshot struct {
	lastWindowSequence  int32
	windowShape         int32
	lastWindowShape     int32
	nBlockSwitchWindows uint32
	attack              int32
	lastattack          int32
	attackIndex         int32
	lastAttackIndex     int32
	allowShortFrames    int32
	allowLookAhead      int32
	noOfGroups          int32
	groupLen            [4]int32
	maxWindowNrg        int32
	windowNrg           [2][8]int32
	windowNrgF          [2][8]int32
	accWindowNrg        int32
	iirStates           [2]int32
}

// fromC converts a C bs_snapshot into the Go mirror.
func fromC(s C.bs_snapshot) cSnapshot {
	var o cSnapshot
	o.lastWindowSequence = int32(s.lastWindowSequence)
	o.windowShape = int32(s.windowShape)
	o.lastWindowShape = int32(s.lastWindowShape)
	o.nBlockSwitchWindows = uint32(s.nBlockSwitchWindows)
	o.attack = int32(s.attack)
	o.lastattack = int32(s.lastattack)
	o.attackIndex = int32(s.attackIndex)
	o.lastAttackIndex = int32(s.lastAttackIndex)
	o.allowShortFrames = int32(s.allowShortFrames)
	o.allowLookAhead = int32(s.allowLookAhead)
	o.noOfGroups = int32(s.noOfGroups)
	for i := 0; i < 4; i++ {
		o.groupLen[i] = int32(s.groupLen[i])
	}
	o.maxWindowNrg = int32(s.maxWindowNrg)
	for j := 0; j < 2; j++ {
		for i := 0; i < 8; i++ {
			o.windowNrg[j][i] = int32(s.windowNrg[j][i])
			o.windowNrgF[j][i] = int32(s.windowNrgF[j][i])
		}
	}
	o.accWindowNrg = int32(s.accWindowNrg)
	for i := 0; i < 2; i++ {
		o.iirStates[i] = int32(s.iirStates[i])
	}
	return o
}

// cState wraps the opaque persistent BLOCK_SWITCHING_CONTROL the C side owns.
type cState struct{ p unsafe.Pointer }

// cNewState allocates a zeroed BLOCK_SWITCHING_CONTROL.
func cNewState() *cState { return &cState{p: C.eparity_new()} }

// free releases the C state.
func (s *cState) free() { C.eparity_free(s.p) }

// cInit runs the genuine FDKaacEnc_InitBlockSwitching and returns the resulting
// start-state snapshot.
func (s *cState) cInit(isLowDelay int) cSnapshot {
	var snap C.bs_snapshot
	C.eparity_init(s.p, C.int(isLowDelay), &snap)
	return fromC(snap)
}

// cBlockSwitch runs the genuine FDKaacEnc_BlockSwitching and returns (rc,
// post-decision snapshot). pTimeSignal is the granuleLength-long INT_PCM frame.
func (s *cState) cBlockSwitch(granuleLength, isLFE int, pTimeSignal []int16) (int, cSnapshot) {
	var snap C.bs_snapshot
	var p *C.int16_t
	if len(pTimeSignal) > 0 {
		p = (*C.int16_t)(unsafe.Pointer(&pTimeSignal[0]))
	}
	rc := int(C.eparity_block_switch(s.p, C.int(granuleLength), C.int(isLFE), p, &snap))
	return rc, fromC(snap)
}

// cSync runs the genuine FDKaacEnc_SyncBlockSwitching over a pair (right may be
// nil for mono) and returns (rc, leftSnap, rightSnap).
func cSync(left, right *cState, nChannels, commonWindow int) (int, cSnapshot, cSnapshot) {
	var sl, sr C.bs_snapshot
	var rp unsafe.Pointer
	if right != nil {
		rp = right.p
	}
	rc := int(C.eparity_sync(left.p, rp, C.int(nChannels), C.int(commonWindow), &sl, &sr))
	return rc, fromC(sl), fromC(sr)
}

// cConsts returns the seven genuine FL2FXCONST_DBL/derived constants the Go port
// embeds: hiPassCoeff[0..1], accWindowNrgFac, oneMinusAccWindowNrgFac,
// invAttackRatio, minAttackNrg, tenConst.
func cConsts() [7]int32 {
	var out [7]C.int32_t
	C.eparity_consts(&out[0])
	var r [7]int32
	for i := 0; i < 7; i++ {
		r[i] = int32(out[i])
	}
	return r
}

// cSineTable16Bit reports whether the C build defined SINETABLE_16BIT.
func cSineTable16Bit() int { return int(C.eparity_sinetable_16bit()) }
