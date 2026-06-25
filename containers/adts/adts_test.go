package adts

import (
	"bytes"
	"io"
	"testing"

	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeFrame builds a single ADTS frame: a header for payloadLen-byte payload
// (filled with a recognisable ramp) and returns the whole frame.
func makeFrame(t *testing.T, h FrameHeader, payloadLen int) []byte {
	t.Helper()
	payload := make([]byte, payloadLen)
	for i := range payload {
		payload[i] = byte(i)
	}
	var hdr [HeaderLenCRC]byte
	n, err := EncodeHeader(hdr[:], h, payloadLen)
	require.NoError(t, err)
	frame := append([]byte{}, hdr[:n]...)
	if h.CRCPresent {
		crc := crc16ADTS(hdr[:HeaderLen], payload)
		frame[7] = byte(crc >> 8)
		frame[8] = byte(crc)
	}
	return append(frame, payload...)
}

func TestParseKnownHeaderVectors(t *testing.T) {
	tests := []struct {
		name             string
		bytes            []byte
		wantProfile      int
		wantSFIndex      int
		wantChanConfig   int
		wantMPEG         int
		wantCRC          bool
		wantFrameLen     int
		wantSampleRate   int
		wantChannels     int
		wantObjectType   aaclib.AudioObjectType
		wantHeaderSize   int
		wantRawDataBlock int
	}{
		{
			// 44.1 kHz stereo AAC-LC, MPEG-4, no CRC, frame_length 7 (header
			// only). Canonical vector: FF F1 50 80 00 1F FC (fullness 0x7FF).
			name:             "44k1-stereo-aaclc",
			bytes:            []byte{0xFF, 0xF1, 0x50, 0x80, 0x00, 0xFF, 0xFC},
			wantProfile:      1,
			wantSFIndex:      4,
			wantChanConfig:   2,
			wantMPEG:         0,
			wantCRC:          false,
			wantFrameLen:     7,
			wantSampleRate:   44100,
			wantChannels:     2,
			wantObjectType:   aaclib.AOTAACLC,
			wantHeaderSize:   7,
			wantRawDataBlock: 1,
		},
		{
			// 48 kHz mono AAC-LC, MPEG-4, no CRC: profile 01, sfIndex 0011,
			// chanConfig 1. Byte2 = 01 0011 0 0 = 0x4C; byte3 = 01 ... = 0x40.
			name:             "48k-mono-aaclc",
			bytes:            []byte{0xFF, 0xF1, 0x4C, 0x40, 0x00, 0xFF, 0xFC},
			wantProfile:      1,
			wantSFIndex:      3,
			wantChanConfig:   1,
			wantMPEG:         0,
			wantCRC:          false,
			wantFrameLen:     7,
			wantSampleRate:   48000,
			wantChannels:     1,
			wantObjectType:   aaclib.AOTAACLC,
			wantHeaderSize:   7,
			wantRawDataBlock: 1,
		},
		{
			// 44.1 kHz stereo AAC-LC with CRC (protection_absent=0): byte1 low
			// bit cleared → 0xF0; header is 9 bytes; frame_length 9.
			name:             "44k1-stereo-crc",
			bytes:            []byte{0xFF, 0xF0, 0x50, 0x80, 0x01, 0x3F, 0xFC},
			wantProfile:      1,
			wantSFIndex:      4,
			wantChanConfig:   2,
			wantMPEG:         0,
			wantCRC:          true,
			wantFrameLen:     9,
			wantSampleRate:   44100,
			wantChannels:     2,
			wantObjectType:   aaclib.AOTAACLC,
			wantHeaderSize:   9,
			wantRawDataBlock: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, err := ParseHeader(tc.bytes)
			require.NoError(t, err)
			assert.Equal(t, tc.wantProfile, h.Profile)
			assert.Equal(t, tc.wantSFIndex, h.SampleRateIndex)
			assert.Equal(t, tc.wantChanConfig, h.ChannelConfiguration)
			assert.Equal(t, tc.wantMPEG, h.MPEGVersion)
			assert.Equal(t, tc.wantCRC, h.CRCPresent)
			assert.Equal(t, tc.wantFrameLen, h.FrameLength)
			assert.Equal(t, tc.wantSampleRate, h.SampleRate())
			assert.Equal(t, tc.wantChannels, h.Channels())
			assert.Equal(t, tc.wantObjectType, h.ObjectType())
			assert.Equal(t, tc.wantHeaderSize, h.HeaderSize())
			assert.Equal(t, tc.wantRawDataBlock, h.RawDataBlocks)
		})
	}
}

