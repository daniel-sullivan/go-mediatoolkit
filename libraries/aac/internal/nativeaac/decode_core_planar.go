// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Core-to-planar AAC-LC decode for the HE-AAC v1 path. The SBR integration lives
// in the sibling internal/nativeaac/heaac package (which imports both nativeaac
// and nativeaac/sbr); to avoid an import cycle — sbr already imports nativeaac
// for its fixed-point math primitives — nativeaac must NOT import sbr. So this
// file exposes the minimal surface heaac needs: decode the AAC-LC core to a
// per-channel planar int32 buffer (the SBR `input`), and locate the SBR
// extension payload inside the fill element (its absolute bit position + bit
// count + CRC flag) so heaac can drive the SBR parser over the same buffer.

// SbrPcmToInt16 narrows one interleaved SBR output sample (PCM_DEC int32 at
// PCM_OUT_HEADROOM == 8) to int16: scaleValueSaturate(src, 8) + round + >>16.
// It is the SBR tail of aacDecoder_DecodeFrame WITHOUT the headroom shift (the
// SBR output already carries PCM_OUT_HEADROOM, so aacdecoder_lib.cpp:1673 is a
// no-op). Exported for the heaac integration package.
func SbrPcmToInt16(src int32) int16 {
	scaled := scaleValueSaturate(src, pcmOutHeadroom)
	added := fAddSaturate(scaled, 0x8000)
	return int16(added >> 16)
}

// AacOutDataHeadroom exposes aacOutDataHeadroom (==3), the headroom of the core
// time output handed to the SBR decoder as inDataHeadroom.
const AacOutDataHeadroom = aacOutDataHeadroom

// SbrPayloadLoc describes where an sbr_extension_data payload sits in the access
// unit, so the SBR parser can be driven over the same buffer at the same bits.
type SbrPayloadLoc struct {
	Present     bool   // an EXT_SBR_DATA payload was found
	Buf         []byte // padded power-of-two AU byte buffer
	BufSize     uint32 // byte length of Buf (power of two)
	StartBit    uint32 // absolute bit position of the sbr_extension_data start
	CountBits   int    // bits available to the SBR parser
	CrcFlag     int    // 1 for EXT_SBR_DATA_CRC, else 0
	PrevElement int    // preceding core element ID (idSCE / idCPE)
}

// EXT_SBR_DATA / EXT_SBR_DATA_CRC payload types (FDK_audio.h:479-480).
const (
	extSBRData    = 0x0d
	extSBRDataCRC = 0x0e
)

