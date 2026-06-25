//go:build cgo

package frameheader

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// makeFrameHeader synthesises a complete FLAC frame header from
// explicit field values (matching RFC 9639 §11.1) and appends the
// CRC-8. It returns the raw bytes (sync code + header + CRC), ready
// to feed into both parsers.
//
// The function does NOT generate subframe payloads — only the
// header. This is enough to exercise the parser exhaustively across
// every encoding choice (blocksize index 0..15, sample-rate index
// 0..14, channel-assignment / bits-per-sample / variable-blocking
// flag).
type frameSpec struct {
	variableBlocking bool
	blocksizeIdx     uint8
	sampleRateIdx    uint8
	channelIdx       uint8 // 0..7 for independent 1..8ch; 8..10 for L/R/M-side
	bitsIdx          uint8
	frameOrSampleNum uint64

	// Used when blocksize / sample-rate index requires follow-on
	// bytes.
	blocksizeFollow  uint32
	sampleRateFollow uint32
}

func writeUTF8Uint32(buf []byte, v uint32) []byte {
	switch {
	case v < 1<<7:
		return append(buf, byte(v))
	case v < 1<<11:
		return append(buf, 0xC0|byte(v>>6), 0x80|byte(v&0x3F))
	case v < 1<<16:
		return append(buf, 0xE0|byte(v>>12), 0x80|byte((v>>6)&0x3F), 0x80|byte(v&0x3F))
	case v < 1<<21:
		return append(buf, 0xF0|byte(v>>18), 0x80|byte((v>>12)&0x3F), 0x80|byte((v>>6)&0x3F), 0x80|byte(v&0x3F))
	}
	return append(buf,
		0xF8|byte(v>>24),
		0x80|byte((v>>18)&0x3F),
		0x80|byte((v>>12)&0x3F),
		0x80|byte((v>>6)&0x3F),
		0x80|byte(v&0x3F))
}

func writeUTF8Uint64(buf []byte, v uint64) []byte {
	if v < 1<<31 {
		return writeUTF8Uint32(buf, uint32(v))
	}
	if v < 1<<36 {
		return append(buf,
			0xFC|byte(v>>30),
			0x80|byte((v>>24)&0x3F),
			0x80|byte((v>>18)&0x3F),
			0x80|byte((v>>12)&0x3F),
			0x80|byte((v>>6)&0x3F),
			0x80|byte(v&0x3F))
	}
	return append(buf, 0xFE,
		0x80|byte((v>>30)&0x3F),
		0x80|byte((v>>24)&0x3F),
		0x80|byte((v>>18)&0x3F),
		0x80|byte((v>>12)&0x3F),
		0x80|byte((v>>6)&0x3F),
		0x80|byte(v&0x3F))
}

func buildFrameHeader(s frameSpec) []byte {
	// Byte 0 = 0xFF (sync hi).
	// Byte 1 = 0xF8 (sync lo, fixed) or 0xF9 (variable).
	hdr0 := byte(0xFF)
	hdr1 := byte(0xF8)
	if s.variableBlocking {
		hdr1 = 0xF9
	}
	out := []byte{hdr0, hdr1}

	// Byte 2: high nibble = blocksizeIdx, low nibble = sampleRateIdx.
	out = append(out, s.blocksizeIdx<<4|s.sampleRateIdx)

	// Byte 3: high nibble = channelIdx, then bits 4..6 = bitsIdx,
	// bit 7 = reserved (0).
	out = append(out, s.channelIdx<<4|(s.bitsIdx<<1))

	// Frame or sample number, UTF-8 style.
	if s.variableBlocking {
		out = writeUTF8Uint64(out, s.frameOrSampleNum)
	} else {
		out = writeUTF8Uint32(out, uint32(s.frameOrSampleNum))
	}

	// Blocksize follow-on.
	switch s.blocksizeIdx {
	case 6:
		out = append(out, byte(s.blocksizeFollow-1))
	case 7:
		v := s.blocksizeFollow - 1
		out = append(out, byte(v>>8), byte(v))
	}

	// Sample-rate follow-on.
	switch s.sampleRateIdx {
	case 12:
		out = append(out, byte(s.sampleRateFollow))
	case 13:
		out = append(out, byte(s.sampleRateFollow>>8), byte(s.sampleRateFollow))
	case 14:
		out = append(out, byte(s.sampleRateFollow>>8), byte(s.sampleRateFollow))
	}

	out = append(out, nativeflac.CRC8(out))
	return out
}

