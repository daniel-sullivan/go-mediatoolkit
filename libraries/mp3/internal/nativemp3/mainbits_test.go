package nativemp3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A canonical MPEG-1 Layer III, 44100 Hz, 128 kbps, stereo, no-padding,
// no-CRC frame header: FF FB 90 04.
//
//	0xFF        — sync byte
//	0xFB        — sync(111) + MPEG1(11) + LayerIII(01) + noCRC(1)
//	0x90        — bitrate idx 9 (128 kbps) + samplerate idx 0 (44100) + nopad
//	0x04        — stereo (00) + ms-ext(00) + ...
var hdrMP1L3 = []byte{0xFF, 0xFB, 0x90, 0x04}

func TestHeaderAccessors(t *testing.T) {
	h := hdrMP1L3
	require.True(t, hdrValid(h))
	assert.False(t, hdrIsMono(h))
	assert.False(t, hdrIsFreeFormat(h))
	assert.False(t, hdrIsCRC(h)) // 0xFB has bit0=1 => not CRC-protected
	assert.False(t, hdrIsLayer1(h))
	assert.False(t, hdrIsFrame576(h))  // MPEG-1 => 1152 samples
	assert.Equal(t, 1, hdrGetLayer(h)) // raw layer field 01 = Layer III
	assert.Equal(t, 9, hdrGetBitrate(h))
	assert.Equal(t, 0, hdrGetSampleRate(h))
	assert.Equal(t, uint(44100), hdrSampleRateHz(h))
	assert.Equal(t, uint(128), hdrBitrateKbps(h))
	assert.Equal(t, uint(1152), hdrFrameSamples(h))
	assert.Equal(t, 0, hdrPadding(h))

	// hdr_frame_bytes = 1152*128*125/44100 = 417 (integer division).
	assert.Equal(t, 417, hdrFrameBytes(h, 0))
}

func TestHdrIsCRC(t *testing.T) {
	// HDR_IS_CRC(h) == !(h[1] & 1). For 0xFB, bit0 is 1 => not protected.
	assert.False(t, hdrIsCRC([]byte{0xFF, 0xFB, 0x90, 0x04}))
	// For 0xFA, bit0 is 0 => CRC protected.
	assert.True(t, hdrIsCRC([]byte{0xFF, 0xFA, 0x90, 0x04}))
}

func TestHdrCompare(t *testing.T) {
	h1 := []byte{0xFF, 0xFB, 0x90, 0x04}
	// Same version/layer/samplerate/freeformat, different bitrate => match.
	h2 := []byte{0xFF, 0xFB, 0xA0, 0x04}
	assert.True(t, hdrCompare(h1, h2))
	// Different layer (Layer II, raw field 10) => no match.
	h3 := []byte{0xFF, 0xFD, 0x90, 0x04}
	assert.False(t, hdrCompare(h1, h3))
}

func TestGetBits(t *testing.T) {
	// 0b1010_1100, 0b0011_0101 = 0xAC, 0x35.
	data := []byte{0xAC, 0x35}
	var bs BitStream
	BsInit(&bs, data, len(data))
	assert.Equal(t, uint32(0b101), GetBits(&bs, 3))   // top 3 bits of 0xAC
	assert.Equal(t, uint32(0b01100), GetBits(&bs, 5)) // remaining 5 bits of 0xAC
	assert.Equal(t, uint32(0x35), GetBits(&bs, 8))    // all of 0x35
	// Reading past the limit yields 0 but advances pos past limit.
	assert.Equal(t, uint32(0), GetBits(&bs, 1))
	assert.Greater(t, bs.Pos, bs.Limit)
}

func TestReservoirRoundTrip(t *testing.T) {
	// Save a tail of main data, then restore it in front of a new frame's
	// payload, exercising L3_save_reservoir + L3_restore_reservoir.
	var dec Decoder
	var s Scratch

	// Pretend a previous frame left 5 bytes of unconsumed main data at
	// scratch.maindata[2..7), consumed up to bit 16 (pos=16) of a 56-bit
	// (7-byte) buffer.
	prev := []byte{0xDE, 0xAD, 0x11, 0x22, 0x33, 0x44, 0x55}
	copy(s.Maindata[:], prev)
	s.Bs.Pos = 16
	s.Bs.Limit = len(prev) * 8
	L3SaveReservoir(&dec, &s)
	require.Equal(t, 5, dec.Reserv)
	assert.Equal(t, []byte{0x11, 0x22, 0x33, 0x44, 0x55}, dec.ReservBuf[:5])

	// Now a new frame whose side info says main_data_begin = 3: the decoder
	// should prepend the last 3 reservoir bytes (0x33,0x44,0x55) ahead of
	// the new frame payload.
	payload := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	var bs BitStream
	BsInit(&bs, payload, len(payload))
	ok := L3RestoreReservoir(&dec, &bs, &s, 3)
	require.True(t, ok) // reserv (5) >= main_data_begin (3)
	// bytes_have = min(5,3)=3, frame_bytes = 4 => maindata = reservoir tail
	// (3 bytes) + payload (4 bytes).
	assert.Equal(t, []byte{0x33, 0x44, 0x55, 0xAA, 0xBB, 0xCC, 0xDD}, s.Maindata[:7])
	assert.Equal(t, 7*8, s.Bs.Limit)
}

func TestFindFrameSyncsOnSecondOffset(t *testing.T) {
	// One garbage byte, then a self-consistent run of identical 417-byte
	// frames so mp3d_match_frame is satisfied.
	const frameLen = 417
	buf := []byte{0x00}
	for i := 0; i < MaxFrameSyncMatches+2; i++ {
		frame := make([]byte, frameLen)
		copy(frame, hdrMP1L3)
		buf = append(buf, frame...)
	}
	freeFmt := 0
	frameBytes := 0
	off := mp3dFindFrame(buf, len(buf), &freeFmt, &frameBytes)
	assert.Equal(t, 1, off)
	assert.Equal(t, frameLen, frameBytes)
}
