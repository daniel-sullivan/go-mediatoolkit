// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Compiles a PRIVATE copy of the vendored LAME 3.100 bitstream writer and
 * surfaces its file-static bit-writer helpers to Go-cgo via mp3parity_*
 * trampolines, for the "huffman-encode" parity slice.
 *
 * Scope. nativemp3's huffman-encode slice (bitstream_encode.go +
 * huffman_encode.go) ports two distinct pieces of LAME's bitstream.c:
 *
 *   (a) the low-level bit WRITER — putbits2 / putbits_noheaders /
 *       putheader_bits — which is self-contained integer shifting, and
 *   (b) the Huffman code EMITTERS — huffman_coder_count1 / Huffmancode /
 *       Short/LongHuffmancodebits — which index the ht[] codebook tables.
 *
 * This oracle covers piece (a) only. Piece (b) cannot be parity-driven yet:
 * the Go ht[] codebook array is declared but still EMPTY (huffman_encode.go:51
 * — "Until the tables slice populates ht, these emitters have no codebooks to
 * read"), and the emitter methods are unexported. Once the tables.c port
 * lands and the emitters are exported, extend this TU with Huffmancode /
 * huffman_coder_count1 trampolines driving a primed gr_info + ht[] and pin
 * them here. See parity_test.go's package doc for the same note.
 *
 * Discipline (per CONTRIBUTING.md "parity oracle per
 * slice"): each go-test binary is one package wide, so this private LAME copy
 * never collides with the one libraries/mp3 compiles for production. This
 * package never imports libraries/mp3; it imports only internal/nativemp3.
 *
 * putbits2 / putbits_noheaders / putheader_bits are all `static` (the first
 * two `inline static`) inside bitstream.c, so they are only reachable from a
 * TU that #includes the implementation. We therefore #include bitstream.c
 * here (real reference code, not a hand reimplementation) plus tables.c for
 * the ht[] / bitrate_table data bitstream.c links against, and wrap each
 * static the trampolines need.
 *
 * Four symbols are referenced by OTHER bitstream.c functions that the
 * bit-writer trampolines never reach (format_bitstream, copy_buffer,
 * drain_into_ancillary, compute_flushbits, writeMainData): UpdateMusicCRC,
 * get_lame_short_version, lame_errorf, and slen1_tab / slen2_tab. They are
 * provided here as link-only definitions so the single-TU oracle links; the
 * slen tables carry the verbatim takehiro.c values so even an accidental read
 * is faithful, and the three functions are inert stubs that are never called
 * on any path this oracle exercises.
 *
 * This slice is integer-only (no FP arithmetic anywhere in the bit writer),
 * so it is bit-identical regardless of build tag or vectorization. The scalar
 * FP flags (-ffp-contract=off, ...) still come from the mise task env, never
 * from the in-source #cgo block, matching the rest of the suite.
 */

#include <config.h>
#include <stdlib.h>
#include <string.h>

#include "libmp3lame/bitstream.c"
#include "libmp3lame/tables.c"

/* Opaque handle the Go side passes back to the trampolines. Declared
 * identically in cgo.go's preamble; the trampolines cast it to the real
 * lame_internal_flags below. */
typedef struct mp3parity_enc_t mp3parity_enc_t;

/* ---- link-only definitions for unreachable bitstream.c code ---- */

const int slen1_tab[16] = { 0, 0, 0, 0, 3, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4 };
const int slen2_tab[16] = { 0, 1, 2, 3, 0, 1, 2, 3, 1, 2, 3, 1, 2, 3, 2, 3 };

void UpdateMusicCRC(uint16_t *crc, const unsigned char *buffer, int size) {
    (void)crc; (void)buffer; (void)size;
}
const char *get_lame_short_version(void) { return ""; }
void lame_errorf(const lame_internal_flags *gfc, const char *format, ...) {
    (void)gfc; (void)format;
}

/* ---- oracle state plumbing ----
 *
 * lame_internal_flags is large; we calloc one zeroed instance and touch only
 * the handful of fields the bit writer reads/writes: bs.{buf,buf_size,totbit,
 * buf_byte_idx,buf_bit_idx}, cfg.sideinfo_len, and the sv_enc header ring
 * (header[w_ptr].{write_timing,buf} + w_ptr). The output bytes land in a
 * caller-sized malloc'd buffer that bs->buf borrows.
 */

mp3parity_enc_t *mp3parity_enc_new(int buf_size) {
    lame_internal_flags *gfc = calloc(1, sizeof(*gfc));
    gfc->bs.buf = calloc((size_t)buf_size, 1);
    gfc->bs.buf_size = buf_size;
    gfc->bs.totbit = 0;
    gfc->bs.buf_byte_idx = 0;
    gfc->bs.buf_bit_idx = 0;
    return (mp3parity_enc_t *)gfc;
}

void mp3parity_enc_free(mp3parity_enc_t *e) {
    lame_internal_flags *gfc = (lame_internal_flags *)e;
    free(gfc->bs.buf);
    free(gfc);
}

/* mp3parity_enc_set_sideinfo_len primes cfg->sideinfo_len, the byte count
 * putheader_bits splices in when a header's write_timing is reached. */
void mp3parity_enc_set_sideinfo_len(mp3parity_enc_t *e, int len) {
    ((lame_internal_flags *)e)->cfg.sideinfo_len = len;
}

/* mp3parity_enc_prime_header loads header[slot].{write_timing,buf[0..len)} so
 * putbits2 can trigger a header splice at the matching totbit. */
void mp3parity_enc_prime_header(mp3parity_enc_t *e, int slot, int write_timing,
                                const unsigned char *buf, int len) {
    lame_internal_flags *gfc = (lame_internal_flags *)e;
    gfc->sv_enc.header[slot].write_timing = write_timing;
    if (len > MAX_HEADER_LEN) len = MAX_HEADER_LEN;
    memcpy(gfc->sv_enc.header[slot].buf, buf, (size_t)len);
}

void mp3parity_enc_set_wptr(mp3parity_enc_t *e, int w_ptr) {
    ((lame_internal_flags *)e)->sv_enc.w_ptr = w_ptr;
}

/* By LAME's contract every UNPRIMED header slot must carry a write_timing that
 * is never == totbit, or putbits2 would splice an all-zero header. The C decode
 * loop guarantees this by setting write_timing as frames are queued; for the
 * isolated bit-writer test we set every slot to a sentinel that putbits2's
 * running totbit can never hit. */
void mp3parity_enc_disarm_headers(mp3parity_enc_t *e, int sentinel) {
    lame_internal_flags *gfc = (lame_internal_flags *)e;
    int i;
    for (i = 0; i < MAX_HEADER_BUF; i++) {
        gfc->sv_enc.header[i].write_timing = sentinel;
    }
}

/* ---- bit-writer trampolines (verbatim calls into the real statics) ---- */

void mp3parity_putbits2(mp3parity_enc_t *e, int val, int j) {
    putbits2((lame_internal_flags *)e, val, j);
}
void mp3parity_putbits_noheaders(mp3parity_enc_t *e, int val, int j) {
    putbits_noheaders((lame_internal_flags *)e, val, j);
}
void mp3parity_putheader_bits(mp3parity_enc_t *e) {
    putheader_bits((lame_internal_flags *)e);
}

/* ---- state read-back ---- */

int mp3parity_enc_totbit(const mp3parity_enc_t *e) {
    return ((const lame_internal_flags *)e)->bs.totbit;
}
int mp3parity_enc_buf_byte_idx(const mp3parity_enc_t *e) {
    return ((const lame_internal_flags *)e)->bs.buf_byte_idx;
}
int mp3parity_enc_buf_bit_idx(const mp3parity_enc_t *e) {
    return ((const lame_internal_flags *)e)->bs.buf_bit_idx;
}
int mp3parity_enc_wptr(const mp3parity_enc_t *e) {
    return ((const lame_internal_flags *)e)->sv_enc.w_ptr;
}

/* mp3parity_enc_copy_buf copies n output bytes from bs->buf into out. */
void mp3parity_enc_copy_buf(const mp3parity_enc_t *e, unsigned char *out, int n) {
    const lame_internal_flags *gfc = (const lame_internal_flags *)e;
    memcpy(out, gfc->bs.buf, (size_t)n);
}
