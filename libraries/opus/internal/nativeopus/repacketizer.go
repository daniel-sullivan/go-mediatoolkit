package nativeopus

// Port of libopus/src/repacketizer.c.
//
// The repacketizer combines or splits Opus packets sharing a compatible
// TOC byte, producing a single bit-exact aggregate packet. The Go port
// mirrors the C file line-for-line, down to variable names and branch
// ordering, so parity with the C oracle is straightforward to verify.
//
// Extensions handling (opus_packet_extensions_count/parse/generate) is
// defined in extensions.go. When no padding/extensions are present —
// the default for packets produced by opus_encode() — the repacketizer
// exercises only the parse + emit paths, which are ported verbatim here.

// OpusRepacketizer — C: opus_private.h:39.
type OpusRepacketizer struct {
	toc               byte
	nb_frames         int
	frames            [48][]byte
	len               [48]opus_int16
	framesize         int
	paddings          [48][]byte
	padding_len       [48]opus_int32
	padding_nb_frames [48]byte
}

// opus_repacketizer_get_size — C: repacketizer.c:38. In Go the caller
// simply allocates the struct directly; the size value is kept for API
// symmetry with callers that might compare against a C peer.
func opus_repacketizer_get_size() int {
	// We cannot reproduce sizeof(OpusRepacketizer) bit-exactly against
	// C without a cgo hop; callers that need the C-reported size should
	// ask cgo directly. Return 0 so accidental reliance is caught early.
	return 0
}

// opus_repacketizer_init — C: repacketizer.c:43.
func opus_repacketizer_init(rp *OpusRepacketizer) *OpusRepacketizer {
	rp.nb_frames = 0
	return rp
}

// opus_repacketizer_create — C: repacketizer.c:49.
func opus_repacketizer_create() *OpusRepacketizer {
	rp := &OpusRepacketizer{}
	return opus_repacketizer_init(rp)
}

// opus_repacketizer_destroy — C: repacketizer.c:57. No-op in Go (GC).
func opus_repacketizer_destroy(rp *OpusRepacketizer) {
	_ = rp
}

// opus_repacketizer_cat_impl — C: repacketizer.c:62.
func opus_repacketizer_cat_impl(rp *OpusRepacketizer, data []byte, len_ opus_int32, self_delimited int) int {
	var tmp_toc byte
	var curr_nb_frames, ret int
	/* Set of check ToC */
	if len_ < 1 {
		return OPUS_INVALID_PACKET
	}
	if rp.nb_frames == 0 {
		rp.toc = data[0]
		rp.framesize = opus_packet_get_samples_per_frame(data, 8000)
	} else if (rp.toc & 0xFC) != (data[0] & 0xFC) {
		return OPUS_INVALID_PACKET
	}
	curr_nb_frames = opus_packet_get_nb_frames(data, len_)
	if curr_nb_frames < 1 {
		return OPUS_INVALID_PACKET
	}

	/* Check the 120 ms maximum packet size */
	if (curr_nb_frames+rp.nb_frames)*rp.framesize > 960 {
		return OPUS_INVALID_PACKET
	}

	// The C call writes into rp->frames[rp->nb_frames .. rp->nb_frames+count-1]
	// and rp->len[...] as arrays starting at the offset. We build slice
	// views into the backing arrays to mirror that.
	framesView := rp.frames[rp.nb_frames:]
	sizeView := rp.len[rp.nb_frames:]
	var padSlot []byte
	var padLenSlot opus_int32
	ret = opus_packet_parse_impl(data, len_, self_delimited, &tmp_toc, framesView, sizeView,
		nil, nil, &padSlot, &padLenSlot)
	if ret < 1 {
		return ret
	}
	rp.paddings[rp.nb_frames] = padSlot
	rp.padding_len[rp.nb_frames] = padLenSlot
	rp.padding_nb_frames[rp.nb_frames] = byte(ret)

	/* set padding length to zero for all but the first frame */
	for curr_nb_frames > 1 {
		rp.nb_frames++
		rp.padding_len[rp.nb_frames] = 0
		rp.padding_nb_frames[rp.nb_frames] = 0
		rp.paddings[rp.nb_frames] = nil
		curr_nb_frames--
	}
	rp.nb_frames++
	return OPUS_OK
}

// opus_repacketizer_cat — C: repacketizer.c:104.
func opus_repacketizer_cat(rp *OpusRepacketizer, data []byte, len_ opus_int32) int {
	return opus_repacketizer_cat_impl(rp, data, len_, 0)
}

