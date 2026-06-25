//go:build cgo

package decode_e2e

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// e2eCase describes one PCM signal to round-trip through encode →
// decode-both-ways.
type e2eCase struct {
	name          string
	channels      uint32
	bitsPerSample uint32
	sampleRate    uint32
	frames        uint64
	compression   uint32
	blockSize     uint32 // 0 = encoder default
	gen           func(ch, i int) int32
}

// signalGen builds a few deterministic per-channel signals that exercise
// the constant / fixed / lpc / verbatim subframe paths and the
// inter-channel decorrelation modes (a correlated stereo pair encodes as
// mid/side; an uncorrelated one stays independent / left-side / right-side).
func signalGen(kind string, bps uint32) func(ch, i int) int32 {
	maxv := int64(1)<<(bps-1) - 1
	minv := -(int64(1) << (bps - 1))
	clamp := func(v int64) int32 {
		if v > maxv {
			v = maxv
		}
		if v < minv {
			v = minv
		}
		return int32(v)
	}
	switch kind {
	case "silence":
		return func(ch, i int) int32 { return 0 }
	case "constant":
		return func(ch, i int) int32 { return clamp(int64(1) << (bps - 2)) }
	case "ramp":
		return func(ch, i int) int32 { return clamp(int64(i + ch*7)) }
	case "sine":
		amp := float64(maxv) * 0.6
		return func(ch, i int) int32 {
			f := 0.013 + 0.004*float64(ch)
			return clamp(int64(amp * math.Sin(2*math.Pi*f*float64(i))))
		}
	case "noise":
		// A simple LCG — deterministic, decorrelated per channel.
		return func(ch, i int) int32 {
			s := uint64(i+1)*6364136223846793005 + uint64(ch+1)*1442695040888963407
			s ^= s >> 33
			return clamp(int64(int32(s)) % (maxv + 1))
		}
	case "stereo_correlated":
		// Left ≈ Right + small delta → mid/side wins.
		amp := float64(maxv) * 0.5
		return func(ch, i int) int32 {
			base := int64(amp * math.Sin(2*math.Pi*0.01*float64(i)))
			if ch == 1 {
				base += int64(i % 3) // tiny side signal
			}
			return clamp(base)
		}
	default:
		return func(ch, i int) int32 { return 0 }
	}
}

func buildPCM(c e2eCase) []int32 {
	pcm := make([]int32, int(c.frames)*int(c.channels))
	for i := 0; i < int(c.frames); i++ {
		for ch := 0; ch < int(c.channels); ch++ {
			pcm[i*int(c.channels)+ch] = c.gen(ch, i)
		}
	}
	return pcm
}

// nativeDecodeAll drives the pure-Go nativeflac decoder over a FLAC byte
// stream, collecting interleaved int32 samples plus STREAMINFO and the
// final MD5-check result. It mirrors the libraries/flac native adapter's
// write-callback interleaving so the comparison is apples-to-apples.
func nativeDecodeAll(t *testing.T, stream []byte, md5Check bool) (samples []int32, si nativeflac.StreamInfo, md5OK bool) {
	t.Helper()
	dec := nativeflac.NewDecoder()

	var interleaved []int32
	write := func(h *nativeflac.FrameHeader, buf [][]int32) nativeflac.DecoderWriteStatus {
		blockSize := int(h.Blocksize)
		channels := int(h.Channels)
		base := len(interleaved)
		interleaved = append(interleaved, make([]int32, blockSize*channels)...)
		for ch := 0; ch < channels; ch++ {
			src := buf[ch]
			for i := 0; i < blockSize; i++ {
				interleaved[base+i*channels+ch] = src[i]
			}
		}
		return nativeflac.DecoderWriteContinue
	}
	onErr := func(status nativeflac.DecoderErrorStatus) {
		t.Fatalf("native decoder reported error status %d", status)
	}

	st := dec.InitStream(bytes.NewReader(stream), write, onErr, md5Check)
	require.Equal(t, nativeflac.DecoderSearchForMetadata, st, "native InitStream")

	require.True(t, dec.ProcessUntilEndOfStream(), "native ProcessUntilEndOfStream")

	md5OK = dec.Finish()

	si, ok := dec.StreamInfo()
	require.True(t, ok, "native decoder never saw STREAMINFO")
	return interleaved, si, md5OK
}

