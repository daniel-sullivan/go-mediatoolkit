package nativeopus

// Port of libopus/src/extensions.c.
//
// The C code traverses a packet's extension payload via raw
// `const unsigned char *` pointers that are repeatedly advanced
// and compared. Go has no pointer arithmetic on slices, so we
// represent each "pointer" as an (opus_int32) byte offset into
// the caller-supplied `data` slice. The mapping is mechanical:
//
//   C pointer  `p`                  -> Go offset `p_off`   (opus_int32)
//   p += n                          -> p_off += n
//   p++                             -> p_off++
//   *p                              -> data[p_off]
//   p[1]                            -> data[p_off+1]
//   p - data                        -> p_off - data_off
//   p == q                          -> p_off == q_off
//   p == NULL                       -> p_off == -1 (sentinel)
//
// Offsets are always relative to the original `iter.data`, which
// matches the C invariant checked by
// `iter->curr_data - iter->data == iter->len - iter->curr_len`.
//
// The public entry points opus_packet_extensions_count /
// opus_packet_extensions_count_ext / opus_packet_extensions_parse /
// opus_packet_extensions_parse_ext / opus_packet_extensions_generate
// keep their C names so the rest of the ported codebase can call
// them 1:1.

// opus_extension_data — port of the C struct. The `data` field
// holds a byte slice that starts at the extension payload (i.e.,
// after the length bytes) for decode results, or is provided by
// the caller for encode inputs. `len` is duplicated because the
// C struct carries it separately from the pointer.
type opus_extension_data struct {
	id    int
	frame int
	data  []byte
	len   opus_int32
}

// OpusExtensionIterator — port of the C struct at
// libopus/src/opus_private.h:50-66. Pointer fields are replaced
// with byte offsets into `data`; a sentinel of -1 represents NULL
// for the optional pointers (last_long, src_data when unset).
type OpusExtensionIterator struct {
	data               []byte
	curr_data_off      opus_int32
	repeat_data_off    opus_int32
	last_long_off      opus_int32 // -1 == NULL
	src_data_off       opus_int32 // -1 == NULL
	len                opus_int32
	curr_len           opus_int32
	repeat_len         opus_int32
	src_len            opus_int32
	trailing_short_len opus_int32
	nb_frames          int
	frame_max          int
	curr_frame         int
	repeat_frame       int
	repeat_l           opus_uint8
}

// skip_extension_payload — port of the static C helper. `pdata_off`
// is an in/out offset into `data`; it is advanced past the
// extension payload. Returns the new remaining length, or -1 on
// truncation. `*pheader_size` receives the number of header bytes
// consumed (used by the caller to compute payload location).
func skip_extension_payload(data []byte, pdata_off *opus_int32,
	len_ opus_int32, pheader_size *opus_int32, id_byte int,
	trailing_short_len opus_int32) opus_int32 {
	var data_off opus_int32
	var header_size opus_int32
	var id, L int
	data_off = *pdata_off
	header_size = 0
	id = id_byte >> 1
	L = id_byte & 1
	if (id == 0 && L == 1) || id == 2 {
		/* Nothing to do. */
	} else if id > 0 && id < 32 {
		if len_ < opus_int32(L) {
			return -1
		}
		data_off += opus_int32(L)
		len_ -= opus_int32(L)
	} else {
		if L == 0 {
			if len_ < trailing_short_len {
				return -1
			}
			data_off += len_ - trailing_short_len
			len_ = trailing_short_len
		} else {
			var bytes_ opus_int32 = 0
			var lacing opus_int32
			for {
				if len_ < 1 {
					return -1
				}
				lacing = opus_int32(data[data_off])
				data_off++
				bytes_ += lacing
				header_size++
				len_ -= lacing + 1
				if lacing != 255 {
					break
				}
			}
			if len_ < 0 {
				return -1
			}
			data_off += bytes_
		}
	}
	*pdata_off = data_off
	*pheader_size = header_size
	return len_
}

