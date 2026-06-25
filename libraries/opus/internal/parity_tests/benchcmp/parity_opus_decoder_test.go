//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// buildOpusDecoderMode mirrors buildFullGoMode but scoped to building
// a CELT mode of the requested Fs/frame size, fully populated with
// trig/MDCT/FFT tables so opus_decode_frame can route through the Go
// CELT decoder without tripping nil-pointer derefs.
func buildOpusDecoderMode(t *testing.T, Fs, frameSize int) (cMode, nativeopus.CeltModeHandle) {
	cm, gm := loadGoMode(t, Fs, frameSize)
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
	return cm, gm
}

// decoderParitySweep drives a back-to-back encode + dual-decode for a
// single {Fs, channels, frameMs, bitrate, app, complexity} configuration
// over `nFrames` frames of synthetic PCM. Bit-exact parity is enforced
// per sample per frame.
func decoderParitySweep(t *testing.T, Fs, channels, frameMs, bitrate, app, complexity, nFrames int, seed int64) {
	t.Helper()
	frameSamples := Fs * frameMs / 1000

	// Build the Go CELT mode mirror. The CELT decoder internally always
	// runs at 48 kHz / 960-sample frames regardless of the decoder Fs;
	// internal SRC maps to the API rate. We therefore always install the
	// 48/960 mode so MDCT and FFT state matches the C side bit-for-bit.
	_, gm := buildOpusDecoderMode(t, 48000, 960)

	cEnc := NewCEncoder(Fs, channels, app)
	if cEnc == nil {
		t.Fatalf("C encoder create failed")
	}
	defer cEnc.Destroy()
	cEnc.SetBitrate(bitrate)
	cEnc.SetComplexity(complexity)

	cDec := NewCDecoder(Fs, channels)
	if cDec == nil {
		t.Fatalf("C decoder create failed")
	}
	defer cDec.Destroy()

	gDec, initRet := nativeopus.NewOpusDecoder(gm, int32(Fs), channels)
	if initRet != nativeopus.OPUS_OK {
		t.Fatalf("Go decoder init: %d", initRet)
	}

	r := rand.New(rand.NewSource(seed))
	pkt := make([]byte, 4000)
	pcmIn := make([]float32, frameSamples*channels)

	for fi := 0; fi < nFrames; fi++ {
		// Mix of sines + small noise per frame to avoid DTX and to vary
		// spectral content frame to frame.
		f1 := 220.0 + 50.0*float64(fi%5)
		f2 := 660.0 + 75.0*float64(fi%4)
		amp := 0.25
		for i := 0; i < frameSamples; i++ {
			t := float64(i) / float64(Fs)
			s := amp*math.Sin(2*math.Pi*f1*t) + (amp/2)*math.Sin(2*math.Pi*f2*t)
			s += 0.02 * (r.Float64()*2 - 1)
			for c := 0; c < channels; c++ {
				pcmIn[i*channels+c] = float32(s)
			}
		}

		n := cEnc.EncodeFrame(pcmIn, frameSamples, pkt)
		if n <= 0 {
			t.Fatalf("frame %d: encode returned %d", fi, n)
		}
		p := pkt[:n]

		outC := make([]float32, frameSamples*channels)
		outG := make([]float32, frameSamples*channels)
		retC := cDec.DecodeFrame(p, outC, frameSamples)
		retG := gDec.DecodeFloat(p, outG, frameSamples, 0)
		if retC != retG {
			t.Fatalf("frame %d: retC=%d retG=%d (Fs=%d ch=%d br=%d app=%d)",
				fi, retC, retG, Fs, channels, bitrate, app)
		}
		if retC < 0 {
			t.Fatalf("frame %d: decode returned %d", fi, retC)
		}

		mismatches := 0
		for i := 0; i < retC*channels; i++ {
			if math.Float32bits(outC[i]) != math.Float32bits(outG[i]) {
				if mismatches < 3 {
					t.Errorf("frame %d sample %d: C=%g Go=%g (Fs=%d ch=%d br=%d app=%d)",
						fi, i, outC[i], outG[i], Fs, channels, bitrate, app)
				}
				mismatches++
			}
		}
		if mismatches > 0 {
			t.Fatalf("frame %d: %d/%d samples mismatched", fi, mismatches, retC*channels)
		}
	}
}

