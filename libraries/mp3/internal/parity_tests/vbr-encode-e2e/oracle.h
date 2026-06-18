/*
 * oracle.h — C oracle surface for the vbr-encode-e2e parity slice.
 *
 * This is the TOP-LEVEL byte-identical gate for the pure-Go LAME VBR encoder. It
 * pins the COMPLETE -V2 (vbr_default == vbr_mtrh) write path — every stage from
 * lame_encode_buffer through the VBR_new_iteration_loop, format_bitstream, the
 * Xing/Info/LAME tag — against the genuine vendored libmp3lame, asserting the
 * FULL OUTPUT STREAM (the finalized tag frame PLUS every audio frame) is
 * byte-for-byte identical.
 *
 * STREAM LAYOUT. LAME's file output is: a placeholder Xing/Info tag frame written
 * first by InitVbrTag (lame_init_bitstream), then the audio frames, and on close
 * the caller fseek(0)s and overwrites the placeholder with the finalized tag from
 * lame_get_lametag_frame. This oracle reproduces that exact file layout: it
 * buffers the whole encode (placeholder first), builds the real tag, and splices
 * it over the leading TotalFrameSize bytes. The Go side (native.go) drives the
 * pure-Go nativemp3 encoder, whose public Close path does the identical splice,
 * and the two assembled streams must match byte-for-byte.
 *
 * IDENTICAL INPUT. To remove any PCM-generation FP divergence between C sin() and
 * Go math.Sin, the oracle GENERATES the synthetic int16 PCM and EXPORTS it, so
 * the Go side encodes the byte-identical int16 samples. Both encoders therefore
 * see the same input and the only thing under test is the encode+tag path.
 *
 * TRANSLATION UNITS (parity discipline — each go-test binary compiles its OWN
 * copy of the reference; see CONTRIBUTING.md). A full
 * real -V2 encode needs the whole vendored libmp3lame encoder, one vendored
 * source per TU (the lame_*.c wrappers), exactly the per-TU split the public cgo
 * backend and the vbrtag slice use (LAME reuses Min/Max macros + per-TU file
 * statics — fi_union/MAGIC in takehiro.c/vbrquantize.c — so they must not share a
 * TU). oracle.c is its own TU: it drives the public LAME API and assembles the
 * file-layout stream. It NEVER imports libraries/mp3 (which would duplicate the
 * LAME symbols at link time); only the pure-Go internal/nativemp3 port is used on
 * the Go side.
 *
 * FP PARITY. The -V2 encode is heavily FP-bearing (mdct, psymodel, the vbrquantize
 * leaf kernels), so the byte-identical stream holds only under the mp3_strict
 * build (FMA-free Go) vs the -ffp-contract=off oracle. The scalar FP flags come
 * from the mise task env (CGO_CFLAGS), never the in-source #cgo block.
 *
 * LGPL note: the whole vendored encoder and the Go port it pins are LGPL LAME
 * source, gated by the mp3lame build tag (plus cgo). A bare `go test` never
 * compiles them.
 */
#ifndef MP3_VBR_ENCODE_E2E_ORACLE_H
#define MP3_VBR_ENCODE_E2E_ORACLE_H

typedef struct mp3parity_vbre2e_t mp3parity_vbre2e_t;

/* ---- handle lifecycle ----
 *
 * mp3parity_vbre2e_run drives a genuine -V2 encode of nsamples_per_ch granules of
 * synthetic PCM (a deterministic frequency-swept sine the caller seeds via
 * `seed`) at the given sample rate / channels, then assembles the file-layout
 * stream (placeholder tag overwritten by the real lame_get_lametag_frame) and
 * captures both the generated int16 PCM and the final stream bytes. Returns NULL
 * on any LAME setup/encode failure. */
mp3parity_vbre2e_t *mp3parity_vbre2e_run(int samplerate, int channels,
                                         int nsamples_per_ch, unsigned seed);
void                mp3parity_vbre2e_free(mp3parity_vbre2e_t *h);

/* ---- generated input PCM (interleaved int16, length = nsamples_per_ch*channels)
 * ---- the Go side encodes these exact samples. */
int          mp3parity_vbre2e_pcm_len(const mp3parity_vbre2e_t *h);
const short *mp3parity_vbre2e_pcm_ptr(const mp3parity_vbre2e_t *h);

/* ---- assembled golden stream (finalized tag frame + audio frames) ---- */
int                  mp3parity_vbre2e_stream_len(const mp3parity_vbre2e_t *h);
const unsigned char *mp3parity_vbre2e_stream_ptr(const mp3parity_vbre2e_t *h);

/* ---- the finalized tag frame length (== the spliced prefix size), so the Go
 * side can localize a divergence to "tag region" vs "audio region". ---- */
int mp3parity_vbre2e_tag_len(const mp3parity_vbre2e_t *h);

#endif /* MP3_VBR_ENCODE_E2E_ORACLE_H */
