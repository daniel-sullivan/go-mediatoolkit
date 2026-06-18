//go:build windows

package devices

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows audio streaming via WASAPI in shared event-driven mode.
//
// Flow:
//   1. IMMDeviceEnumerator::GetDevice(id)      — look the endpoint up.
//   2. IMMDevice::Activate(IID_IAudioClient)   — get a client pointer.
//   3. IAudioClient::GetMixFormat              — engine-native format.
//   4. IAudioClient::Initialize(SHARED|EVENTCALLBACK|AUTOCONVERTPCM)
//      so the OS resamples our requested rate to the engine rate.
//   5. IAudioClient::SetEventHandle(event)     — handle to wait on.
//   6. IAudioClient::GetService(Render|Capture) — per-direction svc.
//   7. Dedicated pump goroutine: WaitForSingleObject → Get/ReleaseBuffer
//      until a stop flag is set.
//
// Every WASAPI pointer is used from the same OS-locked goroutine that
// Activate'd it, with its own CoInitializeEx(MTA) apartment. That
// avoids cross-apartment marshalling and keeps the hot loop syscall-
// only.

// iAudioClient — minimal WASAPI surface. See audioclient.h.
type iAudioClient struct {
	vtbl *iAudioClientVtbl
}

type iAudioClientVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr

	Initialize        uintptr
	GetBufferSize     uintptr
	GetStreamLatency  uintptr
	GetCurrentPadding uintptr
	IsFormatSupported uintptr
	GetMixFormat      uintptr
	GetDevicePeriod   uintptr
	Start             uintptr
	Stop              uintptr
	Reset             uintptr
	SetEventHandle    uintptr
	GetService        uintptr
}

// iAudioRenderClient is the playback-side buffer producer.
type iAudioRenderClient struct {
	vtbl *iAudioRenderClientVtbl
}

type iAudioRenderClientVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr

	GetBuffer     uintptr
	ReleaseBuffer uintptr
}

// iAudioCaptureClient is the capture-side buffer consumer.
type iAudioCaptureClient struct {
	vtbl *iAudioCaptureClientVtbl
}

type iAudioCaptureClientVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr

	GetBuffer         uintptr
	ReleaseBuffer     uintptr
	GetNextPacketSize uintptr
}

// waveformatextensible extends WAVEFORMATEX with the fields needed to
// describe non-16-bit PCM or multi-channel layouts. This is the form
// IAudioClient::GetMixFormat returns in shared mode.
//
//	typedef struct {
//	  WAVEFORMATEX Format;
//	  union { WORD wValidBitsPerSample; WORD wSamplesPerBlock; WORD wReserved; } Samples;
//	  DWORD dwChannelMask;
//	  GUID  SubFormat;
//	} WAVEFORMATEXTENSIBLE;
type waveformatextensible struct {
	Format      waveformatex
	Samples     uint16
	ChannelMask uint32
	SubFormat   windows.GUID
}

// GetMixFormat returns the shared-mode engine format for this device.
// Windows allocates the WAVEFORMATEX via CoTaskMemAlloc; the caller
// must CoTaskMemFree it.
func (c *iAudioClient) GetMixFormat() (*waveformatex, error) {
	var wf *waveformatex
	r, _, _ := syscall.SyscallN(c.vtbl.GetMixFormat,
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&wf)))
	if err := errFromHRESULT("IAudioClient::GetMixFormat", r); err != nil {
		return nil, err
	}
	return wf, nil
}

// Initialize configures the stream. bufferDuration is in 100ns units
// (hns); 0 asks the engine for the default period, which is what
// shared mode wants. sharedMode and flags are standard AUDCLNT_ values.
func (c *iAudioClient) Initialize(sharedMode uint32, flags uint32, bufferDuration int64, periodicity int64, format *waveformatex, sessionGUID *windows.GUID) error {
	r, _, _ := syscall.SyscallN(c.vtbl.Initialize,
		uintptr(unsafe.Pointer(c)),
		uintptr(sharedMode),
		uintptr(flags),
		uintptr(bufferDuration),
		uintptr(periodicity),
		uintptr(unsafe.Pointer(format)),
		uintptr(unsafe.Pointer(sessionGUID)))
	return errFromHRESULT("IAudioClient::Initialize", r)
}

// GetBufferSize returns the total endpoint buffer size in frames.
func (c *iAudioClient) GetBufferSize() (uint32, error) {
	var frames uint32
	r, _, _ := syscall.SyscallN(c.vtbl.GetBufferSize,
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&frames)))
	if err := errFromHRESULT("IAudioClient::GetBufferSize", r); err != nil {
		return 0, err
	}
	return frames, nil
}

