/* Portable config.h for inlined libFLAC build via Go/Cgo.
 *
 * Compiled from libraries/flac/flac_cgo_src.c (the amalgamation
 * translation unit) when a Go file in libraries/flac contains
 * `import "C"`.
 *
 * Default build: SCALAR baseline. SIMD intrinsics are disabled so
 * the cgo path can serve as a bit-exact oracle for the future
 * `internal/nativeflac` Go port (mirrors libraries/opus's scalar
 * default; SIMD is gated behind a separate build mode).
 */

#ifndef FLAC_CONFIG_H_INLINE
#define FLAC_CONFIG_H_INLINE

/* Version identification consumed by libFLAC sources. */
#define PACKAGE_VERSION "1.5.0"

/* libFLAC builds 64-bit bitreader words on every modern target we
 * support. Using 64-bit words is faster and matches upstream's
 * default for 64-bit builds. */
#define ENABLE_64_BIT_WORDS 1

/* Ogg-FLAC encapsulation lives in containers/ogg, not in libFLAC. */
#define FLAC__HAS_OGG 0

/* Standard C library headers — all guaranteed by C99. */
#define HAVE_STDINT_H 1
#define HAVE_INTTYPES_H 1
#define HAVE_STDLIB_H 1
#define HAVE_STRING_H 1
#define HAVE_SYS_TYPES_H 1

#if defined(__unix__) || defined(__APPLE__)
#define HAVE_UNISTD_H 1
#define HAVE_FSEEKO 1
#define HAVE_LROUND 1
#endif

/* MinGW (the toolchain GitHub's windows-latest Go runner uses for cgo)
 * ships POSIX fseeko/ftello and C99 lround. Advertise them so compat.h
 * does not fall into its `#define fseeko fseeko64` fallback (which
 * collides with mingw-w64's own __mingw_ovr fseeko64 inline) and so
 * lpc.c does not declare a private `lround` that conflicts with math.h.
 * MSVC builds (no __MINGW32__) keep using compat.h's _fseeki64 mapping. */
#if defined(__MINGW32__)
#define HAVE_FSEEKO 1
#define HAVE_LROUND 1
#endif

/* Platform identification. */
#if defined(__APPLE__)
#define FLAC__SYS_DARWIN
#endif
#if defined(__linux__)
#define FLAC__SYS_LINUX
#endif

/* CPU identification (preprocessor-only — libFLAC's cpu.h also
 * derives FLAC__CPU_X86_64 / FLAC__CPU_IA32 from compiler macros,
 * but we set FLAC__CPU_ARM64 here because the upstream config does). */
#if defined(__aarch64__) || defined(_M_ARM64)
#define FLAC__CPU_ARM64
#endif

/* Endianness. */
#if defined(__BIG_ENDIAN__) || \
    (defined(__BYTE_ORDER__) && __BYTE_ORDER__ == __ORDER_BIG_ENDIAN__)
#define CPU_IS_BIG_ENDIAN 1
#else
#define CPU_IS_BIG_ENDIAN 0
#endif

/* Compiler intrinsics for byte swapping (gcc/clang). */
#if defined(__GNUC__) || defined(__clang__)
#define HAVE_BSWAP16 1
#define HAVE_BSWAP32 1
#endif

/* No SIMD intrinsics in the scalar baseline build.
 *
 * libFLAC's intrinsic translation units are still in the amalgamation
 * but each top-level kernel is gated on FLAC__HAS_X86INTRIN /
 * FLAC__HAS_NEONINTRIN, so leaving these undefined yields stub TUs.
 * A future build mode (FLAC_PRODUCTION_C, mirroring OPUS_PRODUCTION_C)
 * can flip them on for measuring against a realistic libFLAC build.
 */
/* #undef FLAC__HAS_X86INTRIN */
/* #undef FLAC__HAS_NEONINTRIN */
/* #undef FLAC__HAS_A64NEONINTRIN */
/* #undef FLAC__USE_AVX */

/* No runtime CPU detection — the scalar paths are fully self-contained. */

#endif /* FLAC_CONFIG_H_INLINE */
