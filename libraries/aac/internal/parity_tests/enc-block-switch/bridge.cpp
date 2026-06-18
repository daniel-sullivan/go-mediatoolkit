// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE block-switch
 * decision kernel — libAACenc/src/block_switch.cpp. The block-switch stage is
 * the encoder's window-sequence chooser (psy_main.cpp:486 calls
 * FDKaacEnc_BlockSwitching per channel against a persistent
 * BLOCK_SWITCHING_CONTROL): it runs an IIR high-pass over the INT_PCM time
 * signal, accumulates per-window energies, detects transients ("attacks"), and
 * walks a window-sequence state machine to pick the next blockType (window
 * sequence) and window shape. FDKaacEnc_SyncBlockSwitching then folds a stereo
 * pair's decisions / grouping together (psy_main.cpp:497).
 *
 * This TU provides the extern "C" bridge the Go test calls; it links the GENUINE
 * vendored block_switch.cpp + genericStds.cpp (the sibling TUs), so the oracle is
 * the real reference, NOT a hand-twin (oracle_kind == real_vendored: the test
 * calls the genuine FDKaacEnc_* symbols, including the static
 * FDKaacEnc_CalcWindowEnergy / FDKaacEnc_GetWindowEnergy they invoke).
 *
 * It NEVER imports libraries/aac, so there is no cross-package static-symbol
 * clash (the same amalgamation-split reasoning the sibling encode-filterbank
 * oracle documents). It MAY, and the test does, import the pure-Go
 * internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32 FIXP_DBL
 * Q-format. The block-switch kernel is entirely integer: the IIR filter, the
 * fPow2Div2 + arithmetic-shift energy accumulation, and the attack comparisons
 * are int64-product/shift kernels, bit-identical regardless of -ffp-contract or
 * vectorization, with NO transcendental. So it asserts EXACT int32 equality (no
 * aac_strict FP gate; the gate command still sets aac_strict for consistency).
 */

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "common_fix.h"
#include "psy_const.h"
#include "block_switch.h"

extern "C" {

/* bs_snapshot is a flat, fixed-layout mirror of BLOCK_SWITCHING_CONTROL
 * (block_switch.h:123) the Go test reads to compare every state field
 * bit-for-bit after each kernel call. The field order/types mirror the C struct
 * exactly; all energies/states are int32 FIXP_DBL. */
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
  int32_t groupLen[MAX_NO_OF_GROUPS];
  int32_t maxWindowNrg;
  int32_t windowNrg[2][BLOCK_SWITCH_WINDOWS];
  int32_t windowNrgF[2][BLOCK_SWITCH_WINDOWS];
  int32_t accWindowNrg;
  int32_t iirStates[BLOCK_SWITCHING_IIR_LEN];
} bs_snapshot;

/* eparity_snapshot copies the live C control state into the flat mirror. */
static void eparity_snapshot(const BLOCK_SWITCHING_CONTROL *b, bs_snapshot *o) {
  o->lastWindowSequence = (int32_t)b->lastWindowSequence;
  o->windowShape = (int32_t)b->windowShape;
  o->lastWindowShape = (int32_t)b->lastWindowShape;
  o->nBlockSwitchWindows = (uint32_t)b->nBlockSwitchWindows;
  o->attack = (int32_t)b->attack;
  o->lastattack = (int32_t)b->lastattack;
  o->attackIndex = (int32_t)b->attackIndex;
  o->lastAttackIndex = (int32_t)b->lastAttackIndex;
  o->allowShortFrames = (int32_t)b->allowShortFrames;
  o->allowLookAhead = (int32_t)b->allowLookAhead;
  o->noOfGroups = (int32_t)b->noOfGroups;
  for (int i = 0; i < MAX_NO_OF_GROUPS; i++)
    o->groupLen[i] = (int32_t)b->groupLen[i];
  o->maxWindowNrg = (int32_t)b->maxWindowNrg;
  for (int j = 0; j < 2; j++)
    for (int i = 0; i < BLOCK_SWITCH_WINDOWS; i++) {
      o->windowNrg[j][i] = (int32_t)b->windowNrg[j][i];
      o->windowNrgF[j][i] = (int32_t)b->windowNrgF[j][i];
    }
  o->accWindowNrg = (int32_t)b->accWindowNrg;
  for (int i = 0; i < BLOCK_SWITCHING_IIR_LEN; i++)
    o->iirStates[i] = (int32_t)b->iirStates[i];
}

/* eparity_new allocates a zeroed BLOCK_SWITCHING_CONTROL. */
void *eparity_new(void) {
  BLOCK_SWITCHING_CONTROL *b =
      (BLOCK_SWITCHING_CONTROL *)malloc(sizeof(BLOCK_SWITCHING_CONTROL));
  memset(b, 0, sizeof(*b));
  return b;
}

