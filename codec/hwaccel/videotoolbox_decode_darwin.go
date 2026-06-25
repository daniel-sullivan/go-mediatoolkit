//go:build darwin

// The VideoToolbox decode direction: a hwaccel.Decoder driving a
// VTDecompressionSession via the purego bindings in
// vtbindings_darwin.go. It is the inverse of the VTCompressionSession
// encoder in videotoolbox_darwin.go.
//
// # Input packaging
//
// The decoder consumes the elementary-stream form the encoder produces:
// Annex-B (start-code prefixed) NAL units, with the parameter sets
// (SPS/PPS for H.264; VPS/SPS/PPS for H.265) carried inline on each
// keyframe access unit. From those parameter sets it builds a
// CMVideoFormatDescription and, from that, the VTDecompressionSession.
// VideoToolbox itself wants AVCC (4-byte length-prefixed) NAL units in a
// CMSampleBuffer, so each access unit's VCL NALs are re-prefixed from
// start-codes to 4-byte big-endian lengths before being submitted.
//
// # Synchronous output
//
// DecodeFrame runs without the asynchronous-decompression flag, so the
// VTDecompressionOutputCallback fires (in-thread) before DecodeFrame
// returns and the decoded CVPixelBuffer is converted to a video.Frame
// inside that callback. A WaitForAsynchronousFrames after each submit is
// a belt-and-braces drain in case the host decoder still queued work.
//
// # Callback ABI note
//
// The VTDecompressionOutputCallback's C signature ends in two CMTime
// (24-byte) structs passed by value:
//
//	void cb(void *decompressionOutputRefCon, void *sourceFrameRefCon,
//	        OSStatus status, VTDecodeInfoFlags infoFlags,
//	        CVImageBufferRef imageBuffer,
//	        CMTime presentationTimeStamp, CMTime presentationDuration)
//
// purego.NewCallback cannot host struct-by-value parameters. We sidestep
// that without faking anything: the CVImageBufferRef (the only argument
// the decoder needs) is the fifth argument and lands in an integer
// register on arm64/amd64 BEFORE either CMTime, so a flattened callback
// declaring only the leading scalar arguments receives it correctly.
// Per AAPCS64 a >16-byte integer aggregate is passed indirectly (one
// register holding a pointer to a caller copy), so the trailing CMTimes
// never displace imageBuffer regardless. The two timestamps are not
// needed for transcode (PTS is recovered from the submitted Packet), so
// they are simply not declared on the Go side.

package hwaccel

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// NewDecoder constructs a VideoToolbox decompression-session decoder for
// the configured codec. The VTDecompressionSession itself is created
// lazily on the first keyframe, once the parameter sets needed to build
// the CMVideoFormatDescription are available in the stream.
func (b *vtBackend) newDecoder(cfg Config) (Decoder, error) {
	if err := cfg.validateDecode(); err != nil {
		return nil, err
	}
	codecType, ok := cmCodecType(cfg.Codec)
	if !ok {
		return nil, ErrUnsupportedCodec
	}
	if b.lib.VTDecompressionSessionCreate == nil || b.lib.VTDecompressionSessionDecodeFrame == nil {
		return nil, fmt.Errorf("%w: VTDecompressionSession symbols unavailable", ErrBackendFailure)
	}
	d := &vtDecoder{lib: b.lib, cfg: cfg, codec: cfg.Codec, codecType: codecType}

	decoderRegistryMu.Lock()
	id := decoderNextID
	decoderNextID++
	decoderRegistry[id] = d
	decoderRegistryMu.Unlock()
	d.refcon = id
	return d, nil
}

// ---- decoder registry + callback trampoline --------------------------

// decoderRegistry maps the integer refcon handed to VideoToolbox back to
// the owning vtDecoder, so the C output callback never receives a Go
// pointer (the refcon void* is an index, not a pointer into Go memory).
var (
	decoderRegistryMu  sync.Mutex
	decoderRegistry            = map[uintptr]*vtDecoder{}
	decoderNextID      uintptr = 1
	decodeCallbackPtr  uintptr
	decodeCallbackOnce sync.Once
)

