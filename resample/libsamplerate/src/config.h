/*
 * Minimal config.h for the vendored libsamplerate build.
 *
 * Upstream generates this via CMake / autoconf at install time. We
 * hand-author it instead so the source compiles cleanly under cgo
 * without a separate configure step. Targets POSIX (macOS/Linux) and
 * little-endian x86_64/arm64; no FFTW, ALSA, sndfile, or other optional
 * dependencies are pulled in — we use only the public src_simple /
 * src_process API surface for parity testing and the optional cgo
 * fallback converter.
 */

#ifndef GO_AUDIOTOOLKIT_LIBSAMPLERATE_CONFIG_H
#define GO_AUDIOTOOLKIT_LIBSAMPLERATE_CONFIG_H

#define VERSION "0.2.2"
#define PACKAGE "libsamplerate"

/* All converters enabled — parity tests cover all five. */
#define ENABLE_SINC_FAST_CONVERTER 1
#define ENABLE_SINC_MEDIUM_CONVERTER 1
#define ENABLE_SINC_BEST_CONVERTER 1

/* Standard C library features available on every supported platform. */
#define HAVE_STDINT_H 1
#define HAVE_STDBOOL_H 1
#define HAVE_LRINT 1
#define HAVE_LRINTF 1
#define HAVE_UNISTD_H

/* Endianness — Go targets x86_64 and arm64, both little-endian. */
#define CPU_IS_LITTLE_ENDIAN 1
#define CPU_IS_BIG_ENDIAN 0

/*
 * IEEE 754 floats clip on conversion to int — true for all sane modern
 * CPUs. libsamplerate uses these to pick fast paths in float→int casts.
 */
#define CPU_CLIPS_NEGATIVE 1
#define CPU_CLIPS_POSITIVE 1

#endif
