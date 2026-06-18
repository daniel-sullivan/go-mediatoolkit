// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encpsymodel

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// longBandOffset is a faithful AAC-LC long-window scalefactor-band offset layout
// (the 49-band 44.1 kHz table): a strictly increasing partition of the 1024-line
// spectrum into SFBs. band_nrg operates over an arbitrary monotone partition;
// using a real layout exercises the realistic band widths (up to 96 lines, the
// "max sfbWidth = 96" the CalcBandEnergyOptimLong comment cites). The C and Go
// kernels both read bandOffset[0..numBands].
var longBandOffset = []int{
	0, 4, 8, 12, 16, 20, 24, 28, 32, 36, 40, 48, 56, 64, 72, 80, 88, 96, 108,
	120, 132, 144, 160, 176, 196, 216, 240, 264, 292, 320, 352, 384, 416, 448,
	480, 512, 544, 576, 608, 640, 672, 704, 736, 768, 800, 832, 864, 896, 928,
	1024,
}

// shortBandOffset is a faithful AAC-LC short-window SFB offset layout (14 bands
// over 128 lines): the "max sfbWidth = 36" path CalcBandEnergyOptimShort cites.
var shortBandOffset = []int{
	0, 4, 8, 12, 16, 20, 28, 36, 44, 56, 68, 80, 96, 112, 128,
}

// numLong / numShort are the active band counts.
func numLong() int  { return len(longBandOffset) - 1 }
func numShort() int { return len(shortBandOffset) - 1 }

// specKind enumerates spectrum shapes that drive the energy/headroom kernels
// across their branches (zero, small, large/near-overflow, mixed signs).
type specKind int

const (
	kindZero    specKind = iota // all zero → maxScale clamps, ldData == -1.0
	kindSmall                   // small magnitudes → large headroom, leadingBits>=0
	kindLarge                   // near-full-scale → small/zero headroom, leadingBits<0
	kindMixed                   // full-range random → exercises both shift branches
	kindOneBand                 // energy only in a few bands → maxNrg index selection
)

// makeSpec builds a length-line FIXP_DBL (int32) spectrum for the given kind,
// deterministically seeded so the C and Go sides see identical input.
func makeSpec(kind specKind, length, seed int) []int32 {
	out := make([]int32, length)
	rng := rand.New(rand.NewPCG(uint64(seed)+1, 0x5151))
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
			// push toward full scale to drive headroom to 0/1 (leadingBits<0)
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
	case kindOneBand:
		for i := range out {
			if i%97 == 0 {
				out[i] = int32(rng.Uint32())
			}
		}
	}
	return out
}

func kindName(k specKind) string {
	return [...]string{"zero", "small", "large", "mixed", "oneBand"}[k]
}

var allKinds = []specKind{kindZero, kindSmall, kindLarge, kindMixed, kindOneBand}

// TestLdConstants cross-checks the FL2FXCONST_DBL constants and the ldCoeff ROM
// the Go port embeds against the genuine C macro folding — the bit-exact basis
// of the log2 helper that band_nrg uses.
func TestLdConstants(t *testing.T) {
	c := cLdConsts()
	require.Equal(t, c[0], nativeaac.Fl2fxconstDBLForParity(-1.0), "FL2FXCONST_DBL(-1.0)")
	require.Equal(t, c[1], nativeaac.Fl2fxconstDBLForParity(2.0/64), "FL2FXCONST_DBL(2/64)")
	require.Equal(t, c[2], nativeaac.Fl2fxconstDBLForParity(1.0/64), "FL2FXCONST_DBL(1/64)")
	require.Equal(t, c[3], nativeaac.Fl2fxconstDBLForParity(0.0), "FL2FXCONST_DBL(0.0)")

	coeff := cLdCoeff()
	require.Equal(t, coeff[:], nativeaac.LdCoeffForParity(), "ldCoeff ROM")
}

// TestCalcLdData drives the genuine CalcLdData (fLog2) and the Go calcLdData over
// a spread of FIXP_DBL operands — the log2 kernel band_nrg uses element-wise.
func TestCalcLdData(t *testing.T) {
	ops := []int32{
		0, 1, -1, 2, 100, 0x7FFFFFFF, -0x80000000, 0x40000000, 0x00010000,
		0x7FFF, 0x12345678, -0x12345678, 3, 7, 255, 0x000000FF, 0x55555555,
	}
	rng := rand.New(rand.NewPCG(7, 7))
	for i := 0; i < 4000; i++ {
		ops = append(ops, int32(rng.Uint32()))
	}
	for _, op := range ops {
		require.Equal(t, cCalcLdData(op), nativeaac.CalcLdData(op), "CalcLdData(%d)", op)
	}
}

// TestLdDataVector drives the genuine LdDataVector and the Go ldDataVector over a
// random energy-like vector.
func TestLdDataVector(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 13))
	n := numLong()
	src := make([]int32, n)
	for i := range src {
		// energies are non-negative; include some zeros
		if rng.IntN(8) == 0 {
			src[i] = 0
		} else {
			src[i] = int32(rng.Uint32() & 0x7FFFFFFF)
		}
	}
	want := cLdDataVector(src, n)
	got := make([]int32, n)
	nativeaac.LdDataVector(src, got, n)
	require.Equal(t, want, got, "LdDataVector")
}

