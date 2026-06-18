//go:build cgo

package framedecodedispatch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// requireStrict skips a parity test unless the mp3_strict build tag is set.
// The frame-decode-dispatch PROBE path exercised here is integer-only and
// therefore matches in both build modes, but the suite gates uniformly with
// the FP-bearing slices so a bare `go test` stays clean and the canonical
// `mise run //libraries/mp3:parity` (which sets -tags=mp3_strict + the scalar
// CGO flags) is the single bit-exact gate. See the FP-parity convention in the
// add-audio-format skill.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("frame-decode-dispatch parity asserts bit-exactness; run under -tags=mp3_strict (mise run //libraries/mp3:parity)")
	}
}

// goDecodeProbe runs nativemp3.DecodeFrame in PROBE mode (pcm == nil) over a
// fresh decoder seeded with freeFormatSeed, mirroring cgoDecodeProbe so the
// two can be compared field-for-field. PROBE mode never reaches the l3Decode /
// l12DecodeFrame seams (DecodeFrame returns right after filling info when pcm
// is nil), so this is safe even though those seams are unassigned in the
// current port.
func goDecodeProbe(mp3 []byte, mp3Bytes, freeFormatSeed int) cgoFrameInfo {
	var dec nativemp3.Decoder
	dec.FreeFormatBytes = freeFormatSeed
	var info nativemp3.FrameInfo
	samples := nativemp3.DecodeFrame(&dec, mp3, mp3Bytes, nil, &info)
	return cgoFrameInfo{
		samples:         samples,
		header0:         int(dec.Header[0]),
		freeFormatBytes: dec.FreeFormatBytes,
		frameBytes:      info.FrameBytes,
		frameOffset:     info.FrameOffset,
		channels:        info.Channels,
		hz:              info.Hz,
		layer:           info.Layer,
		bitrateKbps:     info.BitrateKbps,
	}
}

// frameLen is the byte length of an MPEG-1 Layer III 128 kbps 44100 Hz frame
// (hdr_frame_bytes for the {0xFF,0xFB,0x90,0x04} header, no padding). A run of
// these is what mp3d_find_frame accepts as a sync.
const frameLen = 417

// mp3Header is the canonical MPEG-1 L3 128k/44100/stereo/no-pad/no-crc header
// the find-frame corpus is built from.
var mp3Header = []byte{0xFF, 0xFB, 0x90, 0x04}

// buildStream lays leadGarbage bytes of non-sync filler ahead of `frames`
// identical header-prefixed frames of `flen` bytes each, then `trailing` zero
// bytes. mp3d_find_frame needs MAX_FRAME_SYNC_MATCHES consecutive matching
// headers to accept the first frame.
func buildStream(hdr []byte, leadGarbage, frames, flen, trailing int) []byte {
	buf := make([]byte, leadGarbage)
	for i := 0; i < frames; i++ {
		f := make([]byte, flen)
		copy(f, hdr)
		buf = append(buf, f...)
	}
	buf = append(buf, make([]byte, trailing)...)
	return buf
}

// TestInitParity pins mp3dec_init (nativemp3.Mp3decInit): it clears the cached
// header[0] byte and nothing else the fast-resync path keys on.
func TestInitParity(t *testing.T) {
	requireStrict(t)
	for _, seed := range []byte{0x00, 0xFF, 0x42, 0xAB} {
		var dec nativemp3.Decoder
		dec.Header[0] = seed
		nativemp3.Mp3decInit(&dec)
		assert.Equalf(t, cgoInit(seed), int(dec.Header[0]), "mp3dec_init header[0] seed=%#x", seed)
	}
}

