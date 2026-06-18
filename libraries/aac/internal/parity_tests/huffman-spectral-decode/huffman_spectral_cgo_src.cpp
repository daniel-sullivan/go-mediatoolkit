// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC plain spectral Huffman decoder
 * (libAACdec/src/block.h + block.cpp). This translation unit provides the
 * extern "C" bridges the Go test calls. It uses the GENUINE vendored code:
 *
 *   - CBlock_DecodeHuffmanWord / CBlock_DecodeHuffmanWordCB are `inline`
 *     functions in block.h, pulled in by including the header here — the
 *     genuine vendored tree walkers.
 *   - CBlock_GetEscape is defined in block.cpp. block.cpp ALSO defines the rest
 *     of the decoder (CBlock_ReadSpectralData, CBlock_FrequencyToTime, …) which
 *     reference HCR / TNS / PNS / arith-coder / iMDCT symbols from other libfdk
 *     modules; compiling the whole TU therefore demands the entire decoder at
 *     link time. Since CBlock_GetEscape is a tiny self-contained function (it
 *     touches only fAbs + the FDK bitstream reads), the oracle instead carries
 *     a VERBATIM copy of its body below (block.cpp:138, byte-for-byte the
 *     vendored source — only the symbol name is suffixed _oracle to avoid a
 *     duplicate). This is the genuine reference code compiled, without dragging
 *     the rest of the decoder. It is cross-checked against the inline walkers,
 *     which ARE linked whole.
 *   - AACcodeBookDescriptionTable + the HuffmanCodeBook_* ROM trees come from
 *     aac_rom.cpp, compiled as a sibling TU (aac_rom_cgo.cpp).
 *   - The FDK bit buffer back-end (FDK_get32, FDK_byteAlign, …) comes from
 *     FDK_bitbuffer.cpp + genericStds.cpp sibling TUs.
 *
 * No other libfdk module is linked, so there is no cross-package static-symbol
 * clash. This file NEVER imports libraries/aac (which would link a second copy
 * of the whole reference); it stands alone alongside nativeaac.
 *
 * Inputs are fabricated as a raw random byte buffer on the Go side and read
 * MSB-first via FDKinitBitStream(..., BS_READER) — the exact reader the Go
 * port (nativeaac.initBitStream) mirrors — so both sides consume identical
 * bits. fparity_read_spectral_data is a faithful in-place twin of the
 * block.cpp:620 outer loop: the surrounding CBlock_ReadSpectralData takes a
 * giant CAacDecoderChannelInfo that is impractical to fabricate, but every
 * codeword/sign/escape it dispatches is decoded by the genuine vendored
 * CBlock_DecodeHuffmanWordCB / FDKreadBit / CBlock_GetEscape called here.
 *
 * FP-parity: this is a pure integer kernel; the -ffp-contract / vectorize
 * flags from the mise env are irrelevant. See block.cpp / aac_rom.cpp.
 */

#include <stdint.h>

#include "block.h"
#include "aac_rom.h"
#include "FDK_bitstream.h"

/* Verbatim copy of CBlock_GetEscape (libAACdec/src/block.cpp:138), renamed
 * _oracle. Identical body — the genuine reference, decoupled from the rest of
 * block.cpp so the link does not pull the whole decoder. */
static LONG CBlock_GetEscape_oracle(HANDLE_FDK_BITSTREAM bs, const LONG q) {
  if (fAbs(q) != 16) return (q);

  LONG i, off;
  for (i = 4; i < 13; i++) {
    if (FDKreadBit(bs) == 0) break;
  }

  if (i == 13) return (MAX_QUANTIZED_VALUE + 1);

  off = FDKreadBits(bs, i);
  i = off + (1 << i);

  if (q < 0) i = -i;

  return i;
}

extern "C" {

/* The two huffman walkers are inline in block.h (genuine vendored code).
 * CBlock_GetEscape_oracle above is the verbatim block.cpp:138 body. */

int fparity_decode_huffman_word_cb(const uint8_t *buf, int bufSize, int cb,
                                   unsigned *bitNdxInOut) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, (UCHAR *)buf, (UINT)bufSize, (UINT)(bufSize << 3),
                   BS_READER);
  /* advance to the requested starting bit position */
  if (*bitNdxInOut) FDKpushFor(&bs, (INT)*bitNdxInOut);
  const CodeBookDescription *hcb = &AACcodeBookDescriptionTable[cb];
  int v = CBlock_DecodeHuffmanWordCB(&bs, hcb->CodeBook);
  *bitNdxInOut =
      (unsigned)((UINT)(bufSize << 3) - FDKgetValidBits(&bs));
  return v;
}

int fparity_decode_huffman_word(const uint8_t *buf, int bufSize, int cb,
                                unsigned *bitNdxInOut) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, (UCHAR *)buf, (UINT)bufSize, (UINT)(bufSize << 3),
                   BS_READER);
  if (*bitNdxInOut) FDKpushFor(&bs, (INT)*bitNdxInOut);
  const CodeBookDescription *hcb = &AACcodeBookDescriptionTable[cb];
  int v = CBlock_DecodeHuffmanWord(&bs, hcb);
  *bitNdxInOut =
      (unsigned)((UINT)(bufSize << 3) - FDKgetValidBits(&bs));
  return v;
}

