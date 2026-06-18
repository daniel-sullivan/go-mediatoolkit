//go:build cgo

/* Compiles a private copy of the vendored minimp3 reference and surfaces the
 * top-level "frame-decode-dispatch" driver to Go-cgo via mp3parity_*
 * trampolines.
 *
 * The slice under test is minimp3's mp3dec_decode_frame / mp3dec_init
 * (minimp3.h:1708 / :1713) — the orchestrator that detects a frame, fills
 * mp3dec_frame_info_t, and dispatches to the granule decode + synthesis.
 * mp3dec_decode_frame is a public minimp3 symbol, but compiling it pulls in
 * the whole static minimp3 implementation, so we keep this in a private TU
 * (MINIMP3_IMPLEMENTATION defined here) exactly like the other oracle slices;
 * each go-test binary is one package wide, so this private copy never collides
 * with the one libraries/mp3 compiles for production, nor with the other
 * parity packages.
 *
 * The trampolines are verbatim calls into minimp3 — they add nothing but a
 * stable linkage name and (for the info-returning entry points) copy the
 * filled mp3dec_frame_info_t fields back out through plain int pointers so the
 * Go side never has to mirror the C struct layout.
 *
 * Scalar-baseline FP flags (-ffp-contract=off, …) come from the mise task env,
 * not from here. The dispatch surface this oracle exercises (mp3dec_init and
 * mp3dec_decode_frame in PROBE mode, pcm==NULL) is integer-only: it never
 * reaches L3_decode / the synthesis filterbank, so those flags do not affect
 * its results. The full-audio path is deliberately not driven here (see the
 * package doc in cgo.go).
 */

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"

#include <string.h>

/* ---- mp3dec_init ---- */

void mp3parity_init(mp3dec_t *dec) {
    mp3dec_init(dec);
}

/* Expose the cached-header byte the fast-resync path keys on, so the Go test
 * can assert mp3dec_init / a failed decode reset it identically. */
int mp3parity_header0(const mp3dec_t *dec) {
    return dec->header[0];
}

/* ---- mp3dec_decode_frame, PROBE mode (pcm == NULL) ----
 *
 * Drives the full frame-detect + header-parse + info-fill control flow and
 * returns the per-channel sample count, copying every mp3dec_frame_info_t
 * field back out. free_format_bytes round-trips through *io_free_format_bytes
 * so the Go side can compare the decoder's cached free-format size and seed it
 * for the fast-resync path. The return value is the C function's int result
 * (samples-per-channel, or 0 when no frame is accepted).
 */
int mp3parity_decode_probe(mp3dec_t *dec, const uint8_t *mp3, int mp3_bytes,
                           int *io_free_format_bytes, int *out_header0,
                           int *out_frame_bytes, int *out_frame_offset,
                           int *out_channels, int *out_hz, int *out_layer,
                           int *out_bitrate_kbps) {
    mp3dec_frame_info_t info;
    memset(&info, 0, sizeof(info));
    dec->free_format_bytes = *io_free_format_bytes;
    int samples = mp3dec_decode_frame(dec, mp3, mp3_bytes, (mp3d_sample_t *)0, &info);
    *io_free_format_bytes = dec->free_format_bytes;
    *out_header0      = dec->header[0];
    *out_frame_bytes  = info.frame_bytes;
    *out_frame_offset = info.frame_offset;
    *out_channels     = info.channels;
    *out_hz           = info.hz;
    *out_layer        = info.layer;
    *out_bitrate_kbps = info.bitrate_kbps;
    return samples;
}

/* ---- mp3dec_decode_frame fast-resync path ----
 *
 * Probes `mp3` once cold, then probes `mp3 + advance` with the SAME decoder so
 * the second call takes the cached-header branch (dec->header[0] == 0xff &&
 * hdr_compare(dec->header, mp3)). Returns the SECOND call's observables. Both
 * calls run in PROBE mode (pcm == NULL).
 */
int mp3parity_decode_twice(const uint8_t *mp3, int mp3_bytes, int advance,
                           int *out_header0, int *out_frame_bytes,
                           int *out_frame_offset, int *out_channels, int *out_hz,
                           int *out_layer, int *out_bitrate_kbps) {
    mp3dec_t dec;
    memset(&dec, 0, sizeof(dec));
    mp3dec_frame_info_t info;
    memset(&info, 0, sizeof(info));

    mp3dec_decode_frame(&dec, mp3, mp3_bytes, (mp3d_sample_t *)0, &info);

    const uint8_t *tail = mp3 + advance;
    int tail_bytes = mp3_bytes - advance;
    int samples = mp3dec_decode_frame(&dec, tail, tail_bytes, (mp3d_sample_t *)0, &info);

    *out_header0      = dec.header[0];
    *out_frame_bytes  = info.frame_bytes;
    *out_frame_offset = info.frame_offset;
    *out_channels     = info.channels;
    *out_hz           = info.hz;
    *out_layer        = info.layer;
    *out_bitrate_kbps = info.bitrate_kbps;
    return samples;
}
