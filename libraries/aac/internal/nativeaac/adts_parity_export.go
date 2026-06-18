// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// This file exposes the ADTS frame-sync-parse port to its parity oracle, which
// lives in a separate package (internal/parity_tests/frame-sync-parse) and so
// cannot reach the unexported findSyncword / decodeHeader /
// getRawDataBlockLength across the package boundary. Mirrors the
// CalculateChaosMeasure bridge in psy_chaosmeasure.go. Not part of the shipping
// surface — purely a test seam.

package nativeaac

// ADTSResult is the exported projection of the ADTS frame-sync-parse port's
// output for one header, used by the parity oracle to field-compare against the
// vendored C reference. Err is the raw transportDecError value; RawDataBlockLen0
// is getRawDataBlockLength(.., 0).
type ADTSResult struct {
	Err              int
	RawDataBlockLen0 int
	MPEGID           uint8
	Layer            uint8
	ProtectionAbsent uint8
	Profile          uint8
	SampleFreqIndex  uint8
	PrivateBit       uint8
	ChannelConfig    uint8
	Original         uint8
	Home             uint8
	CopyrightID      uint8
	CopyrightStart   uint8
	FrameLength      uint16
	AdtsFullness     uint16
	NumRawBlocks     uint8
	NumPceBits       uint8
}

// FindSyncwordParity runs the ADTS syncword search over buf and returns the raw
// transportDecError value plus the post-syncword bit position (valid only when
// the error is transportDecOK). Exported for the frame-sync-parse parity oracle.
func FindSyncwordParity(buf []byte) (errCode, bitPos int) {
	r := newAdtsBitReader(buf)
	e := findSyncword(r)
	return int(e), r.bitPos
}

// DecodeHeaderParity fabricates a fresh ADTS reader over buf, consumes the
// syncword via findSyncword, runs decodeHeader, and projects the parsed header
// plus the block-0 raw-data-block length into an ADTSResult. decoderCanDoMpeg4,
// bufferFullnessStartFlag and ignoreBufferFullness mirror the matching C
// inputs. Exported for the frame-sync-parse parity oracle.
func DecodeHeaderParity(buf []byte, decoderCanDoMpeg4, bufferFullnessStartFlag int, ignoreBufferFullness bool) ADTSResult {
	r := newAdtsBitReader(buf)
	findSyncword(r)

	var a adts
	a.decoderCanDoMpeg4 = uint8(decoderCanDoMpeg4)
	a.bufferFullnessStartFlag = uint8(bufferFullnessStartFlag)

	e := decodeHeader(&a, r, ignoreBufferFullness)

	return ADTSResult{
		Err:              int(e),
		RawDataBlockLen0: getRawDataBlockLength(&a, 0),
		MPEGID:           a.bs.mpegID,
		Layer:            a.bs.layer,
		ProtectionAbsent: a.bs.protectionAbsent,
		Profile:          a.bs.profile,
		SampleFreqIndex:  a.bs.sampleFreqIndex,
		PrivateBit:       a.bs.privateBit,
		ChannelConfig:    a.bs.channelConfig,
		Original:         a.bs.original,
		Home:             a.bs.home,
		CopyrightID:      a.bs.copyrightID,
		CopyrightStart:   a.bs.copyrightStart,
		FrameLength:      a.bs.frameLength,
		AdtsFullness:     a.bs.adtsFullness,
		NumRawBlocks:     a.bs.numRawBlocks,
		NumPceBits:       a.bs.numPceBits,
	}
}
