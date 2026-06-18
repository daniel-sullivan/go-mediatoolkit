// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Package sbr is the pure-Go 1:1 port of the Fraunhofer FDK-AAC Spectral Band
// Replication (SBR) subsystem — the HE-AAC v1 high-band reconstruction tools —
// derived from the vendored libfdk libSBRdec / libSBRenc / libFDK QMF
// (libraries/aac/libfdk). It is the SBR-specific companion to the AAC-LC core in
// internal/nativeaac, which it imports for the shared fixed-point DCT/DST/FFT and
// multiply/scale kernels (never re-porting them).
//
// This file ports the libFDK QMF filterbank (qmf.cpp + qmf_pcm.h, ~833+621
// lines): the complex exponential-modulated polyphase filterbank that maps time
// samples to a 64-band complex QMF subband matrix (analysis) and back
// (synthesis). It is the computational foundation every other SBR tool builds
// on. fdk-aac SBR is FIXED-POINT: all values are int32 FIXP_DBL Q-format with
// FIXP_SGL Q1.15 ROM, so the reproducibility contract is EXACT integer equality
// (cf. the integer-parity note in nativeaac.go) — there is no FP/aac_strict
// discipline here.
//
// # Scope (HE-AAC v1, STD mode, 64 bands)
//
// The decoder default is the HIGH-QUALITY (complex) filterbank at no_channels ==
// 64 (32 on the analysis input side via the dual-rate prototype), the STD branch
// (NOT CLDFB / MPS-LDFB / down-sampled). Accordingly this port covers exactly:
//   - qmfForwardModulationHQ (qmf.cpp:221-300) + qmfAnaPrototypeFirSlot
//     (qmf_pcm.h:439-485) + qmfAnalysisFilteringSlot/qmfAnalysisFiltering
//     (qmf_pcm.h:525-620) — time -> complex subband matrix.
//   - qmfInverseModulationHQ (qmf.cpp:398-475) + qmfSynPrototypeFirSlot
//     (qmf_pcm.h:128-215) + qmfSynthesisFilteringSlot/qmfSynthesisFiltering
//     (qmf_pcm.h:305-404) — complex subband matrix -> time.
//   - qmfInitFilterBank / qmfInitAnalysisFilterBank / qmfInitSynthesisFilterBank
//     (qmf.cpp:485-747) restricted to the 64-band STD case, plus
//     qmfChangeOutScalefactor / qmfChangeOutGain / qmfGetOutScalefactor /
//     qmfAdaptFilterStates (qmf.cpp:696-814).
//
// EXCLUDED (not HE-AAC v1 STD, flagged for later batches): the QMF_FLAG_LP
// low-power even/odd modulation paths (qmfInverseModulationLP_*/qmfForward
// ModulationLP_*), the CLDFB / MPS-LDFB / down-sampled / NonSymmetric prototype
// branches, and the non-64 channel counts. The LP forward-even path's dct_III
// and the dct_II inverse are still reachable via the exported nativeaac wrappers
// should a later low-power batch need them, but are not wired here.
package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// QMF algorithmic-scaling constants, 1:1 from qmf.h:156/168 and
// FDK_tools_rom.h:213.
const (
	algScalingAnalysis  = 7 // ALGORITHMIC_SCALING_IN_ANALYSIS_FILTERBANK (qmf.h:156)
	algScalingSynthesis = 1 // ALGORITHMIC_SCALING_IN_SYNTHESIS_FILTERBANK (qmf.h:168)
	qmfNoPoly           = 5 // QMF_NO_POLY (FDK_tools_rom.h:213)
)

// QMF init flags, 1:1 from qmf.h:124-143. Only the subset reachable in the
// 64-band STD HE-AAC v1 path is given a name; CLDFB/MPSLDFB/etc. are not used by
// this port but the bit values are kept faithful for the init logic.
const (
	flagLP           = 1  // QMF_FLAG_LP
	flagNonSymmetric = 2  // QMF_FLAG_NONSYMMETRIC
	flagCLDFB        = 4  // QMF_FLAG_CLDFB
	flagKeepStates   = 8  // QMF_FLAG_KEEP_STATES
	flagMPSLDFB      = 16 // QMF_FLAG_MPSLDFB
	flagDownsampled  = 64 // QMF_FLAG_DOWNSAMPLED
)

