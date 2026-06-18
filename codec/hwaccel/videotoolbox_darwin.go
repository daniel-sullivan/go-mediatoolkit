//go:build darwin

// The videotoolbox backend: a hwaccel.Backend driving Apple
// VideoToolbox via the purego bindings in vtbindings_darwin.go. Encode
// uses VTCompressionSession; the capability probe uses
// VTCopySupportedPropertyDictionaryForEncoderSpecification (encode) and
// VTIsHardwareDecodeSupported (decode).
//
// # Output packaging
//
// VideoToolbox emits length-prefixed (AVCC/HVCC) NAL units in the
// CMSampleBuffer's CMBlockBuffer, with the parameter sets carried out
// of band in the format description. The encoder converts both to
// Annex-B (start-code prefixed) and prefixes every keyframe with the
// parameter sets, so each keyframe Packet is independently decodable —
// the form an NVR / elementary-stream consumer expects.

package hwaccel

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"go-mediatoolkit/video"
)

// vtBackend is the VideoToolbox hwaccel.Backend. It is stateless once
// the frameworks are loaded; sessions are per-encoder.
type vtBackend struct {
	lib *vtLib
}

// newVTBackend loads the frameworks and returns a backend, or an error
// if dlopen fails (never on a normal macOS install).
func newVTBackend() (*vtBackend, error) {
	lib, err := loadVT()
	if err != nil {
		return nil, err
	}
	return &vtBackend{lib: lib}, nil
}

func (b *vtBackend) Name() string { return "videotoolbox" }

// Available reports whether the frameworks loaded. Cheap: the dlopen
// result is memoised in loadVT.
func (b *vtBackend) Available() bool {
	_, err := loadVT()
	return err == nil
}

// Probe queries VideoToolbox for encode support (per codec, via
// VTCopySupportedPropertyDictionaryForEncoderSpecification returning a
// non-empty property dictionary) and decode support (via
// VTIsHardwareDecodeSupported). Profiles are reported as the canonical
// set VideoToolbox accepts for each codec.
func (b *vtBackend) Probe() (Capabilities, error) {
	caps := Capabilities{Backend: b.Name()}
	for _, c := range []video.Codec{video.H264, video.H265, video.VP9, video.AV1} {
		ct, ok := cmCodecType(c)
		if !ok {
			continue
		}
		// VideoToolbox on Apple silicon has no VP9/AV1 hardware ENCODER (only
		// H.264/H.265). Encode is probed for H.264/H.265 only; VP9/AV1 are
		// decode-only here.
		var enc bool
		if c == video.H264 || c == video.H265 {
			enc = b.encodeSupported(ct)
		}
		dec := b.lib.VTIsHardwareDecodeSupported(ct)
		if !enc && !dec {
			continue
		}
		caps.Codecs = append(caps.Codecs, CodecCapability{
			Codec:    c,
			Encode:   enc,
			Decode:   dec,
			Profiles: vtProfiles(c),
		})
	}
	return caps, nil
}

// encodeSupported probes a single codec by attempting to create a
// throwaway compression session at a representative resolution. A
// successful create means VideoToolbox has an encoder for the codec on
// this host. (The older spec-level
// VTCopySupportedPropertyDictionaryForEncoderSpecification probe is no
// longer an exported symbol on recent macOS, so session creation is the
// truthful signal.) The session is invalidated immediately.
func (b *vtBackend) encodeSupported(codecType uint32) bool {
	var session uintptr
	st := b.lib.VTCompressionSessionCreate(
		0, 1920, 1080, codecType, 0, 0, 0, 0, 0, &session)
	if st != noErr || session == 0 {
		return false
	}
	b.lib.VTCompressionSessionInvalidate(session)
	b.lib.CFRelease(session)
	return true
}

// NewEncoder constructs a VideoToolbox compression-session encoder.
func (b *vtBackend) NewEncoder(cfg Config) (Encoder, error) {
	if err := cfg.validateEncode(); err != nil {
		return nil, err
	}
	codecType, ok := cmCodecType(cfg.Codec)
	if !ok {
		return nil, ErrUnsupportedCodec
	}
	return newVTEncoder(b.lib, cfg, codecType)
}

