// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC M/S (mid/side) joint-stereo decode
 * tools (libAACdec/src/stereo.cpp). This translation unit provides the
 * extern "C" bridges the Go test calls. It uses the GENUINE vendored kernel:
 *
 *   - CJointStereo_GenerateMSOutput is `static inline` in stereo.cpp
 *     (stereo.cpp:492) — it is NOT exported, and stereo.cpp ALSO defines
 *     CJointStereo_ApplyMS / CJointStereo_ApplyIS, which reach into the giant
 *     CAacDecoderChannelInfo struct and pull the complex-prediction MDST
 *     filterbank, the cplx-pred coefficient ROM and the persistent
 *     previous-frame downmix at link time. Compiling the whole TU therefore
 *     demands the entire decoder.
 *
 *     Since the M/S kernel touches only FIXP_DBL right-shifts and integer
 *     add/sub (fAbs-free, transcendental-free, no other libfdk module), the
 *     oracle instead carries a VERBATIM copy of CJointStereo_GenerateMSOutput's
 *     body below (stereo.cpp:492, byte-for-byte the vendored source — only the
 *     symbol name is suffixed _oracle to avoid a duplicate). This is the
 *     genuine reference code compiled, without dragging the rest of the decoder
 *     — the same hand-twin-on-the-genuine-kernel pattern the
 *     huffman-spectral-decode oracle uses for CBlock_GetEscape.
 *
 *   - fparity_apply_ms is a faithful in-place twin of the non-complex-prediction
 *     ("MS stereo") branch of CJointStereo_ApplyMS (stereo.cpp:1072-1162). The
 *     surrounding function takes a CAacDecoderChannelInfo[2] that is impractical
 *     to fabricate; the inner upmix it dispatches (CJointStereo_GenerateMSOutput
 *     above, plus the left-only / right-only fill-down loops at
 *     stereo.cpp:1122-1150) is reproduced verbatim. cplx_pred_flag is the C
 *     `else` taken here unconditionally (the M/S+IS path never sets it).
 *
 * No libfdk module is linked beyond the common_fix.h fixed-point inlines, so
 * there is no cross-package static-symbol clash. This file NEVER imports
 * libraries/aac (which would link a second copy of the whole reference); it
 * stands alone alongside nativeaac.
 *
 * Inputs (L/R spectra, per-window SFB scale exponents, band offsets, the
 * window-group structure and the MsUsed flag array) are fabricated on the Go
 * side and passed flat — the exact flat per-window layout the Go port
 * (nativeaac.ApplyMS) consumes (SPEC stride == granuleLength, scale base ==
 * window*16). Both sides transform in place; the L/R spectra, the L/R SFB
 * scales and the (possibly cleared) MsUsed array are compared bit-for-bit.
 *
 * FP-parity: this is a pure INTEGER kernel — FIXP_DBL (int32) arithmetic
 * shifts plus integer add/sub. It is bit-identical regardless of
 * -ffp-contract / vectorization, so no transcendental shim and no aac_strict
 * FP split are needed. See stereo.cpp.
 */

#include <stdint.h>

#include "common_fix.h" /* FIXP_DBL, SHORT, fMin, DFRACT_BITS */

/* Verbatim copy of CJointStereo_GenerateMSOutput (libAACdec/src/stereo.cpp:492),
 * renamed _oracle. Identical body — the genuine reference, decoupled from the
 * rest of stereo.cpp so the link does not pull the whole decoder. */
static inline void CJointStereo_GenerateMSOutput_oracle(FIXP_DBL *pSpecLCurrBand,
                                                        FIXP_DBL *pSpecRCurrBand,
                                                        UINT leftScale,
                                                        UINT rightScale,
                                                        UINT nSfbBands) {
  unsigned int i;

  FIXP_DBL leftCoefficient0;
  FIXP_DBL leftCoefficient1;
  FIXP_DBL leftCoefficient2;
  FIXP_DBL leftCoefficient3;

  FIXP_DBL rightCoefficient0;
  FIXP_DBL rightCoefficient1;
  FIXP_DBL rightCoefficient2;
  FIXP_DBL rightCoefficient3;

  for (i = nSfbBands; i > 0; i -= 4) {
    leftCoefficient0 = pSpecLCurrBand[i - 4];
    leftCoefficient1 = pSpecLCurrBand[i - 3];
    leftCoefficient2 = pSpecLCurrBand[i - 2];
    leftCoefficient3 = pSpecLCurrBand[i - 1];

    rightCoefficient0 = pSpecRCurrBand[i - 4];
    rightCoefficient1 = pSpecRCurrBand[i - 3];
    rightCoefficient2 = pSpecRCurrBand[i - 2];
    rightCoefficient3 = pSpecRCurrBand[i - 1];

    /* MS output generation */
    leftCoefficient0 >>= leftScale;
    leftCoefficient1 >>= leftScale;
    leftCoefficient2 >>= leftScale;
    leftCoefficient3 >>= leftScale;

    rightCoefficient0 >>= rightScale;
    rightCoefficient1 >>= rightScale;
    rightCoefficient2 >>= rightScale;
    rightCoefficient3 >>= rightScale;

    pSpecLCurrBand[i - 4] = leftCoefficient0 + rightCoefficient0;
    pSpecLCurrBand[i - 3] = leftCoefficient1 + rightCoefficient1;
    pSpecLCurrBand[i - 2] = leftCoefficient2 + rightCoefficient2;
    pSpecLCurrBand[i - 1] = leftCoefficient3 + rightCoefficient3;

    pSpecRCurrBand[i - 4] = leftCoefficient0 - rightCoefficient0;
    pSpecRCurrBand[i - 3] = leftCoefficient1 - rightCoefficient1;
    pSpecRCurrBand[i - 2] = leftCoefficient2 - rightCoefficient2;
    pSpecRCurrBand[i - 1] = leftCoefficient3 - rightCoefficient3;
  }
}

