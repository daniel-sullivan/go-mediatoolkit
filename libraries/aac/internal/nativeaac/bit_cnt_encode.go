// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// Bitstream-encode area: the Huffman code emitters from bit_cnt.cpp.
//
// 1:1 port of the encode-time spectral and scalefactor Huffman writers that
// the bitstream-encode functions (bitenc.go) drive: FDKaacEnc_codeValues (the
// 11 spectrum codebooks plus the ESC codebook) and
// FDKaacEnc_codeScalefactorDelta. The bit-counting twins
// (FDKaacEnc_countValues / FDKaacEnc_bitCount) belong to the dynamic-bits
// area and are not ported here.
//
// Integer-only kernel: the only value touched is a SHORT spectral coefficient.
// Bit-identical regardless of build tag.
//
// Every function names its bit_cnt.cpp C counterpart as file:line.

package nativeaac

// Codebook numbers (bit_cnt.h:115, enum codeBookNo). The subset codeValues
// switches on, plus the special-purpose books bitenc reads.
const (
	codeBookZeroNo         = 0
	codeBook1No            = 1
	codeBook2No            = 2
	codeBook3No            = 3
	codeBook4No            = 4
	codeBook5No            = 5
	codeBook6No            = 6
	codeBook7No            = 7
	codeBook8No            = 8
	codeBook9No            = 9
	codeBook10No           = 10
	codeBookEscNo          = 11
	codeBookPnsNo          = 13
	codeBookIsOutOfPhaseNo = 14
	codeBookIsInPhaseNo    = 15
)

// codeBookScfLav is the scalefactor codebook largest-absolute-value
// (bit_cnt.h, enum codeBookLav, CODE_BOOK_SCF_LAV = 60).
const codeBookScfLav = 60

// hiLtab extracts the high 16 bits of a packed length-table entry
// (bit_cnt.cpp:107, #define HI_LTAB(a) (a >> 16)).
func hiLtab(a uint32) int { return int(a >> 16) }

// loLtab extracts the low 16 bits of a packed length-table entry
// (bit_cnt.cpp:108, #define LO_LTAB(a) (a & 0xffff)).
func loLtab(a uint32) int { return int(a & 0xffff) }

// signBit reproduces the C idiom ((UINT)ti >> 31) on an int promoted from a
// SHORT: it is 1 when ti is negative, else 0.
func signBit(ti int) int {
	if ti < 0 {
		return 1
	}
	return 0
}

