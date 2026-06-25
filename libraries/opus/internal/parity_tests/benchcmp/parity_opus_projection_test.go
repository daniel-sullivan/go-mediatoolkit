//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// ambisonicConfigs lists the (channels, order+1) pairs the projection
// encoder supports under mapping_family=3. Third-order (16ch) covers
// the most common pro audio use-case. We also cover 4-ch FOA and 9-ch
// SOA so all three small-order code paths are exercised.
func ambisonicConfigs() []struct {
	name     string
	channels int
} {
	return []struct {
		name     string
		channels int
	}{
		{"FOA_4ch", 4},
		{"SOA_9ch", 9},
		{"TOA_16ch", 16},
	}
}

// TestParity_OpusProjectionEncoder_Init sweeps (Fs, channels,
// mapping_family, application) tuples through both Go and C projection
// encoders and asserts that streams/coupled/channels/demixing-matrix
// shape all agree. We do NOT compare arena size (the Go port uses a
// placeholder sizeof; the formula relies on the C pointer-width).
func TestParity_OpusProjectionEncoder_Init(t *testing.T) {
	type tc struct {
		name     string
		Fs       int32
		channels int
		family   int
		app      int
	}
	cases := []tc{}
	for _, a := range ambisonicConfigs() {
		for _, Fs := range []int32{48000, 24000} {
			for _, app := range []int{nativeopus.OPUS_APPLICATION_VOIP, nativeopus.OPUS_APPLICATION_AUDIO} {
				cases = append(cases, tc{
					name:     fmt.Sprintf("%s_Fs%d_app%d", a.name, Fs, app),
					Fs:       Fs,
					channels: a.channels,
					family:   3,
					app:      app,
				})
			}
		}
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cSnap, cRet := CProjEncInitAndSnapshot(c.Fs, c.channels, c.family, c.app)
			_, gm := buildFullGoMode(t)
			goH, gStreams, gCoupled, gRet := nativeopus.ExportProjectionAmbisonicsEncoderInit(
				c.Fs, c.channels, c.family, c.app,
				func(enc *nativeopus.OpusEncoder) int {
					return nativeopus.ExportSetEncoderCeltMode(enc, gm)
				})
			if cRet != gRet {
				t.Fatalf("return mismatch: C=%d Go=%d", cRet, gRet)
			}
			if cRet != nativeopus.OPUS_OK {
				return
			}
			if cSnap.NbStreams != gStreams || cSnap.NbCoupledStreams != gCoupled {
				t.Fatalf("streams/coupled mismatch: C=(%d,%d) Go=(%d,%d)",
					cSnap.NbStreams, cSnap.NbCoupledStreams, gStreams, gCoupled)
			}
			if cSnap.NbChannels != goH.NumChannels() {
				t.Fatalf("channels mismatch: C=%d Go=%d",
					cSnap.NbChannels, goH.NumChannels())
			}
			// Fetch Go demixing matrix via ctl + compare byte-for-byte.
			var gSize int32
			if ret := goH.Ctl(nativeopus.OPUS_PROJECTION_GET_DEMIXING_MATRIX_SIZE_REQUEST,
				&gSize); ret != nativeopus.OPUS_OK {
				t.Fatalf("Go GET_DEMIXING_MATRIX_SIZE: %d", ret)
			}
			if gSize != cSnap.DemixSize {
				t.Fatalf("demix size mismatch: C=%d Go=%d", cSnap.DemixSize, gSize)
			}
			var gGain int32
			if ret := goH.Ctl(nativeopus.OPUS_PROJECTION_GET_DEMIXING_MATRIX_GAIN_REQUEST,
				&gGain); ret != nativeopus.OPUS_OK {
				t.Fatalf("Go GET_DEMIXING_MATRIX_GAIN: %d", ret)
			}
			if gGain != cSnap.DemixGain {
				t.Fatalf("demix gain mismatch: C=%d Go=%d", cSnap.DemixGain, gGain)
			}
			gBuf := make([]byte, gSize)
			if ret := goH.Ctl(nativeopus.OPUS_PROJECTION_GET_DEMIXING_MATRIX_REQUEST,
				gBuf, gSize); ret != nativeopus.OPUS_OK {
				t.Fatalf("Go GET_DEMIXING_MATRIX: %d", ret)
			}
			if !bytes.Equal(gBuf, cSnap.DemixBytes) {
				t.Fatalf("demix matrix bytes differ. len C=%d Go=%d", len(cSnap.DemixBytes), len(gBuf))
			}
		})
	}
}

