//go:build darwin

// VP9 / AV1 decode for VideoToolbox. Unlike H.264/H.265 (Annex-B NAL units +
// CMVideoFormatDescriptionCreateFrom*ParameterSets), VP9 and AV1 carry no NAL
// parameter sets: a video.Packet is one coded VP9 frame/superframe or one AV1
// temporal unit (OBU stream). The format description is built from the codec
// type + the frame dimensions parsed out of the bitstream, plus — for AV1 — an
// av1C configuration-record atom carried under SampleDescriptionExtensionAtoms
// (VideoToolbox's AV1 decoder requires it). The raw frame bytes are submitted
// whole as the sample data.
//
// AV1 hardware decode is available on Apple silicon from the M3 generation;
// VP9 has no Apple-silicon hardware decoder, so VTDecompressionSessionCreate
// for VP9 fails on those hosts and the decoder surfaces ErrBackendFailure
// (the probe already reports VP9 decode=false there, so callers skip it).

package hwaccel

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// decodeOBUOrFrame decodes one VP9 frame/superframe or AV1 temporal unit.
func (d *vtDecoder) decodeOBUOrFrame(p video.Packet) ([]video.Frame, error) {
	w, h, ok := d.parseDimsForVTAV1VP9(p.Data)
	if !ok {
		return nil, ErrBitstreamParse
	}
	if err := d.ensureSessionAV1VP9(p.Data, w, h); err != nil {
		return nil, err
	}
	if d.session == 0 {
		return nil, ErrParameterSetsMissing
	}

	sb, err := d.makeRawSampleBuffer(p.Data)
	if err != nil {
		return nil, err
	}
	defer d.lib.CFRelease(sb)

	var infoFlags uint32
	st := d.lib.VTDecompressionSessionDecodeFrame(
		d.session, sb, kVTDecodeFrameFlagsSync, 0, &infoFlags)
	if st != noErr {
		return nil, fmt.Errorf("%w: VTDecompressionSessionDecodeFrame(av1/vp9) OSStatus=%d", ErrBackendFailure, st)
	}
	if d.lib.VTDecompressionSessionWaitForAsynchronousFrames != nil {
		if st := d.lib.VTDecompressionSessionWaitForAsynchronousFrames(d.session); st != noErr {
			return nil, fmt.Errorf("%w: WaitForAsynchronousFrames(av1/vp9) OSStatus=%d", ErrBackendFailure, st)
		}
	}
	d.frameIdx++
	return d.drain()
}

// parseDimsForVTAV1VP9 parses the coded dimensions from the first VP9 frame or
// the AV1 sequence/frame headers.
func (d *vtDecoder) parseDimsForVTAV1VP9(data []byte) (int, int, bool) {
	if d.codec == video.VP9 {
		frames := splitVP9Superframe(data)
		if len(frames) == 0 {
			return 0, 0, false
		}
		h, err := parseVP9UncompressedHeader(frames[0])
		if err != nil || h.width <= 0 || h.height <= 0 {
			return 0, 0, false
		}
		return h.width, h.height, true
	}
	// AV1: parse seq + frame header.
	seq, fh, ok := parseAV1TUHeaders(data, d.av1Seq)
	if !ok {
		return 0, 0, false
	}
	d.av1Seq = seq
	return fh.frameWidth, fh.frameHeight, true
}

// parseAV1TUHeaders walks a temporal unit and returns the sequence + frame
// header (using a cached seq header if the TU omits one).
func parseAV1TUHeaders(tu []byte, cachedSeq *av1SeqHeader) (*av1SeqHeader, *av1FrameHeader, bool) {
	obus := splitAV1OBUs(tu)
	seq := cachedSeq
	var fh *av1FrameHeader
	for _, o := range obus {
		switch o.typ {
		case av1OBUSequenceHeader:
			s, err := parseAV1SeqHeader(o.payload)
			if err != nil {
				return nil, nil, false
			}
			seq = s
		case av1OBUFrame, av1OBUFrameHeader:
			if seq == nil {
				return nil, nil, false
			}
			h := &av1FrameHeader{}
			r := &av1Reader{data: o.payload}
			if err := parseAV1FrameHeader(r, seq, h, o.temporalID, o.spatialID); err != nil {
				return nil, nil, false
			}
			fh = h
		}
	}
	if seq == nil || fh == nil {
		return nil, nil, false
	}
	return seq, fh, true
}

