// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psdechybrid

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/require"
)

// TestPsHybridInt32Parity is the HE-AAC v2 PS hybrid-filterbank parity gate. It
// drives BOTH the genuine vendored FDKhybridAnalysisApply -> FDKhybridSynthesisApply
// (FDK_hybrid.cpp, via bridge.cpp) and the pure-Go sbr.PsHybridRun over the
// IDENTICAL per-slot 3-band complex QMF input, and asserts the recombined 64-band
// QMF output is EXACTLY equal slot-for-slot.
//
// The filterbank carries cross-slot ringbuffer state (the 13-tap prototype delay
// line), so a multi-slot run also exercises the delay-line bookkeeping. Inputs
// span the full FIXP_DBL range (random Q31) so the SATURATE_LEFT_SHIFT clamps and
// the fft_8 twiddle rounding are exercised.
func TestPsHybridInt32Parity(t *testing.T) {
	r := rand.New(rand.NewSource(20260611))

	cases := []struct {
		name   string
		nSlots int
		amp    int32
	}{
		{"slots32-small", 32, 1 << 20},
		{"slots32-mid", 32, 1 << 26},
		{"slots64-large", 64, 1 << 30},
		{"slots48-full", 48, 0x7fffffff},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			qmfRe := make([][]int32, tc.nSlots)
			qmfImg := make([][]int32, tc.nSlots)
			flatRe := make([]int32, tc.nSlots*3)
			flatImg := make([]int32, tc.nSlots*3)
			for s := 0; s < tc.nSlots; s++ {
				qmfRe[s] = make([]int32, 3)
				qmfImg[s] = make([]int32, 3)
				for i := 0; i < 3; i++ {
					vr := int32(r.Int63())%tc.amp - tc.amp/2
					vi := int32(r.Int63())%tc.amp - tc.amp/2
					qmfRe[s][i] = vr
					qmfImg[s][i] = vi
					flatRe[s*3+i] = vr
					flatImg[s*3+i] = vi
				}
			}

			cRe, cImg := cPsHybrid(tc.nSlots, flatRe, flatImg)
			goRe, goImg := sbr.PsHybridRun(tc.nSlots, qmfRe, qmfImg)

			require.Equal(t, cRe, goRe, "hybrid real output mismatch")
			require.Equal(t, cImg, goImg, "hybrid imag output mismatch")
		})
	}
}
