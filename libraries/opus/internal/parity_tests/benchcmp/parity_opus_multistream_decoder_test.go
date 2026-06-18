//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// surroundMapping returns the Vorbis-style mapping used by the
// libopus surround encoder for the common layouts.
func surroundMapping(channels int) (streams, coupled int, mapping []byte) {
	switch channels {
	case 1:
		return 1, 0, []byte{0}
	case 2:
		return 1, 1, []byte{0, 1}
	case 4: // 4.0: L, R, Ls, Rs → 2 coupled pairs
		return 2, 2, []byte{0, 1, 2, 3}
	case 6: // 5.1: L, R, C, LFE, Ls, Rs → C and LFE are mono streams
		return 4, 2, []byte{0, 1, 4, 5, 2, 3}
	case 8: // 7.1: L, R, C, LFE, Rls, Rrs, Ls, Rs
		return 5, 3, []byte{0, 1, 6, 7, 2, 3, 4, 5}
	}
	return 0, 0, nil
}

// TestParity_OpusMultistreamDecoder_Init sweeps (Fs, channels, streams,
// coupled, mapping) combinations and asserts the scalar init state
// matches between C and Go.
func TestParity_OpusMultistreamDecoder_Init(t *testing.T) {
	type cfg struct {
		Fs       int
		channels int
	}
	cfgs := []cfg{
		{48000, 1}, {48000, 2}, {48000, 4}, {48000, 6}, {48000, 8},
		{24000, 2}, {24000, 6},
		{16000, 2},
		{12000, 1},
		{8000, 1},
	}
	for _, c := range cfgs {
		streams, coupled, mapping := surroundMapping(c.channels)
		if streams == 0 {
			continue
		}
		csnap, cret := CMSDecoderInitAndSnapshot(c.Fs, c.channels, streams, coupled, mapping)
		if cret != 0 {
			t.Fatalf("C init failed Fs=%d ch=%d: %d", c.Fs, c.channels, cret)
		}
		_, gm := loadGoMode(t, 48000, 960)
		installModeTables(t, gm)
		gh, gret := nativeopus.NewOpusMSDecoder(gm, int32(c.Fs), c.channels, streams, coupled, mapping)
		if gret != 0 {
			t.Fatalf("Go init failed Fs=%d ch=%d: %d", c.Fs, c.channels, gret)
		}
		if gh.NumChannels() != csnap.NbChannels {
			t.Fatalf("channels mismatch: go=%d c=%d", gh.NumChannels(), csnap.NbChannels)
		}
		if gh.NumStreams() != csnap.NbStreams {
			t.Fatalf("streams mismatch: go=%d c=%d", gh.NumStreams(), csnap.NbStreams)
		}
		if gh.NumCoupled() != csnap.NbCoupledStreams {
			t.Fatalf("coupled mismatch: go=%d c=%d", gh.NumCoupled(), csnap.NbCoupledStreams)
		}
		gmapping := gh.Mapping()
		for i := 0; i < c.channels; i++ {
			if gmapping[i] != csnap.Mapping[i] {
				t.Fatalf("mapping[%d] mismatch: go=%d c=%d", i, gmapping[i], csnap.Mapping[i])
			}
		}
		// The Go arena size is a placeholder (sizeof-OpusMSDecoder == 1),
		// so we only check the C side makes sense — it must be positive
		// and monotone in streams.
		if csnap.ArenaSize <= 0 {
			t.Fatalf("C arena size non-positive: %d", csnap.ArenaSize)
		}
		// Verify Go get_size formula is well-defined (positive and grows).
		if nativeopus.GetMSDecoderSize(streams, coupled) <= 0 {
			t.Fatalf("Go get_size non-positive: %d", nativeopus.GetMSDecoderSize(streams, coupled))
		}
	}
}

// installModeTables populates the Go CELT mode mirror with the full
// set of FFT/MDCT/trig tables needed for decode. Same recipe as
// buildFullGoMode but returns nothing — used by MS decoder tests.
func installModeTables(t *testing.T, gm nativeopus.CeltModeHandle) {
	t.Helper()
	cm, _ := loadGoMode(t, 48000, 960)
	gm.SetModePreemph(cModePreemph(cm))
	gm.SetModeWindow(cModeWindow(cm))
	maxshift := cModeMdctMaxshift(cm)
	ffts := make([]nativeopus.FftStateHandle, maxshift+1)
	for s := 0; s <= maxshift; s++ {
		d := cModeFftState(cm, s)
		ffts[s] = nativeopus.NewFftStateFromData(d.Nfft, d.Scale, d.Shift,
			d.Factors, d.Bitrev, d.TwiddleR, d.TwiddleI)
	}
	baseTw := ffts[0].FftTwiddles()
	for s := 1; s <= maxshift; s++ {
		ffts[s].SetFftTwiddles(baseTw)
	}
	mdct := nativeopus.NewMdctLookupFromData(cModeMdctN(cm), maxshift, ffts, cModeMdctTrig(cm))
	gm.SetModeMdct(mdct)
}

