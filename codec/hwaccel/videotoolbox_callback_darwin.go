//go:build darwin

// The VTCompressionOutputCallback trampoline and the CMSampleBuffer ->
// Annex-B extraction it performs. The callback runs synchronously inside
// VTCompressionSessionCompleteFrames (we never enable async), so it
// appends to the owning encoder's pending slice without further locking
// of the encoder itself; the only shared structure is encoderRegistry,
// which is mutex-guarded.

package hwaccel

import (
	"unsafe"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
	"github.com/ebitengine/purego"
)

// startCode is the 4-byte Annex-B NAL start code.
var startCode = []byte{0x00, 0x00, 0x00, 0x01}

// newCompressionCallback wraps a Go function as a C-callable
// VTCompressionOutputCallback function pointer. Every parameter is
// uintptr-sized, satisfying purego.NewCallback's constraint.
func newCompressionCallback(fn func(outputRefCon, srcFrameRefCon uintptr, status int32, infoFlags uint32, sampleBuffer uintptr)) uintptr {
	return purego.NewCallback(fn)
}

// outputCallbackTrampoline returns the C-callable function pointer for
// the VTCompressionOutputCallback, creating it once. purego.NewCallback
// allocates a trampoline that is never freed, so we make exactly one and
// route by refcon.
func outputCallbackTrampoline() uintptr {
	outputCallbackOnce.Do(func() {
		outputCallbackPtr = newCompressionCallback(compressionOutput)
	})
	return outputCallbackPtr
}

// compressionOutput is the Go side of the VTCompressionOutputCallback:
//
//	void cb(void *outputRefCon, void *srcFrameRefCon, OSStatus status,
//	        VTEncodeInfoFlags infoFlags, CMSampleBufferRef sampleBuffer)
//
// outputRefCon is the integer encoder id we passed to
// VTCompressionSessionCreate. We resolve it to the *vtEncoder and append
// the extracted packet.
func compressionOutput(outputRefCon, srcFrameRefCon uintptr, status int32, infoFlags uint32, sampleBuffer uintptr) {
	encoderRegistryMu.Lock()
	e := encoderRegistry[outputRefCon]
	encoderRegistryMu.Unlock()
	if e == nil {
		return
	}
	if status != noErr {
		e.cbErr = status
		return
	}
	// A dropped/empty frame yields a nil sampleBuffer with noErr.
	if sampleBuffer == 0 {
		return
	}
	pkt, ok := e.extractPacket(sampleBuffer)
	if ok {
		e.pending = append(e.pending, pkt)
	}
}

// extractPacket converts one compressed CMSampleBuffer to a video.Packet
// in Annex-B form. Keyframes are detected from the sample's format
// description carrying parameter sets and are prefixed with those
// parameter sets so the keyframe is independently decodable.
func (e *vtEncoder) extractPacket(sampleBuffer uintptr) (video.Packet, bool) {
	l := e.lib

	bb := l.CMSampleBufferGetDataBuffer(sampleBuffer)
	if bb == 0 {
		return video.Packet{}, false
	}
	dataLen := l.CMBlockBufferGetDataLength(bb)
	if dataLen == 0 {
		return video.Packet{}, false
	}
	raw := make([]byte, dataLen)
	if st := l.CMBlockBufferCopyDataBytes(bb, 0, dataLen, unsafe.Pointer(&raw[0])); st != noErr {
		return video.Packet{}, false
	}

	// A frame is a keyframe iff its sample is a sync sample (the
	// NotSync attachment is absent / false). Parameter sets are read
	// from the format description and prefixed onto keyframes only, so
	// each keyframe is independently decodable without bloating P-frames.
	keyframe := e.isKeyframe(sampleBuffer)

	out := make([]byte, 0, len(raw)+64)
	if keyframe {
		out = append(out, e.readParameterSets(sampleBuffer)...)
	}
	out = append(out, avccToAnnexB(raw)...)

	pts := ptsToDuration(e.frameIdx-1, int32(roundRate(e.cfg.frameRate())))
	return video.Packet{
		Codec:    e.codec,
		Data:     out,
		Keyframe: keyframe,
		PTS:      pts,
		DTS:      pts, // no B-frames => DTS == PTS
	}, true
}

// isKeyframe reports whether the sample is a sync sample (keyframe). It
// reads the sample's attachments array: a frame is a keyframe unless its
// first attachment dictionary carries kCMSampleAttachmentKey_NotSync ==
// kCFBooleanTrue. A sample with no attachments array, an empty array, or
// no NotSync key is a sync sample.
func (e *vtEncoder) isKeyframe(sampleBuffer uintptr) bool {
	l := e.lib
	arr := l.CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, false)
	if arr == 0 || l.CFArrayGetCount(arr) == 0 {
		return true
	}
	dict := l.CFArrayGetValueAtIndex(arr, 0)
	if dict == 0 {
		return true
	}
	key := l.kCMSampleAttachmentKey_NotSync
	if key == 0 || !l.CFDictionaryContainsKey(dict, key) {
		return true
	}
	val := l.CFDictionaryGetValue(dict, key)
	// NotSync present and true => NOT a keyframe.
	if val != 0 && l.CFBooleanGetValue(val) {
		return false
	}
	return true
}

// readParameterSets reads every parameter set from the sample's format
// description and returns them concatenated in Annex-B form (each
// start-code-prefixed), in the canonical order the codec's
// GetParameterSetAtIndex enumerates them (SPS then PPS for H.264;
// VPS, SPS, PPS for H.265). Returns nil when the description carries no
// parameter sets — which, for VideoToolbox compressed output, means the
// sample is not a sync sample.
func (e *vtEncoder) readParameterSets(sampleBuffer uintptr) []byte {
	l := e.lib
	fd := l.CMSampleBufferGetFormatDescription(sampleBuffer)
	if fd == 0 {
		return nil
	}
	get := l.CMVideoFormatDescriptionGetH264ParameterSetAtIndex
	if e.codec == video.H265 {
		get = l.CMVideoFormatDescriptionGetHEVCParameterSetAtIndex
	}

	// First call with index 0 also reports the total count.
	var first *byte
	var firstSize, count uint64
	var nalHdrLen int32
	if st := get(fd, 0, &first, &firstSize, &count, &nalHdrLen); st != noErr || count == 0 {
		return nil
	}

	var out []byte
	for i := uint64(0); i < count; i++ {
		var ptr *byte
		var size uint64
		if st := get(fd, i, &ptr, &size, nil, nil); st != noErr || ptr == nil || size == 0 {
			continue
		}
		ps := unsafe.Slice(ptr, int(size))
		out = append(out, startCode...)
		out = append(out, ps...)
	}
	return out
}

// avccToAnnexB rewrites a length-prefixed (AVCC/HVCC) NAL sequence into
// Annex-B by replacing each 4-byte big-endian length prefix with the
// 4-byte start code. VideoToolbox always uses a 4-byte NAL length for
// its compressed output, so a fixed 4-byte stride is correct here.
func avccToAnnexB(b []byte) []byte {
	out := make([]byte, 0, len(b)+16)
	i := 0
	for i+4 <= len(b) {
		n := int(b[i])<<24 | int(b[i+1])<<16 | int(b[i+2])<<8 | int(b[i+3])
		i += 4
		if n <= 0 || i+n > len(b) {
			break
		}
		out = append(out, startCode...)
		out = append(out, b[i:i+n]...)
		i += n
	}
	return out
}