// skip_extension — port of the static C helper.
func skip_extension(data []byte, pdata_off *opus_int32, len_ opus_int32,
	pheader_size *opus_int32) opus_int32 {
	var data_off opus_int32
	var id_byte int
	if len_ == 0 {
		*pheader_size = 0
		return 0
	}
	if len_ < 1 {
		return -1
	}
	data_off = *pdata_off
	id_byte = int(data[data_off])
	data_off++
	len_--
	len_ = skip_extension_payload(data, &data_off, len_, pheader_size, id_byte, 0)
	if len_ >= 0 {
		*pdata_off = data_off
		*pheader_size++
	}
	return len_
}

// opus_extension_iterator_init — port of the C function.
func opus_extension_iterator_init(iter *OpusExtensionIterator,
	data []byte, len_ opus_int32, nb_frames opus_int32) {
	celt_assert(len_ >= 0)
	celt_assert(data != nil || len_ == 0)
	celt_assert(nb_frames >= 0 && nb_frames <= 48)
	iter.data = data
	iter.repeat_data_off = 0
	iter.curr_data_off = 0
	iter.last_long_off = -1
	iter.src_data_off = -1
	iter.curr_len = len_
	iter.len = len_
	iter.repeat_len = 0
	iter.src_len = 0
	iter.trailing_short_len = 0
	iter.frame_max = int(nb_frames)
	iter.nb_frames = int(nb_frames)
	iter.repeat_frame = 0
	iter.curr_frame = 0
	iter.repeat_l = 0
}

// opus_extension_iterator_reset — port of the C function.
func opus_extension_iterator_reset(iter *OpusExtensionIterator) {
	iter.repeat_data_off = 0
	iter.curr_data_off = 0
	iter.last_long_off = -1
	iter.curr_len = iter.len
	iter.repeat_frame = 0
	iter.curr_frame = 0
	iter.trailing_short_len = 0
}

// opus_extension_iterator_set_frame_max — port of the C function.
func opus_extension_iterator_set_frame_max(iter *OpusExtensionIterator,
	frame_max int) {
	iter.frame_max = frame_max
}

// opus_extension_iterator_next_repeat — port of the static C helper.
func opus_extension_iterator_next_repeat(iter *OpusExtensionIterator,
	ext *opus_extension_data) int {
	var header_size opus_int32
	celt_assert(iter.repeat_frame > 0)
	for ; iter.repeat_frame < iter.nb_frames; iter.repeat_frame++ {
		for iter.src_len > 0 {
			var curr_data0_off opus_int32
			var repeat_id_byte int
			repeat_id_byte = int(iter.data[iter.src_data_off])
			iter.src_len = skip_extension(iter.data, &iter.src_data_off,
				iter.src_len, &header_size)
			/* We skipped this extension earlier, so it should not fail now. */
			celt_assert(iter.src_len >= 0)
			/* Don't repeat padding or frame separators with a 0 increment. */
			if repeat_id_byte <= 3 {
				continue
			}
			/* If the "Repeat These Extensions" extension had L == 0 and this
			   is the last repeated long extension, then force decoding the
			   payload with L = 0. */
			if iter.repeat_l == 0 &&
				iter.repeat_frame+1 >= iter.nb_frames &&
				iter.src_data_off == iter.last_long_off {
				repeat_id_byte &= ^1
			}
			curr_data0_off = iter.curr_data_off
			iter.curr_len = skip_extension_payload(iter.data, &iter.curr_data_off,
				iter.curr_len, &header_size, repeat_id_byte,
				iter.trailing_short_len)
			if iter.curr_len < 0 {
				return OPUS_INVALID_PACKET
			}
			celt_assert(iter.curr_data_off == iter.len-iter.curr_len)
			/* If we were asked to stop at frame_max, skip extensions for later
			   frames. */
			if iter.repeat_frame >= iter.frame_max {
				continue
			}
			if ext != nil {
				ext.id = repeat_id_byte >> 1
				ext.frame = iter.repeat_frame
				ext.data = iter.data[curr_data0_off+header_size:]
				ext.len = iter.curr_data_off - curr_data0_off - header_size
			}
			return 1
		}
		/* We finished repeating the extensions for this frame. */
		iter.src_data_off = iter.repeat_data_off
		iter.src_len = iter.repeat_len
	}
	/* We finished repeating extensions. */
	iter.repeat_data_off = iter.curr_data_off
	iter.last_long_off = -1
	/* If L == 0, advance the frame number to handle the case where we did
	   not consume all of the data with an L == 0 long extension. */
	if iter.repeat_l == 0 {
		iter.curr_frame++
		/* Ignore additional padding if this was already the last frame. */
		if iter.curr_frame >= iter.nb_frames {
			iter.curr_len = 0
		}
	}
	iter.repeat_frame = 0
	return 0
}

