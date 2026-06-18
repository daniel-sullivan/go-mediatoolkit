// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Decorrelator types, ported 1:1 from the vendored Fraunhofer FDK-AAC
// libFDK/include/FDK_decorrelate.h. The HE-AAC v2 baseline PS path uses
// DECORR_PS with the INDEP_CPLX_PS reverb-band filter type and the PS ducker
// (transient/peak-decay smoothing). Only the PS-relevant fields are exercised;
// the MPS/USAC/LD configs are ported faithfully in the ROM but unreachable here.
//
// FIXED-POINT / arch convention: __ARM_ARCH_8__ implies ARCH_PREFER_MULT_32x16
// (ARCH_PREFER_MULT_32x32 NOT defined), so FIXP_DECORR == FIXP_SGL (Q1.15) and
// FIXP_MPS == FIXP_DBL (Q-format). The filter numerator/denominator and packed
// allpass coefficients are FIXP_SGL.

// fdkDecorrType mirrors FDK_DECORR_TYPE (FDK_decorrelate.h:133).
type fdkDecorrType int

const (
	decorrMps fdkDecorrType = iota
	decorrPs
	decorrUsac
	decorrLd
)

// fdkDuckerType mirrors FDK_DUCKER_TYPE (FDK_decorrelate.h:143).
type fdkDuckerType int

const (
	duckerAutomatic fdkDuckerType = iota
	duckerMps
	duckerPs
)

// revbandFiltType mirrors REVBAND_FILT_TYPE (FDK_decorrelate.h:153).
type revbandFiltType int

const (
	notExist revbandFiltType = iota
	delayBand
	commonReal
	commonCplx
	indepCplx
	indepCplxPs
)

// duckerInstance mirrors DUCKER_INSTANCE (FDK_decorrelate.h:168-193). The (28)
// parameter-band and 2*(28) smooth-energy arrays are sized as the C.
type duckerInstance struct {
	hybridBands      int
	parameterBands   int
	partiallyComplex int
	duckerType       fdkDuckerType

	qsNext                []uint8
	mapProcBands2HybBands []uint8
	mapHybBands2ProcBands []uint8

	smoothDirRevNrg [2 * 28]int32

	peakDecay               [28]int32
	peakDiff                [28]int32
	maxValDirectData        int32
	maxValReverbData        int32
	scaleDirectNrg          int8
	scaleReverbNrg          int8
	scaleSmoothDirRevNrg    int8
	headroomSmoothDirRevNrg int8
}

// decorrFilterInstance mirrors DECORR_FILTER_INSTANCE (FDK_decorrelate.h:195-202).
// stateCplx / delayBufferCplx are slices into the shared decorr buffers; the
// numerator/denominator/packed-coeff pointers index the static PS ROM.
type decorrFilterInstance struct {
	stateCplx       []int32
	delayBufferCplx []int32

	numeratorReal   []int16
	coeffsPacked    []fixSTP // FIXP_STP packed allpass coefficients
	denominatorReal []int16
}

// decorrDec mirrors struct DECORR_DEC (FDK_decorrelate.h:204-222).
type decorrDec struct {
	lStateBufferCplx int
	stateBufferCplx  []int32
	lDelayBufferCplx int
	delayBufferCplx  []int32

	revFiltType                []revbandFiltType
	revBandOffset              []uint8
	revDelay                   []uint8
	revFilterOrder             []int8
	reverbBandDelayBufferIndex [4]int
	stateBufferOffset          [3]uint8

	filter [71]decorrFilterInstance
	ducker duckerInstance

	numbins          int
	partiallyComplex int
}

// fixSTP is FIXP_STP/FIXP_SPK: a packed pair of FIXP_SGL (Q1.15) values, used for
// the decorrelator's packed allpass coefficients.
type fixSTP struct {
	re int16
	im int16
}
