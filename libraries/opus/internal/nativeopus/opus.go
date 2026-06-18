package nativeopus

// Port of libopus/src/opus.c.
//
// Top-level helpers for reading an Opus TOC byte and packet framing.
// The float-only soft-clip state machine is also ported here. All
// identifiers and control flow mirror the C source 1:1 so the mapping
// stays trivial to audit.

// OPUS_BANDWIDTH_* values. C: opus_defines.h.
const (
	OPUS_BANDWIDTH_NARROWBAND    = 1101
	OPUS_BANDWIDTH_MEDIUMBAND    = 1102
	OPUS_BANDWIDTH_WIDEBAND      = 1103
	OPUS_BANDWIDTH_SUPERWIDEBAND = 1104
	OPUS_BANDWIDTH_FULLBAND      = 1105
)

// opus_pcm_soft_clip_impl — soft-clip N*C interleaved samples in-place.
// C: opus.c:39.
func opus_pcm_soft_clip_impl(_x []float32, N int, C int, declip_mem []float32, arch int) {
	var c int
	var i int
	var x []float32
	var all_within_neg1pos1 int

	if C < 1 || N < 1 || _x == nil || declip_mem == nil {
		return
	}

	/* Clamp everything within the range [-2, +2] ... */
	all_within_neg1pos1 = opus_limit2_checkwithin1_c(_x, N*C)
	_ = arch

	for c = 0; c < C; c++ {
		var a float32
		var x0 float32
		var curr int

		x = _x[c:]
		a = declip_mem[c]
		/* Continue applying the non-linearity from the previous frame.
		   C: x[i*C] = x[i*C] + a*x[i*C]*x[i*C]
		   Left-to-right: x + ((a*x)*x) — three muls, not x + a*(x*x). */
		for i = 0; i < N; i++ {
			if mul_f32(x[i*C], a) >= 0 {
				break
			}
			x[i*C] = fma_add(x[i*C], mul_f32(a, x[i*C]), x[i*C])
		}

		curr = 0
		x0 = x[0]
		for {
			var start, end int
			var maxval float32
			var special int = 0
			var peak_pos int
			/* Detection for early exit can be skipped if hinted by `all_within_neg1pos1` */
			if all_within_neg1pos1 != 0 {
				i = N
			} else {
				for i = curr; i < N; i++ {
					if x[i*C] > 1 || x[i*C] < -1 {
						break
					}
				}
			}
			if i == N {
				a = 0
				break
			}
			peak_pos = i
			start = i
			end = i
			maxval = ABS16(x[i*C])
			/* Look for first zero crossing before clipping */
			for start > 0 && mul_f32(x[i*C], x[(start-1)*C]) >= 0 {
				start--
			}
			/* Look for first zero crossing after clipping */
			for end < N && mul_f32(x[i*C], x[end*C]) >= 0 {
				/* Look for other peaks until the next zero-crossing. */
				if ABS16(x[end*C]) > maxval {
					maxval = ABS16(x[end*C])
					peak_pos = end
				}
				end++
			}
			/* Detect the special case where we clip before the first zero crossing */
			if start == 0 && mul_f32(x[i*C], x[0]) >= 0 {
				special = 1
			} else {
				special = 0
			}

			/* Compute a such that maxval + a*maxval^2 = 1 */
			a = sub_f32(maxval, 1) / mul_f32(maxval, maxval)
			/* Slightly boost "a" by 2^-22. */
			a = fma_add(a, a, 2.4e-7)
			if x[i*C] > 0 {
				a = -a
			}
			/* Apply soft clipping.
			   C: x[i*C] = x[i*C] + a*x[i*C]*x[i*C] — left-to-right
			   as x + ((a*x)*x), three muls, not x + a*(x*x). */
			for i = start; i < end; i++ {
				x[i*C] = fma_add(x[i*C], mul_f32(a, x[i*C]), x[i*C])
			}

			if special != 0 && peak_pos >= 2 {
				/* Add a linear ramp from the first sample to the signal peak. */
				var delta float32
				offset := sub_f32(x0, x[0])
				delta = offset / float32(peak_pos)
				for i = curr; i < peak_pos; i++ {
					offset = sub_f32(offset, delta)
					x[i*C] = add_f32(x[i*C], offset)
					x[i*C] = MAX16(-1.0, MIN16(1.0, x[i*C]))
				}
			}
			curr = end
			if curr == N {
				break
			}
		}
		declip_mem[c] = a
	}
}

// opus_pcm_soft_clip — public wrapper. C: opus.c:163.
func opus_pcm_soft_clip(_x []float32, N int, C int, declip_mem []float32) {
	opus_pcm_soft_clip_impl(_x, N, C, declip_mem, 0)
}

// encode_size — write 1 or 2 bytes encoding `size`. C: opus.c:170.
func encode_size(size int, data []byte) int {
	if size < 252 {
		data[0] = byte(size)
		return 1
	}
	data[0] = byte(252 + (size & 0x3))
	data[1] = byte((size - int(data[0])) >> 2)
	return 2
}

