// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// SBR high-frequency-generation ROM tables (sbr_rom.cpp), 1:1 ports. The values
// are materialised from their FL2FXCONST_DBL float literals through the shared
// nativeaac narrowing (Fl2fxconstDBL) at init so the Go ROM is byte-identical to
// the vendored in-RAM C symbols. (The QMF prototype/phaseshift ROM lives in
// rom.go; the broader SBR ROM is sbr_rom.cpp's own batch — only the tables this
// HF-gen batch consumes are duplicated here, to be reconciled when the SBR-ROM
// batch lands.)

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// numWhFactorTableEntries is NUM_WHFACTOR_TABLE_ENTRIES (sbr_rom.h:139).
const numWhFactorTableEntries = 9

// whFactorsIndex is FDK_sbrDecoder_sbr_whFactorsIndex (sbr_rom.cpp:165): the
// crossover-frequency breakpoints (Hz) selecting a whitening tuning row.
var whFactorsIndex = [numWhFactorTableEntries]uint16{
	0, 5000, 6000, 6500, 7000, 7500, 8000, 9000, 10000,
}

// whFactorsTable is FDK_sbrDecoder_sbr_whFactorsTable (sbr_rom.cpp:177): the
// whitening-levels tuning table [entry][OFF, TRANSITION, LOW, MID, HIGH]. With
// the shipped tuning every row is identical. The C declares 6 columns (the 6th
// padded to 0); the port keeps the 5 used FIXP_DBL columns. Materialised at init.
var whFactorsTable [numWhFactorTableEntries][5]int32

func init() {
	// The C literals carry the float `f` suffix (FL2FXCONST_DBL(0.6f) ...), so each
	// is narrowed through float32 BEFORE the Q1.31 conversion — load-bearing for
	// bit-exactness (the float forms differ from their double forms in the low bits).
	off := nativeaac.Fl2fxconstDBL(float64(float32(0.00)))
	tr := nativeaac.Fl2fxconstDBL(float64(float32(0.6)))
	lo := nativeaac.Fl2fxconstDBL(float64(float32(0.75)))
	mid := nativeaac.Fl2fxconstDBL(float64(float32(0.90)))
	hi := nativeaac.Fl2fxconstDBL(float64(float32(0.98)))
	for i := 0; i < numWhFactorTableEntries; i++ {
		whFactorsTable[i] = [5]int32{off, tr, lo, mid, hi}
	}
}
