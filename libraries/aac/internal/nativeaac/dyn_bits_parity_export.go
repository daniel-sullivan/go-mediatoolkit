// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the noiseless section coder / bit estimator
// (dyn_bits.go + bit_cnt_count.go) so the cgo parity oracle in
// internal/parity_tests/enc-qc-main can drive them without being in-package.
// They forward 1:1. This is the inner-loop bit-counting core of the AAC encoder
// quantization / rate-control loop (qc_main.cpp's FDKaacEnc_QCMain): for a
// quantized channel it returns the dynamic-bit consumption the loop drives toward
// the budget. Every value is an INT / SHORT / UINT in integer domain; the parity
// test compares the returned bit total AND the resulting SECTION_DATA fields
// (noOfSections, huffmanBits, sideInfoBits, scalefacBits, noiseNrgBits, firstScf,
// and the per-section codeBook/sfbStart/sfbCnt/sectionBits) element-for-element,
// bit-for-bit, against the genuine vendored FDKaacEnc_dynBitCount.

// DynBitCountResult mirrors the parts of SECTION_DATA the parity oracle compares
// after a FDKaacEnc_dynBitCount call: the total bits and the per-section breakdown.
type DynBitCountResult struct {
	TotalBits    int
	NoOfSections int
	HuffmanBits  int
	SideInfoBits int
	ScalefacBits int
	NoiseNrgBits int
	FirstScf     int
	// Per-section fields, length NoOfSections.
	SectCodeBook    []int
	SectSfbStart    []int
	SectSfbCnt      []int
	SectSectionBits []int
}

// DynBitCountForParity forwards to dynBitCount (dyn_bits.go), allocating a fresh
// bitCntrState scratch and a fresh SectionData, and projects the resulting
// SectionData onto DynBitCountResult so the oracle can compare it field-for-field.
func DynBitCountForParity(quantSpectrum []int16, maxValueInSfb []uint,
	scalefac []int, blockType, sfbCnt, maxSfbPerGroup, sfbPerGroup int,
	sfbOffset []int, noiseNrg, isBook, isScale []int, syntaxFlags uint) DynBitCountResult {
	hBC := new(bitCntrState)
	sd := new(SectionData)

	total := dynBitCount(hBC, quantSpectrum, maxValueInSfb, scalefac,
		blockType, sfbCnt, maxSfbPerGroup, sfbPerGroup, sfbOffset, sd,
		noiseNrg, isBook, isScale, syntaxFlags)

	res := DynBitCountResult{
		TotalBits:    total,
		NoOfSections: sd.NoOfSections,
		HuffmanBits:  sd.HuffmanBits,
		SideInfoBits: sd.SideInfoBits,
		ScalefacBits: sd.ScalefacBits,
		NoiseNrgBits: sd.NoiseNrgBits,
		FirstScf:     sd.FirstScf,
	}
	for i := 0; i < sd.NoOfSections; i++ {
		res.SectCodeBook = append(res.SectCodeBook, sd.HuffSection[i].CodeBook)
		res.SectSfbStart = append(res.SectSfbStart, sd.HuffSection[i].SfbStart)
		res.SectSfbCnt = append(res.SectSfbCnt, sd.HuffSection[i].SfbCnt)
		res.SectSectionBits = append(res.SectSectionBits, sd.HuffSection[i].SectionBits)
	}
	return res
}

// CountValuesForParity forwards to countValues (bit_cnt_count.go): the Huffman
// bit cost of coding width coefficients with a specific codeBook.
func CountValuesForParity(values []int16, width, codeBook int) int {
	return countValues(values, width, codeBook)
}

// BitCountForParity forwards to bitCount (bit_cnt_count.go), returning the
// per-codebook bit-cost row [0, CODE_BOOK_ESC_NDX]. The returned slice has
// CODE_BOOK_ESC_NDX+1 == 12 cells.
func BitCountForParity(values []int16, width, maxVal int) []int {
	bc := make([]int, codeBookEscNdx+1)
	bitCount(values, width, maxVal, bc)
	return bc
}