// CodeValues Huffman-encodes width spectral coefficients of one section using
// codeBook, writing into hBitstream (bit_cnt.cpp:725, FDKaacEnc_codeValues).
// values is the slice starting at the section's first coefficient.
func CodeValues(values []int16, width, codeBook int, hBitstream *bitStream) int {
	var i, t0, t1, t2, t3, t00, t01 int
	var codeWord, codeLength int
	var sign, signLength int
	p := 0 // running pointer into values (mirrors C `*values++`)

	switch codeBook {
	case codeBookZeroNo:

	case codeBook1No:
		for i = 0; i < width; i += 4 {
			t0 = int(values[i+0]) + 1
			t1 = int(values[i+1]) + 1
			t2 = int(values[i+2]) + 1
			t3 = int(values[i+3]) + 1
			codeWord = int(huffctab1[t0][t1][t2][t3])
			codeLength = hiLtab(huffltab1_2[t0][t1][t2][t3])
			hBitstream.writeBits(uint32(codeWord), uint32(codeLength))
		}

	case codeBook2No:
		for i = 0; i < width; i += 4 {
			t0 = int(values[i+0]) + 1
			t1 = int(values[i+1]) + 1
			t2 = int(values[i+2]) + 1
			t3 = int(values[i+3]) + 1
			codeWord = int(huffctab2[t0][t1][t2][t3])
			codeLength = loLtab(huffltab1_2[t0][t1][t2][t3])
			hBitstream.writeBits(uint32(codeWord), uint32(codeLength))
		}

	case codeBook3No:
		for i = 0; i < (width >> 2); i++ {
			sign = 0
			signLength = 0
			var index [4]int
			for j := 0; j < 4; j++ {
				ti := int(values[p])
				p++
				zero := 0
				if ti != 0 {
					zero = 1
				}
				signLength += zero
				sign = (sign << uint(zero)) + signBit(ti)
				index[j] = fixpAbsInt(ti)
			}
			codeWord = int(huffctab3[index[0]][index[1]][index[2]][index[3]])
			codeLength = hiLtab(huffltab3_4[index[0]][index[1]][index[2]][index[3]])
			hBitstream.writeBits(uint32((codeWord<<uint(signLength))|sign),
				uint32(codeLength+signLength))
		}

	case codeBook4No:
		for i = 0; i < width; i += 4 {
			sign = 0
			signLength = 0
			var index [4]int
			for j := 0; j < 4; j++ {
				ti := int(values[p])
				p++
				zero := 0
				if ti != 0 {
					zero = 1
				}
				signLength += zero
				sign = (sign << uint(zero)) + signBit(ti)
				index[j] = fixpAbsInt(ti)
			}
			codeWord = int(huffctab4[index[0]][index[1]][index[2]][index[3]])
			codeLength = loLtab(huffltab3_4[index[0]][index[1]][index[2]][index[3]])
			hBitstream.writeBits(uint32((codeWord<<uint(signLength))|sign),
				uint32(codeLength+signLength))
		}

	case codeBook5No:
		for i = 0; i < (width >> 2); i++ {
			t0 = int(values[p]) + 4
			p++
			t1 = int(values[p]) + 4
			p++
			t2 = int(values[p]) + 4
			p++
			t3 = int(values[p]) + 4
			p++
			codeWord = int(huffctab5[t0][t1])
			codeLength = hiLtab(huffltab5_6[t2][t3]) // length of 2nd cw
			codeWord = (codeWord << uint(codeLength)) + int(huffctab5[t2][t3])
			codeLength += hiLtab(huffltab5_6[t0][t1])
			hBitstream.writeBits(uint32(codeWord), uint32(codeLength))
		}

	case codeBook6No:
		for i = 0; i < (width >> 2); i++ {
			t0 = int(values[p]) + 4
			p++
			t1 = int(values[p]) + 4
			p++
			t2 = int(values[p]) + 4
			p++
			t3 = int(values[p]) + 4
			p++
			codeWord = int(huffctab6[t0][t1])
			codeLength = loLtab(huffltab5_6[t2][t3]) // length of 2nd cw
			codeWord = (codeWord << uint(codeLength)) + int(huffctab6[t2][t3])
			codeLength += loLtab(huffltab5_6[t0][t1])
			hBitstream.writeBits(uint32(codeWord), uint32(codeLength))
		}

	case codeBook7No:
		for i = 0; i < (width >> 1); i++ {
			t0 = int(values[p])
			p++
			sign = signBit(t0)
			t0 = fixpAbsInt(t0)
			signLength = 0
			if t0 != 0 {
				signLength = 1
			}
			t1 = int(values[p])
			p++
			zero := 0
			if t1 != 0 {
				zero = 1
			}
			signLength += zero
			sign = (sign << uint(zero)) + signBit(t1)
			t1 = fixpAbsInt(t1)
			codeWord = int(huffctab7[t0][t1])
			codeLength = hiLtab(huffltab7_8[t0][t1])
			hBitstream.writeBits(uint32((codeWord<<uint(signLength))|sign),
				uint32(codeLength+signLength))
		}

	case codeBook8No:
		for i = 0; i < (width >> 1); i++ {
			t0 = int(values[p])
			p++
			sign = signBit(t0)
			t0 = fixpAbsInt(t0)
			signLength = 0
			if t0 != 0 {
				signLength = 1
			}
			t1 = int(values[p])
			p++
			zero := 0
			if t1 != 0 {
				zero = 1
			}
			signLength += zero
			sign = (sign << uint(zero)) + signBit(t1)
			t1 = fixpAbsInt(t1)
			codeWord = int(huffctab8[t0][t1])
			codeLength = loLtab(huffltab7_8[t0][t1])
			hBitstream.writeBits(uint32((codeWord<<uint(signLength))|sign),
				uint32(codeLength+signLength))
		}

	case codeBook9No:
		for i = 0; i < (width >> 1); i++ {
			t0 = int(values[p])
			p++
			sign = signBit(t0)
			t0 = fixpAbsInt(t0)
			signLength = 0
			if t0 != 0 {
				signLength = 1
			}
			t1 = int(values[p])
			p++
			zero := 0
			if t1 != 0 {
				zero = 1
			}
			signLength += zero
			sign = (sign << uint(zero)) + signBit(t1)
			t1 = fixpAbsInt(t1)
			codeWord = int(huffctab9[t0][t1])
			codeLength = hiLtab(huffltab9_10[t0][t1])
			hBitstream.writeBits(uint32((codeWord<<uint(signLength))|sign),
				uint32(codeLength+signLength))
		}

	case codeBook10No:
		for i = 0; i < (width >> 1); i++ {
			t0 = int(values[p])
			p++
			sign = signBit(t0)
			t0 = fixpAbsInt(t0)
			signLength = 0
			if t0 != 0 {
				signLength = 1
			}
			t1 = int(values[p])
			p++
			zero := 0
			if t1 != 0 {
				zero = 1
			}
			signLength += zero
			sign = (sign << uint(zero)) + signBit(t1)
			t1 = fixpAbsInt(t1)
			codeWord = int(huffctab10[t0][t1])
			codeLength = loLtab(huffltab9_10[t0][t1])
			hBitstream.writeBits(uint32((codeWord<<uint(signLength))|sign),
				uint32(codeLength+signLength))
		}

	case codeBookEscNo:
		for i = 0; i < (width >> 1); i++ {
			t0 = int(values[p])
			p++
			sign = signBit(t0)
			t0 = fixpAbsInt(t0)
			signLength = 0
			if t0 != 0 {
				signLength = 1
			}
			t1 = int(values[p])
			p++
			zero := 0
			if t1 != 0 {
				zero = 1
			}
			signLength += zero
			sign = (sign << uint(zero)) + signBit(t1)
			t1 = fixpAbsInt(t1)

			t00 = fixMin(t0, 16)
			t01 = fixMin(t1, 16)

			codeWord = int(huffctab11[t00][t01])
			codeLength = int(huffltab11[t00][t01])
			hBitstream.writeBits(uint32((codeWord<<uint(signLength))|sign),
				uint32(codeLength+signLength))
			for j := 0; j < 2; j++ {
				if t0 >= 16 {
					n := 4
					q := t0
					for {
						q >>= 1
						if q < 16 {
							break
						}
						n++
					}
					hBitstream.writeBits(
						uint32((((1<<uint(n-3))-2)<<uint(n))|(t0-(1<<uint(n)))),
						uint32(n+n-3))
				}
				t0 = t1
			}
		}

	default:
	}
	return 0
}

// CodeScalefactorDelta Huffman-encodes a DPCM scalefactor delta into
// hBitstream, returning 1 when the delta is out of range
// (bit_cnt.cpp:941, FDKaacEnc_codeScalefactorDelta).
func CodeScalefactorDelta(delta int, hBitstream *bitStream) int {
	if fixpAbsInt(delta) > codeBookScfLav {
		return 1
	}
	codeWord := int(huffctabscf[delta+codeBookScfLav])
	codeLength := int(huffltabscf[delta+codeBookScfLav])
	hBitstream.writeBits(uint32(codeWord), uint32(codeLength))
	return 0
}
