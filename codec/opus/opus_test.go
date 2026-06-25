package opus

import (
	"io"
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/codec"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	opuslib "github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asAudio is a test helper that wraps raw PCM in a mutations.Audio
// using the encoder's declared format — avoids repeating the
// SampleRate/Channels dance at every call site.
func asAudio(enc codec.Encoder, data []float64) mutations.Audio {
	return mutations.Audio{Data: data, SampleRate: enc.SampleRate(), Channels: enc.Channels()}
}

// collectPacketWriter collects packets into a slice.
type collectPacketWriter struct {
	packets [][]byte
}

func (w *collectPacketWriter) WritePacket(data []byte) error {
	pkt := make([]byte, len(data))
	copy(pkt, data)
	w.packets = append(w.packets, pkt)
	return nil
}

// sineFrame generates a mono sine wave frame.
func sineFrame(samples int, freq, sampleRate float64) []float64 {
	pcm := make([]float64, samples)
	for i := range pcm {
		pcm[i] = 0.5 * math.Sin(2*math.Pi*freq*float64(i)/sampleRate)
	}
	return pcm
}

func TestDecoderRoundTrip(t *testing.T) {
	// Encode a sine wave with the library encoder, then decode through the codec decoder.
	libEnc, err := opuslib.NewEncoder(consts.SampleRate48000, 1)
	require.NoError(t, err)

	pcm := sineFrame(960, 440, consts.SampleRate48000) // 20ms frame

	pkt, err := libEnc.Encode(pcm, 1275)
	require.NoError(t, err)

	pr := NewSlicePacketReader([][]byte{pkt})
	dec, err := NewDecoder(pr, consts.SampleRate48000, 1)
	require.NoError(t, err)

	assert.Equal(t, consts.SampleRate48000, dec.SampleRate())
	assert.Equal(t, 1, dec.Channels())

	buf := make([]float64, 960)
	got, err := dec.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 960, len(got.Data))

	// Lossy codec — just verify output is non-silent and has reasonable amplitude.
	var maxAmp float64
	for _, v := range got.Data {
		if a := math.Abs(v); a > maxAmp {
			maxAmp = a
		}
	}
	assert.Greater(t, maxAmp, 0.1, "decoded audio should not be silent")
}