int fparity_get_escape(const uint8_t *buf, int bufSize, int q,
                       unsigned *bitNdxInOut) {
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, (UCHAR *)buf, (UINT)bufSize, (UINT)(bufSize << 3),
                   BS_READER);
  if (*bitNdxInOut) FDKpushFor(&bs, (INT)*bitNdxInOut);
  int v = (int)CBlock_GetEscape_oracle(&bs, (LONG)q);
  *bitNdxInOut =
      (unsigned)((UINT)(bufSize << 3) - FDKgetValidBits(&bs));
  return v;
}

/* Faithful in-place twin of the non-HCR plain-Huffman branch of
 * CBlock_ReadSpectralData (block.cpp:620). The inner decode (huffman word,
 * sign bit, escape) is the GENUINE vendored code. The mdctSpectrum layout is
 * window-major, stride granuleLength; the caller sizes `spectrum`. */
void fparity_read_spectral_data(const uint8_t *buf, int bufSize,
                                uint8_t *codeBook, const int16_t *bandOffsets,
                                int windowGroups, const int *windowGroupLen,
                                int granuleLength, int transmittedBands,
                                int32_t *spectrum, int spectrumLen) {
  enum {
    ZERO_HCB = 0,
    ESCBOOK = 11,
    NOISE_HCB = 13,
    INTENSITY_HCB2 = 14,
    INTENSITY_HCB = 15
  };

  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, (UCHAR *)buf, (UINT)bufSize, (UINT)(bufSize << 3),
                   BS_READER);

  for (int k = 0; k < spectrumLen; k++) spectrum[k] = 0;

  int groupoffset = 0;
  int max_group = windowGroups;

  for (int group = 0; group < max_group; group++) {
    int max_groupwin = windowGroupLen[group];
    int bnds = group * 16;

    int bandOffset1 = bandOffsets[0];
    for (int band = 0; band < transmittedBands; band++, bnds++) {
      unsigned char currentCB = codeBook[bnds];
      int bandOffset0 = bandOffset1;
      bandOffset1 = bandOffsets[band + 1];

      /* patch VCB11 input codebooks (16..31) -> 11 in place, mutating the
       * codebook array exactly as block.cpp:664 (pCodeBook[bnds]=currentCB=11);
       * the Go port mirrors this write-back, so the test compares the patched
       * codebook arrays too. */
      if ((currentCB >= 16) && (currentCB <= 31)) {
        codeBook[bnds] = currentCB = 11;
      }
      if (((currentCB != ZERO_HCB) && (currentCB != NOISE_HCB) &&
           (currentCB != INTENSITY_HCB) && (currentCB != INTENSITY_HCB2))) {
        const CodeBookDescription *hcb =
            &AACcodeBookDescriptionTable[currentCB];
        int step = hcb->Dimension;
        int offset = hcb->Offset;
        int bits = hcb->numBits;
        int mask = (1 << bits) - 1;
        const USHORT(*CodeBook)[HuffmanEntries] = hcb->CodeBook;

        int32_t *mdctSpectrum = &spectrum[groupoffset * granuleLength];

        if (offset == 0) {
          for (int groupwin = 0; groupwin < max_groupwin; groupwin++) {
            for (int index = bandOffset0; index < bandOffset1; index += step) {
              int idx = CBlock_DecodeHuffmanWordCB(&bs, CodeBook);
              for (int i = 0; i < step; i++, idx >>= bits) {
                int32_t tmp = (int32_t)((idx & mask) - offset);
                if (tmp != 0) tmp = (FDKreadBit(&bs)) ? -tmp : tmp;
                mdctSpectrum[index + i] = tmp;
              }
              if (currentCB == ESCBOOK) {
                for (int j = 0; j < 2; j++)
                  mdctSpectrum[index + j] = (int32_t)CBlock_GetEscape_oracle(&bs, (LONG)mdctSpectrum[index + j]);
              }
            }
            mdctSpectrum += granuleLength;
          }
        } else {
          for (int groupwin = 0; groupwin < max_groupwin; groupwin++) {
            for (int index = bandOffset0; index < bandOffset1; index += step) {
              int idx = CBlock_DecodeHuffmanWordCB(&bs, CodeBook);
              for (int i = 0; i < step; i++, idx >>= bits) {
                mdctSpectrum[index + i] = (int32_t)((idx & mask) - offset);
              }
              if (currentCB == ESCBOOK) {
                for (int j = 0; j < 2; j++)
                  mdctSpectrum[index + j] = (int32_t)CBlock_GetEscape_oracle(&bs, (LONG)mdctSpectrum[index + j]);
              }
            }
            mdctSpectrum += granuleLength;
          }
        }
      }
    }
    groupoffset += max_groupwin;
  }
}

} /* extern "C" */
