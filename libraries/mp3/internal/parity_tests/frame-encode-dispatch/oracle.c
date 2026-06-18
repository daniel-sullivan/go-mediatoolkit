// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/*
 * oracle.c — compiles the vendored LAME 3.100 per-frame encode driver and
 * re-exports its dispatcher (lame_encode_mp3_frame, encoder.c:305) and the
 * one-time filterbank prime (lame_encode_frame_init, encoder.c:189) for the
 * frame-encode-dispatch parity tests.
 *
 * This translation unit #includes the committed libmp3lame/encoder.c, which
 * brings the genuine vendored lame_encode_mp3_frame / lame_encode_frame_init
 * (and the file-static adjust_ATH / updateStats they call) into scope. The
 * mp3parity_* trampolines below forward straight through to them, so the C side
 * of every parity assertion is the real reference, never a hand twin.
 *
 * STAGE SEAM (matches the Go FrameEncodeStages stub). lame_encode_mp3_frame is
 * the top of the encode call tree: it calls out to the heavy per-stage callees
 * (L3psycho_anal_vbr in psymodel.c, mdct_sub48 in newmdct.c, the four
 * *_iteration_loop in quantize.c, format_bitstream / copy_buffer in
 * bitstream.c, AddVbrFrame in VbrTag.c) which are separate not-yet-pinned
 * slices. This slice pins only the DISPATCHER'S OWN arithmetic — the padding
 * accumulator, the JOINT_STEREO M/S energy ratio, the M/S-vs-L/R perceptual-
 * entropy sums, the M/S mode_ext decision, and the CBR/ABR perceptual-entropy
 * smoothing FIR — exactly the control flow frame_encode.go translates. To run
 * that genuine vendored arithmetic in isolation, this TU provides inert stub
 * definitions of those extern callees (encoder.c declares them via the headers;
 * they are NOT static in encoder.c, so defining them here satisfies the link).
 * The stubs are the input-fabrication seam, parallel to the Go test's stub
 * Stages: L3psycho_anal_vbr writes the test-supplied pe / pe_MS / tot_ener /
 * blocktype the dispatcher reads back; mdct_sub48 records the buffers it was
 * primed with so the prime's shift logic can be checked; copy_buffer returns a
 * fixed byte count; the rest are no-ops. None of these stubs participate in the
 * arithmetic this slice asserts — they only feed it controlled inputs, the same
 * way the Go FrameEncodeStages stub does.
 *
 * encoder.c is compiled in isolation as its own TU (one .c per parity binary)
 * so each go-test binary's symbol set is self-contained — no cross-package
 * static-symbol clash (see the parity discipline in
 * CONTRIBUTING.md). This package never imports
 * libraries/mp3 (which would duplicate the LAME symbols at link time); it only
 * imports the pure-Go internal/nativemp3 port.
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS
 * (cgo.go). The FP-determinism flags (-ffp-contract=off, -fno-vectorize,
 * -fno-slp-vectorize, -fno-unroll-loops) come from the mise task env so the
 * dispatcher's M/S ratio, PE sums and smoothing FIR round separately, matching
 * the FMA-free mp3_strict Go build. They are NOT placed here because Go's cgo
 * flag allowlist rejects -ffp-contract=off in source.
 *
 * LGPL note: encoder.c is LGPL LAME source, so this oracle TU and the Go
 * dispatcher it pins are gated by the mp3lame build tag (in addition to cgo),
 * exactly like the encoder-dispatch slice they test. A bare `go test` never
 * compiles them.
 */

#include "encoder.c"

#include "oracle.h"

#include <string.h>

/* ------------------------------------------------------------------ *
 *  Stage seam — inert stub callees + the test-supplied inputs they    *
 *  feed back into the genuine dispatcher arithmetic.                  *
 * ------------------------------------------------------------------ */

/* Test-settable psy-model outputs the dispatcher reads back. Indexed
 * [gr][ch] / [gr][k], matching pe[gr][ch], pe_MS[gr][ch], tot_ener[gr][0..3]
 * and blocktype[ch]. mp3parity_set_psy fills these per (gr) before the frame;
 * L3psycho_anal_vbr (the stub) copies them into the dispatcher's locals. */
