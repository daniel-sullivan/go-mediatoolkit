// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package benchcmp

import (
	"math"
	"sync"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// Representative benchmark signal: 1 second, 44100 Hz, 2 channels, interleaved
// int16. A mix of a few sine tones plus a small deterministic pseudo-random
// component — realistic spectral content for the AAC analysis/quantization path
// (not a pure tone the encoder nails to a trivial bitstream). int16 is the
// sample format both the nativeaac and the fdk encoders consume.
//
// The native column drives internal/nativeaac directly (NOT libraries/aac):
// under -tags aacfdk that package compiles a copy of the fdk C, which would
// duplicate-symbol-clash with the fdk reference TUs this benchcmp links for the
// cgo column. Driving nativeaac directly is the same amalgamation-split the
// decode-e2e / encode-e2e parity slices use.
const (
	benchSampleRate = 44100
	benchChannels   = 2
	benchBitrate    = 128000
	benchFrameLen   = 1024 // AAC-LC samples per channel per access unit
	benchFrames     = benchSampleRate / benchFrameLen * benchFrameLen
)

var (
	benchPCMOnce sync.Once
	benchPCM     []int16
)

// signal builds the deterministic representative interleaved int16 PCM buffer
// exactly once (whole frames only, ~1 s of audio).
func signal() []int16 {
	benchPCMOnce.Do(func() {
		pcm := make([]int16, benchFrames*benchChannels)
		for i := 0; i < benchFrames; i++ {
			t := float64(i) / float64(benchSampleRate)
			for ch := 0; ch < benchChannels; ch++ {
				phase := float64(ch) * 0.1
				v := 0.5*math.Sin(2*math.Pi*440*t+phase) +
					0.25*math.Sin(2*math.Pi*1500*t+phase) +
					0.15*math.Sin(2*math.Pi*5000*t)
				s := uint64(i+1)*6364136223846793005 + uint64(ch+1)*1442695040888963407
				s ^= s >> 33
				noise := float64(int32(s)) / 2147483648.0 // [-1, 1)
				pcm[i*benchChannels+ch] = int16((v*0.9 + noise*0.02) * 26000)
			}
		}
		benchPCM = pcm
	})
	return benchPCM
}

// BenchmarkEncode encodes the full ~1 s PCM buffer one AAC-LC access unit at a
// time, for the pure-Go nativeaac encoder vs the vendored fdk (C) encoder. Bytes
// are the input PCM size, so b/s reflects PCM throughput.
func BenchmarkEncode(b *testing.B) {
	pcm := signal()
	pcmBytes := int64(len(pcm) * 2)
	per := benchFrameLen * benchChannels
	frames := len(pcm) / per

	b.Run("native", func(b *testing.B) {
		b.SetBytes(pcmBytes)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			enc, e := nativeaac.NewEncoder(benchSampleRate, benchChannels, benchBitrate)
			if e != nativeaac.AacEncOK {
				b.Fatalf("native NewEncoder: %v", e)
			}
			for f := 0; f < frames; f++ {
				if _, e := enc.EncodeOneFrame(pcm[f*per : (f+1)*per]); e != nativeaac.AacEncOK {
					b.Fatalf("native EncodeOneFrame: %v", e)
				}
			}
		}
	})

	b.Run("cgo", func(b *testing.B) {
		b.SetBytes(pcmBytes)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, _, ok := cgoEncodeAll(benchSampleRate, benchChannels, benchBitrate, benchFrameLen, pcm); !ok {
				b.Fatal("cgo encode failed")
			}
		}
	})
}

// BenchmarkDecode decodes a ~1 s AAC-LC stream (pre-encoded once with the fdk
// encoder so both decoders replay the identical AU sequence) one access unit at
// a time, native vs cgo. Bytes are the compressed stream size.
func BenchmarkDecode(b *testing.B) {
	pcm := signal()
	aus, asc, streamBytes, ok := cgoEncodeAll(benchSampleRate, benchChannels, benchBitrate, benchFrameLen, pcm)
	if !ok || len(aus) == 0 {
		b.Fatal("cgo pre-encode produced no access units")
	}
	b.Logf("stream: %d access units, %d bytes, asc %d bytes", len(aus), streamBytes, len(asc))

	b.Run("native", func(b *testing.B) {
		dec, err := nativeaac.NewDecoder(benchFrameLen, benchSampleRate, benchChannels)
		if err != nil {
			b.Fatalf("native NewDecoder: %v", err)
		}
		out := make([]int16, benchFrameLen*benchChannels)
		b.SetBytes(int64(streamBytes))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			dec.Reset()
			for _, au := range aus {
				if _, err := dec.DecodeAccessUnit(au, out); err != nil {
					b.Fatalf("native DecodeAccessUnit: %v", err)
				}
			}
		}
	})

	b.Run("cgo", func(b *testing.B) {
		b.SetBytes(int64(streamBytes))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			dec := newCgoDecoder(asc, benchFrameLen, benchChannels)
			if dec == nil {
				b.Fatal("cgo decoder open failed")
			}
			for _, au := range aus {
				if _, ok := dec.decode(au); !ok {
					b.Fatal("cgo decode failed")
				}
			}
			dec.close()
		}
	})
}
