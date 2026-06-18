// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencanalysis

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// qmfVals fills a flat QMF buffer with bounded signed FIXP_DBL values (kept with
// headroom so the energy left-shifts don't overflow; both sides see same bytes).
func qmfVals(rng *rand.Rand, n int, shift uint) []int32 {
	v := make([]int32, n)
	for i := range v {
		v[i] = int32(rng.Uint32()) >> (1 + shift)
	}
	return v
}

func TestGetEnergyFromCplxQmfDataParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0xE0E0))
	for iter := 0; iter < 30; iter++ {
		numberBands := 8 + rng.Intn(56) // up to 64
		numberCols := 16                // even (timeslot-pair)
		qmfScaleIn := rng.Intn(10)
		real := qmfVals(rng, numberCols*numberBands, uint(rng.Intn(4)))
		imag := qmfVals(rng, numberCols*numberBands, uint(rng.Intn(4)))

		gE, gR, gI, gqs, ges := sbr.RunGetEnergyFromCplxQmfData(append([]int32(nil), real...), append([]int32(nil), imag...), numberBands, numberCols, qmfScaleIn)
		cE, cR, cI, cqs, ces := cEnergy(real, imag, numberBands, numberCols, qmfScaleIn)

		require.Equal(t, cqs, gqs, "iter %d qmfScale", iter)
		require.Equal(t, ces, ges, "iter %d energyScale", iter)
		require.Equal(t, cE, gE, "iter %d energy", iter)
		require.Equal(t, cR, gR, "iter %d real (mutated)", iter)
		require.Equal(t, cI, gI, "iter %d imag (mutated)", iter)
	}
}

func TestGetEnergyFromCplxQmfDataFullParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0xF1F1))
	for iter := 0; iter < 30; iter++ {
		numberBands := 8 + rng.Intn(56)
		numberCols := 1 + rng.Intn(16) // up to MAX_NRG_SLOTS_LD
		qmfScaleIn := rng.Intn(10)
		real := qmfVals(rng, numberCols*numberBands, uint(rng.Intn(4)))
		imag := qmfVals(rng, numberCols*numberBands, uint(rng.Intn(4)))

		gE, gR, gI, gqs, ges := sbr.RunGetEnergyFromCplxQmfDataFull(append([]int32(nil), real...), append([]int32(nil), imag...), numberBands, numberCols, qmfScaleIn)
		cE, cR, cI, cqs, ces := cEnergyFull(real, imag, numberBands, numberCols, qmfScaleIn)

		require.Equal(t, cqs, gqs, "iter %d qmfScale", iter)
		require.Equal(t, ces, ges, "iter %d energyScale", iter)
		require.Equal(t, cE, gE, "iter %d energy", iter)
		require.Equal(t, cR, gR, "iter %d real", iter)
		require.Equal(t, cI, gI, "iter %d imag", iter)
	}
}

func TestGetTonalityParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x7077))
	for iter := 0; iter < 30; iter++ {
		const qmfChannels = 64
		totEst := 4
		numberCols := 15
		if rng.Intn(2) == 0 {
			numberCols = 16
		}
		noEstPerFrame := 2
		startBand := rng.Intn(10)
		stopBand := startBand + 5 + rng.Intn(40)
		if stopBand > qmfChannels {
			stopBand = qmfChannels
		}

		quota := qmfVals(rng, totEst*qmfChannels, 2)
		for i := range quota { // tonality is non-negative
			if quota[i] < 0 {
				quota[i] = -quota[i]
			}
		}
		energy := qmfVals(rng, 16*qmfChannels, 3)
		for i := range energy {
			if energy[i] < 0 {
				energy[i] = -energy[i]
			}
		}

		g := sbr.RunGetTonality(quota, totEst, qmfChannels, energy, numberCols, noEstPerFrame, 0, startBand, stopBand)
		c := cTonality(quota, totEst, qmfChannels, energy, numberCols, noEstPerFrame, 0, startBand, stopBand)
		require.Equal(t, c, g, "iter %d", iter)
	}
}

func TestMapPanoramaParity(t *testing.T) {
	for ampRes := 0; ampRes <= 1; ampRes++ {
		for nrg := -40; nrg <= 40; nrg++ {
			gp, gqe := sbr.RunMapPanorama(nrg, ampRes)
			cp, cqe := cMapPanorama(nrg, ampRes)
			require.Equal(t, cp, gp, "ampRes %d nrg %d pan", ampRes, nrg)
			require.Equal(t, cqe, gqe, "ampRes %d nrg %d quantErr", ampRes, nrg)
		}
	}
}

func TestNoiseFloorQuantParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x1110))
	for iter := 0; iter < 50; iter++ {
		nl := make([]int32, 10)
		for i := range nl {
			nl[i] = int32(rng.Uint32()>>2) - (1 << 28)
		}
		for _, coupling := range []int{0, 1} {
			g := sbr.RunSbrNoiseFloorLevelsQuantisation(append([]int32(nil), nl...), coupling)
			c := cNoiseQuant(nl, coupling)
			require.Equal(t, c, g, "iter %d coupling %d", iter, coupling)
		}
	}
}

func TestCoupleNoiseFloorParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x2220))
	for iter := 0; iter < 50; iter++ {
		left := make([]int32, 10)
		right := make([]int32, 10)
		for i := range left {
			left[i] = int32(rng.Uint32()>>2) - (1 << 28)
			right[i] = int32(rng.Uint32()>>2) - (1 << 28)
		}
		gl, gr := sbr.RunCoupleNoiseFloor(append([]int32(nil), left...), append([]int32(nil), right...))
		cl, cr := cCoupleNoise(left, right)
		require.Equal(t, cl, gl, "iter %d left", iter)
		require.Equal(t, cr, gr, "iter %d right", iter)
	}
}

func TestGetEnvSfbEnergyParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x3330))
	for iter := 0; iter < 40; iter++ {
		const qmfChannels = 64
		numYRows := 16
		yBufferSzShift := 0
		startPos := 0
		stopPos := numYRows
		borderPos := rng.Intn(numYRows + 1)
		li := rng.Intn(40)
		ui := li + 1 + rng.Intn(20)
		if ui > qmfChannels {
			ui = qmfChannels
		}
		scaleNrg0 := rng.Intn(20)
		scaleNrg1 := rng.Intn(20)

		y := qmfVals(rng, numYRows*qmfChannels, 2)
		for i := range y {
			if y[i] < 0 {
				y[i] = -y[i]
			}
		}

		g := sbr.RunGetEnvSfbEnergy(li, ui, startPos, stopPos, borderPos, append([]int32(nil), y...), numYRows, qmfChannels, yBufferSzShift, scaleNrg0, scaleNrg1)
		c := cEnvSfbEnergy(li, ui, startPos, stopPos, borderPos, y, numYRows, qmfChannels, yBufferSzShift, scaleNrg0, scaleNrg1)
		require.Equal(t, c, g, "iter %d", iter)
	}
}

func TestLoweringEnergyParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x4440))
	for iter := 0; iter < 60; iter++ {
		nrg := int32(rng.Uint32() >> 1)
		M := rng.Intn(8)
		require.Equal(t, cMhLowering(nrg, M), sbr.RunMhLoweringEnergy(nrg, M), "mh iter %d M %d", iter, M)

		nrgSum := int32(rng.Uint32() >> 1)
		nrgSumScale := rng.Intn(20) - 5
		require.Equal(t, cNmhLowering(nrg, nrgSum, nrgSumScale, M), sbr.RunNmhLoweringEnergy(nrg, nrgSum, nrgSumScale, M), "nmh iter %d", iter)
	}
}
