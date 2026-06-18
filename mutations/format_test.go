package mutations

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSampleFormat_BytesPerSample(t *testing.T) {
	tests := []struct {
		format SampleFormat
		want   int
	}{
		{FormatUint8, 1},
		{FormatInt16, 2},
		{FormatInt24, 3},
		{FormatInt32, 4},
		{FormatFloat32, 4},
		{FormatFloat64, 8},
		{SampleFormat(99), 0},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.format.BytesPerSample(), "format %v", tt.format)
	}
}

func TestSampleFormat_String(t *testing.T) {
	assert.Equal(t, "uint8", FormatUint8.String())
	assert.Equal(t, "int16", FormatInt16.String())
	assert.Equal(t, "int24", FormatInt24.String())
	assert.Equal(t, "int32", FormatInt32.String())
	assert.Equal(t, "float32", FormatFloat32.String())
	assert.Equal(t, "float64", FormatFloat64.String())
	assert.Equal(t, "unknown", SampleFormat(99).String())
}

func TestUint8RoundTrip(t *testing.T) {
	samples := []float64{0.0, 1.0, -1.0, 0.5, -0.5}
	buf := make([]byte, len(samples))
	dst := make([]float64, len(samples))

	n := Float64ToUint8(samples, buf, binary.LittleEndian)
	require.Equal(t, len(samples), n)

	n = Uint8ToFloat64(buf, dst, binary.LittleEndian)
	require.Equal(t, len(samples), n)

	// uint8 has low precision (1/128 steps), so use a wide tolerance.
	for i, v := range samples {
		assert.InDelta(t, v, dst[i], 1.0/128.0+0.001, "sample %d", i)
	}
}

func TestUint8Encoding(t *testing.T) {
	buf := make([]byte, 1)

	// Silence (0.0) should encode to 128.
	Float64ToUint8([]float64{0.0}, buf, binary.LittleEndian)
	assert.Equal(t, byte(128), buf[0])

	// Max (1.0) should encode to 255 (clamped).
	Float64ToUint8([]float64{1.0}, buf, binary.LittleEndian)
	assert.Equal(t, byte(255), buf[0])

	// -1.0 should encode to 0.
	Float64ToUint8([]float64{-1.0}, buf, binary.LittleEndian)
	assert.Equal(t, byte(0), buf[0])
}

func TestInt16RoundTrip(t *testing.T) {
	samples := []float64{0.0, 0.5, -0.5, 0.99, -0.99}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		buf := make([]byte, len(samples)*2)
		dst := make([]float64, len(samples))

		n := Float64ToInt16(samples, buf, order)
		require.Equal(t, len(samples), n)

		n = Int16ToFloat64(buf, dst, order)
		require.Equal(t, len(samples), n)

		for i, v := range samples {
			assert.InDelta(t, v, dst[i], 1.0/32768.0+0.001, "order=%v sample %d", order, i)
		}
	}
}

func TestInt16KnownValues(t *testing.T) {
	// 0x7FFF (32767) in little-endian should decode close to 1.0.
	src := []byte{0xFF, 0x7F}
	dst := make([]float64, 1)
	Int16ToFloat64(src, dst, binary.LittleEndian)
	assert.InDelta(t, 32767.0/32768.0, dst[0], 1e-10)

	// 0x8000 (-32768) should decode to -1.0.
	src = []byte{0x00, 0x80}
	Int16ToFloat64(src, dst, binary.LittleEndian)
	assert.InDelta(t, -1.0, dst[0], 1e-10)
}

func TestInt24RoundTrip(t *testing.T) {
	samples := []float64{0.0, 0.5, -0.5, 0.99, -0.99}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		buf := make([]byte, len(samples)*3)
		dst := make([]float64, len(samples))

		n := Float64ToInt24(samples, buf, order)
		require.Equal(t, len(samples), n)

		n = Int24ToFloat64(buf, dst, order)
		require.Equal(t, len(samples), n)

		for i, v := range samples {
			assert.InDelta(t, v, dst[i], 1.0/8388608.0+0.001, "order=%v sample %d", order, i)
		}
	}
}