// TestParity_OpusProjectionEncode_Matrix drives synthetic ambisonic
// PCM through both encoders and asserts byte-exact bitstream parity
// per frame across multiple configurations.
func TestParity_OpusProjectionEncode_Matrix(t *testing.T) {
	_, gm := buildFullGoMode(t)

	type layout struct {
		name     string
		channels int
	}
	layouts := []layout{
		{"FOA_4ch", 4},
		{"SOA_9ch", 9},
		{"TOA_16ch", 16},
	}
	sampleRates := []int32{48000, 24000}
	frameMsList := []int{10, 20}
	bitrates := []int32{64000, 128000}

	type tc struct {
		name      string
		Fs        int32
		frameMs   int
		bitrate   int32
		layoutIdx int
	}
	var cases []tc
	for _, Fs := range sampleRates {
		for _, fm := range frameMsList {
			for _, br := range bitrates {
				for li, l := range layouts {
					cases = append(cases, tc{
						name: fmt.Sprintf("%s_Fs%d_%dms_%dbps", l.name, Fs, fm, br),
						Fs:   Fs, frameMs: fm, bitrate: br, layoutIdx: li,
					})
				}
			}
		}
	}

	nFrames := 3
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			l := layouts[c.layoutIdx]
			frameSize := int(c.Fs) * c.frameMs / 1000

			cEnc, _ := NewCProjEncoder(int(c.Fs), l.channels, 3, nativeopus.OPUS_APPLICATION_AUDIO)
			if cEnc == nil {
				t.Skip("C projection encoder create failed")
			}
			defer cEnc.Destroy()
			cEnc.SetBitrate(int(c.bitrate))
			cEnc.SetComplexity(10)

			goH, _, _, gret := nativeopus.ExportProjectionAmbisonicsEncoderInit(
				c.Fs, l.channels, 3, nativeopus.OPUS_APPLICATION_AUDIO,
				func(enc *nativeopus.OpusEncoder) int {
					return nativeopus.ExportSetEncoderCeltMode(enc, gm)
				})
			if gret != nativeopus.OPUS_OK {
				t.Fatalf("Go projection encoder init: %d", gret)
			}
			if ret := goH.Ctl(nativeopus.OPUS_SET_BITRATE_REQUEST,
				int32(c.bitrate)); ret != nativeopus.OPUS_OK {
				t.Fatalf("Go SET_BITRATE: %d", ret)
			}
			if ret := goH.Ctl(nativeopus.OPUS_SET_COMPLEXITY_REQUEST,
				int32(10)); ret != nativeopus.OPUS_OK {
				t.Fatalf("Go SET_COMPLEXITY: %d", ret)
			}

			pcmBuf := make([]float32, frameSize*l.channels)
			cPkt := make([]byte, 8000*l.channels)
			goPkt := make([]byte, 8000*l.channels)

			for f := 0; f < nFrames; f++ {
				generateAmbisonicPCM(pcmBuf, f, int(c.Fs), l.channels)

				cn := cEnc.EncodeFloat(pcmBuf, frameSize, cPkt)
				if cn < 0 {
					t.Fatalf("frame %d: C encode returned %d", f, cn)
				}
				gn := goH.EncodeFloat(pcmBuf, frameSize, goPkt)
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
					t.Fatalf("frame %d: packet mismatch at byte %d\n  C : % x\n  Go: % x",
						f, first, cPkt[:cn], goPkt[:gn])
				}
			}
		})
	}
}