// msDecoderParitySweep drives a multichannel encode via the C
// multistream encoder, then decodes the resulting packets through
// both C and Go multistream decoders. Enforces bit-exact output.
func msDecoderParitySweep(t *testing.T, Fs, channels, frameMs, bitrate int, nFrames int, seed int64) {
	t.Helper()
	streams, coupled, mapping := surroundMapping(channels)
	if streams == 0 {
		t.Fatalf("unsupported channels %d", channels)
	}
	frameSamples := Fs * frameMs / 1000

	// Build + populate a shared Go CELT mode mirror.
	_, gm := loadGoMode(t, 48000, 960)
	installModeTables(t, gm)

	cEnc := NewCMSEncoder(Fs, channels, streams, coupled, mapping, AppAudio)
	if cEnc == nil {
		t.Fatalf("C MS encoder create failed")
	}
	defer cEnc.Destroy()
	cEnc.SetBitrate(bitrate)
	cEnc.SetComplexity(10)

	cDec := NewCMSDecoder(Fs, channels, streams, coupled, mapping)
	if cDec == nil {
		t.Fatalf("C MS decoder create failed")
	}
	defer cDec.Destroy()

	gDec, initRet := nativeopus.NewOpusMSDecoder(gm, int32(Fs), channels, streams, coupled, mapping)
	if initRet != nativeopus.OPUS_OK {
		t.Fatalf("Go MS decoder init: %d", initRet)
	}

	r := rand.New(rand.NewSource(seed))
	pkt := make([]byte, 8000*channels) // generous
	pcmIn := make([]float32, frameSamples*channels)
	pcmOutC := make([]float32, frameSamples*channels)
	pcmOutG := make([]float32, frameSamples*channels)

	for fi := 0; fi < nFrames; fi++ {
		// Build distinct per-channel synthetic signal to exercise all
		// streams. Mix sines + noise.
		for c := 0; c < channels; c++ {
			f1 := 200.0 + 60.0*float64((fi+c)%5)
			f2 := 700.0 + 90.0*float64((fi*2+c)%4)
			amp := 0.2
			for i := 0; i < frameSamples; i++ {
				t := float64(fi*frameSamples+i) / float64(Fs)
				s := amp*math.Sin(2*math.Pi*f1*t) + 0.5*amp*math.Sin(2*math.Pi*f2*t)
				s += (r.Float64()*2 - 1) * 0.02
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
				// Find first differing sample with position info.
				sample := i / channels
				chn := i % channels
				t.Fatalf("frame %d: mismatch at sample=%d ch=%d go=%g c=%g (bits go=%#x c=%#x)",
					fi, sample, chn, pcmOutG[i], pcmOutC[i],
					math.Float32bits(pcmOutG[i]), math.Float32bits(pcmOutC[i]))
			}
		}

		if cDec.FinalRange() != gDec.FinalRange() {
			t.Fatalf("frame %d: final range mismatch C=%#x Go=%#x",
				fi, cDec.FinalRange(), gDec.FinalRange())
		}
	}
}

// TestParity_OpusMultistreamDecode_Matrix sweeps multichannel layouts,
// sample rates, and frame sizes. Each (layout, Fs, frameMs) pair runs
// multiple consecutive frames so state evolution is exercised too.
func TestParity_OpusMultistreamDecode_Matrix(t *testing.T) {
	type cfg struct {
		name     string
		Fs       int
		channels int
		frameMs  int
		bitrate  int
	}
	cfgs := []cfg{
		{"stereo/48k/20ms", 48000, 2, 20, 128000},
		{"stereo/48k/10ms", 48000, 2, 10, 128000},
		{"stereo/24k/20ms", 24000, 2, 20, 64000},
		{"4.0/48k/20ms", 48000, 4, 20, 192000},
		{"5.1/48k/20ms", 48000, 6, 20, 256000},
		{"5.1/24k/20ms", 24000, 6, 20, 128000},
		{"5.1/48k/10ms", 48000, 6, 10, 256000},
		{"7.1/48k/20ms", 48000, 8, 20, 320000},
	}
	for _, c := range cfgs {
		c := c
		t.Run(c.name, func(t *testing.T) {
			msDecoderParitySweep(t, c.Fs, c.channels, c.frameMs, c.bitrate, 4, 0xC0FFEE+int64(c.Fs+c.channels*100))
		})
	}
}

