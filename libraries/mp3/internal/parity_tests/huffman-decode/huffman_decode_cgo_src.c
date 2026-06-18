//go:build cgo

/* Compiles a private copy of the vendored minimp3 reference and surfaces the
 * static Layer III Huffman-unpacking helpers (L3_huffman, L3_pow_43, and the
 * g_pow43 dequantization table) to Go-cgo via mp3parity_* trampolines.
 *
 * minimp3 is a single-header library: L3_huffman, L3_pow_43 and g_pow43 are
 * all `static` inside minimp3.h, so they can only be reached from a
 * translation unit that #includes the implementation. We therefore define
 * MINIMP3_IMPLEMENTATION here and wrap each behind a stable, non-static
 * linkage name. Each go-test binary is one package wide, so this private
 * minimp3 copy never collides with the one libraries/mp3 compiles for
 * production (and this package never imports libraries/mp3, which would
 * compile minimp3 a second time and clash on its statics).
 *
 * MINIMP3_NO_SIMD keeps the reference on its scalar baseline. The Huffman
 * tree traversal is integer-only, but the dequantization (g_pow43 reads and
 * L3_pow_43's polynomial) is floating point, so the scalar FP flags
 * (-ffp-contract=off, -fno-vectorize, -fno-slp-vectorize, -fno-unroll-loops)
 * matter here. They come from the mise task env (CGO_CFLAGS +
 * CGO_CFLAGS_ALLOW), never from the in-source #cgo block, because Go's cgo
 * flag allowlist rejects them.
 */

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"

#include <string.h>

/* ---- L3_pow_43 + g_pow43 ----
 *
 * Direct pass-throughs so the parity suite can pin the dequantization power
 * function and its lookup table entry-for-entry against the Go port.
 */

float mp3parity_l3_pow_43(int x) { return L3_pow_43(x); }

/* mp3parity_g_pow43 copies the static g_pow43[129 + 16] table out so the Go
 * side can confirm its transcription is bit-identical. */
void mp3parity_g_pow43(float *out) {
    memcpy(out, g_pow43, sizeof(g_pow43));
}

/* ---- L3_huffman ----
 *
 * L3_huffman reads gr_info->{sfbtab, big_values, table_select[3],
 * region_count[3], count1_table}, the scf[] dequantization gains, and the
 * frame's main-data bits via bs, writing 576 dequantized frequency lines into
 * dst and parking bs->pos at layer3gr_limit on return.
 *
 * The trampoline reassembles a bs_t over the caller-supplied payload (the
 * reassembled main-data buffer the C decode loop builds in
 * mp3dec_scratch_t.maindata) and an L3_gr_info_t from the discrete fields the
 * Go side mirrors, then surfaces the 576 dst floats plus the final bs->pos.
 *
 * sfbtab and scf are the const arrays L3_huffman walks with *sfb++ / *scf++;
 * the caller owns them and they must outlive the call. The payload buffer
 * likewise aliases bs->buf for the duration of the call. L3_huffman primes a
 * 4-byte cache and may read a few bytes past bs->pos/8 + the consumed run, so
 * the caller pads the payload generously (as the real decoder's maindata
 * scratch is sized to MAX_BITRESERVOIR_BYTES + MAX_L3_FRAME_PAYLOAD_BYTES).
 */
void mp3parity_l3_huffman(float *dst,
                          const uint8_t *payload, int payload_bytes, int bs_pos,
                          const uint8_t *sfbtab,
                          uint16_t big_values,
                          const uint8_t table_select[3],
                          const uint8_t region_count[3],
                          uint8_t count1_table,
                          const float *scf,
                          int layer3gr_limit,
                          int *out_pos) {
    bs_t bs;
    bs_init(&bs, payload, payload_bytes);
    bs.pos = bs_pos;

    L3_gr_info_t gr;
    memset(&gr, 0, sizeof(gr));
    gr.sfbtab       = sfbtab;
    gr.big_values   = big_values;
    gr.table_select[0] = table_select[0];
    gr.table_select[1] = table_select[1];
    gr.table_select[2] = table_select[2];
    gr.region_count[0] = region_count[0];
    gr.region_count[1] = region_count[1];
    gr.region_count[2] = region_count[2];
    gr.count1_table = count1_table;

    L3_huffman(dst, &bs, &gr, scf, layer3gr_limit);
    *out_pos = bs.pos;
}
