//go:build cgo

package flac

/*
#include <FLAC/stream_decoder.h>
#include <FLAC/format.h>
#include <FLAC/metadata.h>
#include <stdlib.h>

// flacGoInitStream / flacGoSampleAt / flacGoStreamInfo /
// flacGoFrameBlocksize / flacGoFrameChannels are defined in
// flac_cgo_trampolines.c so they can include "_cgo_export.h" — the
// auto-generated header for the //export'd Go callbacks. That header
// is not available to this preamble (cgo processes preambles before
// generating it), so the trampoline lives in a separate translation
// unit.
extern FLAC__StreamDecoderInitStatus flacGoInitStream(FLAC__StreamDecoder *dec, void *client_data);
extern FLAC__int32                   flacGoSampleAt(const FLAC__int32 * const buffer[], int ch, int i);
extern const FLAC__StreamMetadata_StreamInfo *flacGoStreamInfo(const FLAC__StreamMetadata *m);
extern uint32_t                      flacGoFrameBlocksize(const FLAC__Frame *f);
extern uint32_t                      flacGoFrameChannels (const FLAC__Frame *f);

// VORBIS_COMMENT accessors. The block lays the comments and vendor
// inside an anonymous union, which Go-cgo cannot index directly.
extern const FLAC__StreamMetadata_VorbisComment      *flacGoVorbisComment      (const FLAC__StreamMetadata *m);
extern uint32_t                                       flacGoVorbisVendorLength (const FLAC__StreamMetadata_VorbisComment *vc);
extern const char                                    *flacGoVorbisVendorBytes  (const FLAC__StreamMetadata_VorbisComment *vc);
extern uint32_t                                       flacGoVorbisCommentCount (const FLAC__StreamMetadata_VorbisComment *vc);
extern uint32_t                                       flacGoVorbisCommentLength(const FLAC__StreamMetadata_VorbisComment *vc, uint32_t i);
extern const char                                    *flacGoVorbisCommentBytes (const FLAC__StreamMetadata_VorbisComment *vc, uint32_t i);
*/
import "C"

import (
	"io"
	"runtime/cgo"
	"unsafe"
)

// cgoDecoder is the libFLAC-backed Decoder.
type cgoDecoder struct {
	dec    *C.FLAC__StreamDecoder
	handle cgo.Handle
	r      io.Reader
	cfg    decoderConfig

	info     StreamInfo
	haveInfo bool

	vendor string
	tags   map[string][]string

	// pending is the interleaved sample buffer for the most recently
	// decoded frame; pos is the read offset into pending. When pos ==
	// len(pending) the next Decode call drives another frame.
	pending []int32
	pos     int

	// readErr is the most recent error returned by the Go-side reader;
	// surfaces out of Decode after the libFLAC call returns false.
	readErr error

	// decodeErr is set by flacGoErrorCallback when libFLAC reports a
	// decode-side error against the bitstream.
	decodeErr error

	eof    bool
	closed bool
}

func newDecoder(r io.Reader, cfg decoderConfig) (Decoder, error) {
	dec := C.FLAC__stream_decoder_new()
	if dec == nil {
		return nil, ErrAllocFail
	}

	d := &cgoDecoder{dec: dec, r: r, cfg: cfg}
	d.handle = cgo.NewHandle(d)

	if cfg.md5Check {
		C.FLAC__stream_decoder_set_md5_checking(dec, 1)
	}
	// Subscribe to VORBIS_COMMENT so the metadata callback fires for it
	// in addition to the default STREAMINFO subscription.
	C.FLAC__stream_decoder_set_metadata_respond(dec, C.FLAC__METADATA_TYPE_VORBIS_COMMENT)

	st := C.flacGoInitStream(dec, unsafe.Pointer(uintptr(d.handle)))
	if st != C.FLAC__STREAM_DECODER_INIT_STATUS_OK {
		d.handle.Delete()
		C.FLAC__stream_decoder_delete(dec)
		return nil, ErrInternal
	}
	return d, nil
}

func (d *cgoDecoder) ensureMetadata() error {
	if d.haveInfo {
		return nil
	}
	if C.FLAC__stream_decoder_process_until_end_of_metadata(d.dec) == 0 {
		return d.activeError(ErrInvalidStream)
	}
	if !d.haveInfo {
		return ErrInvalidStream
	}
	return nil
}

