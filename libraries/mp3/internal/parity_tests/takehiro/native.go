// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package takehiro

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 takehiro routines through helpers
// shaped like the cgo oracle wrappers (cgo.go), so parity_test.go can compare
// the two sides through a uniform surface. These import only
// internal/nativemp3 (never libraries/mp3).

// nativeTk holds a nativemp3.LameInternalFlags the takehiro routines thread
// through.
type nativeTk struct {
	gfc nativemp3.LameInternalFlags
}

func newNativeTk() *nativeTk { return &nativeTk{} }

func (n *nativeTk) setCfg(modeGr, useBestHuffman int) {
	n.gfc.Cfg.ModeGr = modeGr
	n.gfc.Cfg.UseBestHuffman = useBestHuffman
}

func (n *nativeTk) setSfbLong(l []int) {
	for i, x := range l {
		n.gfc.ScalefacBand.L[i] = x
	}
}

func (n *nativeTk) setSfbShort(s []int) {
	for i, x := range s {
		n.gfc.ScalefacBand.S[i] = x
	}
}

func (n *nativeTk) gi(gr, ch int) *nativemp3.GrInfo { return &n.gfc.L3Side.Tt[gr][ch] }

func (n *nativeTk) setL3Enc(gr, ch int, ix []int) {
	gi := n.gi(gr, ch)
	for i, x := range ix {
		gi.L3Enc[i] = x
	}
}

func (n *nativeTk) setScalefac(gr, ch int, sf []int) {
	gi := n.gi(gr, ch)
	for i, x := range sf {
		gi.Scalefac[i] = x
	}
}

func (n *nativeTk) setWidth(gr, ch int, w []int) {
	gi := n.gi(gr, ch)
	for i, x := range w {
		gi.Width[i] = x
	}
}

func (n *nativeTk) setWindow(gr, ch int, win []int) {
	gi := n.gi(gr, ch)
	for i, x := range win {
		gi.Window[i] = x
	}
}

func (n *nativeTk) setGeom(gr, ch, blockType, mixedBlockFlag, globalGain,
	scalefacScale, preflag, sfbmax, sfbdivide, maxNonzeroCoeff, part23Length int) {
	gi := n.gi(gr, ch)
	gi.BlockType = blockType
	gi.MixedBlockFlag = mixedBlockFlag
	gi.GlobalGain = globalGain
	gi.ScalefacScale = scalefacScale
	gi.Preflag = preflag
	gi.Sfbmax = sfbmax
	gi.Sfbdivide = sfbdivide
	gi.MaxNonzeroCoeff = maxNonzeroCoeff
	gi.Part23Length = part23Length
}

func (n *nativeTk) huffmanInit() { n.gfc.HuffmanInit() }

func (n *nativeTk) chooseTable(gr, ch, begin, end int, bits *int) int {
	return n.gfc.ChooseTable(n.gi(gr, ch), begin, end, bits)
}

func (n *nativeTk) noquantCountBits(gr, ch int) int {
	return n.gfc.NoquantCountBits(n.gi(gr, ch), nil)
}

func (n *nativeTk) scaleBitcount(gr, ch int) int {
	return n.gfc.ScaleBitcount(n.gi(gr, ch))
}

func (n *nativeTk) bestHuffmanDivide(gr, ch int) {
	n.gfc.BestHuffmanDivide(n.gi(gr, ch))
}

func (n *nativeTk) bestScalefacStore(gr, ch int) {
	n.gfc.BestScalefacStore(gr, ch, &n.gfc.L3Side)
}

func (n *nativeTk) bvScf(i int) int                  { return int(n.gfc.SvQnt.BvScf[i]) }
func (n *nativeTk) bigValues(gr, ch int) int         { return n.gi(gr, ch).BigValues }
func (n *nativeTk) count1(gr, ch int) int            { return n.gi(gr, ch).Count1 }
func (n *nativeTk) count1bits(gr, ch int) int        { return n.gi(gr, ch).Count1bits }
func (n *nativeTk) count1tableSelect(gr, ch int) int { return n.gi(gr, ch).Count1tableSelect }
func (n *nativeTk) region0Count(gr, ch int) int      { return n.gi(gr, ch).Region0Count }
func (n *nativeTk) region1Count(gr, ch int) int      { return n.gi(gr, ch).Region1Count }
func (n *nativeTk) tableSelect(gr, ch, i int) int    { return n.gi(gr, ch).TableSelect[i] }
func (n *nativeTk) part23Length(gr, ch int) int      { return n.gi(gr, ch).Part23Length }
func (n *nativeTk) part2Length(gr, ch int) int       { return n.gi(gr, ch).Part2Length }
func (n *nativeTk) scalefacCompress(gr, ch int) int  { return n.gi(gr, ch).ScalefacCompress }
func (n *nativeTk) scalefacScale(gr, ch int) int     { return n.gi(gr, ch).ScalefacScale }
func (n *nativeTk) preflag(gr, ch int) int           { return n.gi(gr, ch).Preflag }
func (n *nativeTk) scalefac(gr, ch, sfb int) int     { return n.gi(gr, ch).Scalefac[sfb] }
func (n *nativeTk) slen(gr, ch, i int) int           { return n.gi(gr, ch).Slen[i] }
func (n *nativeTk) scfsi(ch, i int) int              { return n.gfc.L3Side.Scfsi[ch][i] }
