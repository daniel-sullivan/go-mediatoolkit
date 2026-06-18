// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// Exported driver for the sbr-enc-analysis frame-generator parity slice: it
// inits the generator, runs N frames feeding the supplied transient infos, and
// returns the resulting SbrFrameInfo fields (flattened) so the oracle can assert
// EXACT int equality against the vendored FDKsbrEnc_frameInfoGenerator. Running
// multiple frames exercises the cross-frame follow-up state (VARFIX/VARVAR).

// FrameInfoResult is a flat snapshot of one frame's SbrFrameInfo for comparison.
type FrameInfoResult struct {
	NEnvelopes      int
	Borders         []int
	FreqRes         []int
	ShortEnv        int
	NNoiseEnvelopes int
	BordersNoise    []int
	// Grid clear-text fields that feed the bitstream (also compared).
	FrameClass int
	BsNumEnv   int
	BsAbsBord  int
	N          int
	P          int
	BsAbsBord0 int
	BsAbsBord1 int
	BsNumRel0  int
	BsNumRel1  int
}

// snapshot copies the load-bearing SbrFrameInfo + SbrGrid fields.
func snapshot(fi *SbrFrameInfo, g *SbrGrid) FrameInfoResult {
	r := FrameInfoResult{
		NEnvelopes:      fi.NEnvelopes,
		ShortEnv:        fi.ShortEnv,
		NNoiseEnvelopes: fi.NNoiseEnvelopes,
		FrameClass:      int(g.FrameClass),
		BsNumEnv:        g.BsNumEnv,
		BsAbsBord:       g.BsAbsBord,
		N:               g.N,
		P:               g.P,
		BsAbsBord0:      g.BsAbsBord0,
		BsAbsBord1:      g.BsAbsBord1,
		BsNumRel0:       g.BsNumRel0,
		BsNumRel1:       g.BsNumRel1,
	}
	r.Borders = append([]int(nil), fi.Borders[:]...)
	for _, f := range fi.FreqRes {
		r.FreqRes = append(r.FreqRes, int(f))
	}
	r.BordersNoise = append([]int(nil), fi.BordersNoise[:]...)
	return r
}

// RunFrameInfoGenerator inits the generator and runs len(tranInfos) frames.
// tranInfos[i] is the 3-byte transient_info for frame i; tranInfosPre[i] the
// pre-transient info (LD only). Returns the per-frame snapshots.
func RunFrameInfoGenerator(allowSpread, numEnvStatic, staticFraming, timeSlots int, freqResFixfix []FreqRes, fResTransIsLow uint8, ldGrid int, vTuning []int, tranInfos, tranInfosPre [][]uint8, rightBorderFIX []int) []FrameInfoResult {
	var h SbrEnvelopeFrame
	InitFrameInfoGenerator(&h, allowSpread, numEnvStatic, staticFraming, timeSlots, freqResFixfix, fResTransIsLow, ldGrid)

	out := make([]FrameInfoResult, 0, len(tranInfos))
	for i := range tranInfos {
		pre := []uint8{0, 0, 0}
		if i < len(tranInfosPre) && tranInfosPre[i] != nil {
			pre = tranInfosPre[i]
		}
		rbf := 0
		if i < len(rightBorderFIX) {
			rbf = rightBorderFIX[i]
		}
		fi := FrameInfoGenerator(&h, tranInfos[i], rbf, pre, ldGrid, vTuning)
		out = append(out, snapshot(fi, &h.SbrGrid))
	}
	return out
}
