// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the aacfdk build tag.

package nativeaac

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adtsWriter builds an ADTS bitstream MSB-first for tests, mirroring the field
// order of adtsRead_DecodeHeader's reads.
type adtsWriter struct {
	bits []byte
	n    int // bit count
}

func (w *adtsWriter) put(value uint32, nbits int) {
	for i := nbits - 1; i >= 0; i-- {
		if w.n%8 == 0 {
			w.bits = append(w.bits, 0)
		}
		bit := byte((value >> uint(i)) & 1)
		w.bits[w.n/8] |= bit << uint(7-(w.n%8))
		w.n++
	}
}

// buildADTSLC writes a protection_absent (no-CRC) AAC-LC ADTS frame header with
// num_raw_blocks=0, the path that stays entirely inside the frame-sync-parse
// slice. frameLengthBytes is the total aac_frame_length field value.
func buildADTSLC(profile, sfi, chanCfg, frameLengthBytes uint32) []byte {
	var w adtsWriter
	// adts_fixed_header
	w.put(adtsSyncword, adtsLengthSyncWord) // syncword
	w.put(1, adtsLengthID)                  // mpeg_id (MPEG-4 = 0; use 1 = MPEG-2 to avoid the mpeg4-only PCE path? keep 0)
	w.put(0, adtsLengthLayer)               // layer (must be 0)
	w.put(1, adtsLengthProtectionAbsent)    // protection_absent = 1 (no CRC)
	w.put(profile, adtsLengthProfile)
	w.put(sfi, adtsLengthSamplingFrequencyIndex)
	w.put(0, adtsLengthPrivateBit)
	w.put(chanCfg, adtsLengthChannelConfiguration)
	w.put(0, adtsLengthOriginalCopy)
	w.put(0, adtsLengthHome)
	// adts_variable_header
	w.put(0, adtsLengthCopyrightIdentificationBit)
	w.put(0, adtsLengthCopyrightIdentificationStart)
	w.put(frameLengthBytes, adtsLengthFrameLength)
	w.put(0x7FF, adtsLengthBufferFullness) // VBR sentinel -> skips fullness gate
	w.put(0, adtsLengthNumberOfRawDataBlocksInFrame)
	// pad payload so getValidBits sees frameLength*8 bits available
	for w.n < int(frameLengthBytes)*8 {
		w.put(0, 8)
	}
	return w.bits
}

func TestFindSyncword(t *testing.T) {
	t.Run("at start", func(t *testing.T) {
		frame := buildADTSLC(1, 4, 2, 32)
		r := newAdtsBitReader(frame)
		require.Equal(t, transportDecOK, findSyncword(r))
		// position is just past the 12-bit syncword.
		assert.Equal(t, 12, r.bitPos)
	})

	t.Run("after junk bytes", func(t *testing.T) {
		frame := append([]byte{0x00, 0x00}, buildADTSLC(1, 4, 2, 32)...)
		r := newAdtsBitReader(frame)
		require.Equal(t, transportDecOK, findSyncword(r))
		assert.Equal(t, 16+12, r.bitPos)
	})

	t.Run("no sync", func(t *testing.T) {
		r := newAdtsBitReader(make([]byte, 8))
		assert.Equal(t, transportDecSyncError, findSyncword(r))
	})

	t.Run("too short", func(t *testing.T) {
		r := newAdtsBitReader([]byte{0xFF})
		assert.Equal(t, transportDecNotEnoughBits, findSyncword(r))
	})
}

func TestDecodeHeaderLC(t *testing.T) {
	const frameLen = 40
	frame := buildADTSLC(1 /*AAC-LC*/, 4 /*44100*/, 2, frameLen)
	r := newAdtsBitReader(frame)
	require.Equal(t, transportDecOK, findSyncword(r))

	var a adts
	a.decoderCanDoMpeg4 = 1
	err := decodeHeader(&a, r, false)
	require.Equal(t, transportDecOK, err)

	assert.Equal(t, uint8(0), a.bs.layer)
	assert.Equal(t, uint8(1), a.bs.protectionAbsent)
	assert.Equal(t, uint8(1), a.bs.profile)
	assert.Equal(t, uint8(4), a.bs.sampleFreqIndex)
	assert.Equal(t, uint8(2), a.bs.channelConfig)
	assert.Equal(t, uint16(frameLen), a.bs.frameLength)
	assert.Equal(t, uint16(0x7FF), a.bs.adtsFullness)
	assert.Equal(t, uint8(0), a.bs.numRawBlocks)

	// Raw data block length: (frameLen - 7) << 3, no CRC subtraction since
	// protection_absent. tpdec_adts.cpp:408.
	assert.Equal(t, (int(frameLen)-7)<<3, getRawDataBlockLength(&a, 0))
}

func TestDecodeHeaderRejectsBadLayer(t *testing.T) {
	// layer != 0 -> TRANSPORTDEC_UNSUPPORTED_FORMAT.
	var w adtsWriter
	w.put(adtsSyncword, adtsLengthSyncWord)
	w.put(1, adtsLengthID)
	w.put(1, adtsLengthLayer) // bad layer
	w.put(1, adtsLengthProtectionAbsent)
	w.put(1, adtsLengthProfile)
	w.put(4, adtsLengthSamplingFrequencyIndex)
	w.put(0, adtsLengthPrivateBit)
	w.put(2, adtsLengthChannelConfiguration)
	w.put(0, adtsLengthOriginalCopy)
	w.put(0, adtsLengthHome)
	w.put(0, adtsLengthCopyrightIdentificationBit)
	w.put(0, adtsLengthCopyrightIdentificationStart)
	w.put(32, adtsLengthFrameLength)
	w.put(0x7FF, adtsLengthBufferFullness)
	w.put(0, adtsLengthNumberOfRawDataBlocksInFrame)
	for w.n < 32*8 {
		w.put(0, 8)
	}
	r := newAdtsBitReader(w.bits)
	require.Equal(t, transportDecOK, findSyncword(r))
	var a adts
	a.decoderCanDoMpeg4 = 1
	assert.Equal(t, transportDecUnsupportedFormat, decodeHeader(&a, r, false))
}

func TestSamplingRateTable(t *testing.T) {
	assert.Equal(t, uint32(44100), samplingRateTable[4])
	assert.Equal(t, uint32(48000), samplingRateTable[3])
	assert.Equal(t, 2, getNumberOfEffectiveChannels(2))
	assert.Equal(t, 8, getNumberOfTotalChannels(7))
}
