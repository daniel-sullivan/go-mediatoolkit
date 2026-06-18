// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Parity tests pinning the nativeaac ADTS frame-sync-parse port against the
// vendored Fraunhofer FDK-AAC reference compiled into this binary via cgo.
// Headers are fabricated with the field-order writer below (matching the order
// adtsRead_DecodeHeader reads), then the C oracle and the Go port are run over
// the same bytes and compared field-for-field.
//
// The ADTS parse is an integer kernel (bit reads + integer arithmetic only), so
// it is bit-identical in any build; the strict gate is kept for convention with
// the rest of the aac_strict parity discipline.
package frame_sync_parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// ADTS field bit-lengths, matching the Adts_Length_* defines in
// libfdk/libMpegTPDec/src/tpdec_adts.cpp:149.
const (
	lenSyncWord                     = 12
	lenID                           = 1
	lenLayer                        = 2
	lenProtectionAbsent             = 1
	lenProfile                      = 2
	lenSamplingFrequencyIndex       = 4
	lenPrivateBit                   = 1
	lenChannelConfiguration         = 3
	lenOriginalCopy                 = 1
	lenHome                         = 1
	lenCopyrightIdentificationBit   = 1
	lenCopyrightIdentificationStart = 1
	lenFrameLength                  = 13
	lenBufferFullness               = 11
	lenNumberOfRawDataBlocksInFrame = 2
)

const adtsSyncword = 0xfff

// bitWriter builds an ADTS bitstream MSB-first, mirroring the field order of
// adtsRead_DecodeHeader's reads.
type bitWriter struct {
	bits []byte
	n    int
}

func (w *bitWriter) put(value uint32, nbits int) {
	for i := nbits - 1; i >= 0; i-- {
		if w.n%8 == 0 {
			w.bits = append(w.bits, 0)
		}
		bit := byte((value >> uint(i)) & 1)
		w.bits[w.n/8] |= bit << uint(7-(w.n%8))
		w.n++
	}
}

// adtsFields fully describes a fabricated ADTS fixed+variable header.
type adtsFields struct {
	mpegID                uint32
	layer                 uint32
	protectionAbsent      uint32
	profile               uint32
	sampleFreqIndex       uint32
	channelConfig         uint32
	frameLength           uint32
	bufferFullness        uint32
	numRawDataBlocks      uint32
	padToFrameLengthBytes bool // pad payload so getValidBits sees frameLength*8 bits
}

// buildADTS writes one ADTS frame header from f. When padToFrameLengthBytes is
// set the buffer is zero-padded to frameLength*8 bits, the path that keeps the
// parse inside the protection_absent / num_raw_blocks==0 slice.
func buildADTS(f adtsFields) []byte {
	var w bitWriter
	// adts_fixed_header
	w.put(adtsSyncword, lenSyncWord)
	w.put(f.mpegID, lenID)
	w.put(f.layer, lenLayer)
	w.put(f.protectionAbsent, lenProtectionAbsent)
	w.put(f.profile, lenProfile)
	w.put(f.sampleFreqIndex, lenSamplingFrequencyIndex)
	w.put(0, lenPrivateBit)
	w.put(f.channelConfig, lenChannelConfiguration)
	w.put(0, lenOriginalCopy)
	w.put(0, lenHome)
	// adts_variable_header
	w.put(0, lenCopyrightIdentificationBit)
	w.put(0, lenCopyrightIdentificationStart)
	w.put(f.frameLength, lenFrameLength)
	w.put(f.bufferFullness, lenBufferFullness)
	w.put(f.numRawDataBlocks, lenNumberOfRawDataBlocksInFrame)
	if f.padToFrameLengthBytes {
		for w.n < int(f.frameLength)*8 {
			w.put(0, 8)
		}
	}
	return w.bits
}

func TestFindSyncwordParity(t *testing.T) {
	cases := []struct {
		name string
		buf  []byte
	}{
		{"valid at start", buildADTS(adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 4, channelConfig: 2, frameLength: 40, bufferFullness: 0x7FF, padToFrameLengthBytes: true})},
		{"junk prefix", append([]byte{0x00, 0x00}, buildADTS(adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 4, channelConfig: 2, frameLength: 40, bufferFullness: 0x7FF, padToFrameLengthBytes: true})...)},
		{"no sync", make([]byte, 8)},
		{"too short", []byte{0xFF}},
		{"empty", []byte{}},
		{"odd junk then sync", append([]byte{0xAB, 0xCD, 0xEF}, buildADTS(adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 3, channelConfig: 1, frameLength: 24, bufferFullness: 0x7FF, padToFrameLengthBytes: true})...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cErr, cPos := cFindSyncword(tc.buf)
			gErr, gPos := nativeaac.FindSyncwordParity(tc.buf)
			require.Equal(t, cErr, gErr, "syncword error code mismatch")
			if cErr == 0 {
				assert.Equal(t, cPos, gPos, "post-syncword bit position mismatch")
			}
		})
	}
}

