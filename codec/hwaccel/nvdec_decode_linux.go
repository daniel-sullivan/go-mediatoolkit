//go:build linux

// NVDEC / cuvid decode for H.264 and H.265. The cuvid bitstream parser is a
// callback-driven state machine: the application feeds Annex-B access units to
// cuvidParseVideoData, and the parser invokes three C callbacks synchronously
// on the calling goroutine —
//
//	pfnSequenceCallback(CUVIDEOFORMAT*) — first sequence header / format change:
//	    create (or reconfigure) the NVDEC decoder for the coded geometry.
//	pfnDecodePicture(CUVIDPICPARAMS*)   — a fully-parsed picture is ready:
//	    hand it to cuvidDecodePicture (the parser already filled the huge
//	    codec-specific union).
//	pfnDisplayPicture(CUVIDPARSERDISPINFO*) — a picture is ready in display
//	    order: map its NV12 surface out of device memory, copy to a host
//	    video.Frame, unmap.
//
// The callbacks are bound to this decoder instance via a registry keyed on the
// pUserData pointer passed to the parser, since purego.NewCallback yields a
// plain C function pointer with no closure capture.
//
// # Packet flow
//
//	Decode(annex-b packet) -> cuvidParseVideoData -> [seq cb builds decoder]
//	    -> [decode cb -> cuvidDecodePicture] -> [display cb -> map+copy NV12]
//	    -> accumulated video.Frames returned
//
// Not safe for concurrent use.
//
// NB: hardware-unverified — see the status banner in nvenc_linux.go.

package hwaccel

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
	"github.com/ebitengine/purego"
)

// nvCallbacks holds the three purego.NewCallback trampolines shared by every
// decoder. They are created once (the trampoline count purego allows is
// limited, so they are not per-decoder) and dispatch to the owning decoder via
// the userData registry.
type nvCallbacks struct {
	seq     uintptr
	decode  uintptr
	display uintptr
}

var (
	nvCallbacksOnce sync.Once
	nvCallbacksRef  nvCallbacks

	// nvDecoderRegistry maps the userData token passed to the parser back to
	// the owning decoder so the shared callbacks can dispatch.
	nvDecoderMu  sync.Mutex
	nvDecoderReg = map[uintptr]*nvDecoder{}
	nvDecoderSeq uintptr
)

// initNVCallbacks lazily creates the three parser callbacks.
func initNVCallbacks() nvCallbacks {
	nvCallbacksOnce.Do(func() {
		nvCallbacksRef = nvCallbacks{
			seq: purego.NewCallback(func(user uintptr, fmt2 *cuvideoFormat) int32 {
				if d := lookupNVDecoder(user); d != nil {
					return d.onSequence(fmt2)
				}
				return 1
			}),
			decode: purego.NewCallback(func(user uintptr, pic *cuvidPicParams) int32 {
				if d := lookupNVDecoder(user); d != nil {
					return d.onDecode(pic)
				}
				return 0
			}),
			display: purego.NewCallback(func(user uintptr, disp *cuvidParserDispInfo) int32 {
				if d := lookupNVDecoder(user); d != nil {
					return d.onDisplay(disp)
				}
				return 1
			}),
		}
	})
	return nvCallbacksRef
}

// registerNVDecoder allocates a userData token for d and returns it.
func registerNVDecoder(d *nvDecoder) uintptr {
	nvDecoderMu.Lock()
	defer nvDecoderMu.Unlock()
	nvDecoderSeq++
	tok := nvDecoderSeq
	nvDecoderReg[tok] = d
	return tok
}

func unregisterNVDecoder(tok uintptr) {
	nvDecoderMu.Lock()
	delete(nvDecoderReg, tok)
	nvDecoderMu.Unlock()
}

func lookupNVDecoder(tok uintptr) *nvDecoder {
	nvDecoderMu.Lock()
	defer nvDecoderMu.Unlock()
	return nvDecoderReg[tok]
}

// nvDecoder drives a single cuvid parse + NVDEC decode pipeline. The parser is
// created in newNVDecoder; the decoder is created lazily in the sequence
// callback once the coded geometry is known. Not safe for concurrent use.
type nvDecoder struct {
	b     *nvBackend
	cfg   Config
	codec video.Codec

	userTok uintptr
	parser  uintptr
	decoder uintptr

	width       int
	height      int
	numSurfaces int

	// pending accumulates frames produced by the display callback during a
	// single Decode/Flush call, and any error the callbacks hit.
	pending []video.Frame
	cbErr   error

	closed bool
}

