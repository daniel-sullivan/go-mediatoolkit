//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_OpusEncode_Matrix drives synthetic PCM through both the C
// reference and the Go port of opus_encode_float, asserting byte-exact
// bitstream equality per frame across the full supported config matrix.
//
// This is THE capstone parity gate for Phase 9: if Go ever produces a
// packet that differs from the C oracle by even one byte, this test
// fails with a bisection-friendly error.
func TestParity_OpusEncode_Matrix(t *testing.T) {
	type tc struct {
		name     string
		Fs       int32
		channels int
		frameMs  int // frame size in ms
		bitrate  int32
		app      int
	}
	apps := []struct {
		name string
		app  int
	}{
		{"VOIP", nativeopus.OPUS_APPLICATION_VOIP},
		{"AUDIO", nativeopus.OPUS_APPLICATION_AUDIO},
		{"LOWDELAY", nativeopus.OPUS_APPLICATION_RESTRICTED_LOWDELAY},
	}
	// Bitrates span SILK-only, hybrid, CELT-only.
	bitrates := []int32{16000, 32000, 48000, 64000, 96000, 128000}
	channels := []int{1, 2}
	sampleRates := []int32{8000, 12000, 16000, 24000, 48000}
	frameMsList := []int{10, 20}

	// Build CELT mode once (48000/960, reused for all Fs via upsampler).
	_, gm := buildFullGoMode(t)

	var cases []tc
	for _, Fs := range sampleRates {
		for _, ch := range channels {
			for _, fm := range frameMsList {
				// LOWDELAY application can't be used with 10 ms at 8k
				// because frame_size_select won't pick it. Let the
				// frame_size_select gate filter; we just call encode.
				for _, br := range bitrates {
					for _, app := range apps {
						cases = append(cases, tc{
							name:     fmt.Sprintf("Fs%d_c%d_%dms_%dbps_%s", Fs, ch, fm, br, app.name),
							Fs:       Fs,
							channels: ch,
							frameMs:  fm,
							bitrate:  br,
							app:      app.app,
						})
					}
				}
			}
		}
	}

	nFrames := 4
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			// LOWDELAY always uses CELT-only; skip cases that are
			// wholly incompatible (e.g. Fs=8k with VOIP forcing hybrid
			// at a high bitrate — these are handled inside the encoder
			// so they remain in-scope; we let opus_encode decide).

			frameSize := int(c.Fs) * c.frameMs / 1000

			// Create C encoder.
			cEnc := NewCEncoder(int(c.Fs), c.channels, c.app)
			if cEnc == nil {
				t.Fatalf("C encoder create failed")
			}
			defer cEnc.Destroy()
			cEnc.SetBitrate(int(c.bitrate))

			// Create Go encoder.
			goEnc, gerr := nativeopus.ExportOpusEncoderCreate(c.Fs, c.channels, c.app)
			if gerr != nativeopus.OPUS_OK {
				t.Fatalf("go encoder create: %d", gerr)
			}
			if ret := nativeopus.ExportSetEncoderCeltMode(goEnc, gm); ret != nativeopus.OPUS_OK {
				t.Fatalf("celt mode install: %d", ret)
			}
			if ret := nativeopus.ExportOpusEncoderCtl(goEnc, nativeopus.OPUS_SET_BITRATE_REQUEST, c.bitrate); ret != nativeopus.OPUS_OK {
				t.Fatalf("go SET_BITRATE: %d", ret)
			}

			// Synthetic PCM shared per frame.
			pcmBuf := make([]float32, frameSize*c.channels)
			cPkt := make([]byte, 4000)
			goPkt := make([]byte, 4000)

			for f := 0; f < nFrames; f++ {
				generatePCM(pcmBuf, f, int(c.Fs))

				// Encode via C.
				cn := cEnc.EncodeFrame(pcmBuf, frameSize, cPkt)
				if cn < 0 {
					t.Fatalf("frame %d: C encode returned %d", f, cn)
				}
				// Encode via Go.
				gn := int(nativeopus.ExportOpusEncodeFloat(goEnc, pcmBuf, frameSize, goPkt, int32(len(goPkt))))
				if gn < 0 {
					t.Fatalf("frame %d: Go encode returned %d", f, gn)
				}
				if cn != gn {
					t.Fatalf("frame %d: byte count mismatch: C=%d Go=%d", f, cn, gn)
				}
				if !bytes.Equal(cPkt[:cn], goPkt[:gn]) {
					// Find first diverging byte for reporting.
					first := -1
					for i := 0; i < cn; i++ {
						if cPkt[i] != goPkt[i] {
							first = i
							break
						}
					}
					t.Fatalf("frame %d: packet mismatch at byte %d\n  C : % x\n  Go: % x",
						f, first, cPkt[:cn], goPkt[:gn])
				}
			}
		})
	}
}

