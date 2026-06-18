/* SPDX-License-Identifier: FDK-AAC */
/* Shared bridge declarations for the ADTS frame-sync-parse parity oracle.
 * Included by both the cgo preamble in cgo.go and the oracle TU
 * oracle_adts_cgo.cpp so the fparity_adts_result layout is identical on both
 * sides. Fields mirror STRUCT_ADTS_BS plus the parse result. */
#ifndef FPARITY_ADTS_BRIDGE_H
#define FPARITY_ADTS_BRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
  int err;     /* raw TRANSPORTDEC_ERROR value */
  int rdbLen0; /* adtsRead_GetRawDataBlockLength(pAdts, 0) */
  unsigned char mpeg_id;
  unsigned char layer;
  unsigned char protection_absent;
  unsigned char profile;
  unsigned char sample_freq_index;
  unsigned char private_bit;
  unsigned char channel_config;
  unsigned char original;
  unsigned char home;
  unsigned char copyright_id;
  unsigned char copyright_start;
  unsigned short frame_length;
  unsigned short adts_fullness;
  unsigned char num_raw_blocks;
  unsigned char num_pce_bits;
} fparity_adts_result;

/* Runs the lifted ADTS syncword search over buf[0:len]; returns the raw
 * TRANSPORTDEC_ERROR and, on OK, writes the post-syncword bit position to
 * *bitPosOut. */
extern int fparity_find_syncword(const unsigned char *buf, int len,
                                 int *bitPosOut);

/* Runs the genuine vendored adtsRead_DecodeHeader over buf[0:len] (syncword
 * pre-consumed) and reports every parsed field plus the block-0 length. */
extern void fparity_adts_decode_header(const unsigned char *buf, int len,
                                       int decoderCanDoMpeg4,
                                       int bufferFullnessStartFlag,
                                       int ignoreBufferFullness,
                                       fparity_adts_result *out);

#ifdef __cplusplus
}
#endif

#endif /* FPARITY_ADTS_BRIDGE_H */
