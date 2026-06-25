// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psencextract

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// goExtract drives the pure-Go FDKsbrEnc_PSEncode port (stateful across frames).
func goExtract(st *sbr.PSEncodeState, hybridFlat []int32, dynBandScale []uint8, maxEnvelopes, frameSize, sendHeader int) sbr.PSEncodeResult {
	return st.Encode(hybridFlat, dynBandScale, maxEnvelopes, frameSize, sendHeader)
}

// genHybrid fills a [HYBRID_FRAMESIZE][2 ch][2 reim][71] flat int32 buffer with
// random fixed-point values in a magnitude range that won't overflow the
// `<< dynBandScale`/`<< sc` shifts inside the extractor. mag controls the
// stereo image (left vs right correlation) loosely.
func genHybrid(rng *rand.Rand, mag int32) []int32 {
	out := make([]int32, hybFramesize*2*2*nbHybrid)
	for i := range out {
		// values in [-mag, mag], kept small so left<<scale stays in range.
		out[i] = rng.Int31n(2*mag+1) - mag
	}
	return out
}

func assertResultEqual(t *testing.T, c psExtractResult, g sbr.PSEncodeResult, frame int) {
	t.Helper()
	require.Equal(t, c.enablePSHeader, g.EnablePSHeader, "frame %d enablePSHeader", frame)
	require.Equal(t, c.enableIID, g.EnableIID, "frame %d enableIID", frame)
	require.Equal(t, c.iidMode, g.IidMode, "frame %d iidMode", frame)
	require.Equal(t, c.enableICC, g.EnableICC, "frame %d enableICC", frame)
	require.Equal(t, c.iccMode, g.IccMode, "frame %d iccMode", frame)
	require.Equal(t, c.frameClass, g.FrameClass, "frame %d frameClass", frame)
	require.Equal(t, c.nEnvelopes, g.NEnvelopes, "frame %d nEnvelopes", frame)
	for i := 0; i < 4; i++ {
		assert.Equal(t, c.frameBorder[i], int32(g.FrameBorder[i]), "frame %d frameBorder[%d]", frame, i)
		assert.Equal(t, c.deltaIID[i], int32(g.DeltaIID[i]), "frame %d deltaIID[%d]", frame, i)
		assert.Equal(t, c.deltaICC[i], int32(g.DeltaICC[i]), "frame %d deltaICC[%d]", frame, i)
	}
	for e := 0; e < 4; e++ {
		for b := 0; b < 20; b++ {
			assert.Equal(t, c.iidFlat[e*20+b], int32(g.Iid[e][b]), "frame %d iid[%d][%d]", frame, e, b)
			assert.Equal(t, c.iccFlat[e*20+b], int32(g.Icc[e][b]), "frame %d icc[%d][%d]", frame, e, b)
		}
	}
	for b := 0; b < 20; b++ {
		assert.Equal(t, c.iidLast[b], int32(g.IidLast[b]), "frame %d iidLast[%d]", frame, b)
		assert.Equal(t, c.iccLast[b], int32(g.IccLast[b]), "frame %d iccLast[%d]", frame, b)
	}
}

func TestPSExtractParity(t *testing.T) {
	type cfg struct {
		name         string
		psEncMode    int   // 10 (coarse) or 20 (mid)
		iidQuantThr  int32 // iidQuantErrorThreshold (FIXP_DBL)
		maxEnvelopes int
		frames       int
		mag          int32
	}
	cfgs := []cfg{
		{"coarse_2env", 10, 0x20000000, 2, 4, 1 << 20},
		{"mid_2env", 20, 0x20000000, 2, 4, 1 << 20},
		{"coarse_4env", 10, 0x10000000, 4, 4, 1 << 19},
		{"mid_1env", 20, 0x30000000, 1, 4, 1 << 21},
		{"coarse_small_mag", 10, 0x20000000, 2, 5, 1 << 12},
	}

	for _, cf := range cfgs {
		t.Run(cf.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(0xE471 + len(cf.name))))

			cSt := cNewExtract(cf.psEncMode, cf.iidQuantThr)
			defer cSt.free()
			gSt := sbr.NewPSEncodeParity(cf.psEncMode, cf.iidQuantThr)

			frameSize := hybFramesize
			for f := 0; f < cf.frames; f++ {
				hyb := genHybrid(rng, cf.mag)
				// dynBandScale: a modest per-band scale (psFindBestScaling output
				// surrogate). Identical on both sides; kept small to avoid overflow.
				dyn := make([]uint8, psMaxBands)
				for b := 0; b < psMaxBands; b++ {
					dyn[b] = uint8(rng.Intn(3))
				}
				sendHeader := 0
				if f == 0 {
					sendHeader = 1
				}

				cRes := cSt.run(hyb, dyn, cf.maxEnvelopes, frameSize, sendHeader)
				gRes := goExtract(gSt, hyb, dyn, cf.maxEnvelopes, frameSize, sendHeader)
				assertResultEqual(t, cRes, gRes, f)
			}
		})
	}
}
