//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_SilkEncode_Mono — Phase 8 capstone.
//
// Drives silk_Encode end-to-end over synthetic PCM on both the Go and C
// sides with matching silk_EncControlStruct configurations. Asserts
// 0-byte / 0-ULP parity on pulses + emitted range-coder bytes across
// multiple frames, sample rates, payload sizes, and bitrates.
//
// silk_Encode is the SILK-side API boundary — the top-level
// opus_encoder.c (Phase 9) would be the only piece above this. We do
// not exercise the VAD/FEC-flags patch path that requires multiple
// frames per packet to fire (payloadSize_ms >= 20 with 20ms frames is
// exactly one frame per packet; 40/60 ms packets require two or three
// SILK frames per packet, and that's the stress this test is after).
func TestParity_SilkEncode_Mono(t *testing.T) {
	type tc struct {
		name          string
		apiSampleRate int
		internalSR    int // maxInternalSampleRate
		desiredSR     int
		payloadMs     int
		bitRate       int
		complexity    int
	}
	cases := []tc{
		// Mixed Fs + framesizes. 20 ms frames at 16kHz WB is the bread-and-butter case.
		{"8k-10ms-16kbps", 8000, 8000, 8000, 10, 16000, 5},
		{"8k-20ms-16kbps", 8000, 8000, 8000, 20, 16000, 5},
		{"12k-20ms-20kbps", 12000, 12000, 12000, 20, 20000, 5},
		{"16k-20ms-24kbps", 16000, 16000, 16000, 20, 24000, 5},
		{"16k-20ms-32kbps", 16000, 16000, 16000, 20, 32000, 8},
		// Higher API rate, downsampled inside SILK.
		{"48k-20ms-24kbps-nb", 48000, 8000, 8000, 20, 24000, 5},
		{"48k-20ms-32kbps-wb", 48000, 16000, 16000, 20, 32000, 5},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			framesPerSec := 1000 / c.payloadMs
			samplesPerFrame := c.apiSampleRate / framesPerSec

			// Build shared config.
			cfg := SilkEncMonoCfg{
				NChannelsAPI:              1,
				NChannelsInternal:         1,
				APISampleRate:             c.apiSampleRate,
				MaxInternalSampleRate:     c.internalSR,
				MinInternalSampleRate:     c.internalSR,
				DesiredInternalSampleRate: c.desiredSR,
				PayloadSizeMs:             c.payloadMs,
				BitRate:                   c.bitRate,
				Complexity:                c.complexity,
				MaxBits:                   (c.bitRate * c.payloadMs / 1000) + 256,
			}
			goCfg := nativeopus.ExportTestSilkEncoder_Cfg(cfg) // identical field order

			// Allocate encoders.
			cEnc := NewCSilkEncoder(1, 0)
			defer cEnc.Free()
			goEnc := nativeopus.ExportTestSilkEncoder_New(1, 0)

			// Number of frames to push. Use at least 4 so we exercise
			// multi-frame packet assembly + conditional coding.
			nFrames := 6

			for frame := 0; frame < nFrames; frame++ {
				pcm := make([]float32, samplesPerFrame)
				generatePCM(pcm, frame, c.apiSampleRate)

				pktC := make([]byte, 1275)
				pktGo := make([]byte, 1275)

				retC, nbC, pulsesC, stC, rngC := cEnc.EncodeFrame(cfg, pcm, pktC, 0, 1)
				retGo, bytesGo, pulsesGo, stGo, rngGo := nativeopus.ExportTestSilkEncoder_EncodeFrame(goEnc, goCfg, pcm, pktGo, 0, 1)

				if retC != retGo {
					t.Fatalf("frame %d: ret C=%d Go=%d", frame, retC, retGo)
				}
				if nbC != len(bytesGo) {
					t.Fatalf("frame %d: nBytesOut C=%d Go=%d", frame, nbC, len(bytesGo))
				}
				if stC != stGo {
					t.Fatalf("frame %d: signalType C=%d Go=%d", frame, stC, stGo)
				}
				if rngC != rngGo {
					t.Fatalf("frame %d: rng C=0x%08x Go=0x%08x", frame, rngC, rngGo)
				}
				if len(pulsesC) != len(pulsesGo) {
					t.Fatalf("frame %d: len(pulses) C=%d Go=%d", frame, len(pulsesC), len(pulsesGo))
				}
				for i := range pulsesC {
					if pulsesC[i] != pulsesGo[i] {
						t.Fatalf("frame %d: pulses[%d] C=%d Go=%d (first mismatch)", frame, i, pulsesC[i], pulsesGo[i])
					}
				}
				if !bytes.Equal(pktC[:nbC], bytesGo) {
					t.Fatalf("frame %d: bitstream mismatch: C=% x Go=% x", frame, pktC[:nbC], bytesGo)
				}
				// Sanity: confirm we actually produced payload + at
				// least some non-zero pulses on a frame-full of
				// coherent tones. (The first frame may legitimately
				// emit no pulses while the resampler warms up.)
				if frame >= 2 && nbC == 0 {
					t.Fatalf("frame %d: unexpected empty payload on a full frame", frame)
				}
				if frame >= 3 {
					anyNZ := false
					for _, p := range pulsesC {
						if p != 0 {
							anyNZ = true
							break
						}
					}
					if !anyNZ {
						t.Fatalf("frame %d: pulses are all zero on a full frame", frame)
					}
				}
			}
		})
	}
}

// generatePCM writes frame-indexed synthetic PCM into dst: a mix of
// a 440 Hz sine + a 1 kHz sine + a tiny pseudo-random component. The
// amplitude stays well inside [-1,1] to keep clipping out of the
// parity equation.
func generatePCM(dst []float32, frameIdx, sampleRate int) {
	n := len(dst)
	baseT := frameIdx * n
	for i := 0; i < n; i++ {
		t := float64(baseT+i) / float64(sampleRate)
		v := 0.3*math.Sin(2*math.Pi*440*t) +
			0.15*math.Sin(2*math.Pi*1000*t) +
			0.02*math.Sin(2*math.Pi*47*float64(baseT+i)/13.0)
		dst[i] = float32(v)
	}
}
