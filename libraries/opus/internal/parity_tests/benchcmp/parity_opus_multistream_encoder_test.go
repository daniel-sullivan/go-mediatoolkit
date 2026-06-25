//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_OpusMultistreamEncoder_Init drives a sweep of
// (Fs, channels, streams, coupled, mapping, application) tuples through
// both Go and C opus_multistream_encoder_init and compares the resulting
// scalar snapshots.
func TestParity_OpusMultistreamEncoder_Init(t *testing.T) {
	type tc struct {
		name     string
		Fs       int32
		channels int
		streams  int
		coupled  int
		mapping  []byte
		app      int
	}
	cases := []tc{
		// Stereo 2ch: 1 stream, 1 coupled.
		{"48k_stereo_VOIP", 48000, 2, 1, 1, []byte{0, 1}, nativeopus.OPUS_APPLICATION_VOIP},
		{"48k_stereo_AUDIO", 48000, 2, 1, 1, []byte{0, 1}, nativeopus.OPUS_APPLICATION_AUDIO},
		{"24k_stereo_AUDIO", 24000, 2, 1, 1, []byte{0, 1}, nativeopus.OPUS_APPLICATION_AUDIO},
		{"16k_stereo_VOIP", 16000, 2, 1, 1, []byte{0, 1}, nativeopus.OPUS_APPLICATION_VOIP},
		// Mono.
		{"48k_mono_VOIP", 48000, 1, 1, 0, []byte{0}, nativeopus.OPUS_APPLICATION_VOIP},
		{"48k_mono_AUDIO", 48000, 1, 1, 0, []byte{0}, nativeopus.OPUS_APPLICATION_AUDIO},
		// 5.1 using vorbis layout (6ch, 4 streams, 2 coupled).
		{"48k_5.1_AUDIO", 48000, 6, 4, 2, []byte{0, 4, 1, 2, 3, 5}, nativeopus.OPUS_APPLICATION_AUDIO},
		// 7.1 using vorbis layout (8ch, 5 streams, 3 coupled).
		{"48k_7.1_AUDIO", 48000, 8, 5, 3, []byte{0, 6, 1, 2, 3, 4, 5, 7}, nativeopus.OPUS_APPLICATION_AUDIO},
		// 4.0 quadraphonic (4ch, 2 streams, 2 coupled).
		{"48k_quad_AUDIO", 48000, 4, 2, 2, []byte{0, 1, 2, 3}, nativeopus.OPUS_APPLICATION_AUDIO},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cSnap, cRet := CMSEncoderInitAndSnapshot(c.Fs, c.channels, c.streams,
				c.coupled, c.mapping, c.app)
			gSnap, gRet := nativeopus.ExportMSEncoderInitAndSnapshot(c.Fs, c.channels,
				c.streams, c.coupled, c.mapping, c.app)
			if cRet != gRet {
				t.Fatalf("return mismatch: C=%d Go=%d", cRet, gRet)
			}
			if cRet != nativeopus.OPUS_OK {
				return
			}
			if cSnap.NbChannels != gSnap.NbChannels ||
				cSnap.NbStreams != gSnap.NbStreams ||
				cSnap.NbCoupledStreams != gSnap.NbCoupledStreams {
				t.Errorf("layout: C={%d,%d,%d} Go={%d,%d,%d}",
					cSnap.NbChannels, cSnap.NbStreams, cSnap.NbCoupledStreams,
					gSnap.NbChannels, gSnap.NbStreams, gSnap.NbCoupledStreams)
			}
			for i := 0; i < c.channels; i++ {
				if cSnap.Mapping[i] != gSnap.Mapping[i] {
					t.Errorf("mapping[%d]: C=%d Go=%d", i, cSnap.Mapping[i], gSnap.Mapping[i])
				}
			}
			if cSnap.LfeStream != gSnap.LfeStream {
				t.Errorf("lfe_stream: C=%d Go=%d", cSnap.LfeStream, gSnap.LfeStream)
			}
			if cSnap.Application != gSnap.Application {
				t.Errorf("application: C=%d Go=%d", cSnap.Application, gSnap.Application)
			}
			if cSnap.Fs != gSnap.Fs {
				t.Errorf("Fs: C=%d Go=%d", cSnap.Fs, gSnap.Fs)
			}
			if cSnap.VariableDuration != gSnap.VariableDuration {
				t.Errorf("variable_duration: C=%d Go=%d", cSnap.VariableDuration, gSnap.VariableDuration)
			}
			if cSnap.MappingType != gSnap.MappingType {
				t.Errorf("mapping_type: C=%d Go=%d", cSnap.MappingType, gSnap.MappingType)
			}
			if cSnap.BitrateBps != gSnap.BitrateBps {
				t.Errorf("bitrate_bps: C=%d Go=%d", cSnap.BitrateBps, gSnap.BitrateBps)
			}
		})
	}
}

