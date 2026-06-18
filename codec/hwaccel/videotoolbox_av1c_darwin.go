//go:build darwin

// AV1 codec-configuration-record (av1C) builder for the VideoToolbox AV1
// decoder. VideoToolbox requires the av1C atom (AV1-ISOBMFF §2.3.3) in the
// format description's SampleDescriptionExtensionAtoms. The record is a 4-byte
// fixed header followed by the configOBUs (the sequence-header OBU), which the
// hardware uses to configure the decoder.

package hwaccel

// buildAV1CConfigRecord builds the av1C configuration record from a temporal
// unit: parse the sequence header for the fields the fixed header carries, and
// append the sequence-header OBU verbatim as the configOBUs. Returns nil if no
// sequence header is present.
func buildAV1CConfigRecord(tu []byte) []byte {
	obus := splitAV1OBUs(tu)
	var seqOBU *av1OBU
	for i := range obus {
		if obus[i].typ == av1OBUSequenceHeader {
			seqOBU = &obus[i]
			break
		}
	}
	if seqOBU == nil {
		return nil
	}
	s, err := parseAV1SeqHeader(seqOBU.payload)
	if err != nil {
		return nil
	}

	seqLevelIdx0 := 0 // not separately captured; level 0 is accepted by the decoder
	seqTier0 := 0
	highBitdepth := 0
	twelveBit := 0
	switch s.bitDepth {
	case 10:
		highBitdepth = 1
	case 12:
		highBitdepth = 1
		twelveBit = 1
	}
	monochrome := 0
	if s.monoChrome {
		monochrome = 1
	}

	// av1C fixed header (AV1-ISOBMFF §2.3.3):
	//   byte0: marker(1)=1, version(7)=1            -> 0x81
	//   byte1: seq_profile(3), seq_level_idx_0(5)
	//   byte2: seq_tier_0(1), high_bitdepth(1), twelve_bit(1), monochrome(1),
	//          chroma_subsampling_x(1), chroma_subsampling_y(1),
	//          chroma_sample_position(2)
	//   byte3: reserved(3)=0, initial_presentation_delay_present(1)=0,
	//          initial_presentation_delay_minus_one(4)=0  -> 0x00
	rec := []byte{
		0x81,
		byte(s.seqProfile<<5 | seqLevelIdx0&0x1f),
		byte(seqTier0<<7 | highBitdepth<<6 | twelveBit<<5 | monochrome<<4 |
			(s.subsamplingX&1)<<3 | (s.subsamplingY&1)<<2 | (s.chromaSamplePosition & 0x3)),
		0x00,
	}

	// configOBUs: the sequence-header OBU, reconstructed with its (1- or
	// 2-byte) header + leb128 size + payload, exactly as it appeared in the TU.
	rec = append(rec, reconstructOBU(seqOBU)...)
	return rec
}

// reconstructOBU rebuilds the on-wire bytes of an OBU (header + size field +
// payload) from a parsed av1OBU. obu_has_size_field is forced on.
func reconstructOBU(o *av1OBU) []byte {
	hdr := byte(o.typ<<3) | 0x02 // has_size_field
	ext := o.temporalID != 0 || o.spatialID != 0
	if ext {
		hdr |= 0x04
	}
	out := []byte{hdr}
	if ext {
		out = append(out, byte(o.temporalID<<5|o.spatialID<<3))
	}
	out = append(out, encodeLeb128(len(o.payload))...)
	out = append(out, o.payload...)
	return out
}

// encodeLeb128 encodes a non-negative int as little-endian base-128.
func encodeLeb128(v int) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			break
		}
	}
	return out
}
