package nativeopus

// Exported accessors for the opus.go / repacketizer.go ports so the
// parity tests in libraries/opus/internal/parity_tests/benchcmp/ can
// call the unexported functions. These are test-only; production code
// should not depend on them.

// ExportTestOpusPacketGetBandwidth — parity wrapper.
func ExportTestOpusPacketGetBandwidth(data []byte) int {
	return opus_packet_get_bandwidth(data)
}

// ExportTestOpusPacketGetSamplesPerFrame — parity wrapper.
func ExportTestOpusPacketGetSamplesPerFrame(data []byte, Fs int32) int {
	return opus_packet_get_samples_per_frame(data, opus_int32(Fs))
}

// ExportTestOpusPacketGetNbChannels — parity wrapper.
func ExportTestOpusPacketGetNbChannels(data []byte) int {
	return opus_packet_get_nb_channels(data)
}

// ExportTestOpusPacketGetNbFrames — parity wrapper.
func ExportTestOpusPacketGetNbFrames(packet []byte, length int32) int {
	return opus_packet_get_nb_frames(packet, opus_int32(length))
}

// ExportTestOpusPacketGetNbSamples — parity wrapper.
func ExportTestOpusPacketGetNbSamples(packet []byte, length, Fs int32) int {
	return opus_packet_get_nb_samples(packet, opus_int32(length), opus_int32(Fs))
}

// ExportTestEncodeSize — parity wrapper for the 1-or-2-byte size encoder.
func ExportTestEncodeSize(size int, data []byte) int {
	return encode_size(size, data)
}

// OpusPacketParseResult collects the multiple-return values of
// opus_packet_parse in a shape the parity test can compare.
type OpusPacketParseResult struct {
	Ret           int
	Toc           byte
	FrameOffsets  []int // offset of each frame relative to start of `data`
	Sizes         []int16
	PayloadOffset int
}

// ExportTestOpusPacketParse — parity wrapper. Returns the same
// information the C API exposes via out-pointers.
func ExportTestOpusPacketParse(data []byte) OpusPacketParseResult {
	var toc byte
	var frames [48][]byte
	var sizes [48]opus_int16
	var payloadOffset int
	ret := opus_packet_parse(data, opus_int32(len(data)), &toc, frames[:], sizes[:], &payloadOffset)
	res := OpusPacketParseResult{
		Ret:           ret,
		Toc:           toc,
		PayloadOffset: payloadOffset,
	}
	if ret > 0 {
		res.FrameOffsets = make([]int, ret)
		res.Sizes = make([]int16, ret)
		for i := 0; i < ret; i++ {
			// Compute offset = len(data) - len(frames[i]) since frames[i]
			// shares the backing array.
			res.FrameOffsets[i] = len(data) - len(frames[i])
			res.Sizes[i] = int16(sizes[i])
		}
	}
	return res
}

// ExportTestRepacketizerCatOut exercises the init → cat → out pipeline
// with a sequence of input packets, producing a single aggregate packet.
// The returned (out, ret) tuple mirrors the C output buffer + return
// code for side-by-side comparison.
func ExportTestRepacketizerCatOut(packets [][]byte, maxlen int) ([]byte, int32) {
	rp := opus_repacketizer_create()
	defer opus_repacketizer_destroy(rp)
	for _, p := range packets {
		r := opus_repacketizer_cat(rp, p, opus_int32(len(p)))
		if r != OPUS_OK {
			return nil, int32(r)
		}
	}
	out := make([]byte, maxlen)
	n := opus_repacketizer_out(rp, out, opus_int32(maxlen))
	if n < 0 {
		return nil, int32(n)
	}
	return out[:n], int32(n)
}

// ExportTestOpusPacketPad pads a packet to new_len in place and
// returns the updated buffer.
func ExportTestOpusPacketPad(data []byte, newLen int) ([]byte, int) {
	// Allocate a buffer the target size so we can pad in place.
	buf := make([]byte, newLen)
	copy(buf, data)
	ret := opus_packet_pad(buf, opus_int32(len(data)), opus_int32(newLen))
	return buf, ret
}

// ExportTestOpusPacketUnpad strips padding/extensions from a packet
// and returns the shortened buffer.
func ExportTestOpusPacketUnpad(data []byte) ([]byte, int32) {
	buf := make([]byte, len(data))
	copy(buf, data)
	ret := opus_packet_unpad(buf, opus_int32(len(data)))
	if ret < 0 {
		return nil, int32(ret)
	}
	return buf[:ret], int32(ret)
}
