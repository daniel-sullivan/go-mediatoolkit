//go:build cgo

package encode_e2e

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// goEncode drives the pure-Go nativeflac.StreamEncoder over interleaved
// int32 PCM and returns the .flac byte stream. It reproduces exactly what
// the public libraries/flac.newNativeStreamEncoder adapter does — the same
// set_* sequence and a write callback that appends framed bytes — but
// against nativeflac directly so this parity package does not import
// libraries/flac (which would link a second copy of libFLAC and clash with
// the encoder/decoder TUs compiled into this package).
//
// seek/tell/metadata callbacks are nil, matching the adapter: with no seek
// callback the encoder leaves STREAMINFO at its streaming placeholder and
// never rewrites it, so the bytes line up with the libFLAC reference run
// (also NULL seek/tell) in CgoEncodeNoSeek.
func goEncode(t *testing.T, pcm []int32, channels, bitsPerSample, sampleRate uint32,
	frames uint64, compression, blockSize uint32, totalEstimate uint64, verify bool) []byte {
	t.Helper()

	enc := nativeflac.NewStreamEncoder()
	require.NotNil(t, enc)

	enc.SetChannels(channels)
	enc.SetBitsPerSample(bitsPerSample)
	enc.SetSampleRate(sampleRate)
	enc.SetCompressionLevel(compression)
	if verify {
		enc.SetVerify(true)
	}
	if blockSize != 0 {
		enc.SetBlocksize(blockSize)
	}
	if totalEstimate != 0 {
		enc.SetTotalSamplesEstimate(totalEstimate)
	}

	var out []byte
	write := func(_ *nativeflac.StreamEncoder, buffer []byte, _, _ uint32, _ any) nativeflac.StreamEncoderWriteStatus {
		out = append(out, buffer...)
		return nativeflac.StreamEncoderWriteStatusOK
	}

	st := enc.InitStream(write, nil, nil, nil, nil)
	require.Equal(t, nativeflac.StreamEncoderInitStatusOK, st, "native InitStream")

	require.True(t, enc.ProcessInterleaved(pcm, uint32(frames)), "native ProcessInterleaved")
	require.True(t, enc.Finish(), "native Finish")
	return out
}

// generateTone mirrors libraries/flac/flac_test.go: a per-channel
// phase-offset sine, sign-extended for the bit depth, within FLAC range.
func generateTone(sampleRate, channels, bitsPerSample, samplesPerChannel int, freqHz float64) []int32 {
	amp := float64(int32(1)<<(bitsPerSample-1)-1) * 0.95
	out := make([]int32, samplesPerChannel*channels)
	for i := 0; i < samplesPerChannel; i++ {
		for ch := 0; ch < channels; ch++ {
			phase := 2*math.Pi*freqHz*float64(i)/float64(sampleRate) + float64(ch)*0.1
			out[i*channels+ch] = int32(math.Sin(phase) * amp)
		}
	}
	return out
}

type encodeCase struct {
	name          string
	channels      int
	bitsPerSample int
	sampleRate    int
	samplesPerCh  int
	freqHz        float64
	compression   uint32
	blockSize     uint32
}

func cases() []encodeCase {
	return []encodeCase{
		{"mono16_l5_bs4096", 1, 16, 44100, 4096, 440, 5, 4096},
		{"stereo16_l5_bs4096", 2, 16, 48000, 4096, 1000, 5, 4096},
		{"stereo16_l5_bs1152", 2, 16, 48000, 3000, 1000, 5, 1152},
		{"stereo24_l5_bs4096", 2, 24, 48000, 4096, 1000, 5, 4096},
		{"stereo16_l0_bs4096", 2, 16, 48000, 4096, 1000, 0, 4096},
		{"stereo16_l8_bs4096", 2, 16, 48000, 4096, 1000, 8, 4096},
		{"stereo8_l5_bs4096", 2, 8, 44100, 4096, 1000, 5, 4096},
		{"quad16_l5_bs4096", 4, 16, 48000, 4096, 1000, 5, 4096},
		{"stereo16_l5_multiblock", 2, 16, 48000, 10000, 777, 5, 4096},
	}
}

// TestEncodeRoundTrip asserts the Go-encoded .flac stream is decoded by
// libFLAC back into the exact original PCM (lossless round-trip), and that
// the STREAMINFO libFLAC parses matches the configured stream parameters.
func TestEncodeRoundTrip(t *testing.T) {
	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			pcm := generateTone(tc.sampleRate, tc.channels, tc.bitsPerSample, tc.samplesPerCh, tc.freqHz)
			frames := uint64(tc.samplesPerCh)

			goBytes := goEncode(t, pcm, uint32(tc.channels), uint32(tc.bitsPerSample),
				uint32(tc.sampleRate), frames, tc.compression, tc.blockSize, frames, false)
			require.NotEmpty(t, goBytes, "native encoder produced no output")

			decoded, info, ok := CgoDecode(goBytes, uint32(tc.channels))
			require.True(t, ok, "libFLAC failed to decode the Go-encoded stream")

			assert.Equal(t, uint32(tc.channels), info.Channels, "channels")
			assert.Equal(t, uint32(tc.bitsPerSample), info.BitsPerSample, "bits per sample")
			assert.Equal(t, uint32(tc.sampleRate), info.SampleRate, "sample rate")
			require.Equal(t, len(pcm), len(decoded), "decoded sample count")
			assert.Equal(t, pcm, decoded, "lossless round-trip must be bit-exact")
		})
	}
}

// TestEncodeByteIdentical asserts the Go-encoded byte stream is identical,
// byte-for-byte, to what libFLAC's own encoder produces for the same input
// and settings (NULL seek/tell on both sides, so neither rewrites
// STREAMINFO). This is the strongest parity claim. If a case ever fails to
// be byte-identical it should be moved to a frame-structure equivalence
// assertion and documented; today all cases are expected identical.
func TestEncodeByteIdentical(t *testing.T) {
	if !nativeflac.StrictMode {
		t.Skip("byte-identical encode parity requires -tags flac_strict (FP-exact window/LPC analysis)")
	}
	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			pcm := generateTone(tc.sampleRate, tc.channels, tc.bitsPerSample, tc.samplesPerCh, tc.freqHz)
			frames := uint64(tc.samplesPerCh)

			goBytes := goEncode(t, pcm, uint32(tc.channels), uint32(tc.bitsPerSample),
				uint32(tc.sampleRate), frames, tc.compression, tc.blockSize, frames, false)
			require.NotEmpty(t, goBytes, "native encoder produced no output")

			cBytes := CgoEncodeNoSeek(pcm, uint32(tc.channels), uint32(tc.bitsPerSample),
				uint32(tc.sampleRate), frames, tc.compression, tc.blockSize, frames, false)
			require.NotEmpty(t, cBytes, "libFLAC encoder produced no output")

			require.Equal(t, len(cBytes), len(goBytes), "encoded byte length")
			if !assert.Equal(t, cBytes, goBytes, "Go-encoded bytes must match libFLAC byte-for-byte") {
				// Report the first divergent offset to make a mismatch
				// actionable.
				n := len(cBytes)
				if len(goBytes) < n {
					n = len(goBytes)
				}
				for i := 0; i < n; i++ {
					if cBytes[i] != goBytes[i] {
						t.Fatalf("first byte divergence at offset %d: libFLAC=0x%02x go=0x%02x", i, cBytes[i], goBytes[i])
					}
				}
			}
		})
	}
}
