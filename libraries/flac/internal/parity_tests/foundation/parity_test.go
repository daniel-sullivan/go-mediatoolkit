//go:build cgo

package foundation

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// Each parity test below pins the Go port against the cgo libFLAC
// reference for a specific helper, exercised across edge cases plus a
// large randomised sweep. A failure means the Go translation has
// diverged from the C — usually the place to start debugging is the
// commented-in reference comment on the Go function.

// ── CRC tests ───────────────────────────────────────────────────────

func TestParityCRC8(t *testing.T) {
	cases := [][]byte{
		nil, {}, {0}, {0xFF}, {0, 1, 2, 3, 4, 5, 6, 7},
		[]byte("123456789"),
	}
	for _, c := range cases {
		assert.Equal(t, cgoCRC8(c), nativeflac.CRC8(c), "CRC8 over %d bytes", len(c))
	}
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 100; i++ {
		n := r.IntN(1024)
		buf := make([]byte, n)
		for j := range buf {
			buf[j] = byte(r.Uint32())
		}
		require.Equal(t, cgoCRC8(buf), nativeflac.CRC8(buf), "CRC8 random run %d", i)
	}
}

func TestParityCRC16(t *testing.T) {
	cases := [][]byte{
		nil, {}, {0}, {0xFF}, {0, 1, 2, 3, 4, 5, 6, 7},
		[]byte("123456789"),
	}
	for _, c := range cases {
		assert.Equal(t, cgoCRC16(c), nativeflac.CRC16(c), "CRC16 over %d bytes", len(c))
	}
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 200; i++ {
		// Cover the full range of (len mod 8) so the unrolled vs.
		// scalar tail paths are both exercised.
		n := r.IntN(2048)
		buf := make([]byte, n)
		for j := range buf {
			buf[j] = byte(r.Uint32())
		}
		require.Equal(t, cgoCRC16(buf), nativeflac.CRC16(buf), "CRC16 random run %d (n=%d)", i, n)
	}
}

func TestParityCRC16UpdateWords32(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for i := 0; i < 100; i++ {
		n := r.IntN(64)
		words := make([]uint32, n)
		for j := range words {
			words[j] = r.Uint32()
		}
		seed := uint16(r.Uint32())
		require.Equal(t,
			cgoCRC16Words32(words, seed),
			nativeflac.CRC16UpdateWords32(words, seed),
			"CRC16Words32 run %d (n=%d, seed=%04x)", i, n, seed)
	}
}

func TestParityCRC16UpdateWords64(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for i := 0; i < 100; i++ {
		n := r.IntN(64)
		words := make([]uint64, n)
		for j := range words {
			words[j] = r.Uint64()
		}
		seed := uint16(r.Uint32())
		require.Equal(t,
			cgoCRC16Words64(words, seed),
			nativeflac.CRC16UpdateWords64(words, seed),
			"CRC16Words64 run %d", i)
	}
}

// ── bitmath tests ───────────────────────────────────────────────────

func TestParityILog2(t *testing.T) {
	for _, v := range []uint32{1, 2, 3, 4, 5, 7, 8, 9, 1023, 1024, 1<<16 - 1, 1 << 16, 1 << 31, ^uint32(0)} {
		assert.Equal(t, cgoILog2(v), nativeflac.ILog2(v), "ILog2(%d)", v)
	}
	r := rand.New(rand.NewPCG(9, 10))
	for i := 0; i < 256; i++ {
		v := r.Uint32() | 1 // ensure non-zero
		require.Equal(t, cgoILog2(v), nativeflac.ILog2(v), "ILog2 random %d", v)
	}
}

func TestParityILog2Wide(t *testing.T) {
	for _, v := range []uint64{1, 2, 3, 1 << 31, 1 << 32, 1 << 63, ^uint64(0)} {
		assert.Equal(t, cgoILog2Wide(v), nativeflac.ILog2Wide(v), "ILog2Wide(%d)", v)
	}
	r := rand.New(rand.NewPCG(11, 12))
	for i := 0; i < 256; i++ {
		v := r.Uint64() | 1
		require.Equal(t, cgoILog2Wide(v), nativeflac.ILog2Wide(v), "ILog2Wide random %d", v)
	}
}

func TestParitySILog2(t *testing.T) {
	for _, v := range []int64{0, 1, -1, 2, -2, 7, -7, 8, -8, 1 << 31, -(1 << 31), 1 << 60, -(1 << 60)} {
		assert.Equal(t, cgoSILog2(v), nativeflac.SILog2(v), "SILog2(%d)", v)
	}
	r := rand.New(rand.NewPCG(13, 14))
	for i := 0; i < 512; i++ {
		v := int64(r.Uint64())
		require.Equal(t, cgoSILog2(v), nativeflac.SILog2(v), "SILog2 random %d", v)
	}
}

