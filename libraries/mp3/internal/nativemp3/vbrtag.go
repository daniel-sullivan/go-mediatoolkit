// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

// VbrTag.c — Xing/Info/LAME VBR tag construction. 1:1 port of LAME 3.100
// libmp3lame/VbrTag.c (the Xing VBR tagging by A.L. Faber / Jonathan Dee). This
// code is LAME-derived (LGPL-2.0-or-later) and is fenced behind //go:build
// mp3lame so the default toolkit build links zero LGPL.
//
// The tag is a single embedded layer-III frame written ahead of the audio. For
// a VBR (-V) stream it carries the magic "Xing", a flags word, the total frame
// count, the total stream byte size, a 100-entry seek TOC, and the LAME extension
// (encoder version, VBR method, lowpass, ReplayGain, encoder delay/padding, the
// music CRC over the audio, and a CRC over the tag itself). The byte content
// depends on the real encoded frames: AddVbrFrame accumulates each frame's
// bitrate into VBR_seek_table, copy_buffer accumulates nMusicCRC and
// nBytesWritten over the emitted audio, and lame_get_lametag_frame finalises the
// frame once the stream is complete.
package nativemp3

import "math"

// --- VbrTag.h constants ---

// numTocEntries is VbrTag.h:48 (#define NUMTOCENTRIES 100): the Xing seek TOC
// length.
const numTocEntries = 100

// Xing header flag bits (VbrTag.h:43-46).
const (
	framesFlag   = 0x0001 // FRAMES_FLAG
	bytesFlag    = 0x0002 // BYTES_FLAG
	tocFlag      = 0x0004 // TOC_FLAG
	vbrScaleFlag = 0x0008 // VBR_SCALE_FLAG
)

// VBRHEADERSIZE / LAMEHEADERSIZE (VbrTag.c:59/61): the Xing header size and the
// full LAME header size.
const (
	vbrHeaderSize  = numTocEntries + 4 + 4 + 4 + 4 + 4
	lameHeaderSize = vbrHeaderSize + 9 + 1 + 1 + 8 + 1 + 1 + 3 + 1 + 1 + 2 + 4 + 2 + 2
)

// Xing pretend-bitrates for the embedded tag frame (VbrTag.c:64-66).
const (
	xingBitrate1  = 128 // XING_BITRATE1  (MPEG1)
	xingBitrate2  = 64  // XING_BITRATE2  (MPEG2)
	xingBitrate25 = 32  // XING_BITRATE25 (MPEG2.5)
)

// maxFrameSize is InitVbrTag's MAXFRAMESIZE (VbrTag.c:498): the max freeformat
// 640 kbps 32 kHz frame size.
const maxFrameSize = 2880

// vbrTag0 / vbrTag1 are the magic strings (VbrTag.c:70/71): "Xing" for a VBR
// stream, "Info" for a CBR stream.
var (
	vbrTag0 = [4]byte{'X', 'i', 'n', 'g'}
	vbrTag1 = [4]byte{'I', 'n', 'f', 'o'}
)

// lameTagEncoderShortVersion is version.c:147 get_lame_tag_encoder_short_version:
// "LAME" + major + "." + minor + P, where P is " " for a release with patch 0.
// For LAME 3.100 (release, patch 0) it is "LAME3.100 " (trailing space). The tag
// strncpy's the first 9 bytes ("LAME3.100"). version.h:37-43, version.c:147-155.
const lameTagEncoderShortVersion = "LAME3.100 "