// opus_extension_iterator_next — port of the C function.
func opus_extension_iterator_next(iter *OpusExtensionIterator,
	ext *opus_extension_data) int {
	var header_size opus_int32
	if iter.curr_len < 0 {
		return OPUS_INVALID_PACKET
	}
	if iter.repeat_frame > 0 {
		var ret int
		/* We are in the process of repeating some extensions. */
		ret = opus_extension_iterator_next_repeat(iter, ext)
		if ret != 0 {
			return ret
		}
	}
	/* Checking this here allows opus_extension_iterator_set_frame_max() to be
	   called at any point. */
	if iter.curr_frame >= iter.frame_max {
		return 0
	}
	for iter.curr_len > 0 {
		var curr_data0_off opus_int32
		var id int
		var L int
		curr_data0_off = iter.curr_data_off
		id = int(iter.data[curr_data0_off]) >> 1
		L = int(iter.data[curr_data0_off]) & 1
		iter.curr_len = skip_extension(iter.data, &iter.curr_data_off,
			iter.curr_len, &header_size)
		if iter.curr_len < 0 {
			return OPUS_INVALID_PACKET
		}
		celt_assert(iter.curr_data_off == iter.len-iter.curr_len)
		if id == 1 {
			if L == 0 {
				iter.curr_frame++
			} else {
				/* A frame increment of 0 is a no-op. */
				if iter.data[curr_data0_off+1] == 0 {
					continue
				}
				iter.curr_frame += int(iter.data[curr_data0_off+1])
			}
			if iter.curr_frame >= iter.nb_frames {
				iter.curr_len = -1
				return OPUS_INVALID_PACKET
			}
			/* If we were asked to stop at frame_max, skip extensions for later
			   frames. */
			if iter.curr_frame >= iter.frame_max {
				iter.curr_len = 0
			}
			iter.repeat_data_off = iter.curr_data_off
			iter.last_long_off = -1
			iter.trailing_short_len = 0
		} else if id == 2 {
			var ret int
			iter.repeat_l = opus_uint8(L)
			iter.repeat_frame = iter.curr_frame + 1
			iter.repeat_len = curr_data0_off - iter.repeat_data_off
			iter.src_data_off = iter.repeat_data_off
			iter.src_len = iter.repeat_len
			ret = opus_extension_iterator_next_repeat(iter, ext)
			if ret != 0 {
				return ret
			}
		} else if id > 2 {
			/* Update the location of the last long extension.
			   This lets us know when we need to modify the last L flag if we
			    repeat these extensions with L=0. */
			if id >= 32 {
				iter.last_long_off = iter.curr_data_off
				iter.trailing_short_len = 0
			} else {
				/* Otherwise, keep track of how many payload bytes follow the last
				   long extension. */
				iter.trailing_short_len += opus_int32(L)
			}
			if ext != nil {
				ext.id = id
				ext.frame = iter.curr_frame
				ext.data = iter.data[curr_data0_off+header_size:]
				ext.len = iter.curr_data_off - curr_data0_off - header_size
			}
			return 1
		}
	}
	return 0
}

// opus_extension_iterator_find — port of the C function.
func opus_extension_iterator_find(iter *OpusExtensionIterator,
	ext *opus_extension_data, id int) int {
	var curr_ext opus_extension_data
	var ret int
	for {
		ret = opus_extension_iterator_next(iter, &curr_ext)
		if ret <= 0 {
			return ret
		}
		if curr_ext.id == id {
			*ext = curr_ext
			return ret
		}
	}
}