// FilterBank is the QMF_FILTER_BANK handle, a 1:1 port of struct QMF_FILTER_BANK
// (qmf.h:177-201) restricted to the fields the 64-band STD analysis/synthesis
// path reads. pFilter is the FIXP_SGL prototype-filter ROM (qmfPfilt640), tCos /
// tSin the FIXP_SGL phaseshift twiddles (qmfPhaseshiftCos64/Sin64).
//
// FilterStates holds the polyphase delay line: int32 FIXP_QAS on the analysis
// side and int32 FIXP_QSS on the synthesis side (both alias FIXP_DBL here), with
// (2*QMF_NO_POLY-1)*no_channels == 9*64 elements.
type FilterBank struct {
	pFilter []int16 // p_filter (FIXP_PFT == FIXP_SGL)

	filterStates []int32 // FilterStates (FIXP_QAS / FIXP_QSS == FIXP_DBL)
	filterSize   int     // FilterSize
	tCos         []int16 // t_cos (FIXP_QTW == FIXP_SGL)
	tSin         []int16 // t_sin
	filterScale  int     // filterScale

	noChannels int // no_channels
	noCol      int // no_col
	lsb        int // top of low subbands
	usb        int // top of high subbands

	synScalefactor int   // syn-only
	outScalefactor int   // syn-only
	outGainM       int32 // outGain_m (init 0x80000000 == ignore)
	outGainE       int   // outGain_e

	flags   uint
	pStride int // p_stride
}

// ScaleFactor is the QMF_SCALE_FACTOR (qmf.h:170-175): the per-area block
// exponents the analysis writes and the synthesis reads.
type ScaleFactor struct {
	LbScale   int // lb_scale
	OvLbScale int // ov_lb_scale
	HbScale   int // hb_scale
	OvHbScale int // ov_hb_scale
}

// fMinI / fMaxI are the integer fMin/fMax used by the init lsb/usb clamps
// (qmf.cpp:671-674). Inlined twins of fMin/fMax over plain int.
func fMinI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func fMaxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// initFilterBank is the 1:1 port of qmfInitFilterBank (qmf.cpp:485-687) for the
// 64-band STD branch (no MPSLDFB, no CLDFB). It wires the prototype filter and
// phaseshift ROM, the polyphase stride, and the analysis/synthesis scale
// headroom. synflag selects synthesis (1) vs analysis (0). Returns 0 on success.
//
// Only no_channels == 64 is supported here (the SBR HE-AAC v1 case); other
// channel counts return -1, exactly as the C default branches do for the
// unsupported sizes in this port's scope.
func initFilterBank(h *FilterBank, filterStates []int32, noCols, lsb, usb, noChannels int, flags uint, synflag int) int {
	*h = FilterBank{}

	if flags&flagMPSLDFB != 0 {
		return -1 // MPS-LDFB excluded from HE-AAC v1 scope.
	}
	if flags&flagCLDFB != 0 {
		return -1 // CLDFB excluded from HE-AAC v1 scope.
	}

	// !(MPSLDFB) && !(CLDFB): the STD branch (qmf.cpp:558-630). HE-AAC v1 uses the
	// 64-band synthesis and the 32-band analysis (dual-rate SBR); both are ported.
	switch noChannels {
	case 64:
		h.pFilter = qmfPfilt640[:]
		h.tCos = qmfPhaseshiftCos64[:]
		h.tSin = qmfPhaseshiftSin64[:]
		h.pStride = 1
		h.filterSize = 640
		h.filterScale = 0
	case 32:
		// 32-band analysis (qmf.cpp:580-592). Non-downsampled dual-rate SBR uses
		// qmf_phaseshift_cos32/sin32; the QMF_FLAG_DOWNSAMPLED single-rate variant
		// is excluded (HE-AAC v1 dual-rate always runs full analysis+synthesis).
		h.pFilter = qmfPfilt640[:]
		h.tCos = qmfPhaseshiftCos32[:]
		h.tSin = qmfPhaseshiftSin32[:]
		h.pStride = 2
		h.filterSize = 640
		h.filterScale = 0
	default:
		return -1
	}

	h.synScalefactor = h.filterScale
	// DCT|DST dependency (qmf.cpp:633-664): 64-band and 32-band cases in scope.
	switch noChannels {
	case 64:
		h.synScalefactor += algScalingSynthesis
	case 32:
		h.synScalefactor += algScalingSynthesis - 1
	default:
		return -1
	}

	h.flags = flags
	h.noChannels = noChannels
	h.noCol = noCols

	h.lsb = fMinI(lsb, h.noChannels)
	if synflag != 0 {
		h.usb = fMinI(usb, h.noChannels)
	} else {
		h.usb = usb
	}

	h.filterStates = filterStates

	h.outScalefactor = (algScalingAnalysis + h.filterScale) + h.synScalefactor

	h.outGainM = int32(-0x80000000) // 0x80000000, default ignore value
	h.outGainE = 0

	return 0
}