// generateAmbisonicPCM fills dst with an interleaved synthetic ambisonic
// signal. Channel 0 (W) gets a dominant tone; higher-order channels get
// smaller tones at offset frequencies so the mixing matrix sees a
// non-degenerate vector on every column.
func generateAmbisonicPCM(dst []float32, frameIdx, sampleRate, channels int) {
	n := len(dst) / channels
	baseT := frameIdx * n
	for i := 0; i < n; i++ {
		t := float64(baseT+i) / float64(sampleRate)
		// Channel 0 dominant tone.
		dst[i*channels+0] = float32(0.40 * math.Sin(2*math.Pi*330.0*t))
		for ch := 1; ch < channels; ch++ {
			fch := 210.0 + 70.0*float64(ch)
			phase := 0.07 * float64(ch)
			v := 0.15*math.Sin(2*math.Pi*fch*t+phase) +
				0.05*math.Sin(2*math.Pi*(fch*1.51)*t)
			dst[i*channels+ch] = float32(v)
		}
	}
}

// TestParity_OpusProjectionDecoder_Init sweeps init configurations and
// asserts the per-stream counts + demixing matrix shape match.
func TestParity_OpusProjectionDecoder_Init(t *testing.T) {
	_, gm := loadGoMode(t, 48000, 960)
	installModeTables(t, gm)

	for _, a := range ambisonicConfigs() {
		for _, Fs := range []int32{48000, 24000} {
			name := fmt.Sprintf("%s_Fs%d", a.name, Fs)
			t.Run(name, func(t *testing.T) {
				// Use the encoder-side to obtain a valid demixing matrix.
				cEnc, demix := NewCProjEncoder(int(Fs), a.channels, 3, nativeopus.OPUS_APPLICATION_AUDIO)
				if cEnc == nil {
					t.Skip("C projection encoder create failed")
				}
				defer cEnc.Destroy()
				streams := cEnc.Streams()
				coupled := cEnc.Coupled()

				cDec := NewCProjDecoder(int(Fs), a.channels, streams, coupled, demix)
				if cDec == nil {
					t.Fatalf("C projection decoder create failed")
				}
				defer cDec.Destroy()

				gH, gRet := nativeopus.ExportProjectionDecoderInit(gm, Fs, a.channels,
					streams, coupled, demix)
				if gRet != nativeopus.OPUS_OK {
					t.Fatalf("Go projection decoder init: %d", gRet)
				}
				if gH.NumStreams() != streams || gH.NumCoupled() != coupled ||
					gH.NumChannels() != a.channels {
					t.Fatalf("layout mismatch: go=(%d,%d,%d) c=(%d,%d,%d)",
						gH.NumStreams(), gH.NumCoupled(), gH.NumChannels(),
						streams, coupled, a.channels)
				}
				if gH.DemixingRows() != a.channels {
					t.Fatalf("demixing rows mismatch: got %d want %d",
						gH.DemixingRows(), a.channels)
				}
				if gH.DemixingCols() != streams+coupled {
					t.Fatalf("demixing cols mismatch: got %d want %d",
						gH.DemixingCols(), streams+coupled)
				}
			})
		}
	}
}

// TestParity_OpusProjectionDecode_Matrix encodes synthetic ambisonic PCM
// via the C projection encoder, then decodes through both C and Go
// projection decoders. Enforces bit-exact float output.
func TestParity_OpusProjectionDecode_Matrix(t *testing.T) {
	_, gm := loadGoMode(t, 48000, 960)
	installModeTables(t, gm)

	type cfg struct {
		name     string
		Fs       int
		channels int
		frameMs  int
		bitrate  int
	}
	cfgs := []cfg{
		{"FOA/48k/20ms", 48000, 4, 20, 128000},
		{"FOA/48k/10ms", 48000, 4, 10, 128000},
		{"FOA/24k/20ms", 24000, 4, 20, 96000},
		{"SOA/48k/20ms", 48000, 9, 20, 256000},
		{"TOA/48k/20ms", 48000, 16, 20, 384000},
		{"TOA/48k/10ms", 48000, 16, 10, 384000},
	}
	for _, c := range cfgs {
		c := c
		t.Run(c.name, func(t *testing.T) {
			projDecoderParitySweep(t, gm, c.Fs, c.channels, c.frameMs, c.bitrate, 4,
				int64(0xBAD5EED+c.Fs+c.channels*1001))
		})
	}
}

