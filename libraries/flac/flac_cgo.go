//go:build cgo

package flac

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/libflac
#cgo CFLAGS: -I${SRCDIR}/libflac/include
#cgo CFLAGS: -I${SRCDIR}/libflac/src/libFLAC/include
// -O2 intentionally omitted: env-provided CGO_CFLAGS is expected to
// carry the full optimisation/vectorisation flag set in the order the
// project's parity infrastructure expects (matching libraries/opus's
// flac_cgo.go counterpart). A trailing -O2 here would let clang re-
// order flags and reintroduce vectorisation.
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable
// libFLAC's bitwriter.h declares non-static `inline` functions that
// reference the file-local helper `bitwriter_grow_`; clang flags this
// as -Wstatic-in-inline. The pattern is benign — the inline functions
// are only included from bitwriter.c — but clean up the noise.
#cgo CFLAGS: -Wno-static-in-inline

#include <FLAC/stream_decoder.h>
#include <FLAC/stream_encoder.h>
#include <FLAC/format.h>
#include <FLAC/metadata.h>
*/
import "C"