// crc16Lookup is the fast CRC-16 table (VbrTag.c:80, polynomial
// x^16+x^15+x^2+1), used by CRC_update_lookup.
var crc16Lookup = [256]uint{
	0x0000, 0xC0C1, 0xC181, 0x0140, 0xC301, 0x03C0, 0x0280, 0xC241,
	0xC601, 0x06C0, 0x0780, 0xC741, 0x0500, 0xC5C1, 0xC481, 0x0440,
	0xCC01, 0x0CC0, 0x0D80, 0xCD41, 0x0F00, 0xCFC1, 0xCE81, 0x0E40,
	0x0A00, 0xCAC1, 0xCB81, 0x0B40, 0xC901, 0x09C0, 0x0880, 0xC841,
	0xD801, 0x18C0, 0x1980, 0xD941, 0x1B00, 0xDBC1, 0xDA81, 0x1A40,
	0x1E00, 0xDEC1, 0xDF81, 0x1F40, 0xDD01, 0x1DC0, 0x1C80, 0xDC41,
	0x1400, 0xD4C1, 0xD581, 0x1540, 0xD701, 0x17C0, 0x1680, 0xD641,
	0xD201, 0x12C0, 0x1380, 0xD341, 0x1100, 0xD1C1, 0xD081, 0x1040,
	0xF001, 0x30C0, 0x3180, 0xF141, 0x3300, 0xF3C1, 0xF281, 0x3240,
	0x3600, 0xF6C1, 0xF781, 0x3740, 0xF501, 0x35C0, 0x3480, 0xF441,
	0x3C00, 0xFCC1, 0xFD81, 0x3D40, 0xFF01, 0x3FC0, 0x3E80, 0xFE41,
	0xFA01, 0x3AC0, 0x3B80, 0xFB41, 0x3900, 0xF9C1, 0xF881, 0x3840,
	0x2800, 0xE8C1, 0xE981, 0x2940, 0xEB01, 0x2BC0, 0x2A80, 0xEA41,
	0xEE01, 0x2EC0, 0x2F80, 0xEF41, 0x2D00, 0xEDC1, 0xEC81, 0x2C40,
	0xE401, 0x24C0, 0x2580, 0xE541, 0x2700, 0xE7C1, 0xE681, 0x2640,
	0x2200, 0xE2C1, 0xE381, 0x2340, 0xE101, 0x21C0, 0x2080, 0xE041,
	0xA001, 0x60C0, 0x6180, 0xA141, 0x6300, 0xA3C1, 0xA281, 0x6240,
	0x6600, 0xA6C1, 0xA781, 0x6740, 0xA501, 0x65C0, 0x6480, 0xA441,
	0x6C00, 0xACC1, 0xAD81, 0x6D40, 0xAF01, 0x6FC0, 0x6E80, 0xAE41,
	0xAA01, 0x6AC0, 0x6B80, 0xAB41, 0x6900, 0xA9C1, 0xA881, 0x6840,
	0x7800, 0xB8C1, 0xB981, 0x7940, 0xBB01, 0x7BC0, 0x7A80, 0xBA41,
	0xBE01, 0x7EC0, 0x7F80, 0xBF41, 0x7D00, 0xBDC1, 0xBC81, 0x7C40,
	0xB401, 0x74C0, 0x7580, 0xB541, 0x7700, 0xB7C1, 0xB681, 0x7640,
	0x7200, 0xB2C1, 0xB381, 0x7340, 0xB101, 0x71C0, 0x7080, 0xB041,
	0x5000, 0x90C1, 0x9181, 0x5140, 0x9301, 0x53C0, 0x5280, 0x9241,
	0x9601, 0x56C0, 0x5780, 0x9741, 0x5500, 0x95C1, 0x9481, 0x5440,
	0x9C01, 0x5CC0, 0x5D80, 0x9D41, 0x5F00, 0x9FC1, 0x9E81, 0x5E40,
	0x5A00, 0x9AC1, 0x9B81, 0x5B40, 0x9901, 0x59C0, 0x5880, 0x9841,
	0x8801, 0x48C0, 0x4980, 0x8941, 0x4B00, 0x8BC1, 0x8A81, 0x4A40,
	0x4E00, 0x8EC1, 0x8F81, 0x4F40, 0x8D01, 0x4DC0, 0x4C80, 0x8C41,
	0x4400, 0x84C1, 0x8581, 0x4540, 0x8701, 0x47C0, 0x4680, 0x8641,
	0x8201, 0x42C0, 0x4380, 0x8341, 0x4100, 0x81C1, 0x8081, 0x4040,
}

// addVbr is a 1:1 translation of addVbr (VbrTag.c:123): append one frame's
// bitrate to the seek bag, halving the bag (and doubling the chunk size) when it
// fills.
//
//	static void
//	addVbr(VBR_seek_info_t * v, int bitrate)
func addVbr(v *VbrSeekInfo, bitrate int) {
	v.NVbrNumFrames++
	v.Sum += bitrate
	v.Seen++

	if v.Seen < v.Want {
		return
	}

	if v.Pos < v.Size {
		v.Bag[v.Pos] = v.Sum
		v.Pos++
		v.Seen = 0
	}
	if v.Pos == v.Size {
		for i := 1; i < v.Size; i += 2 {
			v.Bag[i/2] = v.Bag[i]
		}
		v.Want *= 2
		v.Pos /= 2
	}
}

