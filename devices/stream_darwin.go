//go:build darwin

package devices

import (
	"math"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

// darwin audio streaming via CoreAudio HAL.
//
// We use AudioDeviceCreateIOProcID to install a C-callable callback on
// a specific device; CoreAudio invokes it on a realtime IO thread at
// the device's buffer-size cadence. The callback receives both input
// and output AudioBufferLists for the device — on an output-only device
// inInputData is empty, and vice versa.
//
// Sample-format negotiation is intentionally minimal: we read the
// device's current float32-interleaved ASBD via
// kAudioDevicePropertyStreamFormat and report that back through
// Stream.Format. Devices that expose non-interleaved or non-float
// streams (rare on consumer hardware) are rejected with
// ErrInvalidFormat rather than silently producing garbage.

// darwinStream owns one active AudioDeviceIOProcID on a specific
// device. It is created in darwinBackend.OpenOutput / OpenInput and
// destroyed in Close.
type darwinStream struct {
	ca *coreAudio

	device    uint32 // AudioDeviceID
	procID    uintptr
	format    StreamFormat
	direction Direction

	outCB OutputCallback
	inCB  InputCallback

	// scratch is reused every callback to convert between the device's
	// native float32 and our float64 API. Length = Frames*Channels.
	scratch []float64

	handle uintptr // index into the callback registry

	started atomic.Bool
	closed  atomic.Bool

	mu sync.Mutex
}

var (
	darwinStreamMu       sync.Mutex
	darwinStreamNextID   uintptr
	darwinStreamRegistry = map[uintptr]*darwinStream{}

	// ioProcFuncPtr is lazily created via purego.NewCallback — the
	// same C trampoline serves every Go-side stream, dispatching via
	// the clientData handle it receives.
	ioProcOnce    sync.Once
	ioProcFuncPtr uintptr
)

// registerDarwinStream stashes s in the registry and returns a handle.
// The handle is stable for the life of the stream.
func registerDarwinStream(s *darwinStream) uintptr {
	darwinStreamMu.Lock()
	defer darwinStreamMu.Unlock()
	darwinStreamNextID++
	h := darwinStreamNextID
	darwinStreamRegistry[h] = s
	return h
}

// unregisterDarwinStream removes h from the registry.
func unregisterDarwinStream(h uintptr) {
	darwinStreamMu.Lock()
	defer darwinStreamMu.Unlock()
	delete(darwinStreamRegistry, h)
}

// lookupDarwinStream resolves a handle back to the stream. Returns nil
// if the stream has already been unregistered; IOProc fires can
// technically arrive after destroy has been requested.
func lookupDarwinStream(h uintptr) *darwinStream {
	darwinStreamMu.Lock()
	defer darwinStreamMu.Unlock()
	return darwinStreamRegistry[h]
}

// ensureIOProcTrampoline builds the shared C-callable IOProc once. The
// function pointer is valid for the process lifetime; purego retains
// it internally.
func ensureIOProcTrampoline() uintptr {
	ioProcOnce.Do(func() {
		ioProcFuncPtr = purego.NewCallback(darwinIOProc)
	})
	return ioProcFuncPtr
}

// darwinIOProc is the shared CoreAudio IOProc. Arguments are passed as
// uintptrs per purego.NewCallback's ABI. Returns 0 (noErr) unconditionally.
//
//	OSStatus IOProc(AudioObjectID inDevice,
//	                const AudioTimeStamp*  inNow,
//	                const AudioBufferList* inInputData,
//	                const AudioTimeStamp*  inInputTime,
//	                AudioBufferList*       outOutputData,
//	                const AudioTimeStamp*  inOutputTime,
//	                void*                  inClientData);
func darwinIOProc(
	_ uintptr, // inDevice
	_ uintptr, // inNow
	inputData uintptr,
	_ uintptr, // inInputTime
	outputData uintptr,
	_ uintptr, // inOutputTime
	clientData uintptr,
) uintptr {
	s := lookupDarwinStream(clientData)
	if s == nil || s.closed.Load() || !s.started.Load() {
		return 0
	}

	switch s.direction {
	case Output:
		if outputData == 0 {
			return 0
		}
		s.fillOutput(outputData)
	case Input:
		if inputData == 0 {
			return 0
		}
		s.drainInput(inputData)
	}
	return 0
}

// fillOutput invokes the user callback and writes the float32-converted
// samples into the first output AudioBuffer. It zero-pads any trailing
// bytes if the device hands us more than we expect.
func (s *darwinStream) fillOutput(bufListPtr uintptr) {
	buf, bytes := firstAudioBuffer(bufListPtr)
	if buf == nil || bytes == 0 {
		return
	}
	frames := bytes / 4 / uint32(s.format.Channels)
	need := int(frames) * s.format.Channels
	if cap(s.scratch) < need {
		// Hot path should never resize — Open pre-allocated for the
		// worst case. If the device suddenly hands us a bigger buffer
		// we widen once and carry on.
		s.scratch = make([]float64, need)
	}
	scratch := s.scratch[:need]
	for i := range scratch {
		scratch[i] = 0
	}
	s.outCB(scratch)

	out := unsafe.Slice((*float32)(unsafe.Pointer(buf)), need)
	for i, v := range scratch {
		// Clamp to [-1, 1] to avoid wrapping on integer pipelines
		// downstream; the device may be running a loudness/limiter
		// unit but we're polite citizens.
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		out[i] = float32(v)
	}
}

// drainInput reads the first input AudioBuffer as float32, converts to
// float64, and hands it to the user callback.
func (s *darwinStream) drainInput(bufListPtr uintptr) {
	buf, bytes := firstAudioBuffer(bufListPtr)
	if buf == nil || bytes == 0 {
		return
	}
	frames := bytes / 4 / uint32(s.format.Channels)
	need := int(frames) * s.format.Channels
	if cap(s.scratch) < need {
		s.scratch = make([]float64, need)
	}
	scratch := s.scratch[:need]
	in := unsafe.Slice((*float32)(unsafe.Pointer(buf)), need)
	for i, v := range in {
		scratch[i] = float64(v)
	}
	s.inCB(scratch)
}

// firstAudioBuffer returns (mData, mDataByteSize) for the first
// AudioBuffer in an AudioBufferList pointer. Returns (nil, 0) when
// the list is empty.
//
// Layout recap: mNumberBuffers (4) + 4 bytes of padding +
// AudioBuffer[0] { mNumberChannels (4), mDataByteSize (4), mData (ptr) }.
func firstAudioBuffer(listPtr uintptr) (data unsafe.Pointer, size uint32) {
	if listPtr == 0 {
		return nil, 0
	}
	nBuffers := *(*uint32)(unsafe.Pointer(listPtr))
	if nBuffers == 0 {
		return nil, 0
	}
	// mDataByteSize is at offset 8 + 4.
	bytes := *(*uint32)(unsafe.Pointer(listPtr + 12))
	// mData is at offset 8 + 8 = 16.
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(listPtr + 16))
	return ptr, bytes
}