// TestParity_OpusMultistreamEncoder_SurroundInit exercises
// opus_multistream_surround_encoder_init, which computes the mapping
// + streams/coupled from the mapping_family.
func TestParity_OpusMultistreamEncoder_SurroundInit(t *testing.T) {
	type tc struct {
		name     string
		Fs       int32
		channels int
		family   int
		app      int
	}
	cases := []tc{
		{"family0_mono", 48000, 1, 0, nativeopus.OPUS_APPLICATION_AUDIO},
		{"family0_stereo", 48000, 2, 0, nativeopus.OPUS_APPLICATION_AUDIO},
		{"family1_mono", 48000, 1, 1, nativeopus.OPUS_APPLICATION_AUDIO},
		{"family1_stereo", 48000, 2, 1, nativeopus.OPUS_APPLICATION_AUDIO},
		{"family1_3ch", 48000, 3, 1, nativeopus.OPUS_APPLICATION_AUDIO},
		{"family1_quad", 48000, 4, 1, nativeopus.OPUS_APPLICATION_AUDIO},
		{"family1_5.1", 48000, 6, 1, nativeopus.OPUS_APPLICATION_AUDIO},
		{"family1_7.1", 48000, 8, 1, nativeopus.OPUS_APPLICATION_AUDIO},
		{"family255_4ch", 48000, 4, 255, nativeopus.OPUS_APPLICATION_AUDIO},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cSnap, cMap, cStreams, cCoupled, cRet := CMSSurroundEncoderInitAndSnapshot(
				c.Fs, c.channels, c.family, c.app)
			gSnap, gMap, gStreams, gCoupled, gRet := nativeopus.ExportMSSurroundEncoderInitAndSnapshot(
				c.Fs, c.channels, c.family, c.app)
			if cRet != gRet {
				t.Fatalf("return mismatch: C=%d Go=%d", cRet, gRet)
			}
			if cRet != nativeopus.OPUS_OK {
				return
			}
			if cStreams != gStreams || cCoupled != gCoupled {
				t.Fatalf("streams/coupled: C={%d,%d} Go={%d,%d}", cStreams, cCoupled, gStreams, gCoupled)
			}
			if !bytes.Equal(cMap, gMap) {
				t.Fatalf("mapping: C=%v Go=%v", cMap, gMap)
			}
			if cSnap.MappingType != gSnap.MappingType {
				t.Errorf("mapping_type: C=%d Go=%d", cSnap.MappingType, gSnap.MappingType)
			}
			if cSnap.LfeStream != gSnap.LfeStream {
				t.Errorf("lfe_stream: C=%d Go=%d", cSnap.LfeStream, gSnap.LfeStream)
			}
		})
	}
}