extern "C" {

/* fparity_apply_ms is the byte-for-byte non-cplx-pred ("MS stereo") branch of
 * CJointStereo_ApplyMS (stereo.cpp:1072-1162), flattened off the
 * CAacDecoderChannelInfo struct onto the same flat arrays nativeaac.ApplyMS
 * consumes:
 *   - spectrumL/spectrumR : all windows back to back; SPEC(ptr,w,gl)==ptr+w*gl.
 *   - sfbLeftScale/sfbRightScale : 16 scale exponents per window (window*16).
 *   - msUsed[band] : one byte per SFB, bit g set => M/S used in group g.
 *   - sfbOffsets[band] : first spectral line of band; len == max bands + 1.
 *   - windowGroupLength[group] : windows in group g.
 * msMaskPresent==2 clears msUsed afterward (stereo.cpp:1157). The verbatim
 * upmix kernel above is what each M/S band dispatches. */
void fparity_apply_ms(uint8_t msMaskPresent, uint8_t *msUsed,
                      int32_t *spectrumL, int32_t *spectrumR,
                      int16_t *sfbLeftScale, int16_t *sfbRightScale,
                      const int16_t *sfbOffsets,
                      const uint8_t *windowGroupLength, int windowGroups,
                      int max_sfb_ste_outside, int scaleFactorBandsTransmittedL,
                      int scaleFactorBandsTransmittedR, int granuleLength) {
  int window, group, band;
  unsigned char groupMask;

  int min_sfb_ste =
      fMin((INT)scaleFactorBandsTransmittedL, (INT)scaleFactorBandsTransmittedR);
  int scaleFactorBandsTransmitted = min_sfb_ste;

  /* MS stereo */
  for (window = 0, group = 0; group < windowGroups; group++) {
    groupMask = 1 << group;

    for (int groupwin = 0; groupwin < windowGroupLength[group];
         groupwin++, window++) {
      FIXP_DBL *leftSpectrum, *rightSpectrum;
      SHORT *leftScale = &sfbLeftScale[window * 16];
      SHORT *rightScale = &sfbRightScale[window * 16];

      leftSpectrum = (FIXP_DBL *)spectrumL + window * granuleLength;
      rightSpectrum = (FIXP_DBL *)spectrumR + window * granuleLength;

      for (band = 0; band < max_sfb_ste_outside; band++) {
        if (msUsed[band] & groupMask) {
          int lScale = leftScale[band];
          int rScale = rightScale[band];
          int commonScale = lScale > rScale ? lScale : rScale;
          unsigned int offsetCurrBand, offsetNextBand;

          commonScale++;
          leftScale[band] = commonScale;
          rightScale[band] = commonScale;

          lScale = fMin((INT)(DFRACT_BITS - 1), (INT)(commonScale - lScale));
          rScale = fMin((INT)(DFRACT_BITS - 1), (INT)(commonScale - rScale));

          offsetCurrBand = sfbOffsets[band];
          offsetNextBand = sfbOffsets[band + 1];

          CJointStereo_GenerateMSOutput_oracle(&(leftSpectrum[offsetCurrBand]),
                                               &(rightSpectrum[offsetCurrBand]),
                                               lScale, rScale,
                                               offsetNextBand - offsetCurrBand);
        }
      }
      if (scaleFactorBandsTransmittedL > scaleFactorBandsTransmitted) {
        for (; band < scaleFactorBandsTransmittedL; band++) {
          if (msUsed[band] & groupMask) {
            rightScale[band] = leftScale[band];

            for (int index = sfbOffsets[band]; index < sfbOffsets[band + 1];
                 index++) {
              FIXP_DBL leftCoefficient = leftSpectrum[index];
              rightSpectrum[index] = leftCoefficient;
            }
          }
        }
      } else if (scaleFactorBandsTransmittedR > scaleFactorBandsTransmitted) {
        for (; band < scaleFactorBandsTransmittedR; band++) {
          if (msUsed[band] & groupMask) {
            leftScale[band] = rightScale[band];

            for (int index = sfbOffsets[band]; index < sfbOffsets[band + 1];
                 index++) {
              FIXP_DBL rightCoefficient = rightSpectrum[index];

              leftSpectrum[index] = rightCoefficient;
              rightSpectrum[index] = -rightCoefficient;
            }
          }
        }
      }
    }
  }

  /* Reset MsUsed flags if no explicit signalling was transmitted
   * (stereo.cpp:1157). JointStereoMaximumBands == 64 (stereo.h:130). */
  if (msMaskPresent == 2) {
    for (int i = 0; i < 64; i++) msUsed[i] = 0;
  }
}

} /* extern "C" */