// xingSeekTable is a 1:1 translation of Xing_seek_table (VbrTag.c:150): fill the
// 100-entry TOC by sampling the cumulative-bitrate bag.
//
//	static void
//	Xing_seek_table(VBR_seek_info_t const* v, unsigned char *t)
func xingSeekTable(v *VbrSeekInfo, t []byte) {
	if v.Pos <= 0 {
		return
	}

	for i := 1; i < numTocEntries; i++ {
		j := float32(i) / float32(numTocEntries)
		indx := int(math.Floor(float64(j * float32(v.Pos))))
		if indx > v.Pos-1 {
			indx = v.Pos - 1
		}
		act := float32(v.Bag[indx])
		sum := float32(v.Sum)
		seekPoint := int(256.0 * act / sum)
		if seekPoint > 255 {
			seekPoint = 255
		}
		t[i] = byte(seekPoint)
	}
}

// extractI4 is a 1:1 translation of ExtractI4 (VbrTag.c:205): big-endian 4-byte
// read.
func extractI4(buf []byte) int {
	x := int(buf[0])
	x <<= 8
	x |= int(buf[1])
	x <<= 8
	x |= int(buf[2])
	x <<= 8
	x |= int(buf[3])
	return x
}

// createI4 is a 1:1 translation of CreateI4 (VbrTag.c:220): big-endian 4-byte
// write.
func createI4(buf []byte, nValue uint32) {
	buf[0] = byte((nValue >> 24) & 0xff)
	buf[1] = byte((nValue >> 16) & 0xff)
	buf[2] = byte((nValue >> 8) & 0xff)
	buf[3] = byte(nValue & 0xff)
}

// createI2 is a 1:1 translation of CreateI2 (VbrTag.c:232): big-endian 2-byte
// write.
func createI2(buf []byte, nValue int) {
	buf[0] = byte((nValue >> 8) & 0xff)
	buf[1] = byte(nValue & 0xff)
}

// isVbrTag is a 1:1 translation of IsVbrTag (VbrTag.c:241): true if buf begins
// with "Xing" or "Info".
func isVbrTag(buf []byte) bool {
	isTag0 := buf[0] == vbrTag0[0] && buf[1] == vbrTag0[1] && buf[2] == vbrTag0[2] && buf[3] == vbrTag0[3]
	isTag1 := buf[0] == vbrTag1[0] && buf[1] == vbrTag1[1] && buf[2] == vbrTag1[2] && buf[3] == vbrTag1[3]
	return isTag0 || isTag1
}

// shiftInBitsValue is VbrTag.c:254's SHIFT_IN_BITS_VALUE(x,n,v) macro:
// x = (x << n) | (v & ~(-1 << n)). It returns the new byte value.
func shiftInBitsValue(x byte, n uint, v int) byte {
	mask := ^(-1 << n)
	return byte((int(x) << n) | (v & mask))
}

