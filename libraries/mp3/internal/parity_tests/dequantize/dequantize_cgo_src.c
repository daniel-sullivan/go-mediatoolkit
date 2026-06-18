//go:build cgo

/* Compiles a private copy of the vendored minimp3 reference and surfaces the
 * static Layer III scalefactor-dequantization helpers (L3_read_scalefactors,
 * L3_ldexp_q2, and L3_decode_scalefactors) to Go-cgo via mp3parity_*
 * trampolines.
 *
 * minimp3 is a single-header library: L3_read_scalefactors, L3_ldexp_q2 and
 * L3_decode_scalefactors are all `static` inside minimp3.h, so they can only
 * be reached from a translation unit that #includes the implementation. We
 * therefore define MINIMP3_IMPLEMENTATION here and wrap each behind a stable,
 * non-static linkage name. Each go-test binary is one package wide, so this
 * private minimp3 copy never collides with the one libraries/mp3 compiles for
 * production (and this package never imports libraries/mp3, which would
 * compile minimp3 a second time and clash on its statics).
 *
 * MINIMP3_NO_SIMD keeps the reference on its scalar baseline. The scalefactor
 * unpack (L3_read_scalefactors) and the integer body of
 * L3_decode_scalefactors are integer-only, but the gain expansion
 * (L3_ldexp_q2's float32 multiplies) is floating point, so the scalar FP flags
 * (-ffp-contract=off, -fno-vectorize, -fno-slp-vectorize, -fno-unroll-loops)
 * matter here. They come from the mise task env (CGO_CFLAGS +
 * CGO_CFLAGS_ALLOW), never from the in-source #cgo block, because Go's cgo
 * flag allowlist rejects them.
 */

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"

#include <string.h>

/* ---- L3_ldexp_q2 ----
 *
 * Direct pass-through so the parity suite can pin the quarter-step
 * power-of-two gain accumulator across the negative-shift caveat documented in
 * the Go port (the `1 << 30 >> (e >> 2)` shift by a negative count). The C is
 * compiled by clang on the same arm64 host, so the oracle captures the actual
 * hardware lowering the Go port reproduces with explicit mod-32 masking.
 */
float mp3parity_l3_ldexp_q2(float y, int exp_q2) { return L3_ldexp_q2(y, exp_q2); }

/* ---- L3_read_scalefactors ----
 *
 * L3_read_scalefactors unpacks the four scalefactor partitions for one granule
 * from the bit reader into scf, also writing the intensity-stereo position
 * scratch ist_pos. It is pure-integer (memcpy / memset / get_bits), so it
 * matches bit-for-bit in any build, but the suite gates it uniformly.
 *
 * The trampoline reassembles a bs_t over the caller-supplied payload (aliased
 * for the duration of the call) and forwards the discrete arrays. scf and
 * ist_pos are the writable outputs L3_read_scalefactors fills with *scf++ /
 * *ist_pos++ post-increments plus three trailing guard zeros into scf; the
 * caller sizes them as the real decoder does (iscf[40] and ist_pos[39]).
 * scf_size is scf_size[4] and scf_count is the selected g_scf_partitions row
 * (28 bytes); the caller owns all four and they must outlive the call.
 */
void mp3parity_l3_read_scalefactors(uint8_t *scf, uint8_t *ist_pos,
                                    const uint8_t *scf_size, const uint8_t *scf_count,
                                    const uint8_t *payload, int payload_bytes, int bs_pos,
                                    int scfsi, int *out_pos) {
    bs_t bs;
    bs_init(&bs, payload, payload_bytes);
    bs.pos = bs_pos;
    L3_read_scalefactors(scf, ist_pos, scf_size, scf_count, &bs, scfsi);
    *out_pos = bs.pos;
}

/* ---- L3_decode_scalefactors ----
 *
 * L3_decode_scalefactors decodes one granule's scalefactors and expands them
 * into the per-band float gain table scf. It reads the 4-byte frame header
 * (for the MPEG-1 / I-stereo / MS-stereo tests), the per-channel ist_pos
 * scratch, the granule side-info, and the main-data bits via bs, then writes
 * n_long_sfb + n_short_sfb float gains into scf.
 *
 * The trampoline reassembles a bs_t over the caller-supplied payload and an
 * L3_gr_info_t from the discrete fields the Go side mirrors (the members
 * L3_decode_scalefactors consumes: scalefac_compress, global_gain,
 * scalefac_scale, n_long_sfb, n_short_sfb, subblock_gain[3], preflag, scfsi),
 * then surfaces the gain table and the final bs->pos. hdr is the 4 header
 * bytes; ist_pos is this channel's 39-byte scratch.
 */
void mp3parity_l3_decode_scalefactors(const uint8_t hdr[4], uint8_t *ist_pos,
                                      const uint8_t *payload, int payload_bytes, int bs_pos,
                                      uint16_t scalefac_compress,
                                      uint8_t global_gain,
                                      uint8_t scalefac_scale,
                                      uint8_t n_long_sfb,
                                      uint8_t n_short_sfb,
                                      const uint8_t subblock_gain[3],
                                      uint8_t preflag,
                                      uint8_t scfsi,
                                      int ch,
                                      float *scf, int *out_pos) {
    bs_t bs;
    bs_init(&bs, payload, payload_bytes);
    bs.pos = bs_pos;

    L3_gr_info_t gr;
    memset(&gr, 0, sizeof(gr));
    gr.scalefac_compress = scalefac_compress;
    gr.global_gain       = global_gain;
    gr.scalefac_scale    = scalefac_scale;
    gr.n_long_sfb        = n_long_sfb;
    gr.n_short_sfb       = n_short_sfb;
    gr.subblock_gain[0]  = subblock_gain[0];
    gr.subblock_gain[1]  = subblock_gain[1];
    gr.subblock_gain[2]  = subblock_gain[2];
    gr.preflag           = preflag;
    gr.scfsi             = scfsi;

    L3_decode_scalefactors(hdr, ist_pos, &bs, &gr, scf, ch);
    *out_pos = bs.pos;
}
