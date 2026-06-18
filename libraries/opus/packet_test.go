package opus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPacketMode(t *testing.T) {
	// CELT: bit 7 set.
	assert.Equal(t, ModeCELTOnly, packetMode(0x80))
	assert.Equal(t, ModeCELTOnly, packetMode(0xFF))

	// Hybrid: bits 6:5 = 11, bit 7 = 0.
	assert.Equal(t, ModeHybrid, packetMode(0x60))
	assert.Equal(t, ModeHybrid, packetMode(0x7F))

	// SILK: bits 6:5 != 11, bit 7 = 0.
	assert.Equal(t, ModeSILKOnly, packetMode(0x00))
	assert.Equal(t, ModeSILKOnly, packetMode(0x20))
	assert.Equal(t, ModeSILKOnly, packetMode(0x40))
}

func TestPacketBandwidth(t *testing.T) {
	tests := []struct {
		name string
		toc  byte
		bw   Bandwidth
	}{
		// CELT mode (bit 7 set): bandwidth from bits 5-6.
		{"CELT NB", 0x80, BandwidthNarrowband},
		{"CELT WB", 0xA0, BandwidthWideband},
		{"CELT SWB", 0xC0, BandwidthSuperwideband},
		{"CELT FB", 0xE0, BandwidthFullband},

		// Hybrid mode: bit 4 determines SWB vs FB.
		{"Hybrid SWB", 0x60, BandwidthSuperwideband},
		{"Hybrid FB", 0x70, BandwidthFullband},

		// SILK mode: bandwidth from bits 5-6.
		{"SILK NB", 0x00, BandwidthNarrowband},
		{"SILK MB", 0x20, BandwidthMediumband},
		{"SILK WB", 0x40, BandwidthWideband},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.bw, packetBandwidth(tt.toc))
		})
	}
}

func TestPacketFrameDuration(t *testing.T) {
	// CELT mode frame durations.
	assert.Equal(t, 2.5, packetFrameDuration(0x80))  // CELT, bits 3-4 = 00
	assert.Equal(t, 5.0, packetFrameDuration(0x88))  // CELT, bits 3-4 = 01
	assert.Equal(t, 10.0, packetFrameDuration(0x90)) // CELT, bits 3-4 = 10
	assert.Equal(t, 20.0, packetFrameDuration(0x98)) // CELT, bits 3-4 = 11

	// Hybrid mode frame durations.
	assert.Equal(t, 10.0, packetFrameDuration(0x60)) // Hybrid, bit 3 = 0
	assert.Equal(t, 20.0, packetFrameDuration(0x68)) // Hybrid, bit 3 = 1

	// SILK mode frame durations.
	assert.Equal(t, 10.0, packetFrameDuration(0x00)) // SILK, bits 3-4 = 00
	assert.Equal(t, 20.0, packetFrameDuration(0x08)) // SILK, bits 3-4 = 01
	assert.Equal(t, 40.0, packetFrameDuration(0x10)) // SILK, bits 3-4 = 10
	assert.Equal(t, 60.0, packetFrameDuration(0x18)) // SILK, bits 3-4 = 11
}

func TestPacketStereo(t *testing.T) {
	assert.False(t, PacketInfo{}.Stereo) // zero value is mono
	info, _, err := parsePacket([]byte{0x04, 0x00})
	require.NoError(t, err)
	assert.True(t, info.Stereo) // bit 2 set = stereo
}

func TestParsePacketCode0(t *testing.T) {
	// Code 0: 1 frame. TOC byte + payload.
	pkt := []byte{0x98, 0xAA, 0xBB, 0xCC} // CELT 20ms mono, 3 bytes payload
	info, frames, err := parsePacket(pkt)
	require.NoError(t, err)
	assert.Equal(t, 1, info.FrameCount)
	assert.Equal(t, ModeCELTOnly, info.Mode)
	assert.Equal(t, 20.0, info.FrameDuration)
	assert.False(t, info.Stereo)
	require.Len(t, frames, 1)
	assert.Equal(t, []byte{0xAA, 0xBB, 0xCC}, frames[0].Data)
}

