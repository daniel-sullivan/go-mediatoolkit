//go:build cgo

// CFLAGS preamble for the vendored libsamplerate build. Sister files
// in this package (cgo_converter.go) include <samplerate.h> directly;
// this file owns the cgo CFLAGS so they live in one place. Mirrors the
// libraries/opus/opus_cgo.go pattern.

package resample

// #cgo CFLAGS: -DHAVE_CONFIG_H
// #cgo LDFLAGS: -lm
// #cgo CFLAGS: -I${SRCDIR}/libsamplerate/include -I${SRCDIR}/libsamplerate/src
// #cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare
// #include "libsamplerate/include/samplerate.h"
import "C"
