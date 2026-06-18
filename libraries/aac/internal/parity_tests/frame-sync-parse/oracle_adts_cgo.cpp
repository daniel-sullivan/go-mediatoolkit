// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC ADTS frame-sync-parse slice
 * (libMpegTPDec/src/tpdec_adts.cpp + the syncword search in tpdec_lib.cpp).
 * This translation unit compiles the vendored tpdec_adts.cpp verbatim (via
 * #include of the real source, the same per-TU technique the libraries/aac
 * fdk_tu_*.cpp wrappers use) and exposes thin extern "C" bridges that the Go
 * test calls to obtain the reference parse. The Go side
 * (nativeaac.findSyncword / decodeHeader / getRawDataBlockLength, ported 1:1)
 * is asserted field-for-field against this.
 *
 * Linkage: adtsRead_DecodeHeader pulls in AudioSpecificConfig_Init /
 * CProgramConfig_* (tpdec_asc.cpp), the FDK CRC engine (FDK_crc.cpp), the FDK
 * bit buffer (FDK_bitbuffer.cpp) and FDKmemcpy (genericStds.cpp). Each of those
 * is compiled as its own sibling TU (oracle_tu_*.cpp) so file-static helpers
 * never collide and no other libfdk module is linked — there is no
 * cross-package static-symbol clash, and this oracle stands alone alongside
 * nativeaac (it NEVER imports libraries/aac).
 *
 * FP-parity: the ADTS parse is a pure INTEGER kernel — only bit reads and
 * integer arithmetic. It is therefore bit-identical regardless of
 * -ffp-contract / vectorization, so no transcendental shim is needed here. The
 * scalar FP flags (-ffp-contract=off -fno-vectorize -fno-slp-vectorize
 * -fno-unroll-loops) come from the mise task env (CGO_CFLAGS); they are
 * irrelevant to this kernel.
 */

#include "libfdk/libMpegTPDec/src/tpdec_adts.cpp"

#include "FDK_bitstream.h"

#include "oracle_bridge.h"

/* How many bits to advance for synchronization search. tpdec_lib.cpp:1063. */
#define FPARITY_TPDEC_SYNCSKIP 8

/* The FDK bit buffer is a power-of-2 circular buffer: FDK_InitBitBuffer asserts
 * bufSize is 2^n and uses (bufBits-1) masks for wraparound. The real transport
 * decoder always inits it at bufSize = 8192*4 = 32768 bytes (tpdec_lib.cpp:252)
 * and tracks the real content via validBits. We mirror that exactly: the
 * fabricated bytes are copied into a 32768-byte buffer and validBits is set to
 * the real bit count, so the valid region never wraps and the parse sees the
 * same bytes the Go adtsBitReader does. Feeding FDKinitBitStream a
 * non-power-of-2 bufSize (e.g. the raw fabricated length) would put the C
 * reference into the assert-disabled UB path and is NOT a valid oracle. */
#define FPARITY_BSBUF_BYTES (8192 * 4)