// decompressionOutputRecord mirrors the C VTDecompressionOutputCallbackRecord:
//
//	typedef struct {
//	    VTDecompressionOutputCallback decompressionOutputCallback;
//	    void *decompressionOutputRefCon;
//	} VTDecompressionOutputCallbackRecord;
//
// A pointer to it is passed to VTDecompressionSessionCreate. The two
// fields are pointer-sized and naturally aligned.
type decompressionOutputRecord struct {
	Callback uintptr
	RefCon   uintptr
}

// decodeCallbackTrampoline returns the C-callable VTDecompressionOutputCallback
// pointer, created once. See the ABI note atop this file for why the Go
// function declares only the leading scalar arguments.
func decodeCallbackTrampoline() uintptr {
	decodeCallbackOnce.Do(func() {
		decodeCallbackPtr = purego.NewCallback(decompressionOutput)
	})
	return decodeCallbackPtr
}

// decompressionOutput is the Go side of the VTDecompressionOutputCallback.
// outputRefCon is the integer decoder id; imageBuffer is the decoded
// CVPixelBuffer (a CVImageBufferRef). The trailing CMTime arguments are
// intentionally omitted (see the ABI note atop this file).
func decompressionOutput(outputRefCon, srcFrameRefCon uintptr, status int32, infoFlags uint32, imageBuffer uintptr) {
	decoderRegistryMu.Lock()
	d := decoderRegistry[outputRefCon]
	decoderRegistryMu.Unlock()
	if d == nil {
		return
	}
	if status != noErr {
		d.cbErr = status
		return
	}
	// A dropped frame yields a nil imageBuffer with noErr.
	if imageBuffer == 0 {
		return
	}
	if f, ok := d.imageBufferToFrame(imageBuffer); ok {
		d.pending = append(d.pending, f)
	}
}

// ---- decoder ---------------------------------------------------------

// vtDecoder drives a single VTDecompressionSession. Decode is
// synchronous: each access unit is submitted and the output callback has
// run (populating pending) before Decode returns. Not safe for
// concurrent use.
type vtDecoder struct {
	lib       *vtLib
	cfg       Config
	codec     video.Codec
	codecType uint32

	refcon   uintptr
	session  uintptr
	formats  uintptr // current CMVideoFormatDescription
	sessionW int32   // dimensions the live VP9/AV1 format description was built for
	sessionH int32

	// Cached parameter sets the live format description was built from,
	// so a repeated identical keyframe does not rebuild the session.
	sps [][]byte
	pps [][]byte
	vps [][]byte

	// av1Seq caches the last parsed AV1 sequence header so frame OBUs that
	// omit it mid-stream can still be configured.
	av1Seq *av1SeqHeader

	frameIdx int64
	pending  []video.Frame
	cbErr    int32
	closed   bool
}

// Decode submits one encoded access unit and returns any frames the
// decoder finished. The first decodable packet must carry the parameter
// sets (the encoder prefixes them onto every keyframe), from which the
// session is (re)built before the access unit is submitted.
func (d *vtDecoder) Decode(p video.Packet) ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if len(p.Data) == 0 {
		return nil, nil
	}

	// VP9 and AV1 are not Annex-B: the packet is a coded VP9 frame/superframe
	// or an AV1 temporal unit, submitted whole with a codec-type format
	// description (built from the parsed headers, not parameter sets).
	if d.codec == video.VP9 || d.codec == video.AV1 {
		return d.decodeOBUOrFrame(p)
	}

	nals := splitAnnexB(p.Data)
	if len(nals) == 0 {
		return nil, nil
	}

	// Pull out parameter sets and ensure a session exists / is current.
	if err := d.ensureSession(nals); err != nil {
		return nil, err
	}
	if d.session == 0 {
		// No session yet (a P-frame arrived before any keyframe). Nothing
		// we can decode; surface as a backend failure so callers don't
		// silently drop a leading P-frame stream.
		return nil, fmt.Errorf("%w: decode before parameter sets (no keyframe seen)", ErrBackendFailure)
	}

	// Re-prefix the VCL NALs (everything that is not a parameter set or
	// AU delimiter) to AVCC 4-byte lengths for VideoToolbox.
	avcc := d.vclToAVCC(nals)
	if len(avcc) == 0 {
		// A pure parameter-set packet with no slice — nothing to decode.
		return d.drain()
	}

	sb, err := d.makeSampleBuffer(avcc)
	if err != nil {
		return nil, err
	}
	defer d.lib.CFRelease(sb)

	var infoFlags uint32
	st := d.lib.VTDecompressionSessionDecodeFrame(
		d.session, sb, kVTDecodeFrameFlagsSync, 0, &infoFlags)
	if st != noErr {
		return nil, fmt.Errorf("%w: VTDecompressionSessionDecodeFrame OSStatus=%d", ErrBackendFailure, st)
	}
	// Belt-and-braces: drain any frames the host queued asynchronously.
	if d.lib.VTDecompressionSessionWaitForAsynchronousFrames != nil {
		if st := d.lib.VTDecompressionSessionWaitForAsynchronousFrames(d.session); st != noErr {
			return nil, fmt.Errorf("%w: VTDecompressionSessionWaitForAsynchronousFrames OSStatus=%d", ErrBackendFailure, st)
		}
	}
	d.frameIdx++
	return d.drain()
}

