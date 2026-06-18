// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package inverse_quant

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// maxQuantizedValue mirrors MAX_QUANTIZED_VALUE (channelinfo.h:148): the AAC
// quantized spectral lines decode is fed live within [-8191, 8191]. Staying in
// this range keeps EvaluatePower43's `exponent < 14` invariant (the C
// FDK_ASSERT) satisfied, exactly as the real decode guarantees upstream.
const maxQuantizedValue = 8191

// strictGate skips FP-bit-exact-only assertions on a bare (non-strict) go test,
// per the aac_strict parity discipline. The inverse-quantization path is an
// integer/fixed-point kernel and matches in any build, but the gate is kept for
// convention so the strict run is the one that asserts.
func strictGate(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("requires -tags=aac_strict (integer-parity gate); see libraries/aac/mise.toml")
	}
}

// TestParityEvaluatePower43 sweeps EvaluatePower43 over every positive
// quantized magnitude in [1, 8191] for every lsb (0..3), comparing both the
// rescaled mantissa written back AND the returned exponent bit-for-bit. The
// domain is non-negative on purpose: the real decode only ever calls
// EvaluatePower43 on a band MAX magnitude (block.cpp:587 locMax, and via
// GetScaleFromValue after its `value != 0` guard), never on a raw signed line.
// fNormz (clz.h:152) counts leading ONES, so a negative argument would yield
// freeBits 0 / exponent 32 and trip the C FDK_ASSERT(exponent < 14) — a value
// the function is contractually never given. Zero is likewise excluded (it has
// no defined power-4/3 mantissa; GetScaleFromValue guards it upstream).
func TestParityEvaluatePower43(t *testing.T) {
	strictGate(t)
	for lsb := uint32(0); lsb < 4; lsb++ {
		for v := 1; v <= maxQuantizedValue; v++ {
			vC := int32(v)
			gotC, expC := cEvaluatePower43(vC, lsb)

			vN := int32(v)
			expN := nativeaac.EvaluatePower43(&vN, lsb)

			require.Equal(t, gotC, vN, "lsb=%d v=%d mantissa", lsb, v)
			require.Equal(t, expC, int(expN), "lsb=%d v=%d exponent", lsb, v)
		}
	}
}

// TestParityGetScaleFromValue sweeps GetScaleFromValue over the non-negative
// quantized magnitude range [0, 8191] (the function's own zero guard returns 0,
// and it is only ever fed a band-max magnitude — see the EvaluatePower43 domain
// note) for every lsb, comparing the returned shift scale.
func TestParityGetScaleFromValue(t *testing.T) {
	strictGate(t)
	for lsb := uint32(0); lsb < 4; lsb++ {
		for v := 0; v <= maxQuantizedValue; v++ {
			scaleC := cGetScaleFromValue(int32(v), lsb)
			scaleN := nativeaac.GetScaleFromValue(int32(v), lsb)
			require.Equal(t, scaleC, int(scaleN), "lsb=%d v=%d scale", lsb, v)
		}
	}
}

// TestParityMaxabsD sweeps maxabs_D over many random bands of quantized lines
// (including all-zero, single-line, and full-range bands), comparing the band
// maximum bit-for-bit.
func TestParityMaxabsD(t *testing.T) {
	strictGate(t)
	r := rand.New(rand.NewPCG(101, 102))
	for trial := 0; trial < 5000; trial++ {
		noLines := r.IntN(64) // 0..63, exercises the empty-band path too
		band := make([]int32, noLines)
		for i := range band {
			band[i] = int32(r.IntN(2*maxQuantizedValue+1) - maxQuantizedValue)
		}
		gotC := cMaxabsD(band, noLines)
		gotN := nativeaac.MaxabsD(band, noLines)
		require.Equal(t, gotC, gotN, "trial=%d noLines=%d", trial, noLines)
	}
}

// TestParityInverseQuantizeBand drives the full per-band inverse quantizer over
// many fabricated bands, mirroring how CBlock_InverseQuantizeSpectralData calls
// it: compute the band max, derive the headroom scale via EvaluatePower43 +
// CntLeadingZeros, then inverse-quantize the band in place. Both the C oracle
// and the Go port receive the SAME quantized band and the SAME derived scale;
// the rescaled int32 spectrum is compared bit-for-bit.
func TestParityInverseQuantizeBand(t *testing.T) {
	strictGate(t)
	r := rand.New(rand.NewPCG(201, 202))

	invQuantTab := nativeaac.InverseQuantTableRow()

	for trial := 0; trial < 20000; trial++ {
		lsb := r.IntN(4)
		noLines := 1 + r.IntN(48) // 1..48 lines per band

		band := make([]int32, noLines)
		// Bound the magnitudes so the band, and therefore the derived scale,
		// stays in the regime the live decode operates in.
		maxMag := 1 + r.IntN(maxQuantizedValue)
		for i := range band {
			band[i] = int32(r.IntN(2*maxMag+1) - maxMag)
		}

		// Derive the band headroom scale exactly as
		// CBlock_InverseQuantizeSpectralData (block.cpp:584-589): on the band
		// max, via the genuine ported GetScaleFromValue (which itself runs
		// EvaluatePower43 + CntLeadingZeros). A zero band yields scale 0 and is
		// left untouched by both sides.
		locMax := nativeaac.MaxabsD(band, noLines)
		scale := nativeaac.GetScaleFromValue(locMax, uint32(lsb))

		gotC := cInverseQuantizeBand(band, lsb, noLines, scale)

		gotN := append([]int32(nil), band...)
		nativeaac.InverseQuantizeBand(gotN, invQuantTab,
			nativeaac.MantissaTableRow(lsb), nativeaac.ExponentTableRow(lsb),
			noLines, scale)

		require.Equal(t, gotC, gotN,
			"trial=%d lsb=%d noLines=%d scale=%d", trial, lsb, noLines, scale)
	}
}
