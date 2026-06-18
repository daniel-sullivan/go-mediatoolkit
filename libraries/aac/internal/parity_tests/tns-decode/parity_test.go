// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package tns_decode

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// tnsCoeff3 / tnsCoeff4 are the decode-side TNS lattice reflection-coefficient
// ROM tables (FDKaacDec_tnsCoeff3[8] / FDKaacDec_tnsCoeff4[16], aac_rom.cpp:3229
// + :3232). CTns_Apply indexes them by the parsed coefficient (Coeff[i]+4 for
// 3-bit resolution, Coeff[i]+8 for 4-bit) to build the FIXP_TCC == FIXP_DBL
// coefficient vector passed to CLpc_SynthesisLattice (aacdec_tns.cpp:302-310).
// Sweeping coefficients drawn from these tables exercises the genuine decode
// value range; random coefficients additionally stress the saturating MAC.
// Stored as uint32 bit patterns (the C TCC() macro is a raw reinterpret cast of
// the hex pattern to FIXP_DBL); converted to int32 with the same two's-
// complement bits at use via tnsTable.
var tnsCoeff3 = []uint32{
	0x81f1d1d4, 0x9126146c, 0xadb922c4, 0xd438af1f,
	0x00000000, 0x3789809b, 0x64130dd4, 0x7cca7016,
}

var tnsCoeff4 = []uint32{
	0x808bc842, 0x84e2e58c, 0x8d6b49d1, 0x99da920a,
	0xa9c45713, 0xbc9ddeb9, 0xd1c2d51b, 0xe87ae53d,
	0x00000000, 0x1a9cd9b6, 0x340ff254, 0x4b3c8c29,
	0x5f1f5ebb, 0x6ed9ebba, 0x79bc385f, 0x7f4c7e5b,
}

// tnsTable reinterprets the uint32 ROM bit patterns as int32 (FIXP_DBL).
func tnsTable(u []uint32) []int32 {
	out := make([]int32, len(u))
	for i, v := range u {
		out[i] = int32(v)
	}
	return out
}

// strictGate skips FP-bit-exact-only assertions on a bare (non-strict) go test,
// per the aac_strict parity discipline. The TNS lattice is an integer kernel
// and matches in any build, but the gate is kept for convention so the strict
// run is the one that asserts.
func strictGate(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("requires -tags=aac_strict (integer-parity gate); see libraries/aac/mise.toml")
	}
}

// TestParitySynthesisLatticeTnsCoeffs sweeps CLpc_SynthesisLattice (DBL) with
// coefficients drawn from the genuine decode-side TNS ROM tables across the
// full AAC order range (1..TNS_MAXIMUM_ORDER), both filter directions, both
// resolutions, and a range of band sizes / exponents — the exact shapes
// CTns_Apply feeds the lattice. The in-place filtered spectrum is compared
// bit-for-bit.
func TestParitySynthesisLatticeTnsCoeffs(t *testing.T) {
	strictGate(t)
	r := rand.New(rand.NewPCG(101, 102))

	// Orders the AAC-LC/Main parser can produce (1..TNS_MAXIMUM_ORDER==20).
	for order := 1; order <= 20; order++ {
		for _, res := range []int{3, 4} {
			tbl := tnsTable(tnsCoeff3)
			if res == 4 {
				tbl = tnsTable(tnsCoeff4)
			}
			for trial := 0; trial < 500; trial++ {
				coeff := make([]int32, order)
				for i := range coeff {
					coeff[i] = tbl[r.IntN(len(tbl))]
				}

				// Band size: TNS operates on a scalefactor-band line range; keep
				// it within a typical MDCT granule but vary widely.
				size := 1 + r.IntN(256)
				signal := make([]int32, size)
				for i := range signal {
					signal[i] = int32(r.Uint32())
				}

				inc := 1
				if r.IntN(2) == 1 {
					inc = -1 // backward filtering (filter->Direction == -1)
				}
				// CTns_Apply calls with signal_e == 0 and signal_e_out == 0
				// (aacdec_tns.cpp:350). Exercise that canonical case plus a small
				// spread to stress the scaleValue shifts.
				signalE := r.IntN(3)
				signalEOut := r.IntN(3)

				gotC := cSynthesisLatticeDBL(signal, signalE, signalEOut, inc, coeff, order)

				gotN := append([]int32(nil), signal...)
				state := make([]int32, order)
				nativeaac.ClpcSynthesisLatticeDBL(gotN, len(gotN), signalE, signalEOut, inc, coeff, order, state)

				require.Equal(t, gotC, gotN,
					"order=%d res=%d trial=%d inc=%d signalE=%d signalEOut=%d",
					order, res, trial, inc, signalE, signalEOut)
			}
		}
	}
}

