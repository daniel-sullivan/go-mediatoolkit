package wav

import (
	"bytes"
	"io"
	"os"
	"testing"

	"go-mediatoolkit/codec"

	"go-mediatoolkit/codec/pcm"
	"go-mediatoolkit/consts"
	"go-mediatoolkit/containers"
	"go-mediatoolkit/mutations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asAudio wraps raw PCM in a mutations.Audio using the encoder's
// declared format — keeps call sites concise.
func asAudio(enc codec.Encoder, data []float64) mutations.Audio {
	return mutations.Audio{Data: data, SampleRate: enc.SampleRate(), Channels: enc.Channels()}
}

// byteSeeker is an in-memory io.WriteSeeker for tests.
type byteSeeker struct {
	buf []byte
	pos int64
}

func (b *byteSeeker) Write(p []byte) (int, error) {
	need := int(b.pos) + len(p)
	if need > len(b.buf) {
		b.buf = append(b.buf, make([]byte, need-len(b.buf))...)
	}
	n := copy(b.buf[b.pos:], p)
	b.pos += int64(n)
	return n, nil
}

func (b *byteSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		b.pos = offset
	case io.SeekCurrent:
		b.pos += offset
	case io.SeekEnd:
		b.pos = int64(len(b.buf)) + offset
	}
	return b.pos, nil
}

func (b *byteSeeker) Bytes() []byte { return b.buf }

func TestWriteReadRoundTrip(t *testing.T) {
	samples := []float64{0.0, 0.5, -0.5, 0.25, -0.25, 0.1}

	bs := &byteSeeker{}
	h := Header{
		SampleRate:   consts.SampleRate44100,
		Channels:     1,
		SampleFormat: mutations.FormatInt16,
		Tags: containers.StandardTags{
			Title:  new("Test"),
			Artist: new("Daniel"),
			Date:   new("2026-04-22"),
		},
	}

	w, err := NewWriter(bs, h)
	require.NoError(t, err)

	enc, err := pcm.NewEncoder(w.Data(), h.SampleRate, h.Channels, h.SampleFormat)
	require.NoError(t, err)
	_, err = enc.Write(asAudio(enc, samples))
	require.NoError(t, err)
	require.NoError(t, enc.Close())
	require.NoError(t, w.Close())

	// Read it back.
	r, err := NewReader(bytes.NewReader(bs.Bytes()))
	require.NoError(t, err)

	got := r.Header()
	assert.Equal(t, "wav", got.Format)
	assert.Equal(t, consts.SampleRate44100, got.SampleRate)
	assert.Equal(t, 1, got.Channels)
	assert.Equal(t, mutations.FormatInt16, got.SampleFormat)
	require.NotNil(t, got.Tags.Title)
	assert.Equal(t, "Test", *got.Tags.Title)
	require.NotNil(t, got.Tags.Artist)
	assert.Equal(t, "Daniel", *got.Tags.Artist)
	require.NotNil(t, got.Tags.Date)
	assert.Equal(t, "2026-04-22", *got.Tags.Date)

	dec, err := pcm.NewDecoder(r.Data(), got.SampleRate, got.Channels, got.SampleFormat)
	require.NoError(t, err)

	out := make([]float64, len(samples))
	got2, _ := dec.Read(out)
	assert.Equal(t, len(samples), len(got2.Data))
	for i, v := range samples {
		assert.InDelta(t, v, out[i], 0.001, "sample %d", i)
	}
}

func TestReadRejectsNonRIFF(t *testing.T) {
	_, err := NewReader(bytes.NewReader([]byte("NOTAFILE")))
	assert.ErrorIs(t, err, ErrNotRIFF)
}

func TestReadRejectsNonWAVE(t *testing.T) {
	buf := append([]byte{}, idRIFF[:]...)
	buf = append(buf, 0, 0, 0, 0) // size
	buf = append(buf, 'A', 'V', 'I', ' ')
	_, err := NewReader(bytes.NewReader(buf))
	assert.ErrorIs(t, err, ErrNotWAVE)
}

func TestWriterRejectsBadParams(t *testing.T) {
	bs := &byteSeeker{}
	_, err := NewWriter(bs, Header{SampleRate: 0, Channels: 1, SampleFormat: mutations.FormatInt16})
	assert.ErrorIs(t, err, ErrUnsupportedFormat)

	_, err = NewWriter(bs, Header{SampleRate: consts.SampleRate44100, Channels: 0, SampleFormat: mutations.FormatInt16})
	assert.ErrorIs(t, err, ErrUnsupportedFormat)

	_, err = NewWriter(bs, Header{SampleRate: consts.SampleRate44100, Channels: 1, SampleFormat: mutations.SampleFormat(99)})
	assert.ErrorIs(t, err, ErrUnsupportedFormat)
}