func TestDecodeHeaderParity(t *testing.T) {
	// Exercise the protection_absent / num_raw_blocks==0 slice across profiles,
	// sample-rate indices, channel configs and frame lengths, plus the reject
	// paths (bad layer, high sfi, mpeg4-on-mpeg2, implicit-PCE channel_config==0).
	cases := []struct {
		name                    string
		f                       adtsFields
		decoderCanDoMpeg4       int
		bufferFullnessStartFlag int
		ignoreBufferFullness    bool
	}{
		{"aac-lc 44100 stereo", adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 4, channelConfig: 2, frameLength: 40, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 1, 0, false},
		{"aac-lc 48000 mono", adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 3, channelConfig: 1, frameLength: 24, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 1, 0, false},
		{"aac-main 32000 5.1", adtsFields{mpegID: 1, profile: 0, sampleFreqIndex: 5, channelConfig: 6, frameLength: 80, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 1, 0, false},
		{"mpeg4 8000 stereo", adtsFields{mpegID: 0, profile: 1, sampleFreqIndex: 11, channelConfig: 2, frameLength: 32, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 1, 0, false},
		{"bad layer", adtsFields{mpegID: 1, layer: 1, profile: 1, sampleFreqIndex: 4, channelConfig: 2, frameLength: 32, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 1, 0, false},
		{"sfi 13 unsupported", adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 13, channelConfig: 2, frameLength: 32, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 1, 0, false},
		{"sfi 15 unsupported", adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 15, channelConfig: 2, frameLength: 32, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 1, 0, false},
		{"mpeg4 on mpeg2-only decoder", adtsFields{mpegID: 0, profile: 1, sampleFreqIndex: 4, channelConfig: 2, frameLength: 32, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 0, 0, false},
		// channel_config==0 with mpeg_id==0 and no PCE: the C reference hits the
		// `else if (bs.mpeg_id == 0)` implicit-config reject and returns
		// UNSUPPORTED, matching the Go parsePCESeam stand-in. (channel_config==0
		// with mpeg_id==1 is deliberately NOT asserted: the Go slice defers the
		// real PCE parse to the pce-asc area, so it diverges there by design.)
		{"implicit pce channelconfig 0 mpeg2", adtsFields{mpegID: 0, profile: 1, sampleFreqIndex: 4, channelConfig: 0, frameLength: 32, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 1, 0, false},
		{"short frame not enough bits", adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 4, channelConfig: 2, frameLength: 200, bufferFullness: 0x7FF, padToFrameLengthBytes: false}, 1, 0, false},
		{"fullness gate start-flag", adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 4, channelConfig: 2, frameLength: 40, bufferFullness: 100, padToFrameLengthBytes: true}, 1, 1, false},
		{"fullness ignored", adtsFields{mpegID: 1, profile: 1, sampleFreqIndex: 4, channelConfig: 2, frameLength: 40, bufferFullness: 100, padToFrameLengthBytes: true}, 1, 1, true},
		{"low profile high sfi 12", adtsFields{mpegID: 1, profile: 2, sampleFreqIndex: 12, channelConfig: 1, frameLength: 48, bufferFullness: 0x7FF, padToFrameLengthBytes: true}, 1, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := buildADTS(tc.f)
			// Drive both sides over a syncword-prefixed buffer; both consume the
			// syncword before the field parse.
			ign := 0
			if tc.ignoreBufferFullness {
				ign = 1
			}
			cRes := cDecodeHeader(buf, tc.decoderCanDoMpeg4, tc.bufferFullnessStartFlag, ign)
			gRes := nativeaac.DecodeHeaderParity(buf, tc.decoderCanDoMpeg4, tc.bufferFullnessStartFlag, tc.ignoreBufferFullness)

			assert.Equal(t, cRes.err, gRes.Err, "error code")
			assert.Equal(t, cRes.rdbLen0, gRes.RawDataBlockLen0, "raw data block length")
			assert.Equal(t, cRes.mpegID, gRes.MPEGID, "mpeg_id")
			assert.Equal(t, cRes.layer, gRes.Layer, "layer")
			assert.Equal(t, cRes.protectionAbsent, gRes.ProtectionAbsent, "protection_absent")
			assert.Equal(t, cRes.profile, gRes.Profile, "profile")
			assert.Equal(t, cRes.sampleFreqIndex, gRes.SampleFreqIndex, "sample_freq_index")
			assert.Equal(t, cRes.privateBit, gRes.PrivateBit, "private_bit")
			assert.Equal(t, cRes.channelConfig, gRes.ChannelConfig, "channel_config")
			assert.Equal(t, cRes.original, gRes.Original, "original")
			assert.Equal(t, cRes.home, gRes.Home, "home")
			assert.Equal(t, cRes.copyrightID, gRes.CopyrightID, "copyright_id")
			assert.Equal(t, cRes.copyrightStart, gRes.CopyrightStart, "copyright_start")
			assert.Equal(t, cRes.frameLength, gRes.FrameLength, "frame_length")
			assert.Equal(t, cRes.adtsFullness, gRes.AdtsFullness, "adts_fullness")
			assert.Equal(t, cRes.numRawBlocks, gRes.NumRawBlocks, "num_raw_blocks")
			assert.Equal(t, cRes.numPceBits, gRes.NumPceBits, "num_pce_bits")
		})
	}
}
