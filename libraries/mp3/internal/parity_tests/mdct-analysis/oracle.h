/*
 * oracle.h — C oracle surface for the mdct-analysis parity slice.
 *
 * The floating-point heart of LAME 3.100's encoder analysis front end is the
 * polyphase analysis filterbank + MDCT in libmp3lame/newmdct.c:
 * window_subband (newmdct.c:430) applies the 32-band overlapping analysis
 * window and Takehiro's fast IDCT to 32 fresh PCM samples concatenated onto the
 * look-behind history, producing 32 subband samples; mdct_short (newmdct.c:832)
 * and mdct_long (newmdct.c:869) then run the 6-line / 18-line MDCTs that turn
 * windowed subband samples into the 32x18 = 576 MDCT lines the quantizer
 * consumes. These three kernels carry every float multiply/add of the slice, so
 * they are the bit-exactness pins for the "mdct-analysis" area.
 *
 * All three are `inline static` inside newmdct.c, so there is no public C API
 * that reaches them. Per the parity discipline in
 * CONTRIBUTING.md this oracle TU (oracle.c) #includes
 * the vendored libmp3lame/newmdct.c directly — bringing the statics into scope
 * — and re-exports them through the thin oracle_* wrappers declared below. Each
 * wrapper lives in the same translation unit as the static and adds no logic,
 * so the C side of every assertion is the genuine vendored LAME code, not a
 * hand reimplementation.
 *
 * Including newmdct.c pulls in the whole LAME header tree (lame.h, machine.h,
 * encoder.h, util.h, l3side.h), so FLOAT / sample_t / SBLIMIT are defined for
 * us; the wrapper signatures use float* so the Go cgo bridge needs none of
 * those headers. FLOAT and sample_t are both `float` per liblame/config.h
 * (SIZEOF_FLOAT 4), so `float *` is ABI-identical to `FLOAT *` / `sample_t *`.
 *
 * Inputs are fabricated directly as the raw FLOAT buffers each kernel operates
 * on, with no lame_internal_flags involved (only mdct_sub48, NOT wrapped here,
 * reaches through gfc): the parity test fills the buffers with random floats
 * and runs both the C oracle_* and the Go nativemp3.{WindowSubband,MdctShort,
 * MdctLong} over identical bytes, asserting the result is bit-identical.
 *
 * The flat driver mdct_sub48 (newmdct.c:944) is intentionally NOT re-exported:
 * it is pure plumbing (loop nest + memset/memcpy + the per-band amp-filter,
 * short-block pre-twiddle, long-block pre-twiddle, and aliasing-reduction
 * butterfly) layered on top of these three FP kernels, and exercising it would
 * require fabricating a full lame_internal_flags (cfg + sv_enc + l3_side). Its
 * float arithmetic is entirely composed of the three pinned kernels plus the
 * trivial twiddle/butterfly expressions the strict Go build rounds the same
 * way; pinning the kernels is the bit-exact contract for the slice.
 */
#ifndef MP3_MDCT_ANALYSIS_ORACLE_H
#define MP3_MDCT_ANALYSIS_ORACLE_H

/* oracle_window_subband runs the vendored static window_subband over the PCM
 * window buffer x1_base. base is the index of the C `x1[0]` cursor: the kernel
 * reads x1_base[base-286 .. base+256] (the 286-sample look-behind history plus
 * the 32 fresh samples and their windowed reach) and writes 32 subband samples
 * into a[0..31]. float* is ABI-identical to LAME's sample_t* / FLOAT*. */
void oracle_window_subband(float *x1_base, int base, float *a);

/* oracle_mdct_short runs the vendored static mdct_short in place over the
 * 18-line inout buffer (three short-block 6-line MDCTs). */
void oracle_mdct_short(float *inout);

/* oracle_mdct_long runs the vendored static mdct_long, reading 18 windowed
 * inputs from in and writing 18 MDCT lines to out. */
void oracle_mdct_long(float *out, const float *in);

#endif /* MP3_MDCT_ANALYSIS_ORACLE_H */