// Flush drains any remaining frames. With synchronous in-order decode
// there is normally nothing buffered, but a host that queued frames is
// drained here too.
func (d *vtDecoder) Flush() ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if d.session != 0 && d.lib.VTDecompressionSessionWaitForAsynchronousFrames != nil {
		if st := d.lib.VTDecompressionSessionWaitForAsynchronousFrames(d.session); st != noErr {
			return nil, fmt.Errorf("%w: VTDecompressionSessionWaitForAsynchronousFrames(flush) OSStatus=%d", ErrBackendFailure, st)
		}
	}
	return d.drain()
}

// Close invalidates the session, releases the format description, and
// unregisters. Idempotent.
func (d *vtDecoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	if d.session != 0 {
		d.lib.VTDecompressionSessionInvalidate(d.session)
		d.lib.CFRelease(d.session)
		d.session = 0
	}
	if d.formats != 0 {
		d.lib.CFRelease(d.formats)
		d.formats = 0
	}
	decoderRegistryMu.Lock()
	delete(decoderRegistry, d.refcon)
	decoderRegistryMu.Unlock()
	return nil
}

// drain returns and clears the pending frames, surfacing any
// callback-level OSStatus error.
func (d *vtDecoder) drain() ([]video.Frame, error) {
	if d.cbErr != noErr {
		err := fmt.Errorf("%w: decompression callback OSStatus=%d", ErrBackendFailure, d.cbErr)
		d.cbErr = noErr
		return nil, err
	}
	out := d.pending
	d.pending = nil
	return out, nil
}

// ---- session + format description ------------------------------------

// ensureSession extracts any parameter sets from the access unit's NALs
// and, if they differ from the ones backing the current session (or no
// session exists yet), rebuilds the CMVideoFormatDescription and
// VTDecompressionSession. Packets with no parameter sets leave the
// existing session untouched.
func (d *vtDecoder) ensureSession(nals [][]byte) error {
	vps, sps, pps := d.classifyParameterSets(nals)
	if len(sps) == 0 || len(pps) == 0 {
		return nil // not a parameter-set-carrying packet; keep current session
	}
	if d.session != 0 && paramsEqual(d.vps, vps) && paramsEqual(d.sps, sps) && paramsEqual(d.pps, pps) {
		return nil // identical parameter sets; reuse session
	}

	fd, err := d.makeFormatDescription(vps, sps, pps)
	if err != nil {
		return err
	}

	// Tear down any previous session/format before swapping in the new.
	if d.session != 0 {
		d.lib.VTDecompressionSessionInvalidate(d.session)
		d.lib.CFRelease(d.session)
		d.session = 0
	}
	if d.formats != 0 {
		d.lib.CFRelease(d.formats)
		d.formats = 0
	}

	rec := decompressionOutputRecord{Callback: decodeCallbackTrampoline(), RefCon: d.refcon}
	var session uintptr
	st := d.lib.VTDecompressionSessionCreate(
		0,  // default allocator
		fd, // video format description
		0,  // decoderSpecification: nil = let VT pick hardware if available
		0,  // destinationImageBufferAttributes: nil = native format
		unsafe.Pointer(&rec),
		&session,
	)
	if st != noErr || session == 0 {
		d.lib.CFRelease(fd)
		return fmt.Errorf("%w: VTDecompressionSessionCreate OSStatus=%d", ErrBackendFailure, st)
	}
	d.session = session
	d.formats = fd
	d.vps, d.sps, d.pps = cloneParams(vps), cloneParams(sps), cloneParams(pps)
	return nil
}