// TestDecodeProbeParity drives mp3dec_decode_frame in PROBE mode over a corpus
// of streams that exercise every branch of the dispatch reachable without
// audio decode: a clean accepted frame, leading garbage before sync, too-few
// frames to satisfy the sync (falls through to the not-found early return),
// a frame whose tail runs past the buffer end (i+frame_size > mp3_bytes), a
// short buffer, and pure non-sync bytes. Every filled mp3dec_frame_info_t
// field plus the returned sample count, the cached header[0], and the
// round-tripped free_format_bytes are compared.
func TestDecodeProbeParity(t *testing.T) {
	requireStrict(t)

	const syncMatches = nativemp3.MaxFrameSyncMatches

	type tc struct {
		name      string
		stream    []byte
		freeForma int
	}
	cases := []tc{
		{"clean-aligned", buildStream(mp3Header, 0, syncMatches+2, frameLen, 0), 0},
		{"lead-garbage-1", buildStream(mp3Header, 1, syncMatches+2, frameLen, 0), 0},
		{"lead-garbage-37", buildStream(mp3Header, 37, syncMatches+2, frameLen, 0), 0},
		{"few-frames-fallthrough", buildStream(mp3Header, 3, 2, frameLen, 0), 0},
		{"trailing-garbage", buildStream(mp3Header, 0, syncMatches+3, frameLen, 7), 0},
		// A long run so the first frame is accepted, then truncated right
		// after the accepted frame's header so i+frame_size > mp3_bytes is
		// possible on the boundary cases.
		{"single-frame-only", buildStream(mp3Header, 0, 1, frameLen, 0), 0},
		{"mono-header", buildStream([]byte{0xFF, 0xFB, 0x90, 0xC4}, 0, syncMatches+2, frameLen, 0), 0},
		{"mpeg2-l3", buildStream([]byte{0xFF, 0xF3, 0x90, 0x04}, 0, syncMatches+2, 627, 0), 0},
		{"nonsync-bytes", []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}, 0},
		{"empty", []byte{}, 0},
		{"tiny", []byte{0xFF, 0xFB}, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := cgoDecodeProbe(c.stream, len(c.stream), c.freeForma)
			got := goDecodeProbe(c.stream, len(c.stream), c.freeForma)
			assert.Equal(t, want.samples, got.samples, "samples")
			assert.Equal(t, want.frameBytes, got.frameBytes, "frame_bytes")
			assert.Equal(t, want.frameOffset, got.frameOffset, "frame_offset")
			assert.Equal(t, want.channels, got.channels, "channels")
			assert.Equal(t, want.hz, got.hz, "hz")
			assert.Equal(t, want.layer, got.layer, "layer")
			assert.Equal(t, want.bitrateKbps, got.bitrateKbps, "bitrate_kbps")
			assert.Equal(t, want.header0, got.header0, "header[0]")
			assert.Equal(t, want.freeFormatBytes, got.freeFormatBytes, "free_format_bytes")
		})
	}
}

// TestFastResyncParity exercises the cached-header fast path: when a previous
// frame left dec.Header set and the next input begins with a matching header
// at exactly frame_size, the dispatch validates and accepts without a full
// find-frame scan. We drive two decode calls in sequence on each side over a
// multi-frame stream so the second call hits the cached-header compare branch.
func TestFastResyncParity(t *testing.T) {
	requireStrict(t)

	stream := buildStream(mp3Header, 0, nativemp3.MaxFrameSyncMatches+4, frameLen, 0)

	// First call: cold decode of the whole stream (probe).
	wantFirst := cgoDecodeProbe(stream, len(stream), 0)
	gotFirst := goDecodeProbe(stream, len(stream), 0)
	require.Equal(t, wantFirst.frameBytes, gotFirst.frameBytes, "first frame_bytes")

	// Second call: feed the buffer starting at the next frame so the cached
	// header (set by the first call) matches at offset 0. To observe the
	// cached-header branch we must thread the SAME decoder across both calls;
	// drive that on both sides via dedicated two-call trampolines.
	want := cgoDecodeTwice(stream, frameLen)
	got := goDecodeTwice(stream, frameLen)
	assert.Equal(t, want.samples, got.samples, "second samples")
	assert.Equal(t, want.frameBytes, got.frameBytes, "second frame_bytes")
	assert.Equal(t, want.frameOffset, got.frameOffset, "second frame_offset")
	assert.Equal(t, want.channels, got.channels, "second channels")
	assert.Equal(t, want.hz, got.hz, "second hz")
	assert.Equal(t, want.layer, got.layer, "second layer")
	assert.Equal(t, want.bitrateKbps, got.bitrateKbps, "second bitrate_kbps")
	assert.Equal(t, want.header0, got.header0, "second header[0]")
}

// goDecodeTwice probes the stream once (cold), then probes the tail starting
// at `advance` bytes with the SAME decoder, returning the second call's
// observables — so the cached-header fast-resync branch is hit on call two.
func goDecodeTwice(stream []byte, advance int) cgoFrameInfo {
	var dec nativemp3.Decoder
	var info nativemp3.FrameInfo
	nativemp3.DecodeFrame(&dec, stream, len(stream), nil, &info)
	tail := stream[advance:]
	samples := nativemp3.DecodeFrame(&dec, tail, len(tail), nil, &info)
	return cgoFrameInfo{
		samples:     samples,
		header0:     int(dec.Header[0]),
		frameBytes:  info.FrameBytes,
		frameOffset: info.FrameOffset,
		channels:    info.Channels,
		hz:          info.Hz,
		layer:       info.Layer,
		bitrateKbps: info.BitrateKbps,
	}
}