// ensureSessionAV1VP9 (re)builds the format description + decompression session
// for the given dimensions if one is not already current.
func (d *vtDecoder) ensureSessionAV1VP9(data []byte, w, h int) error {
	if d.session != 0 && int(d.sessionW) == w && int(d.sessionH) == h {
		return nil
	}
	if d.session != 0 {
		d.lib.VTDecompressionSessionInvalidate(d.session)
		d.lib.CFRelease(d.session)
		d.session = 0
	}
	if d.formats != 0 {
		d.lib.CFRelease(d.formats)
		d.formats = 0
	}

	fd, err := d.makeAV1VP9FormatDescription(data, w, h)
	if err != nil {
		return err
	}

	rec := decompressionOutputRecord{Callback: decodeCallbackTrampoline(), RefCon: d.refcon}
	var session uintptr
	st := d.lib.VTDecompressionSessionCreate(0, fd, 0, 0, unsafe.Pointer(&rec), &session)
	if st != noErr || session == 0 {
		d.lib.CFRelease(fd)
		return fmt.Errorf("%w: VTDecompressionSessionCreate(av1/vp9) OSStatus=%d (codec %s may lack a hardware decoder on this host)",
			ErrBackendFailure, st, d.codec)
	}
	d.session = session
	d.formats = fd
	d.sessionW = int32(w)
	d.sessionH = int32(h)
	return nil
}

// makeAV1VP9FormatDescription builds the CMVideoFormatDescription. For AV1 it
// attaches the av1C configuration record (built from the sequence-header OBU)
// under kCMFormatDescriptionExtension_SampleDescriptionExtensionAtoms; VP9
// needs only the codec type + dimensions.
func (d *vtDecoder) makeAV1VP9FormatDescription(data []byte, w, h int) (uintptr, error) {
	l := d.lib
	if l.CMVideoFormatDescriptionCreate == nil {
		return 0, fmt.Errorf("%w: CMVideoFormatDescriptionCreate unavailable", ErrBackendFailure)
	}

	var ext uintptr
	if d.codec == video.AV1 {
		atom := buildAV1CConfigRecord(data)
		if atom == nil {
			return 0, ErrBitstreamParse
		}
		ext = d.makeAV1CExtensions(atom)
		if ext == 0 {
			return 0, fmt.Errorf("%w: failed to build av1C extensions dictionary", ErrBackendFailure)
		}
		defer l.CFRelease(ext)
	}

	var fd uintptr
	st := l.CMVideoFormatDescriptionCreate(0, d.codecType, int32(w), int32(h), ext, &fd)
	if st != noErr || fd == 0 {
		return 0, fmt.Errorf("%w: CMVideoFormatDescriptionCreate(%s) OSStatus=%d", ErrBackendFailure, d.codec, st)
	}
	return fd, nil
}

// makeAV1CExtensions wraps the av1C atom bytes in
// {SampleDescriptionExtensionAtoms: {"av1C": <CFData>}}.
func (d *vtDecoder) makeAV1CExtensions(atom []byte) uintptr {
	l := d.lib
	data := l.CFDataCreate(0, &atom[0], int64(len(atom)))
	if data == 0 {
		return 0
	}
	defer l.CFRelease(data)

	av1CKey := l.cfString("av1C")
	defer l.CFRelease(av1CKey)
	innerKeys := []uintptr{av1CKey}
	innerVals := []uintptr{data}
	inner := l.CFDictionaryCreate(0, &innerKeys[0], &innerVals[0], 1,
		l.kCFTypeDictionaryKeyCallBacks, l.kCFTypeDictionaryValueCallBacks)
	if inner == 0 {
		return 0
	}
	defer l.CFRelease(inner)

	atomsKey := l.cfString("SampleDescriptionExtensionAtoms")
	defer l.CFRelease(atomsKey)
	outerKeys := []uintptr{atomsKey}
	outerVals := []uintptr{inner}
	return l.CFDictionaryCreate(0, &outerKeys[0], &outerVals[0], 1,
		l.kCFTypeDictionaryKeyCallBacks, l.kCFTypeDictionaryValueCallBacks)
}

// makeRawSampleBuffer wraps a whole coded frame/temporal-unit in a
// CMBlockBuffer (its own allocation) + CMSampleBuffer carrying the current
// format description.
func (d *vtDecoder) makeRawSampleBuffer(frame []byte) (uintptr, error) {
	l := d.lib
	var bb uintptr
	st := l.CMBlockBufferCreateWithMemoryBlock(
		0, nil, uint64(len(frame)), 0, 0, 0, uint64(len(frame)),
		kCMBlockBufferAssureMemoryNowFlag, &bb)
	if st != noErr || bb == 0 {
		return 0, fmt.Errorf("%w: CMBlockBufferCreateWithMemoryBlock(raw) OSStatus=%d", ErrBackendFailure, st)
	}
	if st := l.CMBlockBufferReplaceDataBytes(unsafe.Pointer(&frame[0]), bb, 0, uint64(len(frame))); st != noErr {
		l.CFRelease(bb)
		return 0, fmt.Errorf("%w: CMBlockBufferReplaceDataBytes(raw) OSStatus=%d", ErrBackendFailure, st)
	}
	sampleSize := uint64(len(frame))
	var sb uintptr
	st = l.CMSampleBufferCreateReady(0, bb, d.formats, 1, 0, nil, 1, &sampleSize, &sb)
	l.CFRelease(bb)
	if st != noErr || sb == 0 {
		return 0, fmt.Errorf("%w: CMSampleBufferCreateReady(raw) OSStatus=%d", ErrBackendFailure, st)
	}
	runtime.KeepAlive(frame)
	return sb, nil
}