func TestAllFormatsRoundTrip(t *testing.T) {
	formats := []struct {
		f   mutations.SampleFormat
		tol float64
	}{
		{mutations.FormatUint8, 1.0/128.0 + 0.01},
		{mutations.FormatInt16, 1.0/32768.0 + 0.001},
		{mutations.FormatInt24, 1.0/8388608.0 + 0.001},
		{mutations.FormatInt32, 1.0/2147483648.0 + 0.001},
		{mutations.FormatFloat32, 1e-6},
		{mutations.FormatFloat64, 0},
	}
	samples := []float64{0.0, 0.5, -0.5, 0.25}

	for _, tt := range formats {
		t.Run(tt.f.String(), func(t *testing.T) {
			bs := &byteSeeker{}
			w, err := NewWriter(bs, Header{SampleRate: consts.SampleRate48000, Channels: 1, SampleFormat: tt.f})
			require.NoError(t, err)
			enc, err := pcm.NewEncoder(w.Data(), consts.SampleRate48000, 1, tt.f)
			require.NoError(t, err)
			_, err = enc.Write(asAudio(enc, samples))
			require.NoError(t, err)
			require.NoError(t, enc.Close())
			require.NoError(t, w.Close())

			r, err := NewReader(bytes.NewReader(bs.Bytes()))
			require.NoError(t, err)
			assert.Equal(t, tt.f, r.Header().SampleFormat)

			dec, err := pcm.NewDecoder(r.Data(), consts.SampleRate48000, 1, tt.f)
			require.NoError(t, err)
			out := make([]float64, len(samples))
			_, _ = dec.Read(out)
			for i, v := range samples {
				if tt.tol == 0 {
					assert.Equal(t, v, out[i])
				} else {
					assert.InDelta(t, v, out[i], tt.tol)
				}
			}
		})
	}
}

func TestStereo(t *testing.T) {
	samples := []float64{0.5, -0.5, 0.25, -0.25, 0.1, -0.1}
	bs := &byteSeeker{}
	w, _ := NewWriter(bs, Header{SampleRate: consts.SampleRate48000, Channels: 2, SampleFormat: mutations.FormatInt16})
	enc, _ := pcm.NewEncoder(w.Data(), consts.SampleRate48000, 2, mutations.FormatInt16)
	enc.Write(asAudio(enc, samples))
	enc.Close()
	w.Close()

	r, _ := NewReader(bytes.NewReader(bs.Bytes()))
	assert.Equal(t, 2, r.Header().Channels)
	assert.Greater(t, r.Header().Duration.Nanoseconds(), int64(0))
}

func TestOddSizedDataPaddedEven(t *testing.T) {
	// int16 * 3 samples = 6 bytes (even). Use uint8 * 3 = 3 bytes (odd) to
	// exercise the trailing pad byte path.
	bs := &byteSeeker{}
	w, _ := NewWriter(bs, Header{SampleRate: 8000, Channels: 1, SampleFormat: mutations.FormatUint8})
	enc, _ := pcm.NewEncoder(w.Data(), 8000, 1, mutations.FormatUint8)
	enc.Write(asAudio(enc, []float64{0, 0.5, -0.5}))
	enc.Close()
	require.NoError(t, w.Close())
	// Total RIFF size must be even; the file should be 2-byte aligned overall.
	assert.Equal(t, 0, len(bs.Bytes())%2)
}

func TestTagsRoundTripUnknownFourCC(t *testing.T) {
	bs := &byteSeeker{}
	h := Header{
		SampleRate:   consts.SampleRate44100,
		Channels:     1,
		SampleFormat: mutations.FormatInt16,
		Tags: containers.StandardTags{
			Title: new("Known"),
			AdditionalTags: containers.Tags{
				"WAV:IXYZ": {"CustomValue"},
			},
		},
	}
	w, _ := NewWriter(bs, h)
	enc, _ := pcm.NewEncoder(w.Data(), consts.SampleRate44100, 1, mutations.FormatInt16)
	enc.Write(asAudio(enc, []float64{0}))
	enc.Close()
	w.Close()

	r, _ := NewReader(bytes.NewReader(bs.Bytes()))
	got := r.Header().Tags
	require.NotNil(t, got.Title)
	assert.Equal(t, "Known", *got.Title)
	assert.Equal(t, []string{"CustomValue"}, got.AdditionalTags["WAV:IXYZ"])
}