// setLameTagFrameHeader is a 1:1 translation of setLameTagFrameHeader
// (VbrTag.c:256): build the 4-byte MPEG frame header for the embedded tag frame.
//
//	static void
//	setLameTagFrameHeader(lame_internal_flags const *gfc, unsigned char *buffer)
func setLameTagFrameHeader(gfc *LameInternalFlags, buffer []byte) {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc
	var abyte, bbyte byte

	buffer[0] = shiftInBitsValue(buffer[0], 8, 0xff)

	errProt := 0
	if cfg.ErrorProtection == 0 {
		errProt = 1
	}
	srLow := 0
	if cfg.SamplerateOut >= 16000 {
		srLow = 1
	}
	buffer[1] = shiftInBitsValue(buffer[1], 3, 7)
	buffer[1] = shiftInBitsValue(buffer[1], 1, srLow)
	buffer[1] = shiftInBitsValue(buffer[1], 1, cfg.Version)
	buffer[1] = shiftInBitsValue(buffer[1], 2, 4-3)
	buffer[1] = shiftInBitsValue(buffer[1], 1, errProt)

	buffer[2] = shiftInBitsValue(buffer[2], 4, eov.BitrateIndex)
	buffer[2] = shiftInBitsValue(buffer[2], 2, cfg.SamplerateIndex)
	buffer[2] = shiftInBitsValue(buffer[2], 1, 0)
	buffer[2] = shiftInBitsValue(buffer[2], 1, cfg.Extension)

	buffer[3] = shiftInBitsValue(buffer[3], 2, cfg.Mode)
	buffer[3] = shiftInBitsValue(buffer[3], 2, eov.ModeExt)
	buffer[3] = shiftInBitsValue(buffer[3], 1, cfg.Copyright)
	buffer[3] = shiftInBitsValue(buffer[3], 1, cfg.Original)
	buffer[3] = shiftInBitsValue(buffer[3], 2, cfg.Emphasis)

	// the default VBR header. 48 kbps layer III, no padding, no crc
	// but sampling freq, mode and copyright/copy protection taken
	// from first valid frame
	buffer[0] = 0xff
	abyte = buffer[1] & 0xf1
	{
		var bitrate int
		if cfg.Version == 1 {
			bitrate = xingBitrate1
		} else {
			if cfg.SamplerateOut < 16000 {
				bitrate = xingBitrate25
			} else {
				bitrate = xingBitrate2
			}
		}

		if cfg.Vbr == vbrOff {
			bitrate = cfg.AvgBitrate
		}

		if cfg.FreeFormat != 0 {
			bbyte = 0x00
		} else {
			bbyte = byte(16 * bitrateIndex(bitrate, cfg.Version, cfg.SamplerateOut))
		}
	}

	// Use as much of the info from the real frames in the
	// Xing header: samplerate, channels, crc, etc...
	if cfg.Version == 1 {
		// MPEG1
		buffer[1] = abyte | 0x0a  // was 0x0b;
		abyte = buffer[2] & 0x0d  // AF keep also private bit
		buffer[2] = bbyte | abyte // 64kbs MPEG1 frame
	} else {
		// MPEG2
		buffer[1] = abyte | 0x02  // was 0x03;
		abyte = buffer[2] & 0x0d  // AF keep also private bit
		buffer[2] = bbyte | abyte // 64kbs MPEG2 frame
	}
}

// VBRTagData is LAME's VBRTAGDATA (VbrTag.h:51): the parsed Xing/Info tag fields
// GetVbrTag fills. Ported for completeness; the encode path does not read it.
type VBRTagData struct {
	HID        int    // h_id (0:MPEG2/2.5, 1:MPEG1)
	SampRate   int    // samprate
	Flags      int    // flags
	Frames     int    // frames
	Bytes      int    // bytes
	VbrScale   int    // vbr_scale
	TOC        []byte // toc (len numTocEntries, may be nil)
	HeaderSize int    // headersize
	EncDelay   int    // enc_delay
	EncPadding int    // enc_padding
}

// getVbrTag is a 1:1 translation of GetVbrTag (VbrTag.c:361): parse a Xing/Info
// VBR tag out of buf. Returns true on success. Ported for completeness (the
// encode path does not call it; it is the reader-side counterpart).
//
//	int
//	GetVbrTag(VBRTAGDATA * pTagData, const unsigned char *buf)
func getVbrTag(pTagData *VBRTagData, buf []byte) bool {
	pTagData.Flags = 0

	hLayer := (buf[1] >> 1) & 3
	if hLayer != 0x01 {
		return false
	}
	hID := int((buf[1] >> 3) & 1)
	hSrIndex := int((buf[2] >> 2) & 3)
	hMode := int((buf[3] >> 6) & 3)
	hBitrate := int((buf[2] >> 4) & 0xf)
	hBitrate = bitrateTable[hID][hBitrate]

	if (buf[1] >> 4) == 0xE {
		pTagData.SampRate = samplerateTable[2][hSrIndex]
	} else {
		pTagData.SampRate = samplerateTable[hID][hSrIndex]
	}

	pos := 0
	if hID != 0 {
		if hMode != 3 {
			pos += 32 + 4
		} else {
			pos += 17 + 4
		}
	} else {
		if hMode != 3 {
			pos += 17 + 4
		} else {
			pos += 9 + 4
		}
	}

	if !isVbrTag(buf[pos:]) {
		return false
	}
	pos += 4

	pTagData.HID = hID

	headFlags := extractI4(buf[pos:])
	pTagData.Flags = headFlags
	pos += 4 // get flags

	if headFlags&framesFlag != 0 {
		pTagData.Frames = extractI4(buf[pos:])
		pos += 4
	}

	if headFlags&bytesFlag != 0 {
		pTagData.Bytes = extractI4(buf[pos:])
		pos += 4
	}

	if headFlags&tocFlag != 0 {
		if pTagData.TOC != nil {
			for i := 0; i < numTocEntries; i++ {
				pTagData.TOC[i] = buf[pos+i]
			}
		}
		pos += numTocEntries
	}

	pTagData.VbrScale = -1

	if headFlags&vbrScaleFlag != 0 {
		pTagData.VbrScale = extractI4(buf[pos:])
		pos += 4
	}

	pTagData.HeaderSize = ((hID + 1) * 72000 * hBitrate) / pTagData.SampRate

	pos += 21
	encDelay := int(buf[pos+0]) << 4
	encDelay += int(buf[pos+1]) >> 4
	encPadding := int(buf[pos+1]&0x0F) << 8
	encPadding += int(buf[pos+2])
	if encDelay < 0 || encDelay > 3000 {
		encDelay = -1
	}
	if encPadding < 0 || encPadding > 3000 {
		encPadding = -1
	}

	pTagData.EncDelay = encDelay
	pTagData.EncPadding = encPadding

	return true // success
}

