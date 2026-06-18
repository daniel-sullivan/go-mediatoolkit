//go:build cgo

// Package benchcmp benchmarks the pure-Go nativeflac encoder/decoder
// against the vendored C libFLAC (compiled inline via Cgo), mirroring the
// libraries/opus benchcmp suite. The C path is the same scalar parity
// oracle the parity_tests use — built with -ffp-contract=off and no
// auto-vectorization/unrolling/intrinsics (set via CGO_CFLAGS in the mise
// tasks) — so it is an apples-to-apples scalar comparison, not a
// native-vs-production-C one.
//
// The libFLAC encoder and decoder TUs both define static helpers with the
// same names, so they live in SEPARATE translation units
// (bench_encoder.c / bench_decoder.c) with the shared support code in
// bench_support.c, mirroring the amalgamation split the e2e parity
// packages use. This package drives nativeflac directly rather than
// importing libraries/flac, which would link a second copy of libFLAC and
// clash with the encoder/decoder TUs compiled here.
package benchcmp

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libflac
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/include
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/src/libFLAC/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable -Wno-static-in-inline

#include <stdint.h>
#include <stdlib.h>

// Encode interleaved int32 PCM into a FLAC byte stream with libFLAC's full
// stream encoder using NULL seek/tell (streaming). Returns encoded byte
// count (0 on failure).
extern size_t fbench_encode(uint8_t *out, size_t out_cap,
                            const int32_t *pcm, uint32_t channels,
                            uint32_t bits_per_sample, uint32_t sample_rate,
                            uint64_t frames, uint32_t compression,
                            uint32_t block_size);

// Decode a FLAC byte stream with libFLAC's full stream decoder. Returns the
// number of inter-channel samples decoded (-1 on failure).
extern long fbench_decode(const uint8_t *in, size_t in_len,
                          int32_t *out, size_t out_cap);
*/
import "C"

import (
	"unsafe"
)

// CgoEncode encodes interleaved int32 PCM (frames*channels samples) to a
// FLAC byte stream using libFLAC's full stream encoder.
func CgoEncode(pcm []int32, channels, bitsPerSample, sampleRate uint32, frames uint64,
	compression, blockSize uint32) []byte {
	out := make([]byte, len(pcm)*4+1<<16)
	var pcmPtr *C.int32_t
	if len(pcm) > 0 {
		pcmPtr = (*C.int32_t)(unsafe.Pointer(&pcm[0]))
	}
	n := C.fbench_encode(
		(*C.uint8_t)(unsafe.Pointer(&out[0])), C.size_t(len(out)),
		pcmPtr, C.uint32_t(channels), C.uint32_t(bitsPerSample), C.uint32_t(sampleRate),
		C.uint64_t(frames), C.uint32_t(compression), C.uint32_t(blockSize))
	if n == 0 {
		return nil
	}
	return out[:n]
}

// CgoDecode decodes a FLAC byte stream with libFLAC's full stream decoder,
// returning the interleaved int32 samples.
func CgoDecode(stream []byte, channels uint32) []int32 {
	if len(stream) == 0 {
		return nil
	}
	outCap := len(stream)*8 + 1<<16
	out := make([]int32, outCap)
	n := C.fbench_decode(
		(*C.uint8_t)(unsafe.Pointer(&stream[0])), C.size_t(len(stream)),
		(*C.int32_t)(unsafe.Pointer(&out[0])), C.size_t(outCap))
	if n < 0 {
		return nil
	}
	total := int(n) * int(channels)
	return out[:total]
}