// goReadFrameHeader runs the Go port over body using a slice-backed
// read callback so the BitReader receives the same bytes.
func goReadFrameHeader(body []byte, hdr0, hdr1 byte, hsi bool, siSR, siBPS, siMinBS, siMaxBS, fixedBS uint32) FHResult {
	br := nativeflac.NewBitReader()
	off := 0
	br.Init(func(buf []byte) (uint, bool) {
		avail := len(body) - off
		if avail <= 0 {
			return 0, false
		}
		n := len(buf)
		if n > avail {
			n = avail
		}
		copy(buf, body[off:off+n])
		off += n
		return uint(n), true
	})
	in := nativeflac.ReadFrameHeaderInput{
		HeaderWarmup:            [2]byte{hdr0, hdr1},
		HasStreamInfo:           hsi,
		StreamInfoSampleRate:    siSR,
		StreamInfoBitsPerSample: siBPS,
		StreamInfoMinBlockSize:  siMinBS,
		StreamInfoMaxBlockSize:  siMaxBS,
		FixedBlockSize:          fixedBS,
	}
	h, nfb, status := nativeflac.ReadFrameHeader(br, in)
	res := FHResult{
		Blocksize:          h.Blocksize,
		SampleRate:         h.SampleRate,
		Channels:           h.Channels,
		ChannelAssignment:  uint8(h.ChannelAssignment),
		BitsPerSample:      h.BitsPerSample,
		NumberType:         uint8(h.NumberType),
		Number:             h.Number,
		CRC:                h.CRC,
		NextFixedBlockSize: nfb,
	}
	switch status {
	case nativeflac.FrameHeaderOK:
		res.Status = 0
	case nativeflac.FrameHeaderReadError:
		res.Status = 1
	case nativeflac.FrameHeaderBadHeader:
		res.Status = 2
	case nativeflac.FrameHeaderUnparseable:
		res.Status = 3
	}
	return res
}

func compare(t *testing.T, name string, raw []byte, hsi bool, siSR, siBPS, siMinBS, siMaxBS, fixedBS uint32) {
	t.Helper()
	require.GreaterOrEqual(t, len(raw), 2)
	body := raw[2:]
	c := CgoReadFrameHeader(body, raw[0], raw[1], hsi, siSR, siBPS, siMinBS, siMaxBS, fixedBS)
	g := goReadFrameHeader(body, raw[0], raw[1], hsi, siSR, siBPS, siMinBS, siMaxBS, fixedBS)
	require.Equal(t, c, g, "%s", name)
}

// ── Encoded sample-rate codes 1–11 ──────────────────────────────────

func TestParityAllSampleRateIndices(t *testing.T) {
	for srIdx := uint8(1); srIdx <= 11; srIdx++ {
		raw := buildFrameHeader(frameSpec{
			blocksizeIdx: 5, sampleRateIdx: srIdx,
			channelIdx: 1, bitsIdx: 4, frameOrSampleNum: 7,
		})
		compare(t, "sr_idx", raw, false, 0, 0, 0, 0, 0)
	}
}

// ── Sample rate from STREAMINFO (idx 0) ─────────────────────────────

func TestParitySampleRateFromStreamInfo(t *testing.T) {
	raw := buildFrameHeader(frameSpec{
		blocksizeIdx: 5, sampleRateIdx: 0,
		channelIdx: 1, bitsIdx: 4, frameOrSampleNum: 0,
	})
	compare(t, "with_si", raw, true, 88100, 16, 4096, 4096, 0)
	// Without StreamInfo → unparseable.
	compare(t, "no_si", raw, false, 0, 0, 0, 0, 0)
}

// ── Sample-rate follow-on codes 12, 13, 14 ──────────────────────────

func TestParitySampleRateFollowOn(t *testing.T) {
	for _, c := range []struct {
		name string
		idx  uint8
		val  uint32
	}{
		{"sr12_64khz", 12, 64},    // x*1000 = 64000
		{"sr13_192k", 13, 192000}, // x = 192000
		{"sr14_882", 14, 8820},    // x*10 = 88200
	} {
		raw := buildFrameHeader(frameSpec{
			blocksizeIdx: 5, sampleRateIdx: c.idx,
			channelIdx: 1, bitsIdx: 4, frameOrSampleNum: 1,
			sampleRateFollow: c.val,
		})
		compare(t, c.name, raw, false, 0, 0, 0, 0, 0)
	}
}

// ── Blocksize indices ───────────────────────────────────────────────

func TestParityAllBlocksizeIndices(t *testing.T) {
	// Indices 1, 2..5, 8..15 are explicit. Index 6 + 7 require
	// follow-on bytes.
	for _, bs := range []uint8{1, 2, 3, 4, 5, 8, 9, 10, 11, 12, 13, 14, 15} {
		raw := buildFrameHeader(frameSpec{
			blocksizeIdx: bs, sampleRateIdx: 9,
			channelIdx: 1, bitsIdx: 4, frameOrSampleNum: 0,
		})
		compare(t, "bs_idx", raw, false, 0, 0, 0, 0, 0)
	}
}

func TestParityBlocksizeFollowOn(t *testing.T) {
	for _, c := range []struct {
		name string
		idx  uint8
		val  uint32
	}{
		{"bs6_short", 6, 100},
		{"bs6_max8", 6, 256},
		{"bs7_2049", 7, 2049},
		{"bs7_max16", 7, 65535},
	} {
		raw := buildFrameHeader(frameSpec{
			blocksizeIdx: c.idx, sampleRateIdx: 9,
			channelIdx: 1, bitsIdx: 4, frameOrSampleNum: 0,
			blocksizeFollow: c.val,
		})
		compare(t, c.name, raw, false, 0, 0, 0, 0, 0)
	}
}