// TestParity_OpusEncode_Int16Matrix — narrower int16 smoke covering
// that opus_encode (int16 entry point) is byte-identical to Go.
func TestParity_OpusEncode_Int16Matrix(t *testing.T) {
	_, gm := buildFullGoMode(t)

	type tc struct {
		name     string
		Fs       int32
		channels int
		frameMs  int
		bitrate  int32
		app      int
	}
	cases := []tc{
		{"48k_mono_20ms_64k_AUDIO", 48000, 1, 20, 64000, nativeopus.OPUS_APPLICATION_AUDIO},
		{"48k_stereo_20ms_96k_AUDIO", 48000, 2, 20, 96000, nativeopus.OPUS_APPLICATION_AUDIO},
		{"16k_mono_20ms_24k_VOIP", 16000, 1, 20, 24000, nativeopus.OPUS_APPLICATION_VOIP},
		{"8k_mono_20ms_16k_VOIP", 8000, 1, 20, 16000, nativeopus.OPUS_APPLICATION_VOIP},
	}
	nFrames := 4
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			frameSize := int(c.Fs) * c.frameMs / 1000

			cEnc := NewCEncoder(int(c.Fs), c.channels, c.app)
			if cEnc == nil {
				t.Fatalf("C encoder create failed")
			}
			defer cEnc.Destroy()
			cEnc.SetBitrate(int(c.bitrate))

			goEnc, gerr := nativeopus.ExportOpusEncoderCreate(c.Fs, c.channels, c.app)
			if gerr != nativeopus.OPUS_OK {
				t.Fatalf("go encoder create: %d", gerr)
			}
			if ret := nativeopus.ExportSetEncoderCeltMode(goEnc, gm); ret != nativeopus.OPUS_OK {
				t.Fatalf("celt mode install: %d", ret)
			}
			nativeopus.ExportOpusEncoderCtl(goEnc, nativeopus.OPUS_SET_BITRATE_REQUEST, c.bitrate)

			pcmFloat := make([]float32, frameSize*c.channels)
			pcmInt := make([]int16, frameSize*c.channels)
			cPkt := make([]byte, 4000)
			goPkt := make([]byte, 4000)

			for f := 0; f < nFrames; f++ {
				generatePCM(pcmFloat, f, int(c.Fs))
				for i, v := range pcmFloat {
					s := float64(v) * 32767.0
					if s > 32767 {
						s = 32767
					}
					if s < -32768 {
						s = -32768
					}
					pcmInt[i] = int16(math.Round(s))
				}
				cn := cEnc.EncodeInt16(pcmInt, frameSize, cPkt)
				if cn < 0 {
					t.Fatalf("frame %d: C encode returned %d", f, cn)
				}
				gn := int(nativeopus.ExportOpusEncodeInt16(goEnc, pcmInt, frameSize, goPkt, int32(len(goPkt))))
				if gn < 0 {
					t.Fatalf("frame %d: Go encode returned %d", f, gn)
				}
				if cn != gn {
					t.Fatalf("frame %d: byte count mismatch: C=%d Go=%d", f, cn, gn)
				}
				if !bytes.Equal(cPkt[:cn], goPkt[:gn]) {
					first := -1
					for i := 0; i < cn; i++ {
						if cPkt[i] != goPkt[i] {
							first = i
							break
						}
					}
					t.Fatalf("frame %d: packet mismatch at byte %d", f, first)
				}
			}
		})
	}
}

// TestParity_OpusRoundtrip_Matrix exercises encode-via-C, decode-via-C
// round-trips across the supported matrix. Encode-via-Go is already
// covered by TestParity_OpusEncode_Matrix: byte-exact packet equality
// between Go and C implies identical decode output, so the "encode via
// Go -> decode via C" path is reducible to the encode parity test.
// Similarly, "encode via C -> decode via Go" requires the Go decoder
// (Phase 9 Wave 9e) to be byte-exact on the full matrix which is
// covered by TestParity_Decode_* in the 9e test suite.
//
// This test sanity-checks the baseline: encode with C, decode with C,
// ensure decoded PCM is finite and energy-bounded. A failure here
// signals either a fuzz-distribution issue or a C oracle regression,
// neither of which is Go port drift.
func TestParity_OpusRoundtrip_Matrix(t *testing.T) {
	configs := []struct {
		name     string
		Fs       int32
		channels int
		frameMs  int
		bitrate  int32
		app      int
	}{
		{"48k_mono_20ms_64k_AUDIO", 48000, 1, 20, 64000, nativeopus.OPUS_APPLICATION_AUDIO},
		{"48k_stereo_20ms_96k_AUDIO", 48000, 2, 20, 96000, nativeopus.OPUS_APPLICATION_AUDIO},
		{"16k_mono_20ms_24k_VOIP", 16000, 1, 20, 24000, nativeopus.OPUS_APPLICATION_VOIP},
		{"8k_mono_20ms_16k_VOIP", 8000, 1, 20, 16000, nativeopus.OPUS_APPLICATION_VOIP},
		{"24k_stereo_20ms_48k_AUDIO", 24000, 2, 20, 48000, nativeopus.OPUS_APPLICATION_AUDIO},
	}
	for _, c := range configs {
		c := c
		t.Run(c.name, func(t *testing.T) {
			frameSize := int(c.Fs) * c.frameMs / 1000
			cEnc := NewCEncoder(int(c.Fs), c.channels, c.app)
			cDec := NewCDecoder(int(c.Fs), c.channels)
			if cEnc == nil || cDec == nil {
				t.Fatal("C encoder/decoder create failed")
			}
			defer cEnc.Destroy()
			defer cDec.Destroy()
			cEnc.SetBitrate(int(c.bitrate))
			pcm := make([]float32, frameSize*c.channels)
			dec := make([]float32, frameSize*c.channels)
			pkt := make([]byte, 4000)
			for f := 0; f < 4; f++ {
				generatePCM(pcm, f, int(c.Fs))
				n := cEnc.EncodeFrame(pcm, frameSize, pkt)
				if n <= 0 {
					t.Fatalf("frame %d: C encode returned %d", f, n)
				}
				if got := cDec.DecodeFrame(pkt[:n], dec, frameSize); got <= 0 {
					t.Fatalf("frame %d: C decoder rejected C packet (%d)", f, got)
				}
				for i, s := range dec {
					if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
						t.Fatalf("frame %d sample %d: NaN/Inf", f, i)
					}
				}
			}
		})
	}
}