// GetCurrentPadding returns how many frames of valid data are already
// queued in the endpoint buffer — used by renderers to compute how
// much new audio they can produce.
func (c *iAudioClient) GetCurrentPadding() (uint32, error) {
	var pad uint32
	r, _, _ := syscall.SyscallN(c.vtbl.GetCurrentPadding,
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&pad)))
	if err := errFromHRESULT("IAudioClient::GetCurrentPadding", r); err != nil {
		return 0, err
	}
	return pad, nil
}

// SetEventHandle attaches a Windows event handle signalled when the
// endpoint is ready for another buffer turnaround.
func (c *iAudioClient) SetEventHandle(h windows.Handle) error {
	r, _, _ := syscall.SyscallN(c.vtbl.SetEventHandle,
		uintptr(unsafe.Pointer(c)),
		uintptr(h))
	return errFromHRESULT("IAudioClient::SetEventHandle", r)
}

// GetService retrieves a companion interface — IAudioRenderClient for
// playback or IAudioCaptureClient for capture.
func (c *iAudioClient) GetService(iid *windows.GUID) (unsafe.Pointer, error) {
	var ppv unsafe.Pointer
	r, _, _ := syscall.SyscallN(c.vtbl.GetService,
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&ppv)))
	if err := errFromHRESULT("IAudioClient::GetService", r); err != nil {
		return nil, err
	}
	return ppv, nil
}

// Start begins audio transport.
func (c *iAudioClient) Start() error {
	r, _, _ := syscall.SyscallN(c.vtbl.Start, uintptr(unsafe.Pointer(c)))
	return errFromHRESULT("IAudioClient::Start", r)
}

// Stop halts audio transport; the endpoint buffer is preserved and
// Start can be used to resume.
func (c *iAudioClient) Stop() error {
	r, _, _ := syscall.SyscallN(c.vtbl.Stop, uintptr(unsafe.Pointer(c)))
	return errFromHRESULT("IAudioClient::Stop", r)
}

// Release drops the reference count.
func (c *iAudioClient) Release() uint32 {
	r, _, _ := syscall.SyscallN(c.vtbl.Release, uintptr(unsafe.Pointer(c)))
	return uint32(r)
}

// GetBuffer locks n frames of output space and returns the pointer.
func (r *iAudioRenderClient) GetBuffer(frames uint32) (unsafe.Pointer, error) {
	var data unsafe.Pointer
	rc, _, _ := syscall.SyscallN(r.vtbl.GetBuffer,
		uintptr(unsafe.Pointer(r)),
		uintptr(frames),
		uintptr(unsafe.Pointer(&data)))
	if err := errFromHRESULT("IAudioRenderClient::GetBuffer", rc); err != nil {
		return nil, err
	}
	return data, nil
}

// ReleaseBuffer commits n frames of data to the endpoint. flags of 0
// means the data is valid; AUDCLNT_BUFFERFLAGS_SILENT = 0x2 means zero it.
func (r *iAudioRenderClient) ReleaseBuffer(frames uint32, flags uint32) error {
	rc, _, _ := syscall.SyscallN(r.vtbl.ReleaseBuffer,
		uintptr(unsafe.Pointer(r)),
		uintptr(frames),
		uintptr(flags))
	return errFromHRESULT("IAudioRenderClient::ReleaseBuffer", rc)
}

// Release drops the reference count.
func (r *iAudioRenderClient) Release() uint32 {
	rc, _, _ := syscall.SyscallN(r.vtbl.Release, uintptr(unsafe.Pointer(r)))
	return uint32(rc)
}

// GetBuffer returns the next captured packet's data pointer, frame
// count, and flags. It may return 0 frames if no data is available.
func (c *iAudioCaptureClient) GetBuffer() (data unsafe.Pointer, frames uint32, flags uint32, err error) {
	var devicePosition, qpcPosition uint64
	r, _, _ := syscall.SyscallN(c.vtbl.GetBuffer,
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&frames)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&devicePosition)),
		uintptr(unsafe.Pointer(&qpcPosition)))
	if e := errFromHRESULT("IAudioCaptureClient::GetBuffer", r); e != nil {
		return nil, 0, 0, e
	}
	return
}

// ReleaseBuffer commits the preceding GetBuffer — frames should match.
func (c *iAudioCaptureClient) ReleaseBuffer(frames uint32) error {
	r, _, _ := syscall.SyscallN(c.vtbl.ReleaseBuffer,
		uintptr(unsafe.Pointer(c)),
		uintptr(frames))
	return errFromHRESULT("IAudioCaptureClient::ReleaseBuffer", r)
}