func TestDecodeE2EParity(t *testing.T) {
	cases := []e2eCase{
		{"silence_mono_16", 1, 16, 44100, 4096, 5, 0, signalGen("silence", 16)},
		{"constant_mono_16", 1, 16, 44100, 4096, 5, 0, signalGen("constant", 16)},
		{"ramp_mono_16", 1, 16, 44100, 4096, 5, 0, signalGen("ramp", 16)},
		{"sine_mono_16", 1, 16, 44100, 8192, 5, 0, signalGen("sine", 16)},
		{"noise_mono_16", 1, 16, 44100, 8192, 8, 0, signalGen("noise", 16)},
		{"sine_stereo_16", 2, 16, 44100, 8192, 5, 0, signalGen("sine", 16)},
		{"correlated_stereo_16", 2, 16, 44100, 8192, 8, 0, signalGen("stereo_correlated", 16)},
		{"noise_stereo_16", 2, 16, 48000, 8192, 8, 0, signalGen("noise", 16)},
		{"sine_stereo_24", 2, 24, 96000, 8192, 5, 0, signalGen("sine", 24)},
		{"noise_stereo_24", 2, 24, 96000, 4096, 8, 0, signalGen("noise", 24)},
		{"sine_mono_8", 1, 8, 8000, 4096, 5, 0, signalGen("sine", 8)},
		{"ramp_stereo_16_blk1024", 2, 16, 44100, 4096, 5, 1024, signalGen("ramp", 16)},
		{"sine_4ch_16", 4, 16, 44100, 4096, 5, 0, signalGen("sine", 16)},
		{"noise_mono_16_lvl0", 1, 16, 44100, 8192, 0, 0, signalGen("noise", 16)},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pcm := buildPCM(c)

			stream := CgoEncode(pcm, c.channels, c.bitsPerSample, c.sampleRate,
				c.frames, c.compression, c.blockSize, true)
			require.NotEmpty(t, stream, "libFLAC encode produced no bytes")

			// Decode via libFLAC (the oracle).
			cSamples, cInfo, cMD5OK, ok := CgoDecode(stream, c.channels, MaxBlock(c), true)
			require.True(t, ok, "libFLAC decode failed")
			require.True(t, cMD5OK, "libFLAC MD5 check failed (encoder/decoder disagree)")

			// libFLAC must reproduce the original PCM exactly (lossless).
			require.Equal(t, len(pcm), len(cSamples), "libFLAC decoded sample count")
			require.Equal(t, pcm, cSamples, "libFLAC decode is not bit-exact vs input")

			// Decode via the pure-Go nativeflac decoder.
			nSamples, nInfo, nMD5OK := nativeDecodeAll(t, stream, true)

			// 1. Identical decoded samples for every channel.
			require.Equal(t, len(cSamples), len(nSamples), "native decoded sample count")
			require.Equal(t, cSamples, nSamples, "native decode differs from libFLAC")

			// 2. Identical STREAMINFO.
			require.Equal(t, cInfo.Channels, nInfo.Channels, "channels")
			require.Equal(t, cInfo.BitsPerSample, nInfo.BitsPerSample, "bits_per_sample")
			require.Equal(t, cInfo.SampleRate, nInfo.SampleRate, "sample_rate")
			require.Equal(t, cInfo.MinBlockSize, nInfo.MinBlockSize, "min_blocksize")
			require.Equal(t, cInfo.MaxBlockSize, nInfo.MaxBlockSize, "max_blocksize")
			require.Equal(t, cInfo.MinFrameSize, nInfo.MinFrameSize, "min_framesize")
			require.Equal(t, cInfo.MaxFrameSize, nInfo.MaxFrameSize, "max_framesize")
			require.Equal(t, cInfo.TotalSamples, nInfo.TotalSamples, "total_samples")

			// 3. Matching MD5 — both the STREAMINFO signature and that the
			// native decoder's running MD5 verified against it.
			require.Equal(t, cInfo.MD5Sum, nInfo.MD5Sum, "STREAMINFO MD5 signature")
			require.True(t, nMD5OK, "native MD5 check failed")
			require.Equal(t, cMD5OK, nMD5OK, "MD5-check result disagreement")
		})
	}
}

// MaxBlock returns the block size the decoder buffer should size to.
func MaxBlock(c e2eCase) uint32 {
	if c.blockSize != 0 {
		return c.blockSize
	}
	return 65535
}

// TestDecodeE2EParityRandomized fuzzes a handful of random signals to
// widen subframe-path coverage beyond the curated set.
func TestDecodeE2EParityRandomized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping randomized e2e in -short")
	}
	seeds := []uint64{1, 7, 42, 1009}
	for _, seed := range seeds {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			const channels, bps, rate = 2, 16, 44100
			const frames = 6000
			maxv := int64(1)<<(bps-1) - 1
			s := seed
			next := func() int64 {
				s ^= s << 13
				s ^= s >> 7
				s ^= s << 17
				return int64(s)
			}
			pcm := make([]int32, frames*channels)
			for i := 0; i < frames; i++ {
				for ch := 0; ch < channels; ch++ {
					pcm[i*channels+ch] = int32(next() % (maxv + 1))
				}
			}

			stream := CgoEncode(pcm, channels, bps, rate, frames, 8, 0, true)
			require.NotEmpty(t, stream)

			cSamples, cInfo, cMD5OK, ok := CgoDecode(stream, channels, 65535, true)
			require.True(t, ok)
			require.True(t, cMD5OK)
			require.Equal(t, pcm, cSamples)

			nSamples, nInfo, nMD5OK := nativeDecodeAll(t, stream, true)
			require.Equal(t, cSamples, nSamples, "native decode differs from libFLAC")
			require.Equal(t, cInfo.MD5Sum, nInfo.MD5Sum)
			require.True(t, nMD5OK)
		})
	}
}
