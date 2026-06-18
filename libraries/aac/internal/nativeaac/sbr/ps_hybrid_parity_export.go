// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Thin exported driver for the PS hybrid-filterbank cgo parity oracle
// (internal/parity_tests/ps-dec-hybrid). It mirrors exactly what the C bridge
// does: open + init the analysis filterbank over the pHybridAnaStatesLFdmx-sized
// LF memory (no HF memory, THREE_TO_TEN, NO_QMF_BANDS_HYBRID20 bands) and the two
// synthesis filterbanks (THREE_TO_TEN, 64 bands), then for each input timeslot
// run analysis -> synthesis and return the recombined QMF output.

// PsHybridRun drives the PS hybrid analysis+synthesis filterbank over nSlots
// timeslots. Each slot provides NO_QMF_BANDS_HYBRID20 (3) complex QMF inputs
// (qmfRe[slot][0..2], qmfImg[slot][0..2]); the analysis splits them into 12
// sub-subbands, those (the HF bands stay zero, mirroring the apply's HF copy
// being out of scope here) feed one synthesis filterbank, and the recombined 64
// QMF output (real,imag) per slot is returned flattened. This isolates exactly
// the FDKhybridAnalysisApply -> FDKhybridSynthesisApply round trip.
func PsHybridRun(nSlots int, qmfRe, qmfImg [][]int32) (outRe, outImg []int32) {
	var ana fdkAnaHybFilter
	var syn fdkSynHybFilter

	// LF state memory: 2*13*NO_QMF_BANDS_HYBRID20, as the C pHybridAnaStatesLFdmx.
	lfMem := make([]int32, 2*13*psNoQmfBandsHybrid20)
	fdkHybridAnalysisOpen(&ana, lfMem, nil)
	fdkHybridAnalysisInit(&ana, threeToTen, psNoQmfBandsHybrid20, psNoQmfBandsHybrid20, true)
	fdkHybridSynthesisInit(&syn, threeToTen, psNoQmfChannels, psNoQmfChannels)

	const noHybridDataBands = 71
	outRe = make([]int32, nSlots*psNoQmfChannels)
	outImg = make([]int32, nSlots*psNoQmfChannels)

	for s := 0; s < nSlots; s++ {
		var qIn [2][psNoQmfBandsHybrid20]int32
		for i := 0; i < psNoQmfBandsHybrid20; i++ {
			qIn[0][i] = qmfRe[s][i]
			qIn[1][i] = qmfImg[s][i]
		}

		hybRe := make([]int32, noHybridDataBands)
		hybImg := make([]int32, noHybridDataBands)
		fdkHybridAnalysisApply(&ana, qIn[0][:], qIn[1][:], hybRe, hybImg)

		qOutRe := outRe[s*psNoQmfChannels : (s+1)*psNoQmfChannels]
		qOutImg := outImg[s*psNoQmfChannels : (s+1)*psNoQmfChannels]
		fdkHybridSynthesisApply(&syn, hybRe, hybImg, qOutRe, qOutImg)
	}
	return outRe, outImg
}
