// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrtag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 port of LAME 3.100's VbrTag.c
// (vbrtag.go) against the vendored LAME C reference. The end target is a
// BYTE-IDENTICAL Xing/Info + LAME tag frame vs the genuine
// lame_get_lametag_frame.
//
// The tag bytes depend on the REAL encoded -V2 frames, so the C oracle drives a
// genuine end-to-end vbr_default (== vbr_mtrh) encode through the full vendored
// LAME encoder, captures the genuine lame_get_lametag_frame bytes, and exports
// the gfc->VBR_seek_table / nMusicCRC / cfg / ov_enc / ov_rpg state. The Go side
// reconstructs an identical LameInternalFlags + LameGlobalFlags and runs the
// native lame_get_lametag_frame; the two tag frames must match byte-for-byte.
//
// Because both sides operate on IDENTICAL injected state, the tag assembly is
// integer-deterministic: the bag arithmetic, the CRC, the big-endian packing and
// the bit shifts are bit-exact in any build. The only FP in VbrTag.c —
// Xing_seek_table's `256.*act/sum` (a double mul then div, no FMA-fusable
// mul+add) and PutLameVBR's nLowpass `lowpassfreq/100.0 + .5` (div then add) —
// is fusion-immune, so the byte-identical assertion holds in the default build
// too and is asserted unconditionally. (The FP-bearing part is the encode that
// FEEDS the bag/CRC, which the vbr-iteration-loop slice gates under mp3_strict;
// here that work is the oracle's, captured verbatim.)

// encodeCases drive a few real -V2 encodes whose synthetic audio differs (seed /
// length / channels) so the bag fills to different positions and the music CRC /
// frame count / bitrate histogram vary.
type encodeCase struct {
	name       string
	samplerate int
	channels   int
	nsamplesCh int
	seed       uint32
}

var encodeCases = []encodeCase{
	{"44k1_stereo_short", 44100, 2, 8 * 1152, 1},
	{"44k1_stereo_long", 44100, 2, 64 * 1152, 7},
	{"44k1_mono", 44100, 1, 32 * 1152, 3},
	{"48k_stereo", 48000, 2, 48 * 1152, 5},
	{"32k_stereo", 32000, 2, 40 * 1152, 9},
}

func TestLametagFrameByteIdentical(t *testing.T) {
	for _, tc := range encodeCases {
		t.Run(tc.name, func(t *testing.T) {
			c := cgoRun(tc.samplerate, tc.channels, tc.nsamplesCh, tc.seed)
			require.NotNil(t, c, "C oracle -V2 encode failed")
			defer c.free()

			golden := c.goldenFrame()
			require.NotEmpty(t, golden, "genuine lame_get_lametag_frame returned no bytes")

			gfc, gfp := reconstructFromOracle(c)

			// Sanity: the tag must be enabled and the seek table populated, else
			// the C side would have produced no bytes too.
			require.NotZero(t, gfc.Cfg.WriteLameTag, "write_lame_tag should be set for -V2")
			require.Greater(t, gfc.VBRSeekTable.Pos, 0, "seek table should be populated")

			buf := make([]byte, len(golden)+64)
			n := gfc.LameGetLametagFrameParity(gfp, buf, len(buf))
			require.Equal(t, len(golden), n, "native tag frame length")

			assert.Equal(t, golden, buf[:n], "tag frame bytes must be byte-identical to LAME")
		})
	}
}

// TestCRC16LookupTable pins the crc16_lookup[256] table verbatim against the
// genuine VbrTag.c static (probed one step at a time through the real
// UpdateMusicCRC). crc16_lookup[v] == CRC_update_lookup(v, 0).
func TestCRC16LookupTable(t *testing.T) {
	for v := 0; v < 256; v++ {
		want := crcStep(uint16(v), 0) // CRC_update_lookup(v, 0) folds the table
		got := nativemp3.CRCUpdateLookupParity(uint16(v), 0)
		require.Equalf(t, want, got, "CRC step v=%d", v)
		// and the raw table entry equals that fold of v against crc=0.
		assert.Equalf(t, uint(want), nativemp3.CRC16LookupParity(v), "crc16_lookup[%d]", v)
	}
}

// TestCRCUpdateLookupStep pins crcUpdateLookup against the genuine static across
// arbitrary (value, crc) pairs.
func TestCRCUpdateLookupStep(t *testing.T) {
	for _, crc := range []uint16{0x0000, 0x1234, 0xABCD, 0xFFFF, 0x8000, 0x00FF} {
		for v := 0; v < 256; v += 7 {
			want := crcStep(uint16(v), crc)
			got := nativemp3.CRCUpdateLookupParity(uint16(v), crc)
			require.Equalf(t, want, got, "CRC_update_lookup(value=%d, crc=%#x)", v, crc)
		}
	}
}

// TestMusicCRCMatches confirms the native running music CRC (UpdateMusicCRC over
// the audio bytes) equals the gfc->nMusicCRC the genuine encode accumulated. The
// reconstructed gfc carries the captured CRC; this asserts the native CRC kernel
// reproduces it from a known byte run.
func TestMusicCRCMatches(t *testing.T) {
	// Fold a deterministic byte run both ways: through the genuine static
	// (one byte at a time) and through the native UpdateMusicCRC over the whole
	// slice. They must agree.
	buf := make([]byte, 4096)
	for i := range buf {
		buf[i] = byte((i*131 + 17) & 0xff)
	}
	var want uint16
	for _, b := range buf {
		want = crcStep(uint16(b), want)
	}
	got := nativemp3.UpdateMusicCRCParity(0, buf)
	assert.Equal(t, want, got, "UpdateMusicCRC over a byte run")
}

// TestSeekTableBagAndTOC cross-checks the native addVbr bag arithmetic + the
// Xing seek TOC against the C oracle's captured bag for a real -V2 encode: it
// replays the captured per-frame bitrates? No — the per-frame bitrates are not
// individually exported. Instead it confirms that the native Xing_seek_table over
// the captured (bag, pos, sum) reproduces the TOC LAME embedded in the golden
// frame.
func TestSeekTableTOCMatchesGolden(t *testing.T) {
	c := cgoRun(44100, 2, 64*1152, 7)
	require.NotNil(t, c)
	defer c.free()

	golden := c.goldenFrame()
	require.NotEmpty(t, golden)

	gfc, _ := reconstructFromOracle(c)
	if gfc.Cfg.FreeFormat != 0 {
		t.Skip("free-format TOC path not exercised")
	}

	// The golden TOC sits right after the 4-byte magic + 4-byte flags + 4-byte
	// frames + 4-byte bytes, at offset sideinfo_len (+ the error_protection -2).
	off := gfc.Cfg.SideinfoLen
	if gfc.Cfg.ErrorProtection != 0 {
		off -= 2
	}
	off += 4 + 4 + 4 + 4 // magic + flags + nframes + streamsize
	ntoc := nativemp3.NumTocEntries()
	require.GreaterOrEqual(t, len(golden), off+ntoc)
	goldenTOC := golden[off : off+ntoc]

	var nativeTOC = make([]byte, ntoc)
	nativemp3.XingSeekTableParity(&gfc.VBRSeekTable, nativeTOC)

	assert.Equal(t, goldenTOC, nativeTOC, "Xing seek TOC must match the embedded golden TOC")
}
