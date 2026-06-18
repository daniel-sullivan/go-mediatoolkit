/* Portable config.h for inlined Opus build via Go/Cgo.
 *
 * Uses compile-time arch detection to enable SIMD intrinsics
 * where they are part of the baseline ISA:
 *   - AArch64: NEON is mandatory, always enabled
 *   - x86_64:  SSE/SSE2 are mandatory, always enabled
 *   - Other:   generic C fallback
 *
 * No runtime CPU detection (RTCD) is used — only intrinsics
 * that are guaranteed available for the target architecture.
 */

/* This is a build of OPUS — also set via -DOPUS_BUILD in cgo CFLAGS */
#ifndef OPUS_BUILD
#define OPUS_BUILD
#endif

/* Hardening (bounds checks in the range coder) */
#define ENABLE_HARDENING 1

/* 24-bit internal resolution for fixed-point */
#define ENABLE_RES24 1

/* Float approximations */
#define FLOAT_APPROX 1

/* Disable DNN debug float */
#define DISABLE_DEBUG_FLOAT 1

/* Use C99 variable-length arrays */
#define VAR_ARRAYS 1

/* Standard C library headers */
#define HAVE_STDINT_H 1
#define HAVE_INTTYPES_H 1
#define HAVE_STDIO_H 1
#define HAVE_STDLIB_H 1
#define HAVE_STRING_H 1
#define HAVE_STRINGS_H 1
#define HAVE_LRINT 1
#define HAVE_LRINTF 1

#if defined(__unix__) || defined(__APPLE__)
#define HAVE_DLFCN_H 1
#define HAVE_UNISTD_H 1
#define HAVE_SYS_TYPES_H 1
#define HAVE_SYS_STAT_H 1
#endif

/* restrict keyword */
#if defined(__GNUC__) || defined(__clang__)
#define restrict __restrict
#elif defined(_MSC_VER)
#define restrict __restrict
#endif

/* ── Platform-specific SIMD ────────────────────────────────────────── */

/* Two build modes, selected by the `OPUS_PRODUCTION_C` preprocessor
 * macro (which in turn is driven by the Go build tag
 * `opus_production_c` via opus_cgo_production.go).
 *
 *   default (macro undefined) — SCALAR ORACLE
 *     NEON/SSE intrinsics DISABLED. Used by the bit-exact parity suite
 *     (`parity:benchcmp`, `parity:blackbox`, `parity:threeway`) to
 *     compare bit-for-bit against the Go opus_strict build. The scalar
 *     paths avoid 1-ULP divergences from:
 *       - vcvtaq_s32_f32 rounding ties-away-from-zero vs scalar
 *         lrintf's ties-to-even
 *       - vfmaq/vmlaq fusing a*b+c with a single rounding step
 *     These deltas propagate through pitch picker / mode selection /
 *     NSQ decision trees and can diverge whole packets.
 *
 *   OPUS_PRODUCTION_C defined — FULL-FAT ORACLE
 *     NEON intrinsics enabled on aarch64, SSE/SSE2 enabled on x86_64.
 *     Used by the `bench:production` task to measure Go against a
 *     realistic libopus build. Output is within PSNR noise of the
 *     scalar oracle (≥ 100 dB on the test vectors) but not bit-exact.
 *     Never used by parity tests.
 */

#ifdef OPUS_PRODUCTION_C
#  if defined(__aarch64__) || defined(_M_ARM64)
#    define OPUS_ARM_PRESUME_NEON 1
#    define OPUS_ARM_PRESUME_NEON_INTR 1
#    define OPUS_ARM_MAY_HAVE_NEON 1
#    define OPUS_ARM_MAY_HAVE_NEON_INTR 1
#  endif
#  if defined(__x86_64__) || defined(_M_X64)
#    define OPUS_X86_PRESUME_SSE 1
#    define OPUS_X86_PRESUME_SSE2 1
#    define OPUS_X86_MAY_HAVE_SSE 1
#    define OPUS_X86_MAY_HAVE_SSE2 1
/* SSE4.1 is widely baseline on x86_64 today but not strictly mandated.
 * Enable by default for production builds; drop if targeting older CPUs. */
#    define OPUS_X86_PRESUME_SSE4_1 1
#    define OPUS_X86_MAY_HAVE_SSE4_1 1
#  endif
#endif

/* No runtime CPU detection — we only use what the ISA guarantees. */
/* #undef OPUS_HAVE_RTCD */

/* No fixed-point — use floating-point SILK */
/* #undef FIXED_POINT */

/* ── Explicitly disabled features ──────────────────────────────────── */

/* No optional codec features */
/* #undef ENABLE_DEEP_PLC */
/* #undef ENABLE_DRED */
/* #undef ENABLE_OSCE */
/* #undef ENABLE_LOSSGEN */
/* #undef ENABLE_QEXT */
/* #undef CUSTOM_MODES */
/* #undef DISABLE_FLOAT_API */
