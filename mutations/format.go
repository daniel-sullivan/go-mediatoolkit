package mutations

import (
	"encoding/binary"
	"math"
)

// SampleFormat identifies a PCM sample encoding.
type SampleFormat int

const (
	FormatUint8   SampleFormat = iota // 1 byte, unsigned, 128 = silence
	FormatInt16                       // 2 bytes, signed
	FormatInt24                       // 3 bytes, signed
	FormatInt32                       // 4 bytes, signed
	FormatFloat32                     // 4 bytes, IEEE 754
	FormatFloat64                     // 8 bytes, IEEE 754
)

// BytesPerSample returns the number of bytes per sample for the format.
func (f SampleFormat) BytesPerSample() int {
	switch f {
	case FormatUint8:
		return 1
	case FormatInt16:
		return 2
	case FormatInt24:
		return 3
	case FormatInt32, FormatFloat32:
		return 4
	case FormatFloat64:
		return 8
	default:
		return 0
	}
}

// String returns the name of the sample format.
func (f SampleFormat) String() string {
	switch f {
	case FormatUint8:
		return "uint8"
	case FormatInt16:
		return "int16"
	case FormatInt24:
		return "int24"
	case FormatInt32:
		return "int32"
	case FormatFloat32:
		return "float32"
	case FormatFloat64:
		return "float64"
	default:
		return "unknown"
	}
}

// clamp restricts v to the range [-1.0, 1.0].
func clamp(v float64) float64 {
	if v > 1.0 {
		return 1.0
	}
	if v < -1.0 {
		return -1.0
	}
	return v
}

// --- Decode functions (bytes -> float64) ---

// Uint8ToFloat64 converts unsigned 8-bit PCM bytes to normalized float64 samples.
// Returns the number of samples converted.
func Uint8ToFloat64(src []byte, dst []float64, _ binary.ByteOrder) int {
	n := len(src)
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		dst[i] = (float64(src[i]) - 128) / 128.0
	}
	return n
}

// Int16ToFloat64 converts signed 16-bit PCM bytes to normalized float64 samples.
// Returns the number of samples converted.
func Int16ToFloat64(src []byte, dst []float64, order binary.ByteOrder) int {
	n := len(src) / 2
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		v := int16(order.Uint16(src[i*2:]))
		dst[i] = float64(v) / 32768.0
	}
	return n
}

// Int24ToFloat64 converts signed 24-bit PCM bytes to normalized float64 samples.
// Returns the number of samples converted.
func Int24ToFloat64(src []byte, dst []float64, order binary.ByteOrder) int {
	n := len(src) / 3
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		off := i * 3
		var val int32
		if order == binary.LittleEndian {
			val = int32(src[off]) | int32(src[off+1])<<8 | int32(src[off+2])<<16
		} else {
			val = int32(src[off])<<16 | int32(src[off+1])<<8 | int32(src[off+2])
		}
		// Sign-extend from 24-bit.
		if val&0x800000 != 0 {
			val |= ^0xFFFFFF
		}
		dst[i] = float64(val) / 8388608.0
	}
	return n
}

// Int32ToFloat64 converts signed 32-bit PCM bytes to normalized float64 samples.
// Returns the number of samples converted.
func Int32ToFloat64(src []byte, dst []float64, order binary.ByteOrder) int {
	n := len(src) / 4
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		v := int32(order.Uint32(src[i*4:]))
		dst[i] = float64(v) / 2147483648.0
	}
	return n
}

// Float32ToFloat64 converts 32-bit IEEE 754 float bytes to float64 samples.
// Returns the number of samples converted.
func Float32ToFloat64(src []byte, dst []float64, order binary.ByteOrder) int {
	n := len(src) / 4
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		bits := order.Uint32(src[i*4:])
		dst[i] = float64(math.Float32frombits(bits))
	}
	return n
}

// BytesToFloat64 converts 64-bit IEEE 754 float bytes to float64 samples.
// Returns the number of samples converted.
func BytesToFloat64(src []byte, dst []float64, order binary.ByteOrder) int {
	n := len(src) / 8
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		bits := order.Uint64(src[i*8:])
		dst[i] = math.Float64frombits(bits)
	}
	return n
}

// --- Encode functions (float64 -> bytes) ---

// Float64ToUint8 converts normalized float64 samples to unsigned 8-bit PCM bytes.
// Samples are clamped to [-1.0, 1.0] before conversion. Returns the number of samples converted.
func Float64ToUint8(src []float64, dst []byte, _ binary.ByteOrder) int {
	n := len(src)
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		v := clamp(src[i])*128.0 + 128.0
		if v > 255 {
			v = 255
		}
		dst[i] = byte(v)
	}
	return n
}