// opus_packet_extensions_count — port of the C function.
func opus_packet_extensions_count(data []byte, len_ opus_int32,
	nb_frames int) opus_int32 {
	var iter OpusExtensionIterator
	var count opus_int32
	opus_extension_iterator_init(&iter, data, len_, opus_int32(nb_frames))
	for count = 0; opus_extension_iterator_next(&iter, nil) > 0; count++ {
	}
	return count
}

// opus_packet_extensions_count_ext — port of the C function.
func opus_packet_extensions_count_ext(data []byte, len_ opus_int32,
	nb_frame_exts []opus_int32, nb_frames int) opus_int32 {
	var iter OpusExtensionIterator
	var ext opus_extension_data
	var count opus_int32
	opus_extension_iterator_init(&iter, data, len_, opus_int32(nb_frames))
	OPUS_CLEAR(nb_frame_exts, nb_frames)
	for count = 0; opus_extension_iterator_next(&iter, &ext) > 0; count++ {
		nb_frame_exts[ext.frame]++
	}
	return count
}

// opus_packet_extensions_parse — port of the C function.
func opus_packet_extensions_parse(data []byte, len_ opus_int32,
	extensions []opus_extension_data, nb_extensions *opus_int32,
	nb_frames int) opus_int32 {
	var iter OpusExtensionIterator
	var count opus_int32
	var ret int
	celt_assert(nb_extensions != nil)
	celt_assert(extensions != nil || *nb_extensions == 0)
	opus_extension_iterator_init(&iter, data, len_, opus_int32(nb_frames))
	for count = 0; ; count++ {
		var ext opus_extension_data
		ret = opus_extension_iterator_next(&iter, &ext)
		if ret <= 0 {
			break
		}
		if count == *nb_extensions {
			return OPUS_BUFFER_TOO_SMALL
		}
		extensions[count] = ext
	}
	*nb_extensions = count
	return opus_int32(ret)
}

// opus_packet_extensions_parse_ext — port of the C function.
func opus_packet_extensions_parse_ext(data []byte, len_ opus_int32,
	extensions []opus_extension_data, nb_extensions *opus_int32,
	nb_frame_exts []opus_int32, nb_frames int) opus_int32 {
	var iter OpusExtensionIterator
	var ext opus_extension_data
	var nb_frames_cum [49]opus_int32
	var count opus_int32
	var prev_total opus_int32
	var ret int
	celt_assert(nb_extensions != nil)
	celt_assert(extensions != nil || *nb_extensions == 0)
	celt_assert(nb_frames <= 48)
	/* Convert the frame extension count array to a cumulative sum. */
	prev_total = 0
	for count = 0; int(count) < nb_frames; count++ {
		var total opus_int32
		total = nb_frame_exts[count] + prev_total
		nb_frames_cum[count] = prev_total
		prev_total = total
	}
	nb_frames_cum[count] = prev_total
	opus_extension_iterator_init(&iter, data, len_, opus_int32(nb_frames))
	for count = 0; ; count++ {
		var idx opus_int32
		ret = opus_extension_iterator_next(&iter, &ext)
		if ret <= 0 {
			break
		}
		idx = nb_frames_cum[ext.frame]
		nb_frames_cum[ext.frame]++
		if idx >= *nb_extensions {
			return OPUS_BUFFER_TOO_SMALL
		}
		celt_assert(idx < nb_frames_cum[ext.frame+1])
		extensions[idx] = ext
	}
	*nb_extensions = count
	return opus_int32(ret)
}

