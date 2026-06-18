// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC inverse-quantization kernels
 * (libAACdec/src/block.h + block.cpp). This translation unit provides the
 * extern "C" bridges the Go test calls. It uses the GENUINE vendored code:
 *
 *   - EvaluatePower43 (block.h:247) and GetScaleFromValue (block.h:283) are
 *     FDK_INLINE functions in block.h, pulled in by including the header here —
 *     the genuine vendored kernels.
 *   - InverseQuantizeBand (block.cpp:436) and maxabs_D (block.cpp:471) are
 *     `static inline` in block.cpp. block.cpp ALSO defines the rest of the
 *     decoder (CBlock_ReadSpectralData, CBlock_FrequencyToTime, …) which
 *     reference HCR / TNS / PNS / arith-coder / iMDCT symbols from other libfdk
 *     modules; compiling the whole TU therefore demands the entire decoder at
 *     link time. Since these two functions are tiny and self-contained (they
 *     touch only fAbs / fMax / CntLeadingZeros / fMultDiv2 / scaleValueInPlace
 *     and the inverse-quant ROM, all already linked), the oracle instead
 *     carries a VERBATIM copy of each body below (block.cpp:436 / block.cpp:471,
 *     byte-for-byte the vendored source — only the symbol name is suffixed
 *     _oracle to avoid a duplicate). This is the genuine reference code
 *     compiled, without dragging the rest of the decoder.
 *   - InverseQuantTable + MantissaTable + ExponentTable (aac_rom.cpp:109/205/219)
 *     come from aac_rom.cpp, compiled as a sibling TU (aac_rom_cgo.cpp).
 *
 * No other libfdk module is linked, so there is no cross-package static-symbol
 * clash. This file NEVER imports libraries/aac (which would link a second copy
 * of the whole reference); it stands alone alongside nativeaac.
 *
 * FP-parity: this is a pure integer / fixed-point kernel; the -ffp-contract /
 * vectorize flags from the mise env are irrelevant. See block.cpp /
 * aac_rom.cpp.
 */

#include <stdint.h>

#include "block.h"
#include "aac_rom.h"

/* Verbatim copy of InverseQuantizeBand (libAACdec/src/block.cpp:436), renamed
 * _oracle. Identical body — the genuine reference, decoupled from the rest of
 * block.cpp so the link does not pull the whole decoder. */
static inline void InverseQuantizeBand_oracle(
    FIXP_DBL *RESTRICT spectrum, const FIXP_DBL *RESTRICT InverseQuantTabler,
    const FIXP_DBL *RESTRICT MantissaTabler,
    const SCHAR *RESTRICT ExponentTabler, INT noLines, INT scale) {
  scale = scale + 1; /* +1 to compensate fMultDiv2 shift-right in loop */

  FIXP_DBL *RESTRICT ptr = spectrum;
  FIXP_DBL signedValue;

  for (INT i = noLines; i--;) {
    if ((signedValue = *ptr++) != FL2FXCONST_DBL(0)) {
      FIXP_DBL value = fAbs(signedValue);
      UINT freeBits = CntLeadingZeros(value);
      UINT exponent = 32 - freeBits;

      UINT x = (UINT)(LONG)value << (INT)freeBits;
      x <<= 1; /* shift out sign bit to avoid masking later on */
      UINT tableIndex = x >> 24;
      x = (x >> 20) & 0x0F;

      UINT r0 = (UINT)(LONG)InverseQuantTabler[tableIndex + 0];
      UINT r1 = (UINT)(LONG)InverseQuantTabler[tableIndex + 1];
      UINT temp = (r1 - r0) * x + (r0 << 4);

      value = fMultDiv2((FIXP_DBL)temp, MantissaTabler[exponent]);

      /* + 1 compensates fMultDiv2() */
      scaleValueInPlace(&value, scale + ExponentTabler[exponent]);

      signedValue = (signedValue < (FIXP_DBL)0) ? -value : value;
      ptr[-1] = signedValue;
    }
  }
}

/* Verbatim copy of maxabs_D (libAACdec/src/block.cpp:471), renamed _oracle. */
static inline FIXP_DBL maxabs_D_oracle(const FIXP_DBL *pSpectralCoefficient,
                                       const int noLines) {
  /* Find max spectral line value of the current sfb */
  FIXP_DBL locMax = (FIXP_DBL)0;
  int i;

  DWORD_ALIGNED(pSpectralCoefficient);

  for (i = noLines; i-- > 0;) {
    /* Expensive memory access */
    locMax = fMax(fixp_abs(pSpectralCoefficient[i]), locMax);
  }

  return locMax;
}

extern "C" {

int fparity_evaluate_power43(int32_t *value, unsigned lsb) {
  /* EvaluatePower43 is the genuine FDK_INLINE from block.h. */
  return EvaluatePower43((FIXP_DBL *)value, (UINT)lsb);
}

int fparity_get_scale_from_value(int32_t value, unsigned lsb) {
  /* GetScaleFromValue is the genuine FDK_INLINE from block.h. */
  return GetScaleFromValue((FIXP_DBL)value, (unsigned int)lsb);
}

int32_t fparity_maxabs_d(const int32_t *spectrum, int noLines) {
  return (int32_t)maxabs_D_oracle((const FIXP_DBL *)spectrum, noLines);
}

void fparity_inverse_quantize_band(int32_t *spectrum, int lsb, int noLines,
                                   int scale) {
  InverseQuantizeBand_oracle((FIXP_DBL *)spectrum, InverseQuantTable,
                             MantissaTable[lsb], ExponentTable[lsb],
                             (INT)noLines, (INT)scale);
}

} /* extern "C" */