// Float64ToInt16 converts normalized float64 samples to signed 16-bit PCM bytes.
// Samples are clamped to [-1.0, 1.0] before conversion. Returns the number of samples converted.
func Float64ToInt16(src []float64, dst []byte, order binary.ByteOrder) int {
	n := len(src)
	if n > len(dst)/2 {
		n = len(dst) / 2
	}
	for i := 0; i < n; i++ {
		v := clamp(src[i])
		s := int16(v * 32767.0)
		order.PutUint16(dst[i*2:], uint16(s))
	}
	return n
}

// Float64ToInt24 converts normalized float64 samples to signed 24-bit PCM bytes.
// Samples are clamped to [-1.0, 1.0] before conversion. Returns the number of samples converted.
func Float64ToInt24(src []float64, dst []byte, order binary.ByteOrder) int {
	n := len(src)
	if n > len(dst)/3 {
		n = len(dst) / 3
	}
	for i := 0; i < n; i++ {
		v := clamp(src[i])
		s := int32(v * 8388607.0)
		off := i * 3
		if order == binary.LittleEndian {
			dst[off] = byte(s)
			dst[off+1] = byte(s >> 8)
			dst[off+2] = byte(s >> 16)
		} else {
			dst[off] = byte(s >> 16)
			dst[off+1] = byte(s >> 8)
			dst[off+2] = byte(s)
		}
	}
	return n
}

// Float64ToInt32 converts normalized float64 samples to signed 32-bit PCM bytes.
// Samples are clamped to [-1.0, 1.0] before conversion. Returns the number of samples converted.
func Float64ToInt32(src []float64, dst []byte, order binary.ByteOrder) int {
	n := len(src)
	if n > len(dst)/4 {
		n = len(dst) / 4
	}
	for i := 0; i < n; i++ {
		v := clamp(src[i])
		s := int32(v * 2147483647.0)
		order.PutUint32(dst[i*4:], uint32(s))
	}
	return n
}

// Float64ToFloat32Bytes converts float64 samples to 32-bit IEEE 754 float bytes.
// Returns the number of samples converted.
func Float64ToFloat32Bytes(src []float64, dst []byte, order binary.ByteOrder) int {
	n := len(src)
	if n > len(dst)/4 {
		n = len(dst) / 4
	}
	for i := 0; i < n; i++ {
		bits := math.Float32bits(float32(src[i]))
		order.PutUint32(dst[i*4:], bits)
	}
	return n
}

// Float64ToBytes converts float64 samples to 64-bit IEEE 754 float bytes.
// Returns the number of samples converted.
func Float64ToBytes(src []float64, dst []byte, order binary.ByteOrder) int {
	n := len(src)
	if n > len(dst)/8 {
		n = len(dst) / 8
	}
	for i := 0; i < n; i++ {
		bits := math.Float64bits(src[i])
		order.PutUint64(dst[i*8:], bits)
	}
	return n
}

// --- Dispatcher functions ---

// DecodeSamples converts bytes to float64 samples using the specified format.
// Returns the number of samples converted.
func DecodeSamples(src []byte, dst []float64, format SampleFormat, order binary.ByteOrder) int {
	switch format {
	case FormatUint8:
		return Uint8ToFloat64(src, dst, order)
	case FormatInt16:
		return Int16ToFloat64(src, dst, order)
	case FormatInt24:
		return Int24ToFloat64(src, dst, order)
	case FormatInt32:
		return Int32ToFloat64(src, dst, order)
	case FormatFloat32:
		return Float32ToFloat64(src, dst, order)
	case FormatFloat64:
		return BytesToFloat64(src, dst, order)
	default:
		return 0
	}
}

// EncodeSamples converts float64 samples to bytes using the specified format.
// Returns the number of samples converted.
func EncodeSamples(src []float64, dst []byte, format SampleFormat, order binary.ByteOrder) int {
	switch format {
	case FormatUint8:
		return Float64ToUint8(src, dst, order)
	case FormatInt16:
		return Float64ToInt16(src, dst, order)
	case FormatInt24:
		return Float64ToInt24(src, dst, order)
	case FormatInt32:
		return Float64ToInt32(src, dst, order)
	case FormatFloat32:
		return Float64ToFloat32Bytes(src, dst, order)
	case FormatFloat64:
		return Float64ToBytes(src, dst, order)
	default:
		return 0
	}
}
