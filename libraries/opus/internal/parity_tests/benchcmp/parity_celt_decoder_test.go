//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// buildFullGoMode returns a CeltModeHandle populated with every field
// the Go decoder needs (rate/vq tables + preemph + window + full MDCT
// state including all four FFT sub-states and the trig table).
func buildFullGoMode(t *testing.T) (cMode, nativeopus.CeltModeHandle) {
	cm, gm := loadGoMode(t, 48000, 960)
	gm.SetModePreemph(cModePreemph(cm))
	gm.SetModeWindow(cModeWindow(cm))
	// MDCT: build FFT states for shifts 0..maxshift, then combine with
	// the trig table into an MdctLookupHandle which we install.
	maxshift := cModeMdctMaxshift(cm)
	ffts := make([]nativeopus.FftStateHandle, maxshift+1)
	for s := 0; s <= maxshift; s++ {
		d := cModeFftState(cm, s)
		ffts[s] = nativeopus.NewFftStateFromData(d.Nfft, d.Scale, d.Shift,
			d.Factors, d.Bitrev, d.TwiddleR, d.TwiddleI)
	}
	// Sub-states point to the base twiddles in C; do the same here so
	// Go sub-FFT butterflies don't index off-end.
	baseTw := ffts[0].FftTwiddles()
	for s := 1; s <= maxshift; s++ {
		ffts[s].SetFftTwiddles(baseTw)
	}
	mdct := nativeopus.NewMdctLookupFromData(cModeMdctN(cm), maxshift, ffts, cModeMdctTrig(cm))
	gm.SetModeMdct(mdct)
	return cm, gm
}

// TestParity_CeltDecode_Mono — encode a short mono PCM burst with C
// celt_encode_with_ec, then decode with both C and Go celt_decode_with_ec.
// Compares PCM output sample-by-sample at 0 ULP.
func TestParity_CeltDecode_Mono(t *testing.T) {
	cm, gm := buildFullGoMode(t)

	for _, frame := range []int{120, 240, 480, 960} {
		for _, bitrate := range []int{32000, 64000, 128000} {
			// C encoder — pure CELT (start_band=0, end_band=21).
			cEnc := cCeltEncoderNew(48000, 1)
			if cEnc.p == nil {
				t.Fatalf("encoder_new failed")
			}
			cEnc.SetStartBand(0)
			cEnc.SetEndBand(21)
			cEnc.SetBitrate(bitrate)
			cEnc.SetSignalling(0) // raw: no Opus framing byte.
			cEnc.SetComplexity(5)

			// Generate a sinusoidal input.
			pcm := make([]float32, frame)
			for i := range pcm {
				pcm[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/48000))
			}

			// Encode with our own ec_enc so the Go side can replay.
			bufSz := bitrate * frame / 48000 / 8
			if bufSz < 4 {
				bufSz = 4
			}
			pkt := make([]byte, bufSz+32)
			ec := cEcEncNew(pkt)
			n := cEnc.EncodeWithEc(pcm, frame, pkt, ec)
			if n <= 0 {
				ec.Free()
				cEnc.Free()
				t.Fatalf("encode returned %d", n)
			}
			ec.EncDone()
			ec.Free()
			cEnc.Free()
			pkt = pkt[:n]

			// C decoder.
			cDec := cCeltDecoderNew(48000, 1)
			cDec.SetStartBand(0)
			cDec.SetEndBand(21)
			cDec.SetSignalling(0)
			outC := make([]float32, frame)
			ret := cDec.DecodeWithEc(pkt, outC, frame, 0)
			cDec.Free()
			if ret != frame {
				t.Errorf("frame=%d br=%d: C decode returned %d", frame, bitrate, ret)
				continue
			}

			// Go decoder.
			gDec, initRet := nativeopus.NewCeltDecoder(gm, 48000, 1)
			if initRet != nativeopus.OPUS_OK {
				t.Fatalf("Go decoder init: %d", initRet)
			}
			gDec.SetStartEnd(0, 21)
			gDec.SetSignalling(0)
			outG := make([]float32, frame)
			retG := nativeopus.ExportTestCeltDecodeWithEc(gDec, pkt, outG, frame, 0)
			if retG != frame {
				t.Errorf("frame=%d br=%d: Go decode returned %d", frame, bitrate, retG)
				continue
			}

			// Compare.
			mismatches := 0
			for i := 0; i < frame; i++ {
				if outC[i] != outG[i] {
					if mismatches < 3 {
						t.Errorf("frame=%d br=%d [%d]: C=%g Go=%g (%d ULP)",
							frame, bitrate, i, outC[i], outG[i], ulpDiffF32(outC[i], outG[i]))
					}
					mismatches++
				}
			}
			if mismatches > 0 {
				t.Errorf("frame=%d br=%d: %d/%d samples mismatched", frame, bitrate, mismatches, frame)
			}
		}
	}
	_ = rand.New
	_ = cm
}

// TestParity_CeltDecode_Stereo — stereo PCM through C encode, both
// sides decode. Also runs several back-to-back frames so we exercise
// the inter-frame state (postfilter memory, oldBandE, etc.).
func TestParity_CeltDecode_Stereo(t *testing.T) {
	_, gm := buildFullGoMode(t)
	const frame = 480
	const N = 8 // frames

	cEnc := cCeltEncoderNew(48000, 2)
	cEnc.SetStartBand(0)
	cEnc.SetEndBand(21)
	cEnc.SetBitrate(128000)
	cEnc.SetSignalling(0)
	cEnc.SetComplexity(5)
	defer cEnc.Free()

	cDec := cCeltDecoderNew(48000, 2)
	cDec.SetStartBand(0)
	cDec.SetEndBand(21)
	cDec.SetSignalling(0)
	defer cDec.Free()

	gDec, initRet := nativeopus.NewCeltDecoder(gm, 48000, 2)
	if initRet != nativeopus.OPUS_OK {
		t.Fatalf("Go decoder init: %d", initRet)
	}
	gDec.SetStartEnd(0, 21)
	gDec.SetSignalling(0)

	r := rand.New(rand.NewSource(199))
	for fi := 0; fi < N; fi++ {
		pcm := make([]float32, 2*frame)
		for i := range pcm {
			pcm[i] = r.Float32()*0.4 - 0.2
		}
		pkt := make([]byte, 256)
		ec := cEcEncNew(pkt)
		n := cEnc.EncodeWithEc(pcm, frame, pkt, ec)
		if n <= 0 {
			ec.Free()
			t.Fatalf("frame %d encode returned %d", fi, n)
		}
		ec.EncDone()
		ec.Free()
		pkt = pkt[:n]

		outC := make([]float32, 2*frame)
		outG := make([]float32, 2*frame)
		cDec.DecodeWithEc(pkt, outC, frame, 0)
		nativeopus.ExportTestCeltDecodeWithEc(gDec, pkt, outG, frame, 0)

		for i := 0; i < 2*frame; i++ {
			if outC[i] != outG[i] {
				t.Errorf("frame=%d [%d]: C=%g Go=%g (%d ULP)",
					fi, i, outC[i], outG[i], ulpDiffF32(outC[i], outG[i]))
				return
			}
		}
	}
}