static FLOAT g_psy_pe[2][2];
static FLOAT g_psy_pe_ms[2][2];
static FLOAT g_psy_tot_ener[2][4];
static int   g_psy_blocktype[2][2];
static int   g_psy_ret; /* return value (non-zero aborts the frame with -4) */

/* mdct_sub48 prime capture: when armed (g_capture_arm, set only by the prime
 * tests), the first call — the filterbank prime inside lame_encode_frame_init —
 * records the shifted primebuff so the prime's zero/shift logic can be checked.
 * Disarmed by default so the Stage-2 frame mdct (whose inbuf is whatever the
 * test supplied, possibly NULL when frameInit is pre-latched) is never copied. */
static int   g_mdct_calls;
static float g_prime0[286 + 1152 + 576];
static float g_prime1[286 + 1152 + 576];
static int   g_prime_captured;
static int   g_capture_arm;

int
L3psycho_anal_vbr(lame_internal_flags * gfc,
                  const sample_t *const buffer[2], int gr,
                  III_psy_ratio ratio[2][2],
                  III_psy_ratio MS_ratio[2][2],
                  FLOAT pe[2], FLOAT pe_MS[2], FLOAT ener[2], int blocktype_d[2])
{
    (void) gfc;
    (void) buffer;
    (void) ratio;
    (void) MS_ratio;
    int ch;
    for (ch = 0; ch < 2; ch++) {
        pe[ch] = g_psy_pe[gr][ch];
        pe_MS[ch] = g_psy_pe_ms[gr][ch];
        blocktype_d[ch] = g_psy_blocktype[gr][ch];
    }
    /* ener is tot_ener[gr], a FLOAT[4] (encoder.c:320 FLOAT tot_ener[2][4]). */
    for (ch = 0; ch < 4; ch++)
        ener[ch] = g_psy_tot_ener[gr][ch];
    /* g_psy_ret drives the dispatcher's abort path (encoder.c:378 return -4). */
    if (g_psy_ret != 0)
        return g_psy_ret;
    return 0;
}

void
mdct_sub48(lame_internal_flags * gfc, const sample_t * w0, const sample_t * w1)
{
    (void) gfc;
    if (g_capture_arm && g_mdct_calls == 0 && !g_prime_captured) {
        memcpy(g_prime0, w0, sizeof(g_prime0));
        if (w1 != NULL)
            memcpy(g_prime1, w1, sizeof(g_prime1));
        g_prime_captured = 1;
    }
    g_mdct_calls++;
}

void
CBR_iteration_loop(lame_internal_flags * gfc, const FLOAT pe[2][2],
                   const FLOAT ms_ratio[2], const III_psy_ratio ratio[2][2])
{
    (void) gfc; (void) pe; (void) ms_ratio; (void) ratio;
}

void
ABR_iteration_loop(lame_internal_flags * gfc, const FLOAT pe[2][2],
                   const FLOAT ms_ratio[2], const III_psy_ratio ratio[2][2])
{
    (void) gfc; (void) pe; (void) ms_ratio; (void) ratio;
}

void
VBR_old_iteration_loop(lame_internal_flags * gfc, const FLOAT pe[2][2],
                       const FLOAT ms_ratio[2], const III_psy_ratio ratio[2][2])
{
    (void) gfc; (void) pe; (void) ms_ratio; (void) ratio;
}

void
VBR_new_iteration_loop(lame_internal_flags * gfc, const FLOAT pe[2][2],
                       const FLOAT ms_ratio[2], const III_psy_ratio ratio[2][2])
{
    (void) gfc; (void) pe; (void) ms_ratio; (void) ratio;
}

int
format_bitstream(lame_internal_flags * gfc)
{
    (void) gfc;
    return 0;
}

int
copy_buffer(lame_internal_flags * gfc, unsigned char *buffer, int size, int mp3data)
{
    (void) gfc; (void) buffer; (void) size; (void) mp3data;
    return 0; /* fixed byte count; the dispatcher returns this verbatim */
}

