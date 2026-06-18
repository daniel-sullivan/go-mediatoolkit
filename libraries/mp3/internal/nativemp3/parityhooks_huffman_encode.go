// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hook for the huffman-encode parity oracle.
//
// The bit writer's public emitters (PutBits2, PutBitsNoHeaders) are already
// exported, and the header ring buffer (EncStateVar / HeaderInfo) and config
// (EncSessionConfig.SideinfoLen) are exported fields, so the
// internal/parity_tests/huffman-encode slice can drive and read back the bit
// writer directly. The only piece it cannot reach is putheaderBits, the
// header-splice helper PutBits2 invokes internally (a 1:1 translation of
// LAME's `static` putheader_bits). The verbatim pass-through below surfaces it
// so the parity suite can also assert that isolated splice op matches the
// vendored C bit-for-bit, without widening the public API. It adds no
// behaviour beyond calling the unexported method it shadows.

// PutHeaderBits exposes putheaderBits for the parity oracle.
func (gfc *LameInternalFlags) PutHeaderBits() { gfc.putheaderBits() }