func TestBextRoundTrip(t *testing.T) {
	bs := &byteSeeker{}
	h := Header{
		SampleRate:   consts.SampleRate48000,
		Channels:     1,
		SampleFormat: mutations.FormatInt16,
		Extra: Extras{
			Bext: &BroadcastExt{
				Description:     "test recording",
				Originator:      "go-mediatoolkit",
				OriginationDate: "2026-04-22",
				OriginationTime: "12:00:00",
				TimeReference:   consts.SampleRate48000 * 60,
				Version:         1,
				CodingHistory:   "A=PCM,F=consts.SampleRate48000,W=16,M=mono",
			},
		},
	}
	w, _ := NewWriter(bs, h)
	enc, _ := pcm.NewEncoder(w.Data(), consts.SampleRate48000, 1, mutations.FormatInt16)
	enc.Write(asAudio(enc, []float64{0.1, 0.2}))
	enc.Close()
	w.Close()

	r, _ := NewReader(bytes.NewReader(bs.Bytes()))
	b := r.Header().Extra.Bext
	require.NotNil(t, b)
	assert.Equal(t, "test recording", b.Description)
	assert.Equal(t, "go-mediatoolkit", b.Originator)
	assert.Equal(t, "2026-04-22", b.OriginationDate)
	assert.Equal(t, uint64(consts.SampleRate48000*60), b.TimeReference)
	assert.Equal(t, "A=PCM,F=consts.SampleRate48000,W=16,M=mono", b.CodingHistory)
}

func TestCueRoundTrip(t *testing.T) {
	bs := &byteSeeker{}
	h := Header{
		SampleRate:   consts.SampleRate48000,
		Channels:     1,
		SampleFormat: mutations.FormatInt16,
		Extra: Extras{
			Cues: []CuePoint{
				{ID: 1, Position: 100, DataChunkID: idDATA, SampleOffset: 100},
				{ID: 2, Position: 200, DataChunkID: idDATA, SampleOffset: 200},
			},
		},
	}
	w, _ := NewWriter(bs, h)
	enc, _ := pcm.NewEncoder(w.Data(), consts.SampleRate48000, 1, mutations.FormatInt16)
	enc.Write(asAudio(enc, []float64{0, 0, 0, 0}))
	enc.Close()
	w.Close()

	r, _ := NewReader(bytes.NewReader(bs.Bytes()))
	cues := r.Header().Extra.Cues
	require.Len(t, cues, 2)
	assert.Equal(t, uint32(1), cues[0].ID)
	assert.Equal(t, uint32(200), cues[1].Position)
}

func TestPartialRead(t *testing.T) {
	bs := &byteSeeker{}
	samples := make([]float64, 100)
	for i := range samples {
		samples[i] = float64(i) / 100.0
	}
	w, _ := NewWriter(bs, Header{SampleRate: consts.SampleRate48000, Channels: 1, SampleFormat: mutations.FormatInt16})
	enc, _ := pcm.NewEncoder(w.Data(), consts.SampleRate48000, 1, mutations.FormatInt16)
	enc.Write(asAudio(enc, samples))
	enc.Close()
	w.Close()

	r, _ := NewReader(bytes.NewReader(bs.Bytes()))
	// Read in 7-sample chunks.
	dec, _ := pcm.NewDecoder(r.Data(), consts.SampleRate48000, 1, mutations.FormatInt16)
	var out []float64
	buf := make([]float64, 7)
	for {
		got, err := dec.Read(buf)
		out = append(out, got.Data...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	assert.Equal(t, len(samples), len(out))
}

func TestFileRoundTrip(t *testing.T) {
	f, err := os.CreateTemp("", "wav-test-*.wav")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	w, err := NewWriter(f, Header{SampleRate: consts.SampleRate48000, Channels: 1, SampleFormat: mutations.FormatInt16})
	require.NoError(t, err)
	enc, _ := pcm.NewEncoder(w.Data(), consts.SampleRate48000, 1, mutations.FormatInt16)
	enc.Write(asAudio(enc, []float64{0.5, -0.5}))
	enc.Close()
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())

	f2, err := os.Open(f.Name())
	require.NoError(t, err)
	defer f2.Close()

	r, err := NewReader(f2)
	require.NoError(t, err)
	assert.Equal(t, consts.SampleRate48000, r.Header().SampleRate)
}