func TestParsePacketCode1(t *testing.T) {
	// Code 1: 2 CBR frames. TOC byte + even-length payload.
	pkt := []byte{0x99, 0x01, 0x02, 0x03, 0x04} // CELT 20ms mono code 1, 4 bytes
	info, frames, err := parsePacket(pkt)
	require.NoError(t, err)
	assert.Equal(t, 2, info.FrameCount)
	require.Len(t, frames, 2)
	assert.Equal(t, []byte{0x01, 0x02}, frames[0].Data)
	assert.Equal(t, []byte{0x03, 0x04}, frames[1].Data)
}

func TestParsePacketCode1OddLength(t *testing.T) {
	// Code 1 with odd payload length should fail.
	pkt := []byte{0x99, 0x01, 0x02, 0x03}
	_, _, err := parsePacket(pkt)
	assert.ErrorIs(t, err, ErrInvalidPacket)
}

func TestParsePacketCode2(t *testing.T) {
	// Code 2: 2 VBR frames. TOC + size1 + frame1 + frame2.
	// size1 = 2 (< 252, so 1 byte), payload after size = frame1(2) + frame2(3)
	pkt := []byte{0x9A, 0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	info, frames, err := parsePacket(pkt)
	require.NoError(t, err)
	assert.Equal(t, 2, info.FrameCount)
	require.Len(t, frames, 2)
	assert.Equal(t, []byte{0xAA, 0xBB}, frames[0].Data)
	assert.Equal(t, []byte{0xCC, 0xDD, 0xEE}, frames[1].Data)
}

func TestParsePacketCode3CBR(t *testing.T) {
	// Code 3, CBR, 3 frames. CELT 20ms (960 samples).
	// ch byte: count=3, no padding, CBR (bit 7 = 0) → 0x03
	payload := make([]byte, 9) // 3 bytes per frame
	pkt := append([]byte{0x9B, 0x03}, payload...)
	info, frames, err := parsePacket(pkt)
	require.NoError(t, err)
	assert.Equal(t, 3, info.FrameCount)
	require.Len(t, frames, 3)
	for _, f := range frames {
		assert.Len(t, f.Data, 3)
	}
}

func TestParsePacketCode3VBR(t *testing.T) {
	// Code 3, VBR, 2 frames. CELT 20ms.
	// ch byte: count=2, no padding, VBR (bit 7 = 1) → 0x82
	// size[0] = 3 (1 byte), size[1] = remaining (2 bytes)
	pkt := []byte{0x9B, 0x82, 0x03, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	info, frames, err := parsePacket(pkt)
	require.NoError(t, err)
	assert.Equal(t, 2, info.FrameCount)
	require.Len(t, frames, 2)
	assert.Len(t, frames[0].Data, 3)
	assert.Len(t, frames[1].Data, 2)
}

func TestParsePacketCode3WithPadding(t *testing.T) {
	// Code 3, CBR, 2 frames, with 5 bytes padding.
	// ch byte: count=2, padding=1, CBR → 0x42
	// padding: 0x05 (5 bytes)
	payload := make([]byte, 4) // 2 bytes per frame
	padding := make([]byte, 5)
	pkt := append([]byte{0x9B, 0x42, 0x05}, payload...)
	pkt = append(pkt, padding...)
	info, frames, err := parsePacket(pkt)
	require.NoError(t, err)
	assert.Equal(t, 2, info.FrameCount)
	require.Len(t, frames, 2)
	assert.Len(t, frames[0].Data, 2)
	assert.Len(t, frames[1].Data, 2)
}

func TestParsePacketEmpty(t *testing.T) {
	_, _, err := parsePacket(nil)
	assert.ErrorIs(t, err, ErrInvalidPacket)

	_, _, err = parsePacket([]byte{})
	assert.ErrorIs(t, err, ErrInvalidPacket)
}

func TestParsePacketTooManyFrames(t *testing.T) {
	// Code 3 with count that exceeds 120ms at 48kHz.
	// CELT 20ms = 960 samples. 960 * 7 = 6720 > 5760 → invalid.
	pkt := []byte{0x9B, 0x07, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, _, err := parsePacket(pkt)
	assert.ErrorIs(t, err, ErrInvalidPacket)
}

func TestSamplesPerFrame(t *testing.T) {
	assert.Equal(t, 120, SamplesPerFrame(2.5, 48000))
	assert.Equal(t, 240, SamplesPerFrame(5, 48000))
	assert.Equal(t, 480, SamplesPerFrame(10, 48000))
	assert.Equal(t, 960, SamplesPerFrame(20, 48000))
	assert.Equal(t, 1920, SamplesPerFrame(40, 48000))
	assert.Equal(t, 2880, SamplesPerFrame(60, 48000))

	// Different sample rates.
	assert.Equal(t, 160, SamplesPerFrame(20, 8000))
	assert.Equal(t, 320, SamplesPerFrame(20, 16000))
}

func TestMaxFrameSize(t *testing.T) {
	assert.Equal(t, 5760, MaxFrameSize(48000))
	assert.Equal(t, 960, MaxFrameSize(8000))
}

func TestDecoderCreate(t *testing.T) {
	dec, err := NewDecoder(48000, 1)
	require.NoError(t, err)
	assert.Equal(t, 48000, dec.SampleRate())
	assert.Equal(t, 1, dec.Channels())
}

func TestDecoderCELTSilence(t *testing.T) {
	// Create a minimal CELT-only packet (code 0, 20ms, mono, fullband).
	// TOC: 0xFC = 1111_1100 = CELT (bit7), FB (bits5-6=11), 20ms (bits3-4=11), mono (bit2=0), code0 (bits0-1=00)
	// Followed by a minimal payload.
	dec, err := NewDecoder(48000, 1)
	require.NoError(t, err)

	// Even with a corrupted/minimal packet, the decoder should not panic.
	// A real test would use reference-encoded packets.
	toc := byte(0xFC) // CELT, fullband, 20ms, mono, code 0
	pkt := make([]byte, 10)
	pkt[0] = toc

	pcm := make([]float64, MaxFrameSize(48000))
	// This may return an error due to corrupted data, but must not panic.
	_, _ = dec.Decode(pkt, pcm)
}

func TestDecoderPLC(t *testing.T) {
	dec, err := NewDecoder(48000, 1)
	require.NoError(t, err)
	pcm := make([]float64, 960)
	n, err := dec.Decode(nil, pcm)
	require.NoError(t, err)
	assert.Equal(t, 960, n) // PLC outputs silence for the default frame duration
	// Verify all samples are zero (silence PLC).
	for i := 0; i < n; i++ {
		assert.Equal(t, 0.0, pcm[i])
	}
}

func TestDecoderHybridMinimal(t *testing.T) {
	dec, err := NewDecoder(48000, 1)
	require.NoError(t, err)
	// Hybrid packet: TOC = 0x60 (Hybrid, SWB, 10ms, mono, code 0)
	pkt := make([]byte, 30)
	pkt[0] = 0x60
	pcm := make([]float64, MaxFrameSize(48000))
	// May return error on corrupt data, must not panic.
	_, _ = dec.Decode(pkt, pcm)
}

func TestDecoderSILKMinimal(t *testing.T) {
	dec, err := NewDecoder(48000, 1)
	require.NoError(t, err)
	// SILK packet: TOC = 0x08 (SILK, NB, 20ms, mono, code 0)
	// With a minimal payload. The decoder should not panic on corrupt data.
	pkt := make([]byte, 20)
	pkt[0] = 0x08
	pcm := make([]float64, 960)
	// May return an error due to corrupted data, but must not panic.
	_, _ = dec.Decode(pkt, pcm)
}
