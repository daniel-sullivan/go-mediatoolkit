package aac

import (
	"encoding/binary"
	"io"
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/codec"
	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The codec/aac adapter is a thin streaming bridge over a
// [libraries/aac.Decoder]/[libraries/aac.Encoder]: it does the
// StreamChunker framing on encode and the remainder buffering on decode,
// and forwards options. These tests exercise that adapter contract with an
// in-memory packet source. Because libraries/aac is a 1:1 C port whose
// pure-Go backend is not yet wired (Decode/Encode return
// [aaclib.ErrUnimplemented]), the round-trip is driven through fake
// codec backends that the unexported decoder/encoder structs accept —
// fakeEncoder/fakeDecoder implement the [aaclib.Encoder]/[aaclib.Decoder]
// interfaces with a trivial reversible "codec" (interleaved float64 <->
// little-endian float64 packet). This makes the round-trip assert the
// adapter's framing/remainder/format logic deterministically, independent
// of the C port's progress. Tests that touch only construction, options
// forwarding, EOF, and the packet adapters go through the real public
// constructors.

const (
	testRate  = 48000
	testFrame = aaclib.FrameSamplesShort // 1024 samples/channel
)

// fakeDecoder is a trivial reversible [aaclib.Decoder]: each packet is a
// little-endian stream of float64 samples that it copies straight into pcm.
// One packet decodes to exactly testFrame samples per channel.
type fakeDecoder struct {
	asc        aaclib.AudioSpecificConfig
	sampleRate int
	channels   int
	frame      int
}

func (d *fakeDecoder) Decode(pkt []byte, pcm []float64) (int, error) {
	total := d.frame * d.channels
	if len(pcm) < total {
		return 0, aaclib.ErrBufferTooSmall
	}
	if len(pkt) != total*8 {
		return 0, aaclib.ErrInvalidPacket
	}
	for i := 0; i < total; i++ {
		bits := binary.LittleEndian.Uint64(pkt[i*8:])
		pcm[i] = math.Float64frombits(bits)
	}
	return d.frame, nil
}

func (d *fakeDecoder) SampleRate() int                    { return d.sampleRate }
func (d *fakeDecoder) Channels() int                      { return d.channels }
func (d *fakeDecoder) Config() aaclib.AudioSpecificConfig { return d.asc }
func (d *fakeDecoder) Reset()                             {}

// fakeEncoder is the inverse: it serialises one frame of interleaved
// float64 PCM into a little-endian byte packet that fakeDecoder reverses.
type fakeEncoder struct {
	sampleRate int
	channels   int
	frame      int
}

func (e *fakeEncoder) Encode(pcm []float64) ([]byte, error) {
	total := e.frame * e.channels
	if len(pcm) != total {
		return nil, aaclib.ErrBadArg
	}
	pkt := make([]byte, total*8)
	for i := 0; i < total; i++ {
		binary.LittleEndian.PutUint64(pkt[i*8:], math.Float64bits(pcm[i]))
	}
	return pkt, nil
}

func (e *fakeEncoder) Config() aaclib.AudioSpecificConfig {
	return aaclib.AudioSpecificConfig{
		ObjectType:   aaclib.AOTAACLC,
		SampleRate:   e.sampleRate,
		Channels:     e.channels,
		FrameSamples: e.frame,
	}
}

func (e *fakeEncoder) SampleRate() int { return e.sampleRate }
func (e *fakeEncoder) Channels() int   { return e.channels }
func (e *fakeEncoder) Reset()          {}

// newFakeEncoder builds a codec.Encoder over fakeEncoder, wiring the same
// StreamChunker/PacketWriter plumbing the public NewEncoder does.
func newFakeEncoder(pw PacketWriter, channels int) *encoder {
	return &encoder{
		pw:         pw,
		channels:   channels,
		sampleRate: testRate,
		enc:        &fakeEncoder{sampleRate: testRate, channels: channels, frame: testFrame},
		chunker:    mutations.NewStreamChunker(testFrame * channels),
	}
}

// newFakeDecoder builds a codec.Decoder over fakeDecoder, wiring the same
// frame/remainder buffering the public NewDecoder does.
func newFakeDecoder(pr PacketReader, channels int) *decoder {
	max := testFrame * channels
	return &decoder{
		pr:         pr,
		channels:   channels,
		sampleRate: testRate,
		dec:        &fakeDecoder{asc: aaclib.AudioSpecificConfig{SampleRate: testRate, Channels: channels, FrameSamples: testFrame}, sampleRate: testRate, channels: channels, frame: testFrame},
		frameBuf:   make([]float64, max),
		remainder:  make([]float64, max),
	}
}

// collectPacketWriter collects packets into a slice (copying each).
type collectPacketWriter struct {
	packets [][]byte
}

func (w *collectPacketWriter) WritePacket(data []byte) error {
	pkt := make([]byte, len(data))
	copy(pkt, data)
	w.packets = append(w.packets, pkt)
	return nil
}

// asAudio wraps PCM in a mutations.Audio matching enc's declared format.
func asAudio(enc codec.Encoder, data []float64) mutations.Audio {
	return mutations.Audio{Data: data, SampleRate: enc.SampleRate(), Channels: enc.Channels()}
}

// ramp produces n deterministic, distinct samples in [-1, 1].
func ramp(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.Sin(float64(i) * 0.01)
	}
	return out
}

func TestEncoderDecoderRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		channels int
		frames   int
		readSize int // samples per Read call on the decode side
	}{
		{name: "mono one frame, full read", channels: 1, frames: 1, readSize: testFrame},
		{name: "mono three frames, full read", channels: 1, frames: 3, readSize: testFrame * 3},
		{name: "mono three frames, partial reads", channels: 1, frames: 3, readSize: 100},
		{name: "stereo two frames, full read", channels: 2, frames: 2, readSize: testFrame * 2 * 2},
		{name: "stereo four frames, odd read size", channels: 2, frames: 4, readSize: 333},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			total := testFrame * tc.channels * tc.frames
			pcm := ramp(total)

			// Encode through the adapter (StreamChunker frames into packets).
			pw := &collectPacketWriter{}
			enc := newFakeEncoder(pw, tc.channels)
			assert.Equal(t, testRate, enc.SampleRate())
			assert.Equal(t, tc.channels, enc.Channels())

			n, err := enc.Write(asAudio(enc, pcm))
			require.NoError(t, err)
			assert.Equal(t, total, n)
			require.NoError(t, enc.Close())
			require.Equal(t, tc.frames, len(pw.packets), "one packet per full frame")

			// Decode through the adapter (remainder buffer across Reads).
			pr := NewSlicePacketReader(pw.packets)
			dec := newFakeDecoder(pr, tc.channels)
			assert.Equal(t, testRate, dec.SampleRate())
			assert.Equal(t, tc.channels, dec.Channels())

			got := make([]float64, 0, total)
			buf := make([]float64, tc.readSize)
			for {
				audio, err := dec.Read(buf)
				got = append(got, audio.Data...)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
			}

			require.Equal(t, total, len(got))
			assert.Equal(t, pcm, got, "round-tripped samples must be bit-identical")
		})
	}
}

func TestEncoderPartialFrameBuffering(t *testing.T) {
	pw := &collectPacketWriter{}
	enc := newFakeEncoder(pw, 1)

	// Less than one frame: nothing emitted yet.
	half := ramp(testFrame / 2)
	n, err := enc.Write(asAudio(enc, half))
	require.NoError(t, err)
	assert.Equal(t, len(half), n)
	assert.Empty(t, pw.packets, "partial frame must not emit a packet")

	// Complete the frame: one packet emitted.
	rest := ramp(testFrame - len(half))
	n, err = enc.Write(asAudio(enc, rest))
	require.NoError(t, err)
	assert.Equal(t, len(rest), n)
	assert.Len(t, pw.packets, 1, "completing a frame emits exactly one packet")
}

func TestEncoderCloseFlushesPadded(t *testing.T) {
	pw := &collectPacketWriter{}
	enc := newFakeEncoder(pw, 1)

	enc.Write(asAudio(enc, ramp(testFrame/2)))
	require.Empty(t, pw.packets)

	require.NoError(t, enc.Close())
	assert.Len(t, pw.packets, 1, "Close pads and flushes the partial frame")
}

func TestEncoderCloseEmpty(t *testing.T) {
	pw := &collectPacketWriter{}
	enc := newFakeEncoder(pw, 1)

	require.NoError(t, enc.Close())
	assert.Empty(t, pw.packets, "Close with nothing pending emits no packet")
}

