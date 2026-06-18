/*
 * oracle.h — C oracle surface for the bit-allocation parity slice.
 *
 * "Bit allocation" in this minimp3-based Layer III port is the scalefactor
 * decode path: L3_decode_scalefactors (minimp3.h:654) turns a granule's coded
 * scalefactor-compress / subblock-gain / preflag / global-gain side-info into
 * the per-band float gain table the requantizer applies to each frequency
 * line. It calls L3_read_scalefactors (the raw scalefactor bit unpack, which
 * also fills the intensity-stereo position table ist_pos) and L3_ldexp_q2 (the
 * quarter-step power-of-two gain expansion — the only floating-point work in
 * the slice). This is the Layer III analog of the Layer I/II per-subband bit
 * allocation; minimp3 is MINIMP3_ONLY_MP3 here, so L3_decode_scalefactors is
 * the bit-allocation surface that exists.
 *
 * All of these functions are file-static inside minimp3.h, so there is no
 * public C API that reaches them. oracle.c defines MINIMP3_IMPLEMENTATION,
 * includes the vendored single-header library, and re-exports the exact static
 * functions through the thin extern wrappers declared below. The wrappers live
 * in the same translation unit as the statics, so they can see them; they add
 * no logic of their own, so the C reference under test is genuinely the
 * vendored minimp3 code, not a hand reimplementation.
 *
 * Input fabrication uses the library's OWN parser: oracle_read_side_info wraps
 * the static L3_read_side_info to build a real L3_gr_info_t from a raw
 * side-info byte buffer, exactly as the decode loop does, and the Go side runs
 * the matching nativemp3.L3ReadSideInfo over the same bytes. The two parsers
 * are bit-exact ports, so the granule structs match by construction; the
 * parity test then drives oracle_decode_scalefactors on that gr and the Go
 * L3DecodeScalefactors on the Go gr, and compares the resulting scf[40] gains
 * (bit-for-bit) and ist_pos[39] bytes. (L3_read_side_info's success RETURN
 * value differs between the committed minimp3 and the Go port — raw
 * main_data_begin vs bs.Pos/8 — but that quantity is never compared here; only
 * the populated gr fields, which agree, are used.)
 *
 * oracle_gr_t is a byte-layout mirror of minimp3's L3_gr_info_t so the Go side
 * can drive it via cgo without the parity TU exposing minimp3's own
 * (file-static-typedef'd) struct name. The size is asserted equal in oracle.c.
 */
#ifndef MP3_BIT_ALLOCATION_ORACLE_H
#define MP3_BIT_ALLOCATION_ORACLE_H

#include <stdint.h>

/* bs_t mirror (a borrowed byte pointer plus two ints). */
typedef struct {
    const uint8_t *buf;
    int pos, limit;
} oracle_bs_t;

/* L3_gr_info_t mirror. Field order/types match minimp3.h:209 exactly so the
 * oracle struct is ABI-identical to the one L3_read_side_info fills and
 * L3_decode_scalefactors reads. sfbtab is a borrowed pointer into a static
 * table; the parity test does not inspect it (L3_decode_scalefactors never
 * reads sfbtab), but it is mirrored for layout fidelity. */
typedef struct {
    const uint8_t *sfbtab;
    uint16_t part_23_length, big_values, scalefac_compress;
    uint8_t global_gain, block_type, mixed_block_flag, n_long_sfb, n_short_sfb;
    uint8_t table_select[3], region_count[3], subblock_gain[3];
    uint8_t preflag, scalefac_scale, count1_table, scfsi;
} oracle_gr_t;

/* Allocate / free a C-owned copy of a byte buffer. bs_t stashes a raw pointer
 * to the bytes it reads, and cgo forbids storing a Go pointer in C-visible
 * memory across calls, so the side-info / main-data buffers must live in
 * malloc'd C storage for the reader's lifetime. */
uint8_t *oracle_buf_new(const uint8_t *data, int bytes);
void     oracle_buf_free(uint8_t *p);

/* Parse a raw side-info byte buffer into up to 4 granule structs via the
 * vendored static L3_read_side_info. Returns 1 on success (valid side info),
 * 0 on the C error path (the "-1" return). On success out_gr[0..gr_count) are
 * filled, where gr_count is 4 for MPEG-1 stereo, 2 for MPEG-1 mono / MPEG-2
 * stereo, 1 for MPEG-2 mono. The header h selects MPEG version / channel mode
 * exactly as in the decode loop. */
int oracle_read_side_info(const uint8_t *side, int side_bytes,
                          const uint8_t *h, oracle_gr_t *out_gr);

/* Run the vendored static L3_decode_scalefactors for one granule/channel.
 *  - h            : 4-byte frame header (MPEG-1 / I-stereo / MS-stereo tests)
 *  - main         : main-data byte buffer the scalefactor bits are read from
 *  - main_bytes   : length of main in bytes (sets the bit reader limit)
 *  - gr           : the granule side-info produced by oracle_read_side_info
 *  - ch           : channel index (0/1) — selects the I-stereo branch
 *  - ist_pos_seed : 39 bytes pre-loaded into the ist_pos scratch (the C
 *                   ist_pos[ch] row carries across channels via scfsi reuse,
 *                   so the parity test seeds it identically on both sides)
 *  - out_scf      : receives the 40 float gains L3_decode_scalefactors writes
 *  - out_ist_pos  : receives the 39 ist_pos bytes after the call
 *  - out_bs_pos   : receives the bit reader position after the call
 */
void oracle_decode_scalefactors(const uint8_t *h,
                                const uint8_t *main, int main_bytes,
                                const oracle_gr_t *gr, int ch,
                                const uint8_t *ist_pos_seed,
                                float *out_scf, uint8_t *out_ist_pos,
                                int *out_bs_pos);

#endif /* MP3_BIT_ALLOCATION_ORACLE_H */
