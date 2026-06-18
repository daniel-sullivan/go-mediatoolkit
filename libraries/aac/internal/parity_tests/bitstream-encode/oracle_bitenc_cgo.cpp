// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC bitstream-encode area: the FDK bit
 * WRITER (FDKwriteBits + its 32-bit cache, FDKsyncCache, FDKbyteAlign, and the
 * underlying FDK_put ring store) and the encode-time spectral / scalefactor
 * Huffman emitters (FDKaacEnc_codeValues, FDKaacEnc_codeScalefactorDelta).
 *
 * The Huffman emitters are the genuine vendored functions — this bridge calls
 * them directly (compiled in the sibling TU tu_bit_cnt_cgo.cpp, which #includes
 * the real bit_cnt.cpp). The bit WRITER lives entirely in inline header
 * functions in FDK_bitstream.h, so it is exercised by driving FDKinitBitStream
 * / FDKwriteBits / FDKbyteAlign here; FDK_put resolves in tu_fdk_bitbuffer.
 * The Go side (nativeaac.WriteBitsParity / CodeValuesParity /
 * CodeScalefactorDeltaParity, ported 1:1) is asserted bit-for-bit against this.
 *
 * Why this slice is a clean oracle: codeValues / codeScalefactorDelta are
 * non-static, self-contained functions over a SHORT spectral array / an INT
 * delta plus a HANDLE_FDK_BITSTREAM; their only external dependencies are the
 * FDKaacEnc_huff_* tables (compiled as the aacEnc_rom sibling TU) and the
 * inline FDK bit writer. No other libfdk module is linked, so there is no
 * cross-package static-symbol clash. This file NEVER imports libraries/aac
 * (which would link a second copy of the whole reference); it stands alone
 * alongside nativeaac.
 *
 * FP-parity: the whole bitstream-encode area is a pure INTEGER kernel — bit
 * shifts, masks and table lookups on SHORT/INT operands and a UCHAR ring
 * store. It is therefore bit-identical regardless of -ffp-contract /
 * vectorization, so no transcendental shim is needed here. The strict-gate on
 * the Go assertions is the area convention (the aac_strict parity discipline),
 * not a numerical necessity.
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS (see
 * cgo.go). The scalar FP flags (-ffp-contract=off -fno-vectorize
 * -fno-slp-vectorize -fno-unroll-loops) come from the mise task env
 * (CGO_CFLAGS), not here — but they are irrelevant to this integer kernel.
 */

#include "FDK_bitstream.h"
#include "bit_cnt.h"

#include <string.h>

/* fparity_write_bits replays n (value, width) writes through FDKwriteBits into
 * bufBytes bytes of out (a power-of-two buffer the caller pre-zeroes), then
 * byte-aligns to anchor 0 and flushes. It stores the pre-alignment valid-bit
 * count into *validBits and returns the byte length the produced bits occupy.
 * This pins the raw bit writer + FDK_put ring store independently of the
 * Huffman tables. */
extern "C" int fparity_write_bits(const unsigned int *values,
                                  const unsigned int *widths, int n,
                                  unsigned char *out, int bufBytes,
                                  int *validBits) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  for (int i = 0; i < n; i++) {
    FDKwriteBits(&bs, (UINT)values[i], (UINT)widths[i]);
  }
  *validBits = (int)FDKgetValidBits(&bs);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

/* fparity_code_values Huffman-encodes width SHORT coefficients of one section
 * with codeBook into bufBytes bytes of out (pre-zeroed), byte-aligns to anchor
 * 0 and flushes. Stores the pre-alignment valid-bit count into *validBits and
 * returns the produced byte length. */
extern "C" int fparity_code_values(const short *values, int width, int codeBook,
                                   unsigned char *out, int bufBytes,
                                   int *validBits) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  /* FDKaacEnc_codeValues takes a non-const SHORT* (it does *values++ in the
   * sign-extracting books); copy into a scratch so the caller's slice is not
   * mutated and the pointer arithmetic has room. */
  short scratch[2048];
  int w = width;
  if (w > (int)(sizeof(scratch) / sizeof(scratch[0]))) {
    w = (int)(sizeof(scratch) / sizeof(scratch[0]));
  }
  memcpy(scratch, values, (size_t)w * sizeof(short));
  FDKaacEnc_codeValues(scratch, w, codeBook, &bs);
  *validBits = (int)FDKgetValidBits(&bs);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}

/* fparity_code_scalefactor_delta DPCM-encodes a single scalefactor delta into
 * bufBytes bytes of out (pre-zeroed), byte-aligns to anchor 0 and flushes.
 * Stores the pre-alignment valid-bit count into *validBits and the C
 * range-error return (1 when |delta| > CODE_BOOK_SCF_LAV, else 0) into
 * *rangeErr; returns the produced byte length. */
extern "C" int fparity_code_scalefactor_delta(int delta, unsigned char *out,
                                              int bufBytes, int *validBits,
                                              int *rangeErr) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, out, (UINT)bufBytes, 0, BS_WRITER);
  *rangeErr = FDKaacEnc_codeScalefactorDelta(delta, &bs);
  *validBits = (int)FDKgetValidBits(&bs);
  FDKbyteAlign(&bs, 0);
  return ((int)FDKgetValidBits(&bs) + 7) >> 3;
}