func TestParityExtraMulbitsUnsigned(t *testing.T) {
	for _, v := range []uint32{0, 1, 2, 3, 4, 5, 7, 8, 15, 16, 17, 1023, 1024, 1025} {
		assert.Equal(t,
			cgoExtraMulbitsUnsigned(v),
			nativeflac.ExtraMulbitsUnsigned(v),
			"ExtraMulbitsUnsigned(%d)", v)
	}
	r := rand.New(rand.NewPCG(15, 16))
	for i := 0; i < 256; i++ {
		v := r.Uint32()
		require.Equal(t,
			cgoExtraMulbitsUnsigned(v),
			nativeflac.ExtraMulbitsUnsigned(v),
			"ExtraMulbitsUnsigned random %d", v)
	}
}

// ── format.c tests ──────────────────────────────────────────────────

func TestParityFormatSampleRateValid(t *testing.T) {
	for _, sr := range []uint32{0, 1, 8000, 44100, 48000, 96000, 192000, nativeflac.MaxSampleRate, nativeflac.MaxSampleRate + 1, ^uint32(0)} {
		assert.Equal(t,
			cgoFormatSampleRateIsValid(sr),
			nativeflac.FormatSampleRateIsValid(sr),
			"sample_rate_is_valid(%d)", sr)
	}
}

func TestParityFormatBlocksizeIsSubset(t *testing.T) {
	cases := []struct{ bs, sr uint32 }{
		{16, 48000}, {4608, 48000}, {4609, 48000},
		{16384, 96000}, {16385, 96000},
		{65535, 192000},
		{0, 44100},
	}
	for _, c := range cases {
		assert.Equal(t,
			cgoFormatBlocksizeIsSubset(c.bs, c.sr),
			nativeflac.FormatBlocksizeIsSubset(c.bs, c.sr),
			"blocksize_is_subset(%d, %d)", c.bs, c.sr)
	}
	r := rand.New(rand.NewPCG(21, 22))
	for i := 0; i < 200; i++ {
		bs := r.Uint32() % 70000
		sr := r.Uint32() % 200000
		require.Equal(t,
			cgoFormatBlocksizeIsSubset(bs, sr),
			nativeflac.FormatBlocksizeIsSubset(bs, sr),
			"blocksize_is_subset(%d, %d)", bs, sr)
	}
}

func TestParityFormatSampleRateIsSubset(t *testing.T) {
	cases := []uint32{0, 1, 8000, 44100, 48000, 65535, 65536, 65540, 65541, 96000, 100000, 100001, 655359, 655360, 655361, nativeflac.MaxSampleRate, nativeflac.MaxSampleRate + 1}
	for _, sr := range cases {
		assert.Equal(t,
			cgoFormatSampleRateIsSubset(sr),
			nativeflac.FormatSampleRateIsSubset(sr),
			"sample_rate_is_subset(%d)", sr)
	}
}

func TestParityMaxRicePartitionOrder(t *testing.T) {
	for _, bs := range []uint32{1, 2, 3, 4, 16, 17, 64, 96, 1024, 2048, 4096, 4097, 65535, 65536} {
		assert.Equal(t,
			cgoMaxRicePartitionOrderFromBlocksize(bs),
			nativeflac.MaxRicePartitionOrderFromBlocksize(bs),
			"max_rice_partition_order_from_blocksize(%d)", bs)
	}
	r := rand.New(rand.NewPCG(23, 24))
	for i := 0; i < 200; i++ {
		bs := r.Uint32() % 70000
		if bs == 0 {
			bs = 1
		}
		require.Equal(t,
			cgoMaxRicePartitionOrderFromBlocksize(bs),
			nativeflac.MaxRicePartitionOrderFromBlocksize(bs),
			"max_rice_partition_order_from_blocksize(%d)", bs)
	}
}

func TestParityMaxRicePartitionOrderLimited(t *testing.T) {
	r := rand.New(rand.NewPCG(25, 26))
	for i := 0; i < 200; i++ {
		bs := uint32(16 + r.IntN(65520)) // 16..65535
		limit := r.Uint32() % 16         // 0..15 (matches FLAC__MAX_RICE_PARTITION_ORDER)
		po := r.Uint32() % 33            // 0..32 (FLAC__MAX_LPC_ORDER)
		// libFLAC asserts blocksize >= predictor_order on entry; skip
		// degenerate cases (the assertion is a debug-build-only).
		if bs <= po {
			continue
		}
		require.Equal(t,
			cgoMaxRicePartitionOrderFromBlocksizeLimited(limit, bs, po),
			nativeflac.MaxRicePartitionOrderFromBlocksizeLimited(limit, bs, po),
			"max_rice_partition_order_limited(%d, %d, %d)", limit, bs, po)
	}
}

