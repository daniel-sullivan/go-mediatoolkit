// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the pure-Go 1:1 port of the rate-control state structures the
// FDK-AAC quantizer/coder driver (qc_main.cpp / qc_data.h) operates on. It
// extends the partial qc_data.h structs declared in bitenc_types.go (which
// carried only the fields the bitstream writer reads) with the rate-loop
// fields that FDKaacEnc_QCMain, FDKaacEnc_EncodeFrame and their helpers touch.
//
// Every field carries its C counterpart. These structs are pure integer
// state; there is no floating point. The aacfdk fence keeps a default
// `go build ./...` from linking any FDK-derived code.

package nativeaac

// QcdataBrMode mirrors QCDATA_BR_MODE (qc_data.h:114): the rate-control mode.
type QcdataBrMode int

const (
	QcdataBrModeInvalid QcdataBrMode = -1 // QCDATA_BR_MODE_INVALID
	QcdataBrModeCBR     QcdataBrMode = 0  // QCDATA_BR_MODE_CBR
	QcdataBrModeVBR1    QcdataBrMode = 1  // QCDATA_BR_MODE_VBR_1
	QcdataBrModeVBR2    QcdataBrMode = 2  // QCDATA_BR_MODE_VBR_2
	QcdataBrModeVBR3    QcdataBrMode = 3  // QCDATA_BR_MODE_VBR_3
	QcdataBrModeVBR4    QcdataBrMode = 4  // QCDATA_BR_MODE_VBR_4
	QcdataBrModeVBR5    QcdataBrMode = 5  // QCDATA_BR_MODE_VBR_5
	QcdataBrModeFF      QcdataBrMode = 6  // QCDATA_BR_MODE_FF
	QcdataBrModeSFR     QcdataBrMode = 7  // QCDATA_BR_MODE_SFR
)

// isConstantBitrateMode mirrors isConstantBitrateMode() (qc_data.h): true for
// CBR and the fixed-framing/superframe modes.
func isConstantBitrateMode(m QcdataBrMode) bool {
	return m == QcdataBrModeCBR || m == QcdataBrModeFF || m == QcdataBrModeSFR
}

// AacencBitresMode mirrors AACENC_BITRES_MODE (aacenc.h): full / reduced /
// disabled bit reservoir. The driver compares only against FULL.
type AacencBitresMode int

const (
	AacencBrModeFull     AacencBitresMode = 0 // AACENC_BR_MODE_FULL
	AacencBrModeReduced  AacencBitresMode = 1 // AACENC_BR_MODE_REDUCED
	AacencBrModeDisabled AacencBitresMode = 2 // AACENC_BR_MODE_DISABLED
)

// Padding mirrors the PADDING struct (qc_data.h:144): the byte-padding
// accumulator FDKaacEnc_framePadding maintains across frames.
type Padding struct {
	PaddingRest int // paddingRest
}

// ElementBits mirrors ELEMENT_BITS (qc_data.h:286): the per-element bit budget
// and bit-reservoir split used by FDKaacEnc_BitResRedistribution and
// FDKaacEnc_distributeElementDynBits.
type ElementBits struct {
	ChBitrateEl     int   // chBitrateEl
	MaxBitsEl       int   // maxBitsEl (used in crash recovery)
	BitResLevelEl   int   // bitResLevelEl
	MaxBitResBitsEl int   // maxBitResBitsEl
	RelativeBitsEl  int32 // relativeBitsEl (FIXP_DBL)
}
