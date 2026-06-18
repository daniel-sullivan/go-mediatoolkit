/*
 * oracle.h — C oracle surface for the vbrquantize-leaf parity slice.
 *
 * This slice pins the floating-point leaf kernels of LAME 3.100's VBR
 * (vbr_mtrh / -V) quantizer translation unit libmp3lame/vbrquantize.c:
 *
 *   - vec_max_c            (vbrquantize.c:116, inline static) — band max xr34.
 *   - find_lowest_scalefac (:148, inline static) — lowest in-range scalefactor.
 *   - k_34_4               (:169, inline static) — the TAKEHIRO_IEEE754_HACK
 *                           magic-float quantize of 4 coefficients to integers.
 *   - calc_sfb_noise_x34   (:218, static) — quantization-noise energy for a
 *                           scalefactor over one band.
 *   - tri_calc_sfb_noise_x34 (:278, static) — sf / sf+-1 distortion test.
 *   - calc_scalefac        (:317, static) — log10-based step-size estimate.
 *   - guess_scalefac_x34   (:324, static) — clamped calc_scalefac.
 *   - find_scalefac_x34    (:347, static) — binary-search the largest
 *                           distortion-free scalefactor.
 *
 * Every one is file-static (or inline static) inside vbrquantize.c, so — per the
 * parity discipline in CONTRIBUTING.md — oracle.c
 * #includes the committed libmp3lame/vbrquantize.c directly (bringing the
 * statics into scope) and re-exports them through thin oracle_* trampolines.
 * Each trampoline lives in the same translation unit as the static and adds no
 * logic, so the C side of every assertion is the genuine vendored LAME code, not
 * a hand reimplementation.
 *
 * TABLE FILL. The kernels read four file-global precompute tables that
 * vbrquantize.c declares `extern` and iteration_init (quantize_pvt.c) fills:
 * pow20[], ipow20[], pow43[] and — because the vendored config.h defines
 * TAKEHIRO_IEEE754_HACK — adj43asm[]. oracle.c defines these and fills them with
 * a verbatim copy of iteration_init's table-fill loop (quantize_pvt.c:351-367,
 * the TAKEHIRO_IEEE754_HACK branch), documented and bounds-asserted against the
 * genuine #define sizes in oracle.c. The Go side mirrors this with
 * nativemp3.FillVbrQuantizeTables (InitQuantizePvtTables + InitVbrQuantizeTables),
 * which runs the same loop. The FLOAT type is `float` per liblame's config.h
 * (SIZEOF_FLOAT 4), so float* is ABI-identical to FLOAT*.
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS (cgo.go).
 * The FP-determinism flags (-ffp-contract=off, -fno-vectorize, -fno-slp-
 * vectorize, -fno-unroll-loops) come from the mise task env so every multiply /
 * add / the magic-float add round separately, matching the FMA-free mp3_strict
 * Go build. Go's cgo flag allowlist rejects -ffp-contract=off in source.
 *
 * LGPL note: vbrquantize.c is LGPL LAME source, so this oracle TU and the Go
 * port it pins are gated by the mp3lame build tag (in addition to cgo), exactly
 * like the VBR slice they test. A bare `go test` never compiles them.
 */
#ifndef MP3_VBRQUANTIZE_LEAF_ORACLE_H
#define MP3_VBRQUANTIZE_LEAF_ORACLE_H

/* oracle_fill_tables fills pow20 / ipow20 / pow43 / adj43asm via the verbatim
 * iteration_init table-fill loop (TAKEHIRO_IEEE754_HACK branch). Idempotent. */
void oracle_fill_tables(void);

/* oracle_vec_max_c forwards to the genuine inline-static vec_max_c. */
float oracle_vec_max_c(const float *xr34, unsigned int bw);

/* oracle_find_lowest_scalefac forwards to the genuine find_lowest_scalefac. */
unsigned char oracle_find_lowest_scalefac(float xr34);

/* oracle_k_34_4 forwards to the genuine k_34_4: quantizes x[0..3] to l3[0..3]
 * via the TAKEHIRO_IEEE754_HACK. x is double[4] (DOUBLEX). */
void oracle_k_34_4(double *x /*4*/, int *l3 /*4*/);

/* oracle_calc_sfb_noise_x34 forwards to the genuine calc_sfb_noise_x34. */
float oracle_calc_sfb_noise_x34(const float *xr, const float *xr34,
                                unsigned int bw, unsigned char sf);

/* oracle_tri_calc_sfb_noise_x34 forwards to the genuine tri_calc_sfb_noise_x34
 * with a fresh 256-entry calc_noise_cache (seeded internally). */
unsigned char oracle_tri_calc_sfb_noise_x34(const float *xr, const float *xr34,
                                            float l3_xmin, unsigned int bw,
                                            unsigned char sf);

/* oracle_calc_scalefac forwards to the genuine calc_scalefac. */
int oracle_calc_scalefac(float l3_xmin, int bw);

/* oracle_guess_scalefac_x34 forwards to the genuine guess_scalefac_x34. */
unsigned char oracle_guess_scalefac_x34(const float *xr, const float *xr34,
                                        float l3_xmin, unsigned int bw,
                                        unsigned char sf_min);

/* oracle_find_scalefac_x34 forwards to the genuine find_scalefac_x34. */
unsigned char oracle_find_scalefac_x34(const float *xr, const float *xr34,
                                       float l3_xmin, unsigned int bw,
                                       unsigned char sf_min);

#endif /* MP3_VBRQUANTIZE_LEAF_ORACLE_H */