// NewDecoder constructs a VideoToolbox decompression-session decoder.
// The session is built lazily from the parameter sets in the first
// keyframe; see videotoolbox_decode_darwin.go.
func (b *vtBackend) NewDecoder(cfg Config) (Decoder, error) {
	return b.newDecoder(cfg)
}

// cmCodecType maps a video.Codec to its CMVideoCodecType FourCC.
func cmCodecType(c video.Codec) (uint32, bool) {
	switch c {
	case video.H264:
		return kCMVideoCodecTypeH264, true
	case video.H265:
		return kCMVideoCodecTypeHEVC, true
	case video.VP9:
		return kCMVideoCodecTypeVP9, true
	case video.AV1:
		return kCMVideoCodecTypeAV1, true
	default:
		return 0, false
	}
}

// vtProfiles returns the profile tokens the backend advertises for a
// codec. These match the ProfileLevel keys VideoToolbox accepts.
func vtProfiles(c video.Codec) []string {
	switch c {
	case video.H264:
		return []string{"baseline", "main", "high"}
	case video.H265:
		return []string{"main", "main10"}
	default:
		return nil
	}
}

// ---- encoder ----------------------------------------------------------

// encoderRegistry maps the integer refcon passed to VideoToolbox back to
// the owning vtEncoder, so the C output callback never receives a Go
// pointer. Entries are added on session create and removed on Close.
var (
	encoderRegistryMu  sync.Mutex
	encoderRegistry            = map[uintptr]*vtEncoder{}
	encoderNextID      uintptr = 1
	outputCallbackPtr  uintptr // lazily-created purego.NewCallback trampoline
	outputCallbackOnce sync.Once
)

// vtEncoder drives a single VTCompressionSession. Encode is synchronous:
// each frame is submitted and CompleteFrames forces the encoder to emit
// it, so the output callback has run and pending is populated before
// Encode returns. Not safe for concurrent use.
type vtEncoder struct {
	lib       *vtLib
	cfg       Config
	codec     video.Codec
	codecType uint32
	session   uintptr
	refcon    uintptr

	frameIdx int64
	pending  []video.Packet
	cbErr    int32 // last non-zero OSStatus seen in the callback
	closed   bool
}

// newVTEncoder creates and configures a compression session.
func newVTEncoder(lib *vtLib, cfg Config, codecType uint32) (*vtEncoder, error) {
	e := &vtEncoder{lib: lib, cfg: cfg, codec: cfg.Codec, codecType: codecType}

	// Register so the callback can find us via the refcon int.
	encoderRegistryMu.Lock()
	id := encoderNextID
	encoderNextID++
	encoderRegistry[id] = e
	encoderRegistryMu.Unlock()
	e.refcon = id

	cb := outputCallbackTrampoline()

	var session uintptr
	st := lib.VTCompressionSessionCreate(
		0, // default allocator
		int32(cfg.Width), int32(cfg.Height),
		codecType,
		0, // encoderSpecification: nil = let VT pick hardware if available
		0, // sourceImageBufferAttributes: nil
		0, // compressedDataAllocator: default
		cb, id, &session,
	)
	if st != noErr || session == 0 {
		e.unregister()
		return nil, fmt.Errorf("%w: VTCompressionSessionCreate OSStatus=%d", ErrBackendFailure, st)
	}
	e.session = session

	if err := e.configure(); err != nil {
		lib.VTCompressionSessionInvalidate(session)
		lib.CFRelease(session)
		e.unregister()
		return nil, err
	}
	lib.VTCompressionSessionPrepareToEncodeFrames(session)
	return e, nil
}