func TestEncoderFormatMismatch(t *testing.T) {
	pw := &collectPacketWriter{}
	enc := newFakeEncoder(pw, 2)

	// Wrong channel count.
	_, err := enc.Write(mutations.Audio{Data: ramp(testFrame), SampleRate: testRate, Channels: 1})
	assert.ErrorIs(t, err, ErrFormatMismatch)

	// Wrong sample rate.
	_, err = enc.Write(mutations.Audio{Data: ramp(testFrame * 2), SampleRate: 44100, Channels: 2})
	assert.ErrorIs(t, err, ErrFormatMismatch)
}

func TestDecoderEOF(t *testing.T) {
	dec := newFakeDecoder(NewSlicePacketReader(nil), 1)
	buf := make([]float64, testFrame)
	audio, err := dec.Read(buf)
	assert.Empty(t, audio.Data)
	assert.ErrorIs(t, err, io.EOF)
}

func TestDecoderEmptyBuf(t *testing.T) {
	dec := newFakeDecoder(NewSlicePacketReader(nil), 1)
	audio, err := dec.Read(nil)
	assert.Empty(t, audio.Data)
	assert.NoError(t, err)
}

func TestDecoderReadFull(t *testing.T) {
	const frames = 4
	total := testFrame * frames
	pcm := ramp(total)

	pw := &collectPacketWriter{}
	enc := newFakeEncoder(pw, 1)
	_, err := enc.Write(asAudio(enc, pcm))
	require.NoError(t, err)
	require.NoError(t, enc.Close())

	dec := newFakeDecoder(NewSlicePacketReader(pw.packets), 1)
	buf := make([]float64, total)
	audio, err := codec.ReadFull(dec, buf)
	require.NoError(t, err)
	require.Equal(t, total, len(audio.Data))
	assert.Equal(t, pcm, audio.Data)
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
	require.NoError(t, err)
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

	require.NoError(t, pw.WritePacket([]byte{4, 5, 6}))
	assert.Equal(t, []byte{4, 5, 6}, received)
}

func TestSlicePacketReader(t *testing.T) {
	packets := [][]byte{{1}, {2, 3}, {4, 5, 6}}
	pr := NewSlicePacketReader(packets)

	for _, want := range packets {
		got, err := pr.ReadPacket()
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := pr.ReadPacket()
	assert.ErrorIs(t, err, io.EOF)
}

// TestPublicConstructorsForwardOptions verifies the public NewEncoder /
// NewDecoder forward their functional options into libraries/aac. AAC's
// only engine (FDK-AAC) is fenced behind the opt-in aacfdk build tag, so in
// the default build the underlying aaclib.New{Encoder,Decoder} return
// aac.ErrEngineRequiresFDK and these constructors surface it; under
// `-tags aacfdk` (cgo) they build a real backend. The test asserts the
// contract holds either way: construction succeeds and reports the right
// format, or it fails with exactly ErrEngineRequiresFDK.
func TestPublicConstructorsForwardOptions(t *testing.T) {
	pw := &collectPacketWriter{}
	enc, err := NewEncoder(pw, testRate, 2,
		WithObjectType(aaclib.AOTAACLC),
		WithBitrate(96000),
	)
	if err != nil {
		require.ErrorIs(t, err, aaclib.ErrEngineRequiresFDK)
	} else {
		assert.Equal(t, testRate, enc.SampleRate())
		assert.Equal(t, 2, enc.Channels())
	}

	asc := aaclib.AudioSpecificConfig{
		ObjectType:   aaclib.AOTAACLC,
		SampleRate:   testRate,
		Channels:     2,
		FrameSamples: testFrame,
		// A complete ASC needs the raw wire bytes: the FDK backend
		// (-tags aacfdk) configures itself from Raw via aacDecoder_ConfigRaw
		// and rejects an empty config with ErrInvalidConfig. The two bytes
		// pack audioObjectType(2=AAC-LC), samplingFrequencyIndex(3=48000 Hz),
		// and channelConfiguration(2=stereo): (2<<11)|(3<<7)|(2<<3)=0x1190.
		Raw: []byte{0x11, 0x90},
	}
	dec, err := NewDecoder(NewSlicePacketReader(nil), asc)
	if err != nil {
		require.ErrorIs(t, err, aaclib.ErrEngineRequiresFDK)
	} else {
		assert.Equal(t, testRate, dec.SampleRate())
		assert.Equal(t, 2, dec.Channels())
	}
}