// TestCalcSfbMaxScaleSpec asserts the per-SFB headroom kernel matches on every
// spectrum kind for both long and short band layouts.
func TestCalcSfbMaxScaleSpec(t *testing.T) {
	for _, layout := range []struct {
		name string
		off  []int
		n    int
		ln   int
	}{
		{"long", longBandOffset, numLong(), 1024},
		{"short", shortBandOffset, numShort(), 128},
	} {
		for _, k := range allKinds {
			spec := makeSpec(k, layout.ln, int(k)*3+len(layout.name))
			want := cCalcSfbMaxScaleSpec(spec, layout.off, layout.n)
			got := make([]int, layout.n)
			nativeaac.CalcSfbMaxScaleSpec(spec, layout.off, got, layout.n)
			require.Equal(t, want, got, "%s/%s", layout.name, kindName(k))
		}
	}
}

// TestCheckBandEnergyOptim drives the first-pass SFB energy + ldData + maxNrg
// kernel, deriving sfbMaxScaleSpec from the genuine kernel first (as psy_main
// does) so the inputs match the production data flow.
func TestCheckBandEnergyOptim(t *testing.T) {
	for _, k := range allKinds {
		spec := makeSpec(k, 1024, int(k)+20)
		n := numLong()
		ms := cCalcSfbMaxScaleSpec(spec, longBandOffset, n)

		// minSpecShift = min over bands - 4 (psy_main.cpp:663 passes
		// minSpecShift-4; here we compute the minimum of the per-band max scale
		// like psy_main, then subtract 4).
		minScale := 32
		for _, v := range ms {
			if v < minScale {
				minScale = v
			}
		}
		minSpecShift := minScale - 4

		wantMax, wantBe, wantBeLd := cCheckBandEnergyOptim(spec, ms, longBandOffset, n, minSpecShift)
		gotBe := make([]int32, n)
		gotBeLd := make([]int32, n)
		gotMax := nativeaac.CheckBandEnergyOptim(spec, gotBe, gotBeLd, ms, longBandOffset, n, minSpecShift)

		require.Equal(t, wantBe, gotBe, "%s bandEnergy", kindName(k))
		require.Equal(t, wantBeLd, gotBeLd, "%s bandEnergyLdData", kindName(k))
		require.Equal(t, wantMax, gotMax, "%s maxNrg", kindName(k))
	}
}

// TestCalcBandEnergyOptimLong drives the long-block energy kernel and its return
// shiftBits, deriving sfbMaxScaleSpec from the genuine kernel first.
func TestCalcBandEnergyOptimLong(t *testing.T) {
	for _, k := range allKinds {
		spec := makeSpec(k, 1024, int(k)+40)
		n := numLong()
		ms := cCalcSfbMaxScaleSpec(spec, longBandOffset, n)

		// sfbMaxScaleSpec is read-only in the long kernel; pass a copy to each
		// side so a (non-existent) mutation can't cross-contaminate.
		wantShift, wantBe, wantBeLd := cCalcBandEnergyOptimLong(spec, ms, longBandOffset, n)
		gotBe := make([]int32, n)
		gotBeLd := make([]int32, n)
		gotShift := nativeaac.CalcBandEnergyOptimLong(spec, gotBe, gotBeLd, append([]int(nil), ms...), longBandOffset, n)

		require.Equal(t, wantShift, gotShift, "%s shiftBits", kindName(k))
		require.Equal(t, wantBe, gotBe, "%s bandEnergy", kindName(k))
		require.Equal(t, wantBeLd, gotBeLd, "%s bandEnergyLdData", kindName(k))
	}
}

// TestCalcBandEnergyOptimShort drives the short-block energy kernel.
func TestCalcBandEnergyOptimShort(t *testing.T) {
	for _, k := range allKinds {
		spec := makeSpec(k, 128, int(k)+60)
		n := numShort()
		ms := cCalcSfbMaxScaleSpec(spec, shortBandOffset, n)

		wantBe := cCalcBandEnergyOptimShort(spec, ms, shortBandOffset, n)
		gotBe := make([]int32, n)
		nativeaac.CalcBandEnergyOptimShort(spec, gotBe, append([]int(nil), ms...), shortBandOffset, n)
		require.Equal(t, wantBe, gotBe, "%s bandEnergy", kindName(k))
	}
}

// TestCalcBandNrgMSOpt drives the mid/side energy kernel over left/right pairs,
// for both calcLdData on and off.
func TestCalcBandNrgMSOpt(t *testing.T) {
	for _, calcLd := range []int{1, 0} {
		for _, kl := range allKinds {
			for _, kr := range []specKind{kindSmall, kindMixed} {
				specL := makeSpec(kl, 1024, int(kl)*5+1)
				specR := makeSpec(kr, 1024, int(kr)*5+2)
				n := numLong()
				msL := cCalcSfbMaxScaleSpec(specL, longBandOffset, n)
				msR := cCalcSfbMaxScaleSpec(specR, longBandOffset, n)

				wMid, wSide, wMidLd, wSideLd := cCalcBandNrgMSOpt(specL, specR, msL, msR, longBandOffset, n, calcLd)

				gMid := make([]int32, n)
				gSide := make([]int32, n)
				gMidLd := make([]int32, n)
				gSideLd := make([]int32, n)
				nativeaac.CalcBandNrgMSOpt(specL, specR, append([]int(nil), msL...), append([]int(nil), msR...),
					longBandOffset, n, gMid, gSide, calcLd, gMidLd, gSideLd)

				ctx := kindName(kl) + "/" + kindName(kr) + "/ld" + itoa(calcLd)
				require.Equal(t, wMid, gMid, "%s mid", ctx)
				require.Equal(t, wSide, gSide, "%s side", ctx)
				if calcLd != 0 {
					require.Equal(t, wMidLd, gMidLd, "%s midLd", ctx)
					require.Equal(t, wSideLd, gSideLd, "%s sideLd", ctx)
				}
			}
		}
	}
}

// itoa is a tiny int formatter for test context strings.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
