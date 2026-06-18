// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hooks for the vbrtag parity oracle
// (internal/parity_tests/vbrtag). The VbrTag.c port (vbrtag.go) is normally
// reached through FrameEncodeStages.AddVbrFrame (per frame), copy_buffer
// (nMusicCRC / nBytesWritten) and lameInitBitstream (InitVbrTag); these hooks add
// the thin entries the byte-identical parity suite needs to (1) probe the CRC
// table + step, (2) drive lame_get_lametag_frame over a LameInternalFlags the
// test reconstructs from the C oracle's captured -V2 encode state, and (3)
// cross-check the seek-table + LAME-extension sub-functions. mp3lame-gated like
// the slice.

// CRC16LookupParity returns crc16_lookup[i] for the parity table check.
func CRC16LookupParity(i int) uint { return crc16Lookup[i] }

// CRCUpdateLookupParity exposes crcUpdateLookup for the parity oracle.
func CRCUpdateLookupParity(value, crc uint16) uint16 { return crcUpdateLookup(value, crc) }

// UpdateMusicCRCParity folds buffer[:size] into crc via UpdateMusicCRC.
func UpdateMusicCRCParity(crc uint16, buffer []byte) uint16 {
	c := crc
	UpdateMusicCRC(&c, buffer, len(buffer))
	return c
}

// AddVbrParity appends one frame's bitrate to a VbrSeekInfo (the static addVbr),
// for cross-checking the bag arithmetic against the C oracle's captured bag.
func AddVbrParity(v *VbrSeekInfo, bitrate int) { addVbr(v, bitrate) }

// XingSeekTableParity fills t (len numTocEntries) from v via xingSeekTable.
func XingSeekTableParity(v *VbrSeekInfo, t []byte) { xingSeekTable(v, t) }

// SetLameTagFrameHeaderParity exposes setLameTagFrameHeader for the parity
// oracle (the 4-byte embedded-frame MPEG header).
func (gfc *LameInternalFlags) SetLameTagFrameHeaderParity(buffer []byte) {
	setLameTagFrameHeader(gfc, buffer)
}

// PutLameVBRParity exposes putLameVBR for the parity oracle, returning the byte
// count written into pbtStreamBuffer.
func (gfc *LameInternalFlags) PutLameVBRParity(gfp *LameGlobalFlags, nMusicLength uint,
	pbtStreamBuffer []byte, crc uint16) int {
	return putLameVBR(gfc, gfp, nMusicLength, pbtStreamBuffer, crc)
}

// LameGetLametagFrameParity exposes lameGetLametagFrame for the parity oracle,
// assembling the full Xing/Info + LAME tag frame into buffer and returning the
// tag frame size.
func (gfc *LameInternalFlags) LameGetLametagFrameParity(gfp *LameGlobalFlags,
	buffer []byte, size int) int {
	return lameGetLametagFrame(gfc, gfp, buffer, size)
}

// NumTocEntries exposes the Xing TOC length for the parity oracle.
func NumTocEntries() int { return numTocEntries }
