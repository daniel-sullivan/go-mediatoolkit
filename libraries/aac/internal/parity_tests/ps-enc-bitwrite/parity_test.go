// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psencbitwrite

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// goWritePS mirrors psbw_run using the pure-Go port.
func goWritePS(s psOutScenario) (payload []byte, nBits int) {
	p := sbr.PSOutParity{
		EnablePSHeader: s.enablePSHeader,
		EnableIID:      s.enableIID,
		IidMode:        s.iidMode,
		EnableICC:      s.enableICC,
		IccMode:        s.iccMode,
		EnableIpdOpd:   0,
		FrameClass:     s.frameClass,
		NEnvelopes:     s.nEnvelopes,
	}
	for i := 0; i < 4; i++ {
		p.FrameBorder[i] = int(s.frameBorder[i])
		p.DeltaIID[i] = int(s.deltaIID[i])
		p.DeltaICC[i] = int(s.deltaICC[i])
		for b := 0; b < 20; b++ {
			p.Iid[i][b] = int(s.iidFlat[i*20+b])
			p.Icc[i][b] = int(s.iccFlat[i*20+b])
		}
	}
	for b := 0; b < 20; b++ {
		p.IidLast[b] = int(s.iidLast[b])
		p.IccLast[b] = int(s.iccLast[b])
	}
	return sbr.WritePSBitstreamParity(p)
}

// nBandsForMode mirrors getNoBands: coarse modes (0,3) -> 10 bands, mid (1,4) -> 20.
func nBandsForMode(mode int) int {
	switch mode {
	case 1, 4:
		return 20
	default:
		return 10
	}
}

// iidValidRange returns (offset, maxVal) for the iid mode's coarse/fine table so
// scenarios stay within the valid (no-clamp) delta range, exactly matching the
// encoder's quantizer outputs.
func iidRange(mode int) (off, maxVal int) {
	if mode < 3 { // coarse
		return 14, 28
	}
	return 30, 60 // fine
}

func TestPSBitWriteParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x9501))

	// mkScenario builds a valid PS_OUT: per-env quantized indices whose DPCM
	// deltas stay inside the table range (so the encoder writes no clamped/error
	// symbols), exercising header on/off, IID/ICC enable combos, coarse/fine/mid
	// modes, freq/time DPCM, and 1..4 envelopes.
	mkScenario := func(hdr, enIID, iidMode, enICC, iccMode, nEnv int, dirMix bool) psOutScenario {
		s := psOutScenario{
			enablePSHeader: hdr, enableIID: enIID, iidMode: iidMode,
			enableICC: enICC, iccMode: iccMode, frameClass: 0, nEnvelopes: nEnv,
		}
		iidBands := nBandsForMode(iidMode)
		iccBands := nBandsForMode(iccMode)
		iidOff, iidMax := iidRange(iidMode)
		// ICC delta table: offset 7, maxVal 14 (both freq+time).
		iccOff, iccMax := 7, 14

		// Build "last" frame indices first (for DELTA_TIME envelopes).
		for b := 0; b < 20; b++ {
			s.iidLast[b] = int32(rng.Intn(iidMax+1) - iidOff)
			s.iccLast[b] = int32(rng.Intn(iccMax+1) - iccOff)
		}
		for e := 0; e < nEnv; e++ {
			// direction: env 0 FREQ unless dirMix randomizes for e>0.
			dIid := 0
			dIcc := 0
			if dirMix && e > 0 {
				dIid = rng.Intn(2)
				dIcc = rng.Intn(2)
			}
			s.deltaIID[e] = int32(dIid)
			s.deltaICC[e] = int32(dIcc)

			// Generate indices whose successive deltas land in-range.
			prevIidRef := make([]int, 20) // reference (last env or 0 for freq)
			prevIccRef := make([]int, 20)
			if dIid == 1 { // TIME: ref is prior env (or iidLast for env 0)
				if e == 0 {
					for b := 0; b < 20; b++ {
						prevIidRef[b] = int(s.iidLast[b])
					}
				} else {
					for b := 0; b < 20; b++ {
						prevIidRef[b] = int(s.iidFlat[(e-1)*20+b])
					}
				}
			}
			if dIcc == 1 {
				if e == 0 {
					for b := 0; b < 20; b++ {
						prevIccRef[b] = int(s.iccLast[b])
					}
				} else {
					for b := 0; b < 20; b++ {
						prevIccRef[b] = int(s.iccFlat[(e-1)*20+b])
					}
				}
			}

			// FREQ: lastVal walks the produced values; TIME: ref is prevRef[b].
			lastIid := 0
			lastIcc := 0
			for b := 0; b < iidBands; b++ {
				var ref int
				if dIid == 1 {
					ref = prevIidRef[b]
				} else {
					ref = lastIid
				}
				// pick delta-1 in [-off, maxVal-off] so (val-ref)+off in [0,maxVal]
				delta := rng.Intn(iidMax+1) - iidOff
				val := ref + delta
				s.iidFlat[e*20+b] = int32(val)
				lastIid = val
			}
			for b := 0; b < iccBands; b++ {
				var ref int
				if dIcc == 1 {
					ref = prevIccRef[b]
				} else {
					ref = lastIcc
				}
				delta := rng.Intn(iccMax+1) - iccOff
				val := ref + delta
				s.iccFlat[e*20+b] = int32(val)
				lastIcc = val
			}
		}
		return s
	}

	type tc struct {
		name                                string
		hdr, enIID, iidMode, enICC, iccMode int
		nEnv                                int
		dirMix                              bool
	}
	tcs := []tc{
		{"hdr_iid_icc_coarse_1env", 1, 1, 0, 1, 0, 1, false},
		{"hdr_iid_fine_2env_mix", 1, 1, 3, 1, 0, 2, true},
		{"nohdr_iid_mid_icc_mid_2env", 0, 1, 1, 1, 1, 2, true},
		{"hdr_iid_only_coarse_3env", 1, 1, 0, 0, 0, 3, true},
		{"hdr_icc_only_4env", 1, 0, 0, 1, 0, 4, true},
		{"nohdr_iid_fine_icc_4env", 0, 1, 3, 1, 0, 4, true},
		{"hdr_no_iid_no_icc_1env", 1, 0, 0, 0, 0, 1, false},
		{"hdr_iid_mid_icc_coarse_2env", 1, 1, 1, 1, 0, 2, true},
	}
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			s := mkScenario(c.hdr, c.enIID, c.iidMode, c.enICC, c.iccMode, c.nEnv, c.dirMix)
			cPay, cBits := cWritePS(s)
			gPay, gBits := goWritePS(s)
			require.Equal(t, cBits, gBits, "bit count")
			assert.Equal(t, cPay, gPay, "ps_data bytes")
		})
	}
}