// AddVbrFrame is a 1:1 translation of AddVbrFrame (VbrTag.c:195): add the current
// frame's bitrate to the VBR seek table. Called per frame by the dispatcher when
// cfg.WriteLameTag is set.
//
//	void
//	AddVbrFrame(lame_internal_flags * gfc)
func AddVbrFrame(gfc *LameInternalFlags) {
	kbps := bitrateTable[gfc.Cfg.Version][gfc.OvEnc.BitrateIndex]
	// assert(gfc->VBR_seek_table.bag);
	addVbr(&gfc.VBRSeekTable, kbps)
}

// InitVbrTag is a 1:1 translation of InitVbrTag (VbrTag.c:491): size the embedded
// tag frame, reset the seek table, allocate the bag, and write a dummy all-zero
// tag frame into the bitstream so the real audio starts at the right offset.
// Returns 0 on success, -1 on allocation failure (and disables the tag when it
// does not fit).
//
//	int
//	InitVbrTag(lame_global_flags * gfp)
func InitVbrTag(gfc *LameInternalFlags) int {
	cfg := &gfc.Cfg
	var kbpsHeader int

	if cfg.Version == 1 {
		kbpsHeader = xingBitrate1
	} else {
		if cfg.SamplerateOut < 16000 {
			kbpsHeader = xingBitrate25
		} else {
			kbpsHeader = xingBitrate2
		}
	}

	if cfg.Vbr == vbrOff {
		kbpsHeader = cfg.AvgBitrate
	}

	// make sure LAME Header fits into Frame
	{
		totalFrameSize := ((cfg.Version + 1) * 72000 * kbpsHeader) / cfg.SamplerateOut
		headerSize := cfg.SideinfoLen + lameHeaderSize
		gfc.VBRSeekTable.TotalFrameSize = uint(totalFrameSize)
		if totalFrameSize < headerSize || totalFrameSize > maxFrameSize {
			// disable tag, it wont fit
			gfc.Cfg.WriteLameTag = 0
			return 0
		}
	}

	gfc.VBRSeekTable.NVbrNumFrames = 0
	gfc.VBRSeekTable.NBytesWritten = 0
	gfc.VBRSeekTable.Sum = 0

	gfc.VBRSeekTable.Seen = 0
	gfc.VBRSeekTable.Want = 1
	gfc.VBRSeekTable.Pos = 0

	if gfc.VBRSeekTable.Bag == nil {
		gfc.VBRSeekTable.Bag = make([]int, 400) // lame_calloc(int, 400)
		gfc.VBRSeekTable.Size = 400
	}

	// write dummy VBR tag of all 0's into bitstream
	{
		var buffer [maxFrameSize]byte
		setLameTagFrameHeader(gfc, buffer[:])
		n := int(gfc.VBRSeekTable.TotalFrameSize)
		for i := 0; i < n; i++ {
			gfc.addDummyByte(buffer[i], 1)
		}
	}
	return 0
}

// crcUpdateLookup is a 1:1 translation of CRC_update_lookup (VbrTag.c:582): the
// table-driven CRC-16 step.
//
//	static uint16_t
//	CRC_update_lookup(uint16_t value, uint16_t crc)
func crcUpdateLookup(value, crc uint16) uint16 {
	tmp := crc ^ value
	crc = (crc >> 8) ^ uint16(crc16Lookup[tmp&0xff])
	return crc
}

