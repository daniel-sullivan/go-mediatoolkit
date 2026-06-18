//go:build cgo

package mp3

// This file carries the shared cgo build configuration for the vendored MP3
// backends: minimp3 (decode, libraries/mp3/libminimp3) and LAME 3.100
// (encode, libraries/mp3/liblame). The decoder/encoder logic lives in
// decoder_cgo.go and encoder_cgo.go; the C sources are compiled by the
// aggregation translation units mp3_cgo_minimp3.c and mp3_cgo_lame.c.
//
// minimp3 is public-domain (CC0); LAME is LGPL 2.0+ (see
// libraries/mp3/liblame/COPYING.LAME). Both are vendored and statically
// linked.
//
// -ffp-contract / vectorisation flags are intentionally NOT set here: the
// parity infrastructure supplies them via the mise task env (CGO_CFLAGS +
// CGO_CFLAGS_ALLOW), matching libraries/flac. For ordinary (non-parity)
// builds the default optimisation is fine — these backends are reference
// oracles for the pure-Go port, not the bit-exact target themselves.

/*
#cgo CFLAGS: -I${SRCDIR}/libminimp3
#cgo LDFLAGS: -lm
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo CFLAGS: -I${SRCDIR}/liblame
#cgo CFLAGS: -I${SRCDIR}/liblame/libmp3lame
#cgo CFLAGS: -I${SRCDIR}/liblame/mpglib
#cgo CFLAGS: -I${SRCDIR}/liblame/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable
// Vendored LAME 3.100 trips several benign upstream warnings under a modern
// clang (negative-value shifts in VbrTag's bit packer, fabs-on-int in lame.c's
// EQ/NEQ float-compare macros, tautological array!=NULL checks). Silence the
// noise without touching the reference source.
#cgo CFLAGS: -Wno-shift-negative-value -Wno-absolute-value -Wno-tautological-pointer-compare
*/
import "C"