// DecodeAccessUnitCorePlanar decodes one AAC-LC raw_data_block to a per-channel
// planar int32 PCM_DEC buffer (channel c occupies coreOut[c*frameLen:]) at
// aacOutDataHeadroom, returning the SBR payload location for the fill element.
// coreOut must hold channels*frameLen int32. The SBR payload bits are NOT
// consumed/decoded here — only located. The AAC-LC-only DecodeAccessUnit path is
// unchanged.
func (d *Decoder) DecodeAccessUnitCorePlanar(pkt []byte, coreOut []int32) (SbrPayloadLoc, error) {
	bufSize := uint32(1)
	for bufSize < uint32(len(pkt)) {
		bufSize <<= 1
	}
	if bufSize < 1 {
		bufSize = 1
	}
	buf := make([]byte, bufSize)
	copy(buf, pkt)

	var bs bitStream
	initBitStream(&bs, buf, bufSize, uint32(len(pkt))*8)

	flags := uint32(0)
	var l, r channelData
	var jsd JointStereoData
	var commonWindow uint8
	sawElement := false
	prevElement := -1

	var loc SbrPayloadLoc
	loc.Buf = buf
	loc.BufSize = bufSize

	for {
		id := int(bs.readBits(3)) // id_syn_ele

		switch id {
		case idSCE:
			if d.channels != 1 {
				return loc, errChannelMismatch
			}
			cTnsReset(&l.tns)
			if err := readSingleChannelElement(&bs, &l, &d.sri, d.frameLen, flags); err != aacDecOK {
				return loc, decodeError(err)
			}
			decodeMonoElement(&l, &d.sri, d.frameLen, flags)
			frequencyToTimeChannel(d.states[0], &l, &d.sri, d.frameLen, d.tmp())
			copy(coreOut[0:d.frameLen], l.timePCM[:d.frameLen])
			prevElement = idSCE
			sawElement = true

		case idCPE:
			if d.channels != 2 {
				return loc, errChannelMismatch
			}
			cTnsReset(&l.tns)
			cTnsReset(&r.tns)
			if err := readChannelPairElement(&bs, &l, &r, &commonWindow, &jsd, &d.sri, d.frameLen, flags); err != aacDecOK {
				return loc, decodeError(err)
			}
			decodeStereoElement(&l, &r, &jsd, commonWindow, &d.sri, d.frameLen, flags)
			frequencyToTimeChannel(d.states[0], &l, &d.sri, d.frameLen, d.tmp())
			frequencyToTimeChannel(d.states[1], &r, &d.sri, d.frameLen, d.tmp())
			copy(coreOut[0:d.frameLen], l.timePCM[:d.frameLen])
			copy(coreOut[d.frameLen:2*d.frameLen], r.timePCM[:d.frameLen])
			prevElement = idCPE
			sawElement = true

		case idFIL:
			bitCnt := int(bs.readBits(4))
			if bitCnt == 15 {
				bitCnt = int(bs.readBits(8)) + 14
			}
			bitCnt <<= 3 // to bits
			d.scanFillForSbr(&bs, bitCnt, prevElement, &loc)

		case idDSE:
			bs.readBits(4)
			align := bs.readBits(1)
			cnt := int(bs.readBits(8))
			if cnt == 255 {
				cnt += int(bs.readBits(8))
			}
			if align != 0 {
				bs.byteAlign()
			}
			for i := 0; i < cnt; i++ {
				bs.readBits(8)
			}

		case idEND:
			if !sawElement {
				return loc, errNoElement
			}
			return loc, nil

		default:
			return loc, errUnsupportedElement
		}
	}
}

// scanFillForSbr scans one fill element (bitCnt bits) for an EXT_SBR_DATA payload
// and records its location in loc, then consumes the whole fill element so the
// core reader stays in sync. It mirrors CAacDecoder_ExtPayloadParse's dispatch
// (aacdecoder.cpp:893-1014) but only locates (does not decode) the SBR payload.
func (d *Decoder) scanFillForSbr(bs *bitStream, bitCnt int, prevElement int, loc *SbrPayloadLoc) {
	for bitCnt >= 4 {
		extType := int(bs.readBits(4))
		bitCnt -= 4
		if (extType == extSBRData || extType == extSBRDataCRC) &&
			(prevElement == idSCE || prevElement == idCPE) {
			loc.Present = true
			loc.StartBit = bs.bitPosition()
			loc.CountBits = bitCnt
			loc.PrevElement = prevElement
			if extType == extSBRDataCRC {
				loc.CrcFlag = 1
			}
		}
		// Whether SBR or another extension, the rest of the fill element is the
		// payload; consume it. (HE-AAC v1 fill elements carrying SBR data carry no
		// other extension_payload — ISO/IEC 14496-3 4.5.2.1.5.2.)
		for bitCnt >= 8 {
			bs.readBits(8)
			bitCnt -= 8
		}
		if bitCnt > 0 {
			bs.readBits(uint32(bitCnt))
			bitCnt = 0
		}
		return
	}
	// Drain any sub-4-bit remainder.
	for bitCnt > 0 {
		take := bitCnt
		if take > 8 {
			take = 8
		}
		bs.readBits(uint32(take))
		bitCnt -= take
	}
}
