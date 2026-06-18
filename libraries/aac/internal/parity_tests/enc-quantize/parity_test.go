// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encquantize

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// These are pure INTEGER fixed-point kernels (FIXP_DBL == int32, SHORT == int16):
// leading-bit normalisation, arithmetic shifts, int64-product fixmul, and the
// table-driven ^3/4 / ^4/3 mantissa lookups + fLog2 are bit-identical regardless
// of -ffp-contract / vectorization, with no transcendental and no float. So the
// assertions run unconditionally (like the sibling enc-psy-model / band_nrg
// oracle), NOT gated on nativeaac.StrictMode — there is no FP path to gate. The
// canonical gate still runs under -tags 'aac_strict aacfdk' for consistency.

// longBandOffset is a faithful AAC-LC long-window scalefactor-band offset layout
// (the 49-band 44.1 kHz table): a strictly increasing partition of the 1024-line
// spectrum into SFBs. QuantizeSpectrum operates over an arbitrary monotone
// partition; this exercises realistic band widths.
var longBandOffset = []int{
	0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 48, 56, 64, 72, 80, 88, 96, 108,
	120, 132, 144, 160, 176, 196, 216, 240, 264, 292, 320, 352, 384, 416, 448,
	480, 512, 544, 576, 608, 640, 672, 704, 736, 768, 800, 832, 864, 896, 928,
	1024,
}

func numLong() int { return len(longBandOffset) - 1 }

// specKind enumerates spectrum shapes driving the quantizer across its branches
// (zero, small, large/near-overflow, mixed signs, sparse).
type specKind int

const (
	kindZero specKind = iota
	kindSmall
	kindLarge
	kindMixed
	kindSparse
)

var allKinds = []specKind{kindZero, kindSmall, kindLarge, kindMixed, kindSparse}

func kindName(k specKind) string {
	return [...]string{"zero", "small", "large", "mixed", "sparse"}[k]
}

// makeSpec builds a length-line FIXP_DBL (int32) spectrum for the given kind,
// deterministically seeded so the C and Go sides see identical input.
func makeSpec(kind specKind, length, seed int) []int32 {
	out := make([]int32, length)
	rng := rand.New(rand.NewPCG(uint64(seed)+1, 0x9a3b))
	switch kind {
	case kindZero:
		// leave zero
	case kindSmall:
		for i := range out {
			out[i] = int32(rng.IntN(2001) - 1000) // |x| <= 1000, lots of headroom
		}
	case kindLarge:
		for i := range out {
			v := int32(rng.Uint32())
			if v >= 0 {
				out[i] = v | 0x40000000
			} else {
				out[i] = v | int32(-0x40000000)
			}
		}
	case kindMixed:
		for i := range out {
			out[i] = int32(rng.Uint32())
		}
	case kindSparse:
		for i := range out {
			if i%53 == 0 {
				out[i] = int32(rng.Uint32())
			}
		}
	}
	return out
}

// gains spans the QSS range the FORWARD quantizer is driven over. quantizeLines
// computes quantizer = quantTableQ[(-gain)&3] and quantizershift = ((-gain)>>2)+1,
// then clamps its down-shift with fixMin(totalShift, DFRACT_BITS-1) — so it is
// well-defined for any gain; cover a wide spread of positive/negative values.
var gains = []int{-64, -40, -20, -8, -4, -1, 0, 1, 4, 8, 20, 40, 64, 100, 140}

// invGains is the gain range for the INVERSE quantizer. Unlike the forward path,
// FDKaacEnc_invQuantizeLines applies an UNCLAMPED `accu <<= ...` / `accu >>= ...`
// whose count is (iquantizershift +/- specExp): the genuine kernel is only
// defined where that count stays in [0, 31] (its FDK_ASSERT(specExp<14) bounds
// abs(q)<=8191 -> specExp in [0,13], specExp2 = specExpTableComb-1 in [0,18]).
// Outside that window the C performs a >=32 shift on a 32-bit INT, which is
// undefined behaviour: clang folds it differently at -O0 vs -O2 (verified), so
// there is NO bit-exact target there and the real encoder never reaches it (scf
// is limited so quantized magnitudes and the shift stay in range — sf_estim.cpp:
// 1133 "lower scf limit to avoid quantized values bigger than MAX_QUANT"). We
// therefore drive the inverse quantizer only over its defined domain: with
// |q|<=8191 (specExp2<=18) a gain in [-28, 28] keeps |iquantizershift|<=7 and the
// shift count <=25, comfortably in-range.
var invGains = []int{-28, -20, -12, -8, -4, -1, 0, 1, 4, 8, 12, 20, 28}

// TestQuantRom verifies the Go-embedded quantizer ROM tables match the genuine
// vendored aacEnc_rom.cpp tables bit-for-bit (the narrowing macros folded
// identically).
func TestQuantRom(t *testing.T) {
	cM34, cQ, cE, cM43 := cQuantRom()
	gM34, gQ, gE, gM43 := nativeaac.QuantRomForParity()
	require.Equal(t, cM34, gM34, "FDKaacEnc_mTab_3_4")
	require.Equal(t, cQ, gQ, "FDKaacEnc_quantTableQ")
	require.Equal(t, cE, gE, "FDKaacEnc_quantTableE")
	require.Equal(t, cM43, gM43, "FDKaacEnc_mTab_4_3Elc")
}