// configure applies the Config to the session via VTSessionSetProperty.
func (e *vtEncoder) configure() error {
	l := e.lib

	// Real-time off: we want best quality, not low latency, for a
	// transcode pipeline. (Real-time can be a future option.)
	if err := e.setProp(l.kVTCompressionPropertyKey_RealTime, l.kCFBooleanFalse, "RealTime"); err != nil {
		return err
	}
	// No B-frames: keeps PTS == DTS so the Packet timing stays simple.
	if err := e.setProp(l.kVTCompressionPropertyKey_AllowFrameReordering, l.kCFBooleanFalse, "AllowFrameReordering"); err != nil {
		return err
	}
	if e.cfg.Bitrate > 0 {
		v := l.cfNumberInt32(int32(e.cfg.Bitrate))
		defer l.CFRelease(v)
		if err := e.setProp(l.kVTCompressionPropertyKey_AverageBitRate, v, "AverageBitRate"); err != nil {
			return err
		}
	}
	if e.cfg.KeyframeInterval > 0 {
		v := l.cfNumberInt32(int32(e.cfg.KeyframeInterval))
		defer l.CFRelease(v)
		if err := e.setProp(l.kVTCompressionPropertyKey_MaxKeyFrameInterval, v, "MaxKeyFrameInterval"); err != nil {
			return err
		}
	}
	fr := l.cfNumberFloat64(e.cfg.frameRate())
	defer l.CFRelease(fr)
	if err := e.setProp(l.kVTCompressionPropertyKey_ExpectedFrameRate, fr, "ExpectedFrameRate"); err != nil {
		return err
	}
	return nil
}

// setProp sets one session property, releasing nothing (caller owns the
// value). A non-zero OSStatus other than for an unsupported optional
// key is fatal.
func (e *vtEncoder) setProp(key, value uintptr, name string) error {
	if key == 0 {
		return nil // key symbol not resolved; treat as no-op
	}
	st := e.lib.VTSessionSetProperty(e.session, key, value)
	if st != noErr {
		return fmt.Errorf("%w: VTSessionSetProperty(%s) OSStatus=%d", ErrBackendFailure, name, st)
	}
	return nil
}

// Encode submits one raw frame, forces the encoder to emit it, and
// returns the resulting packets (parameter sets are folded into each
// keyframe). NV12 and I420 inputs are accepted.
func (e *vtEncoder) Encode(f video.Frame) ([]video.Packet, error) {
	if e.closed {
		return nil, ErrClosed
	}
	pixFmt, ok := cvPixelFormat(f.PixelFormat)
	if !ok {
		return nil, ErrUnsupportedPixelFormat
	}
	pb, err := e.wrapPixelBuffer(f, pixFmt)
	if err != nil {
		return nil, err
	}
	defer e.lib.CVPixelBufferRelease(pb)

	fr := e.cfg.frameRate()
	pts := makeCMTime(e.frameIdx, int32(roundRate(fr)))
	dur := makeCMTime(1, int32(roundRate(fr)))
	e.frameIdx++

	var infoFlags uint32
	st := e.lib.VTCompressionSessionEncodeFrame(e.session, pb, pts, dur, 0, 0, &infoFlags)
	if st != noErr {
		return nil, fmt.Errorf("%w: VTCompressionSessionEncodeFrame OSStatus=%d", ErrBackendFailure, st)
	}
	// Force this frame out so the callback runs synchronously. We
	// complete up to the just-submitted PTS only.
	st = e.lib.VTCompressionSessionCompleteFrames(e.session, makeCMTime(e.frameIdx, int32(roundRate(fr))))
	if st != noErr {
		return nil, fmt.Errorf("%w: VTCompressionSessionCompleteFrames OSStatus=%d", ErrBackendFailure, st)
	}
	return e.drain()
}

// Flush completes any remaining frames and returns the tail packets.
func (e *vtEncoder) Flush() ([]video.Packet, error) {
	if e.closed {
		return nil, ErrClosed
	}
	// kCMTimeInvalid (flags=0) means "complete all pending frames".
	st := e.lib.VTCompressionSessionCompleteFrames(e.session, cmTime{})
	if st != noErr {
		return nil, fmt.Errorf("%w: VTCompressionSessionCompleteFrames(flush) OSStatus=%d", ErrBackendFailure, st)
	}
	return e.drain()
}

// drain returns and clears the pending packets accumulated by the
// callback, surfacing any callback-level OSStatus error.
func (e *vtEncoder) drain() ([]video.Packet, error) {
	if e.cbErr != noErr {
		err := fmt.Errorf("%w: output callback OSStatus=%d", ErrBackendFailure, e.cbErr)
		e.cbErr = noErr
		return nil, err
	}
	out := e.pending
	e.pending = nil
	return out, nil
}

