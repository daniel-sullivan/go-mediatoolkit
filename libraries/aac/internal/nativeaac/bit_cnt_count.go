// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Pure-Go 1:1 port of the Fraunhofer FDK-AAC encoder spectral bit-COUNTING
// kernels — libAACenc/src/bit_cnt.cpp's FDKaacEnc_bitCount / FDKaacEnc_countValues
// and the seven per-codebook count functions (count1_2_3_4_5_6_7_8_9_10_11 ..
// countEsc). These are the inner-loop bit estimator the noiseless section coder
// (dyn_bits.cpp) calls to decide the cheapest Huffman codebook per band and the
// crash-recovery path calls to size individual sections.
//
// Every value is an INT / SHORT — pure integer arithmetic (the packed length
// tables huffltab* are reused from huffman_rom.go; HI_LTAB/LO_LTAB are the
// hiLtab/loLtab helpers in bit_cnt_encode.go). Bit-identical to the C regardless
// of vectorization; no float, no FMA, no ULP tolerance. Carries the aacfdk fence
// so a default build links none of it.

package nativeaac

// invalidBitcount is INVALID_BITCOUNT (bit_cnt.h:110, FDK_INT_MAX/4): the
// sentinel marking a codebook that cannot represent the band.
const invalidBitcount = fdkIntMax / 4

// codeBookEscNdx is CODE_BOOK_ESC_NDX (bit_cnt.h:152) == 11: the highest spectral
// codebook index; the bitLookUp / bitCount arrays span [0, CODE_BOOK_ESC_NDX].
const codeBookEscNdx = 11

// codeBookEscLav is CODE_BOOK_ESC_LAV (bit_cnt.h:176) == 16: the largest absolute
// value the ESC codebook represents inline; it bounds the count-function table.
const codeBookEscLav = 16

// countFuncTable mirrors the static COUNT_FUNCTION countFuncTable[] dispatch
// (bit_cnt.cpp:522): indexed by fixMin(maxVal, CODE_BOOK_ESC_LAV), it selects the
// narrowest count function that still covers maxVal.
var countFuncTable = [codeBookEscLav + 1]func(values []int16, width int, bitCount []int){
	count1_2_3_4_5_6_7_8_9_10_11, // 0
	count1_2_3_4_5_6_7_8_9_10_11, // 1
	count3_4_5_6_7_8_9_10_11,     // 2
	count5_6_7_8_9_10_11,         // 3
	count5_6_7_8_9_10_11,         // 4
	count7_8_9_10_11,             // 5
	count7_8_9_10_11,             // 6
	count7_8_9_10_11,             // 7
	count9_10_11,                 // 8
	count9_10_11,                 // 9
	count9_10_11,                 // 10
	count9_10_11,                 // 11
	count9_10_11,                 // 12
	count11,                      // 13
	count11,                      // 14
	count11,                      // 15
	countEsc,                     // 16
}

// count1_2_3_4_5_6_7_8_9_10_11 is the 1:1 port of
// FDKaacEnc_count1_2_3_4_5_6_7_8_9_10_11 (bit_cnt.cpp:121): count the Huffman
// bits for every codebook 1..11 over width signed coefficients in one pass.
func count1_2_3_4_5_6_7_8_9_10_11(values []int16, width int, bitCount []int) {
	var bc1_2, bc3_4, bc5_6, bc7_8, bc9_10, bc11, sc int

	for i := 0; i < width; i += 4 {
		t0 := int(values[i+0])
		t1 := int(values[i+1])
		t2 := int(values[i+2])
		t3 := int(values[i+3])

		bc1_2 += int(huffltab1_2[t0+1][t1+1][t2+1][t3+1])
		bc5_6 += int(huffltab5_6[t0+4][t1+4]) + int(huffltab5_6[t2+4][t3+4])

		t0 = fixpAbsInt(t0)
		sc += b2i(t0 > 0)
		t1 = fixpAbsInt(t1)
		sc += b2i(t1 > 0)
		t2 = fixpAbsInt(t2)
		sc += b2i(t2 > 0)
		t3 = fixpAbsInt(t3)
		sc += b2i(t3 > 0)

		bc3_4 += int(huffltab3_4[t0][t1][t2][t3])
		bc7_8 += int(huffltab7_8[t0][t1]) + int(huffltab7_8[t2][t3])
		bc9_10 += int(huffltab9_10[t0][t1]) + int(huffltab9_10[t2][t3])
		bc11 += int(huffltab11[t0][t1]) + int(huffltab11[t2][t3])
	}
	bitCount[1] = hiLtab(uint32(bc1_2))
	bitCount[2] = loLtab(uint32(bc1_2))
	bitCount[3] = hiLtab(uint32(bc3_4)) + sc
	bitCount[4] = loLtab(uint32(bc3_4)) + sc
	bitCount[5] = hiLtab(uint32(bc5_6))
	bitCount[6] = loLtab(uint32(bc5_6))
	bitCount[7] = hiLtab(uint32(bc7_8)) + sc
	bitCount[8] = loLtab(uint32(bc7_8)) + sc
	bitCount[9] = hiLtab(uint32(bc9_10)) + sc
	bitCount[10] = loLtab(uint32(bc9_10)) + sc
	bitCount[11] = bc11 + sc
}

