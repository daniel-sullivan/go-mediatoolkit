// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file holds the interface-level types for the AAC decoder's Temporal
// Noise Shaping (TNS) filter area, ported 1:1 from the vendored FDK-AAC
// reference. The TNS data layout is a faithful translation of the C structs;
// the integer kernels and the scale-headroom computation live in the feature
// files tns_scale.go (integer scale primitives) and tns_filter.go (the
// block.cpp TNS-band headroom translation).
//
// Sample/scale layout note (matches the C): spectral coefficients are
// FIXP_DBL (int32) Q1.31 fixed-point MDCT lines laid out per-window; aSfbScale
// is a per-(window,sfb) SHORT (int16) exponent and specScale is the resulting
// per-window block exponent. TNS forces extra mantissa head room into the
// block exponent so the lattice predictor cannot overflow.

// TNS filter limits, ported 1:1 from aacdec_tns.h:108-117.
const (
	// tnsMaxWindows is TNS_MAX_WINDOWS (aacdec_tns.h:109).
	tnsMaxWindows = 8
	// tnsMaximumFilters is TNS_MAXIMUM_FILTERS (aacdec_tns.h:110).
	tnsMaximumFilters = 3
	// tnsMaximumOrder is TNS_MAXIMUM_ORDER (aacdec_tns.h:117): 12 for AAC-LC
	// and AAC-SSR, 20 for AAC-Main (AOT 1) and broken AAC-LC encoders, 15
	// for USAC (AOT 42).
	tnsMaximumOrder = 20
)

// CFilter is a single TNS lattice filter description, ported 1:1 from the
// CFilter struct in aacdec_tns.h:123-133.
type CFilter struct {
	// Coeff holds the parsed lattice reflection-coefficient indices
	// (SCHAR Coeff[TNS_MAXIMUM_ORDER]).
	Coeff [tnsMaximumOrder]int8

	// StartBand and StopBand are the inclusive/exclusive scalefactor-band
	// range the filter covers (UCHAR StartBand/StopBand).
	StartBand uint8
	StopBand  uint8

	// Direction is the filtering direction (SCHAR Direction): 0 forward,
	// 1 backward.
	Direction int8
	// Resolution is the coefficient resolution selector (SCHAR Resolution):
	// 3 or 4 bits per coefficient.
	Resolution int8

	// Order is the filter order (UCHAR Order).
	Order uint8
}

// CTnsData holds all TNS filters for one channel's frame, ported 1:1 from the
// CTnsData struct in aacdec_tns.h:135-145.
type CTnsData struct {
	// Filter is Filter[TNS_MAX_WINDOWS][TNS_MAXIMUM_FILTERS].
	Filter [tnsMaxWindows][tnsMaximumFilters]CFilter
	// NumberOfFilters is the active filter count per window
	// (UCHAR NumberOfFilters[TNS_MAX_WINDOWS]).
	NumberOfFilters [tnsMaxWindows]uint8
	// DataPresent is the tns_data_present flag (UCHAR DataPresent).
	DataPresent uint8
	// Active is set once any filter is present and parsed (UCHAR Active).
	Active uint8

	// GainLd is log2 of the maximum total filter gain. It is required to
	// keep necessary mantissa head room so that while applying the TNS
	// predictor the mantissas do not overflow (UCHAR GainLd).
	GainLd uint8
}