// Close invalidates the session and releases resources. Idempotent.
func (e *vtEncoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	if e.session != 0 {
		e.lib.VTCompressionSessionInvalidate(e.session)
		e.lib.CFRelease(e.session)
		e.session = 0
	}
	e.unregister()
	return nil
}

func (e *vtEncoder) unregister() {
	encoderRegistryMu.Lock()
	delete(encoderRegistry, e.refcon)
	encoderRegistryMu.Unlock()
}

// roundRate rounds a frame rate to a usable integer timescale (>=1).
func roundRate(fr float64) int {
	r := int(fr + 0.5)
	if r < 1 {
		r = 1
	}
	return r
}

// cvPixelFormat maps a video.PixelFormat to its CVPixelFormatType.
func cvPixelFormat(p video.PixelFormat) (uint32, bool) {
	switch p {
	case video.NV12:
		return kCVPixelFormatType420YpCbCr8BiPlanarVideoRange, true
	case video.I420:
		return kCVPixelFormatTypeYpCbCr8Planar, true
	default:
		return 0, false
	}
}

// wrapPixelBuffer creates a CVPixelBuffer that aliases the frame's plane
// bytes via CVPixelBufferCreateWithPlanarBytes. The Go plane slices must
// stay alive for the duration of the encode call; they do (Encode holds
// f, and CompleteFrames forces synchronous emission before returning).
//
// We pass a NULL dataPtr / zero release callback: per the CoreVideo
// contract, supplying per-plane base addresses with a NULL "whole
// buffer" pointer makes the pixel buffer reference the plane pointers
// directly. The plane base-address, width, height, and bytes-per-row
// arrays must outlive the call, so they are kept on the stack here and
// the call completes synchronously.
func (e *vtEncoder) wrapPixelBuffer(f video.Frame, pixFmt uint32) (uintptr, error) {
	n := f.PixelFormat.Planes()
	if n == 0 || len(f.Planes) < n || len(f.Strides) < n {
		return 0, ErrUnsupportedPixelFormat
	}
	baseAddrs := make([]uintptr, n)
	widths := make([]uint64, n)
	heights := make([]uint64, n)
	bytesPerRow := make([]uint64, n)
	for i := 0; i < n; i++ {
		if len(f.Planes[i]) == 0 {
			return 0, ErrUnsupportedPixelFormat
		}
		baseAddrs[i] = uintptr(unsafe.Pointer(&f.Planes[i][0]))
		pw, ph := planeGeometry(f.PixelFormat, i, f.Width, f.Height)
		widths[i] = uint64(pw)
		heights[i] = uint64(ph)
		bytesPerRow[i] = uint64(f.Strides[i])
	}

	var pb uintptr
	st := e.lib.CVPixelBufferCreateWithPlanarBytes(
		0,
		uint64(f.Width), uint64(f.Height), pixFmt,
		nil, 0, // dataPtr/dataSize: NULL whole-buffer; use plane pointers
		uint64(n),
		&baseAddrs[0], &widths[0], &heights[0], &bytesPerRow[0],
		0, 0, // releaseCallback / releaseRefCon: none (we own the memory)
		0, // pixelBufferAttributes: none
		&pb,
	)
	if st != 0 || pb == 0 {
		return 0, fmt.Errorf("%w: CVPixelBufferCreateWithPlanarBytes CVReturn=%d", ErrBackendFailure, st)
	}
	return pb, nil
}

// planeGeometry returns the visible width/height of plane i for a 4:2:0
// format. Luma is full size; chroma is half in each dimension. For NV12
// the single chroma plane is full-width (interleaved Cb/Cr) and
// half-height.
func planeGeometry(p video.PixelFormat, i, w, h int) (int, int) {
	switch p {
	case video.NV12:
		if i == 0 {
			return w, h
		}
		return w, h / 2 // interleaved CbCr: w bytes/row, h/2 rows
	case video.I420:
		if i == 0 {
			return w, h
		}
		return w / 2, h / 2
	default:
		return w, h
	}
}

// ptsToDuration converts a CMTime-derived (value, timescale) to a
// time.Duration.
func ptsToDuration(value int64, timescale int32) time.Duration {
	if timescale <= 0 {
		return 0
	}
	return time.Duration(float64(value) / float64(timescale) * float64(time.Second))
}