// OpenOutput resolves the device UID, validates the format, and
// installs an IOProc that drives cb.
func (b *darwinBackend) OpenOutput(dev Device, format StreamFormat, cb OutputCallback) (Stream, error) {
	return b.openStream(dev, format, Output, cb, nil)
}

// OpenInput mirrors OpenOutput for capture.
func (b *darwinBackend) OpenInput(dev Device, format StreamFormat, cb InputCallback) (Stream, error) {
	return b.openStream(dev, format, Input, nil, cb)
}

func (b *darwinBackend) openStream(dev Device, format StreamFormat, dir Direction, outCB OutputCallback, inCB InputCallback) (Stream, error) {
	if dev.ID == "" {
		return nil, ErrDeviceNotFound
	}
	devID, err := b.deviceIDFromUID(dev.ID)
	if err != nil {
		return nil, err
	}

	scope := kAudioObjectPropertyScopeOutput
	if dir == Input {
		scope = kAudioObjectPropertyScopeInput
	}
	asbd, err := b.currentStreamFormat(devID, scope)
	if err != nil {
		return nil, err
	}
	if asbd.FormatID != kAudioFormatLinearPCM {
		return nil, ErrInvalidFormat
	}
	if asbd.FormatFlags&kAudioFormatFlagIsFloat == 0 {
		return nil, ErrInvalidFormat
	}
	if asbd.FormatFlags&kAudioFormatFlagIsNonInterleaved != 0 {
		return nil, ErrInvalidFormat
	}
	if asbd.BitsPerChannel != 32 {
		return nil, ErrInvalidFormat
	}
	if asbd.ChannelsPerFrame == 0 {
		return nil, ErrInvalidFormat
	}

	frames := format.Frames
	if frames <= 0 {
		frames = int(b.currentBufferFrameSize(devID))
		if frames <= 0 {
			frames = 512
		}
	}

	actual := StreamFormat{
		SampleRate: int(math.Round(asbd.SampleRate)),
		Channels:   int(asbd.ChannelsPerFrame),
		Frames:     frames,
	}

	s := &darwinStream{
		ca:        b.ca,
		device:    devID,
		format:    actual,
		direction: dir,
		outCB:     outCB,
		inCB:      inCB,
		scratch:   make([]float64, frames*actual.Channels),
	}
	s.handle = registerDarwinStream(s)

	trampoline := ensureIOProcTrampoline()
	var procID uintptr
	rc := b.ca.AudioDeviceCreateIOProcID(devID, trampoline, s.handle, &procID)
	if rc != 0 || procID == 0 {
		unregisterDarwinStream(s.handle)
		return nil, ErrInvalidFormat
	}
	s.procID = procID
	return s, nil
}