// parse_size — read 1 or 2 bytes encoding a size into *size. C: opus.c:183.
func parse_size(data []byte, len_ opus_int32, size *opus_int16) int {
	if len_ < 1 {
		*size = -1
		return -1
	} else if data[0] < 252 {
		*size = opus_int16(data[0])
		return 1
	} else if len_ < 2 {
		*size = -1
		return -1
	}
	*size = opus_int16(4*int(data[1]) + int(data[0]))
	return 2
}

// opus_packet_get_samples_per_frame — C: opus.c:203.
func opus_packet_get_samples_per_frame(data []byte, Fs opus_int32) int {
	var audiosize int
	if data[0]&0x80 != 0 {
		audiosize = int((data[0] >> 3) & 0x3)
		audiosize = int(Fs<<audiosize) / 400
	} else if (data[0] & 0x60) == 0x60 {
		if data[0]&0x08 != 0 {
			audiosize = int(Fs) / 50
		} else {
			audiosize = int(Fs) / 100
		}
	} else {
		audiosize = int((data[0] >> 3) & 0x3)
		if audiosize == 3 {
			audiosize = int(Fs) * 60 / 1000
		} else {
			audiosize = int(Fs<<audiosize) / 100
		}
	}
	return audiosize
}

// opus_packet_get_bandwidth — C: opus_decoder.c:1250. Lives in opus.c-port
// for convenience since it is part of the public TOC API.
func opus_packet_get_bandwidth(data []byte) int {
	var bandwidth int
	if data[0]&0x80 != 0 {
		bandwidth = OPUS_BANDWIDTH_MEDIUMBAND + int((data[0]>>5)&0x3)
		if bandwidth == OPUS_BANDWIDTH_MEDIUMBAND {
			bandwidth = OPUS_BANDWIDTH_NARROWBAND
		}
	} else if (data[0] & 0x60) == 0x60 {
		if data[0]&0x10 != 0 {
			bandwidth = OPUS_BANDWIDTH_FULLBAND
		} else {
			bandwidth = OPUS_BANDWIDTH_SUPERWIDEBAND
		}
	} else {
		bandwidth = OPUS_BANDWIDTH_NARROWBAND + int((data[0]>>5)&0x3)
	}
	return bandwidth
}

// opus_packet_get_nb_channels — C: opus_decoder.c:1268.
func opus_packet_get_nb_channels(data []byte) int {
	if data[0]&0x4 != 0 {
		return 2
	}
	return 1
}

// opus_packet_get_nb_frames — C: opus_decoder.c:1273.
func opus_packet_get_nb_frames(packet []byte, len_ opus_int32) int {
	var count int
	if len_ < 1 {
		return OPUS_BAD_ARG
	}
	count = int(packet[0] & 0x3)
	if count == 0 {
		return 1
	} else if count != 3 {
		return 2
	} else if len_ < 2 {
		return OPUS_INVALID_PACKET
	}
	return int(packet[1] & 0x3F)
}

// opus_packet_get_nb_samples — C: opus_decoder.c:1289.
func opus_packet_get_nb_samples(packet []byte, len_ opus_int32, Fs opus_int32) int {
	var samples int
	count := opus_packet_get_nb_frames(packet, len_)

	if count < 0 {
		return count
	}

	samples = count * opus_packet_get_samples_per_frame(packet, Fs)
	/* Can't have more than 120 ms */
	if opus_int32(samples)*25 > Fs*3 {
		return OPUS_INVALID_PACKET
	}
	return samples
}

