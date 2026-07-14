//go:build cgo && rnnoise_strict

package bands

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/rnnoise"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bitEq(t *testing.T, ctx string, i int, c, g float32) {
	t.Helper()
	cb, gb := math.Float32bits(c), math.Float32bits(g)
	assert.Equalf(t, cb, gb, "%s[%d]: C=%v (0x%08x) Go=%v (0x%08x)", ctx, i, c, cb, g, gb)
}

// spectra returns FreqSize (parallel r/i) spectra derived from the FFT of
// varied signals, so band energies span realistic magnitudes.
func spectra(seed int64) (xr, xi []float32) {
	n := rnnoise.WindowSize
	sig := make([]float32, n)
	r := rand.New(rand.NewSource(seed))
	for i := range sig {
		sig[i] = float32(6000*math.Sin(2*math.Pi*250*float64(i)/48000)) +
			float32(r.Intn(20000)-10000)
	}
	return rnnoise.ForwardTransform(sig)
}

func TestEbandTableParity(t *testing.T) {
	g := rnnoise.Eband()
	require.Len(t, g, 34)
	for i := range g {
		assert.Equalf(t, cEband(i), g[i], "eband20ms[%d]", i)
	}
}

func TestBandEnergyParity(t *testing.T) {
	for s := int64(1); s <= 4; s++ {
		xr, xi := spectra(s)
		c := cBandEnergy(xr, xi, rnnoise.NBBands)
		g := rnnoise.ComputeBandEnergy(xr, xi)
		for i := range c {
			bitEq(t, "bandE", i, c[i], g[i])
		}
	}
}

func TestBandCorrParity(t *testing.T) {
	for s := int64(1); s <= 4; s++ {
		xr, xi := spectra(s)
		pr, pi := spectra(s + 100)
		c := cBandCorr(xr, xi, pr, pi, rnnoise.NBBands)
		g := rnnoise.ComputeBandCorr(xr, xi, pr, pi)
		for i := range c {
			bitEq(t, "bandCorr", i, c[i], g[i])
		}
	}
}

func TestInterpBandGainParity(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for tc := 0; tc < 4; tc++ {
		bandE := make([]float32, rnnoise.NBBands)
		for i := range bandE {
			bandE[i] = r.Float32()*2 - 0.5
		}
		c := cInterpBandGain(bandE, rnnoise.FreqSize)
		g := rnnoise.InterpBandGain(bandE)
		require.Len(t, g, len(c))
		for i := range c {
			bitEq(t, "interp", i, c[i], g[i])
		}
	}
}

func TestDctParity(t *testing.T) {
	r := rand.New(rand.NewSource(13))
	for tc := 0; tc < 6; tc++ {
		in := make([]float32, rnnoise.NBBands)
		for i := range in {
			in[i] = r.Float32()*8 - 4
		}
		c := cDct(in, rnnoise.NBBands)
		g := rnnoise.Dct(in)
		for i := range c {
			bitEq(t, "dct", i, c[i], g[i])
		}
	}
}