void
AddVbrFrame(lame_internal_flags * gfc)
{
    (void) gfc;
}

/* set_frame_pinfo (quantize_pvt.h) is referenced only inside the
 * `cfg->analysis && gfc->pinfo != NULL` block, which never executes here
 * (analysis stays 0 / pinfo stays NULL — calloc'd gfc). It must still link, so
 * provide an inert definition. */
void
set_frame_pinfo(lame_internal_flags * gfc, const III_psy_ratio ratio[2][2])
{
    (void) gfc; (void) ratio;
}

/* ------------------------------------------------------------------ *
 *  Oracle handle + trampolines.                                       *
 * ------------------------------------------------------------------ */

struct mp3parity_fe_t {
    lame_internal_flags gfc;
    sample_t *inbuf0;
    sample_t *inbuf1;
    int inlen; /* per-channel sample count of inbuf0/inbuf1 */
};

mp3parity_fe_t *
mp3parity_fe_new(void)
{
    mp3parity_fe_t *h = (mp3parity_fe_t *) calloc(1, sizeof(*h));
    /* The dispatcher calls the genuine static adjust_ATH (encoder.c:397), which
     * derefs gfc->ATH->use_adjust. adjust_ATH is NOT part of this slice's
     * arithmetic seam — the Go dispatcher delegates it to Stages.AdjustATH
     * (a no-op stub). To keep the genuine C adjust_ATH inert (matching that
     * no-op) without it crashing on a NULL ATH, give it a calloc'd ATH_t whose
     * use_adjust == 0, so adjust_ATH takes its early-return branch
     * (encoder.c:61) and touches nothing else. */
    h->gfc.ATH = (ATH_t *) calloc(1, sizeof(ATH_t));
    /* lame_encode_frame_init ends with two debug asserts on sv_enc.mf_size
     * (encoder.c:231 / :233): mf_size >= BLKSIZE + framesize - FFTOFFSET and
     * mf_size >= 512 + framesize - 32. These are NDEBUG-gated checks that emit
     * no bytes — the Go port deliberately omits them (frame_encode.go doc) — but
     * the genuine C runs them, so seed mf_size large enough to satisfy both for
     * any mode_gr (framesize <= 1152): 1024 + 1152 - 272 = 1904 is the larger
     * bound, so 4096 clears it with margin. This changes no emitted output. */
    h->gfc.sv_enc.mf_size = 4096;
    return h;
}

void
mp3parity_fe_free(mp3parity_fe_t *h)
{
    if (h == NULL)
        return;
    free(h->gfc.ATH);
    free(h->inbuf0);
    free(h->inbuf1);
    free(h);
}

void
mp3parity_fe_set_cfg(mp3parity_fe_t *h, int samplerate_out, int channels_out,
                     int mode_gr, int mode, int force_ms, int vbr, int write_lame_tag)
{
    SessionConfig_t *cfg = &h->gfc.cfg;
    cfg->samplerate_out = samplerate_out;
    cfg->channels_out = channels_out;
    cfg->mode_gr = mode_gr;
    cfg->mode = (MPEG_mode) mode;
    cfg->force_ms = force_ms;
    cfg->vbr = (vbr_mode) vbr;
    cfg->write_lame_tag = write_lame_tag;
}

void
mp3parity_fe_set_pad(mp3parity_fe_t *h, int frac_spf, int slot_lag)
{
    h->gfc.sv_enc.frac_SpF = frac_spf;
    h->gfc.sv_enc.slot_lag = slot_lag;
}

void
mp3parity_fe_set_pefirbuf(mp3parity_fe_t *h, const float *buf19)
{
    int i;
    for (i = 0; i < 19; i++)
        h->gfc.sv_enc.pefirbuf[i] = (FLOAT) buf19[i];
}

void
mp3parity_fe_set_frame_init(mp3parity_fe_t *h, int v)
{
    h->gfc.lame_encode_frame_init = v;
}

