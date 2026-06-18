/*
 * oracle.h — C oracle surface for the bitstream-format parity slice.
 *
 * minimp3's bitstream / header / frame-sync / reservoir routines are all
 * file-static inside minimp3.h, so there is no public C API that reaches
 * them. oracle.c defines MINIMP3_IMPLEMENTATION, includes the vendored
 * single-header library, and re-exports the exact static functions through
 * the thin extern wrappers declared below. The wrappers live in the same
 * translation unit as the statics, so they can see them; they perform no
 * logic of their own beyond forwarding arguments, so the C reference under
 * test is genuinely the vendored minimp3 code, not a hand reimplementation.
 *
 * The structs (oracle_bs_t, oracle_mp3dec_t, oracle_scratch_t) are byte
 * layout mirrors of minimp3's bs_t / mp3dec_t / mp3dec_scratch_t so the Go
 * side can drive them via cgo without the parity TU exposing minimp3's own
 * (also static-typedef'd) struct names. Sizes/offsets are asserted equal in
 * oracle.c via _Static_assert.
 */
#ifndef MP3_BITSTREAM_FORMAT_ORACLE_H
#define MP3_BITSTREAM_FORMAT_ORACLE_H

#include <stdint.h>

/* MAX_BITRESERVOIR_BYTES + MAX_L3_FRAME_PAYLOAD_BYTES from minimp3.h. */
#define ORACLE_MAINDATA_BYTES (511 + 2304)
#define ORACLE_RESERV_BYTES   511

/* bs_t mirror. */
typedef struct {
    const uint8_t *buf;
    int pos, limit;
} oracle_bs_t;

/* The subset of mp3dec_t the reservoir routines touch. The full struct also
 * carries float mdct_overlap / qmf_state arrays that belong to other slices;
 * we mirror the whole thing so memcpy/offsets line up with the real type. */
typedef struct {
    float mdct_overlap[2][9 * 32], qmf_state[15 * 2 * 32];
    int reserv, free_format_bytes;
    unsigned char header[4], reserv_buf[ORACLE_RESERV_BYTES];
} oracle_mp3dec_t;

/* bit reader. The backing bytes must live in C-owned storage for the lifetime
 * of the reader: bs_t stashes a raw pointer into them, and cgo forbids storing
 * a Go pointer in C-visible memory across calls. oracle_buf_new copies a Go
 * slice into a malloc'd buffer the caller frees with oracle_buf_free. */
uint8_t *oracle_buf_new(const uint8_t *data, int bytes);
void     oracle_buf_free(uint8_t *p);
void     oracle_bs_init(oracle_bs_t *bs, const uint8_t *data, int bytes);
uint32_t oracle_get_bits(oracle_bs_t *bs, int n);

/* header field accessors / predicates */
int      oracle_hdr_is_mono(const uint8_t *h);
int      oracle_hdr_is_free_format(const uint8_t *h);
int      oracle_hdr_is_crc(const uint8_t *h);
int      oracle_hdr_test_padding(const uint8_t *h);
int      oracle_hdr_test_mpeg1(const uint8_t *h);
int      oracle_hdr_test_not_mpeg25(const uint8_t *h);
int      oracle_hdr_get_layer(const uint8_t *h);
int      oracle_hdr_get_bitrate(const uint8_t *h);
int      oracle_hdr_get_sample_rate(const uint8_t *h);
int      oracle_hdr_get_my_sample_rate(const uint8_t *h);
int      oracle_hdr_is_frame_576(const uint8_t *h);
int      oracle_hdr_is_layer_1(const uint8_t *h);
int      oracle_hdr_valid(const uint8_t *h);
int      oracle_hdr_compare(const uint8_t *h1, const uint8_t *h2);
unsigned oracle_hdr_bitrate_kbps(const uint8_t *h);
unsigned oracle_hdr_sample_rate_hz(const uint8_t *h);
unsigned oracle_hdr_frame_samples(const uint8_t *h);
int      oracle_hdr_frame_bytes(const uint8_t *h, int free_format_size);
int      oracle_hdr_padding(const uint8_t *h);

/* frame sync */
int oracle_mp3d_match_frame(const uint8_t *hdr, int mp3_bytes, int frame_bytes);
int oracle_mp3d_find_frame(const uint8_t *mp3, int mp3_bytes,
                           int *free_format_bytes, int *ptr_frame_bytes);

/* reservoir reassembly. The decoder + scratch buffers are owned by C so the
 * memmove/memcpy land in real minimp3-sized storage; the Go side reads the
 * relevant fields back through the accessors below. */
typedef struct oracle_scratch oracle_scratch_t;
oracle_scratch_t *oracle_scratch_new(void);
void              oracle_scratch_free(oracle_scratch_t *s);

oracle_mp3dec_t  *oracle_dec_new(void);
void              oracle_dec_free(oracle_mp3dec_t *h);

/* Seed scratch.maindata[0..n) and the scratch bit reader (pos/limit) so the
 * Go and C reservoir round-trips start from identical state. */
void oracle_scratch_set_maindata(oracle_scratch_t *s, const uint8_t *data, int n);
void oracle_scratch_set_bs(oracle_scratch_t *s, int pos, int limit);
void oracle_scratch_get_maindata(oracle_scratch_t *s, uint8_t *out, int n);
int  oracle_scratch_bs_pos(oracle_scratch_t *s);
int  oracle_scratch_bs_limit(oracle_scratch_t *s);

void oracle_L3_save_reservoir(oracle_mp3dec_t *h, oracle_scratch_t *s);
int  oracle_L3_restore_reservoir(oracle_mp3dec_t *h, oracle_bs_t *bs,
                                 oracle_scratch_t *s, int main_data_begin);

/* Note: L3_read_side_info is intentionally NOT exposed here. The committed
 * vendored minimp3.h returns the raw main_data_begin back-pointer on success,
 * whereas the Go port (nativemp3.L3ReadSideInfo) returns bs.Pos/8 (the
 * main-data byte offset) — they were tracked from different minimp3 revisions.
 * A parity oracle would compare different quantities, so the side-info slice
 * is deferred pending a reconciliation decision (see the task report). */

#endif /* MP3_BITSTREAM_FORMAT_ORACLE_H */