func TestDecoderMultiplePackets(t *testing.T) {
	libEnc, err := opuslib.NewEncoder(consts.SampleRate48000, 1)
	require.NoError(t, err)

	var packets [][]byte
	for i := 0; i < 5; i++ {
		pcm := sineFrame(960, 440, consts.SampleRate48000)
		pkt, err := libEnc.Encode(pcm, 1275)
		require.NoError(t, err)
		packets = append(packets, pkt)
	}

	pr := NewSlicePacketReader(packets)
	dec, err := NewDecoder(pr, consts.SampleRate48000, 1)
	require.NoError(t, err)

	// Read all 5 frames in one go.
	buf := make([]float64, 960*5)
	total := 0
	for total < len(buf) {
		got, err := dec.Read(buf[total:])
		total += len(got.Data)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	assert.Equal(t, 960*5, total)
}

func TestDecoderPartialRead(t *testing.T) {
	// Decode a 960-sample frame but only read 100 at a time.
	libEnc, err := opuslib.NewEncoder(consts.SampleRate48000, 1)
	require.NoError(t, err)

	pcm := sineFrame(960, 440, consts.SampleRate48000)
	pkt, err := libEnc.Encode(pcm, 1275)
	require.NoError(t, err)

	pr := NewSlicePacketReader([][]byte{pkt})
	dec, err := NewDecoder(pr, consts.SampleRate48000, 1)
	require.NoError(t, err)

	collected := make([]float64, 0, 960)
	buf := make([]float64, 100)
	for {
		got, err := dec.Read(buf)
		collected = append(collected, got.Data...)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	assert.Equal(t, 960, len(collected))
}

func TestDecoderEOF(t *testing.T) {
	pr := NewSlicePacketReader(nil)
	dec, err := NewDecoder(pr, consts.SampleRate48000, 1)
	require.NoError(t, err)

	buf := make([]float64, 960)
	got, err := dec.Read(buf)
	assert.Equal(t, 0, len(got.Data))
	assert.ErrorIs(t, err, io.EOF)
}

func TestDecoderEmptyBuf(t *testing.T) {
	pr := NewSlicePacketReader(nil)
	dec, err := NewDecoder(pr, consts.SampleRate48000, 1)
	require.NoError(t, err)

	got, err := dec.Read(nil)
	assert.Equal(t, 0, len(got.Data))
	assert.NoError(t, err)
}

func TestDecoderBadArgs(t *testing.T) {
	pr := NewSlicePacketReader(nil)

	_, err := NewDecoder(pr, consts.SampleRate44100, 1) // unsupported rate
	assert.ErrorIs(t, err, ErrBadSampleRate)

	_, err = NewDecoder(pr, consts.SampleRate48000, 3) // unsupported channels
	assert.ErrorIs(t, err, ErrBadChannels)
}

func TestEncoderWrite(t *testing.T) {
	pw := &collectPacketWriter{}
	enc, err := NewEncoder(pw, consts.SampleRate48000, 1)
	require.NoError(t, err)

	assert.Equal(t, consts.SampleRate48000, enc.SampleRate())
	assert.Equal(t, 1, enc.Channels())

	// Write exactly one 20ms frame (960 samples at 48kHz).
	pcm := sineFrame(960, 440, consts.SampleRate48000)
	n, err := enc.Write(asAudio(enc, pcm))
	require.NoError(t, err)
	assert.Equal(t, 960, n)
	assert.Equal(t, 1, len(pw.packets), "should have produced 1 packet")
	assert.Greater(t, len(pw.packets[0]), 0)
}

func TestEncoderMultipleFrames(t *testing.T) {
	pw := &collectPacketWriter{}
	enc, err := NewEncoder(pw, consts.SampleRate48000, 1)
	require.NoError(t, err)

	// Write 3 frames worth in one call.
	pcm := sineFrame(960*3, 440, consts.SampleRate48000)
	n, err := enc.Write(asAudio(enc, pcm))
	require.NoError(t, err)
	assert.Equal(t, 960*3, n)
	assert.Equal(t, 3, len(pw.packets))
}

func TestEncoderPartialFrames(t *testing.T) {
	pw := &collectPacketWriter{}
	enc, err := NewEncoder(pw, consts.SampleRate48000, 1)
	require.NoError(t, err)

	// Write 500 samples — less than one frame.
	pcm := sineFrame(500, 440, consts.SampleRate48000)
	n, err := enc.Write(asAudio(enc, pcm))
	require.NoError(t, err)
	assert.Equal(t, 500, n)
	assert.Equal(t, 0, len(pw.packets), "partial frame should not emit a packet")

	// Write 460 more to complete the frame.
	pcm2 := sineFrame(460, 440, consts.SampleRate48000)
	n, err = enc.Write(asAudio(enc, pcm2))
	require.NoError(t, err)
	assert.Equal(t, 460, n)
	assert.Equal(t, 1, len(pw.packets), "completing the frame should emit a packet")
}

func TestEncoderClose(t *testing.T) {
	pw := &collectPacketWriter{}
	enc, err := NewEncoder(pw, consts.SampleRate48000, 1)
	require.NoError(t, err)

	// Write a partial frame.
	pcm := sineFrame(500, 440, consts.SampleRate48000)
	enc.Write(asAudio(enc, pcm))
	assert.Equal(t, 0, len(pw.packets))

	// Close should pad and flush.
	err = enc.Close()
	require.NoError(t, err)
	assert.Equal(t, 1, len(pw.packets), "Close should flush partial frame")
}

func TestEncoderCloseEmpty(t *testing.T) {
	pw := &collectPacketWriter{}
	enc, err := NewEncoder(pw, consts.SampleRate48000, 1)
	require.NoError(t, err)

	err = enc.Close()
	require.NoError(t, err)
	assert.Equal(t, 0, len(pw.packets), "Close with no pending data should not emit")
}

func TestEncoderBadArgs(t *testing.T) {
	pw := &collectPacketWriter{}

	_, err := NewEncoder(pw, consts.SampleRate44100, 1)
	assert.ErrorIs(t, err, ErrBadSampleRate)

	_, err = NewEncoder(pw, consts.SampleRate48000, 3)
	assert.ErrorIs(t, err, ErrBadChannels)
}

func TestEncoderDecoderRoundTrip(t *testing.T) {
	// Full round trip through the codec layer.
	pw := &collectPacketWriter{}
	enc, err := NewEncoder(pw, consts.SampleRate48000, 1)
	require.NoError(t, err)

	// Write 3 frames.
	pcm := sineFrame(960*3, 440, consts.SampleRate48000)
	_, err = enc.Write(asAudio(enc, pcm))
	require.NoError(t, err)
	require.NoError(t, enc.Close())
	require.Equal(t, 3, len(pw.packets))

	// Decode.
	pr := NewSlicePacketReader(pw.packets)
	dec, err := NewDecoder(pr, consts.SampleRate48000, 1)
	require.NoError(t, err)

	buf := make([]float64, 960*3)
	got, err := codec.ReadFull(dec, buf)
	assert.Equal(t, 960*3, len(got.Data))

	// Verify non-silent.
	var maxAmp float64
	for _, v := range buf {
		if a := math.Abs(v); a > maxAmp {
			maxAmp = a
		}
	}
	assert.Greater(t, maxAmp, 0.1)
}

func TestPacketReaderFunc(t *testing.T) {
	calls := 0
	pr := PacketReaderFunc(func() ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, io.EOF
		}
		return []byte{1, 2, 3}, nil
	})

	pkt, err := pr.ReadPacket()
	assert.NoError(t, err)
	assert.Equal(t, []byte{1, 2, 3}, pkt)

	_, err = pr.ReadPacket()
	assert.ErrorIs(t, err, io.EOF)
}

