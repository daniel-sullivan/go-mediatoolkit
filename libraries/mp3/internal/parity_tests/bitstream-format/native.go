//go:build cgo

package bitstreamformat

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 port through helpers shaped like the
// cgo oracle wrappers, so parity_test.go can compare the two sides through a
// uniform surface. These import only internal/nativemp3 (never libraries/mp3).

// ── bit reader ───────────────────────────────────────────────────────────

type nativeBitStream struct {
	bs nativemp3.BitStream
}

func newNativeBitStream(data []byte) *nativeBitStream {
	n := &nativeBitStream{}
	nativemp3.BsInit(&n.bs, data, len(data))
	return n
}

func (n *nativeBitStream) getBits(bits int) uint32 { return nativemp3.GetBits(&n.bs, bits) }
func (n *nativeBitStream) pos() int                { return n.bs.Pos }
func (n *nativeBitStream) limit() int              { return n.bs.Limit }

// ── frame sync ───────────────────────────────────────────────────────────

func nativeFindFrame(mp3 []byte, mp3Bytes int, freeFormatBytes *int) (off, frameBytes int) {
	var fb int
	off = nativemp3.Mp3dFindFrame(mp3, mp3Bytes, freeFormatBytes, &fb)
	return off, fb
}

func nativeMatchFrame(hdr []byte, mp3Bytes, frameBytes int) bool {
	return nativemp3.Mp3dMatchFrame(hdr, mp3Bytes, frameBytes)
}

// ── reservoir ────────────────────────────────────────────────────────────

type nativeReservoir struct {
	dec nativemp3.Decoder
	s   nativemp3.Scratch
}

func newNativeReservoir() *nativeReservoir { return &nativeReservoir{} }

func (n *nativeReservoir) setMaindata(data []byte) { copy(n.s.Maindata[:], data) }
func (n *nativeReservoir) setBs(pos, limit int)    { n.s.Bs.Pos = pos; n.s.Bs.Limit = limit }
func (n *nativeReservoir) maindata(c int) []byte   { return append([]byte(nil), n.s.Maindata[:c]...) }
func (n *nativeReservoir) bsPos() int              { return n.s.Bs.Pos }
func (n *nativeReservoir) bsLimit() int            { return n.s.Bs.Limit }
func (n *nativeReservoir) reserv() int             { return n.dec.Reserv }
func (n *nativeReservoir) reservBuf(c int) []byte  { return append([]byte(nil), n.dec.ReservBuf[:c]...) }
func (n *nativeReservoir) saveReservoir()          { nativemp3.L3SaveReservoir(&n.dec, &n.s) }

func (n *nativeReservoir) restoreReservoir(payload []byte, mainDataBegin int) bool {
	var bs nativemp3.BitStream
	nativemp3.BsInit(&bs, payload, len(payload))
	return nativemp3.L3RestoreReservoir(&n.dec, &bs, &n.s, mainDataBegin)
}

// Native header-accessor pass-throughs (named to read like the cgo wrappers).
func nativeHdrIsMono(h []byte) bool         { return nativemp3.HdrIsMono(h) }
func nativeHdrIsFreeFormat(h []byte) bool   { return nativemp3.HdrIsFreeFormat(h) }
func nativeHdrIsCRC(h []byte) bool          { return nativemp3.HdrIsCRC(h) }
func nativeHdrTestPadding(h []byte) int     { return nativemp3.HdrTestPadding(h) }
func nativeHdrTestMPEG1(h []byte) int       { return nativemp3.HdrTestMPEG1(h) }
func nativeHdrTestNotMPEG25(h []byte) int   { return nativemp3.HdrTestNotMPEG25(h) }
func nativeHdrGetLayer(h []byte) int        { return nativemp3.HdrGetLayer(h) }
func nativeHdrGetBitrate(h []byte) int      { return nativemp3.HdrGetBitrate(h) }
func nativeHdrGetSampleRate(h []byte) int   { return nativemp3.HdrGetSampleRate(h) }
func nativeHdrGetMySampleRate(h []byte) int { return nativemp3.HdrGetMySampleRate(h) }
func nativeHdrIsFrame576(h []byte) bool     { return nativemp3.HdrIsFrame576(h) }
func nativeHdrIsLayer1(h []byte) bool       { return nativemp3.HdrIsLayer1(h) }
func nativeHdrValid(h []byte) bool          { return nativemp3.HdrValid(h) }
func nativeHdrCompare(h1, h2 []byte) bool   { return nativemp3.HdrCompare(h1, h2) }
func nativeHdrBitrateKbps(h []byte) uint    { return nativemp3.HdrBitrateKbps(h) }
func nativeHdrSampleRateHz(h []byte) uint   { return nativemp3.HdrSampleRateHz(h) }
func nativeHdrFrameSamples(h []byte) uint   { return nativemp3.HdrFrameSamples(h) }
func nativeHdrPadding(h []byte) int         { return nativemp3.HdrPadding(h) }
func nativeHdrFrameBytes(h []byte, ff int) int {
	return nativemp3.HdrFrameBytes(h, ff)
}