func TestParseHeaderErrors(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"too-short", []byte{0xFF, 0xF1, 0x50}, ErrShortHeader},
		{"bad-sync-byte0", []byte{0xFE, 0xF1, 0x50, 0x80, 0x00, 0x1F, 0xFC}, ErrBadSyncword},
		{"bad-sync-nibble", []byte{0xFF, 0xE1, 0x50, 0x80, 0x00, 0x1F, 0xFC}, ErrBadSyncword},
		{"frame-shorter-than-header", []byte{0xFF, 0xF1, 0x50, 0x80, 0x00, 0x0F, 0xFC}, ErrBadFrameLength},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseHeader(tc.in)
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestEncodeParseRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		objectType aaclib.AudioObjectType
		sampleRate int
		channels   int
		crc        bool
		mpeg       int
		payloadLen int
	}{
		{"aaclc-44k1-stereo", aaclib.AOTAACLC, 44100, 2, false, 0, 380},
		{"aaclc-48k-mono", aaclib.AOTAACLC, 48000, 1, false, 0, 200},
		{"aaclc-32k-5ch-crc", aaclib.AOTAACLC, 32000, 5, true, 0, 512},
		{"aaclc-8k-mono-mpeg2", aaclib.AOTAACLC, 8000, 1, false, 1, 64},
		{"aaclc-96k-stereo-max", aaclib.AOTAACLC, 96000, 2, false, 0, 8000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sfIndex, ok := sampleRateIndex(tc.sampleRate)
			require.True(t, ok)
			chanConfig, ok := channelConfigIndex(tc.channels)
			require.True(t, ok)

			in := FrameHeader{
				MPEGVersion:          tc.mpeg,
				Profile:              int(tc.objectType) - 1,
				SampleRateIndex:      sfIndex,
				ChannelConfiguration: chanConfig,
				CRCPresent:           tc.crc,
				RawDataBlocks:        1,
			}
			var dst [HeaderLenCRC]byte
			n, err := EncodeHeader(dst[:], in, tc.payloadLen)
			require.NoError(t, err)
			assert.Equal(t, in.HeaderSize(), n)

			out, err := ParseHeader(dst[:n])
			require.NoError(t, err)
			assert.Equal(t, tc.mpeg, out.MPEGVersion)
			assert.Equal(t, in.Profile, out.Profile)
			assert.Equal(t, sfIndex, out.SampleRateIndex)
			assert.Equal(t, chanConfig, out.ChannelConfiguration)
			assert.Equal(t, tc.crc, out.CRCPresent)
			assert.Equal(t, n+tc.payloadLen, out.FrameLength)
			assert.Equal(t, tc.sampleRate, out.SampleRate())
			assert.Equal(t, tc.channels, out.Channels())
			assert.Equal(t, tc.objectType, out.ObjectType())
		})
	}
}

func TestEncodeHeaderErrors(t *testing.T) {
	h := FrameHeader{Profile: 1, SampleRateIndex: 4, ChannelConfiguration: 2}
	t.Run("dst-too-small", func(t *testing.T) {
		var dst [5]byte
		_, err := EncodeHeader(dst[:], h, 100)
		assert.ErrorIs(t, err, ErrShortHeader)
	})
	t.Run("frame-too-large", func(t *testing.T) {
		var dst [HeaderLenCRC]byte
		_, err := EncodeHeader(dst[:], h, MaxFrameLen)
		assert.ErrorIs(t, err, ErrBadFrameLength)
	})
}

func TestAudioSpecificConfigProjection(t *testing.T) {
	// 44.1k stereo AAC-LC → ASC 0x12 0x10 (AOT 2, sfIndex 4, chanConfig 2):
	// 00010 0100 0010 000 = 0001 0010 0001 0000.
	h, err := ParseHeader([]byte{0xFF, 0xF1, 0x50, 0x80, 0x00, 0xFF, 0xFC})
	require.NoError(t, err)
	asc := h.AudioSpecificConfig()
	assert.Equal(t, aaclib.AOTAACLC, asc.ObjectType)
	assert.Equal(t, 44100, asc.SampleRate)
	assert.Equal(t, 2, asc.Channels)
	assert.Equal(t, aaclib.FrameSamplesShort, asc.FrameSamples)
	assert.Equal(t, []byte{0x12, 0x10}, asc.Raw)
}

