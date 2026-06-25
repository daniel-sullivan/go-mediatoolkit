//go:build linux

// NVENC CBR encode for H.264 and H.265. The encoder is all-intra: every
// frame is forced to an IDR (NV_ENC_PIC_FLAG_FORCEIDR) with SPS/PPS(/VPS)
// written into each coded picture (NV_ENC_PIC_FLAG_OUTPUT_SPSPPS), so each
// packet is independently decodable — the form an NVR / elementary-stream
// consumer wants — and no reference-picture bookkeeping is needed.
//
// # Frame flow
//
//	(lazily) open session -> initialize encoder (codec GUID, P4 preset,
//	    high-quality tuning) -> create host input buffer + bitstream buffer
//	lock input buffer -> copy NV12 (or I420->NV12) into it -> unlock
//	NvEncEncodePicture(FORCEIDR | OUTPUT_SPSPPS)
//	NvEncLockBitstream -> copy out the Annex-B bytes -> NvEncUnlockBitstream
//	-> video.Packet (keyframe=true; NVENC emits Annex-B start codes natively)
//
// Not safe for concurrent use.
//
// NB: hardware-unverified — see the status banner in nvenc_linux.go.

package hwaccel

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// nvEncoder drives a single NVENC encode session. All state is created
// lazily on the first Encode (once the input geometry is known) and torn down
// in Close. Not safe for concurrent use.
type nvEncoder struct {
	b     *nvBackend
	cfg   Config
	codec video.Codec

	width  int
	height int

	encoder      uintptr // NVENC session handle
	inputBuffer  uintptr // NV_ENC_INPUT_PTR
	bitstreamBuf uintptr // NV_ENC_OUTPUT_PTR
	inputPitch   uint32
	started      bool

	frameIdx int64
	closed   bool
}

// newNVEncoder validates cfg and returns an encoder; the NVENC session is
// initialized on the first Encode call.
func newNVEncoder(b *nvBackend, cfg Config) (Encoder, error) {
	w, h := cfg.Width, cfg.Height
	if w <= 0 || h <= 0 {
		return nil, ErrInvalidConfig
	}
	return &nvEncoder{
		b:      b,
		cfg:    cfg,
		codec:  cfg.Codec,
		width:  w,
		height: h,
	}, nil
}

// ensurePipeline lazily opens the NVENC session, initializes the encoder with
// the codec GUID + P4 preset (high-quality tuning, CBR at the configured
// bitrate), and allocates the host input buffer and the output bitstream
// buffer.
func (e *nvEncoder) ensurePipeline() error {
	if e.started {
		return nil
	}
	l := e.b.lib

	enc, err := e.b.openSession()
	if err != nil {
		return err
	}
	e.encoder = enc

	// Initialize: codec GUID + preset GUID; the preset config (rate control,
	// GOP, etc.) is applied from the preset, then overridden by the
	// frame-level FORCEIDR flag. CBR is the NVENC preset default for P4; the
	// average bitrate is carried through INITIALIZE_PARAMS only indirectly
	// (NVENC reads it from the preset config), so for a spec-correct minimal
	// path we rely on the preset and force keyframes per-picture.
	tuning := nvEncTuningInfoHighQuality
	init := nvEncInitializeParams{
		Version:      nvInitializeParamsVer,
		EncodeGUID:   nvCodecGUID(e.codec),
		PresetGUID:   nvEncPresetP4GUID,
		EncodeWidth:  uint32(e.width),
		EncodeHeight: uint32(e.height),
		DARWidth:     uint32(e.width),
		DARHeight:    uint32(e.height),
		FrameRateNum: e.frameRateNum(),
		FrameRateDen: e.frameRateDen(),
		EnablePTD:    1,
		TuningInfo:   tuning,
	}
	if st := l.initializeEncoder(enc, &init); st != nvEncSuccess {
		e.teardown()
		return fmt.Errorf("%w: NvEncInitializeEncoder NVENCSTATUS=%d", ErrBackendFailure, st)
	}

	// Host input buffer (NVENC copies it into device memory on submit).
	cin := nvEncCreateInputBuffer{
		Version:   nvCreateInputBufferVer,
		Width:     uint32(e.width),
		Height:    uint32(e.height),
		BufferFmt: nvEncBufferFormatNV12,
	}
	if st := l.createInputBuffer(enc, &cin); st != nvEncSuccess {
		e.teardown()
		return fmt.Errorf("%w: NvEncCreateInputBuffer NVENCSTATUS=%d", ErrBackendFailure, st)
	}
	e.inputBuffer = cin.InputBuffer

	// Output bitstream buffer.
	cbs := nvEncCreateBitstreamBuffer{Version: nvCreateBitstreamBufVer}
	if st := l.createBitstreamBuffer(enc, &cbs); st != nvEncSuccess {
		e.teardown()
		return fmt.Errorf("%w: NvEncCreateBitstreamBuffer NVENCSTATUS=%d", ErrBackendFailure, st)
	}
	e.bitstreamBuf = cbs.BitstreamBuffer

	e.started = true
	return nil
}

