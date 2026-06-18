package nativemp3

// Exported test hooks for the imdct-synthesis-filterbank parity oracle.
//
// The IMDCT and polyphase-synthesis functions (L3_imdct_gr, L3_change_sign,
// mp3d_DCT_II, mp3d_synth_granule and their helpers) are 1:1 translations of
// minimp3's `static` routines and have no place in the public surface, so they
// stay unexported. The cgo parity package
// internal/parity_tests/imdct-synthesis-filterbank cannot live inside
// nativemp3 (it compiles the minimp3 oracle, which would clash on minimp3's
// static symbols), so it reaches the port through the thin pass-throughs
// below. Each wrapper is a verbatim call to the unexported function it
// shadows; they exist solely so the parity suite can assert the Go port
// matches the vendored minimp3 bit-for-bit.

// L3IMDCTGr exposes l3IMDCTGr for the parity oracle.
func L3IMDCTGr(grbuf, overlap []float32, blockType uint8, nLongBands uint) {
	l3IMDCTGr(grbuf, overlap, blockType, nLongBands)
}

// L3ChangeSign exposes l3ChangeSign for the parity oracle.
func L3ChangeSign(grbuf []float32) { l3ChangeSign(grbuf) }

// Mp3dDCTII exposes mp3dDCTII for the parity oracle.
func Mp3dDCTII(grbuf []float32, n int) { mp3dDCTII(grbuf, n) }

// Mp3dSynthGranule exposes mp3dSynthGranule for the parity oracle.
func Mp3dSynthGranule(qmfState, grbuf []float32, nbands, nch int, pcm []int16, lins []float32) {
	mp3dSynthGranule(qmfState, grbuf, nbands, nch, pcm, lins)
}
