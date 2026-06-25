//go:build cgo

package bitallocation

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 port the same way oracle.c drives the
// vendored C: parse a raw side-info buffer with nativemp3.L3ReadSideInfo to
// build the granule structs, then expand one granule's scalefactors with
// nativemp3.L3DecodeScalefactors. Keeping the Go driver beside the cgo bridge
// (rather than inline in the test) mirrors the C oracle structure one-to-one so
// the two sides of each assertion are visibly symmetric.

// goGranules holds the granules nativemp3.L3ReadSideInfo parsed from a
// side-info buffer.
type goGranules struct {
	gr [4]nativemp3.L3GrInfo
}

// goReadSideInfo runs the Go port's side-info parser over side and reports
// whether it accepted the buffer (the C "-1" error path returns ok=false).
func goReadSideInfo(side []byte, hdr []byte) (*goGranules, bool) {
	g := new(goGranules)
	var bs nativemp3.BitStream
	nativemp3.BsInit(&bs, side, len(side))
	_, ok := nativemp3.L3ReadSideInfo(&bs, g.gr[:], hdr)
	return g, ok
}

// scfsi returns the Go port's parsed scfsi field for granule gi.
func (g *goGranules) scfsi(gi int) uint8 { return g.gr[gi].Scfsi }

// goDecodeScalefactors runs the Go port's L3DecodeScalefactors for granule
// gi / channel ch over main, seeding the ist_pos scratch with seed (39 bytes),
// and returns the resulting 40 float gains, 39 ist_pos bytes, and final bit
// position — the same observables the C oracle returns.
func goDecodeScalefactors(g *goGranules, gi int, hdr, main []byte, ch int, seed []uint8) (scf []float32, istPos []uint8, bsPos int) {
	var bs nativemp3.BitStream
	nativemp3.BsInit(&bs, main, len(main))

	ist := make([]uint8, 39)
	copy(ist, seed)
	out := make([]float32, 40)

	nativemp3.L3DecodeScalefactors(hdr, ist, &bs, &g.gr[gi], out, ch)

	return out, ist, bs.Pos
}