// opus_repacketizer_get_nb_frames — C: repacketizer.c:109.
func opus_repacketizer_get_nb_frames(rp *OpusRepacketizer) int {
	return rp.nb_frames
}

// opus_repacketizer_out_range_impl — C: repacketizer.c:114.
func opus_repacketizer_out_range_impl(rp *OpusRepacketizer, begin, end int,
	data []byte, maxlen opus_int32, self_delimited, pad int,
	extensions []opus_extension_data, nb_extensions int) opus_int32 {

	var i, count int
	var tot_size opus_int32
	var len_ []opus_int16
	var frames [][]byte
	var ptr int /* index into data, mirrors C ptr pointer */
	var ones_begin, ones_end int = 0, 0
	var ext_begin, ext_len opus_int32 = 0, 0
	var ext_count, total_ext_count int

	if begin < 0 || begin >= end || end > rp.nb_frames {
		return OPUS_BAD_ARG
	}
	count = end - begin

	len_ = rp.len[begin:]
	frames = rp.frames[begin:]
	if self_delimited != 0 {
		tot_size = 1
		if len_[count-1] >= 252 {
			tot_size++
		}
	} else {
		tot_size = 0
	}

	/* figure out total number of extensions */
	total_ext_count = nb_extensions
	for i = begin; i < end; i++ {
		n := opus_packet_extensions_count(rp.paddings[i], rp.padding_len[i],
			int(rp.padding_nb_frames[i]))
		if n > 0 {
			total_ext_count += int(n)
		}
	}
	var all_extensions []opus_extension_data
	if total_ext_count > 0 {
		all_extensions = make([]opus_extension_data, total_ext_count)
	}
	/* copy over any extensions that were passed in */
	for ext_count = 0; ext_count < nb_extensions; ext_count++ {
		all_extensions[ext_count] = extensions[ext_count]
	}

	/* incorporate any extensions from the repacketizer padding */
	for i = begin; i < end; i++ {
		var j int
		var ret opus_int32
		var frame_ext_count opus_int32
		frame_ext_count = opus_int32(total_ext_count - ext_count)
		var targetSlice []opus_extension_data
		if all_extensions != nil {
			targetSlice = all_extensions[ext_count:]
		}
		ret = opus_packet_extensions_parse(rp.paddings[i], rp.padding_len[i],
			targetSlice, &frame_ext_count, int(rp.padding_nb_frames[i]))
		if ret < 0 {
			return OPUS_INTERNAL_ERROR
		}
		/* renumber the extension frame numbers */
		for j = 0; opus_int32(j) < frame_ext_count; j++ {
			all_extensions[ext_count+j].frame += i - begin
		}
		ext_count += int(frame_ext_count)
	}

	ptr = 0
	if count == 1 {
		/* Code 0 */
		tot_size += opus_int32(len_[0]) + 1
		if tot_size > maxlen {
			return OPUS_BUFFER_TOO_SMALL
		}
		data[ptr] = rp.toc & 0xFC
		ptr++
	} else if count == 2 {
		if len_[1] == len_[0] {
			/* Code 1 */
			tot_size += 2*opus_int32(len_[0]) + 1
			if tot_size > maxlen {
				return OPUS_BUFFER_TOO_SMALL
			}
			data[ptr] = (rp.toc & 0xFC) | 0x1
			ptr++
		} else {
			/* Code 2 */
			extra := opus_int32(0)
			if len_[0] >= 252 {
				extra = 1
			}
			tot_size += opus_int32(len_[0]) + opus_int32(len_[1]) + 2 + extra
			if tot_size > maxlen {
				return OPUS_BUFFER_TOO_SMALL
			}
			data[ptr] = (rp.toc & 0xFC) | 0x2
			ptr++
			ptr += encode_size(int(len_[0]), data[ptr:])
		}
	}
	if count > 2 || (pad != 0 && tot_size < maxlen) || ext_count > 0 {
		/* Code 3 */
		var vbr int
		var pad_amount opus_int32 = 0

		/* Restart the process for the padding case */
		ptr = 0
		if self_delimited != 0 {
			tot_size = 1
			if len_[count-1] >= 252 {
				tot_size++
			}
		} else {
			tot_size = 0
		}
		vbr = 0
		for i = 1; i < count; i++ {
			if len_[i] != len_[0] {
				vbr = 1
				break
			}
		}
		if vbr != 0 {
			tot_size += 2
			for i = 0; i < count-1; i++ {
				extra := opus_int32(0)
				if len_[i] >= 252 {
					extra = 1
				}
				tot_size += 1 + extra + opus_int32(len_[i])
			}
			tot_size += opus_int32(len_[count-1])

			if tot_size > maxlen {
				return OPUS_BUFFER_TOO_SMALL
			}
			data[ptr] = (rp.toc & 0xFC) | 0x3
			ptr++
			data[ptr] = byte(count) | 0x80
			ptr++
		} else {
			tot_size += opus_int32(count)*opus_int32(len_[0]) + 2
			if tot_size > maxlen {
				return OPUS_BUFFER_TOO_SMALL
			}
			data[ptr] = (rp.toc & 0xFC) | 0x3
			ptr++
			data[ptr] = byte(count)
			ptr++
		}
		if pad != 0 {
			pad_amount = maxlen - tot_size
		} else {
			pad_amount = 0
		}
		if ext_count > 0 {
			/* figure out how much space we need for the extensions */
			ext_len = opus_packet_extensions_generate(nil, maxlen-tot_size,
				all_extensions, opus_int32(ext_count), count, 0)
			if ext_len < 0 {
				return ext_len
			}
			if pad == 0 {
				if ext_len != 0 {
					pad_amount = ext_len + (ext_len+253)/254
				} else {
					pad_amount = ext_len + 1
				}
			}
		}
		if pad_amount != 0 {
			var nb_255s opus_int32
			data[1] |= 0x40
			nb_255s = (pad_amount - 1) / 255
			if tot_size+ext_len+nb_255s+1 > maxlen {
				return OPUS_BUFFER_TOO_SMALL
			}
			ext_begin = tot_size + pad_amount - ext_len
			/* Prepend 0x01 padding */
			ones_begin = int(tot_size + nb_255s + 1)
			ones_end = int(tot_size + pad_amount - ext_len)
			for i = 0; opus_int32(i) < nb_255s; i++ {
				data[ptr] = 255
				ptr++
			}
			data[ptr] = byte(pad_amount - 255*nb_255s - 1)
			ptr++
			tot_size += pad_amount
		}
		if vbr != 0 {
			for i = 0; i < count-1; i++ {
				ptr += encode_size(int(len_[i]), data[ptr:])
			}
		}
	}
	if self_delimited != 0 {
		sdlen := encode_size(int(len_[count-1]), data[ptr:])
		ptr += sdlen
	}
	/* Copy the actual data */
	for i = 0; i < count; i++ {
		/* Using OPUS_MOVE() instead of OPUS_COPY() in case we're doing in-place
		   padding from opus_packet_pad or opus_packet_unpad(). */
		copy(data[ptr:ptr+int(len_[i])], frames[i][:int(len_[i])])
		ptr += int(len_[i])
	}
	if ext_len > 0 {
		ret := opus_packet_extensions_generate(data[ext_begin:], ext_len,
			all_extensions, opus_int32(ext_count), count, 0)
		celt_assert(ret == ext_len)
	}
	for i = ones_begin; i < ones_end; i++ {
		data[i] = 0x01
	}
	if pad != 0 && ext_count == 0 {
		/* Fill padding with zeros. */
		for ptr < int(maxlen) {
			data[ptr] = 0
			ptr++
		}
	}
	return tot_size
}