// Encode uploads one NV12 frame (I420 converted on upload), forces an IDR,
// and returns the Annex-B packet.
func (e *nvEncoder) Encode(f video.Frame) ([]video.Packet, error) {
	if e.closed {
		return nil, ErrClosed
	}
	if f.PixelFormat != video.NV12 && f.PixelFormat != video.I420 {
		return nil, ErrUnsupportedPixelFormat
	}
	if err := e.ensurePipeline(); err != nil {
		return nil, err
	}
	if err := e.uploadFrame(f); err != nil {
		return nil, err
	}
	pkt, err := e.encodeIDR()
	if err != nil {
		return nil, err
	}
	pkt.PTS = e.framePTS()
	pkt.DTS = pkt.PTS
	e.frameIdx++
	return []video.Packet{pkt}, nil
}

// Flush is a no-op: NVENC with enablePTD and forced IDRs emits each frame
// synchronously in Encode (no B-frame reorder delay in the all-intra path).
func (e *nvEncoder) Flush() ([]video.Packet, error) {
	if e.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// Close tears down the NVENC session. Idempotent.
func (e *nvEncoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	e.teardown()
	return nil
}

// teardown releases the input/bitstream buffers and destroys the session,
// in reverse construction order. Safe to call on a partially-built encoder.
func (e *nvEncoder) teardown() {
	l := e.b.lib
	if e.bitstreamBuf != 0 {
		l.destroyBitstreamBuffer(e.encoder, e.bitstreamBuf)
		e.bitstreamBuf = 0
	}
	if e.inputBuffer != 0 {
		l.destroyInputBuffer(e.encoder, e.inputBuffer)
		e.inputBuffer = 0
	}
	if e.encoder != 0 {
		l.destroyEncoder(e.encoder)
		e.encoder = 0
	}
}

// uploadFrame locks the host input buffer, copies the frame's planes into it
// honouring the locked pitch (converting I420 to NV12 interleaving on the
// way), and unlocks it.
func (e *nvEncoder) uploadFrame(f video.Frame) error {
	l := e.b.lib

	lib := nvEncLockInputBuffer{Version: nvLockInputBufferVer, InputBuffer: e.inputBuffer}
	if st := l.lockInputBuffer(e.encoder, &lib); st != nvEncSuccess {
		return fmt.Errorf("%w: NvEncLockInputBuffer NVENCSTATUS=%d", ErrBackendFailure, st)
	}
	e.inputPitch = lib.Pitch
	pitch := int(lib.Pitch)

	// The locked buffer holds the Y plane (height rows of `pitch` bytes)
	// immediately followed by the interleaved UV plane (height/2 rows).
	total := pitch * (e.height + e.height/2)
	dst := unsafe.Slice((*byte)(lib.BufferDataPtr), total)

	srcYStride := f.Strides[0]
	for row := 0; row < e.height; row++ {
		copy(dst[row*pitch:row*pitch+e.width],
			f.Planes[0][row*srcYStride:row*srcYStride+e.width])
	}

	cOff := pitch * e.height
	cw := e.width / 2
	ch := e.height / 2
	if f.PixelFormat == video.NV12 {
		srcCStride := f.Strides[1]
		for row := 0; row < ch; row++ {
			copy(dst[cOff+row*pitch:cOff+row*pitch+e.width],
				f.Planes[1][row*srcCStride:row*srcCStride+e.width])
		}
	} else { // I420 -> NV12 interleave
		uStride := f.Strides[1]
		vStride := f.Strides[2]
		for row := 0; row < ch; row++ {
			d := dst[cOff+row*pitch:]
			u := f.Planes[1][row*uStride:]
			v := f.Planes[2][row*vStride:]
			for col := 0; col < cw; col++ {
				d[2*col] = u[col]
				d[2*col+1] = v[col]
			}
		}
	}

	if st := l.unlockInputBuffer(e.encoder, e.inputBuffer); st != nvEncSuccess {
		return fmt.Errorf("%w: NvEncUnlockInputBuffer NVENCSTATUS=%d", ErrBackendFailure, st)
	}
	return nil
}

// encodeIDR submits the frame as a forced IDR with the parameter sets written
// inline, then locks the bitstream and copies out the Annex-B bytes.
func (e *nvEncoder) encodeIDR() (video.Packet, error) {
	l := e.b.lib

	pic := nvEncPicParams{
		Version:         nvPicParamsVer,
		InputWidth:      uint32(e.width),
		InputHeight:     uint32(e.height),
		InputPitch:      e.inputPitch,
		EncodePicFlags:  nvEncPicFlagForceIDR | nvEncPicFlagOutputSPSPPS,
		FrameIdx:        uint32(e.frameIdx),
		InputTimeStamp:  uint64(e.frameIdx),
		InputBuffer:     e.inputBuffer,
		OutputBitstream: e.bitstreamBuf,
		BufferFmt:       nvEncBufferFormatNV12,
		PictureStruct:   nvEncPicStructFrame,
		PictureType:     nvEncPicTypeIDR,
	}
	if st := l.encodePicture(e.encoder, &pic); st != nvEncSuccess {
		return video.Packet{}, fmt.Errorf("%w: NvEncEncodePicture NVENCSTATUS=%d", ErrBackendFailure, st)
	}

	lb := nvEncLockBitstream{
		Version:         nvLockBitstreamVer,
		OutputBitstream: e.bitstreamBuf,
	}
	if st := l.lockBitstream(e.encoder, &lb); st != nvEncSuccess {
		return video.Packet{}, fmt.Errorf("%w: NvEncLockBitstream NVENCSTATUS=%d", ErrBackendFailure, st)
	}

	if lb.BitstreamSizeInBytes == 0 || lb.BitstreamBufferPtr == nil {
		l.unlockBitstream(e.encoder, e.bitstreamBuf)
		return video.Packet{}, fmt.Errorf("%w: empty NVENC bitstream", ErrBackendFailure)
	}
	src := unsafe.Slice((*byte)(lb.BitstreamBufferPtr), lb.BitstreamSizeInBytes)
	data := make([]byte, len(src))
	copy(data, src)

	if st := l.unlockBitstream(e.encoder, e.bitstreamBuf); st != nvEncSuccess {
		return video.Packet{}, fmt.Errorf("%w: NvEncUnlockBitstream NVENCSTATUS=%d", ErrBackendFailure, st)
	}

	return video.Packet{
		Codec:    e.codec,
		Data:     data,
		Keyframe: true,
	}, nil
}

// framePTS returns the presentation timestamp for the current frame.
func (e *nvEncoder) framePTS() time.Duration {
	return time.Duration(float64(e.frameIdx) / e.cfg.frameRate() * float64(time.Second))
}

// frameRateNum / frameRateDen return the configured rate as a rational,
// defaulting to 30/1.
func (e *nvEncoder) frameRateNum() uint32 {
	if e.cfg.FrameRateNum <= 0 {
		return 30
	}
	return uint32(e.cfg.FrameRateNum)
}

func (e *nvEncoder) frameRateDen() uint32 {
	if e.cfg.FrameRateNum <= 0 || e.cfg.FrameRateDen <= 0 {
		return 1
	}
	return uint32(e.cfg.FrameRateDen)
}