// makeFormatDescription builds a CMVideoFormatDescription from the parsed
// parameter sets using the codec-specific create-from-parameter-sets
// call. The NAL header length is fixed at 4 to match the AVCC framing the
// decoder produces in vclToAVCC.
func (d *vtDecoder) makeFormatDescription(vps, sps, pps [][]byte) (uintptr, error) {
	l := d.lib
	var fd uintptr

	if d.codec == video.H265 {
		if l.CMVideoFormatDescriptionCreateFromHEVCParameterSets == nil {
			return 0, fmt.Errorf("%w: CMVideoFormatDescriptionCreateFromHEVCParameterSets unavailable", ErrBackendFailure)
		}
		sets := append(append(append([][]byte{}, vps...), sps...), pps...)
		ptrs, sizes, pin := paramSetArrays(sets)
		st := l.CMVideoFormatDescriptionCreateFromHEVCParameterSets(
			0, uint64(len(sets)), ptrs, sizes, 4, 0, &fd)
		runtime.KeepAlive(pin)
		if st != noErr || fd == 0 {
			return 0, fmt.Errorf("%w: CMVideoFormatDescriptionCreateFromHEVCParameterSets OSStatus=%d", ErrBackendFailure, st)
		}
		return fd, nil
	}

	if l.CMVideoFormatDescriptionCreateFromH264ParameterSets == nil {
		return 0, fmt.Errorf("%w: CMVideoFormatDescriptionCreateFromH264ParameterSets unavailable", ErrBackendFailure)
	}
	sets := append(append([][]byte{}, sps...), pps...)
	ptrs, sizes, pin := paramSetArrays(sets)
	st := l.CMVideoFormatDescriptionCreateFromH264ParameterSets(
		0, uint64(len(sets)), ptrs, sizes, 4, &fd)
	runtime.KeepAlive(pin)
	if st != noErr || fd == 0 {
		return 0, fmt.Errorf("%w: CMVideoFormatDescriptionCreateFromH264ParameterSets OSStatus=%d", ErrBackendFailure, st)
	}
	return fd, nil
}

// makeSampleBuffer wraps an AVCC access unit in a CMBlockBuffer (its own
// allocated memory, so VideoToolbox never references Go memory) and binds
// it into a CMSampleBuffer carrying the current format description.
func (d *vtDecoder) makeSampleBuffer(avcc []byte) (uintptr, error) {
	l := d.lib

	var bb uintptr
	st := l.CMBlockBufferCreateWithMemoryBlock(
		0,                                 // default allocator
		nil,                               // memoryBlock: nil = allocate internally
		uint64(len(avcc)),                 // blockLength
		0,                                 // blockAllocator: default
		0,                                 // customBlockSource: none
		0,                                 // offsetToData
		uint64(len(avcc)),                 // dataLength
		kCMBlockBufferAssureMemoryNowFlag, // allocate now
		&bb,
	)
	if st != noErr || bb == 0 {
		return 0, fmt.Errorf("%w: CMBlockBufferCreateWithMemoryBlock OSStatus=%d", ErrBackendFailure, st)
	}
	// Copy the AU bytes into the block buffer's own allocation.
	if st := l.CMBlockBufferReplaceDataBytes(unsafe.Pointer(&avcc[0]), bb, 0, uint64(len(avcc))); st != noErr {
		l.CFRelease(bb)
		return 0, fmt.Errorf("%w: CMBlockBufferReplaceDataBytes OSStatus=%d", ErrBackendFailure, st)
	}

	sampleSize := uint64(len(avcc))
	var sb uintptr
	st = l.CMSampleBufferCreateReady(
		0,         // default allocator
		bb,        // dataBuffer
		d.formats, // formatDescription
		1,         // numSamples
		0, nil,    // no timing entries
		1, &sampleSize, // one sample size entry
		&sb,
	)
	// CMSampleBufferCreateReady retains the block buffer; release our ref.
	l.CFRelease(bb)
	if st != noErr || sb == 0 {
		return 0, fmt.Errorf("%w: CMSampleBufferCreateReady OSStatus=%d", ErrBackendFailure, st)
	}
	return sb, nil
}