// ── Channel assignment ──────────────────────────────────────────────

func TestParityChannelAssignments(t *testing.T) {
	// 0..7 = independent 1..8 channels; 8 = L/side; 9 = R/side; 10 = M/side.
	for ch := uint8(0); ch <= 10; ch++ {
		raw := buildFrameHeader(frameSpec{
			blocksizeIdx: 5, sampleRateIdx: 9,
			channelIdx: ch, bitsIdx: 4, frameOrSampleNum: 0,
		})
		compare(t, "ch", raw, false, 0, 0, 0, 0, 0)
	}
}

// 11..15 are reserved → unparseable; both parsers should agree.
func TestParityReservedChannelAssignments(t *testing.T) {
	for ch := uint8(11); ch <= 15; ch++ {
		raw := buildFrameHeader(frameSpec{
			blocksizeIdx: 5, sampleRateIdx: 9,
			channelIdx: ch, bitsIdx: 4, frameOrSampleNum: 0,
		})
		compare(t, "rsvd_ch", raw, false, 0, 0, 0, 0, 0)
	}
}

// ── Bits-per-sample ─────────────────────────────────────────────────

func TestParityBitsPerSample(t *testing.T) {
	for _, b := range []uint8{0, 1, 2, 4, 5, 6, 7} {
		raw := buildFrameHeader(frameSpec{
			blocksizeIdx: 5, sampleRateIdx: 9,
			channelIdx: 1, bitsIdx: b, frameOrSampleNum: 0,
		})
		// idx 0 needs StreamInfo → test both with and without.
		compare(t, "bps_no_si", raw, false, 0, 0, 0, 0, 0)
		compare(t, "bps_si", raw, true, 44100, 16, 4096, 4096, 0)
	}
	// idx 3 is reserved → unparseable.
	raw := buildFrameHeader(frameSpec{
		blocksizeIdx: 5, sampleRateIdx: 9,
		channelIdx: 1, bitsIdx: 3, frameOrSampleNum: 0,
	})
	compare(t, "bps_rsvd", raw, true, 44100, 16, 4096, 4096, 0)
}

// ── Variable vs fixed blocking strategy ─────────────────────────────

func TestParityVariableBlocking(t *testing.T) {
	// Fixed blocking, frame number 0..big.
	for _, n := range []uint64{0, 1, 1234, 0xFFFF, 0x7FFFFFFF} {
		raw := buildFrameHeader(frameSpec{
			blocksizeIdx: 5, sampleRateIdx: 9,
			channelIdx: 1, bitsIdx: 4, frameOrSampleNum: n,
		})
		compare(t, "fixed", raw, true, 44100, 16, 4096, 4096, 0)
	}
	// Variable blocking, sample number 0..big.
	for _, n := range []uint64{0, 1, 1 << 20, 1 << 35} {
		raw := buildFrameHeader(frameSpec{
			variableBlocking: true,
			blocksizeIdx:     5, sampleRateIdx: 9,
			channelIdx: 1, bitsIdx: 4, frameOrSampleNum: n,
		})
		compare(t, "variable", raw, true, 44100, 16, 4096, 4096, 0)
	}
}

// ── Frame-number → sample-number conversion ─────────────────────────

func TestParityFrameNumberToSampleNumber(t *testing.T) {
	// Fixed blocking with various FixedBlockSize hints.
	for _, fixed := range []uint32{4096, 1024, 16384} {
		raw := buildFrameHeader(frameSpec{
			blocksizeIdx: 5, sampleRateIdx: 9,
			channelIdx: 1, bitsIdx: 4, frameOrSampleNum: 7,
		})
		compare(t, "fixed_bs", raw, false, 0, 0, 0, 0, fixed)
	}
}

// ── Truncated input ─────────────────────────────────────────────────

func TestParityTruncatedHeader(t *testing.T) {
	raw := buildFrameHeader(frameSpec{
		blocksizeIdx: 5, sampleRateIdx: 9,
		channelIdx: 1, bitsIdx: 4, frameOrSampleNum: 0,
	})
	// Cut off various lengths after the warmup.
	for cutAt := 3; cutAt < len(raw); cutAt++ {
		compare(t, "trunc", raw[:cutAt], false, 0, 0, 0, 0, 0)
	}
}

// ── Bad CRC-8 ───────────────────────────────────────────────────────

func TestParityBadCRC8(t *testing.T) {
	raw := buildFrameHeader(frameSpec{
		blocksizeIdx: 5, sampleRateIdx: 9,
		channelIdx: 1, bitsIdx: 4, frameOrSampleNum: 0,
	})
	// Corrupt the CRC byte.
	raw[len(raw)-1] ^= 0xFF
	compare(t, "bad_crc", raw, false, 0, 0, 0, 0, 0)
}