func TestInt24SignExtension(t *testing.T) {
	// -1 in 24-bit LE: 0xFF 0xFF 0xFF
	src := []byte{0xFF, 0xFF, 0xFF}
	dst := make([]float64, 1)
	Int24ToFloat64(src, dst, binary.LittleEndian)
	assert.InDelta(t, -1.0/8388608.0, dst[0], 1e-10)

	// -1 in 24-bit BE: 0xFF 0xFF 0xFF (same bytes, different interpretation path)
	Int24ToFloat64(src, dst, binary.BigEndian)
	assert.InDelta(t, -1.0/8388608.0, dst[0], 1e-10)

	// 0x800000 (-8388608) in LE: most negative 24-bit value.
	src = []byte{0x00, 0x00, 0x80}
	Int24ToFloat64(src, dst, binary.LittleEndian)
	assert.InDelta(t, -1.0, dst[0], 1e-10)
}

func TestInt32RoundTrip(t *testing.T) {
	samples := []float64{0.0, 0.5, -0.5, 0.99, -0.99}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		buf := make([]byte, len(samples)*4)
		dst := make([]float64, len(samples))

		n := Float64ToInt32(samples, buf, order)
		require.Equal(t, len(samples), n)

		n = Int32ToFloat64(buf, dst, order)
		require.Equal(t, len(samples), n)

		for i, v := range samples {
			assert.InDelta(t, v, dst[i], 1.0/2147483648.0+0.001, "order=%v sample %d", order, i)
		}
	}
}

func TestFloat32RoundTrip(t *testing.T) {
	samples := []float64{0.0, 1.0, -1.0, 0.5, -0.5, 0.123456}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		buf := make([]byte, len(samples)*4)
		dst := make([]float64, len(samples))

		n := Float64ToFloat32Bytes(samples, buf, order)
		require.Equal(t, len(samples), n)

		n = Float32ToFloat64(buf, dst, order)
		require.Equal(t, len(samples), n)

		for i, v := range samples {
			// float32 precision loss.
			assert.InDelta(t, v, dst[i], 1e-6, "order=%v sample %d", order, i)
		}
	}
}

func TestFloat64RoundTrip(t *testing.T) {
	samples := []float64{0.0, 1.0, -1.0, 0.5, -0.5, math.Pi, -math.E}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		buf := make([]byte, len(samples)*8)
		dst := make([]float64, len(samples))

		n := Float64ToBytes(samples, buf, order)
		require.Equal(t, len(samples), n)

		n = BytesToFloat64(buf, dst, order)
		require.Equal(t, len(samples), n)

		for i, v := range samples {
			assert.Equal(t, v, dst[i], "order=%v sample %d", order, i)
		}
	}
}

func TestClampOnEncode(t *testing.T) {
	samples := []float64{2.0, -2.0, 1.5, -1.5}

	// int16 should clamp.
	buf := make([]byte, len(samples)*2)
	Float64ToInt16(samples, buf, binary.LittleEndian)
	dst := make([]float64, len(samples))
	Int16ToFloat64(buf, dst, binary.LittleEndian)

	for _, v := range dst {
		assert.LessOrEqual(t, v, 1.0)
		assert.GreaterOrEqual(t, v, -1.0)
	}

	// int32 should clamp.
	buf = make([]byte, len(samples)*4)
	Float64ToInt32(samples, buf, binary.LittleEndian)
	dst = make([]float64, len(samples))
	Int32ToFloat64(buf, dst, binary.LittleEndian)

	for _, v := range dst {
		assert.LessOrEqual(t, v, 1.0)
		assert.GreaterOrEqual(t, v, -1.0)
	}
}

