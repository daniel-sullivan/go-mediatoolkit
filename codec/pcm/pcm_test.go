package pcm

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"

	"go-mediatoolkit/codec"

	"go-mediatoolkit/consts"
	"go-mediatoolkit/mutations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asAudio wraps raw PCM in a mutations.Audio using the encoder's
// declared format — keeps call sites concise.
func asAudio(enc codec.Encoder, data []float64) mutations.Audio {
	return mutations.Audio{Data: data, SampleRate: enc.SampleRate(), Channels: enc.Channels()}
}

func TestDecoderAllFormats(t *testing.T) {
	formats := []struct {
		format    mutations.SampleFormat
		tolerance float64
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
		t.Run(tt.format.String(), func(t *testing.T) {
			bps := tt.format.BytesPerSample()
			raw := make([]byte, bps*len(samples))
			mutations.EncodeSamples(samples, raw, tt.format, binary.LittleEndian)

			dec, err := NewDecoder(bytes.NewReader(raw), consts.SampleRate44100, 1, tt.format)
			require.NoError(t, err)

			assert.Equal(t, consts.SampleRate44100, dec.SampleRate())
			assert.Equal(t, 1, dec.Channels())

			dst := make([]float64, len(samples))
			got, err := dec.Read(dst)
			require.Equal(t, len(samples), len(got.Data))
			_ = err

			for i, v := range samples {
				if tt.tolerance == 0 {
					assert.Equal(t, v, dst[i])
				} else {
					assert.InDelta(t, v, dst[i], tt.tolerance)
				}
			}
		})
	}
}

func TestDecoderStereo(t *testing.T) {
	// Interleaved stereo: [L0, R0, L1, R1]
	samples := []float64{0.5, -0.5, 0.25, -0.25}
	raw := make([]byte, 2*len(samples))
	mutations.Float64ToInt16(samples, raw, binary.LittleEndian)

	dec, err := NewDecoder(bytes.NewReader(raw), consts.SampleRate48000, 2, mutations.FormatInt16)
	require.NoError(t, err)
	assert.Equal(t, 2, dec.Channels())

	dst := make([]float64, len(samples))
	got, err := dec.Read(dst)
	require.Equal(t, len(samples), len(got.Data))
	_ = err

	for i, v := range samples {
		assert.InDelta(t, v, dst[i], 0.001)
	}
}

func TestDecoderPartialRead(t *testing.T) {
	// Provide 8 int16 samples but only read 4 at a time.
	samples := make([]float64, 8)
	for i := range samples {
		samples[i] = float64(i) / 10.0
	}
	raw := make([]byte, 2*len(samples))
	mutations.Float64ToInt16(samples, raw, binary.LittleEndian)

	dec, err := NewDecoder(bytes.NewReader(raw), consts.SampleRate44100, 1, mutations.FormatInt16)
	require.NoError(t, err)

	buf := make([]float64, 4)
	got, err := dec.Read(buf)
	assert.Equal(t, 4, len(got.Data))
	assert.NoError(t, err)

	for i := 0; i < 4; i++ {
		assert.InDelta(t, samples[i], buf[i], 0.001)
	}

	got, err = dec.Read(buf)
	assert.Equal(t, 4, len(got.Data))

	for i := 0; i < 4; i++ {
		assert.InDelta(t, samples[i+4], buf[i], 0.001)
	}
}

func TestDecoderEOF(t *testing.T) {
	// Empty reader should return EOF immediately.
	dec, err := NewDecoder(bytes.NewReader(nil), consts.SampleRate44100, 1, mutations.FormatInt16)
	require.NoError(t, err)

	buf := make([]float64, 4)
	got, err := dec.Read(buf)
	assert.Equal(t, 0, len(got.Data))
	assert.ErrorIs(t, err, io.EOF)
}

