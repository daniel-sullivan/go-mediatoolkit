/* config.h — scalar-baseline LAME 3.100 configuration for the cgo build.
 *
 * Hand-written (not autoconf-generated) to give the vendored LAME a
 * portable, vectorisation-free baseline, mirroring libraries/flac/libflac/
 * config.h. No NASM/MMX/SSE asm, no analysis plug-in, no libsndfile, no
 * decode-on-the-fly. The encoder + the in-tree mpglib decoder are built
 * purely from the committed C. Targets a modern clang/gcc on a 64-bit
 * (LP64) host with <stdint.h>.
 */
#ifndef LAME_CONFIG_H
#define LAME_CONFIG_H

/* Standard headers — all present on every platform we build cgo on. */
#define HAVE_ERRNO_H 1
#define HAVE_FCNTL_H 1
#define HAVE_INTTYPES_H 1
#define HAVE_LIMITS_H 1
#define HAVE_MEMORY_H 1
#define HAVE_STDINT_H 1
#define HAVE_STDLIB_H 1
#define HAVE_STRINGS_H 1
#define HAVE_STRING_H 1
#define HAVE_SYS_STAT_H 1
#define HAVE_SYS_TYPES_H 1
#define HAVE_UNISTD_H 1
#define STDC_HEADERS 1

/* Library routines. */
#define HAVE_GETTIMEOFDAY 1
#define HAVE_STRTOL 1

/* Fixed-width integer types come from <stdint.h>. Declaring the A_*INT*_T
 * pairs makes util.h adopt the stdint typedefs rather than guessing. */
#define HAVE_INT8_T 1
#define HAVE_INT16_T 1
#define HAVE_INT32_T 1
#define HAVE_INT64_T 1
#define HAVE_UINT8_T 1
#define HAVE_UINT16_T 1
#define HAVE_UINT32_T 1
#define HAVE_UINT64_T 1
#define A_INT32_T  int32_t
#define A_UINT32_T uint32_t
#define A_INT64_T  int64_t
#define A_UINT64_T uint64_t

/* LAME's float typedefs. Upstream config.h emits these directly (the
 * HAVE_IEEE754_FLOAT32_T autoconf check is informational); util.h and the
 * encoder rely on them. */
#ifndef HAVE_IEEE754_FLOAT32_T
	typedef float ieee754_float32_t;
#endif
#ifndef HAVE_IEEE754_FLOAT64_T
	typedef double ieee754_float64_t;
#endif

/* Type sizes on an LP64 host. */
#define SIZEOF_SHORT 2
#define SIZEOF_UNSIGNED_SHORT 2
#define SIZEOF_INT 4
#define SIZEOF_UNSIGNED_INT 4
#define SIZEOF_LONG 8
#define SIZEOF_UNSIGNED_LONG 8
#define SIZEOF_LONG_LONG 8
#define SIZEOF_UNSIGNED_LONG_LONG 8
#define SIZEOF_FLOAT 4
#define SIZEOF_DOUBLE 8
#define SIZEOF_LONG_DOUBLE 16

/* Package identity. */
#define PACKAGE "lame"
#define PACKAGE_NAME "lame"
#define PACKAGE_TARNAME "lame"
#define PACKAGE_VERSION "3.100"
#define PACKAGE_STRING "lame 3.100"
#define PACKAGE_BUGREPORT "lame-dev@lists.sourceforge.net"
#define PACKAGE_URL ""
#define VERSION "3.100"

/* Build the library, with the in-tree mpglib decoder available, but no
 * decode-on-the-fly in the encoder path and no analysis hooks. */
#define LAME_LIBRARY_BUILD 1
#define HAVE_MPGLIB 1
#define NOANALYSIS 1

/* The IEEE-754 fast int round hack in takehiro.c is portable on every
 * little-endian IEEE host clang/gcc target; enable it for the standard
 * LAME quantisation path. */
#define TAKEHIRO_IEEE754_HACK 1

#endif /* LAME_CONFIG_H */
