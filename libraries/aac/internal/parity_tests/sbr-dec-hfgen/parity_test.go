// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrhfgen

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// TestWhFactorsROM verifies the LPP whitening ROM (sbr_rom.cpp) is byte-identical
// to the genuine FDK tables.
func TestWhFactorsROM(t *testing.T) {
	const rows = 9
	idxC := cWhFactorsIndex(rows)
	idxGo := sbr.WhFactorsIndex()
	require.Equal(t, rows, len(idxGo))
	assert.Equal(t, idxC, idxGo, "whFactorsIndex")

	tblC := cWhFactorsTable(rows)
	tblGo := sbr.WhFactorsTableFlat()
	assert.Equal(t, tblC, tblGo, "whFactorsTable")
}

// genBand fills a length-N buffer with two leading history samples + data, all
// QMF-magnitude FIXP_DBL values with a couple of headroom bits.
func genBand(r *rand.Rand, n int) []int32 {
	buf := make([]int32, n+2)
	for i := range buf {
		// keep a few headroom bits so the autocorr shifts behave like real QMF data
		buf[i] = int32(r.Int63()>>33) - (1 << 29)
	}
	return buf
}

// TestAutoCorr2ndReal pins autoCorr2nd_real over random low-band buffers.
func TestAutoCorr2ndReal(t *testing.T) {
	r := rand.New(rand.NewSource(0x5b2a))
	for _, length := range []int{16, 18, 30, 38} { // even lengths (nCols+overlap)
		for trial := 0; trial < 32; trial++ {
			buf := genBand(r, length)
			base := 2
			r11rG, r22rG, r01rG, r12rG, r02rG, detG, dsG, scG := sbr.RunAutoCorr2ndReal(buf, base, length)
			r11rC, r22rC, r01rC, r12rC, r02rC, detC, dsC, scC := cAutoCorr2ndReal(buf, base, length)
			require.Equal(t, scC, scG, "scaling len=%d trial=%d", length, trial)
			assert.Equal(t, r11rC, r11rG, "r11r")
			assert.Equal(t, r22rC, r22rG, "r22r")
			assert.Equal(t, r01rC, r01rG, "r01r")
			assert.Equal(t, r12rC, r12rG, "r12r")
			assert.Equal(t, r02rC, r02rG, "r02r")
			assert.Equal(t, detC, detG, "det")
			assert.Equal(t, dsC, dsG, "det_scale")
		}
	}
}

// TestAutoCorr2ndCplx pins autoCorr2nd_cplx over random complex low-band buffers.
func TestAutoCorr2ndCplx(t *testing.T) {
	r := rand.New(rand.NewSource(0x5b2b))
	for _, length := range []int{16, 18, 30, 38} {
		for trial := 0; trial < 32; trial++ {
			re := genBand(r, length)
			im := genBand(r, length)
			base := 2
			g := func() (a, b, c, d, e, f, g, h, i, j int32, k, l int) {
				return sbr.RunAutoCorr2ndCplx(re, im, base, length)
			}
			r00G, r11G, r22G, r01rG, r12rG, r01iG, r12iG, r02rG, r02iG, detG, dsG, scG := g()
			r00C, r11C, r22C, r01rC, r12rC, r01iC, r12iC, r02rC, r02iC, detC, dsC, scC := cAutoCorr2ndCplx(re, im, base, length)
			require.Equal(t, scC, scG, "scaling len=%d trial=%d", length, trial)
			assert.Equal(t, r00C, r00G, "r00r")
			assert.Equal(t, r11C, r11G, "r11r")
			assert.Equal(t, r22C, r22G, "r22r")
			assert.Equal(t, r01rC, r01rG, "r01r")
			assert.Equal(t, r12rC, r12rG, "r12r")
			assert.Equal(t, r01iC, r01iG, "r01i")
			assert.Equal(t, r12iC, r12iG, "r12i")
			assert.Equal(t, r02rC, r02rG, "r02r")
			assert.Equal(t, r02iC, r02iG, "r02i")
			assert.Equal(t, detC, detG, "det")
			assert.Equal(t, dsC, dsG, "det_scale")
		}
	}
}

// vKMasterStd is a representative SBR master frequency table (k0=lsb .. kx ..)
// for fs=44100, used to drive the patch generation and the full transposer.
var (
	vKMasterStd     = []uint8{12, 14, 16, 18, 20, 23, 26, 29, 32, 36, 40, 45, 51, 57, 64}
	noiseBandTblStd = []uint8{12, 24, 38, 64}
)