// TestParity_OpusMultistreamEncoder_Encode_Matrix pushes real
// multichannel PCM through both C and Go multistream encoders and
// asserts byte-exact bitstream equality per frame across the supported
// channel-layout / Fs / frame-size / bitrate / application matrix.
func TestParity_OpusMultistreamEncoder_Encode_Matrix(t *testing.T) {
	_, gm := buildFullGoMode(t)

	type layout struct {
		name     string
		channels int
		streams  int
		coupled  int
		mapping  []byte
	}
	layouts := []layout{
		{"stereo", 2, 1, 1, []byte{0, 1}},
		{"quad", 4, 2, 2, []byte{0, 1, 2, 3}},
		{"5.1", 6, 4, 2, []byte{0, 4, 1, 2, 3, 5}},
		{"7.1", 8, 5, 3, []byte{0, 6, 1, 2, 3, 4, 5, 7}},
	}
	apps := []struct {
		name string
		app  int
	}{
		{"VOIP", nativeopus.OPUS_APPLICATION_VOIP},
		{"AUDIO", nativeopus.OPUS_APPLICATION_AUDIO},
	}
	sampleRates := []int32{48000, 24000}
	frameMsList := []int{10, 20}
	bitrates := []int32{32000, 64000, 128000}

	type tc struct {
		name      string
		Fs        int32
		frameMs   int
		bitrate   int32
		app       int
		layoutIdx int
	}
	var cases []tc
	for _, Fs := range sampleRates {
		for _, fm := range frameMsList {
			for _, br := range bitrates {
				for _, a := range apps {
					for li, l := range layouts {
						cases = append(cases, tc{
							name: fmt.Sprintf("Fs%d_%s_%dms_%dbps_%s",
								Fs, l.name, fm, br, a.name),
							Fs: Fs, frameMs: fm, bitrate: br, app: a.app,
							layoutIdx: li,
						})
					}
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

			cEnc := NewCMSEncoder(int(c.Fs), l.channels, l.streams, l.coupled, l.mapping, c.app)
			if cEnc == nil {
				t.Skip("C multistream encoder create failed (config not supported)")
			}
			defer cEnc.Destroy()
			cEnc.SetBitrate(int(c.bitrate))

			goEnc, gerr := nativeopus.ExportMSEncoderCreate(c.Fs, l.channels, l.streams, l.coupled,
				l.mapping, c.app, func(enc *nativeopus.OpusEncoder) int {
					return nativeopus.ExportSetEncoderCeltMode(enc, gm)
				})
			if gerr != nativeopus.OPUS_OK {
				t.Fatalf("Go ms encoder create: %d", gerr)
			}
			if ret := nativeopus.ExportMSEncoderCtl(goEnc, nativeopus.OPUS_SET_BITRATE_REQUEST,
				int32(c.bitrate)); ret != nativeopus.OPUS_OK {
				t.Fatalf("Go SET_BITRATE: %d", ret)
			}

			pcmBuf := make([]float32, frameSize*l.channels)
			cPkt := make([]byte, 8000)
			goPkt := make([]byte, 8000)

			for f := 0; f < nFrames; f++ {
				generateMultichannelPCM(pcmBuf, f, int(c.Fs), l.channels)

				cn := cEnc.EncodeFloat(pcmBuf, frameSize, cPkt)
				if cn < 0 {
					t.Fatalf("frame %d: C encode returned %d", f, cn)
				}
				gn := int(nativeopus.ExportMSEncodeFloat(goEnc, pcmBuf, frameSize, goPkt, int32(len(goPkt))))
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

// generateMultichannelPCM fills dst with an interleaved synthetic signal.
// Each channel gets a distinct mix of sinusoids so the energy-mask /
// surround-analysis code exercises its spreading / LFE paths.
func generateMultichannelPCM(dst []float32, frameIdx, sampleRate, channels int) {
	n := len(dst) / channels
	baseT := frameIdx * n
	for i := 0; i < n; i++ {
		t := float64(baseT+i) / float64(sampleRate)
		for ch := 0; ch < channels; ch++ {
			// Vary frequency and amplitude per channel.
			fch := 220.0 + 110.0*float64(ch)
			phase := 0.1 * float64(ch)
			v := 0.30*math.Sin(2*math.Pi*fch*t+phase) +
				0.10*math.Sin(2*math.Pi*(fch*2.37)*t) +
				0.02*math.Sin(2*math.Pi*47*float64(baseT+i)/13.0)
			dst[i*channels+ch] = float32(v)
		}
	}
}