// count3_4_5_6_7_8_9_10_11 is the 1:1 port of
// FDKaacEnc_count3_4_5_6_7_8_9_10_11 (bit_cnt.cpp:187): codebooks 1,2 marked
// invalid; count 3..11.
func count3_4_5_6_7_8_9_10_11(values []int16, width int, bitCount []int) {
	var bc3_4, bc5_6, bc7_8, bc9_10, bc11, sc int

	for i := 0; i < width; i += 4 {
		t0 := int(values[i+0])
		t1 := int(values[i+1])
		t2 := int(values[i+2])
		t3 := int(values[i+3])

		bc5_6 += int(huffltab5_6[t0+4][t1+4]) + int(huffltab5_6[t2+4][t3+4])

		t0 = fixpAbsInt(t0)
		sc += b2i(t0 > 0)
		t1 = fixpAbsInt(t1)
		sc += b2i(t1 > 0)
		t2 = fixpAbsInt(t2)
		sc += b2i(t2 > 0)
		t3 = fixpAbsInt(t3)
		sc += b2i(t3 > 0)

		bc3_4 += int(huffltab3_4[t0][t1][t2][t3])
		bc7_8 += int(huffltab7_8[t0][t1]) + int(huffltab7_8[t2][t3])
		bc9_10 += int(huffltab9_10[t0][t1]) + int(huffltab9_10[t2][t3])
		bc11 += int(huffltab11[t0][t1]) + int(huffltab11[t2][t3])
	}

	bitCount[1] = invalidBitcount
	bitCount[2] = invalidBitcount
	bitCount[3] = hiLtab(uint32(bc3_4)) + sc
	bitCount[4] = loLtab(uint32(bc3_4)) + sc
	bitCount[5] = hiLtab(uint32(bc5_6))
	bitCount[6] = loLtab(uint32(bc5_6))
	bitCount[7] = hiLtab(uint32(bc7_8)) + sc
	bitCount[8] = loLtab(uint32(bc7_8)) + sc
	bitCount[9] = hiLtab(uint32(bc9_10)) + sc
	bitCount[10] = loLtab(uint32(bc9_10)) + sc
	bitCount[11] = bc11 + sc
}

// count5_6_7_8_9_10_11 is the 1:1 port of FDKaacEnc_count5_6_7_8_9_10_11
// (bit_cnt.cpp:253): codebooks 1..4 invalid; count 5..11.
func count5_6_7_8_9_10_11(values []int16, width int, bitCount []int) {
	var bc5_6, bc7_8, bc9_10, bc11, sc int

	for i := 0; i < width; i += 4 {
		t0 := int(values[i+0])
		t1 := int(values[i+1])
		t2 := int(values[i+2])
		t3 := int(values[i+3])

		bc5_6 += int(huffltab5_6[t0+4][t1+4]) + int(huffltab5_6[t2+4][t3+4])

		t0 = fixpAbsInt(t0)
		sc += b2i(t0 > 0)
		t1 = fixpAbsInt(t1)
		sc += b2i(t1 > 0)
		t2 = fixpAbsInt(t2)
		sc += b2i(t2 > 0)
		t3 = fixpAbsInt(t3)
		sc += b2i(t3 > 0)

		bc7_8 += int(huffltab7_8[t0][t1]) + int(huffltab7_8[t2][t3])
		bc9_10 += int(huffltab9_10[t0][t1]) + int(huffltab9_10[t2][t3])
		bc11 += int(huffltab11[t0][t1]) + int(huffltab11[t2][t3])
	}
	bitCount[1] = invalidBitcount
	bitCount[2] = invalidBitcount
	bitCount[3] = invalidBitcount
	bitCount[4] = invalidBitcount
	bitCount[5] = hiLtab(uint32(bc5_6))
	bitCount[6] = loLtab(uint32(bc5_6))
	bitCount[7] = hiLtab(uint32(bc7_8)) + sc
	bitCount[8] = loLtab(uint32(bc7_8)) + sc
	bitCount[9] = hiLtab(uint32(bc9_10)) + sc
	bitCount[10] = loLtab(uint32(bc9_10)) + sc
	bitCount[11] = bc11 + sc
}