// ---- pixel-buffer readback -------------------------------------------

// imageBufferToFrame copies a decoded CVPixelBuffer's planes into a
// video.Frame. NV12 (bi-planar) and I420 (tri-planar) are recognised;
// other pixel formats yield ok=false. The base addresses are valid only
// between Lock and Unlock, so every plane is copied into Go-owned slices
// before unlocking.
func (d *vtDecoder) imageBufferToFrame(pb uintptr) (video.Frame, bool) {
	l := d.lib

	pixFmt := l.CVPixelBufferGetPixelFormatType(pb)
	vf, ok := pixelFormatFromCV(pixFmt)
	if !ok {
		return video.Frame{}, false
	}
	w := int(l.CVPixelBufferGetWidth(pb))
	h := int(l.CVPixelBufferGetHeight(pb))
	if w == 0 || h == 0 {
		return video.Frame{}, false
	}

	if st := l.CVPixelBufferLockBaseAddress(pb, kCVPixelBufferLock_ReadOnly); st != noErr {
		return video.Frame{}, false
	}
	defer l.CVPixelBufferUnlockBaseAddress(pb, kCVPixelBufferLock_ReadOnly)

	nPlanes := vf.Planes()
	planes := make([][]byte, nPlanes)
	strides := make([]int, nPlanes)

	// A CVPixelBuffer can be planar (per-plane accessors) or, rarely for
	// these formats, non-planar; honour whichever the buffer reports.
	if l.CVPixelBufferIsPlanar(pb) {
		if int(l.CVPixelBufferGetPlaneCount(pb)) < nPlanes {
			return video.Frame{}, false
		}
		for i := 0; i < nPlanes; i++ {
			base := l.CVPixelBufferGetBaseAddressOfPlane(pb, uint64(i))
			if base == nil {
				return video.Frame{}, false
			}
			stride := int(l.CVPixelBufferGetBytesPerRowOfPlane(pb, uint64(i)))
			ph := int(l.CVPixelBufferGetHeightOfPlane(pb, uint64(i)))
			planes[i] = copyPlane(base, stride, ph)
			strides[i] = stride
		}
	} else {
		base := l.CVPixelBufferGetBaseAddress(pb)
		if base == nil {
			return video.Frame{}, false
		}
		stride := int(l.CVPixelBufferGetBytesPerRow(pb))
		planes[0] = copyPlane(base, stride, h)
		strides[0] = stride
		// Non-planar 4:2:0 is not expected from VideoToolbox; bail rather
		// than mis-slice the interleaved remainder.
		if nPlanes > 1 {
			return video.Frame{}, false
		}
	}

	return video.Frame{
		PixelFormat: vf,
		Width:       w,
		Height:      h,
		Planes:      planes,
		Strides:     strides,
		PTS:         ptsToDuration(d.frameIdx, int32(roundRate(d.cfg.frameRate()))),
	}, true
}

// copyPlane copies stride*rows bytes from a C plane base address into a
// fresh Go slice.
func copyPlane(base unsafe.Pointer, stride, rows int) []byte {
	if stride <= 0 || rows <= 0 {
		return nil
	}
	src := unsafe.Slice((*byte)(base), stride*rows)
	dst := make([]byte, stride*rows)
	copy(dst, src)
	return dst
}

// pixelFormatFromCV maps a CVPixelFormatType back to a video.PixelFormat.
func pixelFormatFromCV(t uint32) (video.PixelFormat, bool) {
	switch t {
	case kCVPixelFormatType420YpCbCr8BiPlanarVideoRange,
		kCVPixelFormatType420YpCbCr8BiPlanarFullRange:
		return video.NV12, true
	case kCVPixelFormatTypeYpCbCr8Planar,
		kCVPixelFormatTypeYpCbCr8PlanarFullRange:
		return video.I420, true
	default:
		return video.PixelFormatUnknown, false
	}
}
