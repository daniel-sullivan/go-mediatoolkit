//go:build cgo

// Package decode_e2e proves end-to-end FLAC decode parity. It encodes a
// known PCM signal to a real .flac byte stream using libFLAC's full
// stream encoder (compiled into this package), then decodes those bytes
// two ways — through libFLAC's full stream decoder (also compiled in)
// and through the pure-Go nativeflac decoder — and asserts the decoded
// int32 samples, STREAMINFO, and MD5 are identical.
//
// The libFLAC encoder and decoder TUs both define static helpers with
// the same names (read_callback_, set_defaults_, …), so they live in
// SEPARATE translation units (decode_e2e_encoder.c / decode_e2e_decoder.c)
// with the shared support code in decode_e2e_support.c, mirroring the
// amalgamation split the main libraries/flac cgo package uses.
package decode_e2e

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libflac
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/include
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/src/libFLAC/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable -Wno-static-in-inline

#include <stdint.h>
#include <stdlib.h>

// Encode `frames` inter-channel samples of interleaved int32 PCM into a
// FLAC byte stream. Returns the encoded byte count (0 on failure); the
// bytes are written into out[0..ret). The encoder uses the given
// compression level and a fixed block size when block_size != 0.
extern size_t fparity_e2e_encode(uint8_t *out, size_t out_cap,
                                  const int32_t *pcm, uint32_t channels,
                                  uint32_t bits_per_sample, uint32_t sample_rate,
                                  uint64_t frames, uint32_t compression,
                                  uint32_t block_size, int md5_check);

// Decode a FLAC byte stream with libFLAC's full stream decoder. The
// decoded interleaved int32 samples are written into out (capacity
// out_cap int32). Returns the number of inter-channel samples decoded,
// or -1 on failure. STREAMINFO fields and the computed/expected MD5 are
// reported through the out-params. md5_check enables libFLAC's own MD5
// verification; md5_ok reports whether it matched (1) or not (0).
extern long fparity_e2e_decode(const uint8_t *in, size_t in_len,
                                int32_t *out, size_t out_cap,
                                uint32_t *channels, uint32_t *bits_per_sample,
                                uint32_t *sample_rate,
                                uint32_t *min_blocksize, uint32_t *max_blocksize,
                                uint32_t *min_framesize, uint32_t *max_framesize,
                                uint64_t *total_samples, uint8_t *md5sum,
                                int md5_check, int *md5_ok);
*/
import "C"

import (
	"unsafe"
)

// EncodedStreamInfo carries the STREAMINFO fields the C decoder reports
// so the test can compare them against the native decoder's view.
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

// CgoEncode encodes interleaved int32 PCM to a FLAC byte stream using
// libFLAC's full stream encoder. pcm holds frames*channels samples.
func CgoEncode(pcm []int32, channels, bitsPerSample, sampleRate uint32, frames uint64,
	compression, blockSize uint32, md5Check bool) []byte {
	out := make([]byte, len(pcm)*4+1<<16) // generous; FLAC is lossless so never larger than raw+headers
	var pcmPtr *C.int32_t
	if len(pcm) > 0 {
		pcmPtr = (*C.int32_t)(unsafe.Pointer(&pcm[0]))
	}
	mc := C.int(0)
	if md5Check {
		mc = 1
	}
	n := C.fparity_e2e_encode(
		(*C.uint8_t)(unsafe.Pointer(&out[0])), C.size_t(len(out)),
		pcmPtr, C.uint32_t(channels), C.uint32_t(bitsPerSample), C.uint32_t(sampleRate),
		C.uint64_t(frames), C.uint32_t(compression), C.uint32_t(blockSize), mc)
	if n == 0 {
		return nil
	}
	return out[:n]
}

// CgoDecode decodes a FLAC byte stream with libFLAC's full stream
// decoder. It returns the interleaved int32 samples, the STREAMINFO, and
// whether libFLAC's own MD5 check passed (only meaningful when md5Check).
func CgoDecode(stream []byte, channels, maxBlockSize uint32, md5Check bool) (samples []int32, info EncodedStreamInfo, md5OK bool, ok bool) {
	if len(stream) == 0 {
		return nil, EncodedStreamInfo{}, false, false
	}
	// Generous output capacity: lossless, so decoded sample count equals
	// the encoder's input. The test passes the exact frame count; size to
	// it via maxBlockSize headroom plus the known stream length bound.
	outCap := len(stream)*8 + int(maxBlockSize)*int(channels) + 1<<16
	out := make([]int32, outCap)

	var cch, cbps, csr, cminbs, cmaxbs, cminfs, cmaxfs C.uint32_t
	var ctotal C.uint64_t
	var md5 [16]C.uint8_t
	mc := C.int(0)
	if md5Check {
		mc = 1
	}
	var cMD5OK C.int

	n := C.fparity_e2e_decode(
		(*C.uint8_t)(unsafe.Pointer(&stream[0])), C.size_t(len(stream)),
		(*C.int32_t)(unsafe.Pointer(&out[0])), C.size_t(outCap),
		&cch, &cbps, &csr, &cminbs, &cmaxbs, &cminfs, &cmaxfs, &ctotal,
		&md5[0], mc, &cMD5OK)
	if n < 0 {
		return nil, EncodedStreamInfo{}, false, false
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
	total := int(n) * int(info.Channels)
	return out[:total], info, cMD5OK != 0, true
}