func TestReaderSingleFrame(t *testing.T) {
	h := FrameHeader{Profile: 1, SampleRateIndex: 4, ChannelConfiguration: 2, RawDataBlocks: 1}
	frame := makeFrame(t, h, 100)

	rd, err := NewReader(bytes.NewReader(frame))
	require.NoError(t, err)

	hdr := rd.Header()
	assert.Equal(t, "adts", hdr.Format)
	assert.Equal(t, 44100, hdr.SampleRate)
	assert.Equal(t, 2, hdr.Channels)
	assert.Equal(t, aaclib.AOTAACLC, hdr.Extra.Config.ObjectType)
	assert.False(t, hdr.Extra.CRCPresent)

	au, err := rd.ReadPacket()
	require.NoError(t, err)
	assert.Len(t, au, 100)
	assert.Equal(t, byte(0), au[0])
	assert.Equal(t, byte(99), au[99])

	_, err = rd.ReadPacket()
	assert.ErrorIs(t, err, io.EOF)

	assert.Equal(t, 1, rd.Header().Extra.Frames)
}

func TestReaderMultiFrame(t *testing.T) {
	h := FrameHeader{Profile: 1, SampleRateIndex: 4, ChannelConfiguration: 2, RawDataBlocks: 1}
	var stream []byte
	lengths := []int{50, 120, 7, 300}
	for _, n := range lengths {
		stream = append(stream, makeFrame(t, h, n)...)
	}

	rd, err := NewReader(bytes.NewReader(stream))
	require.NoError(t, err)

	for i, want := range lengths {
		au, err := rd.ReadPacket()
		require.NoError(t, err, "frame %d", i)
		assert.Len(t, au, want, "frame %d", i)
	}
	_, err = rd.ReadPacket()
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, len(lengths), rd.Header().Extra.Frames)
}

func TestReaderResyncOnGarbage(t *testing.T) {
	h := FrameHeader{Profile: 1, SampleRateIndex: 4, ChannelConfiguration: 2, RawDataBlocks: 1}
	f1 := makeFrame(t, h, 80)
	f2 := makeFrame(t, h, 90)

	// Leading garbage, valid frame, garbage between frames, valid frame.
	garbage := []byte{0x00, 0x12, 0xAB, 0xFF, 0x00, 0xCD}
	var stream []byte
	stream = append(stream, garbage...)
	stream = append(stream, f1...)
	stream = append(stream, 0x55, 0x66, 0x77) // inter-frame garbage
	stream = append(stream, f2...)

	rd, err := NewReader(bytes.NewReader(stream))
	require.NoError(t, err)

	au, err := rd.ReadPacket()
	require.NoError(t, err)
	assert.Len(t, au, 80)

	au, err = rd.ReadPacket()
	require.NoError(t, err)
	assert.Len(t, au, 90)

	_, err = rd.ReadPacket()
	assert.ErrorIs(t, err, io.EOF)
}

func TestReaderNoSync(t *testing.T) {
	// All non-syncword bytes.
	stream := bytes.Repeat([]byte{0x00, 0x11, 0x22}, 100)
	_, err := NewReader(bytes.NewReader(stream))
	assert.ErrorIs(t, err, ErrNoSync)
}

func TestReaderEmpty(t *testing.T) {
	_, err := NewReader(bytes.NewReader(nil))
	assert.ErrorIs(t, err, io.EOF)
}

func TestReaderNilArg(t *testing.T) {
	_, err := NewReader(nil)
	assert.ErrorIs(t, err, ErrBadArg)
}

func TestReaderCRCFrame(t *testing.T) {
	h := FrameHeader{Profile: 1, SampleRateIndex: 4, ChannelConfiguration: 2, CRCPresent: true, RawDataBlocks: 1}
	frame := makeFrame(t, h, 64)
	require.Equal(t, HeaderLenCRC+64, len(frame))

	rd, err := NewReader(bytes.NewReader(frame))
	require.NoError(t, err)
	assert.True(t, rd.Header().Extra.CRCPresent)

	au, err := rd.ReadPacket()
	require.NoError(t, err)
	// CRC bytes are stripped along with the header — payload only.
	assert.Len(t, au, 64)
	assert.Equal(t, byte(0), au[0])
}

func TestWriterReaderRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate int
		channels   int
		crc        bool
		opts       []WriterOption
	}{
		{"44k1-stereo", 44100, 2, false, nil},
		{"48k-mono", 48000, 1, false, nil},
		{"44k1-stereo-crc", 44100, 2, true, []WriterOption{WithCRC(true)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(&buf, tc.sampleRate, tc.channels, tc.opts...)
			require.NoError(t, err)

			aus := [][]byte{
				bytes.Repeat([]byte{0xAA}, 100),
				bytes.Repeat([]byte{0xBB}, 200),
				bytes.Repeat([]byte{0xCC}, 7),
			}
			for _, au := range aus {
				require.NoError(t, w.WritePacket(au))
			}
			assert.Equal(t, len(aus), w.Frames())

			rd, err := NewReader(bytes.NewReader(buf.Bytes()))
			require.NoError(t, err)
			assert.Equal(t, tc.sampleRate, rd.Header().SampleRate)
			assert.Equal(t, tc.channels, rd.Header().Channels)
			assert.Equal(t, tc.crc, rd.Header().Extra.CRCPresent)

			got, err := rd.AccessUnits()
			require.NoError(t, err)
			require.Len(t, got, len(aus))
			for i := range aus {
				assert.Equal(t, aus[i], got[i], "au %d", i)
			}

			// Writer ASC and Reader ASC must agree.
			assert.Equal(t, w.ASC(), rd.ASC())
		})
	}
}

func TestWriterEmitsKnownHeaderBytes(t *testing.T) {
	// A 44.1k stereo AAC-LC writer with a zero-length payload (degenerate but
	// header-exercising) must emit the canonical fixed bytes FF F1 50 80 ...
	var buf bytes.Buffer
	w, err := NewWriter(&buf, 44100, 2)
	require.NoError(t, err)
	require.NoError(t, w.WritePacket(bytes.Repeat([]byte{0x01}, 1)))

	got := buf.Bytes()
	require.GreaterOrEqual(t, len(got), HeaderLen)
	assert.Equal(t, byte(0xFF), got[0])
	assert.Equal(t, byte(0xF1), got[1])
	assert.Equal(t, byte(0x50), got[2])
	assert.Equal(t, byte(0x80), got[3]&0xC0|got[3]&0x80) // chanConfig hi/lo region
	// frame_length = 8 (7 header + 1 payload): bits across byte3 lo2|byte4|byte5 hi3.
	h, err := ParseHeader(got)
	require.NoError(t, err)
	assert.Equal(t, 8, h.FrameLength)
}

func TestWriterUnsupportedParams(t *testing.T) {
	var buf bytes.Buffer
	t.Run("bad-rate", func(t *testing.T) {
		_, err := NewWriter(&buf, 44101, 2)
		assert.ErrorIs(t, err, ErrUnsupportedSampleRate)
	})
	t.Run("bad-channels", func(t *testing.T) {
		_, err := NewWriter(&buf, 44100, 7)
		assert.ErrorIs(t, err, ErrUnsupportedChannels)
	})
	t.Run("nil-writer", func(t *testing.T) {
		_, err := NewWriter(nil, 44100, 2)
		assert.ErrorIs(t, err, ErrBadArg)
	})
}

func TestWriterPacketTooLarge(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, 44100, 2)
	require.NoError(t, err)
	err = w.WritePacket(bytes.Repeat([]byte{0}, MaxFrameLen))
	assert.ErrorIs(t, err, ErrPacketTooLarge)
}

func TestWriterObjectTypeAndMPEGVersion(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, 44100, 2,
		WithObjectType(aaclib.AOTAACLTP), WithMPEGVersion(1))
	require.NoError(t, err)
	require.NoError(t, w.WritePacket([]byte{0x00}))

	h, err := ParseHeader(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, 1, h.MPEGVersion)
	assert.Equal(t, aaclib.AOTAACLTP, h.ObjectType())
	assert.Equal(t, int(aaclib.AOTAACLTP)-1, h.Profile)
}

func TestCRCDeterministic(t *testing.T) {
	// crc16ADTS over a fixed input is stable and order-sensitive across parts.
	a := crc16ADTS([]byte{0xFF, 0xF0, 0x50, 0x80, 0x01, 0x3F, 0xFC}, []byte{0x01, 0x02, 0x03})
	b := crc16ADTS([]byte{0xFF, 0xF0, 0x50, 0x80, 0x01, 0x3F, 0xFC, 0x01, 0x02, 0x03})
	assert.Equal(t, b, a, "concatenated parts equal single run")
}
