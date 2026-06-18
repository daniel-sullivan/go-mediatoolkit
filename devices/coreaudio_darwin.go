//go:build darwin

package devices

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// This file contains the low-level purego bindings to CoreAudio and
// CoreFoundation used by the darwin Backend implementation. It is
// restricted to the no-CGo build; the CGo variant in a future change
// will use <CoreAudio/AudioHardware.h> directly and also install
// property listeners for real hotplug events.

// fourCC packs a four-byte ASCII tag into a uint32 in big-endian order,
// matching the convention used by CoreAudio OSType / four-char-code
// constants.
func fourCC(s string) uint32 {
	if len(s) != 4 {
		panic("devices: fourCC requires a 4-byte string")
	}
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}

// CoreAudio / CoreFoundation constants. FourCC values verified against
// Apple's AudioHardwareBase.h, AudioHardware.h, and CFString.h headers.
var (
	kAudioHardwarePropertyDevices              = fourCC("dev#")
	kAudioHardwarePropertyDefaultOutputDevice  = fourCC("dOut")
	kAudioHardwarePropertyDefaultInputDevice   = fourCC("dIn ")
	kAudioHardwarePropertyTranslateUIDToDevice = fourCC("uidd")
	kAudioDevicePropertyStreamConfiguration    = fourCC("slay")
	kAudioDevicePropertyNominalSampleRate      = fourCC("nsrt")
	kAudioDevicePropertyDeviceUID              = fourCC("uid ")
	kAudioDevicePropertyStreamFormat           = fourCC("sfmt")
	kAudioDevicePropertyBufferFrameSize        = fourCC("fsiz")
	kAudioObjectPropertyName                   = fourCC("lnam")
	kAudioObjectPropertyScopeGlobal            = fourCC("glob")
	kAudioObjectPropertyScopeOutput            = fourCC("outp")
	kAudioObjectPropertyScopeInput             = fourCC("inpt")

	// 'lpcm' — linear PCM. The only format flag combination we accept.
	kAudioFormatLinearPCM = fourCC("lpcm")
)

// AudioFormatFlags we care about. See AudioStreamBasicDescription docs.
const (
	kAudioFormatFlagIsFloat          uint32 = 1 << 0
	kAudioFormatFlagIsBigEndian      uint32 = 1 << 1
	kAudioFormatFlagIsSignedInteger  uint32 = 1 << 2
	kAudioFormatFlagIsPacked         uint32 = 1 << 3
	kAudioFormatFlagIsNonInterleaved uint32 = 1 << 5
)

const (
	kAudioObjectPropertyElementMain uint32 = 0
	kAudioObjectSystemObject        uint32 = 1

	// kCFStringEncodingUTF8 matches <CoreFoundation/CFString.h>.
	kCFStringEncodingUTF8 uint32 = 0x08000100
)

// audioPropAddr mirrors AudioObjectPropertyAddress. The layout is three
// packed uint32s; Go aligns this naturally with no padding, matching
// the C struct.
type audioPropAddr struct {
	Selector uint32
	Scope    uint32
	Element  uint32
}

// audioBufferHeader mirrors the leading fields of AudioBuffer
// (mNumberChannels, mDataByteSize). The trailing void* mData is
// represented separately so parsing matches the ABI on both amd64
// and arm64 where pointers are 8 bytes. See audioBufferStride.
type audioBufferHeader struct {
	NumberChannels uint32
	DataByteSize   uint32
}

// audioBufferStride is the size in bytes of a single AudioBuffer entry
// inside an AudioBufferList: two uint32s plus a pointer-sized mData
// field. On darwin amd64 and arm64 this is 16 bytes.
const audioBufferStride = 8 + 8

// audioStreamBasicDescription mirrors the CoreAudio ASBD struct.
//
//	struct AudioStreamBasicDescription {
//	  Float64 mSampleRate;
//	  UInt32  mFormatID;
//	  UInt32  mFormatFlags;
//	  UInt32  mBytesPerPacket;
//	  UInt32  mFramesPerPacket;
//	  UInt32  mBytesPerFrame;
//	  UInt32  mChannelsPerFrame;
//	  UInt32  mBitsPerChannel;
//	  UInt32  mReserved;
//	};
//
// Total size 40 bytes on 64-bit.
type audioStreamBasicDescription struct {
	SampleRate       float64
	FormatID         uint32
	FormatFlags      uint32
	BytesPerPacket   uint32
	FramesPerPacket  uint32
	BytesPerFrame    uint32
	ChannelsPerFrame uint32
	BitsPerChannel   uint32
	Reserved         uint32
}

// coreAudio holds the dynamically-resolved CoreAudio and
// CoreFoundation symbols used by the backend.
type coreAudio struct {
	AudioObjectGetPropertyDataSize func(id uint32, addr *audioPropAddr, qsz uint32, q unsafe.Pointer, dataSize *uint32) int32
	AudioObjectGetPropertyData     func(id uint32, addr *audioPropAddr, qsz uint32, q unsafe.Pointer, dataSize *uint32, data unsafe.Pointer) int32
	AudioObjectSetPropertyData     func(id uint32, addr *audioPropAddr, qsz uint32, q unsafe.Pointer, dataSize uint32, data unsafe.Pointer) int32
	AudioObjectHasProperty         func(id uint32, addr *audioPropAddr) bool

	AudioDeviceCreateIOProcID  func(id uint32, proc uintptr, clientData uintptr, outProcID *uintptr) int32
	AudioDeviceDestroyIOProcID func(id uint32, procID uintptr) int32
	AudioDeviceStart           func(id uint32, procID uintptr) int32
	AudioDeviceStop            func(id uint32, procID uintptr) int32

	CFStringCreateWithBytes func(alloc uintptr, bytes *byte, length int64, encoding uint32, external bool) uintptr
	CFStringGetLength       func(s uintptr) int64
	CFStringGetCString      func(s uintptr, buf *byte, sz int64, encoding uint32) bool
	CFRelease               func(obj uintptr)
}