// GetNextPacketSize returns the frame count of the next packet without
// dequeueing it. Zero means no data available yet.
func (c *iAudioCaptureClient) GetNextPacketSize() (uint32, error) {
	var n uint32
	r, _, _ := syscall.SyscallN(c.vtbl.GetNextPacketSize,
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&n)))
	if err := errFromHRESULT("IAudioCaptureClient::GetNextPacketSize", r); err != nil {
		return 0, err
	}
	return n, nil
}

// Release drops the reference count.
func (c *iAudioCaptureClient) Release() uint32 {
	r, _, _ := syscall.SyscallN(c.vtbl.Release, uintptr(unsafe.Pointer(c)))
	return uint32(r)
}

// wasapiStream is the shared state for a single WASAPI playback or
// capture session. Its pump goroutine owns all COM pointers.
type wasapiStream struct {
	direction Direction

	format    StreamFormat
	mixFormat *waveformatex
	isFloat   bool

	client  *iAudioClient
	render  *iAudioRenderClient
	capture *iAudioCaptureClient

	event windows.Handle

	out OutputCallback
	in  InputCallback

	// scratch holds one fixed-size frame in float64 for the user cb.
	scratch []float64

	// pumpCmd carries Start/Stop/Close commands to the pump goroutine.
	pumpCmd  chan wasapiCmd
	pumpDone chan struct{}

	started atomic.Bool
	closed  atomic.Bool

	mu sync.Mutex
}

type wasapiCmd int

const (
	cmdStart wasapiCmd = iota
	cmdStop
	cmdClose
)

// OpenOutput sets up a shared-mode event-driven render stream on dev.
func (b *wasapiBackend) OpenOutput(dev Device, format StreamFormat, cb OutputCallback) (Stream, error) {
	return b.openStream(dev, format, Output, cb, nil)
}

// OpenInput sets up a shared-mode event-driven capture stream on dev.
func (b *wasapiBackend) OpenInput(dev Device, format StreamFormat, cb InputCallback) (Stream, error) {
	return b.openStream(dev, format, Input, nil, cb)
}

func (b *wasapiBackend) openStream(dev Device, format StreamFormat, dir Direction, outCB OutputCallback, inCB InputCallback) (Stream, error) {
	if format.SampleRate <= 0 || format.Channels <= 0 {
		return nil, ErrInvalidFormat
	}

	var immDev *iMMDevice
	var ferr error
	b.com.run(func() {
		immDev, ferr = b.enumerator.GetDevice(dev.ID)
	})
	if ferr != nil {
		return nil, ErrDeviceNotFound
	}

	s := &wasapiStream{
		direction: dir,
		out:       outCB,
		in:        inCB,
		pumpCmd:   make(chan wasapiCmd, 4),
		pumpDone:  make(chan struct{}),
	}

	initErr := make(chan error, 1)
	go s.runPump(immDev, format, initErr)
	if err := <-initErr; err != nil {
		// Pump signals init failure and exits; it already released
		// anything it allocated on the failure path.
		<-s.pumpDone
		return nil, err
	}
	return s, nil
}

// runPump is the dedicated OS-thread-locked goroutine that owns every
// WASAPI pointer for the stream's lifetime. It reports init success or
// failure via initErr, then services commands from pumpCmd until Close.
func (s *wasapiStream) runPump(immDev *iMMDevice, requested StreamFormat, initErr chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(s.pumpDone)

	state, err := coInitialize()
	if err != nil {
		immDev.Release()
		initErr <- err
		return
	}
	defer coUninitialize(state)

	if err := s.setup(immDev, requested); err != nil {
		initErr <- err
		return
	}
	initErr <- nil

	defer s.teardown()

	for cmd := range s.pumpCmd {
		switch cmd {
		case cmdStart:
			if err := s.client.Start(); err != nil {
				continue
			}
			s.started.Store(true)
			s.runIO()
			s.started.Store(false)
		case cmdStop:
			// runIO returned on its own — nothing to do.
		case cmdClose:
			return
		}
	}
}

