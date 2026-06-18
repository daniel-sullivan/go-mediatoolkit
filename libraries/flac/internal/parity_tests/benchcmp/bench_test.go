//go:build cgo

package benchcmp

import (
	"bytes"
	"math"
	"sync"
	"testing"

	"go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// Representative benchmark signal: 1 second, 44100 Hz, 2 channels, 16-bit,
// interleaved int32. A mix of a few sine tones plus a small deterministic
// pseudo-random component — compressible (FLAC finds structure) but
// realistic (not a pure tone the predictor nails to zero residual).
const (
	benchSampleRate    = 44100
	benchChannels      = 2
	benchBitsPerSample = 16
	benchFrames        = benchSampleRate // 1 second
	benchBlockSize     = 4096
)

var (
	benchPCMOnce sync.Once
	benchPCM     []int32
)

// signal builds the deterministic representative PCM buffer exactly once.
func signal() []int32 {
	benchPCMOnce.Do(func() {
		amp := float64(int32(1)<<(benchBitsPerSample-1)-1) * 0.7
		pcm := make([]int32, benchFrames*benchChannels)
		// A 32-bit LCG drives the low-amplitude noise floor; deterministic
		// across runs and per (channel, sample) so the signal is stable.
		for i := 0; i < benchFrames; i++ {
			t := float64(i) / float64(benchSampleRate)
			for ch := 0; ch < benchChannels; ch++ {
				phase := float64(ch) * 0.1
				v := 0.5*math.Sin(2*math.Pi*440*t+phase) +
					0.3*math.Sin(2*math.Pi*1320*t+phase) +
					0.2*math.Sin(2*math.Pi*60*t)
				s := uint64(i+1)*6364136223846793005 + uint64(ch+1)*1442695040888963407
				s ^= s >> 33
				noise := float64(int32(s)) / 2147483648.0 // [-1, 1)
				sample := v*amp + noise*amp*0.02
				pcm[i*benchChannels+ch] = int32(sample)
			}
		}
		benchPCM = pcm
	})
	return benchPCM
}

// goEncode drives the pure-Go nativeflac.StreamEncoder over interleaved
// int32 PCM and returns the encoded byte count. It mirrors encode_e2e's
// goEncode (NULL seek/tell, streaming) with *testing.T stripped.
func goEncode(b *testing.B, pcm []int32, channels, bitsPerSample, sampleRate uint32,
	frames uint64, compression, blockSize uint32) int {
	enc := nativeflac.NewStreamEncoder()
	if enc == nil {
		b.Fatal("NewStreamEncoder returned nil")
	}
	enc.SetChannels(channels)
	enc.SetBitsPerSample(bitsPerSample)
	enc.SetSampleRate(sampleRate)
	enc.SetCompressionLevel(compression)
	if blockSize != 0 {
		enc.SetBlocksize(blockSize)
	}
	enc.SetTotalSamplesEstimate(frames)

	var n int
	write := func(_ *nativeflac.StreamEncoder, buffer []byte, _, _ uint32, _ any) nativeflac.StreamEncoderWriteStatus {
		n += len(buffer)
		return nativeflac.StreamEncoderWriteStatusOK
	}

	if st := enc.InitStream(write, nil, nil, nil, nil); st != nativeflac.StreamEncoderInitStatusOK {
		b.Fatalf("native InitStream: %v", st)
	}
	if !enc.ProcessInterleaved(pcm, uint32(frames)) {
		b.Fatal("native ProcessInterleaved failed")
	}
	if !enc.Finish() {
		b.Fatal("native Finish failed")
	}
	return n
}

// nativeDecodeAll drives the pure-Go nativeflac decoder over a FLAC byte
// stream, counting decoded samples. It mirrors decode_e2e's nativeDecodeAll
// (write callback interleaving) with *testing.T stripped and MD5 disabled.
func nativeDecode(b *testing.B, stream []byte) int {
	dec := nativeflac.NewDecoder()
	var count int
	write := func(h *nativeflac.FrameHeader, buf [][]int32) nativeflac.DecoderWriteStatus {
		count += int(h.Blocksize) * int(h.Channels)
		return nativeflac.DecoderWriteContinue
	}
	onErr := func(status nativeflac.DecoderErrorStatus) {
		b.Fatalf("native decoder reported error status %d", status)
	}
	if st := dec.InitStream(bytes.NewReader(stream), write, onErr, false); st != nativeflac.DecoderSearchForMetadata {
		b.Fatalf("native InitStream: %v", st)
	}
	if !dec.ProcessUntilEndOfStream() {
		b.Fatal("native ProcessUntilEndOfStream failed")
	}
	dec.Finish()
	return count
}

var compressionLevels = []uint32{0, 5, 8}

// BenchmarkEncode encodes the full 1s PCM buffer to a discarded sink, for
// native vs cgo at compression levels 0, 5, 8.
func BenchmarkEncode(b *testing.B) {
	pcm := signal()
	pcmBytes := int64(len(pcm) * 4)

	for _, level := range compressionLevels {
		level := level
		b.Run(levelName(level)+"/native", func(b *testing.B) {
			b.SetBytes(pcmBytes)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				goEncode(b, pcm, benchChannels, benchBitsPerSample, benchSampleRate,
					benchFrames, level, benchBlockSize)
			}
		})
		b.Run(levelName(level)+"/cgo", func(b *testing.B) {
			b.SetBytes(pcmBytes)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				CgoEncode(pcm, benchChannels, benchBitsPerSample, benchSampleRate,
					benchFrames, level, benchBlockSize)
			}
		})
	}
}

// BenchmarkDecode decodes the full 1s stream (pre-encoded once at level 5
// via the cgo encoder), for native vs cgo.
func BenchmarkDecode(b *testing.B) {
	pcm := signal()
	stream := CgoEncode(pcm, benchChannels, benchBitsPerSample, benchSampleRate,
		benchFrames, 5, benchBlockSize)
	if len(stream) == 0 {
		b.Fatal("cgo encode produced no bytes")
	}
	streamBytes := int64(len(stream))

	b.Run("native", func(b *testing.B) {
		b.SetBytes(streamBytes)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			nativeDecode(b, stream)
		}
	})
	b.Run("cgo", func(b *testing.B) {
		b.SetBytes(streamBytes)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			CgoDecode(stream, benchChannels)
		}
	})
}

func levelName(level uint32) string {
	switch level {
	case 0:
		return "level0"
	case 5:
		return "level5"
	case 8:
		return "level8"
	default:
		return "level?"
	}
}