func TestPacketWriterFunc(t *testing.T) {
	var received []byte
	pw := PacketWriterFunc(func(data []byte) error {
		received = data
		return nil
	})

	err := pw.WritePacket([]byte{4, 5, 6})
	assert.NoError(t, err)
	assert.Equal(t, []byte{4, 5, 6}, received)
}

func TestEncoderWithOptions(t *testing.T) {
	pw := &collectPacketWriter{}
	enc, err := NewEncoder(pw, consts.SampleRate48000, 1,
		WithBitrate(128000),
		WithComplexity(5),
		WithApplication(opuslib.AppVoIP),
		WithFrameDuration(10),
	)
	require.NoError(t, err)

	// 10ms frame at 48kHz = 480 samples.
	pcm := sineFrame(480, 440, consts.SampleRate48000)
	n, err := enc.Write(asAudio(enc, pcm))
	require.NoError(t, err)
	assert.Equal(t, 480, n)
	assert.Equal(t, 1, len(pw.packets))
}

func TestDecoderStereo(t *testing.T) {
	libEnc, err := opuslib.NewEncoder(consts.SampleRate48000, 2)
	require.NoError(t, err)

	// Stereo 20ms frame: 960 samples per channel * 2 channels = 1920 float64s.
	pcm := make([]float64, 1920)
	for i := 0; i < 960; i++ {
		v := 0.5 * math.Sin(2*math.Pi*440*float64(i)/consts.SampleRate48000)
		pcm[i*2] = v   // left
		pcm[i*2+1] = v // right
	}

	pkt, err := libEnc.Encode(pcm, 1275)
	require.NoError(t, err)

	pr := NewSlicePacketReader([][]byte{pkt})
	dec, err := NewDecoder(pr, consts.SampleRate48000, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, dec.Channels())

	buf := make([]float64, 1920)
	got, err := dec.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 1920, len(got.Data))
}
