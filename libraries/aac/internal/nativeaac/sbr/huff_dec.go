// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// SBR Huffman symbol decode, ported 1:1 from the vendored Fraunhofer FDK-AAC
// reference libSBRdec/src/huff_dec.cpp. The codebook tables themselves live in
// rom_dec_data.go (sbr_rom.cpp:911-1038), narrowed to [N][2]int8 exactly as the
// C SCHAR[N][2] arrays. This is a pure integer kernel: bit-exact in any build.

// huffman is the C `Huffman` typedef (huff_dec.h): a pointer to an SCHAR[2]
// codebook. In Go the codebook is passed as a slice of [2]int8 nodes.
type huffman = [][2]int8

// decodeHuffmanCW decodes one Huffman code word: it reads bits from hBs until a
// valid codeword is found. Each table entry is an index to the next entry, or —
// if negative — the codeword (the decoded value is index + 64).
//
// C counterpart: DecodeHuffmanCW (huff_dec.cpp:123-137).
func decodeHuffmanCW(h huffman, hBs *bitStream) int {
	var index int8 = 0
	for index >= 0 {
		bit := hBs.readBits(1)
		index = h[index][bit]
	}
	value := int(index) + 64 // Add offset
	return value
}
