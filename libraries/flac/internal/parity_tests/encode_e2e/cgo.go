//go:build cgo

// Package encode_e2e proves end-to-end FLAC ENCODE parity. It encodes a
// known PCM signal to a real .flac byte stream using the pure-Go
// nativeflac encoder (driven through the public libraries/flac
// NewNativeEncoder adapter), then:
//
//   - decodes those Go-produced bytes with libFLAC's full stream decoder
//     (compiled into this package) and asserts the reconstructed int32
//     samples equal the original input — the lossless round-trip claim; and
//   - for a fixed compression setting, encodes the SAME input with
//     libFLAC's full stream encoder (also compiled in) using NULL
//     seek/tell callbacks — exactly matching the native adapter, which
//     also leaves STREAMINFO un-rewritten — and asserts the two byte
//     streams are bit-identical (the strongest parity claim).
//
// The libFLAC encoder and decoder TUs both define static helpers with the
// same names, so they live in SEPARATE translation units
// (encode_e2e_encoder.c / encode_e2e_decoder.c) with the shared support
// code in encode_e2e_support.c, mirroring the amalgamation split the main
// libraries/flac cgo package uses.
package encode_e2e

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libflac
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/include
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/src/libFLAC/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable -Wno-static-in-inline

#include <stdint.h>
#include <stdlib.h>

// Encode with libFLAC's full encoder using NULL seek/tell (no STREAMINFO
// rewrite), matching the pure-Go native adapter. Returns encoded byte
// count (0 on failure).
extern size_t fparity_ee_encode_noseek(uint8_t *out, size_t out_cap,
                                       const int32_t *pcm, uint32_t channels,
                                       uint32_t bits_per_sample, uint32_t sample_rate,
                                       uint64_t frames, uint32_t compression,
                                       uint32_t block_size, uint64_t total_estimate,
                                       int do_verify);

// Decode a FLAC byte stream with libFLAC's full stream decoder. Returns
// the number of inter-channel samples decoded (-1 on failure).
extern long fparity_ee_decode(const uint8_t *in, size_t in_len,
                              int32_t *out, size_t out_cap,
                              uint32_t *channels, uint32_t *bits_per_sample,
                              uint32_t *sample_rate,
                              uint32_t *min_blocksize, uint32_t *max_blocksize,
                              uint32_t *min_framesize, uint32_t *max_framesize,
                              uint64_t *total_samples, uint8_t *md5sum);
*/
import "C"

import (
	"unsafe"
)

// EncodedStreamInfo carries the STREAMINFO fields the C decoder reports
// from a Go-encoded .flac stream.
type EncodedStreamInfo struct {
	Channels      uint32
	BitsPerSample uint32
	SampleRate    uint32
	MinBlockSize  uint32
	MaxBlockSize  uint32
	MinFrameSize  uint32
	MaxFrameSize  uint32
	TotalSamples  uint64
	MD5Sum        [16]byte
}

// CgoEncodeNoSeek encodes interleaved int32 PCM to a FLAC byte stream
// using libFLAC's full stream encoder with NULL seek/tell — the reference
// byte stream the Go-encoded output is compared against bit-for-bit. pcm
// holds frames*channels samples.
func CgoEncodeNoSeek(pcm []int32, channels, bitsPerSample, sampleRate uint32, frames uint64,
	compression, blockSize uint32, totalEstimate uint64, verify bool) []byte {
	out := make([]byte, len(pcm)*4+1<<16)
	var pcmPtr *C.int32_t
	if len(pcm) > 0 {
		pcmPtr = (*C.int32_t)(unsafe.Pointer(&pcm[0]))
	}
	v := C.int(0)
	if verify {
		v = 1
	}
	n := C.fparity_ee_encode_noseek(
		(*C.uint8_t)(unsafe.Pointer(&out[0])), C.size_t(len(out)),
		pcmPtr, C.uint32_t(channels), C.uint32_t(bitsPerSample), C.uint32_t(sampleRate),
		C.uint64_t(frames), C.uint32_t(compression), C.uint32_t(blockSize),
		C.uint64_t(totalEstimate), v)
	if n == 0 {
		return nil
	}
	return out[:n]
}

// CgoDecode decodes a FLAC byte stream (produced by the native Go
// encoder) with libFLAC's full stream decoder. It returns the interleaved
// int32 samples and the STREAMINFO libFLAC parsed from the stream.
func CgoDecode(stream []byte, maxChannels uint32) (samples []int32, info EncodedStreamInfo, ok bool) {
	if len(stream) == 0 {
		return nil, EncodedStreamInfo{}, false
	}
	outCap := len(stream)*8 + 1<<16
	out := make([]int32, outCap)

	var cch, cbps, csr, cminbs, cmaxbs, cminfs, cmaxfs C.uint32_t
	var ctotal C.uint64_t
	var md5 [16]C.uint8_t

	n := C.fparity_ee_decode(
		(*C.uint8_t)(unsafe.Pointer(&stream[0])), C.size_t(len(stream)),
		(*C.int32_t)(unsafe.Pointer(&out[0])), C.size_t(outCap),
		&cch, &cbps, &csr, &cminbs, &cmaxbs, &cminfs, &cmaxfs, &ctotal, &md5[0])
	if n < 0 {
		return nil, EncodedStreamInfo{}, false
	}

	info = EncodedStreamInfo{
		Channels:      uint32(cch),
		BitsPerSample: uint32(cbps),
		SampleRate:    uint32(csr),
		MinBlockSize:  uint32(cminbs),
		MaxBlockSize:  uint32(cmaxbs),
		MinFrameSize:  uint32(cminfs),
		MaxFrameSize:  uint32(cmaxfs),
		TotalSamples:  uint64(ctotal),
	}
	for i := 0; i < 16; i++ {
		info.MD5Sum[i] = byte(md5[i])
	}
	_ = maxChannels
	total := int(n) * int(info.Channels)
	return out[:total], info, true
}