// opusDecodeCase — one row of the decode parity matrix.
type opusDecodeCase struct {
	Fs, channels, frameMs, bitrate, app int
	tag                                 string
}

// opusDecodeMatrix — full configuration matrix covering all three
// internal modes (SILK-only, Hybrid, CELT-only), all supported sample
// rates, mono/stereo, 10 ms / 20 ms frames, and every reachable Opus
// bandwidth (NB/MB/WB/SWB/FB).
//
// Mode selection cheat-sheet (matches opus_encoder.c):
//   - SILK-only:    AppVOIP at low bitrates (<~ 24 kbps mono / ~40 kbps stereo).
//   - Hybrid:       AppVOIP at mid bitrates with Fs >= 24 kHz. Requires the
//     encoder to pick SUPERWIDEBAND or FULLBAND.
//   - CELT-only:    AppRestrictedLowDel any bitrate, or AppAudio at high
//     bitrates. We use AppRestrictedLowDel to force CELT.
func opusDecodeMatrix() []opusDecodeCase {
	cases := []opusDecodeCase{
		// ---- SILK-only — NB (8 kHz) ----
		{8000, 1, 10, 10000, AppVOIP, "silk_nb_mono_10ms"},
		{8000, 1, 20, 10000, AppVOIP, "silk_nb_mono_20ms"},
		{8000, 2, 20, 20000, AppVOIP, "silk_nb_stereo_20ms"},

		// ---- SILK-only — MB (12 kHz) ----
		{12000, 1, 10, 14000, AppVOIP, "silk_mb_mono_10ms"},
		{12000, 1, 20, 14000, AppVOIP, "silk_mb_mono_20ms"},
		{12000, 2, 20, 24000, AppVOIP, "silk_mb_stereo_20ms"},

		// ---- SILK-only — WB (16 kHz) ----
		{16000, 1, 10, 18000, AppVOIP, "silk_wb_mono_10ms"},
		{16000, 1, 20, 18000, AppVOIP, "silk_wb_mono_20ms"},
		{16000, 2, 20, 32000, AppVOIP, "silk_wb_stereo_20ms"},

		// ---- SILK-only WB via higher Fs (decoder resamples) ----
		// Fs = 24/48 kHz but encoder still picks SILK WB at these bitrates.
		{24000, 1, 20, 18000, AppVOIP, "silk_wb_mono_20ms_24k"},
		{48000, 1, 20, 18000, AppVOIP, "silk_wb_mono_20ms_48k"},
		{48000, 2, 20, 32000, AppVOIP, "silk_wb_stereo_20ms_48k"},

		// ---- Hybrid — SWB (24 kHz) ----
		// AppVOIP at 32 kbps at 24 kHz puts the encoder in Hybrid/SWB.
		{24000, 1, 20, 32000, AppVOIP, "hybrid_swb_mono_20ms"},
		{24000, 2, 20, 48000, AppVOIP, "hybrid_swb_stereo_20ms"},
		{24000, 1, 10, 32000, AppVOIP, "hybrid_swb_mono_10ms"},

		// ---- Hybrid — FB (48 kHz) ----
		{48000, 1, 20, 32000, AppVOIP, "hybrid_fb_mono_20ms"},
		{48000, 2, 20, 48000, AppVOIP, "hybrid_fb_stereo_20ms"},
		{48000, 1, 10, 32000, AppVOIP, "hybrid_fb_mono_10ms"},

		// ---- CELT-only — all bandwidths via AppRestrictedLowDel ----
		// RESTRICTED_LOWDELAY forces the CELT-only layer regardless of
		// bitrate, and the encoder still picks a bandwidth based on Fs.
		{8000, 1, 10, 48000, AppRestrictedLowDel, "celt_nb_mono_10ms"},
		{8000, 1, 20, 48000, AppRestrictedLowDel, "celt_nb_mono_20ms"},
		{8000, 2, 20, 64000, AppRestrictedLowDel, "celt_nb_stereo_20ms"},

		{16000, 1, 10, 64000, AppRestrictedLowDel, "celt_wb_mono_10ms"},
		{16000, 1, 20, 64000, AppRestrictedLowDel, "celt_wb_mono_20ms"},
		{16000, 2, 20, 96000, AppRestrictedLowDel, "celt_wb_stereo_20ms"},

		{24000, 1, 10, 96000, AppRestrictedLowDel, "celt_swb_mono_10ms"},
		{24000, 1, 20, 96000, AppRestrictedLowDel, "celt_swb_mono_20ms"},
		{24000, 2, 20, 128000, AppRestrictedLowDel, "celt_swb_stereo_20ms"},

		{48000, 1, 10, 128000, AppRestrictedLowDel, "celt_fb_mono_10ms"},
		{48000, 1, 20, 128000, AppRestrictedLowDel, "celt_fb_mono_20ms"},
		{48000, 2, 20, 192000, AppRestrictedLowDel, "celt_fb_stereo_20ms"},

		// ---- CELT-only via AppAudio at high bitrates ----
		{48000, 1, 20, 128000, AppAudio, "audio_fb_mono_20ms"},
		{48000, 2, 20, 192000, AppAudio, "audio_fb_stereo_20ms"},
	}
	// 12 kHz has no legal 10 ms hybrid/CELT configs because frame sizes
	// below 2.5 ms are rejected and the encoder refuses some 10 ms slots
	// for non-SILK at lower Fs. The above matrix reflects only legal
	// combinations.
	return cases
}

