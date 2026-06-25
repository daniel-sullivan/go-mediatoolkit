//go:build cgo && mp3lame

// Package benchcodec holds the end-to-end MP3 codec benchmarks (full frame
// decode and full frame encode), complementing the bit-reader micro-benchmark
// in ../benchcmp. It exercises the whole public libraries/mp3 surface:
//
//   - Decode: the pure-Go nativemp3 decoder (NewNativeDecoder) vs the vendored
//     C minimp3 decoder (NewDecoder under cgo), both decoding the SAME MP3
//     stream frame by frame to interleaved int16.
//   - Encode: the pure-Go LAME port (NewNativeEncoder) vs the vendored C
//     libmp3lame (NewEncoder), both compressing the SAME int16 PCM.
//
// It lives in its OWN package (not ../benchcmp) on purpose: ../benchcmp compiles
// a private copy of minimp3 (MINIMP3_IMPLEMENTATION) for its bit-reader bench,
// and minimp3's mp3dec_* entry points are NOT static, so linking that package
// together with libraries/mp3 (which also links minimp3) duplicate-symbol
// clashes. This package imports the public libraries/mp3 only — minimp3 and
// libmp3lame are each linked exactly once, through libraries/mp3.
//
// The input MP3 stream is fabricated once with the cgo libmp3lame encoder, so
// this file needs the mp3lame tag (encoding requires it; without it NewEncoder
// returns ErrEncoderRequiresLAME).
package benchcodec

import (
	"bytes"
	"io"
	"math"
	"testing"

	mp3 "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3"
)

const (
	codecSampleRate = 44100
	codecChannels   = 2
	codecBitRate    = 192000
	codecFrames     = codecSampleRate // ~1 s of audio (samples per channel)
)

// codecPCM builds ~1 s of interleaved int16 PCM: a few sine tones plus a tiny
// deterministic noise floor, so the encoder exercises real granule/Huffman
// paths rather than a trivial tone.
func codecPCM() []int16 {
	pcm := make([]int16, codecFrames*codecChannels)
	for i := 0; i < codecFrames; i++ {
		t := float64(i) / float64(codecSampleRate)
		for ch := 0; ch < codecChannels; ch++ {
			phase := float64(ch) * 0.1
			v := 0.5*math.Sin(2*math.Pi*440*t+phase) +
				0.3*math.Sin(2*math.Pi*1320*t+phase) +
				0.2*math.Sin(2*math.Pi*60*t)
			s := uint64(i+1)*6364136223846793005 + uint64(ch+1)*1442695040888963407
			s ^= s >> 33
			noise := float64(int32(s)) / 2147483648.0
			pcm[i*codecChannels+ch] = int16((v*0.9 + noise*0.02) * 26000)
		}
	}
	return pcm
}

// encodeMP3 compresses pcm to an MP3 byte stream once with the cgo libmp3lame
// encoder, used as the decode-benchmark input.
func encodeMP3(b *testing.B, pcm []int16) []byte {
	b.Helper()
	var buf bytes.Buffer
	enc, err := mp3.NewEncoder(&buf, mp3.StreamInfo{SampleRate: codecSampleRate, Channels: codecChannels},
		mp3.WithBitRate(codecBitRate))
	if err != nil {
		b.Fatalf("cgo NewEncoder: %v", err)
	}
	if err := enc.EncodeFrame(pcm); err != nil {
		b.Fatalf("cgo EncodeFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		b.Fatalf("cgo encoder Close: %v", err)
	}
	if buf.Len() == 0 {
		b.Fatal("cgo encoder produced no output")
	}
	return buf.Bytes()
}

// decodeAll drains every audio frame from dec, returning the total
// samples-per-channel decoded.
func decodeAll(b *testing.B, dec mp3.Decoder) int {
	b.Helper()
	buf := make([]int16, mp3.MaxSamplesPerFrame*mp3.MaxChannels)
	var total int
	for {
		n, err := dec.DecodeFrame(buf)
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			b.Fatalf("DecodeFrame: %v", err)
		}
	}
	if err := dec.Close(); err != nil {
		b.Fatalf("decoder Close: %v", err)
	}
	return total
}

// BenchmarkCodecDecode decodes a ~1 s MP3 stream frame by frame, pure-Go
// nativemp3 vs the vendored C minimp3. Bytes are the compressed stream size.
func BenchmarkCodecDecode(b *testing.B) {
	pcm := codecPCM()
	stream := encodeMP3(b, pcm)
	streamBytes := int64(len(stream))
	b.Logf("stream: %d bytes", len(stream))

	b.Run("native", func(b *testing.B) {
		b.SetBytes(streamBytes)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			dec, err := mp3.NewNativeDecoder(bytes.NewReader(stream))
			if err != nil {
				b.Fatalf("NewNativeDecoder: %v", err)
			}
			decodeAll(b, dec)
		}
	})
	b.Run("cgo", func(b *testing.B) {
		b.SetBytes(streamBytes)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			dec, err := mp3.NewDecoder(bytes.NewReader(stream))
			if err != nil {
				b.Fatalf("NewDecoder: %v", err)
			}
			decodeAll(b, dec)
		}
	})
}

// BenchmarkCodecEncode compresses ~1 s of int16 PCM to MP3, pure-Go LAME port
// vs the vendored C libmp3lame. Bytes are the input PCM size, so b/s reflects
// PCM throughput.
func BenchmarkCodecEncode(b *testing.B) {
	pcm := codecPCM()
	pcmBytes := int64(len(pcm) * 2)
	info := mp3.StreamInfo{SampleRate: codecSampleRate, Channels: codecChannels}

	b.Run("native", func(b *testing.B) {
		b.SetBytes(pcmBytes)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			enc, err := mp3.NewNativeEncoder(io.Discard, info, mp3.WithBitRate(codecBitRate))
			if err != nil {
				b.Fatalf("NewNativeEncoder: %v", err)
			}
			if err := enc.EncodeFrame(pcm); err != nil {
				b.Fatalf("native EncodeFrame: %v", err)
			}
			if err := enc.Close(); err != nil {
				b.Fatalf("native Close: %v", err)
			}
		}
	})
	b.Run("cgo", func(b *testing.B) {
		b.SetBytes(pcmBytes)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			enc, err := mp3.NewEncoder(io.Discard, info, mp3.WithBitRate(codecBitRate))
			if err != nil {
				b.Fatalf("NewEncoder: %v", err)
			}
			if err := enc.EncodeFrame(pcm); err != nil {
				b.Fatalf("cgo EncodeFrame: %v", err)
			}
			if err := enc.Close(); err != nil {
				b.Fatalf("cgo Close: %v", err)
			}
		}
	})
}