void eparity_free(void *st) { free(st); }

/* eparity_init runs the genuine FDKaacEnc_InitBlockSwitching, then snapshots the
 * resulting start state. */
void eparity_init(void *st, int isLowDelay, bs_snapshot *out) {
  BLOCK_SWITCHING_CONTROL *b = (BLOCK_SWITCHING_CONTROL *)st;
  FDKaacEnc_InitBlockSwitching(b, isLowDelay);
  eparity_snapshot(b, out);
}

/* eparity_block_switch runs the genuine FDKaacEnc_BlockSwitching over a
 * granuleLength-long INT_PCM (int16) frame, then snapshots the post-decision
 * state. Returns the kernel rc. */
int eparity_block_switch(void *st, int granuleLength, int isLFE,
                         const int16_t *pTimeSignal, bs_snapshot *out) {
  BLOCK_SWITCHING_CONTROL *b = (BLOCK_SWITCHING_CONTROL *)st;
  int rc = FDKaacEnc_BlockSwitching(b, granuleLength, isLFE,
                                    (const INT_PCM *)pTimeSignal);
  eparity_snapshot(b, out);
  return rc;
}

/* eparity_sync runs the genuine FDKaacEnc_SyncBlockSwitching over a pair (right
 * may be NULL for mono), then snapshots both controls (right snapshot is left
 * untouched when right==NULL). Returns the kernel rc. */
int eparity_sync(void *left, void *right, int nChannels, int commonWindow,
                 bs_snapshot *outL, bs_snapshot *outR) {
  BLOCK_SWITCHING_CONTROL *l = (BLOCK_SWITCHING_CONTROL *)left;
  BLOCK_SWITCHING_CONTROL *r = (BLOCK_SWITCHING_CONTROL *)right;
  int rc = FDKaacEnc_SyncBlockSwitching(l, r, nChannels, commonWindow);
  eparity_snapshot(l, outL);
  if (r) eparity_snapshot(r, outR);
  return rc;
}

/* eparity_consts publishes the compile-time FL2FXCONST_DBL constants and derived
 * scalars the Go port embeds, so the test cross-checks the genuine macro output
 * against the Go literals. Layout:
 *   [0] hiPassCoeff[0]            [1] hiPassCoeff[1]
 *   [2] accWindowNrgFac          [3] oneMinusAccWindowNrgFac
 *   [4] invAttackRatio           [5] minAttackNrg
 *   [6] tenConst (10 << (DFRACT_BITS-1-4))
 * These mirror the values used in block_switch.cpp:130-145,318-319. */
void eparity_consts(int32_t *out) {
#ifndef SINETABLE_16BIT
  const FIXP_DBL hiPassCoeff[BLOCK_SWITCHING_IIR_LEN] = {
      FL2FXCONST_DBL(-0.5095), FL2FXCONST_DBL(0.7548)};
#else
  const FIXP_SGL hiPassCoeff[BLOCK_SWITCHING_IIR_LEN] = {
      FL2FXCONST_SGL(-0.5095), FL2FXCONST_SGL(0.7548)};
#endif
  out[0] = (int32_t)hiPassCoeff[0];
  out[1] = (int32_t)hiPassCoeff[1];
  out[2] = (int32_t)FL2FXCONST_DBL(0.3f); /* accWindowNrgFac: FIXP_DBL in both */
#ifndef SINETABLE_16BIT
  out[3] = (int32_t)FL2FXCONST_DBL(0.7f); /* oneMinusAccWindowNrgFac */
  out[4] = (int32_t)FL2FXCONST_DBL(0.1f); /* invAttackRatio */
#else
  out[3] = (int32_t)FL2FXCONST_SGL(0.7f); /* oneMinusAccWindowNrgFac (FIXP_SGL) */
  out[4] = (int32_t)FL2FXCONST_SGL(0.1f); /* invAttackRatio (FIXP_SGL) */
#endif
  out[5] = (int32_t)((FL2FXCONST_DBL(1e+6f * NORM_PCM_ENERGY) >>
                      BLOCK_SWITCH_ENERGY_SHIFT));
  out[6] = (int32_t)(10 << (DFRACT_BITS - 1 - 4));
}

/* eparity_sinetable_16bit reports whether SINETABLE_16BIT is defined on the
 * build platform — the Go port asserts it is OFF (so hiPassCoeff is FIXP_DBL and
 * the generic fix-mul primitives apply). Returns 1 if defined, 0 otherwise. */
int eparity_sinetable_16bit(void) {
#ifdef SINETABLE_16BIT
  return 1;
#else
  return 0;
#endif
}

} /* extern "C" */
