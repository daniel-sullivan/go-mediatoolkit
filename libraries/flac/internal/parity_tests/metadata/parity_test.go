//go:build cgo

package metadata

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// runWithSource builds a Go BitReader over body and invokes f, then
// returns the bytes the reader consumed (body length minus the
// unconsumed remainder). Every observation point in these tests is
// byte aligned, so the byte count is exact.
func runWithSource(body []byte, f func(*nativeflac.BitReader)) (consumed uint32) {
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
	f(br)
	return uint32(len(body)) - br.GetInputBitsUnconsumed()/8
}

// ── STREAMINFO ──────────────────────────────────────────────────────

func TestParityReadMetadataStreamInfo(t *testing.T) {
	md5a := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	md5zero := [16]byte{}

	cases := []struct {
		name                       string
		isLast                     bool
		minBS, maxBS, minFS, maxFS uint32
		sr, ch, bps                uint32
		total                      uint64
		md5                        [16]byte
		extraPad                   uint32
	}{
		{"typical", false, 4096, 4096, 100, 8192, 44100, 2, 16, 1000000, md5a, 0},
		{"mono-24", true, 1024, 4096, 0, 0, 96000, 1, 24, 0, md5a, 0},
		{"8ch-32-maxrate", false, 16, 65535, 1, 16777215, 1048575, 8, 32, 0xFFFFFFFFF, md5a, 0},
		{"zero-md5-disables-check", true, 4608, 4608, 200, 4000, 48000, 2, 16, 500, md5zero, 0},
		{"extra-pad-skip", false, 4096, 4096, 0, 0, 44100, 2, 16, 0, md5a, 7},
		{"min-bps-min-ch", true, 16, 16, 0, 0, 1, 1, 4, 1, md5a, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := EncodeStreamInfo(tc.isLast, 0, tc.minBS, tc.maxBS, tc.minFS, tc.maxFS,
				tc.sr, tc.ch, tc.bps, tc.total, tc.md5, tc.extraPad)

			cInfo, cSt, cConsumed := CgoReadMetadata(body)
			require.Equal(t, 0, cSt, "C oracle read_metadata_ failed")
			require.True(t, cInfo.HasStreamInfo)

			var res nativeflac.ReadMetadataResult
			goConsumed := runWithSource(body, func(br *nativeflac.BitReader) {
				st := nativeflac.ReadMetadata(br, &res)
				require.Equal(t, nativeflac.ReadMetadataOK, st, "Go ReadMetadata failed")
			})

			require.True(t, res.HasStreamInfo)
			require.Equal(t, cInfo.IsLast, res.Header.IsLast)
			require.Equal(t, nativeflac.MetadataType(cInfo.Type), res.Header.Type)
			require.Equal(t, cInfo.Length, res.Header.Length)
			require.Equal(t, cInfo.MinBlockSize, res.StreamInfo.MinBlockSize)
			require.Equal(t, cInfo.MaxBlockSize, res.StreamInfo.MaxBlockSize)
			require.Equal(t, cInfo.MinFrameSize, res.StreamInfo.MinFrameSize)
			require.Equal(t, cInfo.MaxFrameSize, res.StreamInfo.MaxFrameSize)
			require.Equal(t, cInfo.SampleRate, res.StreamInfo.SampleRate)
			require.Equal(t, cInfo.Channels, res.StreamInfo.Channels)
			require.Equal(t, cInfo.BitsPerSample, res.StreamInfo.BitsPerSample)
			require.Equal(t, cInfo.TotalSamples, res.StreamInfo.TotalSamples)
			require.Equal(t, cInfo.MD5Sum, res.StreamInfo.MD5Sum)
			require.Equal(t, cInfo.MD5IsZero, res.MD5IsZero)
			require.Equal(t, cConsumed, goConsumed, "post-read position mismatch")
		})
	}
}

// ── SKIP PATH (non-STREAMINFO blocks) ───────────────────────────────