// opus_repacketizer_out_range — C: repacketizer.c:325.
func opus_repacketizer_out_range(rp *OpusRepacketizer, begin, end int, data []byte, maxlen opus_int32) opus_int32 {
	return opus_repacketizer_out_range_impl(rp, begin, end, data, maxlen, 0, 0, nil, 0)
}

// opus_repacketizer_out — C: repacketizer.c:330.
func opus_repacketizer_out(rp *OpusRepacketizer, data []byte, maxlen opus_int32) opus_int32 {
	return opus_repacketizer_out_range_impl(rp, 0, rp.nb_frames, data, maxlen, 0, 0, nil, 0)
}

// opus_packet_pad_impl — C: repacketizer.c:335.
func opus_packet_pad_impl(data []byte, len_, new_len opus_int32, pad int,
	extensions []opus_extension_data, nb_extensions int) opus_int32 {
	var rp OpusRepacketizer
	var ret opus_int32
	if len_ < 1 {
		return OPUS_BAD_ARG
	}
	if len_ == new_len {
		return OPUS_OK
	} else if len_ > new_len {
		return OPUS_BAD_ARG
	}
	copyBuf := make([]byte, len_)
	opus_repacketizer_init(&rp)
	/* Moving payload to the end of the packet so we can do in-place padding */
	OPUS_COPY(copyBuf, data, int(len_))
	r := opus_repacketizer_cat(&rp, copyBuf, len_)
	if r != OPUS_OK {
		return opus_int32(r)
	}
	ret = opus_repacketizer_out_range_impl(&rp, 0, rp.nb_frames, data, new_len, 0, pad, extensions, nb_extensions)
	return ret
}