/* Set the fabricated psy-model outputs the stub L3psycho_anal_vbr feeds back. */
void
mp3parity_fe_set_psy(int gr, const float *pe2, const float *pe_ms2,
                     const float *tot_ener4, const int *blocktype2)
{
    int i;
    for (i = 0; i < 2; i++) {
        g_psy_pe[gr][i] = (FLOAT) pe2[i];
        g_psy_pe_ms[gr][i] = (FLOAT) pe_ms2[i];
        g_psy_blocktype[gr][i] = blocktype2[i];
    }
    for (i = 0; i < 4; i++)
        g_psy_tot_ener[gr][i] = (FLOAT) tot_ener4[i];
}

void
mp3parity_fe_set_psy_ret(int ret)
{
    g_psy_ret = ret;
}

void
mp3parity_fe_arm_capture(void)
{
    g_capture_arm = 1;
}

void
mp3parity_fe_reset_capture(void)
{
    g_mdct_calls = 0;
    g_prime_captured = 0;
    g_psy_ret = 0;
    g_capture_arm = 0;
    memset(g_prime0, 0, sizeof(g_prime0));
    memset(g_prime1, 0, sizeof(g_prime1));
}

/* Allocate the input PCM the dispatcher reads. The dispatcher reads
 * inbuf[ch][576 + gr*576 - FFTOFFSET ...] for the psy window and inbuf[ch][..]
 * for mdct; the test supplies whatever it needs. inlen is the per-channel
 * length; values are filled by mp3parity_fe_set_input. */
void
mp3parity_fe_alloc_input(mp3parity_fe_t *h, int inlen)
{
    free(h->inbuf0);
    free(h->inbuf1);
    h->inlen = inlen;
    h->inbuf0 = (sample_t *) calloc((size_t) inlen, sizeof(sample_t));
    h->inbuf1 = (sample_t *) calloc((size_t) inlen, sizeof(sample_t));
}

void
mp3parity_fe_set_input(mp3parity_fe_t *h, int ch, const float *vals, int n)
{
    sample_t *dst = (ch == 0) ? h->inbuf0 : h->inbuf1;
    int i;
    for (i = 0; i < n && i < h->inlen; i++)
        dst[i] = (sample_t) vals[i];
}

/* Run the genuine vendored dispatcher over the configured handle, returning its
 * mp3count return value. mp3buf is a scratch output buffer (copy_buffer is
 * stubbed, so its contents are irrelevant). */
int
mp3parity_fe_encode(mp3parity_fe_t *h)
{
    static unsigned char mp3buf[16384];
    return lame_encode_mp3_frame(&h->gfc, h->inbuf0, h->inbuf1, mp3buf, (int) sizeof(mp3buf));
}

/* Read-back accessors for the dispatcher's observable outputs. */
int   mp3parity_fe_padding(const mp3parity_fe_t *h)      { return h->gfc.ov_enc.padding; }
int   mp3parity_fe_slot_lag(const mp3parity_fe_t *h)     { return h->gfc.sv_enc.slot_lag; }
int   mp3parity_fe_mode_ext(const mp3parity_fe_t *h)     { return h->gfc.ov_enc.mode_ext; }
int   mp3parity_fe_frame_number(const mp3parity_fe_t *h) { return h->gfc.ov_enc.frame_number; }
int   mp3parity_fe_frame_init(const mp3parity_fe_t *h)   { return h->gfc.lame_encode_frame_init; }

float mp3parity_fe_pefirbuf(const mp3parity_fe_t *h, int i)
{
    return (float) h->gfc.sv_enc.pefirbuf[i];
}

int   mp3parity_fe_block_type(const mp3parity_fe_t *h, int gr, int ch)
{
    return h->gfc.l3_side.tt[gr][ch].block_type;
}

int   mp3parity_fe_mixed_block_flag(const mp3parity_fe_t *h, int gr, int ch)
{
    return h->gfc.l3_side.tt[gr][ch].mixed_block_flag;
}

/* mdct prime-capture read-back (the shifted primebuff lame_encode_frame_init
 * fed to mdct_sub48 on the first frame). */
int   mp3parity_fe_mdct_calls(void)         { return g_mdct_calls; }
float mp3parity_fe_prime0(int i)            { return g_prime0[i]; }
float mp3parity_fe_prime1(int i)            { return g_prime1[i]; }