// opus_packet_parse_impl — full TOC + frame length parser.
// C: opus.c:224.
//
// The C API exposes `frames` as an array of `const unsigned char *`
// pointers. In Go we return per-frame offsets into the input `data`
// slice by populating `frames` with slices sharing the backing array.
// Callers that do not need frames may pass `nil`.
//
// `padding` is returned as a slice sharing the backing array, and
// `padding_len` carries its length.
func opus_packet_parse_impl(data []byte, len_ opus_int32,
	self_delimited int, out_toc *byte,
	frames [][]byte, size []opus_int16,
	payload_offset *int, packet_offset *opus_int32,
	padding *[]byte, padding_len *opus_int32) int {

	var i, bytes_ int
	var count int
	var cbr int
	var ch, toc byte
	var framesize int
	var last_size opus_int32
	var pad opus_int32 = 0
	data0 := data
	dpos := 0 /* index into data0 matching C `data` pointer */

	/* Make sure we return NULL/0 on error. */
	if padding != nil {
		*padding = nil
		*padding_len = 0
	}

	if size == nil || len_ < 0 {
		return OPUS_BAD_ARG
	}
	if len_ == 0 {
		return OPUS_INVALID_PACKET
	}

	framesize = opus_packet_get_samples_per_frame(data[dpos:], 48000)

	cbr = 0
	toc = data[dpos]
	dpos++
	len_--
	last_size = len_
	switch toc & 0x3 {
	/* One frame */
	case 0:
		count = 1
	/* Two CBR frames */
	case 1:
		count = 2
		cbr = 1
		if self_delimited == 0 {
			if len_&0x1 != 0 {
				return OPUS_INVALID_PACKET
			}
			last_size = len_ / 2
			/* If last_size doesn't fit in size[0], we'll catch it later */
			size[0] = opus_int16(last_size)
		}
	/* Two VBR frames */
	case 2:
		count = 2
		bytes_ = parse_size(data[dpos:], len_, &size[0])
		len_ -= opus_int32(bytes_)
		if size[0] < 0 || opus_int32(size[0]) > len_ {
			return OPUS_INVALID_PACKET
		}
		dpos += bytes_
		last_size = len_ - opus_int32(size[0])
	/* Multiple CBR/VBR frames (from 0 to 120 ms) */
	default: /*case 3:*/
		if len_ < 1 {
			return OPUS_INVALID_PACKET
		}
		/* Number of frames encoded in bits 0 to 5 */
		ch = data[dpos]
		dpos++
		count = int(ch & 0x3F)
		if count <= 0 || opus_int32(framesize)*opus_int32(count) > 5760 {
			return OPUS_INVALID_PACKET
		}
		len_--
		/* Padding flag is bit 6 */
		if ch&0x40 != 0 {
			var p int
			for {
				var tmp int
				if len_ <= 0 {
					return OPUS_INVALID_PACKET
				}
				p = int(data[dpos])
				dpos++
				len_--
				if p == 255 {
					tmp = 254
				} else {
					tmp = p
				}
				len_ -= opus_int32(tmp)
				pad += opus_int32(tmp)
				if p != 255 {
					break
				}
			}
		}
		if len_ < 0 {
			return OPUS_INVALID_PACKET
		}
		/* VBR flag is bit 7 */
		if ch&0x80 != 0 {
			cbr = 0
		} else {
			cbr = 1
		}
		if cbr == 0 {
			/* VBR case */
			last_size = len_
			for i = 0; i < count-1; i++ {
				bytes_ = parse_size(data[dpos:], len_, &size[i])
				len_ -= opus_int32(bytes_)
				if size[i] < 0 || opus_int32(size[i]) > len_ {
					return OPUS_INVALID_PACKET
				}
				dpos += bytes_
				last_size -= opus_int32(bytes_) + opus_int32(size[i])
			}
			if last_size < 0 {
				return OPUS_INVALID_PACKET
			}
		} else if self_delimited == 0 {
			/* CBR case */
			last_size = len_ / opus_int32(count)
			if last_size*opus_int32(count) != len_ {
				return OPUS_INVALID_PACKET
			}
			for i = 0; i < count-1; i++ {
				size[i] = opus_int16(last_size)
			}
		}
	}
	/* Self-delimited framing has an extra size for the last frame. */
	if self_delimited != 0 {
		bytes_ = parse_size(data[dpos:], len_, &size[count-1])
		len_ -= opus_int32(bytes_)
		if size[count-1] < 0 || opus_int32(size[count-1]) > len_ {
			return OPUS_INVALID_PACKET
		}
		dpos += bytes_
		/* For CBR packets, apply the size to all the frames. */
		if cbr != 0 {
			if opus_int32(size[count-1])*opus_int32(count) > len_ {
				return OPUS_INVALID_PACKET
			}
			for i = 0; i < count-1; i++ {
				size[i] = size[count-1]
			}
		} else if opus_int32(bytes_)+opus_int32(size[count-1]) > last_size {
			return OPUS_INVALID_PACKET
		}
	} else {
		/* Because it's not encoded explicitly, it's possible the size of the
		   last packet (or all the packets, for the CBR case) is larger than
		   1275. Reject them here.*/
		if last_size > 1275 {
			return OPUS_INVALID_PACKET
		}
		size[count-1] = opus_int16(last_size)
	}

	if payload_offset != nil {
		*payload_offset = dpos
	}

	for i = 0; i < count; i++ {
		if frames != nil {
			frames[i] = data0[dpos:]
		}
		dpos += int(size[i])
	}

	if padding != nil {
		*padding = data0[dpos:]
		*padding_len = pad
	}
	if packet_offset != nil {
		*packet_offset = pad + opus_int32(dpos)
	}

	if out_toc != nil {
		*out_toc = toc
	}

	return count
}

// opus_packet_parse — C: opus.c:392.
func opus_packet_parse(data []byte, len_ opus_int32,
	out_toc *byte, frames [][]byte,
	size []opus_int16, payload_offset *int) int {
	return opus_packet_parse_impl(data, len_, 0, out_toc,
		frames, size, payload_offset, nil, nil, nil)
}
