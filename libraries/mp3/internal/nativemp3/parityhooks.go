package nativemp3

// Exported test hooks for the main-bits parity oracle.
//
// The header-field accessors and the frame-sync helpers are unexported
// (they are 1:1 translations of minimp3's `static` HDR_* macros and
// hdr_* / mp3d_* functions and have no place in the public surface). The
// cgo parity package internal/parity_tests/main-bits lives in its own
// package — it cannot compile the minimp3 oracle and also sit inside
// nativemp3 — so it reaches these helpers through the thin pass-through
// wrappers below. Each wrapper is a verbatim call to the unexported
// function it shadows; they exist solely so the parity suite can assert
// the Go port matches the vendored C bit-for-bit.

// HdrValid exposes hdrValid for the parity oracle.
func HdrValid(h []byte) bool { return hdrValid(h) }

// HdrCompare exposes hdrCompare for the parity oracle.
func HdrCompare(h1, h2 []byte) bool { return hdrCompare(h1, h2) }

// HdrBitrateKbps exposes hdrBitrateKbps for the parity oracle.
func HdrBitrateKbps(h []byte) uint { return hdrBitrateKbps(h) }

// HdrSampleRateHz exposes hdrSampleRateHz for the parity oracle.
func HdrSampleRateHz(h []byte) uint { return hdrSampleRateHz(h) }

// HdrFrameSamples exposes hdrFrameSamples for the parity oracle.
func HdrFrameSamples(h []byte) uint { return hdrFrameSamples(h) }

// HdrFrameBytes exposes hdrFrameBytes for the parity oracle.
func HdrFrameBytes(h []byte, freeFormatSize int) int { return hdrFrameBytes(h, freeFormatSize) }

// HdrPadding exposes hdrPadding for the parity oracle.
func HdrPadding(h []byte) int { return hdrPadding(h) }

// Mp3dMatchFrame exposes mp3dMatchFrame for the parity oracle.
func Mp3dMatchFrame(hdr []byte, mp3Bytes, frameBytes int) bool {
	return mp3dMatchFrame(hdr, mp3Bytes, frameBytes)
}

// Mp3dFindFrame exposes mp3dFindFrame for the parity oracle.
func Mp3dFindFrame(mp3 []byte, mp3Bytes int, freeFormatBytes, ptrFrameBytes *int) int {
	return mp3dFindFrame(mp3, mp3Bytes, freeFormatBytes, ptrFrameBytes)
}
