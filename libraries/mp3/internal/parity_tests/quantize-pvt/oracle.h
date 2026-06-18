/*
 * oracle.h — C oracle surface for the quantize-pvt parity slice.
 *
 * This slice pins the floating-point heart of LAME 3.100's quantizer-support
 * translation unit libmp3lame/quantize_pvt.c:
 *
 *   - athAdjust       (quantize_pvt.c:555) — the ATH noise-floor adjust.
 *   - ATHmdct         (quantize_pvt.c:211, file-static) — per-frequency ATH in
 *                      MDCT-domain energy (ATHformula + dB offset + powf).
 *   - compute_ath     (quantize_pvt.c:231, file-static) — fills the per-sfb ATH
 *                      energy floors (ATH->l / psfb21 / s / psfb12 / floor).
 *   - calc_xmin       (quantize_pvt.c:590) — the per-band allowed-distortion
 *                      budget xmin = ratio*en/bw clamped to the ATH, plus the
 *                      energy_above_cutoff flags and max_nonzero_coeff.
 *   - calc_noise      (quantize_pvt.c:816) + calc_noise_core_c (:751) — the
 *                      per-band quantization-noise measure against xmin.
 *
 * These carry every float multiply/add of the slice (the psfb/en/thm energy
 * sums and the masking ratios named in the porting task), so they are the
 * bit-exactness pins for the "quantize-pvt" area.
 *
 * athAdjust / calc_xmin / calc_noise are PUBLIC (declared in quantize_pvt.h),
 * so the oracle calls them directly. ATHmdct / compute_ath / calc_noise_core_c
 * are file-static inside quantize_pvt.c, so — per the parity discipline in
 * CONTRIBUTING.md — oracle.c #includes the committed
 * libmp3lame/quantize_pvt.c directly (bringing the statics into scope) and
 * re-exports them through the thin oracle_* trampolines declared below. Each
 * trampoline lives in the same translation unit as the static and adds no
 * logic, so the C side of every assertion is the genuine vendored LAME code,
 * not a hand reimplementation. oracle.c also #includes util.c so ATHformula
 * (which ATHmdct/compute_ath call) is the genuine vendored formula, and stubs
 * the two extern table-init callees (huffman_init / init_xrpow_core_init) that
 * iteration_init reaches in other TUs but whose bodies this slice does not
 * exercise.
 *
 * INPUT FABRICATION. The genuine kernels read a lame_internal_flags (cfg + ATH
 * + sv_qnt + scalefac_band + cd_psy), a gr_info and an III_psy_ratio. Rather
 * than reach through LAME's full init, the oracle builds those real LAME structs
 * field-by-field from flat input arrays the parity test fills (the same flat
 * arrays the Go nativemp3 port is driven over), so the two sides operate on
 * byte-identical inputs. The pow43[] / pow20[] file globals calc_noise indexes
 * are populated by oracle_fill_tables, which calls the GENUINE iteration_init
 * (its pow43/pow20/ipow20/adj43 table-fill loops run for real; the stubbed
 * huffman_init / init_xrpow_core_init are no-ops). The Go side mirrors this with
 * nativemp3.InitQuantizePvtTables. The FLOAT type is `float` per liblame's
 * config.h (SIZEOF_FLOAT 4), so float* is ABI-identical to FLOAT*.
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS
 * (cgo.go). The FP-determinism flags (-ffp-contract=off, -fno-vectorize,
 * -fno-slp-vectorize, -fno-unroll-loops) come from the mise task env so the
 * energy sums / masking ratios / ATH products round separately, matching the
 * FMA-free mp3_strict Go build. Go's cgo flag allowlist rejects
 * -ffp-contract=off in source, so they are NOT placed here.
 *
 * LGPL note: quantize_pvt.c / util.c are LGPL LAME source, so this oracle TU and
 * the Go port it pins are gated by the mp3lame build tag (in addition to cgo),
 * exactly like the quantizer slice they test. A bare `go test` never compiles
 * them.
 */
#ifndef MP3_QUANTIZE_PVT_ORACLE_H
#define MP3_QUANTIZE_PVT_ORACLE_H

/* Block-type / sfb-count constants the flat I/O mirrors size against. Mirrors
 * encoder.h SBMAX_l / SBMAX_s / PSFB21 / PSFB12 and l3side.h SFBMAX; asserted
 * equal to the genuine LAME values in oracle.c. */
#define ORC_SBMAX_L 22
#define ORC_SBMAX_S 13
#define ORC_PSFB21  6
#define ORC_PSFB12  6
#define ORC_SFBMAX  (ORC_SBMAX_S * 3)

/* oracle_fill_tables runs the genuine iteration_init table-fill (pow43 / pow20 /
 * ipow20 / adj43) so the calc_noise pow43[]/pow20[] lookups resolve. It builds a
 * throwaway lame_internal_flags with just the cfg / scalefac_band / ATH that
 * iteration_init's compute_ath touches; the table fill is the genuine vendored
 * loop. Idempotent-guarded by LAME's own iteration_init_init latch, so the Go
 * test calls it once. samplerate_out and the long/short scalefac-band boundary
 * arrays (sbL[ORC_SBMAX_L+1], sbS[ORC_SBMAX_S+1]) seed compute_ath. */