func projDecoderParitySweep(t *testing.T, gm nativeopus.CeltModeHandle,
	Fs, channels, frameMs, bitrate, nFrames int, seed int64) {
	t.Helper()
	frameSamples := Fs * frameMs / 1000

	cEnc, demix := NewCProjEncoder(Fs, channels, 3, nativeopus.OPUS_APPLICATION_AUDIO)
	if cEnc == nil {
		t.Fatalf("C projection encoder create failed")
	}
	defer cEnc.Destroy()
	cEnc.SetBitrate(bitrate)
	cEnc.SetComplexity(10)
	streams := cEnc.Streams()
	coupled := cEnc.Coupled()

	cDec := NewCProjDecoder(Fs, channels, streams, coupled, demix)
	if cDec == nil {
		t.Fatalf("C projection decoder create failed")
	}
	defer cDec.Destroy()

	gDec, initRet := nativeopus.ExportProjectionDecoderInit(gm, int32(Fs), channels,
		streams, coupled, demix)
	if initRet != nativeopus.OPUS_OK {
		t.Fatalf("Go projection decoder init: %d", initRet)
	}

	r := rand.New(rand.NewSource(seed))
	pkt := make([]byte, 8000*channels)
	pcmIn := make([]float32, frameSamples*channels)
	pcmOutC := make([]float32, frameSamples*channels)
	pcmOutG := make([]float32, frameSamples*channels)

	for fi := 0; fi < nFrames; fi++ {
		for c := 0; c < channels; c++ {
			f1 := 220.0 + 50.0*float64((fi+c)%6)
			f2 := 680.0 + 70.0*float64((fi*3+c)%5)
			amp := 0.22
			for i := 0; i < frameSamples; i++ {
				t := float64(fi*frameSamples+i) / float64(Fs)
				s := amp*math.Sin(2*math.Pi*f1*t) + 0.4*amp*math.Sin(2*math.Pi*f2*t)
				s += (r.Float64()*2 - 1) * 0.015
				pcmIn[i*channels+c] = float32(s)
			}
		}
		n := cEnc.EncodeFloat(pcmIn, frameSamples, pkt)
		if n <= 0 {
			t.Fatalf("frame %d: C encode returned %d", fi, n)
		}
		packet := pkt[:n]

		cRet := cDec.DecodeFloat(packet, pcmOutC, frameSamples)
		if cRet <= 0 {
			t.Fatalf("frame %d: C decode returned %d", fi, cRet)
		}
		gRet := gDec.DecodeFloat(packet, pcmOutG, frameSamples, 0)
		if gRet <= 0 {
			t.Fatalf("frame %d: Go decode returned %d", fi, gRet)
		}
		if cRet != gRet {
			t.Fatalf("frame %d: ret mismatch C=%d Go=%d", fi, cRet, gRet)
		}
		for i := 0; i < cRet*channels; i++ {
			if pcmOutC[i] != pcmOutG[i] {
				sample := i / channels
				chn := i % channels
				t.Fatalf("frame %d: mismatch sample=%d ch=%d go=%g c=%g (bits go=%#x c=%#x)",
					fi, sample, chn, pcmOutG[i], pcmOutC[i],
					math.Float32bits(pcmOutG[i]), math.Float32bits(pcmOutC[i]))
			}
		}
	}
}
