//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_StaticMode48000 compares every scalar and every array
// element of the ported mode48000_960_120 against the C-built
// descriptor returned by opus_custom_mode_create(48000, 960, ...).
// Bit-exact on all float fields; value-exact on all integer fields.
func TestParity_StaticMode48000(t *testing.T) {
	cm := cStaticMode48()
	gm := nativeopus.ExportStaticMode48000()

	// ── Scalar fields ────────────────────────────────────────────────
	if gm.Fs() != cm.Fs() {
		t.Errorf("Fs: Go=%d C=%d", gm.Fs(), cm.Fs())
	}
	if gm.Overlap() != cm.Overlap() {
		t.Errorf("overlap: Go=%d C=%d", gm.Overlap(), cm.Overlap())
	}
	if gm.NbEBands() != cm.NbEBands() {
		t.Errorf("nbEBands: Go=%d C=%d", gm.NbEBands(), cm.NbEBands())
	}
	if gm.EffEBands() != cm.EffEBands() {
		t.Errorf("effEBands: Go=%d C=%d", gm.EffEBands(), cm.EffEBands())
	}
	if gm.MaxLM() != cm.MaxLM() {
		t.Errorf("maxLM: Go=%d C=%d", gm.MaxLM(), cm.MaxLM())
	}
	if gm.NbShortMdcts() != cm.NbShortMdcts() {
		t.Errorf("nbShortMdcts: Go=%d C=%d", gm.NbShortMdcts(), cm.NbShortMdcts())
	}
	if gm.ShortMdctSize() != cm.ShortMdctSize() {
		t.Errorf("shortMdctSize: Go=%d C=%d", gm.ShortMdctSize(), cm.ShortMdctSize())
	}
	if gm.NbAllocVectors() != cm.NbAllocVectors() {
		t.Errorf("nbAllocVectors: Go=%d C=%d", gm.NbAllocVectors(), cm.NbAllocVectors())
	}

	// ── preemph[4] ───────────────────────────────────────────────────
	gp := gm.Preemph()
	cp := cm.Preemph()
	for i := 0; i < 4; i++ {
		if math.Float32bits(gp[i]) != math.Float32bits(cp[i]) {
			t.Errorf("preemph[%d]: Go=%g C=%g", i, gp[i], cp[i])
		}
	}

	// ── eBands ──────────────────────────────────────────────────────
	compareInt16("eBands", t, gm.EBands(), cm.EBands())
	compareInt16("logN", t, gm.LogN(), cm.LogN())
	compareBytes("allocVectors", t, gm.AllocVectors(), cm.AllocVectors())
	compareFloat32("window", t, gm.Window(), cm.Window())

	// ── MDCT lookup ─────────────────────────────────────────────────
	if gm.MdctN() != cm.MdctN() {
		t.Errorf("mdct.n: Go=%d C=%d", gm.MdctN(), cm.MdctN())
	}
	if gm.MdctMaxshift() != cm.MdctMaxshift() {
		t.Errorf("mdct.maxshift: Go=%d C=%d", gm.MdctMaxshift(), cm.MdctMaxshift())
	}
	compareFloat32("mdct.trig", t, gm.MdctTrig(), cm.MdctTrig())

	// ── Per-shift FFT state ─────────────────────────────────────────
	for s := 0; s <= cm.MdctMaxshift(); s++ {
		if gm.FftNfft(s) != cm.FftNfft(s) {
			t.Errorf("kfft[%d].nfft: Go=%d C=%d", s, gm.FftNfft(s), cm.FftNfft(s))
		}
		if math.Float32bits(gm.FftScale(s)) != math.Float32bits(cm.FftScale(s)) {
			t.Errorf("kfft[%d].scale: Go=%g C=%g", s, gm.FftScale(s), cm.FftScale(s))
		}
		if gm.FftShift(s) != cm.FftShift(s) {
			t.Errorf("kfft[%d].shift: Go=%d C=%d", s, gm.FftShift(s), cm.FftShift(s))
		}
		gf := gm.FftFactors(s)
		cf := cm.FftFactors(s)
		for i := 0; i < 16; i++ {
			if gf[i] != cf[i] {
				t.Errorf("kfft[%d].factors[%d]: Go=%d C=%d", s, i, gf[i], cf[i])
			}
		}
		compareInt16Tag(t, "kfft["+itoaDec(s)+"].bitrev", gm.FftBitrev(s), cm.FftBitrev(s))

		// twiddles (sub-states share shift-0's buffer). Compare the
		// base-length version.
		nBase := cm.FftNfft(0)
		if gm.FftTwiddlesLen(s) != nBase {
			t.Errorf("kfft[%d].twiddles len: Go=%d C=%d", s, gm.FftTwiddlesLen(s), nBase)
		}
		twC := cm.FftTwiddles(s) // always base-indexed
		for i := 0; i < nBase; i++ {
			gr, gi := gm.FftTwiddle(s, i)
			if math.Float32bits(gr) != math.Float32bits(twC[i][0]) ||
				math.Float32bits(gi) != math.Float32bits(twC[i][1]) {
				t.Errorf("kfft[%d].twiddles[%d]: Go=(%g,%g) C=(%g,%g)",
					s, i, gr, gi, twC[i][0], twC[i][1])
			}
		}
	}

	// ── Pulse cache ─────────────────────────────────────────────────
	if gm.CacheSize() != cm.CacheSize() {
		t.Errorf("cache.size: Go=%d C=%d", gm.CacheSize(), cm.CacheSize())
	}
	compareInt16("cache.index", t, gm.CacheIndex(), cm.CacheIndex())
	compareBytes("cache.bits", t, gm.CacheBits(), cm.CacheBits())
	compareBytes("cache.caps", t, gm.CacheCaps(), cm.CacheCaps())
}