// count7_8_9_10_11 is the 1:1 port of FDKaacEnc_count7_8_9_10_11
// (bit_cnt.cpp:315): codebooks 1..6 invalid; count 7..11.
func count7_8_9_10_11(values []int16, width int, bitCount []int) {
	var bc7_8, bc9_10, bc11, sc int

	for i := 0; i < width; i += 4 {
		t0 := fixpAbsInt(int(values[i+0]))
		sc += b2i(t0 > 0)
		t1 := fixpAbsInt(int(values[i+1]))
		sc += b2i(t1 > 0)
		t2 := fixpAbsInt(int(values[i+2]))
		sc += b2i(t2 > 0)
		t3 := fixpAbsInt(int(values[i+3]))
		sc += b2i(t3 > 0)

		bc7_8 += int(huffltab7_8[t0][t1]) + int(huffltab7_8[t2][t3])
		bc9_10 += int(huffltab9_10[t0][t1]) + int(huffltab9_10[t2][t3])
		bc11 += int(huffltab11[t0][t1]) + int(huffltab11[t2][t3])
	}

	bitCount[1] = invalidBitcount
	bitCount[2] = invalidBitcount
	bitCount[3] = invalidBitcount
	bitCount[4] = invalidBitcount
	bitCount[5] = invalidBitcount
	bitCount[6] = invalidBitcount
	bitCount[7] = hiLtab(uint32(bc7_8)) + sc
	bitCount[8] = loLtab(uint32(bc7_8)) + sc
	bitCount[9] = hiLtab(uint32(bc9_10)) + sc
	bitCount[10] = loLtab(uint32(bc9_10)) + sc
	bitCount[11] = bc11 + sc
}

// count9_10_11 is the 1:1 port of FDKaacEnc_count9_10_11 (bit_cnt.cpp:375):
// codebooks 1..8 invalid; count 9..11.
func count9_10_11(values []int16, width int, bitCount []int) {
	var bc9_10, bc11, sc int

	for i := 0; i < width; i += 4 {
		t0 := fixpAbsInt(int(values[i+0]))
		sc += b2i(t0 > 0)
		t1 := fixpAbsInt(int(values[i+1]))
		sc += b2i(t1 > 0)
		t2 := fixpAbsInt(int(values[i+2]))
		sc += b2i(t2 > 0)
		t3 := fixpAbsInt(int(values[i+3]))
		sc += b2i(t3 > 0)

		bc9_10 += int(huffltab9_10[t0][t1]) + int(huffltab9_10[t2][t3])
		bc11 += int(huffltab11[t0][t1]) + int(huffltab11[t2][t3])
	}

	bitCount[1] = invalidBitcount
	bitCount[2] = invalidBitcount
	bitCount[3] = invalidBitcount
	bitCount[4] = invalidBitcount
	bitCount[5] = invalidBitcount
	bitCount[6] = invalidBitcount
	bitCount[7] = invalidBitcount
	bitCount[8] = invalidBitcount
	bitCount[9] = hiLtab(uint32(bc9_10)) + sc
	bitCount[10] = loLtab(uint32(bc9_10)) + sc
	bitCount[11] = bc11 + sc
}

// count11 is the 1:1 port of FDKaacEnc_count11 (bit_cnt.cpp:431): codebooks
// 1..10 invalid; count only table 11 (no escapes).
func count11(values []int16, width int, bitCount []int) {
	var bc11, sc int

	for i := 0; i < width; i += 4 {
		t0 := fixpAbsInt(int(values[i+0]))
		sc += b2i(t0 > 0)
		t1 := fixpAbsInt(int(values[i+1]))
		sc += b2i(t1 > 0)
		t2 := fixpAbsInt(int(values[i+2]))
		sc += b2i(t2 > 0)
		t3 := fixpAbsInt(int(values[i+3]))
		sc += b2i(t3 > 0)

		bc11 += int(huffltab11[t0][t1]) + int(huffltab11[t2][t3])
	}

	bitCount[1] = invalidBitcount
	bitCount[2] = invalidBitcount
	bitCount[3] = invalidBitcount
	bitCount[4] = invalidBitcount
	bitCount[5] = invalidBitcount
	bitCount[6] = invalidBitcount
	bitCount[7] = invalidBitcount
	bitCount[8] = invalidBitcount
	bitCount[9] = invalidBitcount
	bitCount[10] = invalidBitcount
	bitCount[11] = bc11 + sc
}

