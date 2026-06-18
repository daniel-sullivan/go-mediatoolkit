//go:build linux

// Annex-B NAL parsing and an RBSP bit reader (Exp-Golomb) for the VAAPI
// decoder. The decoder consumes the elementary-stream form the encoder
// produces — start-code-prefixed (Annex-B) NAL units, with the parameter
// sets carried inline on each keyframe — splits it into NALs, strips
// emulation-prevention bytes to read the RBSP of an SPS/PPS, and reads the
// codec syntax with Exp-Golomb / fixed-length fields to fill the VA
// parameter buffers. This mirrors the spirit of the VideoToolbox NAL
// splitter but adds the SPS/PPS parsing VA-API needs (VideoToolbox parsed
// the parameter sets in the OS framework; here we must do it ourselves).

package hwaccel

// splitAnnexBNALs splits an Annex-B byte stream into raw NAL units (start
// codes stripped). It recognises both 3-byte (00 00 01) and 4-byte
// (00 00 00 01) start codes. Empty units are dropped. The returned slices
// alias b.
func splitAnnexBNALs(b []byte) [][]byte {
	var nals [][]byte
	n := len(b)
	i := 0
	start := -1
	for i+3 <= n {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			start = i + 3
			i = start
			break
		}
		i++
	}
	if start < 0 {
		return nil
	}
	for i+3 <= n {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			end := i
			// A preceding zero byte belongs to a 4-byte start code.
			if end > start && b[end-1] == 0 {
				end--
			}
			if end > start {
				nals = append(nals, b[start:end])
			}
			start = i + 3
			i = start
			continue
		}
		i++
	}
	if start < n {
		tail := b[start:n]
		for len(tail) > 0 && tail[len(tail)-1] == 0 {
			tail = tail[:len(tail)-1]
		}
		if len(tail) > 0 {
			nals = append(nals, tail)
		}
	}
	return nals
}

// ebspToRBSP strips H.264/H.265 emulation-prevention bytes: any 0x03 in a
// 0x00 0x00 0x03 sequence is removed, yielding the raw byte sequence
// payload (RBSP) for bit-accurate syntax parsing.
func ebspToRBSP(ebsp []byte) []byte {
	out := make([]byte, 0, len(ebsp))
	zeros := 0
	for i := 0; i < len(ebsp); i++ {
		b := ebsp[i]
		if zeros >= 2 && b == 0x03 && i+1 < len(ebsp) && ebsp[i+1] <= 0x03 {
			zeros = 0
			continue // drop the emulation-prevention byte
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

// bitReader reads MSB-first bits over an RBSP byte slice, with the
// Exp-Golomb helpers H.264/H.265 slice and parameter-set parsing needs.
type bitReader struct {
	data []byte
	pos  int // bit position
}

func newBitReader(data []byte) *bitReader { return &bitReader{data: data} }

// bitsLeft reports how many bits remain unread.
func (r *bitReader) bitsLeft() int { return len(r.data)*8 - r.pos }

// u1 reads a single bit.
func (r *bitReader) u1() uint32 {
	if r.pos >= len(r.data)*8 {
		r.pos++
		return 0
	}
	byteIdx := r.pos >> 3
	bitIdx := 7 - (r.pos & 7)
	r.pos++
	return uint32((r.data[byteIdx] >> bitIdx) & 1)
}

// u reads n bits as an unsigned integer (n <= 32).
func (r *bitReader) u(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v = (v << 1) | r.u1()
	}
	return v
}

// ue reads an unsigned Exp-Golomb code.
func (r *bitReader) ue() uint32 {
	zeros := 0
	for r.bitsLeft() > 0 && r.u1() == 0 {
		zeros++
		if zeros > 31 {
			break
		}
	}
	if zeros == 0 {
		return 0
	}
	return (1 << zeros) - 1 + r.u(zeros)
}

// se reads a signed Exp-Golomb code.
func (r *bitReader) se() int32 {
	k := r.ue()
	if k == 0 {
		return 0
	}
	if k&1 == 1 {
		return int32((k + 1) / 2)
	}
	return -int32(k / 2)
}