extern "C" {

/* fparity_find_syncword is a hand-twin of the TT_MP4_ADTS /
 * numberOfRawDataBlocks==0 / !TPDEC_SYNCOK branch of the file-static
 * synchronization() loop in tpdec_lib.cpp:1118, lifted verbatim onto the public
 * FDK bitstream API because the original cannot be called in isolation. It
 * mirrors nativeaac.findSyncword. Returns the raw TRANSPORTDEC_ERROR; on OK the
 * 12 syncword bits have been consumed and *bitPosOut holds the post-syncword
 * bit position. */
int fparity_find_syncword(const unsigned char *buf, int len, int *bitPosOut) {
  static UCHAR bsbuf[FPARITY_BSBUF_BYTES];
  FDKmemclear(bsbuf, sizeof(bsbuf));
  if (len > FPARITY_BSBUF_BYTES) {
    len = FPARITY_BSBUF_BYTES;
  }
  if (len > 0) {
    FDKmemcpy(bsbuf, buf, len);
  }

  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, bsbuf, FPARITY_BSBUF_BYTES, (UINT)len * 8, BS_READER);

  const UINT syncWord = ADTS_SYNCWORD;
  const INT syncLength = ADTS_SYNCLENGTH;
  const UINT syncMask = (1 << syncLength) - 1;

  INT bitsAvail = (INT)FDKgetValidBits(&bs);

  if ((bitsAvail - syncLength) < FPARITY_TPDEC_SYNCSKIP) {
    return (int)TRANSPORTDEC_NOT_ENOUGH_BITS;
  }

  UINT synch = FDKreadBits(&bs, syncLength);
  for (; (bitsAvail - syncLength) >= FPARITY_TPDEC_SYNCSKIP;
       bitsAvail -= FPARITY_TPDEC_SYNCSKIP) {
    if (synch == syncWord) {
      break;
    }
    synch = ((synch << FPARITY_TPDEC_SYNCSKIP) & syncMask) |
            FDKreadBits(&bs, FPARITY_TPDEC_SYNCSKIP);
  }
  if (synch != syncWord) {
    return (int)TRANSPORTDEC_SYNC_ERROR;
  }
  if (bitPosOut != 0) {
    /* Bits consumed so far = total valid bits minus what remains. */
    *bitPosOut = (INT)((UINT)len * 8 - FDKgetValidBits(&bs));
  }
  return (int)TRANSPORTDEC_OK;
}

void fparity_adts_decode_header(const unsigned char *buf, int len,
                                int decoderCanDoMpeg4,
                                int bufferFullnessStartFlag,
                                int ignoreBufferFullness,
                                fparity_adts_result *out) {
  FDKmemclear(out, sizeof(*out));

  static UCHAR bsbuf[FPARITY_BSBUF_BYTES];
  FDKmemclear(bsbuf, sizeof(bsbuf));
  if (len > FPARITY_BSBUF_BYTES) {
    len = FPARITY_BSBUF_BYTES;
  }
  if (len > 0) {
    FDKmemcpy(bsbuf, buf, len);
  }

  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, bsbuf, FPARITY_BSBUF_BYTES, (UINT)len * 8, BS_READER);

  /* Mirror the test flow: consume the syncword first (as findSyncword does),
   * so adtsRead_DecodeHeader sees the post-syncword position and restores the
   * 12 bits via valBits = FDKgetValidBits(hBs) + ADTS_SYNCLENGTH. */
  FDKreadBits(&bs, ADTS_SYNCLENGTH);

  STRUCT_ADTS adts;
  FDKmemclear(&adts, sizeof(adts));
  adtsRead_CrcInit(&adts);
  adts.decoderCanDoMpeg4 = (UCHAR)decoderCanDoMpeg4;
  adts.BufferFullnesStartFlag = (UCHAR)bufferFullnessStartFlag;

  CSAudioSpecificConfig asc;
  AudioSpecificConfig_Init(&asc);

  TRANSPORTDEC_ERROR err =
      adtsRead_DecodeHeader(&adts, &asc, &bs, (INT)ignoreBufferFullness);

  out->err = (int)err;
  out->mpeg_id = adts.bs.mpeg_id;
  out->layer = adts.bs.layer;
  out->protection_absent = adts.bs.protection_absent;
  out->profile = adts.bs.profile;
  out->sample_freq_index = adts.bs.sample_freq_index;
  out->private_bit = adts.bs.private_bit;
  out->channel_config = adts.bs.channel_config;
  out->original = adts.bs.original;
  out->home = adts.bs.home;
  out->copyright_id = adts.bs.copyright_id;
  out->copyright_start = adts.bs.copyright_start;
  out->frame_length = adts.bs.frame_length;
  out->adts_fullness = adts.bs.adts_fullness;
  out->num_raw_blocks = adts.bs.num_raw_blocks;
  out->num_pce_bits = adts.bs.num_pce_bits;
  out->rdbLen0 = adtsRead_GetRawDataBlockLength(&adts, 0);
}

} /* extern "C" */