// InitAnalysisFilterBank is the 1:1 port of qmfInitAnalysisFilterBank
// (qmf_pcm.h:414-433). It initialises the analysis bank over pFilterStates
// (which must hold (2*QMF_NO_POLY-1)*no_channels == 9*64 int32 FIXP_QAS) and
// clears the states unless QMF_FLAG_KEEP_STATES is set. Returns 0 on success.
func InitAnalysisFilterBank(h *FilterBank, pFilterStates []int32, noCols, lsb, usb, noChannels int, flags uint) int {
	err := initFilterBank(h, pFilterStates, noCols, lsb, usb, noChannels, flags, 0)
	if flags&flagKeepStates == 0 && h.filterStates != nil {
		clearInt32(h.filterStates, (2*qmfNoPoly-1)*h.noChannels)
	}
	return err
}

// InitSynthesisFilterBank is the 1:1 port of qmfInitSynthesisFilterBank
// (qmf.cpp:721-747). It initialises the synthesis bank over pFilterStates (9*64
// int32 FIXP_QSS) and clears them unless QMF_FLAG_KEEP_STATES is set (in which
// case it adapts the states to the new out-scale via qmfAdaptFilterStates).
func InitSynthesisFilterBank(h *FilterBank, pFilterStates []int32, noCols, lsb, usb, noChannels int, flags uint) int {
	oldOutScale := h.outScalefactor
	err := initFilterBank(h, pFilterStates, noCols, lsb, usb, noChannels, flags, 1)
	if h.filterStates != nil {
		if flags&flagKeepStates == 0 {
			clearInt32(h.filterStates, (2*qmfNoPoly-1)*h.noChannels)
		} else {
			adaptFilterStates(h, oldOutScale-h.outScalefactor)
		}
	}
	return err
}

// adaptFilterStates is the 1:1 port of qmfAdaptFilterStates (qmf.cpp:696-711):
// it rescales the synthesis filter states by scaleFactorDiff, saturating on a
// positive (right-to-left) shift.
func adaptFilterStates(h *FilterBank, scaleFactorDiff int) {
	if h == nil || h.filterStates == nil {
		return
	}
	n := h.noChannels * (qmfNoPoly*2 - 1)
	if scaleFactorDiff > 0 {
		nativeaac.ScaleValuesSaturateInPlace(h.filterStates, n, int32(scaleFactorDiff))
	} else {
		for i := 0; i < n; i++ {
			h.filterStates[i] = nativeaac.ScaleValue(h.filterStates[i], int32(scaleFactorDiff))
		}
	}
}

// ChangeOutScalefactor is the 1:1 port of qmfChangeOutScalefactor
// (qmf.cpp:756-780): it folds the internal filterbank scale into the requested
// output scalefactor and adapts the filter states if it changed.
func ChangeOutScalefactor(h *FilterBank, outScalefactor int) {
	if h == nil {
		return
	}
	outScalefactor += (algScalingAnalysis + h.filterScale) + h.synScalefactor
	if h.outScalefactor != outScalefactor {
		diff := h.outScalefactor - outScalefactor
		adaptFilterStates(h, diff)
		h.outScalefactor = outScalefactor
	}
}

// GetOutScalefactor is the 1:1 port of qmfGetOutScalefactor (qmf.cpp:789-798).
func GetOutScalefactor(h *FilterBank) int {
	if h.outScalefactor != 0 {
		return h.outScalefactor - (algScalingAnalysis + h.filterScale + h.synScalefactor)
	}
	return 0
}

// ChangeOutGain is the 1:1 port of qmfChangeOutGain (qmf.cpp:807-814).
func ChangeOutGain(h *FilterBank, outputGain int32, outputGainScale int) {
	h.outGainM = outputGain
	h.outGainE = outputGainScale
}

// clearInt32 zeroes the first n elements of s (FDKmemclear).
func clearInt32(s []int32, n int) {
	for i := 0; i < n; i++ {
		s[i] = 0
	}
}