// deviceIDFromUID resolves a UID string to a numeric AudioDeviceID via
// kAudioHardwarePropertyTranslateUIDToDevice.
func (b *darwinBackend) deviceIDFromUID(uid string) (uint32, error) {
	bytes := []byte(uid)
	var cfRef uintptr
	if len(bytes) == 0 {
		return 0, ErrDeviceNotFound
	}
	cfRef = b.ca.CFStringCreateWithBytes(0, &bytes[0], int64(len(bytes)), kCFStringEncodingUTF8, false)
	if cfRef == 0 {
		return 0, ErrDeviceNotFound
	}
	defer b.ca.CFRelease(cfRef)

	addr := audioPropAddr{
		Selector: kAudioHardwarePropertyTranslateUIDToDevice,
		Scope:    kAudioObjectPropertyScopeGlobal,
		Element:  kAudioObjectPropertyElementMain,
	}
	var devID uint32
	size := uint32(4)
	qsize := uint32(unsafe.Sizeof(cfRef))
	rc := b.ca.AudioObjectGetPropertyData(
		kAudioObjectSystemObject, &addr,
		qsize, unsafe.Pointer(&cfRef),
		&size, unsafe.Pointer(&devID),
	)
	if rc != 0 || devID == 0 {
		return 0, ErrDeviceNotFound
	}
	return devID, nil
}

// currentStreamFormat reads the device-level ASBD.
func (b *darwinBackend) currentStreamFormat(id, scope uint32) (audioStreamBasicDescription, error) {
	addr := audioPropAddr{
		Selector: kAudioDevicePropertyStreamFormat,
		Scope:    scope,
		Element:  kAudioObjectPropertyElementMain,
	}
	var asbd audioStreamBasicDescription
	size := uint32(unsafe.Sizeof(asbd))
	if rc := b.ca.AudioObjectGetPropertyData(id, &addr, 0, nil, &size, unsafe.Pointer(&asbd)); rc != 0 {
		return asbd, ErrInvalidFormat
	}
	return asbd, nil
}

// currentBufferFrameSize reads the device's current I/O buffer size in
// frames. Zero is returned (and treated as "use a sensible default")
// on any failure.
func (b *darwinBackend) currentBufferFrameSize(id uint32) uint32 {
	addr := audioPropAddr{
		Selector: kAudioDevicePropertyBufferFrameSize,
		Scope:    kAudioObjectPropertyScopeGlobal,
		Element:  kAudioObjectPropertyElementMain,
	}
	var frames uint32
	size := uint32(4)
	if rc := b.ca.AudioObjectGetPropertyData(id, &addr, 0, nil, &size, unsafe.Pointer(&frames)); rc != 0 {
		return 0
	}
	return frames
}

// Start begins IOProc delivery. No-op when already running.
func (s *darwinStream) Start() error {
	if s.closed.Load() {
		return ErrStreamClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started.Load() {
		return nil
	}
	if rc := s.ca.AudioDeviceStart(s.device, s.procID); rc != 0 {
		return ErrInvalidFormat
	}
	s.started.Store(true)
	return nil
}

// Stop halts IOProc delivery. No-op when already stopped.
func (s *darwinStream) Stop() error {
	if s.closed.Load() {
		return ErrStreamClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started.Load() {
		return nil
	}
	if rc := s.ca.AudioDeviceStop(s.device, s.procID); rc != 0 {
		return ErrInvalidFormat
	}
	s.started.Store(false)
	return nil
}

// Close stops the stream and destroys the IOProcID. Late IOProc fires
// are tolerated — the registry lookup returns nil and the callback is
// a no-op.
func (s *darwinStream) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started.Load() {
		_ = s.ca.AudioDeviceStop(s.device, s.procID)
		s.started.Store(false)
	}
	if s.procID != 0 {
		_ = s.ca.AudioDeviceDestroyIOProcID(s.device, s.procID)
		s.procID = 0
	}
	unregisterDarwinStream(s.handle)
	return nil
}

// Format returns the negotiated device format.
func (s *darwinStream) Format() StreamFormat { return s.format }