func (d *cgoDecoder) Decode(buf []int32) (int, error) {
	if d.closed {
		return 0, ErrClosed
	}
	if err := d.ensureMetadata(); err != nil {
		return 0, err
	}
	if len(buf) == 0 {
		return 0, nil
	}
	if len(buf)%d.info.Channels != 0 {
		return 0, ErrBadArg
	}

	written := 0
	for written < len(buf) {
		if d.pos < len(d.pending) {
			avail := len(d.pending) - d.pos
			n := len(buf) - written
			if n > avail {
				n = avail
			}
			// pending is whole blocks, so n is naturally a channel
			// multiple unless buf has fewer samples than channels —
			// guarded by the modulus check above.
			copy(buf[written:written+n], d.pending[d.pos:d.pos+n])
			d.pos += n
			written += n
			continue
		}
		if d.eof {
			break
		}
		if cap(buf)-written < d.info.MaxBlockSize*d.info.Channels && written > 0 {
			// Caller's buffer can't safely absorb another whole
			// block. Return what we have; the caller can try again.
			break
		}
		if C.FLAC__stream_decoder_process_single(d.dec) == 0 {
			if written > 0 {
				return written / d.info.Channels, d.activeError(ErrInvalidStream)
			}
			return 0, d.activeError(ErrInvalidStream)
		}
		state := C.FLAC__stream_decoder_get_state(d.dec)
		if state == C.FLAC__STREAM_DECODER_END_OF_STREAM {
			d.eof = true
			if d.cfg.md5Check {
				if C.FLAC__stream_decoder_finish(d.dec) == 0 {
					return written / d.info.Channels, ErrMD5Mismatch
				}
			}
		}
	}

	if written == 0 {
		return 0, io.EOF
	}
	if d.eof && d.pos == len(d.pending) {
		return written / d.info.Channels, io.EOF
	}
	return written / d.info.Channels, nil
}

// activeError prefers a Go-side reader error over a libFLAC one when both
// are set — the Go reader error is the more proximate cause.
func (d *cgoDecoder) activeError(fallback error) error {
	if d.readErr != nil {
		err := d.readErr
		d.readErr = nil
		return err
	}
	if d.decodeErr != nil {
		err := d.decodeErr
		d.decodeErr = nil
		return err
	}
	return fallback
}

func (d *cgoDecoder) StreamInfo() StreamInfo    { return d.info }
func (d *cgoDecoder) SampleRate() int           { return d.info.SampleRate }
func (d *cgoDecoder) Channels() int             { return d.info.Channels }
func (d *cgoDecoder) BitsPerSample() int        { return d.info.BitsPerSample }
func (d *cgoDecoder) Vendor() string            { return d.vendor }
func (d *cgoDecoder) Tags() map[string][]string { return d.tags }