// newNVDecoder creates the cuvid parser bound to a fresh userData token. The
// NVDEC decoder is created in the sequence callback.
func newNVDecoder(b *nvBackend, cfg Config) Decoder {
	d := &nvDecoder{b: b, cfg: cfg, codec: cfg.Codec}
	d.userTok = registerNVDecoder(d)
	return d
}

// ensureParser lazily creates the cuvid video parser for the configured codec.
func (d *nvDecoder) ensureParser() error {
	if d.parser != 0 {
		return nil
	}
	cbs := initNVCallbacks()
	params := cuvidParserParams{
		CodecType:              nvCudaVideoCodec(d.codec),
		UlMaxNumDecodeSurfaces: 1, // overridden by the sequence callback's return
		UlMaxDisplayDelay:      0, // no reorder delay: emit in display order ASAP
		PUserData:              d.userTok,
		PfnSequenceCallback:    cbs.seq,
		PfnDecodePicture:       cbs.decode,
		PfnDisplayPicture:      cbs.display,
	}
	var parser uintptr
	if st := d.b.lib.cuvidCreateVideoParser(&parser, &params); st != cudaSuccess {
		return fmt.Errorf("%w: cuvidCreateVideoParser CUresult=%d", ErrBackendFailure, st)
	}
	d.parser = parser
	return nil
}

// Decode feeds one Annex-B access unit to the parser and returns whatever
// frames the display callback produced during the parse.
func (d *nvDecoder) Decode(p video.Packet) ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if err := d.ensureParser(); err != nil {
		return nil, err
	}
	if len(p.Data) == 0 {
		return nil, nil
	}

	d.pending = d.pending[:0]
	d.cbErr = nil

	pkt := cuvidSourceDataPacket{
		Flags:       uint64(cuvidPktTimestamp),
		PayloadSize: uint64(len(p.Data)),
		Payload:     uintptr(unsafe.Pointer(&p.Data[0])),
		Timestamp:   int64(p.PTS),
	}
	if st := d.b.lib.cuvidParseVideoData(d.parser, &pkt); st != cudaSuccess {
		return nil, fmt.Errorf("%w: cuvidParseVideoData CUresult=%d", ErrBackendFailure, st)
	}
	if d.cbErr != nil {
		return nil, d.cbErr
	}
	out := make([]video.Frame, len(d.pending))
	copy(out, d.pending)
	return out, nil
}

// Flush sends an end-of-stream packet so the parser drains any buffered
// pictures, returning the remaining frames.
func (d *nvDecoder) Flush() ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if d.parser == 0 {
		return nil, nil
	}
	d.pending = d.pending[:0]
	d.cbErr = nil

	pkt := cuvidSourceDataPacket{Flags: uint64(cuvidPktEndOfStream)}
	if st := d.b.lib.cuvidParseVideoData(d.parser, &pkt); st != cudaSuccess {
		return nil, fmt.Errorf("%w: cuvidParseVideoData(EOS) CUresult=%d", ErrBackendFailure, st)
	}
	if d.cbErr != nil {
		return nil, d.cbErr
	}
	out := make([]video.Frame, len(d.pending))
	copy(out, d.pending)
	return out, nil
}

// Close destroys the parser and decoder and unregisters the userData token.
// Idempotent.
func (d *nvDecoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	l := d.b.lib
	if d.parser != 0 {
		l.cuvidDestroyVideoParser(d.parser)
		d.parser = 0
	}
	if d.decoder != 0 {
		l.cuvidDestroyDecoder(d.decoder)
		d.decoder = 0
	}
	unregisterNVDecoder(d.userTok)
	return nil
}

