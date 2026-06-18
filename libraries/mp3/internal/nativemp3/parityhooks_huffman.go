package nativemp3

// Additional exported test hooks for the huffman-decode parity oracle.
//
// L3Huffman, L3Pow43 and the L3GrInfo struct are already exported (the public
// pure-Go decode path uses them), so the internal/parity_tests/huffman-decode
// slice reaches the Huffman unpacking directly. The only piece it cannot see
// is the unexported gPow43 dequantization table, which it pins entry-for-entry
// against the vendored minimp3 g_pow43. The two pass-throughs below surface it
// without widening the public API: they exist solely so the parity suite can
// assert the Go transcription matches minimp3 bit-for-bit.

// GPow43Len exposes the length of the gPow43 dequantization table for the
// parity oracle.
func GPow43Len() int { return len(gPow43) }

// GPow43At exposes gPow43[i] for the parity oracle.
func GPow43At(i int) float32 { return gPow43[i] }