func TestParityReadMetadataSkip(t *testing.T) {
	// type, length pairs covering every skip-path block type.
	cases := []struct {
		name   string
		typ    uint32
		length uint32
		isLast bool
	}{
		{"padding-empty", 1, 0, false},
		{"padding", 1, 100, false},
		{"application", 2, 64, false},       // includes 4-byte id + 60 body
		{"application-id-only", 2, 4, true}, // exactly the id, no body
		{"seektable", 3, 18 * 3, false},
		{"vorbis-comment", 4, 200, false},
		{"cuesheet", 5, 512, false},
		{"picture", 6, 1024, true},
		{"unknown-type", 7, 50, false},
		{"max-type", 126, 33, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := EncodeGeneric(tc.isLast, tc.typ, tc.length, nil)

			cInfo, cSt, cConsumed := CgoReadMetadata(body)
			require.Equal(t, 0, cSt, "C oracle read_metadata_ failed")
			require.False(t, cInfo.HasStreamInfo)

			var res nativeflac.ReadMetadataResult
			goConsumed := runWithSource(body, func(br *nativeflac.BitReader) {
				st := nativeflac.ReadMetadata(br, &res)
				require.Equal(t, nativeflac.ReadMetadataOK, st, "Go ReadMetadata failed")
			})

			require.False(t, res.HasStreamInfo)
			require.Equal(t, cInfo.IsLast, res.Header.IsLast)
			require.Equal(t, nativeflac.MetadataType(cInfo.Type), res.Header.Type)
			require.Equal(t, cInfo.Length, res.Header.Length)
			require.Equal(t, cConsumed, goConsumed, "post-read position mismatch")
		})
	}
}

// ── FIND METADATA ───────────────────────────────────────────────────

func TestParityFindMetadataFLaC(t *testing.T) {
	// "fLaC" directly, then a metadata header byte that must remain
	// unconsumed by find_metadata_.
	body := append([]byte("fLaC"), 0x00, 0x00, 0x00, 0x22)

	cSt, cCached, cLook, _, cLost, cConsumed := CgoFindMetadata(body, false, 0)
	require.Equal(t, 0, cSt) // FM_READ_METADATA

	var st nativeflac.FindMetadataState
	var goStatus nativeflac.FindMetadataStatus
	goConsumed := runWithSource(body, func(br *nativeflac.BitReader) {
		goStatus = nativeflac.FindMetadata(br, &st)
	})
	require.Equal(t, nativeflac.FindMetadataReadMetadata, goStatus)
	require.Equal(t, cCached, st.Cached)
	require.Equal(t, cLook, st.Lookahead)
	require.Equal(t, cLost, st.LostSync)
	require.Equal(t, cConsumed, goConsumed, "post-find position mismatch")
}

func TestParityFindMetadataFrameSync(t *testing.T) {
	// Headerless stream: first bytes are a frame sync 0xFF 0xF8.
	body := []byte{0xFF, 0xF8, 0x12, 0x34}

	cSt, _, _, cWarm, cLost, cConsumed := CgoFindMetadata(body, false, 0)
	require.Equal(t, 1, cSt) // FM_READ_FRAME

	var st nativeflac.FindMetadataState
	var goStatus nativeflac.FindMetadataStatus
	goConsumed := runWithSource(body, func(br *nativeflac.BitReader) {
		goStatus = nativeflac.FindMetadata(br, &st)
	})
	require.Equal(t, nativeflac.FindMetadataReadFrame, goStatus)
	require.Equal(t, cWarm, st.HeaderWarmup)
	require.Equal(t, cLost, st.LostSync)
	require.Equal(t, cConsumed, goConsumed, "post-find position mismatch")
}