// countEsc is the 1:1 port of FDKaacEnc_countEsc (bit_cnt.cpp:484): codebooks
// 0..10 invalid; count table 11 with escape coding (the only codebook able to
// represent absolute values >= 16).
func countEsc(values []int16, width int, bitCount []int) {
	var bc11, ec, sc int

	for i := 0; i < width; i += 2 {
		t0 := fixpAbsInt(int(values[i+0]))
		t1 := fixpAbsInt(int(values[i+1]))

		sc += b2i(t0 > 0) + b2i(t1 > 0)

		t00 := fixMin(t0, 16)
		t01 := fixMin(t1, 16)
		bc11 += int(huffltab11[t00][t01])

		if t0 >= 16 {
			ec += 5
			for {
				t0 >>= 1
				if t0 < 16 {
					break
				}
				ec += 2
			}
		}

		if t1 >= 16 {
			ec += 5
			for {
				t1 >>= 1
				if t1 < 16 {
					break
				}
				ec += 2
			}
		}
	}

	for i := 0; i < 11; i++ {
		bitCount[i] = invalidBitcount
	}

	bitCount[11] = bc11 + sc + ec
}

// bitCount is the 1:1 port of FDKaacEnc_bitCount (bit_cnt.cpp:543): fill
// bitCount[0..CODE_BOOK_ESC_NDX] with the Huffman bit cost of coding width
// coefficients (whose maximum absolute value is maxVal) under each codebook.
// bitCount[0] (CODE_BOOK_ZERO) is 0 only for an all-zero band, else invalid.
// The dispatch to the narrowest count function is via countFuncTable; entries
// the chosen function does not write retain their caller-supplied / sentinel
// value exactly as in C (C never zeroes the array first either).
func bitCount(values []int16, width, maxVal int, bitCnt []int) {
	if maxVal == 0 {
		bitCnt[0] = 0
	} else {
		bitCnt[0] = invalidBitcount
	}

	countFuncTable[fixMin(maxVal, codeBookEscLav)](values, width, bitCnt)
}

