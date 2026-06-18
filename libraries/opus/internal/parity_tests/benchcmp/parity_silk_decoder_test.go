//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// floatsEq checks for bit-exact equality; SILK output is produced from
// integer PCM scaled by 1/32768, so any floating-point drift signals a
// real parity bug.
func floatsEq(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			return false
		}
	}
	return true
}

func TestParity_SilkPLCReset(t *testing.T) {
	for _, fsKhz := range []int{8, 12, 16} {
		for _, fl := range []int{80, 160, 240, 320} {
			cp0, cg0, cg1, csl, cns := cSilkPLCReset(fl, fsKhz)
			gp0, gg0, gg1, gsl, gns := nativeopus.ExportTestSilkPLCReset(fl, fsKhz)
			if cp0 != gp0 || cg0 != gg0 || cg1 != gg1 || csl != gsl || cns != gns {
				t.Fatalf("PLC_Reset fl=%d fsKHz=%d: C=(%d,%d,%d,%d,%d) Go=(%d,%d,%d,%d,%d)",
					fl, fsKhz, cp0, cg0, cg1, csl, cns, gp0, gg0, gg1, gsl, gns)
			}
		}
	}
}

func TestParity_SilkCNGReset(t *testing.T) {
	for _, order := range []int{10, 16} {
		cSm, cG, cR := cSilkCNGReset(order)
		gSm, gG, gR := nativeopus.ExportTestSilkCNGReset(order)
		if !eqInt16Slice(cSm, gSm) || cG != gG || cR != gR {
			t.Fatalf("CNG_Reset order=%d: C=(%v,%d,%d) Go=(%v,%d,%d)",
				order, cSm, cG, cR, gSm, gG, gR)
		}
	}
}

func TestParity_SilkInitDecoder(t *testing.T) {
	cpg, cff := cSilkInitDecoder()
	gpg, gff, _ := nativeopus.ExportTestSilkInitDecoder()
	if cpg != gpg || cff != gff {
		t.Fatalf("init_decoder: C=(%d,%d) Go=(%d,%d)", cpg, cff, gpg, gff)
	}
}

func TestParity_SilkDecoderSetFs(t *testing.T) {
	for _, nbSubfr := range []int{2, 4} {
		for _, fsKhz := range []int{8, 12, 16} {
			for _, apiHz := range []int32{8000, 16000, 24000, 48000} {
				cr, csl, cfl, cll, clo, clp, clg, cps, crf := cSilkDecoderSetFs(nbSubfr, fsKhz, apiHz)
				gr, gsl, gfl, gll, glo, glp, glg, gps, grf := nativeopus.ExportTestSilkDecoderSetFs(nbSubfr, fsKhz, apiHz)
				if cr != gr || csl != gsl || cfl != gfl || cll != gll ||
					clo != glo || clp != glp || clg != glg || cps != gps || crf != grf {
					t.Fatalf("decoder_set_fs nbSubfr=%d fsKHz=%d apiHz=%d:\n  C=(%d,%d,%d,%d,%d,%d,%d,%d,%d)\n  Go=(%d,%d,%d,%d,%d,%d,%d,%d,%d)",
						nbSubfr, fsKhz, apiHz,
						cr, csl, cfl, cll, clo, clp, clg, cps, crf,
						gr, gsl, gfl, gll, glo, glp, glg, gps, grf)
				}
			}
		}
	}
}

// TestParity_SilkDecodeFull — end-to-end silk_Decode parity test. We
// encode 20 ms of mono 16 kHz PCM at VOIP bitrate (pure SILK WB) with
// the C encoder, strip the TOC byte (SILK-only frames use simple code 0
// packaging: TOC + payload), and feed the payload to both C and Go
// silk_Decode. The recovered PCM and final range-coder state must be
// bit-exact.
func TestParity_SilkDecodeFull(t *testing.T) {
	const apiHz int32 = 16000
	const frameMs = 20
	const samplesPerFrame = int(apiHz) / 1000 * frameMs // 320

	// Build a varied deterministic test signal: sum of two sinusoids.
	pcm := make([]float32, samplesPerFrame)
	for i := range pcm {
		t := float32(i)
		pcm[i] = 0.25*float32(sin32(0.1*t)) + 0.1*float32(sin32(0.03*t))
	}

	enc := NewCEncoder(int(apiHz), 1, AppVOIP)
	if enc == nil {
		t.Skip("failed to create C encoder")
	}
	defer enc.Destroy()
	enc.SetBitrate(16000)
	enc.SetComplexity(5)

	pkt := make([]byte, 1024)
	n := enc.Encode(pcm, pkt)
	if n <= 2 {
		t.Fatalf("encode returned %d bytes", n)
	}
	full := pkt[:n]
	t.Logf("packet len %d, TOC byte 0x%02x", n, full[0])

	// Parse TOC byte. SILK-only narrowband+wideband use config
	// 0..15. We force VOIP which yields SILK-only for low rates.
	// TOC format: [config:5][s:1][c:2]. c==0 => single frame
	// (code 0), payload is full[1:].
	toc := full[0]
	code := toc & 0x3
	if code != 0 {
		t.Fatalf("expected code-0 packet, got code=%d (TOC=0x%02x)", code, toc)
	}
	payload := full[1:]

	// Decode with C.
	cPCM, cRng, cRet := cSilkDecodeFull(payload, 1, 1, apiHz, apiHz, frameMs, 0, 1)
	if cRet != 0 {
		t.Fatalf("C silk_Decode returned %d", cRet)
	}
	// Decode with Go.
	goPCM, goRng, goRet := nativeopus.ExportTestSilkDecodeFull(payload, 1, 1, apiHz, apiHz, frameMs, 0, 1)
	if goRet != 0 {
		t.Fatalf("Go silk_Decode returned %d", goRet)
	}
	if cRng != goRng {
		t.Fatalf("final rng mismatch: C=0x%08x Go=0x%08x", cRng, goRng)
	}
	if !floatsEq(cPCM, goPCM) {
		// Find first difference.
		for i := range cPCM {
			if math.Float32bits(cPCM[i]) != math.Float32bits(goPCM[i]) {
				t.Fatalf("pcm mismatch at sample %d: C=%g Go=%g (of %d)",
					i, cPCM[i], goPCM[i], len(cPCM))
			}
		}
		t.Fatalf("pcm length mismatch: %d vs %d", len(cPCM), len(goPCM))
	}
}

func sin32(x float32) float32 {
	return float32(math.Sin(float64(x)))
}