func compareInt16(tag string, t *testing.T, g, c []int16) {
	t.Helper()
	if len(g) != len(c) {
		t.Errorf("%s: len Go=%d C=%d", tag, len(g), len(c))
		return
	}
	mismatches := 0
	for i := range g {
		if g[i] != c[i] {
			if mismatches < 5 {
				t.Errorf("%s[%d]: Go=%d C=%d", tag, i, g[i], c[i])
			}
			mismatches++
		}
	}
	if mismatches > 0 {
		t.Errorf("%s: %d mismatches / %d elements", tag, mismatches, len(g))
	}
}

func compareInt16Tag(t *testing.T, tag string, g, c []int16) {
	t.Helper()
	compareInt16(tag, t, g, c)
}

func compareBytes(tag string, t *testing.T, g, c []byte) {
	t.Helper()
	if len(g) != len(c) {
		t.Errorf("%s: len Go=%d C=%d", tag, len(g), len(c))
		return
	}
	mismatches := 0
	for i := range g {
		if g[i] != c[i] {
			if mismatches < 5 {
				t.Errorf("%s[%d]: Go=%d C=%d", tag, i, g[i], c[i])
			}
			mismatches++
		}
	}
	if mismatches > 0 {
		t.Errorf("%s: %d mismatches / %d elements", tag, mismatches, len(g))
	}
}

func compareFloat32(tag string, t *testing.T, g, c []float32) {
	t.Helper()
	if len(g) != len(c) {
		t.Errorf("%s: len Go=%d C=%d", tag, len(g), len(c))
		return
	}
	mismatches := 0
	for i := range g {
		if math.Float32bits(g[i]) != math.Float32bits(c[i]) {
			if mismatches < 5 {
				t.Errorf("%s[%d]: Go=%g (%#x) C=%g (%#x)", tag, i,
					g[i], math.Float32bits(g[i]),
					c[i], math.Float32bits(c[i]))
			}
			mismatches++
		}
	}
	if mismatches > 0 {
		t.Errorf("%s: %d mismatches / %d elements", tag, mismatches, len(g))
	}
}
