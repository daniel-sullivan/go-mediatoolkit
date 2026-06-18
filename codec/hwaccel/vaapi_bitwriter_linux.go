//go:build linux

// An MSB-first RBSP bit writer with Exp-Golomb helpers and Annex-B / EBSP
// emulation-prevention packaging, used by the VAAPI encoder to author the
// SPS/PPS/VPS parameter-set NAL units it feeds to the driver as packed
// headers. The driver encodes the slice data itself; the parameter sets
// are ours to write, so the encoder constructs minimal but spec-conformant
// RBSPs here (mirroring the role the OS framework played for VideoToolbox,
// but with the RBSP authored explicitly).

package hwaccel

// bitWriter accumulates bits MSB-first into a byte slice.
type bitWriter struct {
	buf  []byte
	cur  byte
	ncur uint // bits currently filled in cur (0..7)
}

func newBitWriter() *bitWriter { return &bitWriter{} }

// putBit writes a single bit.
func (w *bitWriter) putBit(b uint32) {
	w.cur |= byte(b&1) << (7 - w.ncur)
	w.ncur++
	if w.ncur == 8 {
		w.buf = append(w.buf, w.cur)
		w.cur = 0
		w.ncur = 0
	}
}

// putBits writes the low n bits of v, MSB-first.
func (w *bitWriter) putBits(v uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		w.putBit((v >> uint(i)) & 1)
	}
}

// ue writes an unsigned Exp-Golomb code.
func (w *bitWriter) ue(v uint32) {
	v1 := v + 1
	n := 0
	for t := v1; t > 1; t >>= 1 {
		n++
	}
	for i := 0; i < n; i++ {
		w.putBit(0)
	}
	w.putBits(v1, n+1)
}

// se writes a signed Exp-Golomb code.
func (w *bitWriter) se(v int32) {
	if v == 0 {
		w.ue(0)
		return
	}
	if v > 0 {
		w.ue(uint32(2*v - 1))
	} else {
		w.ue(uint32(-2 * v))
	}
}

// rbspTrailing appends the rbsp_trailing_bits(): a stop-one then zero pad
// to a byte boundary. Returns the finished RBSP bytes.
func (w *bitWriter) rbspTrailing() []byte {
	w.putBit(1)
	for w.ncur != 0 {
		w.putBit(0)
	}
	out := make([]byte, len(w.buf))
	copy(out, w.buf)
	return out
}

// bitLen returns the number of bits written so far (before trailing).
func (w *bitWriter) bitLen() int { return len(w.buf)*8 + int(w.ncur) }

// byteAlign appends byte_alignment(): an alignment_bit_equal_to_one followed
// by alignment_bit_equal_to_zero bits up to a byte boundary. Returns the
// finished bytes. Used by the HEVC slice_segment_header() (which ends in
// byte_alignment rather than rbsp_trailing_bits).
func (w *bitWriter) byteAlign() []byte {
	w.putBit(1)
	for w.ncur != 0 {
		w.putBit(0)
	}
	out := make([]byte, len(w.buf))
	copy(out, w.buf)
	return out
}

// rbspToEBSP inserts H.264/H.265 emulation-prevention bytes: a 0x03 is
// inserted after any 00 00 sequence that would otherwise be followed by a
// byte <= 0x03. This is the inverse of ebspToRBSP.
func rbspToEBSP(rbsp []byte) []byte {
	out := make([]byte, 0, len(rbsp)+len(rbsp)/64+1)
	zeros := 0
	for _, b := range rbsp {
		if zeros >= 2 && b <= 0x03 {
			out = append(out, 0x03)
			zeros = 0
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