// write_extension_payload — port of the static C helper.
// `data` may be nil to compute only the output length.
func write_extension_payload(data []byte, len_ opus_int32, pos opus_int32,
	ext *opus_extension_data, last int) opus_int32 {
	celt_assert(ext.id >= 3 && ext.id <= 127)
	if ext.id < 32 {
		if ext.len < 0 || ext.len > 1 {
			return OPUS_BAD_ARG
		}
		if ext.len > 0 {
			if len_-pos < ext.len {
				return OPUS_BUFFER_TOO_SMALL
			}
			if data != nil {
				data[pos] = ext.data[0]
			}
			pos++
		}
	} else {
		var length_bytes opus_int32
		if ext.len < 0 {
			return OPUS_BAD_ARG
		}
		length_bytes = 1 + ext.len/255
		if last != 0 {
			length_bytes = 0
		}
		if len_-pos < length_bytes+ext.len {
			return OPUS_BUFFER_TOO_SMALL
		}
		if last == 0 {
			var j opus_int32
			for j = 0; j < ext.len/255; j++ {
				if data != nil {
					data[pos] = 255
				}
				pos++
			}
			if data != nil {
				data[pos] = opus_uint8(ext.len % 255)
			}
			pos++
		}
		if data != nil {
			OPUS_COPY(data[pos:], ext.data, int(ext.len))
		}
		pos += ext.len
	}
	return pos
}

// write_extension — port of the static C helper.
func write_extension(data []byte, len_ opus_int32, pos opus_int32,
	ext *opus_extension_data, last int) opus_int32 {
	if len_-pos < 1 {
		return OPUS_BUFFER_TOO_SMALL
	}
	celt_assert(ext.id >= 3 && ext.id <= 127)
	if data != nil {
		var lowbit int
		if ext.id < 32 {
			lowbit = int(ext.len)
		} else {
			if last != 0 {
				lowbit = 0
			} else {
				lowbit = 1
			}
		}
		data[pos] = opus_uint8((ext.id << 1) + lowbit)
	}
	pos++
	return write_extension_payload(data, len_, pos, ext, last)
}

