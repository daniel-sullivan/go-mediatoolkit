// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencanalysis

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// makeEnergies builds a flat (rows*cols) FIXP_DBL energy matrix of bounded
// magnitude (energies are non-negative QMF magnitudes, kept well below 2^31 to
// avoid the FDK_ASSERT overflow guards in the C; both sides see the same bytes).
func makeEnergies(rng *rand.Rand, rows, cols int, maxShift uint) []int32 {
	e := make([]int32, rows*cols)
	for i := range e {
		e[i] = int32(rng.Uint32() >> (1 + maxShift)) // non-negative, headroom
	}
	return e
}

func TestInitSbrFastTransientDetectorParity(t *testing.T) {
	cases := []struct {
		slots, bw, ch, firstBand int
	}{
		{16, 1, 64, 8},
		{16, 1, 64, 5},
		{15, 1, 64, 6},
		{16, 1, 32, 4},
	}
	for _, tc := range cases {
		gM, gE, gSb, gEb := sbr.RunInitFastTransientDetector(tc.slots, tc.bw, tc.ch, tc.firstBand)
		cM, cE, cSb, cEb := cInitFastTran(tc.slots, tc.bw, tc.ch, tc.firstBand)
		require.Equal(t, cSb, gSb, "startBand")
		require.Equal(t, cEb, gEb, "stopBand")
		assert.Equal(t, cM, gM, "dBf_m ROM")
		assert.Equal(t, cE, gE, "dBf_e ROM")
	}
}

func TestFastTransientDetectParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0xABCDEF))
	const ch = 64
	for iter := 0; iter < 40; iter++ {
		slots := 16
		lookahead := 2
		rows := slots + lookahead
		yb := slots / 2
		energy := makeEnergies(rng, rows, ch, 2)
		sc := []int{int(rng.Intn(20)) + 1, int(rng.Intn(20)) + 1}

		g := sbr.RunFastTransientDetect(energy, rows, ch, sc, yb, slots, 1, 8)
		c := cFastTran(energy, rows, ch, sc, yb, slots, 1, 8)
		require.Equal(t, c, g, "iter %d sc=%v", iter, sc)
	}
}

func TestInitSbrTransientDetectorParity(t *testing.T) {
	cases := []struct {
		lowDelay                                                       int
		frameSize, sampleFreq, stdBitrate, nCh, codecBitrate           int
		tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tOff int
	}{
		{0, 2048, 44100, 128000, 2, 128000, 850, 1, 0, 32, 64, 0, 8},
		{0, 2048, 48000, 64000, 1, 64000, 1000, 1, 0, 32, 48, 0, 8},
		{0, 1024, 32000, 32000, 1, 0, 500, 1, 0, 16, 32, 0, 4},
		{1, 512, 48000, 64000, 1, 64000, 850, 1, 0, 8, 32, 0, 0},
	}
	for i, tc := range cases {
		gtt, gsm, gse := sbr.RunInitTransientDetector(tc.lowDelay != 0, tc.frameSize, tc.sampleFreq, tc.stdBitrate, tc.nCh, tc.codecBitrate, tc.tranThr, tc.tranDetMode, tc.tranFc, tc.noCols, tc.noRows, tc.frameShift, tc.tOff)
		ctt, csm, cse := cInitTran(tc.lowDelay, tc.frameSize, tc.sampleFreq, tc.stdBitrate, tc.nCh, tc.codecBitrate, tc.tranThr, tc.tranDetMode, tc.tranFc, tc.noCols, tc.noRows, tc.frameShift, tc.tOff)
		require.Equal(t, ctt, gtt, "case %d tran_thr", i)
		require.Equal(t, csm, gsm, "case %d split_thr_m", i)
		require.Equal(t, cse, gse, "case %d split_thr_e", i)
	}
}

func TestTransientDetectParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x12345))
	const ch = 64
	for iter := 0; iter < 40; iter++ {
		noCols := 32
		noRows := 64
		yBufferSzShift := 1
		tranOff := 8
		yb := 16 // YBufferWriteOffset
		timeStep := 2
		frameMiddleBorder := 8

		// Energy matrix: extractTransientCandidates reads up to
		// endEnerg = ((noCols + (yb<<szShift)) - 1) >> szShift inclusive, and
		// calculateThresholds up to (noCols>>szShift)+tranOff. Provide enough rows
		// to cover the larger of the two (plus a small margin), matching the C's
		// statically-sized energy buffer.
		endEnerg := ((noCols + (yb << yBufferSzShift)) - 1) >> yBufferSzShift
		rows := endEnerg + 2
		if r := (noCols >> yBufferSzShift) + tranOff + 2; r > rows {
			rows = r
		}
		energy := makeEnergies(rng, rows, ch, 3)
		sc := []int{rng.Intn(15) + 1, rng.Intn(15) + 1}

		gti, gthr, gtran := sbr.RunTransientDetect(energy, rows, ch, sc, false,
			2048, 44100, 128000, 2, 128000, 850, 1, 0, noCols, noRows, 0, tranOff,
			yb, yBufferSzShift, timeStep, frameMiddleBorder)
		cti, cthr, ctran := cTran(energy, rows, ch, sc, 0,
			2048, 44100, 128000, 2, 128000, 850, 1, 0, noCols, noRows, 0, tranOff,
			yb, yBufferSzShift, timeStep, frameMiddleBorder)

		require.Equal(t, cthr, gthr, "iter %d thresholds", iter)
		require.Equal(t, ctran, gtran, "iter %d transients", iter)
		require.Equal(t, cti, gti, "iter %d transient_info", iter)
	}
}

func TestFrameSplitterParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x99))
	const ch = 64
	for iter := 0; iter < 40; iter++ {
		noCols := 32
		noRows := 64
		yBufferSzShift := 1
		tranOff := 8
		yb := 16
		timeStep := 2
		nSfb := 8

		// freqBandTable: LORES table, freqBandTable[0] = #lowbands, then nSfb+1
		// monotonically increasing band edges within ch.
		fbt := make([]uint8, nSfb+1)
		fbt[0] = 20
		for j := 1; j <= nSfb; j++ {
			fbt[j] = fbt[j-1] + uint8(2+rng.Intn(3))
		}

		rows := (noCols >> yBufferSzShift) + tranOff + 4
		energy := makeEnergies(rng, rows, ch, 3)
		sc := []int{rng.Intn(15) + 1, rng.Intn(15) + 1}
		prevLow := int32(rng.Uint32() >> 4)
		tranIn := []uint8{0, 0, 0} // no transient -> splitter active
		tonIn := int32(rng.Uint32() >> 2)

		gtv, gpl, gph, gtn := sbr.RunFrameSplitter(energy, rows, ch, sc, false,
			2048, 44100, 128000, 2, 128000, 850, 1, 0, noCols, noRows, 0, tranOff,
			prevLow, fbt, tranIn, yb, yBufferSzShift, nSfb, timeStep, tonIn)
		ctv, cpl, cph, ctn := cFrameSplitter(energy, rows, ch, sc, 0,
			2048, 44100, 128000, 2, 128000, 850, 1, 0, noCols, noRows, 0, tranOff,
			prevLow, fbt, tranIn, yb, yBufferSzShift, nSfb, timeStep, tonIn)

		require.Equal(t, ctv, gtv, "iter %d tran_vector", iter)
		require.Equal(t, cpl, gpl, "iter %d prevLow", iter)
		require.Equal(t, cph, gph, "iter %d prevHigh", iter)
		require.Equal(t, ctn, gtn, "iter %d tonality", iter)
	}
}