// UpdateMusicCRC is a 1:1 translation of UpdateMusicCRC (VbrTag.c:591): fold size
// audio bytes into the running music CRC.
//
//	void
//	UpdateMusicCRC(uint16_t * crc, unsigned char const *buffer, int size)
func UpdateMusicCRC(crc *uint16, buffer []byte, size int) {
	for i := 0; i < size; i++ {
		*crc = crcUpdateLookup(uint16(buffer[i]), *crc)
	}
}

// putLameVBR is a 1:1 translation of PutLameVBR (VbrTag.c:614): write the LAME
// extension (encoder version, VBR method, lowpass, ReplayGain, encoder
// delay/padding, music length + CRC, and the tag CRC) into pbtStreamBuffer.
// Returns the number of bytes written.
//
//	static int
//	PutLameVBR(lame_global_flags const *gfp, size_t nMusicLength,
//	           uint8_t * pbtStreamBuffer, uint16_t crc)
func putLameVBR(gfc *LameInternalFlags, gfp *LameGlobalFlags, nMusicLength uint, pbtStreamBuffer []byte, crc uint16) int {
	cfg := &gfc.Cfg

	nBytesWritten := 0

	encDelay := gfc.OvEnc.EncoderDelay     // encoder delay
	encPadding := gfc.OvEnc.EncoderPadding // encoder padding

	nQuality := 100 - 10*gfp.VBRq - gfp.Quality

	szVersion := lameTagEncoderShortVersion
	var nVBR byte
	var nRevision byte = 0x00
	var nRevMethod byte
	vbrTypeTranslator := [7]byte{1, 5, 3, 2, 4, 0, 3} // numbering different in vbr_mode vs. Lame tag

	nLowpass := byte(0)
	{
		lp := float64(cfg.LowpassFreq)/100.0 + 0.5
		if lp > 255 {
			nLowpass = 255
		} else {
			nLowpass = byte(lp)
		}
	}

	var nPeakSignalAmplitude uint32 = 0

	var nRadioReplayGain uint16 = 0
	var nAudiophileReplayGain uint16 = 0

	nNoiseShaping := byte(cfg.NoiseShaping)
	var nStereoMode byte = 0
	bNonOptimal := 0
	var nSourceFreq byte = 0
	var nMisc byte = 0
	var nMusicCRC uint16 = 0

	// psy model type: Gpsycho or NsPsytune
	var bExpNPsyTune byte = 1 // only NsPsytune
	var bSafeJoint byte = 0
	if cfg.UseSafeJointStereo != 0 {
		bSafeJoint = 1
	}

	var bNoGapMore byte = 0
	var bNoGapPrevious byte = 0

	nNoGapCount := gfp.NogapTotal
	nNoGapCurr := gfp.NogapCurrent

	nAthType := byte(cfg.ATHtype) // 4 bits.

	var nFlags byte = 0

	// if ABR, {store bitrate <=255} else { store "-b"}
	var nABRBitrate int
	switch cfg.Vbr {
	case vbrAbr:
		nABRBitrate = cfg.VbrAvgBitrateKbps
	case vbrOff:
		nABRBitrate = cfg.AvgBitrate
	default: // vbr modes
		nABRBitrate = bitrateTable[cfg.Version][cfg.VbrMinBitrateIndex]
	}

	// revision and vbr method
	if cfg.Vbr < len(vbrTypeTranslator) {
		nVBR = vbrTypeTranslator[cfg.Vbr]
	} else {
		nVBR = 0x00 // unknown.
	}

	nRevMethod = 0x10*nRevision + nVBR

	// ReplayGain
	if cfg.FindReplayGain != 0 {
		radioGain := gfc.OvRpg.RadioGain
		if radioGain > 0x1FE {
			radioGain = 0x1FE
		}
		if radioGain < -0x1FE {
			radioGain = -0x1FE
		}

		nRadioReplayGain = 0x2000 // set name code
		nRadioReplayGain |= 0xC00 // set originator code to `determined automatically'

		if radioGain >= 0 {
			nRadioReplayGain |= uint16(radioGain) // set gain adjustment
		} else {
			nRadioReplayGain |= 0x200              // set the sign bit
			nRadioReplayGain |= uint16(-radioGain) // set gain adjustment
		}
	}

	// peak sample
	if cfg.FindPeakSample != 0 {
		nPeakSignalAmplitude = uint32(absInt(vbrPeakSignalAmplitude(gfc.OvRpg.PeakSample)))
	}

	// nogap
	if nNoGapCount != -1 {
		if nNoGapCurr > 0 {
			bNoGapPrevious = 1
		}
		if nNoGapCurr < nNoGapCount-1 {
			bNoGapMore = 1
		}
	}

	// flags
	nFlags = nAthType + (bExpNPsyTune << 4) +
		(bSafeJoint << 5) +
		(bNoGapMore << 6) +
		(bNoGapPrevious << 7)

	if nQuality < 0 {
		nQuality = 0
	}

	// stereo mode field... a bit ugly.
	switch cfg.Mode {
	case mpegMono: // MONO
		nStereoMode = 0
	case mpegStereo: // STEREO
		nStereoMode = 1
	case mpegDualChannel: // DUAL_CHANNEL
		nStereoMode = 2
	case mpegJointStereo: // JOINT_STEREO
		if cfg.ForceMs != 0 {
			nStereoMode = 4
		} else {
			nStereoMode = 3
		}
	case mpegNotSet: // NOT_SET
		fallthrough
	default:
		nStereoMode = 7
	}

	// Intensity stereo : nStereoMode = 6. IS is not implemented

	if cfg.SamplerateIn <= 32000 {
		nSourceFreq = 0x00
	} else if cfg.SamplerateIn == 48000 {
		nSourceFreq = 0x02
	} else if cfg.SamplerateIn > 48000 {
		nSourceFreq = 0x03
	} else {
		nSourceFreq = 0x01 // default is 44100Hz.
	}

	// Check if the user overrided the default LAME behaviour with some nasty options
	if cfg.ShortBlocks == shortBlockForced || cfg.ShortBlocks == shortBlockDispensed ||
		((cfg.LowpassFreq == -1) && (cfg.HighpassFreq == -1)) || // "-k"
		(cfg.DisableReservoir != 0 && cfg.AvgBitrate < 320) ||
		cfg.NoATH != 0 || cfg.ATHonly != 0 || (nAthType == 0) || cfg.SamplerateIn <= 32000 {
		bNonOptimal = 1
	}

	nMisc = nNoiseShaping + (nStereoMode << 2) +
		byte(bNonOptimal<<5) +
		(nSourceFreq << 6)

	nMusicCRC = gfc.NMusicCRC

	// Write all this information into the stream
	createI4(pbtStreamBuffer[nBytesWritten:], uint32(nQuality))
	nBytesWritten += 4

	// strncpy((char *) &pbtStreamBuffer[nBytesWritten], szVersion, 9)
	for i := 0; i < 9; i++ {
		pbtStreamBuffer[nBytesWritten+i] = szVersion[i]
	}
	nBytesWritten += 9

	pbtStreamBuffer[nBytesWritten] = nRevMethod
	nBytesWritten++

	pbtStreamBuffer[nBytesWritten] = nLowpass
	nBytesWritten++

	createI4(pbtStreamBuffer[nBytesWritten:], nPeakSignalAmplitude)
	nBytesWritten += 4

	createI2(pbtStreamBuffer[nBytesWritten:], int(nRadioReplayGain))
	nBytesWritten += 2

	createI2(pbtStreamBuffer[nBytesWritten:], int(nAudiophileReplayGain))
	nBytesWritten += 2

	pbtStreamBuffer[nBytesWritten] = nFlags
	nBytesWritten++

	if nABRBitrate >= 255 {
		pbtStreamBuffer[nBytesWritten] = 0xFF
	} else {
		pbtStreamBuffer[nBytesWritten] = byte(nABRBitrate)
	}
	nBytesWritten++

	pbtStreamBuffer[nBytesWritten] = byte(encDelay >> 4)
	pbtStreamBuffer[nBytesWritten+1] = byte((encDelay << 4) + (encPadding >> 8))
	pbtStreamBuffer[nBytesWritten+2] = byte(encPadding)

	nBytesWritten += 3

	pbtStreamBuffer[nBytesWritten] = nMisc
	nBytesWritten++

	pbtStreamBuffer[nBytesWritten] = 0 // unused in rev0
	nBytesWritten++

	createI2(pbtStreamBuffer[nBytesWritten:], cfg.Preset)
	nBytesWritten += 2

	createI4(pbtStreamBuffer[nBytesWritten:], uint32(int(nMusicLength)))
	nBytesWritten += 4

	createI2(pbtStreamBuffer[nBytesWritten:], int(nMusicCRC))
	nBytesWritten += 2

	// Calculate tag CRC.... must be done here, since it includes
	// previous information
	for i := 0; i < nBytesWritten; i++ {
		crc = crcUpdateLookup(uint16(pbtStreamBuffer[i]), crc)
	}

	createI2(pbtStreamBuffer[nBytesWritten:], int(crc))
	nBytesWritten += 2

	return nBytesWritten
}

