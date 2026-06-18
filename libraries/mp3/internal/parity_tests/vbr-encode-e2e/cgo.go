// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package vbre2e holds the vbr-encode-e2e parity slice: the TOP-LEVEL
// byte-identical gate for the pure-Go LAME VBR encoder. It drives a genuine
// end-to-end -V2 (vbr_default == vbr_mtrh) encode through the vendored libmp3lame
// and asserts the pure-Go nativemp3 encoder produces a byte-for-byte identical
// FULL OUTPUT STREAM — the finalized Xing/Info/LAME tag frame plus every audio
// frame, in LAME's file layout (placeholder tag first, overwritten on close).
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference, one vendored source per TU
// (the lame_*.c / mpglib_*.c wrappers — the same per-TU split the public cgo
// backend and the vbrtag slice use, because LAME reuses Min/Max macros + per-TU
// file statics). oracle.c drives the public LAME API, generates the synthetic
// int16 PCM (exported so the Go side encodes the byte-identical input), and
// assembles the file-layout stream. It NEVER imports libraries/mp3 (which would
// duplicate the LAME symbols at link time); only the pure-Go internal/nativemp3
// port is imported on the Go side (native.go).
//
// The -V2 encode is heavily FP-bearing (mdct, psymodel, the vbrquantize leaf
// kernels), so the byte-identical stream holds only under the mp3_strict build
// (FMA-free Go) vs the -ffp-contract=off oracle. The scalar FP flags come from
// the mise task env (CGO_CFLAGS), never the in-source #cgo block. The strict gate
// lives in parity_test.go.
package vbre2e

/*
#cgo CFLAGS: -I${SRCDIR}/../../../liblame
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/libmp3lame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/mpglib
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/include
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable
#cgo CFLAGS: -Wno-shift-negative-value -Wno-absolute-value -Wno-tautological-pointer-compare
#cgo CFLAGS: -Wno-missing-field-initializers -Wno-parentheses

#include <stdlib.h>
#include "oracle.h"
*/
import "C"

import "unsafe"

// cgoHandle wraps a C oracle handle (the captured PCM + assembled stream of a
// real -V2 encode).
type cgoHandle struct{ h *C.mp3parity_vbre2e_t }

// cgoRun drives a genuine -V2 encode of nsamplesPerCh synthetic samples per
// channel at the given samplerate and captures the generated int16 PCM and the
// assembled file-layout stream. Returns nil on a LAME setup/encode failure.
func cgoRun(samplerate, channels, nsamplesPerCh int, seed uint32) *cgoHandle {
	h := C.mp3parity_vbre2e_run(C.int(samplerate), C.int(channels),
		C.int(nsamplesPerCh), C.uint(seed))
	if h == nil {
		return nil
	}
	return &cgoHandle{h: h}
}

func (c *cgoHandle) free() { C.mp3parity_vbre2e_free(c.h) }

// pcm returns the generated interleaved int16 input PCM (the Go side encodes
// these exact samples).
func (c *cgoHandle) pcm() []int16 {
	n := int(C.mp3parity_vbre2e_pcm_len(c.h))
	if n <= 0 {
		return nil
	}
	ptr := C.mp3parity_vbre2e_pcm_ptr(c.h)
	out := make([]int16, n)
	src := unsafe.Slice((*int16)(unsafe.Pointer(ptr)), n)
	copy(out, src)
	return out
}

// goldenStream returns the assembled file-layout stream (finalized tag frame +
// audio frames).
func (c *cgoHandle) goldenStream() []byte {
	n := int(C.mp3parity_vbre2e_stream_len(c.h))
	if n <= 0 {
		return nil
	}
	ptr := C.mp3parity_vbre2e_stream_ptr(c.h)
	return C.GoBytes(unsafe.Pointer(ptr), C.int(n))
}

// tagLen is the finalized tag frame length (the spliced prefix), used to
// localize a divergence to the tag region vs the audio region.
func (c *cgoHandle) tagLen() int { return int(C.mp3parity_vbre2e_tag_len(c.h)) }