func TestPartialBuffers(t *testing.T) {
	// src has 4 int16 samples (8 bytes), but dst only has room for 2.
	src := make([]byte, 8)
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint16(src[i*2:], uint16(int16(i*1000)))
	}
	dst := make([]float64, 2)
	n := Int16ToFloat64(src, dst, binary.LittleEndian)
	assert.Equal(t, 2, n)

	// Reverse: src has 4 float64 samples, but dst only has room for 2 int16.
	f64 := []float64{0.1, 0.2, 0.3, 0.4}
	byteDst := make([]byte, 4) // room for 2 int16 samples
	n = Float64ToInt16(f64, byteDst, binary.LittleEndian)
	assert.Equal(t, 2, n)
}

func TestByteOrderMatters(t *testing.T) {
	// Encode as LE, decode as BE — should produce different values.
	samples := []float64{0.5}
	buf := make([]byte, 2)
	Float64ToInt16(samples, buf, binary.LittleEndian)

	dstLE := make([]float64, 1)
	dstBE := make([]float64, 1)
	Int16ToFloat64(buf, dstLE, binary.LittleEndian)
	Int16ToFloat64(buf, dstBE, binary.BigEndian)

	assert.InDelta(t, 0.5, dstLE[0], 0.001)
	assert.True(t, math.Abs(0.5-dstBE[0]) > 0.01, "BE decode of LE data should differ")
}

func TestDispatcherDecodeSamples(t *testing.T) {
	formats := []SampleFormat{FormatUint8, FormatInt16, FormatInt24, FormatInt32, FormatFloat32, FormatFloat64}
	for _, fmt := range formats {
		bps := fmt.BytesPerSample()
		src := make([]byte, bps*4)
		dst := make([]float64, 4)
		n := DecodeSamples(src, dst, fmt, binary.LittleEndian)
		assert.Equal(t, 4, n, "format %v", fmt)
	}
}

func TestDispatcherEncodeSamples(t *testing.T) {
	formats := []SampleFormat{FormatUint8, FormatInt16, FormatInt24, FormatInt32, FormatFloat32, FormatFloat64}
	for _, fmt := range formats {
		bps := fmt.BytesPerSample()
		src := []float64{0.0, 0.5, -0.5, 0.25}
		dst := make([]byte, bps*4)
		n := EncodeSamples(src, dst, fmt, binary.LittleEndian)
		assert.Equal(t, 4, n, "format %v", fmt)
	}
}

func TestDispatcherUnknownFormat(t *testing.T) {
	assert.Equal(t, 0, DecodeSamples([]byte{0}, []float64{0}, SampleFormat(99), binary.LittleEndian))
	assert.Equal(t, 0, EncodeSamples([]float64{0}, []byte{0}, SampleFormat(99), binary.LittleEndian))
}

func TestDispatcherRoundTrip(t *testing.T) {
	formats := []struct {
		format    SampleFormat
		tolerance float64
	}{
		{FormatUint8, 1.0/128.0 + 0.01},
		{FormatInt16, 1.0/32768.0 + 0.001},
		{FormatInt24, 1.0/8388608.0 + 0.001},
		{FormatInt32, 1.0/2147483648.0 + 0.001},
		{FormatFloat32, 1e-6},
		{FormatFloat64, 0},
	}

	samples := []float64{0.0, 0.5, -0.5, 0.25}

	for _, tt := range formats {
		bps := tt.format.BytesPerSample()
		encoded := make([]byte, bps*len(samples))
		decoded := make([]float64, len(samples))

		EncodeSamples(samples, encoded, tt.format, binary.LittleEndian)
		DecodeSamples(encoded, decoded, tt.format, binary.LittleEndian)

		for i, v := range samples {
			if tt.tolerance == 0 {
				assert.Equal(t, v, decoded[i], "format %v sample %d", tt.format, i)
			} else {
				assert.InDelta(t, v, decoded[i], tt.tolerance, "format %v sample %d", tt.format, i)
			}
		}
	}
}
