/* SPDX-License-Identifier: FDK-AAC */
/* Shared bridge declarations for the sfb-offset / sampling-rate-info ROM parity
 * oracle. Included by both the cgo preamble in cgo.go and the oracle TU
 * oracle_romtables_cgo.cpp so the fparity_sri_result layout is identical on both
 * sides. Mirrors the vendored SamplingRateInfo (channelinfo.h:152) plus the
 * AAC_DECODER_ERROR code getSamplingRateInfo returns; the variable-length
 * ScaleFactorBands_Long / _Short pointers are materialised into fixed buffers
 * (the longest long table is 52 entries, the longest short table 16) so the Go
 * side can compare every offset by value. */
#ifndef FPARITY_ROMTABLES_BRIDGE_H
#define FPARITY_ROMTABLES_BRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

/* Upper bounds for the copied offset arrays. The longest long table is
 * sfb_32_1024 (52 entries incl. the terminating transform length); the longest
 * short table is 16 entries. */
#define FPARITY_MAX_LONG 64
#define FPARITY_MAX_SHORT 32

typedef struct {
  int err; /* raw AAC_DECODER_ERROR value (0 == AAC_DEC_OK) */

  /* SamplingRateInfo scalar fields (channelinfo.h:152). */
  unsigned char number_of_sfb_long;  /* NumberOfScaleFactorBands_Long */
  unsigned char number_of_sfb_short; /* NumberOfScaleFactorBands_Short */
  unsigned int sampling_rate_index;  /* samplingRateIndex */
  unsigned int sampling_rate;        /* samplingRate */

  /* Whether the resolved long / short table pointer was NULL. */
  int long_is_null;
  int short_is_null;

  /* The resolved offset tables, copied [0 .. number_of_sfb_*] inclusive (the
   * terminating transform length is included). Valid only when the matching
   * *_is_null is 0. */
  short long_offsets[FPARITY_MAX_LONG];
  short short_offsets[FPARITY_MAX_SHORT];
} fparity_sri_result;

/* Runs the verbatim getSamplingRateInfo (channelinfo.cpp:225) for the given
 * frame length / sampling-rate index / rate and reports every resolved field
 * plus the copied offset tables. */
extern void fparity_get_sampling_rate_info(unsigned int samplesPerFrame,
                                           unsigned int samplingRateIndex,
                                           unsigned int samplingRate,
                                           fparity_sri_result *out);

#ifdef __cplusplus
}
#endif

#endif /* FPARITY_ROMTABLES_BRIDGE_H */