// setup runs inside the pump goroutine's apartment. It Activate's an
// IAudioClient on the device, negotiates the mix format, initialises
// shared event-driven mode, and fetches the render or capture service.
// On any failure it releases every pointer it acquired.
func (s *wasapiStream) setup(immDev *iMMDevice, requested StreamFormat) error {
	defer immDev.Release()

	ppv, err := immDev.Activate(&iidIAudioClient, clsctxAll)
	if err != nil {
		return err
	}
	client := (*iAudioClient)(ppv)

	wf, err := client.GetMixFormat()
	if err != nil {
		client.Release()
		return err
	}
	// The engine format controls the frame size of our pump. We do not
	// try to override it — AUTOCONVERTPCM + SRC_DEFAULT_QUALITY lets
	// the OS resample if the caller requested a different rate, but
	// the buffers we fill are still in the engine's layout.
	isFloat, ok := isFloatFormat(wf)
	if !ok {
		windows.CoTaskMemFree(unsafe.Pointer(wf))
		client.Release()
		return ErrInvalidFormat
	}

	flags := audclntStreamflagsEventCallback
	if requested.SampleRate > 0 && int(wf.nSamplesPerSec) != requested.SampleRate {
		flags |= audclntStreamflagsAutoconvertPCM | audclntStreamflagsSRCDefaultQuality
	}

	// Shared mode: pass 0 for buffer duration and periodicity so the
	// engine picks its default (~10ms on modern Windows).
	if err := client.Initialize(audclntShareModeShared, flags, 0, 0, wf, nil); err != nil {
		windows.CoTaskMemFree(unsafe.Pointer(wf))
		client.Release()
		return err
	}

	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		windows.CoTaskMemFree(unsafe.Pointer(wf))
		client.Release()
		return err
	}
	if err := client.SetEventHandle(event); err != nil {
		windows.CloseHandle(event)
		windows.CoTaskMemFree(unsafe.Pointer(wf))
		client.Release()
		return err
	}

	bufferFrames, err := client.GetBufferSize()
	if err != nil {
		windows.CloseHandle(event)
		windows.CoTaskMemFree(unsafe.Pointer(wf))
		client.Release()
		return err
	}

	var iid windows.GUID
	switch s.direction {
	case Output:
		iid = iidIAudioRenderClient
	case Input:
		iid = iidIAudioCaptureClient
	}
	ppv2, err := client.GetService(&iid)
	if err != nil {
		windows.CloseHandle(event)
		windows.CoTaskMemFree(unsafe.Pointer(wf))
		client.Release()
		return err
	}

	channels := int(wf.nChannels)
	s.client = client
	s.event = event
	s.mixFormat = wf
	s.isFloat = isFloat
	s.format = StreamFormat{
		SampleRate: int(wf.nSamplesPerSec),
		Channels:   channels,
		Frames:     int(bufferFrames),
	}
	s.scratch = make([]float64, int(bufferFrames)*channels)
	switch s.direction {
	case Output:
		s.render = (*iAudioRenderClient)(ppv2)
	case Input:
		s.capture = (*iAudioCaptureClient)(ppv2)
	}
	return nil
}

// teardown releases every COM pointer and OS handle owned by the
// pump. It runs after the pump loop exits, so nothing else is holding
// any of these references.
func (s *wasapiStream) teardown() {
	if s.started.Load() {
		_ = s.client.Stop()
	}
	if s.render != nil {
		s.render.Release()
		s.render = nil
	}
	if s.capture != nil {
		s.capture.Release()
		s.capture = nil
	}
	if s.client != nil {
		s.client.Release()
		s.client = nil
	}
	if s.event != 0 {
		windows.CloseHandle(s.event)
		s.event = 0
	}
	if s.mixFormat != nil {
		windows.CoTaskMemFree(unsafe.Pointer(s.mixFormat))
		s.mixFormat = nil
	}
}

// runIO is the transport loop. It blocks on the event handle between
// turnarounds; each wake services one buffer (render: produce, capture:
// drain all ready packets). Exits when stopped via cmdStop or on
// close.
func (s *wasapiStream) runIO() {
	total := int(s.format.Frames)

	for {
		// Wait up to 2 seconds — a safety net so a stuck endpoint
		// doesn't deadlock Close forever.
		r, err := windows.WaitForSingleObject(s.event, 2000)
		if err != nil {
			return
		}
		if r == uint32(windows.WAIT_TIMEOUT) {
			// No data ready; check for shutdown and loop.
			if !s.started.Load() || s.closed.Load() {
				return
			}
			continue
		}
		if !s.started.Load() || s.closed.Load() {
			return
		}
		if s.direction == Output {
			if err := s.renderOnce(uint32(total)); err != nil {
				return
			}
		} else {
			if err := s.captureDrain(); err != nil {
				return
			}
		}
	}
}