// opus_packet_pad — C: repacketizer.c:359.
func opus_packet_pad(data []byte, len_, new_len opus_int32) int {
	ret := opus_packet_pad_impl(data, len_, new_len, 1, nil, 0)
	if ret > 0 {
		return OPUS_OK
	}
	return int(ret)
}

// opus_packet_unpad — C: repacketizer.c:371.
func opus_packet_unpad(data []byte, len_ opus_int32) opus_int32 {
	var rp OpusRepacketizer
	var ret opus_int32
	var i int
	if len_ < 1 {
		return OPUS_BAD_ARG
	}
	opus_repacketizer_init(&rp)
	r := opus_repacketizer_cat(&rp, data, len_)
	if r < 0 {
		return opus_int32(r)
	}
	/* Discard all padding and extensions. */
	for i = 0; i < rp.nb_frames; i++ {
		rp.padding_len[i] = 0
		rp.paddings[i] = nil
	}
	ret = opus_repacketizer_out_range_impl(&rp, 0, rp.nb_frames, data, len_, 0, 0, nil, 0)
	celt_assert(ret > 0 && ret <= len_)
	return ret
}

// opus_multistream_packet_pad — C: repacketizer.c:392.
func opus_multistream_packet_pad(data []byte, len_, new_len opus_int32, nb_streams int) int {
	var s int
	var count int
	var toc byte
	var size [48]opus_int16
	var packet_offset opus_int32
	var amount opus_int32

	if len_ < 1 {
		return OPUS_BAD_ARG
	}
	if len_ == new_len {
		return OPUS_OK
	} else if len_ > new_len {
		return OPUS_BAD_ARG
	}
	amount = new_len - len_
	/* Seek to last stream */
	dpos := 0
	for s = 0; s < nb_streams-1; s++ {
		if len_ <= 0 {
			return OPUS_INVALID_PACKET
		}
		count = opus_packet_parse_impl(data[dpos:], len_, 1, &toc, nil,
			size[:], nil, &packet_offset, nil, nil)
		if count < 0 {
			return count
		}
		dpos += int(packet_offset)
		len_ -= packet_offset
	}
	return opus_packet_pad(data[dpos:], len_, len_+amount)
}

// opus_multistream_packet_unpad — C: repacketizer.c:423.
func opus_multistream_packet_unpad(data []byte, len_ opus_int32, nb_streams int) opus_int32 {
	var s int
	var toc byte
	var size [48]opus_int16
	var packet_offset opus_int32
	var rp OpusRepacketizer
	var dst_len opus_int32

	if len_ < 1 {
		return OPUS_BAD_ARG
	}
	dstPos := 0
	srcPos := 0
	dst_len = 0
	/* Unpad all frames */
	for s = 0; s < nb_streams; s++ {
		var ret opus_int32
		var i int
		var self_delimited int = 0
		if s != nb_streams-1 {
			self_delimited = 1
		}
		if len_ <= 0 {
			return OPUS_INVALID_PACKET
		}
		opus_repacketizer_init(&rp)
		r := opus_packet_parse_impl(data[srcPos:], len_, self_delimited, &toc, nil,
			size[:], nil, &packet_offset, nil, nil)
		if r < 0 {
			return opus_int32(r)
		}
		r = opus_repacketizer_cat_impl(&rp, data[srcPos:], packet_offset, self_delimited)
		if r < 0 {
			return opus_int32(r)
		}
		/* Discard all padding and extensions. */
		for i = 0; i < rp.nb_frames; i++ {
			rp.padding_len[i] = 0
			rp.paddings[i] = nil
		}
		ret = opus_repacketizer_out_range_impl(&rp, 0, rp.nb_frames, data[dstPos:], len_, self_delimited, 0, nil, 0)
		if ret < 0 {
			return ret
		}
		dst_len += ret
		dstPos += int(ret)
		srcPos += int(packet_offset)
		len_ -= packet_offset
	}
	return dst_len
}
