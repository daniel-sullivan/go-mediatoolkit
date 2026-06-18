// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrquantizeframe

import "go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 VBR_encode_frame the same way oracle.c
// drives the vendored C: a goHandle carries a LameInternalFlags (mirroring the
// oracle's calloc'd gfc) and each wrapper forwards the identical flat inputs into
// the nativemp3 parity hook (parityhooks_vbrquantize_frame.go). Keeping the Go
// drivers beside the cgo bridge mirrors the C oracle structure so the two sides
// of each assertion are visibly symmetric. These import only internal/nativemp3
// (never libraries/mp3).

func goFillTables() { nativemp3.FillVbrQuantizeTables() }

// goHandle mirrors cgoHandle: a LameInternalFlags with the 2x2 l3_side.tt
// gr_info grid so a test can populate cfg + per-granule geometry + inputs, drive
// VBR_encode_frame and read the resolved side info from the same Go-side state.
type goHandle struct {
	gfc *nativemp3.LameInternalFlags
}

func goNewHandle() *goHandle { return &goHandle{gfc: new(nativemp3.LameInternalFlags)} }

func (g *goHandle) setCfg(modeGr, channelsOut, noiseShaping, fullOuterLoop, useBestHuffman int) {
	g.gfc.Cfg.ModeGr = modeGr
	g.gfc.Cfg.ChannelsOut = channelsOut
	g.gfc.Cfg.NoiseShaping = noiseShaping
	g.gfc.Cfg.FullOuterLoop = fullOuterLoop
	g.gfc.Cfg.UseBestHuffman = useBestHuffman
}

func (g *goHandle) setSfbLong(l []int) {
	for i, x := range l {
		g.gfc.ScalefacBand.L[i] = x
	}
}
func (g *goHandle) setSfbShort(s []int) {
	for i, x := range s {
		g.gfc.ScalefacBand.S[i] = x
	}
}
func (g *goHandle) huffmanInit() { g.gfc.HuffmanInit() }

func (g *goHandle) gi(gr, ch int) *nativemp3.GrInfo { return &g.gfc.L3Side.Tt[gr][ch] }

func (g *goHandle) setXr(gr, ch int, xr []float32) {
	gi := g.gi(gr, ch)
	for i, x := range xr {
		gi.Xr[i] = x
	}
}
func (g *goHandle) setWidth(gr, ch int, w []int) {
	gi := g.gi(gr, ch)
	for i, x := range w {
		gi.Width[i] = x
	}
}
func (g *goHandle) setWindow(gr, ch int, win []int) {
	gi := g.gi(gr, ch)
	for i, x := range win {
		gi.Window[i] = x
	}
}
func (g *goHandle) setEac(gr, ch int, eac []byte) {
	gi := g.gi(gr, ch)
	for i, x := range eac {
		gi.EnergyAboveCutoff[i] = x
	}
}
func (g *goHandle) setGeom(gr, ch, blockType, mixedBlockFlag, sfbmax, sfbdivide, psymax, maxNonzeroCoeff int, xrpowMax float32) {
	gi := g.gi(gr, ch)
	gi.BlockType = blockType
	gi.MixedBlockFlag = mixedBlockFlag
	gi.Sfbmax = sfbmax
	gi.Sfbdivide = sfbdivide
	gi.Psymax = psymax
	gi.MaxNonzeroCoeff = maxNonzeroCoeff
	gi.XrpowMax = xrpowMax
}

// encode drives the pure-Go VBRencodeFrame and returns its bit-usage result.
func (g *goHandle) encode(xr34orig, l3Xmin []float32, maxBits []int) int {
	var x34 [2][2][576]float32
	var xmin [2][2][nativemp3.SFBMAX]float32
	var mb [2][2]int
	const sfbmaxN = nativemp3.SFBMAX
	for gr := 0; gr < 2; gr++ {
		for ch := 0; ch < 2; ch++ {
			copy(x34[gr][ch][:], xr34orig[(gr*2+ch)*576:(gr*2+ch)*576+576])
			copy(xmin[gr][ch][:], l3Xmin[(gr*2+ch)*sfbmaxN:(gr*2+ch)*sfbmaxN+sfbmaxN])
			mb[gr][ch] = maxBits[gr*2+ch]
		}
	}
	return g.gfc.VBRencodeFrameParity(&x34, &xmin, &mb)
}

func (g *goHandle) globalGain(gr, ch int) int       { return g.gi(gr, ch).GlobalGain }
func (g *goHandle) scalefacScale(gr, ch int) int    { return g.gi(gr, ch).ScalefacScale }
func (g *goHandle) preflag(gr, ch int) int          { return g.gi(gr, ch).Preflag }
func (g *goHandle) scalefac(gr, ch, sfb int) int    { return g.gi(gr, ch).Scalefac[sfb] }
func (g *goHandle) subblockGain(gr, ch, i int) int  { return g.gi(gr, ch).SubblockGain[i] }
func (g *goHandle) l3enc(gr, ch, i int) int         { return g.gi(gr, ch).L3Enc[i] }
func (g *goHandle) part23Length(gr, ch int) int     { return g.gi(gr, ch).Part23Length }
func (g *goHandle) part2Length(gr, ch int) int      { return g.gi(gr, ch).Part2Length }
func (g *goHandle) scalefacCompress(gr, ch int) int { return g.gi(gr, ch).ScalefacCompress }
func (g *goHandle) bigValues(gr, ch int) int        { return g.gi(gr, ch).BigValues }
func (g *goHandle) tableSelect(gr, ch, i int) int   { return g.gi(gr, ch).TableSelect[i] }
func (g *goHandle) region0Count(gr, ch int) int     { return g.gi(gr, ch).Region0Count }
func (g *goHandle) region1Count(gr, ch int) int     { return g.gi(gr, ch).Region1Count }
func (g *goHandle) scfsi(ch, band int) int          { return g.gfc.L3Side.Scfsi[ch][band] }