// TestParity_OpusDecode — full parity matrix across SILK-only, Hybrid,
// and CELT-only paths. Every case must match the C decoder bit-for-bit
// on every sample of every decoded frame.
func TestParity_OpusDecode(t *testing.T) {
	cases := opusDecodeMatrix()
	for _, c := range cases {
		c := c
		t.Run(c.tag, func(t *testing.T) {
			decoderParitySweep(t, c.Fs, c.channels, c.frameMs, c.bitrate,
				c.app, 5, 4, int64(c.Fs+c.bitrate+c.channels*1000+c.app))
		})
	}
}

// TestParity_OpusDecode_Mono — SILK-only mono slice of the matrix,
// kept separate so a narrow `-run TestParity_OpusDecode_Mono` still
// works for targeted debugging. Fully subsumed by TestParity_OpusDecode.
func TestParity_OpusDecode_Mono(t *testing.T) {
	cases := []struct {
		Fs, frameMs, bitrate int
	}{
		{16000, 20, 20000},
		{12000, 20, 16000},
		{8000, 20, 10000},
		{8000, 10, 10000},
	}
	for _, c := range cases {
		name := mkName(c.Fs, 1, c.frameMs, c.bitrate)
		t.Run(name, func(t *testing.T) {
			decoderParitySweep(t, c.Fs, 1, c.frameMs, c.bitrate, AppVOIP, 5, 4, int64(c.Fs+c.bitrate))
		})
	}
}

// TestParity_OpusDecode_Stereo — SILK-only stereo slice, ditto.
func TestParity_OpusDecode_Stereo(t *testing.T) {
	cases := []struct {
		Fs, frameMs, bitrate int
	}{
		{16000, 20, 40000},
		{8000, 20, 24000},
	}
	for _, c := range cases {
		name := mkName(c.Fs, 2, c.frameMs, c.bitrate)
		t.Run(name, func(t *testing.T) {
			decoderParitySweep(t, c.Fs, 2, c.frameMs, c.bitrate, AppVOIP, 5, 4, int64(c.Fs+c.bitrate+1))
		})
	}
}

func mkName(Fs, ch, ms, br int) string {
	return itoaDec(Fs) + "hz_" + itoaDec(ch) + "ch_" + itoaDec(ms) + "ms_" + itoaDec(br) + "bps"
}

func itoaDec(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