// lameGetLametagFrame is a 1:1 translation of lame_get_lametag_frame
// (VbrTag.c:899): assemble the complete Xing/Info + LAME tag frame into buffer.
// Returns the tag frame size (TotalFrameSize) if it built the frame, the
// required size if buffer is too small, or 0 if the tag is disabled / has no
// data. gfp carries VBR_q / quality / nogap; gfc carries the seek table and CRC.
//
//	size_t
//	lame_get_lametag_frame(lame_global_flags const *gfp, unsigned char *buffer, size_t size)
func lameGetLametagFrame(gfc *LameInternalFlags, gfp *LameGlobalFlags, buffer []byte, size int) int {
	cfg := &gfc.Cfg

	if cfg.WriteLameTag == 0 {
		return 0
	}
	if gfc.VBRSeekTable.Pos <= 0 {
		return 0
	}
	if size < int(gfc.VBRSeekTable.TotalFrameSize) {
		return int(gfc.VBRSeekTable.TotalFrameSize)
	}
	if buffer == nil {
		return 0
	}

	total := int(gfc.VBRSeekTable.TotalFrameSize)
	for i := 0; i < total; i++ {
		buffer[i] = 0
	}

	// 4 bytes frame header
	setLameTagFrameHeader(gfc, buffer)

	// Clear all TOC entries
	var btToc [numTocEntries]byte

	if cfg.FreeFormat != 0 {
		for i := 1; i < numTocEntries; i++ {
			btToc[i] = byte(255 * i / 100)
		}
	} else {
		xingSeekTable(&gfc.VBRSeekTable, btToc[:])
	}

	// Start writing the tag after the zero frame
	nStreamIndex := cfg.SideinfoLen
	// note! Xing header specifies that Xing data goes in the ancillary data
	// with NO ERROR PROTECTION. If error protection is enabled, the Xing data
	// still starts at the same offset, and now it is in sideinfo data block, and
	// thus will not decode correctly by non-Xing tag aware players.
	if cfg.ErrorProtection != 0 {
		nStreamIndex -= 2
	}

	// Put Vbr tag
	if cfg.Vbr == vbrOff {
		buffer[nStreamIndex] = vbrTag1[0]
		nStreamIndex++
		buffer[nStreamIndex] = vbrTag1[1]
		nStreamIndex++
		buffer[nStreamIndex] = vbrTag1[2]
		nStreamIndex++
		buffer[nStreamIndex] = vbrTag1[3]
		nStreamIndex++
	} else {
		buffer[nStreamIndex] = vbrTag0[0]
		nStreamIndex++
		buffer[nStreamIndex] = vbrTag0[1]
		nStreamIndex++
		buffer[nStreamIndex] = vbrTag0[2]
		nStreamIndex++
		buffer[nStreamIndex] = vbrTag0[3]
		nStreamIndex++
	}

	// Put header flags
	createI4(buffer[nStreamIndex:], framesFlag+bytesFlag+tocFlag+vbrScaleFlag)
	nStreamIndex += 4

	// Put Total Number of frames
	createI4(buffer[nStreamIndex:], uint32(gfc.VBRSeekTable.NVbrNumFrames))
	nStreamIndex += 4

	// Put total audio stream size, including Xing/LAME Header
	streamSize := gfc.VBRSeekTable.NBytesWritten + uint64(gfc.VBRSeekTable.TotalFrameSize)
	createI4(buffer[nStreamIndex:], uint32(streamSize))
	nStreamIndex += 4

	// Put TOC
	copy(buffer[nStreamIndex:nStreamIndex+numTocEntries], btToc[:])
	nStreamIndex += numTocEntries

	if cfg.ErrorProtection != 0 {
		// (jo) error_protection: add crc16 information to header
		gfc.crcWriteheader(buffer)
	}
	{
		// work out CRC so far: initially crc = 0
		var crc uint16 = 0x00
		for i := 0; i < nStreamIndex; i++ {
			crc = crcUpdateLookup(uint16(buffer[i]), crc)
		}
		// Put LAME VBR info
		nStreamIndex += putLameVBR(gfc, gfp, uint(streamSize), buffer[nStreamIndex:], crc)
	}

	return int(gfc.VBRSeekTable.TotalFrameSize)
}