// TestParitySynthesisLatticeRandomCoeffs stresses the saturating MAC chain with
// arbitrary FIXP_DBL coefficients (not just the ROM values), driving the
// SATURATE_LEFT_SHIFT_ALT clamps and sign-inversion edge of the lattice harder
// than the ROM tables alone reach.
func TestParitySynthesisLatticeRandomCoeffs(t *testing.T) {
	strictGate(t)
	r := rand.New(rand.NewPCG(201, 202))

	for order := 1; order <= 20; order++ {
		for trial := 0; trial < 800; trial++ {
			coeff := make([]int32, order)
			for i := range coeff {
				coeff[i] = int32(r.Uint32())
			}
			size := 1 + r.IntN(128)
			signal := make([]int32, size)
			for i := range signal {
				signal[i] = int32(r.Uint32())
			}
			inc := 1
			if r.IntN(2) == 1 {
				inc = -1
			}
			signalE := r.IntN(4)
			signalEOut := r.IntN(4)

			gotC := cSynthesisLatticeDBL(signal, signalE, signalEOut, inc, coeff, order)

			gotN := append([]int32(nil), signal...)
			state := make([]int32, order)
			nativeaac.ClpcSynthesisLatticeDBL(gotN, len(gotN), signalE, signalEOut, inc, coeff, order, state)

			require.Equal(t, gotC, gotN,
				"order=%d trial=%d inc=%d signalE=%d signalEOut=%d",
				order, trial, inc, signalE, signalEOut)
		}
	}
}

// TestParitySynthesisLatticeSaturation drives saturation-prone inputs:
// near-MAXVAL coefficients and signal lines so the SATURATE_LEFT_SHIFT_ALT
// clamps (both the +MAXVAL and the -(MAXVAL-1) "alt" branch) are hit and the
// pure-Go saturateLeftShiftAlt is verified against the C macro at the boundary.
func TestParitySynthesisLatticeSaturation(t *testing.T) {
	strictGate(t)
	r := rand.New(rand.NewPCG(301, 302))

	const (
		maxv = int32(0x7FFFFFFF)
		minv = int32(-0x80000000)
	)
	extremes := []int32{maxv, minv, maxv - 1, minv + 1, maxv / 2, minv / 2, 0, 1, -1}

	for order := 1; order <= 20; order++ {
		for trial := 0; trial < 600; trial++ {
			coeff := make([]int32, order)
			for i := range coeff {
				coeff[i] = extremes[r.IntN(len(extremes))]
			}
			size := 1 + r.IntN(64)
			signal := make([]int32, size)
			for i := range signal {
				signal[i] = extremes[r.IntN(len(extremes))]
			}
			inc := 1
			if r.IntN(2) == 1 {
				inc = -1
			}
			signalE := r.IntN(3)
			signalEOut := r.IntN(3)

			gotC := cSynthesisLatticeDBL(signal, signalE, signalEOut, inc, coeff, order)

			gotN := append([]int32(nil), signal...)
			state := make([]int32, order)
			nativeaac.ClpcSynthesisLatticeDBL(gotN, len(gotN), signalE, signalEOut, inc, coeff, order, state)

			require.Equal(t, gotC, gotN,
				"order=%d trial=%d inc=%d signalE=%d signalEOut=%d",
				order, trial, inc, signalE, signalEOut)
		}
	}
}