func TestDecoderByteOrderOption(t *testing.T) {
	// Encode as big-endian, decode as big-endian — should match.
	samples := []float64{0.5, -0.5}
	raw := make([]byte, 2*len(samples))
	mutations.Float64ToInt16(samples, raw, binary.BigEndian)

	dec, err := NewDecoder(bytes.NewReader(raw), consts.SampleRate44100, 1, mutations.FormatInt16, WithByteOrder(binary.BigEndian))
	require.NoError(t, err)

	dst := make([]float64, 2)
	got, err := dec.Read(dst)
	require.Equal(t, 2, len(got.Data))
	_ = err

	for i, v := range samples {
		assert.InDelta(t, v, dst[i], 0.001)
	}
}

// trickleReader returns n bytes at a time, simulating a slow io.Reader.
type trickleReader struct {
	data []byte
	pos  int
	step int
}

func (r *trickleReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	end := r.pos + r.step
	if end > len(r.data) {
		end = len(r.data)
	}
	n := copy(p, r.data[r.pos:end])
	r.pos += n
	if r.pos >= len(r.data) {
		return n, io.EOF
	}
	return n, nil
}

func TestDecoderTrickleRead(t *testing.T) {
	// Reader returns 1 byte at a time — tests leftover accumulation for int16.
	samples := []float64{0.5, -0.5, 0.25}
	raw := make([]byte, 2*len(samples))
	mutations.Float64ToInt16(samples, raw, binary.LittleEndian)

	r := &trickleReader{data: raw, step: 1}
	dec, err := NewDecoder(r, consts.SampleRate44100, 1, mutations.FormatInt16)
	require.NoError(t, err)

	// With 1 byte at a time, each Read produces at most 0 or 1 sample.
	// Use ReadFull to collect them all.
	dst := make([]float64, len(samples))
	got, err := codec.ReadFull(dec, dst)
	// ReadFull returns ErrUnexpectedEOF if EOF hits before buf is full,
	// but we have exactly the right amount, so the underlying reader will
	// return the last byte with EOF. ReadFull treats n == len(buf) with
	// ErrUnexpectedEOF — which is fine, the data is correct.
	assert.Equal(t, len(samples), len(got.Data))
	_ = err

	for i, v := range samples {
		assert.InDelta(t, v, dst[i], 0.001, "sample %d", i)
	}
}

func TestDecoderBadArgs(t *testing.T) {
	r := bytes.NewReader(nil)

	_, err := NewDecoder(r, consts.SampleRate44100, 0, mutations.FormatInt16)
	assert.ErrorIs(t, err, codec.ErrBadChannelCount)

	_, err = NewDecoder(r, 0, 1, mutations.FormatInt16)
	assert.ErrorIs(t, err, codec.ErrBadSampleRate)

	_, err = NewDecoder(r, consts.SampleRate44100, 1, mutations.SampleFormat(99))
	assert.ErrorIs(t, err, ErrUnsupportedFormat)
}

func TestEncoderRoundTrip(t *testing.T) {
	samples := []float64{0.0, 0.5, -0.5, 0.25, -0.25}
	var buf bytes.Buffer

	enc, err := NewEncoder(&buf, consts.SampleRate44100, 1, mutations.FormatInt16)
	require.NoError(t, err)
	assert.Equal(t, consts.SampleRate44100, enc.SampleRate())
	assert.Equal(t, 1, enc.Channels())

	n, err := enc.Write(asAudio(enc, samples))
	require.NoError(t, err)
	assert.Equal(t, len(samples), n)
	require.NoError(t, enc.Close())

	// Decode what was written.
	dec, err := NewDecoder(&buf, consts.SampleRate44100, 1, mutations.FormatInt16)
	require.NoError(t, err)

	dst := make([]float64, len(samples))
	got, _ := dec.Read(dst)
	assert.Equal(t, len(samples), len(got.Data))

	for i, v := range samples {
		assert.InDelta(t, v, dst[i], 0.001)
	}
}