void oracle_fill_tables(int samplerate_out, int athtype, float athcurve,
                        float ath_offset_db, float athfixpoint, int noath,
                        const int *sbL, const int *sbS,
                        const int *psfb21, const int *psfb12,
                        const float *adjust_long /*4*/, const float *adjust_short /*4*/);

/* oracle_athadjust forwards to the genuine public athAdjust. */
float oracle_athadjust(float a, float x, float athFloor, float athFixpoint);

/* oracle_athmdct forwards to the genuine file-static ATHmdct over a cfg built
 * from the flat ATH parameters (samplerate_out is unused by ATHmdct itself but
 * carried for symmetry). */
float oracle_athmdct(int athtype, float athcurve, float ath_offset_db,
                     float athfixpoint, float f);

/* oracle_compute_ath forwards to the genuine file-static compute_ath, building a
 * lame_internal_flags from the flat cfg + scalefac_band, and copies the four ATH
 * floor arrays (l / psfb21 / s / psfb12) and ATH->floor back out. */
void oracle_compute_ath(int samplerate_out, int athtype, float athcurve,
                        float ath_offset_db, float athfixpoint, int noath,
                        const int *sbL, const int *sbS,
                        const int *psfb21, const int *psfb12,
                        float *out_l /*ORC_SBMAX_L*/, float *out_psfb21 /*ORC_PSFB21*/,
                        float *out_s /*ORC_SBMAX_S*/, float *out_psfb12 /*ORC_PSFB12*/,
                        float *out_floor /*1*/);

/* oracle_calc_xmin forwards to the genuine public calc_xmin. It builds a
 * lame_internal_flags (ATH adjust_factor/floor/l/s, sv_qnt longfact/shortfact,
 * cd_psy decay, cfg ATHfixpoint/samplerate_out/sfb21_extra/use_temporal_masking
 * + scalefac_band) and a gr_info (xr, width, psy_lmax, psymax, sfb_smin,
 * block_type) and an III_psy_ratio (en/thm long + short) from the flat inputs,
 * then runs calc_xmin and copies the pxmin[] budget, the energy_above_cutoff[]
 * flags and cod_info->max_nonzero_coeff back out. Returns calc_xmin's ath_over.
 *
 * The ATH long/short arrays are passed pre-adjusted? No — calc_xmin itself calls
 * athAdjust on ATH->l/s, so the raw ATH->l/s/adjust_factor/floor are supplied.
 */
int oracle_calc_xmin(
    /* cfg */
    int samplerate_out, float athfixpoint, int sfb21_extra, int use_temporal,
    /* scalefac_band */
    const int *sbL, const int *sbS,
    /* ATH */
    float ath_adjust_factor, float ath_floor, const float *ath_l, const float *ath_s,
    /* sv_qnt */
    const float *longfact /*ORC_SBMAX_L*/, const float *shortfact /*ORC_SBMAX_S*/,
    /* cd_psy */
    float decay,
    /* gr_info */
    const float *xr /*576*/, const int *width /*ORC_SFBMAX*/,
    int psy_lmax, int psymax, int sfb_smin, int block_type,
    /* ratio: en/thm long (ORC_SBMAX_L) and short (ORC_SBMAX_S*3, [sfb][b] row-major) */
    const float *en_l, const float *thm_l, const float *en_s, const float *thm_s,
    /* outputs */
    float *out_xmin /*ORC_SFBMAX*/, signed char *out_eac /*ORC_SFBMAX*/,
    int *out_max_nonzero);

/* oracle_calc_noise forwards to the genuine public calc_noise (prev_noise = NULL,
 * matching the set_pinfo call site and the simplest parity surface). It builds a
 * gr_info from the flat side-info + xr + l3_enc and runs calc_noise over the
 * supplied l3_xmin, copying distort[] and the calc_noise_result statistics out.
 * Returns calc_noise's over count. */
int oracle_calc_noise(
    /* gr_info */
    const float *xr /*576*/, const int *l3_enc /*576*/, const int *scalefac /*ORC_SFBMAX*/,
    const int *width /*ORC_SFBMAX*/, const int *window /*ORC_SFBMAX*/,
    const int *subblock_gain /*4*/, int global_gain, int scalefac_scale,
    int preflag, int psymax, int max_nonzero_coeff, int count1, int big_values,
    /* xmin */
    const float *l3_xmin /*ORC_SFBMAX*/,
    /* outputs */
    float *out_distort /*ORC_SFBMAX*/,
    float *out_over_noise, float *out_tot_noise, float *out_max_noise,
    int *out_over_count, int *out_over_ssd);

#endif /* MP3_QUANTIZE_PVT_ORACLE_H */