var (
	coreAudioOnce sync.Once
	coreAudioRef  *coreAudio
	coreAudioErr  error
)

// loadCoreAudio dlopens CoreAudio + CoreFoundation and binds the
// symbols the backend needs. It is memoised; subsequent calls return
// the cached result. Frameworks are never dlclose'd — loading is
// process-wide.
func loadCoreAudio() (*coreAudio, error) {
	coreAudioOnce.Do(func() {
		caHandle, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreAudio.framework/CoreAudio",
			purego.RTLD_LAZY|purego.RTLD_GLOBAL,
		)
		if err != nil {
			coreAudioErr = errors.Join(errors.New("devices: dlopen CoreAudio failed"), err)
			return
		}
		cfHandle, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			purego.RTLD_LAZY|purego.RTLD_GLOBAL,
		)
		if err != nil {
			coreAudioErr = errors.Join(errors.New("devices: dlopen CoreFoundation failed"), err)
			return
		}

		ca := &coreAudio{}
		purego.RegisterLibFunc(&ca.AudioObjectGetPropertyDataSize, caHandle, "AudioObjectGetPropertyDataSize")
		purego.RegisterLibFunc(&ca.AudioObjectGetPropertyData, caHandle, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&ca.AudioObjectSetPropertyData, caHandle, "AudioObjectSetPropertyData")
		purego.RegisterLibFunc(&ca.AudioObjectHasProperty, caHandle, "AudioObjectHasProperty")
		purego.RegisterLibFunc(&ca.AudioDeviceCreateIOProcID, caHandle, "AudioDeviceCreateIOProcID")
		purego.RegisterLibFunc(&ca.AudioDeviceDestroyIOProcID, caHandle, "AudioDeviceDestroyIOProcID")
		purego.RegisterLibFunc(&ca.AudioDeviceStart, caHandle, "AudioDeviceStart")
		purego.RegisterLibFunc(&ca.AudioDeviceStop, caHandle, "AudioDeviceStop")
		purego.RegisterLibFunc(&ca.CFStringCreateWithBytes, cfHandle, "CFStringCreateWithBytes")
		purego.RegisterLibFunc(&ca.CFStringGetLength, cfHandle, "CFStringGetLength")
		purego.RegisterLibFunc(&ca.CFStringGetCString, cfHandle, "CFStringGetCString")
		purego.RegisterLibFunc(&ca.CFRelease, cfHandle, "CFRelease")
		coreAudioRef = ca
	})
	return coreAudioRef, coreAudioErr
}

// cfStringToGo copies the contents of a CFStringRef into a new Go
// string. It releases cfRef on the caller's behalf. Returns the empty
// string if cfRef is zero or the C call reports failure.
//
// The intermediate buffer is allocated in Go memory; because
// CFStringGetCString writes the whole string synchronously and returns
// before we inspect the buffer, the buffer's address does not need to
// remain stable past the call.
func cfStringToGo(ca *coreAudio, cfRef uintptr) string {
	if cfRef == 0 {
		return ""
	}
	defer ca.CFRelease(cfRef)

	length := ca.CFStringGetLength(cfRef)
	if length <= 0 {
		return ""
	}
	// CFStringGetCString wants the buffer size including the NUL
	// terminator. UTF-8 requires up to 4 bytes per UTF-16 unit for
	// surrogate pairs, which is a safe overestimate.
	bufSize := length*4 + 1
	buf := make([]byte, bufSize)
	if !ca.CFStringGetCString(cfRef, &buf[0], bufSize, kCFStringEncodingUTF8) {
		return ""
	}
	// Trim at the first NUL.
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// countChannelsInBufferList interprets data as an AudioBufferList and
// returns the sum of mNumberChannels across every AudioBuffer entry.
// The layout is:
//
//	UInt32 mNumberBuffers;
//	AudioBuffer mBuffers[mNumberBuffers];
//
// with each AudioBuffer being {UInt32, UInt32, void*}. An explicit
// 4-byte padding sits between mNumberBuffers and the first buffer on
// 64-bit targets so that mData aligns to 8 bytes.
func countChannelsInBufferList(data []byte) int {
	if len(data) < 4 {
		return 0
	}
	nBuffers := hostUint32(data[0:4])
	if nBuffers == 0 {
		return 0
	}
	// The first AudioBuffer starts at offset 8 on 64-bit targets —
	// mNumberBuffers (4 bytes) plus 4 bytes of padding added by the
	// compiler so mData is 8-byte aligned.
	const firstBufferOffset = 8
	total := 0
	for i := uint32(0); i < nBuffers; i++ {
		off := firstBufferOffset + int(i)*audioBufferStride
		if off+8 > len(data) {
			return total
		}
		total += int(hostUint32(data[off : off+4]))
	}
	return total
}

// hostUint32 reads a little-endian uint32 from b. All darwin targets
// (amd64, arm64) are little-endian, so reading the native byte order
// is equivalent.
func hostUint32(b []byte) uint32 {
	_ = b[3]
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