// countValues is the 1:1 port of FDKaacEnc_countValues (bit_cnt.cpp:560): return
// the number of Huffman bits needed to code width spectral coefficients with a
// specific codeBook (the optimal book selected by the section coder). Unlike
// bitCount it knows the book, so it walks one packed length table directly,
// adding sign bits for the unsigned codebooks and escape bits for codebook 11.
func countValues(values []int16, width, codeBook int) int {
	var t0, t1, t2, t3 int
	bitCnt := 0

	switch codeBook {
	case codeBookZeroNo:

	case codeBook1No:
		for i := 0; i < width; i += 4 {
			t0 = int(values[i+0])
			t1 = int(values[i+1])
			t2 = int(values[i+2])
			t3 = int(values[i+3])
			bitCnt += hiLtab(huffltab1_2[t0+1][t1+1][t2+1][t3+1])
		}

	case codeBook2No:
		for i := 0; i < width; i += 4 {
			t0 = int(values[i+0])
			t1 = int(values[i+1])
			t2 = int(values[i+2])
			t3 = int(values[i+3])
			bitCnt += loLtab(huffltab1_2[t0+1][t1+1][t2+1][t3+1])
		}

	case codeBook3No:
		for i := 0; i < width; i += 4 {
			t0 = fixpAbsInt(int(values[i+0]))
			bitCnt += b2i(t0 > 0)
			t1 = fixpAbsInt(int(values[i+1]))
			bitCnt += b2i(t1 > 0)
			t2 = fixpAbsInt(int(values[i+2]))
			bitCnt += b2i(t2 > 0)
			t3 = fixpAbsInt(int(values[i+3]))
			bitCnt += b2i(t3 > 0)
			bitCnt += hiLtab(huffltab3_4[t0][t1][t2][t3])
		}

	case codeBook4No:
		for i := 0; i < width; i += 4 {
			t0 = fixpAbsInt(int(values[i+0]))
			bitCnt += b2i(t0 > 0)
			t1 = fixpAbsInt(int(values[i+1]))
			bitCnt += b2i(t1 > 0)
			t2 = fixpAbsInt(int(values[i+2]))
			bitCnt += b2i(t2 > 0)
			t3 = fixpAbsInt(int(values[i+3]))
			bitCnt += b2i(t3 > 0)
			bitCnt += loLtab(huffltab3_4[t0][t1][t2][t3])
		}

	case codeBook5No:
		for i := 0; i < width; i += 4 {
			t0 = int(values[i+0])
			t1 = int(values[i+1])
			t2 = int(values[i+2])
			t3 = int(values[i+3])
			bitCnt += hiLtab(huffltab5_6[t0+4][t1+4]) + hiLtab(huffltab5_6[t2+4][t3+4])
		}

	case codeBook6No:
		for i := 0; i < width; i += 4 {
			t0 = int(values[i+0])
			t1 = int(values[i+1])
			t2 = int(values[i+2])
			t3 = int(values[i+3])
			bitCnt += loLtab(huffltab5_6[t0+4][t1+4]) + loLtab(huffltab5_6[t2+4][t3+4])
		}

	case codeBook7No:
		for i := 0; i < width; i += 4 {
			t0 = fixpAbsInt(int(values[i+0]))
			bitCnt += b2i(t0 > 0)
			t1 = fixpAbsInt(int(values[i+1]))
			bitCnt += b2i(t1 > 0)
			t2 = fixpAbsInt(int(values[i+2]))
			bitCnt += b2i(t2 > 0)
			t3 = fixpAbsInt(int(values[i+3]))
			bitCnt += b2i(t3 > 0)
			bitCnt += hiLtab(huffltab7_8[t0][t1]) + hiLtab(huffltab7_8[t2][t3])
		}

	case codeBook8No:
		for i := 0; i < width; i += 4 {
			t0 = fixpAbsInt(int(values[i+0]))
			bitCnt += b2i(t0 > 0)
			t1 = fixpAbsInt(int(values[i+1]))
			bitCnt += b2i(t1 > 0)
			t2 = fixpAbsInt(int(values[i+2]))
			bitCnt += b2i(t2 > 0)
			t3 = fixpAbsInt(int(values[i+3]))
			bitCnt += b2i(t3 > 0)
			bitCnt += loLtab(huffltab7_8[t0][t1]) + loLtab(huffltab7_8[t2][t3])
		}

	case codeBook9No:
		for i := 0; i < width; i += 4 {
			t0 = fixpAbsInt(int(values[i+0]))
			bitCnt += b2i(t0 > 0)
			t1 = fixpAbsInt(int(values[i+1]))
			bitCnt += b2i(t1 > 0)
			t2 = fixpAbsInt(int(values[i+2]))
			bitCnt += b2i(t2 > 0)
			t3 = fixpAbsInt(int(values[i+3]))
			bitCnt += b2i(t3 > 0)
			bitCnt += hiLtab(huffltab9_10[t0][t1]) + hiLtab(huffltab9_10[t2][t3])
		}

	case codeBook10No:
		for i := 0; i < width; i += 4 {
			t0 = fixpAbsInt(int(values[i+0]))
			bitCnt += b2i(t0 > 0)
			t1 = fixpAbsInt(int(values[i+1]))
			bitCnt += b2i(t1 > 0)
			t2 = fixpAbsInt(int(values[i+2]))
			bitCnt += b2i(t2 > 0)
			t3 = fixpAbsInt(int(values[i+3]))
			bitCnt += b2i(t3 > 0)
			bitCnt += loLtab(huffltab9_10[t0][t1]) + loLtab(huffltab9_10[t2][t3])
		}

	case codeBookEscNo:
		for i := 0; i < width; i += 2 {
			t0 = fixpAbsInt(int(values[i+0]))
			bitCnt += b2i(t0 > 0)
			t1 = fixpAbsInt(int(values[i+1]))
			bitCnt += b2i(t1 > 0)
			bitCnt += int(huffltab11[fixMin(t0, 16)][fixMin(t1, 16)])
			if t0 >= 16 {
				bitCnt += 5
				for {
					t0 >>= 1
					if t0 < 16 {
						break
					}
					bitCnt += 2
				}
			}
			if t1 >= 16 {
				bitCnt += 5
				for {
					t1 >>= 1
					if t1 < 16 {
						break
					}
					bitCnt += 2
				}
			}
		}

	default:
	}

	return bitCnt
}

// b2i mirrors the C idiom of using a boolean comparison as an INT 0/1 in
// arithmetic (e.g. `sc += (t0 > 0)`): 1 when cond holds, else 0.
func b2i(cond bool) int {
	if cond {
		return 1
	}
	return 0
}