// opus_packet_extensions_generate — port of the C function.
func opus_packet_extensions_generate(data []byte, len_ opus_int32,
	extensions []opus_extension_data, nb_extensions opus_int32,
	nb_frames int, pad int) opus_int32 {
	var frame_min_idx [48]opus_int32
	var frame_max_idx [48]opus_int32
	var frame_repeat_idx [48]opus_int32
	var i opus_int32
	var f int
	var curr_frame int = 0
	var pos opus_int32 = 0
	var written opus_int32 = 0

	celt_assert(len_ >= 0)
	if nb_frames > 48 {
		return OPUS_BAD_ARG
	}

	/* Do a little work up-front to make this O(nb_extensions) instead of
	   O(nb_extensions*nb_frames) so long as the extensions are in frame
	   order (without requiring that they be in frame order). */
	for f = 0; f < nb_frames; f++ {
		frame_min_idx[f] = nb_extensions
	}
	OPUS_CLEAR(frame_max_idx[:], nb_frames)
	for i = 0; i < nb_extensions; i++ {
		f = extensions[i].frame
		if f < 0 || f >= nb_frames {
			return OPUS_BAD_ARG
		}
		if extensions[i].id < 3 || extensions[i].id > 127 {
			return OPUS_BAD_ARG
		}
		frame_min_idx[f] = opus_int32(IMIN(int(frame_min_idx[f]), int(i)))
		frame_max_idx[f] = opus_int32(IMAX(int(frame_max_idx[f]), int(i+1)))
	}
	for f = 0; f < nb_frames; f++ {
		frame_repeat_idx[f] = frame_min_idx[f]
	}
	for f = 0; f < nb_frames; f++ {
		var last_long_idx opus_int32
		var repeat_count int
		repeat_count = 0
		last_long_idx = -1
		if f+1 < nb_frames {
			for i = frame_min_idx[f]; i < frame_max_idx[f]; i++ {
				if extensions[i].frame == f {
					var g int
					/* Test if we can repeat this extension in future frames. */
					for g = f + 1; g < nb_frames; g++ {
						if frame_repeat_idx[g] >= frame_max_idx[g] {
							break
						}
						celt_assert(extensions[frame_repeat_idx[g]].frame == g)
						if extensions[frame_repeat_idx[g]].id != extensions[i].id {
							break
						}
						if extensions[frame_repeat_idx[g]].id < 32 &&
							extensions[frame_repeat_idx[g]].len != extensions[i].len {
							break
						}
					}
					if g < nb_frames {
						break
					}
					/* We can! */
					/* If this is a long extension, save the index of the last
					   instance, so we can modify its L flag. */
					if extensions[i].id >= 32 {
						last_long_idx = frame_repeat_idx[nb_frames-1]
					}
					/* Using the repeat mechanism almost always makes the
					    encoding smaller (or at least no larger).
					   However, there's one case where that might not be true: if
					    the last repeated long extension in the last frame was
					    previously the last extension, but using the repeat
					    mechanism makes that no longer true (because there are other
					    non-repeated extensions in earlier frames that must now be
					    coded after it), and coding its length requires more bytes
					    than the repeat mechanism saves.
					   This can only be true if its length is at least 255 bytes
					    (although sometimes it requires even more).
					   Currently we do not check for that, and just always use the
					    repeat mechanism if we can.
					   See git history for code that does the check. */
					/* Advance the repeat pointers. */
					for g = f + 1; g < nb_frames; g++ {
						var j opus_int32
						for j = frame_repeat_idx[g] + 1; j < frame_max_idx[g] &&
							extensions[j].frame != g; j++ {
						}
						frame_repeat_idx[g] = j
					}
					repeat_count++
					/* Point the repeat pointer for this frame to the current
					   extension, so we know when to trigger the repeats. */
					frame_repeat_idx[f] = i
				}
			}
		}
		for i = frame_min_idx[f]; i < frame_max_idx[f]; i++ {
			if extensions[i].frame == f {
				/* Insert separator when needed. */
				if f != curr_frame {
					var diff int = f - curr_frame
					if len_-pos < 2 {
						return OPUS_BUFFER_TOO_SMALL
					}
					if diff == 1 {
						if data != nil {
							data[pos] = 0x02
						}
						pos++
					} else {
						if data != nil {
							data[pos] = 0x03
						}
						pos++
						if data != nil {
							data[pos] = opus_uint8(diff)
						}
						pos++
					}
					curr_frame = f
				}

				{
					var last int
					if written == nb_extensions-1 {
						last = 1
					} else {
						last = 0
					}
					pos = write_extension(data, len_, pos, &extensions[i], last)
				}
				if pos < 0 {
					return pos
				}
				written++

				if repeat_count > 0 && frame_repeat_idx[f] == i {
					var nb_repeated int
					var last int
					var g int
					/* Add the repeat indicator. */
					nb_repeated = repeat_count * (nb_frames - (f + 1))
					if written+opus_int32(nb_repeated) == nb_extensions ||
						(last_long_idx < 0 && i+1 >= frame_max_idx[f]) {
						last = 1
					} else {
						last = 0
					}
					if len_-pos < 1 {
						return OPUS_BUFFER_TOO_SMALL
					}
					if data != nil {
						var v opus_uint8
						if last != 0 {
							v = 0x04
						} else {
							v = 0x05
						}
						data[pos] = v
					}
					pos++
					for g = f + 1; g < nb_frames; g++ {
						var j opus_int32
						for j = frame_min_idx[g]; j < frame_repeat_idx[g]; j++ {
							if extensions[j].frame == g {
								var sublast int
								if last != 0 && j == last_long_idx {
									sublast = 1
								} else {
									sublast = 0
								}
								pos = write_extension_payload(data, len_, pos,
									&extensions[j], sublast)
								if pos < 0 {
									return pos
								}
								written++
							}
						}
						frame_min_idx[g] = j
					}
					if last != 0 {
						curr_frame++
					}
				}
			}
		}
	}
	celt_assert(written == nb_extensions)
	/* If we need to pad, just prepend 0x01 bytes. Even better would be to fill the
	   end with zeros, but that requires checking that turning the last extension into
	   an L=1 case still fits. */
	if pad != 0 && pos < len_ {
		var padding opus_int32 = len_ - pos
		if data != nil {
			OPUS_MOVE(data[padding:], data, int(pos))
			for i = 0; i < padding; i++ {
				data[i] = 0x01
			}
		}
		pos += padding
	}
	return pos
}
