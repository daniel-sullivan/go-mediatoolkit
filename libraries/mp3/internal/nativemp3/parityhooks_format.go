package nativemp3

// Additional exported test hooks for the bitstream-format parity oracle.
//
// parityhooks.go already exposes the frame-sync helpers and the higher-level
// header accessors (HdrValid, HdrCompare, HdrFrameBytes, …). The
// internal/parity_tests/bitstream-format slice also pins the lower-level
// HDR_* field accessors and the bit reader directly against the vendored C,
// so the verbatim pass-throughs they need are gathered here. Each is a plain
// call to the unexported function it shadows; they add no behaviour and exist
// solely so the parity suite can assert the Go port matches minimp3
// bit-for-bit.

// HdrIsMono exposes hdrIsMono for the parity oracle.
func HdrIsMono(h []byte) bool { return hdrIsMono(h) }

// HdrIsFreeFormat exposes hdrIsFreeFormat for the parity oracle.
func HdrIsFreeFormat(h []byte) bool { return hdrIsFreeFormat(h) }

// HdrIsCRC exposes hdrIsCRC for the parity oracle.
func HdrIsCRC(h []byte) bool { return hdrIsCRC(h) }

// HdrTestPadding exposes hdrTestPadding for the parity oracle.
func HdrTestPadding(h []byte) int { return hdrTestPadding(h) }

// HdrTestMPEG1 exposes hdrTestMPEG1 for the parity oracle.
func HdrTestMPEG1(h []byte) int { return hdrTestMPEG1(h) }

// HdrTestNotMPEG25 exposes hdrTestNotMPEG25 for the parity oracle.
func HdrTestNotMPEG25(h []byte) int { return hdrTestNotMPEG25(h) }

// HdrGetLayer exposes hdrGetLayer for the parity oracle.
func HdrGetLayer(h []byte) int { return hdrGetLayer(h) }

// HdrGetBitrate exposes hdrGetBitrate for the parity oracle.
func HdrGetBitrate(h []byte) int { return hdrGetBitrate(h) }

// HdrGetSampleRate exposes hdrGetSampleRate for the parity oracle.
func HdrGetSampleRate(h []byte) int { return hdrGetSampleRate(h) }

// HdrGetMySampleRate exposes hdrGetMySampleRate for the parity oracle.
func HdrGetMySampleRate(h []byte) int { return hdrGetMySampleRate(h) }

// HdrIsFrame576 exposes hdrIsFrame576 for the parity oracle.
func HdrIsFrame576(h []byte) bool { return hdrIsFrame576(h) }

// HdrIsLayer1 exposes hdrIsLayer1 for the parity oracle.
func HdrIsLayer1(h []byte) bool { return hdrIsLayer1(h) }