// TestQuantizeLines asserts the per-run quantizer matches on every spectrum kind
// and every gain, for both dZone settings.
func TestQuantizeLines(t *testing.T) {
	for _, dz := range []bool{false, true} {
		for _, k := range allKinds {
			spec := makeSpec(k, 256, int(k)*7+11)
			for _, gain := range gains {
				want := cQuantizeLines(gain, len(spec), spec, dz)
				got := make([]int16, len(spec))
				nativeaac.QuantizeLinesForParity(gain, len(spec), spec, got, dz)
				require.Equal(t, want, got, "dz=%v %s gain=%d", dz, kindName(k), gain)
			}
		}
	}
}

// TestInvQuantizeLines asserts the inverse quantizer matches over a spread of
// quantized SHORT values (|q| <= MAX_QUANT==8191, per the FDK_ASSERT specExp<14
// precondition) and every gain.
func TestInvQuantizeLines(t *testing.T) {
	rng := rand.New(rand.NewPCG(23, 29))
	q := make([]int16, 4096)
	for i := range q {
		switch i % 5 {
		case 0:
			q[i] = 0
		case 1:
			q[i] = int16(rng.IntN(8192)) // 0..8191
		case 2:
			q[i] = int16(-rng.IntN(8192))
		case 3:
			q[i] = 8191
		default:
			q[i] = int16(rng.IntN(8192) - 4096)
		}
	}
	for _, gain := range invGains {
		want := cInvQuantizeLines(gain, len(q), q)
		got := make([]int32, len(q))
		nativeaac.InvQuantizeLinesForParity(gain, len(q), q, got)
		require.Equal(t, want, got, "gain=%d", gain)
	}
}

// TestQuantizeSpectrum drives the whole-spectrum quantizer over the long band
// layout with per-band scalefactors, for several global gains and both dZone
// settings — the FDKaacEnc_QuantizeSpectrum driver as the encoder calls it.
func TestQuantizeSpectrum(t *testing.T) {
	n := numLong()
	rng := rand.New(rand.NewPCG(31, 37))
	for _, dz := range []bool{false, true} {
		for _, k := range []specKind{kindSmall, kindLarge, kindMixed, kindSparse} {
			spec := makeSpec(k, longBandOffset[len(longBandOffset)-1], int(k)*5+3)
			for _, globalGain := range []int{0, 40, 80, 120, 160, 200} {
				scf := make([]int, n)
				for i := range scf {
					scf[i] = rng.IntN(60) // per-band scalefactors 0..59
				}
				// AAC-LC long block: one group, sfbPerGroup == sfbCnt == n.
				want := cQuantizeSpectrum(n, n, n, longBandOffset, spec, globalGain, scf, dz)
				got := make([]int16, longBandOffset[n])
				nativeaac.QuantizeSpectrumForParity(n, n, n, longBandOffset, spec, globalGain, scf, got, dz)
				require.Equal(t, want, got, "dz=%v %s gg=%d", dz, kindName(k), globalGain)
			}
		}
	}
}

// TestCalcSfbDist asserts the ld-domain distortion cost function matches on every
// spectrum kind and every gain (including the MAX_QUANT overflow -> 0 path the
// large/high-gain combinations trigger).
func TestCalcSfbDist(t *testing.T) {
	for _, dz := range []bool{false, true} {
		for _, k := range allKinds {
			for _, n := range []int{1, 4, 16, 96, 256} {
				spec := makeSpec(k, n, int(k)*13+n)
				for _, gain := range invGains {
					want := cCalcSfbDist(spec, n, gain, dz)
					got := nativeaac.CalcSfbDistForParity(spec, make([]int16, n), n, gain, dz)
					require.Equal(t, want, got, "dz=%v %s n=%d gain=%d", dz, kindName(k), n, gain)
				}
			}
		}
	}
}

// TestCalcSfbQuantEnergyAndDist asserts the ld-domain energy+distortion cost
// matches for already-quantized bands. Quantized lines are produced by the
// genuine quantizer so the inputs are realistic (and exercise the MAX_QUANT
// overflow path with hand-injected out-of-range values).
func TestCalcSfbQuantEnergyAndDist(t *testing.T) {
	for _, k := range allKinds {
		for _, n := range []int{1, 4, 16, 96, 256} {
			spec := makeSpec(k, n, int(k)*17+n+1)
			for _, gain := range invGains {
				// Produce realistic quantized lines for these (spec, gain).
				qs := cQuantizeLines(gain, n, spec, false)
				wantEn, wantDist := cCalcSfbQuantEnergyAndDist(spec, qs, n, gain)
				gotEn, gotDist := nativeaac.CalcSfbQuantEnergyAndDistForParity(spec, qs, n, gain)
				require.Equal(t, wantEn, gotEn, "en %s n=%d gain=%d", kindName(k), n, gain)
				require.Equal(t, wantDist, gotDist, "dist %s n=%d gain=%d", kindName(k), n, gain)
			}
		}
	}

	// MAX_QUANT overflow path: inject an out-of-range magnitude -> en/dist == 0.
	spec := makeSpec(kindMixed, 8, 99)
	qs := make([]int16, 8)
	qs[3] = 9000 // > MAX_QUANT (8191)
	wantEn, wantDist := cCalcSfbQuantEnergyAndDist(spec, qs, 8, 0)
	gotEn, gotDist := nativeaac.CalcSfbQuantEnergyAndDistForParity(spec, qs, 8, 0)
	require.Equal(t, wantEn, gotEn, "overflow en")
	require.Equal(t, wantDist, gotDist, "overflow dist")
	require.Equal(t, int32(0), gotEn, "overflow en==0")
}