// renderOnce asks the user callback for the currently-available frames
// and writes them into the endpoint buffer.
func (s *wasapiStream) renderOnce(total uint32) error {
	pad, err := s.client.GetCurrentPadding()
	if err != nil {
		return err
	}
	available := total - pad
	if available == 0 {
		return nil
	}

	data, err := s.render.GetBuffer(available)
	if err != nil {
		return err
	}

	channels := int(s.mixFormat.nChannels)
	need := int(available) * channels
	if cap(s.scratch) < need {
		s.scratch = make([]float64, need)
	}
	scratch := s.scratch[:need]
	for i := range scratch {
		scratch[i] = 0
	}
	s.out(scratch)

	writeFloat32Interleaved(data, scratch, s.mixFormat, channels, int(available))
	return s.render.ReleaseBuffer(available, 0)
}

// captureDrain pulls every ready packet out of the capture client,
// converts to float64, and forwards to the user callback.
func (s *wasapiStream) captureDrain() error {
	channels := int(s.mixFormat.nChannels)
	for {
		next, err := s.capture.GetNextPacketSize()
		if err != nil {
			return err
		}
		if next == 0 {
			return nil
		}
		data, frames, flags, err := s.capture.GetBuffer()
		if err != nil {
			return err
		}
		need := int(frames) * channels
		if cap(s.scratch) < need {
			s.scratch = make([]float64, need)
		}
		scratch := s.scratch[:need]
		readFloat32Interleaved(scratch, data, s.mixFormat, channels, int(frames), flags)
		s.in(scratch)
		if err := s.capture.ReleaseBuffer(frames); err != nil {
			return err
		}
	}
}

// writeFloat32Interleaved converts interleaved float64 samples into
// the endpoint format. Today we only support float32 endpoints, which
// is what Windows' shared-mode engine always serves up.
func writeFloat32Interleaved(dst unsafe.Pointer, src []float64, wf *waveformatex, channels int, frames int) {
	if wf.wBitsPerSample != 32 {
		return
	}
	out := unsafe.Slice((*float32)(dst), frames*channels)
	for i, v := range src {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		out[i] = float32(v)
	}
}

// readFloat32Interleaved is the capture-side counterpart. The SILENT
// flag means the OS is telling us to treat the buffer as zero.
func readFloat32Interleaved(dst []float64, src unsafe.Pointer, wf *waveformatex, channels int, frames int, flags uint32) {
	const audclntBufferflagsSilent = 0x2
	if flags&audclntBufferflagsSilent != 0 {
		for i := range dst {
			dst[i] = 0
		}
		return
	}
	if wf.wBitsPerSample != 32 {
		for i := range dst {
			dst[i] = 0
		}
		return
	}
	in := unsafe.Slice((*float32)(src), frames*channels)
	for i, v := range in {
		dst[i] = float64(v)
	}
}

// isFloatFormat inspects a WAVEFORMATEX / WAVEFORMATEXTENSIBLE and
// reports whether it describes a float32 interleaved PCM stream.
func isFloatFormat(wf *waveformatex) (bool, bool) {
	switch wf.wFormatTag {
	case waveFormatIEEEFloat:
		return wf.wBitsPerSample == 32, true
	case waveFormatExtensible:
		if wf.cbSize < 22 {
			return false, false
		}
		ext := (*waveformatextensible)(unsafe.Pointer(wf))
		return ext.SubFormat == ksDataFormatSubtypeIEEEFloat && wf.wBitsPerSample == 32, true
	case waveFormatPCM:
		return false, true
	}
	return false, false
}

// Start posts a Start command to the pump. Idempotent.
func (s *wasapiStream) Start() error {
	if s.closed.Load() {
		return ErrStreamClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started.Load() {
		return nil
	}
	s.pumpCmd <- cmdStart
	return nil
}

// Stop posts a Stop command, which makes runIO exit. The pump
// goroutine stays alive awaiting Close or another Start.
func (s *wasapiStream) Stop() error {
	if s.closed.Load() {
		return ErrStreamClosed
	}
	if !s.started.Load() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Tell the pump's IO loop to exit at the next iteration.
	s.started.Store(false)
	// Nudge the waiter so WaitForSingleObject returns immediately.
	if s.event != 0 {
		windows.SetEvent(s.event)
	}
	if s.client != nil {
		_ = s.client.Stop()
	}
	return nil
}

// Close tears the stream down entirely. The pump goroutine releases
// every COM pointer before exiting.
func (s *wasapiStream) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.mu.Lock()
	started := s.started.Load()
	s.started.Store(false)
	if s.event != 0 {
		windows.SetEvent(s.event)
	}
	s.mu.Unlock()
	if started && s.client != nil {
		_ = s.client.Stop()
	}
	s.pumpCmd <- cmdClose
	close(s.pumpCmd)
	<-s.pumpDone
	return nil
}

// Format returns the format the WASAPI engine actually negotiated.
func (s *wasapiStream) Format() StreamFormat { return s.format }
