// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbriterationloop

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 VBR iteration loops the same way
// oracle.c drives the vendored C: a goHandle carries a LameInternalFlags
// (mirroring the oracle's calloc'd gfc + ATH + ratio grid) and each wrapper sets
// the identical field the cgo setter sets, then drives the loop through the
// nativemp3 parity hook (parityhooks_quantize_vbr.go). It imports only
// internal/nativemp3 (never libraries/mp3).

func goFillTables() { nativemp3.FillVbrQuantizeTables() }

// goHandle mirrors cgoHandle: a LameInternalFlags + an ATH + a 2x2
// III_psy_ratio grid so a test can populate cfg / sv_qnt / ATH / reservoir /
// scalefac_band / geometry + inputs, drive an iteration loop and read the
// resolved frame output from the same Go-side state.
type goHandle struct {
	gfc   *nativemp3.LameInternalFlags
	ath   nativemp3.ATH
	ratio [2][2]nativemp3.III_psy_ratio
}

func goNewHandle() *goHandle {
	g := &goHandle{gfc: new(nativemp3.LameInternalFlags)}
	g.gfc.ATH = &g.ath
	return g
}

func (g *goHandle) setCfg(modeGr, channelsOut, version, samplerateOut, avgBitrate,
	sideinfoLen, bufferConstraint, vbrMin, vbrMax, disableResv, freeFormat,
	enforceMin, modeExt int) {
	g.gfc.Cfg.ModeGr = modeGr
	g.gfc.Cfg.ChannelsOut = channelsOut
	g.gfc.Cfg.Version = version
	g.gfc.Cfg.SamplerateOut = samplerateOut
	g.gfc.Cfg.AvgBitrate = avgBitrate
	g.gfc.Cfg.SideinfoLen = sideinfoLen
	g.gfc.Cfg.BufferConstraint = bufferConstraint
	g.gfc.Cfg.VbrMinBitrateIndex = vbrMin
	g.gfc.Cfg.VbrMaxBitrateIndex = vbrMax
	g.gfc.Cfg.DisableReservoir = disableResv
	g.gfc.Cfg.FreeFormat = freeFormat
	g.gfc.Cfg.EnforceMinBitrate = enforceMin
	g.gfc.OvEnc.ModeExt = modeExt
}

func (g *goHandle) setCfgQuant(noiseShaping, fullOuterLoop, useBestHuffman int, athFixpoint, athCurve float32, athType int) {
	g.gfc.Cfg.NoiseShaping = noiseShaping
	g.gfc.Cfg.FullOuterLoop = fullOuterLoop
	g.gfc.Cfg.UseBestHuffman = useBestHuffman
	g.gfc.Cfg.ATHfixpoint = athFixpoint
	g.gfc.Cfg.ATHcurve = athCurve
	g.gfc.Cfg.ATHtype = athType
}

func (g *goHandle) setResv(resvSize, resvMax int) {
	g.gfc.SvEnc.ResvSize = resvSize
	g.gfc.SvEnc.ResvMax = resvMax
}

func (g *goHandle) setBinsearch(oldValue, currentStep int) {
	for ch := 0; ch < 2; ch++ {
		g.gfc.SvQnt.OldValue[ch] = oldValue
		g.gfc.SvQnt.CurrentStep[ch] = currentStep
	}
}

func (g *goHandle) setSvQnt(maskAdjust, maskAdjustShort float32, substepShaping, sfb21Extra int) {
	g.gfc.SvQnt.MaskAdjust = maskAdjust
	g.gfc.SvQnt.MaskAdjustShort = maskAdjustShort
	g.gfc.SvQnt.SubstepShaping = substepShaping
	g.gfc.SvQnt.Sfb21Extra = sfb21Extra
}

func (g *goHandle) setLongfact(lf []float32) {
	for i, x := range lf {
		g.gfc.SvQnt.Longfact[i] = x
	}
}
func (g *goHandle) setShortfact(sf []float32) {
	for i, x := range sf {
		g.gfc.SvQnt.Shortfact[i] = x
	}
}

func (g *goHandle) setATH(adjustFactor, floor float32, l, s []float32) {
	g.ath.AdjustFactor = adjustFactor
	g.ath.Floor = floor
	for i, x := range l {
		g.ath.L[i] = x
	}
	for i, x := range s {
		g.ath.S[i] = x
	}
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
func (g *goHandle) setGeom(gr, ch, blockType, mixedBlockFlag int) {
	gi := g.gi(gr, ch)
	gi.BlockType = blockType
	gi.MixedBlockFlag = mixedBlockFlag
}
func (g *goHandle) setRatioL(gr, ch int, enL, thmL []float32) {
	r := &g.ratio[gr][ch]
	for i := range enL {
		r.En.L[i] = enL[i]
		r.Thm.L[i] = thmL[i]
	}
}
func (g *goHandle) setRatioS(gr, ch int, enS, thmS []float32, n int) {
	r := &g.ratio[gr][ch]
	for i := 0; i < n; i++ {
		for b := 0; b < 3; b++ {
			r.En.S[i][b] = enS[i*3+b]
			r.Thm.S[i][b] = thmS[i*3+b]
		}
	}
}

func peGrid(pe []float32) *[2][2]float32 {
	var p [2][2]float32
	for gr := 0; gr < 2; gr++ {
		for ch := 0; ch < 2; ch++ {
			p[gr][ch] = pe[gr*2+ch]
		}
	}
	return &p
}
func merVec(mer []float32) *[2]float32 {
	var m [2]float32
	m[0], m[1] = mer[0], mer[1]
	return &m
}

func (g *goHandle) runNew(pe []float32, mer []float32) {
	g.gfc.VBRNewIterationLoopParity(peGrid(pe), merVec(mer), &g.ratio)
}
func (g *goHandle) runOld(pe []float32, mer []float32) {
	g.gfc.VBROldIterationLoopParity(peGrid(pe), merVec(mer), &g.ratio)
}

func (g *goHandle) bitrateIndex() int { return g.gfc.OvEnc.BitrateIndex }
func (g *goHandle) resvSize() int     { return g.gfc.SvEnc.ResvSize }
func (g *goHandle) modeExt() int      { return g.gfc.OvEnc.ModeExt }

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
func (g *goHandle) blockType(gr, ch int) int        { return g.gi(gr, ch).BlockType }