// TestResetLppTransposer pins the patch-layout generation (resetLppTransposer)
// across a sweep of crossover / stop bands.
func TestResetLppTransposer(t *testing.T) {
	cases := []struct {
		highBandStartSb, usb, timeSlots, nCols, noNoiseBands int
		fs                                                   uint
	}{
		{14, 64, 16, 32, 3, 44100},
		{16, 64, 15, 32, 3, 44100},
		{18, 57, 16, 32, 3, 48000},
		{20, 64, 16, 32, 2, 32000},
	}
	for ci, c := range cases {
		numMaster := len(vKMasterStd) - 1
		overlap := 8
		rcG, npG, lbStartG, lbStopG, ssG, sstopG, tsG, toG, gsG, nbG, bwG, whG :=
			sbr.RunResetLppTransposer(c.highBandStartSb, vKMasterStd, numMaster, c.usb, c.timeSlots, c.nCols,
				noiseBandTblStd, c.noNoiseBands, c.fs, overlap)
		rcC, npC, lbStartC, lbStopC, ssC, sstopC, tsC, toC, gsC, nbC, bwC, whC :=
			cResetLppTransposer(c.highBandStartSb, vKMasterStd, numMaster, c.usb, c.timeSlots, c.nCols,
				noiseBandTblStd, c.noNoiseBands, c.fs, overlap)

		require.Equal(t, rcC, rcG, "case %d return code", ci)
		if rcC != 0 {
			continue // both errored identically; nothing more to compare
		}
		assert.Equal(t, npC, npG, "case %d noOfPatches", ci)
		assert.Equal(t, lbStartC, lbStartG, "case %d lbStartPatching", ci)
		assert.Equal(t, lbStopC, lbStopG, "case %d lbStopPatching", ci)
		assert.Equal(t, ssC, ssG, "case %d sourceStartBand", ci)
		assert.Equal(t, sstopC, sstopG, "case %d sourceStopBand", ci)
		assert.Equal(t, tsC, tsG, "case %d targetStartBand", ci)
		assert.Equal(t, toC, toG, "case %d targetBandOffs", ci)
		assert.Equal(t, gsC, gsG, "case %d guardStartBand", ci)
		assert.Equal(t, nbC, nbG, "case %d numBandsInPatch", ci)
		assert.Equal(t, bwC, bwG, "case %d bwBorders", ci)
		assert.Equal(t, whC, whG, "case %d whFactors", ci)
	}
}

// genQmfBuffers builds slot-major QMF buffers (nSlots*64) of headroomed FIXP_DBL.
func genQmfBuffers(r *rand.Rand, nSlots int) (re, im []int32) {
	re = make([]int32, nSlots*64)
	im = make([]int32, nSlots*64)
	for i := range re {
		re[i] = int32(r.Int63()>>34) - (1 << 28)
		im[i] = int32(r.Int63()>>34) - (1 << 28)
	}
	return re, im
}

// TestCalculateGainVec pins the pre-flattening gain vector over random QMF energy.
func TestCalculateGainVec(t *testing.T) {
	r := rand.New(rand.NewSource(0x9f17))
	// numBands sweeps both polynomial-fit branches (<=POLY_ORDER+1 and >).
	for _, numBands := range []int{3, 5, 8, 16, 24, 32} {
		for trial := 0; trial < 8; trial++ {
			nSlots := 16
			overlap := 8
			reA, imA := genQmfBuffers(r, nSlots)
			reB := append([]int32(nil), reA...)
			imB := append([]int32(nil), imA...)

			gainG, expG := sbr.RunCalculateGainVec(reA, imA, nSlots, 2, 1, overlap, numBands, 0, nSlots)
			gainC, expC := cCalculateGainVec(reB, imB, nSlots, 2, 1, overlap, numBands, 0, nSlots)
			assert.Equal(t, gainC, gainG, "gain numBands=%d trial=%d", numBands, trial)
			assert.Equal(t, expC, expG, "gainExp numBands=%d trial=%d", numBands, trial)
		}
	}
}

// TestLppTransposer pins the full LPP transposer (patch + whitening) end-to-end,
// in both the useLP (real-only) and high-quality (complex) modes.
func TestLppTransposer(t *testing.T) {
	r := rand.New(rand.NewSource(0xa113))
	numMaster := len(vKMasterStd) - 1
	for _, useLP := range []bool{true, false} {
		for trial := 0; trial < 16; trial++ {
			const nSlots = 40 // nCols(32) + overlap(8) headroom
			reA, imA := genQmfBuffers(r, nSlots)
			reB := append([]int32(nil), reA...)
			imB := append([]int32(nil), imA...)

			nInvf := 4
			invfMod := []int{1, 2, 3, 0}
			invfModPrev := []int{0, 1, 2, 3}

			highBandStartSb, usb, timeSlots, nCols := 14, 64, 16, 32
			noNoiseBands := 3
			overlap := 8
			fs := uint(44100)
			lbScale, ovLbScale := 5, 5
			vKMaster0 := int(vKMasterStd[0])

			degG, hbG := func() ([]int32, int) {
				_, _, dg, hb := sbr.RunLppTransposer(reA, imA, nSlots,
					highBandStartSb, vKMasterStd, numMaster, usb, timeSlots, nCols,
					noiseBandTblStd, noNoiseBands, fs, overlap, lbScale, ovLbScale,
					useLP, false, vKMaster0, 2, 0, 0, nInvf, invfMod, invfModPrev)
				return dg, hb
			}()
			degC, hbC := cLppTransposer(reB, imB, nSlots,
				highBandStartSb, vKMasterStd, numMaster, usb, timeSlots, nCols,
				noiseBandTblStd, noNoiseBands, fs, overlap, lbScale, ovLbScale,
				useLP, false, vKMaster0, 2, 0, 0, nInvf, invfMod, invfModPrev)

			require.Equal(t, hbC, hbG, "hb_scale useLP=%v trial=%d", useLP, trial)
			assert.Equal(t, reB, reA, "qmfReal useLP=%v trial=%d", useLP, trial)
			if !useLP {
				assert.Equal(t, imB, imA, "qmfImag useLP=%v trial=%d", useLP, trial)
			}
			assert.Equal(t, degC, degG, "degreeAlias useLP=%v trial=%d", useLP, trial)
		}
	}
}
