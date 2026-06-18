/* Amalgamation file for the vendored libsamplerate build.
 *
 * Compiling all sources as a single translation unit (rather than as
 * separate .c files) makes the cgo invocation simpler and matches the
 * pattern we use for the vendored libopus build under
 * libraries/opus/opus_cgo_celt.c.
 *
 * cgo only feeds this file to the C compiler when a Go file in this
 * package contains `import "C"` — that gate is provided by
 * cgo_converter.go (`//go:build cgo`).
 */

#include "libsamplerate/src/samplerate.c"
#include "libsamplerate/src/src_linear.c"
#include "libsamplerate/src/src_sinc.c"
#include "libsamplerate/src/src_zoh.c"