func TestParityFindMetadataLostSyncThenFLaC(t *testing.T) {
	// Junk bytes that don't match "fLaC" / "ID3" / a sync, then "fLaC".
	// libFLAC emits one LOST_SYNC, scans past the junk, then succeeds.
	body := append([]byte{0x12, 0x34, 0x56}, []byte("fLaC")...)
	body = append(body, 0x00)

	cSt, cCached, cLook, _, cLost, cConsumed := CgoFindMetadata(body, false, 0)
	require.Equal(t, 0, cSt)
	require.True(t, cLost)

	var st nativeflac.FindMetadataState
	var goStatus nativeflac.FindMetadataStatus
	goConsumed := runWithSource(body, func(br *nativeflac.BitReader) {
		goStatus = nativeflac.FindMetadata(br, &st)
	})
	require.Equal(t, nativeflac.FindMetadataReadMetadata, goStatus)
	require.Equal(t, cCached, st.Cached)
	require.Equal(t, cLook, st.Lookahead)
	require.Equal(t, cLost, st.LostSync)
	require.Equal(t, cConsumed, goConsumed, "post-find position mismatch")
}

func TestParityFindMetadataID3Skip(t *testing.T) {
	// ID3v2 tag: "ID3" + 2 version bytes + 1 flags byte + 4 syncsafe
	// size bytes, then `size` tag bytes, then "fLaC".
	const tagSize = 10
	hdr := []byte{'I', 'D', '3', 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, byte(tagSize)}
	body := append([]byte{}, hdr...)
	body = append(body, make([]byte, tagSize)...)
	body = append(body, []byte("fLaC")...)
	body = append(body, 0x00)

	cSt, cCached, cLook, _, cLost, cConsumed := CgoFindMetadata(body, false, 0)
	require.Equal(t, 0, cSt)

	var st nativeflac.FindMetadataState
	var goStatus nativeflac.FindMetadataStatus
	goConsumed := runWithSource(body, func(br *nativeflac.BitReader) {
		goStatus = nativeflac.FindMetadata(br, &st)
	})
	require.Equal(t, nativeflac.FindMetadataReadMetadata, goStatus)
	require.Equal(t, cCached, st.Cached)
	require.Equal(t, cLook, st.Lookahead)
	require.Equal(t, cLost, st.LostSync)
	require.Equal(t, cConsumed, goConsumed, "post-find position mismatch")
}

// TestParityFindThenReadStreamInfo exercises the full decode-front:
// find the "fLaC" marker, then read the first (STREAMINFO) block,
// asserting both decoders land at the same stream position.
func TestParityFindThenReadStreamInfo(t *testing.T) {
	md5 := [16]byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5, 6}
	si := EncodeStreamInfo(true, 0, 4096, 4096, 10, 9000, 44100, 2, 16, 123456, md5, 0)
	body := append([]byte("fLaC"), si...)

	// C oracle: find, then read.
	cFindSt, _, _, _, _, _ := CgoFindMetadata(body, false, 0)
	require.Equal(t, 0, cFindSt)
	cInfo, cReadSt, _ := CgoReadMetadata(body[4:])
	require.Equal(t, 0, cReadSt)
	require.True(t, cInfo.HasStreamInfo)

	// Go: single reader, find then read in sequence.
	var res nativeflac.ReadMetadataResult
	var fst nativeflac.FindMetadataState
	var findStatus nativeflac.FindMetadataStatus
	var readStatus nativeflac.ReadMetadataStatus
	goConsumed := runWithSource(body, func(br *nativeflac.BitReader) {
		findStatus = nativeflac.FindMetadata(br, &fst)
		require.Equal(t, nativeflac.FindMetadataReadMetadata, findStatus)
		readStatus = nativeflac.ReadMetadata(br, &res)
	})
	require.Equal(t, nativeflac.ReadMetadataOK, readStatus)
	require.True(t, res.HasStreamInfo)
	require.Equal(t, cInfo.SampleRate, res.StreamInfo.SampleRate)
	require.Equal(t, cInfo.Channels, res.StreamInfo.Channels)
	require.Equal(t, cInfo.BitsPerSample, res.StreamInfo.BitsPerSample)
	require.Equal(t, cInfo.TotalSamples, res.StreamInfo.TotalSamples)
	require.Equal(t, cInfo.MD5Sum, res.StreamInfo.MD5Sum)
	require.Equal(t, uint32(len(body)), goConsumed, "Go should consume the whole buffer")
}
