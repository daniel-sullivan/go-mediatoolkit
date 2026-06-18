package ogg

import (
	"bytes"
	"io"
	"math"
	"testing"

	"go-mediatoolkit/codec"

	"go-mediatoolkit/codec/opus"
	"go-mediatoolkit/consts"
	"go-mediatoolkit/containers"
	opuslib "go-mediatoolkit/libraries/opus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenericWriterReaderRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	s, err := w.AddStream(1234)
	require.NoError(t, err)

	packets := [][]byte{
		[]byte("first"),
		[]byte("second"),
		[]byte("third"),
	}
	for i, pkt := range packets {
		s.SetGranule(int64((i + 1) * 100))
		require.NoError(t, s.WritePacket(pkt))
	}
	s.SetEOS()
	require.NoError(t, w.Close())

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)

	streams := r.Streams()
	require.Len(t, streams, 1)
	assert.Equal(t, int32(1234), streams[0].SerialNo)

	// First packet was treated as BOS (header). The generic reader stashes
	// it in HeaderPackets; user code reads data packets via ReadPacket().
	assert.Equal(t, []byte("first"), streams[0].HeaderPackets[0])

	got := [][]byte{}
	for {
		pkt, err := streams[0].ReadPacket()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		got = append(got, pkt)
	}
	assert.Equal(t, [][]byte{[]byte("second"), []byte("third")}, got)
}

func TestGenericReaderRejectsEmpty(t *testing.T) {
	_, err := NewReader(bytes.NewReader(nil))
	assert.ErrorIs(t, err, ErrNoStreams)
}

func TestGenericMultipleStreams(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	sA, _ := w.AddStream(1)
	sB, _ := w.AddStream(2)

	// Write BOS packets for both before any data (required by Ogg spec).
	sA.WritePacket([]byte("headA"))
	sB.WritePacket([]byte("headB"))
	// Interleave data packets.
	sA.WritePacket([]byte("a0"))
	sB.WritePacket([]byte("b0"))
	sA.WritePacket([]byte("a1"))
	sA.SetEOS()
	sB.SetEOS()
	require.NoError(t, w.Close())

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Len(t, r.Streams(), 2)

	sr1 := r.Stream(1)
	sr2 := r.Stream(2)
	require.NotNil(t, sr1)
	require.NotNil(t, sr2)

	assert.Equal(t, []byte("headA"), sr1.HeaderPackets[0])
	assert.Equal(t, []byte("headB"), sr2.HeaderPackets[0])

	// Drain both streams independently.
	drain := func(s *Stream) [][]byte {
		var out [][]byte
		for {
			pkt, err := s.ReadPacket()
			if err == io.EOF {
				return out
			}
			require.NoError(t, err)
			out = append(out, pkt)
		}
	}
	assert.Equal(t, [][]byte{[]byte("a0"), []byte("a1")}, drain(sr1))
	assert.Equal(t, [][]byte{[]byte("b0")}, drain(sr2))
}

func TestOpusRoundTrip(t *testing.T) {
	// Encode a sine wave with libopus, write via OpusWriter, read via
	// OpusReader, decode with libopus, compare peak amplitude.
	libEnc, err := opuslib.NewEncoder(opuslib.Rate48000, 1)
	require.NoError(t, err)

	const frames = 5
	const samplesPerFrame = 960

	pcm := make([]float64, samplesPerFrame)

	var buf bytes.Buffer
	w, err := NewOpusWriter(&buf, 1,
		WithOpusTags(containers.StandardTags{
			Title:  new("Opus-in-Ogg Test"),
			Artist: new("Daniel"),
		}),
	)
	require.NoError(t, err)

	for f := 0; f < frames; f++ {
		for i := range pcm {
			t := float64(f*samplesPerFrame+i) / 48000.0
			pcm[i] = 0.5 * math.Sin(2*math.Pi*440*t)
		}
		pkt, err := libEnc.Encode(pcm, opuslib.MaxFrameBytes)
		require.NoError(t, err)
		require.NoError(t, w.WritePacket(pkt))
	}
	require.NoError(t, w.Close())

	// Read back.
	r, err := NewOpusReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)

	h := r.Header()
	assert.Equal(t, "ogg/opus", h.Format)
	assert.Equal(t, 1, h.Channels)
	require.NotNil(t, h.Tags.Title)
	assert.Equal(t, "Opus-in-Ogg Test", *h.Tags.Title)
	require.NotNil(t, h.Tags.Artist)
	assert.Equal(t, "Daniel", *h.Tags.Artist)
	assert.Equal(t, "go-mediatoolkit", h.Extra.Vendor)
	assert.NotZero(t, h.Extra.Head.PreSkip)

	// Decode through the codec/opus layer (structural compatibility).
	dec, err := opus.NewDecoder(r, h.SampleRate, h.Channels)
	require.NoError(t, err)

	out := make([]float64, frames*samplesPerFrame)
	got, _ := codec.ReadFull(dec, out)
	assert.Equal(t, len(out), len(got.Data))

	var peak float64
	for _, v := range out {
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	assert.Greater(t, peak, 0.1, "decoded audio should not be silent")
}

func TestOpusReaderRejectsNonOpus(t *testing.T) {
	// Build a minimal non-Opus Ogg stream.
	var buf bytes.Buffer
	w := NewWriter(&buf)
	s, _ := w.AddStream(42)
	s.WritePacket([]byte("NotOpus"))
	s.WritePacket([]byte("data"))
	s.SetEOS()
	w.Close()

	_, err := NewOpusReader(bytes.NewReader(buf.Bytes()))
	assert.ErrorIs(t, err, ErrNoOpusStream)
}

func TestOpusHeadRoundTrip(t *testing.T) {
	in := OpusHead{
		Version:         1,
		Channels:        2,
		PreSkip:         312,
		InputSampleRate: consts.SampleRate48000,
		OutputGain:      -128,
		ChannelMapping:  0,
	}
	pkt := buildOpusHead(in)
	assert.True(t, bytes.HasPrefix(pkt, []byte("OpusHead")))
	out, err := parseOpusHead(pkt)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestOpusTagsRoundTrip(t *testing.T) {
	// Typed fields flow through; the multi-value ARTIST demonstrates that
	// AdditionalTags preserves the extras beyond the first value.
	st := containers.StandardTags{
		Title:  new("A Song"),
		Artist: new("Alice"),
		AdditionalTags: containers.Tags{
			"ARTIST": {"Bob"},
		},
	}
	pkt := buildOpusTags("my-encoder", st)
	assert.True(t, bytes.HasPrefix(pkt, []byte("OpusTags")))

	vendor, got, err := parseOpusTags(pkt)
	require.NoError(t, err)
	assert.Equal(t, "my-encoder", vendor)
	assert.Equal(t, "A Song", got.Get("TITLE"))
	assert.ElementsMatch(t, []string{"Alice", "Bob"}, got.GetAll("ARTIST"))
}

func TestOpusInputRateDefaults(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewOpusWriter(&buf, 1, WithOpusInputSampleRate(0))
	require.NoError(t, err)
	libEnc, _ := opuslib.NewEncoder(opuslib.Rate48000, 1)
	pcm := make([]float64, 960)
	pkt, _ := libEnc.Encode(pcm, opuslib.MaxFrameBytes)
	w.WritePacket(pkt)
	w.Close()

	r, err := NewOpusReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	// InputSampleRate=0 means "unspecified", and the header still reports 48 kHz.
	assert.Equal(t, consts.SampleRate48000, r.Header().SampleRate)
	assert.Equal(t, uint32(0), r.Header().Extra.Head.InputSampleRate)
}
