package nativemp3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHdrGetMySampleRate checks the combined sample-rate index that folds in
// the MPEG version bits (HDR_GET_MY_SAMPLE_RATE).
func TestHdrGetMySampleRate(t *testing.T) {
	tests := []struct {
		name string
		hdr  []byte
		want int
	}{
		// MPEG-1 (h[1]&0x18 == 0x18): both version bits set => +2*3=6.
		{"mpeg1 srate0", []byte{0xFF, 0xFB, 0x90, 0x04}, 0 + 6},
		// MPEG-1, sample-rate index 2 (h[2] bits 2..3 = 10).
		{"mpeg1 srate2", []byte{0xFF, 0xFB, 0x98, 0x04}, 2 + 6},
		// MPEG-2 (h[1]&0x18 == 0x10): only the NOT_MPEG25 bit set => +1*3=3.
		{"mpeg2 srate0", []byte{0xFF, 0xF3, 0x90, 0x04}, 0 + 3},
		// MPEG-2.5 (h[1]&0x18 == 0x00): neither bit set => +0.
		{"mpeg25 srate0", []byte{0xFF, 0xE3, 0x90, 0x04}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hdrGetMySampleRate(tc.hdr))
		})
	}
}

// TestL3ReadSideInfoZeroedMPEG1Stereo parses an all-zero side-info payload for
// an MPEG-1 stereo frame. With every field zero the parser walks the
// windowed-flag=0 branch for all four granules and consumes exactly 256 bits
// (32 bytes). With all-zero side-info bytes the parsed main_data_begin (the
// leading 9-bit field) is 0, and the parse succeeds.
func TestL3ReadSideInfoZeroedMPEG1Stereo(t *testing.T) {
	hdr := []byte{0xFF, 0xFB, 0x90, 0x04} // MPEG-1 Layer III, stereo
	require.False(t, hdrIsMono(hdr))
	require.NotZero(t, hdrTestMPEG1(hdr))

	// 32 bytes of zeros is enough side info (256 bits) plus a non-negative
	// limit so the final reservoir check passes.
	buf := make([]byte, 64)
	var bs BitStream
	BsInit(&bs, buf, len(buf))

	gr := make([]L3GrInfo, 4)
	mainData, ok := L3ReadSideInfo(&bs, gr, hdr)
	require.True(t, ok)
	assert.Equal(t, 256, bs.Pos)
	assert.Equal(t, 0, mainData) // all-zero bytes => main_data_begin field = 0

	// hdr 0xFF,0xFB,... is MPEG-1 at sample-rate index 0, so
	// HDR_GET_MY_SAMPLE_RATE = 6 and the table row index sr_idx = 6-1 = 5.
	require.Equal(t, 6, hdrGetMySampleRate(hdr))

	// With a windowed flag of 0, region_count[2] is forced to 255 and the
	// long-block scale-factor table row for sr_idx=5 is selected.
	for i := range gr {
		assert.EqualValues(t, 255, gr[i].RegionCount[2])
		assert.EqualValues(t, 22, gr[i].NLongSfb)
		assert.Equal(t, gScfLong[5][:], gr[i].Sfbtab)
	}
}

// TestL3ReadSideInfoRejectsBigValues exercises the big_values > 288 guard,
// which is minimp3's "return -1" malformed-side-info path.
func TestL3ReadSideInfoRejectsBigValues(t *testing.T) {
	hdr := []byte{0xFF, 0xFB, 0x90, 0x04} // MPEG-1 stereo

	// Lay out the bits so the first granule's big_values field is 289 (> 288).
	// Header side-info prefix for MPEG-1 stereo: main_data_begin(9) +
	// scfsi(7+4=11) = 20 bits, then part_23_length(12), then big_values(9).
	var w bitWriter
	w.put(0, 9)   // main_data_begin
	w.put(0, 11)  // scfsi
	w.put(0, 12)  // part_23_length
	w.put(289, 9) // big_values = 289 (> 288)
	buf := w.bytes()
	if len(buf) < 8 {
		buf = append(buf, make([]byte, 8-len(buf))...)
	}

	var bs BitStream
	BsInit(&bs, buf, len(buf))
	gr := make([]L3GrInfo, 4)
	_, ok := L3ReadSideInfo(&bs, gr, hdr)
	assert.False(t, ok)
}

// bitWriter is a tiny MSB-first bit packer used only by the side-info tests to
// fabricate bitstreams; it is not part of the port.
type bitWriter struct {
	buf  []byte
	nbit int
}

func (w *bitWriter) put(v uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		bit := (v >> uint(i)) & 1
		if w.nbit%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		if bit != 0 {
			w.buf[w.nbit/8] |= byte(1 << uint(7-(w.nbit%8)))
		}
		w.nbit++
	}
}

func (w *bitWriter) bytes() []byte { return w.buf }