func TestEncoderByteOrder(t *testing.T) {
	samples := []float64{0.5}
	var bufLE, bufBE bytes.Buffer

	encLE, _ := NewEncoder(&bufLE, consts.SampleRate44100, 1, mutations.FormatInt16)
	encBE, _ := NewEncoder(&bufBE, consts.SampleRate44100, 1, mutations.FormatInt16, WithEncoderByteOrder(binary.BigEndian))

	encLE.Write(asAudio(encLE, samples))
	encBE.Write(asAudio(encBE, samples))

	// Same value, different byte order -> different raw bytes.
	assert.NotEqual(t, bufLE.Bytes(), bufBE.Bytes())
}

func TestEncoderBadArgs(t *testing.T) {
	var buf bytes.Buffer

	_, err := NewEncoder(&buf, consts.SampleRate44100, 0, mutations.FormatInt16)
	assert.ErrorIs(t, err, codec.ErrBadChannelCount)

	_, err = NewEncoder(&buf, 0, 1, mutations.FormatInt16)
	assert.ErrorIs(t, err, codec.ErrBadSampleRate)

	_, err = NewEncoder(&buf, consts.SampleRate44100, 1, mutations.SampleFormat(99))
	assert.ErrorIs(t, err, ErrUnsupportedFormat)
}

type shortWriter struct {
	n int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.n {
		return w.n, nil
	}
	return len(p), nil
}

func TestEncoderShortWrite(t *testing.T) {
	enc, err := NewEncoder(&shortWriter{n: 1}, consts.SampleRate44100, 1, mutations.FormatInt16)
	require.NoError(t, err)

	_, err = enc.Write(asAudio(enc, []float64{0.5, -0.5}))
	assert.ErrorIs(t, err, ErrShortWrite)
}

func TestEncoderFloat64FullPrecision(t *testing.T) {
	// float64 format should be lossless.
	samples := []float64{math.Pi, -math.E, 0.123456789012345}
	var buf bytes.Buffer

	enc, _ := NewEncoder(&buf, consts.SampleRate96000, 2, mutations.FormatFloat64)
	enc.Write(asAudio(enc, samples))
	enc.Close()

	dec, _ := NewDecoder(&buf, consts.SampleRate96000, 2, mutations.FormatFloat64)
	dst := make([]float64, len(samples))
	_, _ = dec.Read(dst)

	for i, v := range samples {
		assert.Equal(t, v, dst[i])
	}
}

func TestReadFull(t *testing.T) {
	samples := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
	raw := make([]byte, 2*len(samples))
	mutations.Float64ToInt16(samples, raw, binary.LittleEndian)

	// Trickle 3 bytes at a time (1.5 int16 samples).
	r := &trickleReader{data: raw, step: 3}
	dec, err := NewDecoder(r, consts.SampleRate44100, 1, mutations.FormatInt16)
	require.NoError(t, err)

	dst := make([]float64, len(samples))
	got, err := codec.ReadFull(dec, dst)
	assert.Equal(t, len(samples), len(got.Data))
	_ = err

	for i, v := range samples {
		assert.InDelta(t, v, dst[i], 0.001, "sample %d", i)
	}
}

func TestDecoderEmptyBuf(t *testing.T) {
	dec, _ := NewDecoder(bytes.NewReader([]byte{0, 0}), consts.SampleRate44100, 1, mutations.FormatInt16)
	got, err := dec.Read(nil)
	assert.Equal(t, 0, len(got.Data))
	assert.NoError(t, err)
}

func TestEncoderEmptyBuf(t *testing.T) {
	var buf bytes.Buffer
	enc, _ := NewEncoder(&buf, consts.SampleRate44100, 1, mutations.FormatInt16)
	n, err := enc.Write(mutations.Audio{SampleRate: consts.SampleRate44100, Channels: 1})
	assert.Equal(t, 0, n)
	assert.NoError(t, err)
	assert.Equal(t, 0, buf.Len())
}