func (d *cgoDecoder) Reset() error {
	seeker, ok := d.r.(io.Seeker)
	if !ok {
		return ErrUnsupportedStream
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if C.FLAC__stream_decoder_reset(d.dec) == 0 {
		return ErrInternal
	}
	d.haveInfo = false
	d.pending = d.pending[:0]
	d.pos = 0
	d.eof = false
	d.readErr = nil
	d.decodeErr = nil
	return nil
}

func (d *cgoDecoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	C.FLAC__stream_decoder_finish(d.dec)
	C.FLAC__stream_decoder_delete(d.dec)
	d.dec = nil
	d.handle.Delete()
	return nil
}

// ── Exported callbacks ─────────────────────────────────────────────────
//
// These are referenced from the C trampoline above; cgo synthesises a
// matching declaration when it sees //export.

//export flacGoReadCallback
func flacGoReadCallback(dec *C.FLAC__StreamDecoder, buf *C.FLAC__byte, bytes *C.size_t, clientData unsafe.Pointer) C.FLAC__StreamDecoderReadStatus {
	_ = dec
	d := cgo.Handle(uintptr(clientData)).Value().(*cgoDecoder)
	want := int(*bytes)
	if want == 0 {
		return C.FLAC__STREAM_DECODER_READ_STATUS_ABORT
	}
	goBuf := unsafe.Slice((*byte)(unsafe.Pointer(buf)), want)
	n, err := d.r.Read(goBuf)
	*bytes = C.size_t(n)
	if err == io.EOF {
		if n > 0 {
			return C.FLAC__STREAM_DECODER_READ_STATUS_CONTINUE
		}
		return C.FLAC__STREAM_DECODER_READ_STATUS_END_OF_STREAM
	}
	if err != nil {
		d.readErr = err
		return C.FLAC__STREAM_DECODER_READ_STATUS_ABORT
	}
	return C.FLAC__STREAM_DECODER_READ_STATUS_CONTINUE
}

//export flacGoWriteCallback
func flacGoWriteCallback(dec *C.FLAC__StreamDecoder, frame *C.FLAC__Frame, buffer **C.FLAC__int32, clientData unsafe.Pointer) C.FLAC__StreamDecoderWriteStatus {
	_ = dec
	d := cgo.Handle(uintptr(clientData)).Value().(*cgoDecoder)
	blockSize := int(C.flacGoFrameBlocksize(frame))
	channels := int(C.flacGoFrameChannels(frame))
	total := blockSize * channels
	if cap(d.pending) < total {
		d.pending = make([]int32, total)
	} else {
		d.pending = d.pending[:total]
	}
	d.pos = 0
	for ch := 0; ch < channels; ch++ {
		for i := 0; i < blockSize; i++ {
			d.pending[i*channels+ch] = int32(C.flacGoSampleAt(buffer, C.int(ch), C.int(i)))
		}
	}
	return C.FLAC__STREAM_DECODER_WRITE_STATUS_CONTINUE
}

//export flacGoErrorCallback
func flacGoErrorCallback(dec *C.FLAC__StreamDecoder, status C.FLAC__StreamDecoderErrorStatus, clientData unsafe.Pointer) {
	_ = dec
	_ = status
	d := cgo.Handle(uintptr(clientData)).Value().(*cgoDecoder)
	d.decodeErr = ErrInvalidStream
}

//export flacGoMetadataCallback
func flacGoMetadataCallback(dec *C.FLAC__StreamDecoder, metadata *C.FLAC__StreamMetadata, clientData unsafe.Pointer) {
	_ = dec
	d := cgo.Handle(uintptr(clientData)).Value().(*cgoDecoder)
	switch metadata._type {
	case C.FLAC__METADATA_TYPE_STREAMINFO:
		si := C.flacGoStreamInfo(metadata)
		d.info = StreamInfo{
			SampleRate:    int(si.sample_rate),
			Channels:      int(si.channels),
			BitsPerSample: int(si.bits_per_sample),
			MinBlockSize:  int(si.min_blocksize),
			MaxBlockSize:  int(si.max_blocksize),
			MinFrameSize:  int(si.min_framesize),
			MaxFrameSize:  int(si.max_framesize),
			TotalSamples:  uint64(si.total_samples),
		}
		for i := 0; i < 16; i++ {
			d.info.MD5Signature[i] = byte(si.md5sum[i])
		}
		d.haveInfo = true

	case C.FLAC__METADATA_TYPE_VORBIS_COMMENT:
		vc := C.flacGoVorbisComment(metadata)
		vendorLen := int(C.flacGoVorbisVendorLength(vc))
		if vendorLen > 0 {
			d.vendor = C.GoStringN(C.flacGoVorbisVendorBytes(vc), C.int(vendorLen))
		}
		count := uint32(C.flacGoVorbisCommentCount(vc))
		if count > 0 {
			d.tags = make(map[string][]string)
			for i := uint32(0); i < count; i++ {
				entryLen := int(C.flacGoVorbisCommentLength(vc, C.uint32_t(i)))
				if entryLen <= 0 {
					continue
				}
				entry := C.GoStringN(C.flacGoVorbisCommentBytes(vc, C.uint32_t(i)), C.int(entryLen))
				eq := -1
				for j := 0; j < len(entry); j++ {
					if entry[j] == '=' {
						eq = j
						break
					}
				}
				if eq <= 0 {
					continue
				}
				key := upper(entry[:eq])
				d.tags[key] = append(d.tags[key], entry[eq+1:])
			}
		}
	}
}
