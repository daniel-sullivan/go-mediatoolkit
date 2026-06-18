// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrquantizesfalloc

import "go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 VBR allocation port the same way
// oracle.c drives the vendored C: a goHandle carries one GrInfo inside a
// LameInternalFlags (mirroring the oracle's calloc'd gfc with a gr_info at
// l3_side.tt[0][0]) and each wrapper forwards the identical flat inputs into the
// nativemp3 parity hook (parityhooks_vbrquantize_sfalloc.go). Keeping the Go
// drivers beside the cgo bridge mirrors the C oracle structure so the two sides
// of each assertion are visibly symmetric. These import only internal/nativemp3
// (never libraries/mp3).

func goFillTables() { nativemp3.FillVbrQuantizeTables() }

// goHandle mirrors cgoHandle: a single GrInfo in a LameInternalFlags so a test
// can populate inputs, run a kernel and read the resolved side info from the
// same Go-side state.
type goHandle struct {
	gfc *nativemp3.LameInternalFlags
	gi  *nativemp3.GrInfo
}

func goNewHandle() *goHandle {
	return &goHandle{
		gfc: new(nativemp3.LameInternalFlags),
		gi:  new(nativemp3.GrInfo),
	}
}

func (g *goHandle) setCfg(modeGr, noiseShaping int) {
	g.gfc.Cfg.ModeGr = modeGr
	g.gfc.Cfg.NoiseShaping = noiseShaping
}

func (g *goHandle) setXr(xr []float32) {
	for i, v := range xr {
		g.gi.Xr[i] = v
	}
}
func (g *goHandle) setWidth(w []int) {
	for i, v := range w {
		g.gi.Width[i] = v
	}
}
func (g *goHandle) setWindow(win []int) {
	for i, v := range win {
		g.gi.Window[i] = v
	}
}
func (g *goHandle) setEac(eac []byte) {
	for i, v := range eac {
		g.gi.EnergyAboveCutoff[i] = v
	}
}
func (g *goHandle) setScalefac(sf []int) {
	for i, v := range sf {
		g.gi.Scalefac[i] = v
	}
}
func (g *goHandle) setSubblockGain(sbg []int) {
	for i, v := range sbg {
		g.gi.SubblockGain[i] = v
	}
}
func (g *goHandle) setGeom(blockType, globalGain, scalefacScale, preflag, sfbmax, psymax, maxNonzeroCoeff int) {
	g.gi.BlockType = blockType
	g.gi.GlobalGain = globalGain
	g.gi.ScalefacScale = scalefacScale
	g.gi.Preflag = preflag
	g.gi.Sfbmax = sfbmax
	g.gi.Psymax = psymax
	g.gi.MaxNonzeroCoeff = maxNonzeroCoeff
}

func (g *goHandle) globalGain() int        { return g.gi.GlobalGain }
func (g *goHandle) scalefacScale() int     { return g.gi.ScalefacScale }
func (g *goHandle) preflag() int           { return g.gi.Preflag }
func (g *goHandle) scalefac(sfb int) int   { return g.gi.Scalefac[sfb] }
func (g *goHandle) subblockGain(i int) int { return g.gi.SubblockGain[i] }
func (g *goHandle) l3enc(i int) int        { return g.gi.L3Enc[i] }

func (g *goHandle) blockSf(xr34orig, l3Xmin []float32, findSel int, vbrsf, vbrsfmin []int) (int, int, [3]int) {
	return g.gfc.BlockSf(g.gi, xr34orig, l3Xmin, findSel, vbrsf, vbrsfmin)
}

func (g *goHandle) quantizeX34(xr34orig []float32) {
	g.gfc.QuantizeX34(g.gi, xr34orig)
}

func (g *goHandle) setSubblockGainK(mingainS [3]int, sf []int) {
	g.gfc.SetSubblockGain(g.gi, mingainS, sf)
}

func (g *goHandle) setScalefacsK(vbrsfmin, sf []int, maxRangeSel int) {
	g.gfc.SetScalefacs(g.gi, vbrsfmin, sf, maxRangeSel)
}

func (g *goHandle) checkScalefactor(vbrsfmin []int) bool {
	return g.gfc.CheckScalefactor(g.gi, vbrsfmin)
}

func (g *goHandle) shortConstrain(vbrsf, vbrsfmin []int, vbrmax, mingainL int, mingainS [3]int) {
	g.gfc.ShortBlockConstrain(g.gi, vbrsf, vbrsfmin, vbrmax, mingainL, mingainS)
}

func (g *goHandle) longConstrain(vbrsf, vbrsfmin []int, vbrmax, mingainL int, mingainS [3]int) {
	g.gfc.LongBlockConstrain(g.gi, vbrsf, vbrsfmin, vbrmax, mingainL, mingainS)
}
