// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC decoder TNS filter
 * (libFDK/src/FDK_lpc.cpp CLpc_SynthesisLattice, the FIXP_DBL coefficient
 * overload that libAACdec/src/aacdec_tns.cpp CTns_Apply dispatches). This
 * translation unit provides the extern "C" bridge the Go test calls.
 *
 * It carries a VERBATIM copy of CLpc_SynthesisLattice's FIXP_DBL body
 * (FDK_lpc.cpp:168-209, byte-for-byte the vendored source — only the symbol
 * name is suffixed _oracle to avoid a duplicate). Compiling FDK_lpc.cpp whole
 * would also pull CLpc_AutoToParcor / CLpc_ParcorToLpc, which reference
 * schur_div / fDivNormSigned defined in fixpoint_math.cpp — a needless link
 * cascade. The lattice is self-contained: it depends only on the fixmath
 * inlines (fMultDiv2 / fMultSubDiv2 / fMultAddDiv2 in common_fix.h + fixmadd.h,
 * scaleValue / SATURATE_LEFT_SHIFT_ALT in scale.h), which ARE the genuine
 * vendored headers included here. No other libfdk TU is linked, so there is no
 * cross-package static-symbol clash. This file NEVER imports libraries/aac.
 *
 * FP-parity: TNS is implemented in fixed point (FIXP_DBL == int32, Q1.31); the
 * lattice MAC chain is the integer fMultDiv2 (int64 product >> 32) plus integer
 * saturating shifts. It is bit-identical regardless of -ffp-contract /
 * vectorization; the mise scalar flags are irrelevant to it.
 */

#include <stdint.h>

#include "common_fix.h"
#include "scale.h"

/* Verbatim copy of the FIXP_DBL overload of CLpc_SynthesisLattice
 * (libFDK/src/FDK_lpc.cpp:168-209), renamed _oracle. Identical body — the
 * genuine reference, decoupled from the rest of FDK_lpc.cpp so the link does
 * not pull schur_div / fDivNormSigned. */
static void CLpc_SynthesisLattice_oracle(FIXP_DBL *signal, const int signal_size,
                                         const int signal_e,
                                         const int signal_e_out, const int inc,
                                         const FIXP_DBL *coeff, const int order,
                                         FIXP_DBL *state) {
  int i, j;
  FIXP_DBL *pSignal;

  FDK_ASSERT(order <= LPC_MAX_ORDER);
  FDK_ASSERT(order > 0);

  if (inc == -1)
    pSignal = &signal[signal_size - 1];
  else
    pSignal = &signal[0];

  FDK_ASSERT(signal_size > 0);
  for (i = signal_size; i != 0; i--) {
    FIXP_DBL *pState = state + order - 1;
    const FIXP_DBL *pCoeff = coeff + order - 1;
    FIXP_DBL tmp, accu;

    accu =
        fMultSubDiv2(scaleValue(*pSignal, signal_e - 1), *pCoeff--, *pState--);
    tmp = SATURATE_LEFT_SHIFT_ALT(accu, 1, DFRACT_BITS);

    for (j = order - 1; j != 0; j--) {
      accu = fMultSubDiv2(tmp >> 1, pCoeff[0], pState[0]);
      tmp = SATURATE_LEFT_SHIFT_ALT(accu, 1, DFRACT_BITS);

      accu = fMultAddDiv2(pState[0] >> 1, *pCoeff--, tmp);
      pState[1] = SATURATE_LEFT_SHIFT_ALT(accu, 1, DFRACT_BITS);

      pState--;
    }

    *pSignal = scaleValue(tmp, -signal_e_out);

    /* exponent of state[] is 0 */
    pState[1] = tmp;
    pSignal += inc;
  }
}

extern "C" {

void fparity_synthesis_lattice_dbl(int32_t *signal, int signalSize, int signalE,
                                   int signalEOut, int inc,
                                   const int32_t *coeff, int order,
                                   int32_t *state) {
  CLpc_SynthesisLattice_oracle((FIXP_DBL *)signal, signalSize, signalE,
                               signalEOut, inc, (const FIXP_DBL *)coeff, order,
                               (FIXP_DBL *)state);
}

} /* extern "C" */
