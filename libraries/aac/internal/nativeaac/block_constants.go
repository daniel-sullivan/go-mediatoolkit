// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file ports the constants and the Huffman codebook descriptor table
// that the plain spectral Huffman decoder consults, 1:1 from the vendored
// Fraunhofer FDK-AAC reference. These are integer-domain definitions: they
// are bit-identical regardless of build tag.

// Huffman decode-tree shape constants.
//
// C counterpart: libAACdec/src/aac_rom.h:157
//
//	enum { HuffmanBits = 2, HuffmanEntries = (1 << HuffmanBits) };
const (
	huffmanBits    = 2
	huffmanEntries = 1 << huffmanBits
)

// Quantized-value and codebook-index constants.
//
// C counterparts: libAACdec/src/channelinfo.h:148 (MAX_QUANTIZED_VALUE) and
// channelinfo.h:183 (the ZERO_HCB .. INTENSITY_HCB enum).
const (
	maxQuantizedValue = 8191 // channelinfo.h:148

	zeroHCB       = 0  // channelinfo.h:183
	escBook       = 11 // channelinfo.h:184
	noiseHCB      = 13 // channelinfo.h:187
	intensityHCB2 = 14 // channelinfo.h:188
	intensityHCB  = 15 // channelinfo.h:189
)

// codeBookDescription ports the C struct CodeBookDescription
// (libAACdec/src/aac_rom.h:159): the decode tree plus the unpacking
// parameters for one spectral Huffman codebook. dimension is the number of
// quantized coefficients packed per codeword, numBits the field width of
// each coefficient inside the decoded index, and offset the bias subtracted
// from each unpacked coefficient.
type codeBookDescription struct {
	codeBook  [][huffmanEntries]uint16
	dimension int
	numBits   int
	offset    int
}

// aacCodeBookDescriptionTable ports AACcodeBookDescriptionTable[13]
// (libAACdec/src/aac_rom.cpp:905): index 0 is the unused ZERO_HCB slot, 1..11
// are the spectral codebooks, 12 is the scalefactor book (BOOKSCL). The
// nil-codeBook entry at index 0 mirrors the C {NULL, 0, 0, 0}.
var aacCodeBookDescriptionTable = [13]codeBookDescription{
	{nil, 0, 0, 0},
	{huffmanCodeBook1[:], 4, 2, 1},
	{huffmanCodeBook2[:], 4, 2, 1},
	{huffmanCodeBook3[:], 4, 2, 0},
	{huffmanCodeBook4[:], 4, 2, 0},
	{huffmanCodeBook5[:], 2, 4, 4},
	{huffmanCodeBook6[:], 2, 4, 4},
	{huffmanCodeBook7[:], 2, 4, 0},
	{huffmanCodeBook8[:], 2, 4, 0},
	{huffmanCodeBook9[:], 2, 4, 0},
	{huffmanCodeBook10[:], 2, 4, 0},
	{huffmanCodeBook11[:], 2, 5, 0},
	{huffmanCodeBookSCL[:], 1, 8, 60},
}