func TestParityVorbisCommentNameLegal(t *testing.T) {
	cases := []string{"", "TITLE", "Artist", "with space", "withcontrolbyte\x01", "with=equals", "with~tilde~", "with}brace", "ALL_OK_123"}
	for _, c := range cases {
		assert.Equal(t,
			cgoVCNameLegal(c),
			nativeflac.FormatVorbisCommentEntryNameIsLegal(c),
			"vc_name_is_legal(%q)", c)
	}
}

func TestParityVorbisCommentValueLegal(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		[]byte("plain ASCII"),
		[]byte("café"),           // 2-byte UTF-8
		[]byte("日本語"),            // 3-byte UTF-8
		[]byte{0xC0, 0x80},       // overlong NUL — invalid
		[]byte{0xED, 0xA0, 0x80}, // surrogate — invalid
		[]byte{0xEF, 0xBF, 0xBE}, // U+FFFE — invalid
		[]byte{0xC2},             // truncated 2-byte
		[]byte("hello\x00world"), // embedded NUL — legal: 0x00 is valid 1-byte UTF-8
	}
	for _, c := range cases {
		assert.Equal(t,
			cgoVCValueLegal(c),
			nativeflac.FormatVorbisCommentEntryValueIsLegal(c),
			"vc_value_is_legal(%q)", c)
	}
}

func TestParityVorbisCommentEntryLegal(t *testing.T) {
	cases := [][]byte{
		[]byte("TITLE=Hello"),
		[]byte("ARTIST=日本"),
		[]byte("="),              // empty name allowed (RFC §10)
		[]byte("BAD NAME=value"), // space in name — invalid
		[]byte("BAD\x01=v"),      // control byte in name — invalid
		[]byte("BAD=\xC0\x80"),   // overlong UTF-8 in value — invalid
		[]byte("noequals"),       // missing '=' — invalid
		[]byte(""),               // empty entry — invalid
	}
	for _, c := range cases {
		assert.Equal(t,
			cgoVCEntryLegal(c),
			nativeflac.FormatVorbisCommentEntryIsLegal(c),
			"vc_entry_is_legal(%q)", c)
	}
}

// ── MD5 tests ───────────────────────────────────────────────────────

func TestParityMD5OnRandomBytes(t *testing.T) {
	r := rand.New(rand.NewPCG(17, 18))
	for i := 0; i < 50; i++ {
		n := r.IntN(8192)
		buf := make([]byte, n)
		for j := range buf {
			buf[j] = byte(r.Uint32())
		}

		c := newCgoMD5()
		c.Update(buf)
		want := c.Final()
		c.Free()

		var ctx nativeflac.MD5Context
		ctx.Init()
		ctx.Update(buf)
		got := ctx.Final()

		require.Equal(t, want, got, "MD5 over %d random bytes", n)
	}
}

func TestParityMD5Accumulate(t *testing.T) {
	r := rand.New(rand.NewPCG(19, 20))
	type tc struct {
		channels       uint32
		samples        uint32
		bytesPerSample uint32
	}
	cases := []tc{
		{1, 64, 1},
		{1, 64, 2},
		{1, 64, 3},
		{1, 64, 4},
		{2, 128, 2},
		{2, 128, 3},
		{6, 256, 2},
		{8, 64, 1},
		{8, 64, 2},
		{8, 64, 3},
		{3, 257, 4}, // odd channel count + non-trivial sample count
	}
	for _, c := range cases {
		// Build per-channel int32 buffers, sign-extended to fit
		// bytesPerSample*8 bits.
		signal := make([][]int32, c.channels)
		half := int64(1) << (c.bytesPerSample*8 - 1)
		for ch := uint32(0); ch < c.channels; ch++ {
			signal[ch] = make([]int32, c.samples)
			for s := uint32(0); s < c.samples; s++ {
				v := int64(r.Uint64()) % (2 * half)
				if v >= half {
					v -= 2 * half
				}
				signal[ch][s] = int32(v)
			}
		}

		want := cgoMD5Accumulate(signal, c.channels, c.samples, c.bytesPerSample)

		var ctx nativeflac.MD5Context
		ctx.Init()
		ok := ctx.Accumulate(signal, c.channels, c.samples, c.bytesPerSample)
		require.True(t, ok)
		got := ctx.Final()

		require.Equal(t, want, got, "MD5Accumulate(channels=%d, samples=%d, bps=%d)", c.channels, c.samples, c.bytesPerSample)
	}
}
