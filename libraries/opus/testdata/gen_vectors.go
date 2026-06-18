//go:build ignore

// gen_vectors generates test vectors for the Opus decoder using the reference
// libopus via Cgo. It encodes test signals and decodes them, writing both the
// encoded packets and reference PCM output to files.
//
// Usage: cd libraries/opus/testdata && go run gen_vectors.go
// Requires: libopus (brew install opus)
package main

/*
#cgo pkg-config: opus
#cgo LDFLAGS: -lm
#include <opus.h>
#include <stdlib.h>

// Wrapper for variadic opus_encoder_ctl with OPUS_SET_BITRATE
static int set_bitrate(OpusEncoder *enc, opus_int32 bitrate) {
    return opus_encoder_ctl(enc, OPUS_SET_BITRATE(bitrate));
}

// Wrapper for opus_encoder_ctl with OPUS_SET_COMPLEXITY
static int set_complexity(OpusEncoder *enc, opus_int32 complexity) {
    return opus_encoder_ctl(enc, OPUS_SET_COMPLEXITY(complexity));
}

// Wrapper for opus_encoder_ctl with OPUS_SET_FORCE_CHANNELS
static int set_force_channels(OpusEncoder *enc, opus_int32 channels) {
    return opus_encoder_ctl(enc, OPUS_SET_FORCE_CHANNELS(channels));
}

// Wrapper for opus_decoder_ctl with OPUS_GET_FINAL_RANGE
static int get_final_range(OpusDecoder *dec, opus_uint32 *range) {
    return opus_decoder_ctl(dec, OPUS_GET_FINAL_RANGE(range));
}
*/
import "C"
import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"unsafe"
)

type testCase struct {
	name       string
	sampleRate int
	channels   int
	app        int
	bitrate    int
	frameMs    float64
	signal     func(n int) []float32
}

func main() {
	cases := []testCase{
		{
			name:       "celt_mono_48k_20ms",
			sampleRate: 48000,
			channels:   1,
			app:        C.OPUS_APPLICATION_RESTRICTED_LOWDELAY, // forces CELT
			bitrate:    64000,
			frameMs:    20,
			signal:     sineSignal(440, 48000),
		},
		{
			name:       "celt_mono_48k_10ms",
			sampleRate: 48000,
			channels:   1,
			app:        C.OPUS_APPLICATION_RESTRICTED_LOWDELAY,
			bitrate:    64000,
			frameMs:    10,
			signal:     sineSignal(440, 48000),
		},
		{
			name:       "silk_mono_48k_20ms",
			sampleRate: 48000,
			channels:   1,
			app:        C.OPUS_APPLICATION_VOIP,
			bitrate:    12000,
			frameMs:    20,
			signal:     sineSignal(220, 48000),
		},
	}

	for _, tc := range cases {
		fmt.Printf("Generating %s...\n", tc.name)
		if err := generateVector(tc); err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			continue
		}
	}
	fmt.Println("Done.")
}

func generateVector(tc testCase) error {
	frameSamples := int(tc.frameMs * float64(tc.sampleRate) / 1000)
	nFrames := 5

	// Create encoder.
	var encErr C.int
	enc := C.opus_encoder_create(C.opus_int32(tc.sampleRate), C.int(tc.channels), C.int(tc.app), &encErr)
	if encErr != 0 {
		return fmt.Errorf("encoder create: %d", encErr)
	}
	defer C.opus_encoder_destroy(enc)

	C.set_bitrate(enc, C.opus_int32(tc.bitrate))
	C.set_complexity(enc, 10)
	C.set_force_channels(enc, C.opus_int32(tc.channels))

	// Create decoder.
	var decErr C.int
	dec := C.opus_decoder_create(C.opus_int32(tc.sampleRate), C.int(tc.channels), &decErr)
	if decErr != 0 {
		return fmt.Errorf("decoder create: %d", decErr)
	}
	defer C.opus_decoder_destroy(dec)

	// Generate signal.
	totalSamples := frameSamples * nFrames * tc.channels
	signal := tc.signal(totalSamples)

	// Open output files.
	pktFile, err := os.Create(tc.name + ".pkt")
	if err != nil {
		return err
	}
	defer pktFile.Close()

	pcmFile, err := os.Create(tc.name + ".pcm")
	if err != nil {
		return err
	}
	defer pcmFile.Close()

	pktBuf := make([]byte, 4000)
	decodedBuf := make([]C.float, frameSamples*tc.channels)

	for f := 0; f < nFrames; f++ {
		off := f * frameSamples * tc.channels
		inBuf := signal[off : off+frameSamples*tc.channels]

		// Encode.
		n := C.opus_encode_float(enc,
			(*C.float)(unsafe.Pointer(&inBuf[0])),
			C.int(frameSamples),
			(*C.uchar)(unsafe.Pointer(&pktBuf[0])),
			C.opus_int32(len(pktBuf)))
		if n < 0 {
			return fmt.Errorf("encode frame %d: %d", f, n)
		}
		pktLen := int(n)

		// Write packet: 4-byte LE length prefix + data.
		var lenBuf [4]byte
		binary.LittleEndian.PutUint32(lenBuf[:], uint32(pktLen))
		pktFile.Write(lenBuf[:])
		pktFile.Write(pktBuf[:pktLen])

		// Decode with reference.
		dn := C.opus_decode_float(dec,
			(*C.uchar)(unsafe.Pointer(&pktBuf[0])),
			C.opus_int32(pktLen),
			(*C.float)(unsafe.Pointer(&decodedBuf[0])),
			C.int(frameSamples), 0)
		if dn < 0 {
			return fmt.Errorf("decode frame %d: %d", f, dn)
		}

		// Write reference PCM as float32 LE.
		for i := 0; i < int(dn)*tc.channels; i++ {
			var buf [4]byte
			binary.LittleEndian.PutUint32(buf[:], math.Float32bits(float32(decodedBuf[i])))
			pcmFile.Write(buf[:])
		}

		// Get range for verification.
		var rng C.opus_uint32
		C.get_final_range(dec, &rng)

		// Parse TOC to show mode.
		toc := pktBuf[0]
		mode := "SILK"
		if toc&0x80 != 0 {
			mode = "CELT"
		} else if toc&0x60 == 0x60 {
			mode = "Hybrid"
		}
		fmt.Printf("  frame %d: %s pkt=%d bytes, decoded=%d samples, rng=0x%08x\n",
			f, mode, pktLen, dn, rng)
	}
	return nil
}

func sineSignal(freq float64, sampleRate int) func(int) []float32 {
	return func(n int) []float32 {
		out := make([]float32, n)
		for i := range out {
			out[i] = float32(0.5 * math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)))
		}
		return out
	}
}