// onSequence is the parser's sequence callback: it creates (or recreates) the
// NVDEC decoder for the coded geometry. It returns the decode-surface count so
// the parser cycles through that many internal surfaces; 0 signals failure.
func (d *nvDecoder) onSequence(f *cuvideoFormat) int32 {
	l := d.b.lib

	d.width = int(f.DisplayAreaRight - f.DisplayAreaLeft)
	d.height = int(f.DisplayAreaBottom - f.DisplayAreaTop)
	if d.width <= 0 || d.height <= 0 {
		d.width = int(f.CodedWidth)
		d.height = int(f.CodedHeight)
	}

	surfaces := int(f.MinNumDecodeSurfaces)
	if surfaces < 1 {
		surfaces = 4
	}
	d.numSurfaces = surfaces

	// Recreate the decoder if one already exists (a mid-stream format change).
	if d.decoder != 0 {
		l.cuvidDestroyDecoder(d.decoder)
		d.decoder = 0
	}

	ci := cuvidDecodeCreateInfo{
		UlWidth:             uint64(f.CodedWidth),
		UlHeight:            uint64(f.CodedHeight),
		UlNumDecodeSurfaces: uint64(surfaces),
		CodecType:           f.Codec,
		ChromaFormat:        f.ChromaFormat,
		BitDepthMinus8:      uint64(f.BitDepthLumaMinus8),
		OutputFormat:        cudaVideoSurfaceFormatNV12,
		UlTargetWidth:       uint64(f.CodedWidth),
		UlTargetHeight:      uint64(f.CodedHeight),
		UlNumOutputSurfaces: 2,
		UlMaxWidth:          uint64(f.CodedWidth),
		UlMaxHeight:         uint64(f.CodedHeight),
		DisplayAreaLeft:     int16(f.DisplayAreaLeft),
		DisplayAreaTop:      int16(f.DisplayAreaTop),
		DisplayAreaRight:    int16(f.DisplayAreaRight),
		DisplayAreaBottom:   int16(f.DisplayAreaBottom),
	}
	var decoder uintptr
	if st := l.cuvidCreateDecoder(&decoder, &ci); st != cudaSuccess {
		d.cbErr = fmt.Errorf("%w: cuvidCreateDecoder CUresult=%d", ErrBackendFailure, st)
		return 0
	}
	d.decoder = decoder
	return int32(surfaces)
}

// onDecode is the parser's decode callback: hand the fully-parsed picture to
// NVDEC. The parser has already populated the codec-specific union.
func (d *nvDecoder) onDecode(pic *cuvidPicParams) int32 {
	if d.decoder == 0 {
		d.cbErr = ErrParameterSetsMissing
		return 0
	}
	if st := d.b.lib.cuvidDecodePicture(d.decoder, pic); st != cudaSuccess {
		d.cbErr = fmt.Errorf("%w: cuvidDecodePicture CUresult=%d", ErrBackendFailure, st)
		return 0
	}
	return 1
}

// onDisplay is the parser's display callback: map the decoded NV12 surface out
// of device memory, copy it into a host video.Frame, and unmap. Returns 1 on
// success.
func (d *nvDecoder) onDisplay(disp *cuvidParserDispInfo) int32 {
	if d.decoder == 0 {
		d.cbErr = ErrParameterSetsMissing
		return 0
	}
	l := d.b.lib

	proc := cuvidProcParams{ProgressiveFrame: disp.ProgressiveFrame}
	var devPtr uint64
	var pitch uint32
	if st := l.cuvidMapVideoFrame64(d.decoder, disp.PictureIndex, &devPtr, &pitch, &proc); st != cudaSuccess {
		d.cbErr = fmt.Errorf("%w: cuvidMapVideoFrame64 CUresult=%d", ErrBackendFailure, st)
		return 0
	}

	frame, err := d.copyNV12(devPtr, int(pitch))
	l.cuvidUnmapVideoFrame64(d.decoder, devPtr)
	if err != nil {
		d.cbErr = err
		return 0
	}
	frame.PTS = time.Duration(disp.Timestamp)
	d.pending = append(d.pending, frame)
	return 1
}

// copyNV12 copies the mapped NV12 device surface (Y plane of `height` rows of
// `pitch` bytes, then interleaved UV of `height/2` rows) into a fresh host
// video.Frame with tightly-packed planes (stride == width). The device->host
// copy uses cuMemcpyDtoH per plane region.
func (d *nvDecoder) copyNV12(devPtr uint64, pitch int) (video.Frame, error) {
	l := d.b.lib

	w, h := d.width, d.height
	// Read the whole mapped region (Y rows + UV rows) into a host staging
	// buffer honouring the device pitch, then de-stride into packed planes.
	staging := make([]byte, pitch*(h+h/2))
	if st := l.cuMemcpyDtoH(unsafe.Pointer(&staging[0]), devPtr, uint64(len(staging))); st != cudaSuccess {
		return video.Frame{}, fmt.Errorf("%w: cuMemcpyDtoH CUresult=%d", ErrBackendFailure, st)
	}

	y := make([]byte, w*h)
	for row := 0; row < h; row++ {
		copy(y[row*w:row*w+w], staging[row*pitch:row*pitch+w])
	}
	c := make([]byte, w*(h/2))
	cOff := pitch * h
	for row := 0; row < h/2; row++ {
		copy(c[row*w:row*w+w], staging[cOff+row*pitch:cOff+row*pitch+w])
	}

	return video.Frame{
		PixelFormat: video.NV12,
		Width:       w,
		Height:      h,
		Planes:      [][]byte{y, c},
		Strides:     []int{w, w},
	}, nil
}