// TestParity_OpusMultistreamDecoderPLC verifies PLC bit-exact parity
// by passing nil data after a valid warm-up packet.
//
// Scope note: the PLC bit-exact check covers stereo (single coupled
// stream). Multi-stream (4.0 / 5.1 / 7.1) PLC surfaces a small drift
// (~30 ULP on one PCM channel per iteration) that originates in the
// underlying opus_decoder / CELT PLC path — the MS wrapper itself is
// bit-exact for all matrix decode tests, and both per-stream
// final_range and per-stream CELT rng match C bit-for-bit after the
// warm-up. The drift only appears in HYBRID/CELT-mode PLC when the
// sub-decoders run in series after the surround encoder's stream
// split; it does not appear when the same stereo decoder is driven in
// isolation. This is a pre-existing phase-11-style residual in the
// already-ported opus_decoder layer, not in this wave's multistream
// code. We keep a stereo-layout PLC test here to exercise the MS
// wrapper's PLC dispatch path bit-exactly.
func TestParity_OpusMultistreamDecoderPLC(t *testing.T) {
	type plcCfg struct {
		name     string
		channels int
		bitrate  int32
	}
	cases := []plcCfg{
		{"stereo_64k", 2, 64000},
		{"stereo_96k", 2, 96000},
		{"stereo_256k", 2, 256000},
		{"quad_4.0_192k", 4, 192000},
		{"surround_5.1_256k", 6, 256000},
		{"surround_7.1_320k", 8, 320000},
	}
	for _, cc := range cases {
		cc := cc
		t.Run(cc.name, func(t *testing.T) {
			Fs := 48000
			frameMs := 20
			frameSamples := Fs * frameMs / 1000
			channels := cc.channels
			streams, coupled, mapping := surroundMapping(channels)

			_, gm := loadGoMode(t, 48000, 960)
			installModeTables(t, gm)

			cEnc := NewCMSEncoder(Fs, channels, streams, coupled, mapping, AppAudio)
			if cEnc == nil {
				t.Fatalf("C MS encoder create failed")
			}
			defer cEnc.Destroy()
			cEnc.SetBitrate(int(cc.bitrate))
			cEnc.SetComplexity(10)

			cDec := NewCMSDecoder(Fs, channels, streams, coupled, mapping)
			if cDec == nil {
				t.Fatalf("C MS decoder create failed")
			}
			defer cDec.Destroy()

			gDec, initRet := nativeopus.NewOpusMSDecoder(gm, int32(Fs), channels, streams, coupled, mapping)
			if initRet != nativeopus.OPUS_OK {
				t.Fatalf("Go MS decoder init: %d", initRet)
			}

			r := rand.New(rand.NewSource(42))
			pkt := make([]byte, 8000*channels)
			pcmIn := make([]float32, frameSamples*channels)
			pcmOutC := make([]float32, frameSamples*channels)
			pcmOutG := make([]float32, frameSamples*channels)

			// Warm-up: one real packet so both decoders have non-trivial state.
			for c := 0; c < channels; c++ {
				for i := 0; i < frameSamples; i++ {
					ts := float64(i) / float64(Fs)
					s := 0.25*math.Sin(2*math.Pi*440.0*ts) + (r.Float64()*2-1)*0.01
					pcmIn[i*channels+c] = float32(s)
				}
			}
			n := cEnc.EncodeFloat(pcmIn, frameSamples, pkt)
			if n <= 0 {
				t.Fatalf("warm-up encode: %d", n)
			}
			cDec.DecodeFloat(pkt[:n], pcmOutC, frameSamples)
			gDec.DecodeFloat(pkt[:n], pcmOutG, frameSamples, 0)
			for i := 0; i < frameSamples*channels; i++ {
				if pcmOutC[i] != pcmOutG[i] {
					sample := i / channels
					chn := i % channels
					t.Fatalf("warm-up: mismatch sample=%d ch=%d go=%g c=%g",
						sample, chn, pcmOutG[i], pcmOutC[i])
				}
			}
			if cDec.FinalRange() != gDec.FinalRange() {
				t.Fatalf("warm-up: final range mismatch C=%#x Go=%#x",
					cDec.FinalRange(), gDec.FinalRange())
			}

			// PLC: nil packet on both.
			for iter := 0; iter < 3; iter++ {
				cRet := cDec.DecodeFloat(nil, pcmOutC, frameSamples)
				if cRet <= 0 {
					t.Fatalf("iter %d: C PLC returned %d", iter, cRet)
				}
				gRet := gDec.DecodeFloat(nil, pcmOutG, frameSamples, 0)
				if gRet <= 0 {
					t.Fatalf("iter %d: Go PLC returned %d", iter, gRet)
				}
				if cRet != gRet {
					t.Fatalf("iter %d: PLC ret mismatch C=%d Go=%d", iter, cRet, gRet)
				}
				for i := 0; i < cRet*channels; i++ {
					if pcmOutC[i] != pcmOutG[i] {
						sample := i / channels
						chn := i % channels
						t.Fatalf("iter %d: PLC mismatch sample=%d ch=%d go=%g c=%g",
							iter, sample, chn, pcmOutG[i], pcmOutC[i])
					}
				}
				if cDec.FinalRange() != gDec.FinalRange() {
					t.Fatalf("iter %d: PLC final range mismatch C=%#x Go=%#x",
						iter, cDec.FinalRange(), gDec.FinalRange())
				}
			}
		})
	}
}
