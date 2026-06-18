//go:build darwin

package devices

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFourCC(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want uint32
	}{
		{"devices", "dev#", 0x64657623},        // 'd' 'e' 'v' '#'
		{"default output", "dOut", 0x644F7574}, // 'd' 'O' 'u' 't'
		{"default input (trailing space)", "dIn ", 0x64496E20},
		{"stream config", "slay", 0x736C6179},
		{"nominal sample rate", "nsrt", 0x6E737274},
		{"uid", "uid ", 0x75696420},
		{"name", "lnam", 0x6C6E616D},
		{"scope global", "glob", 0x676C6F62},
		{"scope output", "outp", 0x6F757470},
		{"scope input", "inpt", 0x696E7074},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, fourCC(tc.in))
		})
	}
}

func TestFourCCPanicsOnWrongLength(t *testing.T) {
	assert.Panics(t, func() { fourCC("abc") })
	assert.Panics(t, func() { fourCC("abcde") })
	assert.Panics(t, func() { fourCC("") })
}

// bufferList builds a synthetic AudioBufferList byte payload with the
// given per-buffer channel counts. The layout mirrors the C struct:
//
//	UInt32      mNumberBuffers
//	UInt32      (padding to 8-byte align)
//	AudioBuffer mBuffers[mNumberBuffers]  // {UInt32 ch, UInt32 sz, void* data}
func bufferList(channels []uint32) []byte {
	const firstBufferOffset = 8
	const stride = audioBufferStride
	out := make([]byte, firstBufferOffset+len(channels)*stride)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(channels)))
	for i, ch := range channels {
		off := firstBufferOffset + i*stride
		binary.LittleEndian.PutUint32(out[off:off+4], ch)     // mNumberChannels
		binary.LittleEndian.PutUint32(out[off+4:off+8], ch*4) // mDataByteSize (arbitrary)
		// mData at off+8..off+16 is left zero; parser ignores it.
	}
	return out
}

func TestCountChannelsInBufferList(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"empty slice", nil, 0},
		{"too short", []byte{0x00}, 0},
		{"zero buffers", bufferList(nil), 0},
		{"single mono buffer", bufferList([]uint32{1}), 1},
		{"single stereo buffer", bufferList([]uint32{2}), 2},
		{"two mono buffers (split layout)", bufferList([]uint32{1, 1}), 2},
		{"multi buffer mixed", bufferList([]uint32{2, 1, 4}), 7},
		{"8-channel surround", bufferList([]uint32{8}), 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, countChannelsInBufferList(tc.data))
		})
	}
}

func TestCountChannelsInBufferListTruncated(t *testing.T) {
	// Claim 4 buffers but truncate after the first. The parser must
	// stop at the boundary rather than reading past the slice.
	data := bufferList([]uint32{2, 2, 2, 2})
	truncated := data[:8+audioBufferStride] // header + one buffer
	assert.Equal(t, 2, countChannelsInBufferList(truncated))
}

func TestHostUint32(t *testing.T) {
	// hostUint32 reads little-endian; darwin is always little-endian.
	b := []byte{0x78, 0x56, 0x34, 0x12}
	assert.Equal(t, uint32(0x12345678), hostUint32(b))
}

func TestAudioBufferStride(t *testing.T) {
	// Must match the C AudioBuffer layout on 64-bit darwin:
	// UInt32 + UInt32 + void* == 4 + 4 + 8 == 16 bytes.
	require.Equal(t, 16, audioBufferStride)
}

func TestCFStringEncodingUTF8(t *testing.T) {
	// <CoreFoundation/CFString.h>: kCFStringEncodingUTF8 = 0x08000100.
	assert.Equal(t, uint32(0x08000100), kCFStringEncodingUTF8)
}

func TestCFStringToGoReleasesNil(t *testing.T) {
	// A zero CFStringRef must short-circuit without invoking CFRelease,
	// so we pass a coreAudio whose CFRelease would panic if called.
	ca := &coreAudio{
		CFRelease: func(uintptr) { t.Fatal("CFRelease must not be called on nil ref") },
	}
	assert.Equal(t, "", cfStringToGo(ca, 0))
}

func TestCFStringToGoDecodesBuffer(t *testing.T) {
	// Simulate CoreFoundation by wiring CFStringGetLength /
	// CFStringGetCString to write "hello" into the Go-allocated buffer.
	const want = "hello"
	released := false
	ca := &coreAudio{
		CFStringGetLength: func(uintptr) int64 { return int64(len(want)) },
		CFStringGetCString: func(_ uintptr, buf *byte, sz int64, enc uint32) bool {
			require.Equal(t, uint32(0x08000100), enc)
			require.GreaterOrEqual(t, sz, int64(len(want)+1))
			dst := unsafe.Slice(buf, int(sz))
			copy(dst, want)
			dst[len(want)] = 0
			return true
		},
		CFRelease: func(uintptr) { released = true },
	}
	assert.Equal(t, want, cfStringToGo(ca, 0xdeadbeef))
	assert.True(t, released, "CFRelease must always run for a non-nil ref")
}

func TestCFStringToGoHandlesCFailure(t *testing.T) {
	ca := &coreAudio{
		CFStringGetLength:  func(uintptr) int64 { return 5 },
		CFStringGetCString: func(uintptr, *byte, int64, uint32) bool { return false },
		CFRelease:          func(uintptr) {},
	}
	assert.Equal(t, "", cfStringToGo(ca, 0xdeadbeef))
}

func TestCFStringToGoEmptyString(t *testing.T) {
	released := false
	ca := &coreAudio{
		CFStringGetLength:  func(uintptr) int64 { return 0 },
		CFStringGetCString: func(uintptr, *byte, int64, uint32) bool { return true },
		CFRelease:          func(uintptr) { released = true },
	}
	assert.Equal(t, "", cfStringToGo(ca, 0xdeadbeef))
	assert.True(t, released)
}
