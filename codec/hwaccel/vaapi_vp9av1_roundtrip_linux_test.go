//go:build linux

package hwaccel

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// ivfFrames parses an IVF file (32-byte DKIF header, then per-frame 4-byte LE
// size + 8-byte timestamp + payload) into its coded frame payloads.
func ivfFrames(t *testing.T, data []byte) [][]byte {
	t.Helper()
	require.GreaterOrEqual(t, len(data), 32, "IVF too short")
	require.Equal(t, "DKIF", string(data[0:4]), "not an IVF file")
	var frames [][]byte
	off := 32
	for off+12 <= len(data) {
		sz := int(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 12
		if off+sz > len(data) {
			break
		}
		frames = append(frames, data[off:off+sz])
		off += sz
	}
	return frames
}

// ffmpegIVF runs ffmpeg to produce an IVF elementary stream for the given
// codec, or skips the test if ffmpeg/the encoder is unavailable.
func ffmpegIVF(t *testing.T, encoder string, extra ...string) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed (needed to produce a reference stream)")
	}
	tmp := t.TempDir() + "/ref.ivf"
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=30:duration=0.2",
		"-pix_fmt", "yuv420p", "-c:v", encoder, "-g", "1", "-frames:v", "3"}
	args = append(args, extra...)
	args = append(args, "-f", "ivf", tmp)
	cmd := exec.Command("ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg %s reference encode failed (%v): %s", encoder, err, out)
	}
	data, err := os.ReadFile(tmp)
	require.NoError(t, err)
	return data
}

// assertRealPicture checks a decoded luma plane is not predominantly zero.
func assertRealPicture(t *testing.T, f video.Frame) {
	t.Helper()
	require.GreaterOrEqual(t, len(f.Planes), 2)
	var nonzero int
	for _, v := range f.Planes[0] {
		if v != 0 {
			nonzero++
		}
	}
	assert.Greater(t, nonzero, len(f.Planes[0])/10, "decoded luma looks empty")
}

// TestVAAPIVP9Decode decodes an ffmpeg libvpx-vp9 IVF stream through the VAAPI
// VP9 VLD path and asserts real pixels come back.
func TestVAAPIVP9Decode(t *testing.T) {
	b, err := newVABackend()
	if err != nil {
		t.Skipf("vaapi unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.VP9, Decode) {
		t.Skip("VP9 decode not supported on this host")
	}

	data := ffmpegIVF(t, "libvpx-vp9", "-profile:v", "0")
	frames := ivfFrames(t, data)
	require.NotEmpty(t, frames)

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.VP9)))
	require.NoError(t, err)
	defer dec.Close()

	var decoded []video.Frame
	start := time.Now()
	for i, fr := range frames {
		out, err := dec.Decode(video.Packet{Codec: video.VP9, Data: fr, Keyframe: true})
		require.NoError(t, err, "decode frame %d (%d bytes)", i, len(fr))
		decoded = append(decoded, out...)
	}
	elapsed := time.Since(start)
	require.NotEmpty(t, decoded, "decoder produced no frames")

	f := decoded[0]
	assert.Equal(t, 320, f.Width)
	assert.Equal(t, 240, f.Height)
	assert.Equal(t, video.NV12, f.PixelFormat)
	assertRealPicture(t, f)
	t.Logf("vp9 decode: %d frames -> %d decoded %dx%d, %v (%.0f fps)",
		len(frames), len(decoded), f.Width, f.Height, elapsed,
		float64(len(decoded))/elapsed.Seconds())
}

// TestVAAPIVP9RoundTrip proves a full hardware VP9 transcode round-trip:
// synthesise NV12 -> VAAPI VP9 encode -> VAAPI VP9 decode -> assert near-
// lossless luma recovery.
func TestVAAPIVP9RoundTrip(t *testing.T) {
	b, err := newVABackend()
	if err != nil {
		t.Skipf("vaapi unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.VP9, Encode) || !caps.Supports(video.VP9, Decode) {
		t.Skip("VP9 encode+decode not supported on this host")
	}

	enc, err := b.NewEncoder(NewConfig(
		WithCodec(video.VP9), WithResolution(vaTestWidth, vaTestHeight), WithFrameRate(30, 1)))
	if errors.Is(err, ErrEncodeUnsupportedOnDriver) {
		t.Skip("VP9 encode not drivable on this iHD driver (see vaapi_encode_linux.go); decode is covered by TestVAAPIVP9Decode")
	}
	require.NoError(t, err)

	sources := make([]video.Frame, vaTestFrames)
	var packets []video.Packet
	for i := 0; i < vaTestFrames; i++ {
		f := makeVANV12(vaTestWidth, vaTestHeight, i)
		sources[i] = f
		pkts, err := enc.Encode(f)
		require.NoError(t, err, "vp9 encode frame %d", i)
		packets = append(packets, pkts...)
	}
	tail, err := enc.Flush()
	require.NoError(t, err)
	packets = append(packets, tail...)
	require.NoError(t, enc.Close())
	require.NotEmpty(t, packets, "encoder produced no packets")
	for i, p := range packets {
		assert.Equal(t, video.VP9, p.Codec, "packet %d codec", i)
	}

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.VP9)))
	require.NoError(t, err)
	defer dec.Close()

	var decoded []video.Frame
	start := time.Now()
	for i, p := range packets {
		out, err := dec.Decode(p)
		require.NoError(t, err, "decode packet %d (%d bytes)", i, len(p.Data))
		decoded = append(decoded, out...)
	}
	elapsed := time.Since(start)
	require.NotEmpty(t, decoded, "decoder produced no frames")
	assert.GreaterOrEqual(t, len(decoded), vaTestFrames-1)

	for i, f := range decoded {
		assert.Equal(t, vaTestWidth, f.Width, "frame %d width", i)
		assert.Equal(t, vaTestHeight, f.Height, "frame %d height", i)
		assert.Equal(t, video.NV12, f.PixelFormat, "frame %d format", i)
	}

	mae0 := vaLumaMAE(sources[0].Planes[0], sources[0].Strides[0],
		decoded[0].Planes[0], decoded[0].Strides[0], vaTestWidth, vaTestHeight)
	assert.Less(t, mae0, 8.0, "keyframe luma MAE too high (%.2f)", mae0)

	pairs := len(decoded)
	if pairs > vaTestFrames {
		pairs = vaTestFrames
	}
	var sumMAE float64
	for i := 0; i < pairs; i++ {
		sumMAE += vaLumaMAE(sources[i].Planes[0], sources[i].Strides[0],
			decoded[i].Planes[0], decoded[i].Strides[0], vaTestWidth, vaTestHeight)
	}
	avgMAE := sumMAE / float64(pairs)
	assert.Less(t, avgMAE, 10.0, "average luma MAE too high (%.2f)", avgMAE)

	t.Logf("vp9 round-trip: %d packets -> %d decoded frames %dx%d, "+
		"keyframe luma MAE=%.2f, avg luma MAE=%.2f, decode %v (%.0f fps)",
		len(packets), len(decoded), vaTestWidth, vaTestHeight, mae0, avgMAE, elapsed,
		float64(len(decoded))/elapsed.Seconds())
}

// TestAV1ParseReference validates the AV1 OBU/seq/frame-header parser against
// the field values the iHD reference submission carries for a 320x240 libaom
// keyframe (recovered by LIBVA_TRACE). It runs without hardware (parse only),
// skipping if ffmpeg cannot produce the stream.
func TestAV1ParseReference(t *testing.T) {
	data := ffmpegIVF(t, "libaom-av1", "-cpu-used", "8")
	frames := ivfFrames(t, data)
	require.NotEmpty(t, frames)
	obus := splitAV1OBUs(frames[0])
	require.NotEmpty(t, obus)

	var seq *av1SeqHeader
	var fh *av1FrameHeader
	for _, o := range obus {
		switch o.typ {
		case av1OBUSequenceHeader:
			s, err := parseAV1SeqHeader(o.payload)
			require.NoError(t, err)
			seq = s
		case av1OBUFrame, av1OBUFrameHeader:
			require.NotNil(t, seq)
			h := &av1FrameHeader{}
			r := &av1Reader{data: o.payload}
			require.NoError(t, parseAV1FrameHeader(r, seq, h, o.temporalID, o.spatialID))
			fh = h
		}
	}
	require.NotNil(t, seq, "no sequence header")
	require.NotNil(t, fh, "no frame header")

	assert.Equal(t, 0, seq.seqProfile, "seq_profile")
	assert.Equal(t, 8, seq.bitDepth, "bit_depth")
	assert.Equal(t, 1, seq.subsamplingX)
	assert.Equal(t, 1, seq.subsamplingY)
	assert.True(t, seq.enableCdef, "enable_cdef")
	assert.True(t, seq.enableOrderHint, "enable_order_hint")
	assert.Equal(t, 7, seq.orderHintBits, "order_hint_bits (minus_1 = 6)")

	assert.Equal(t, av1KeyFrame, fh.frameType, "frame_type")
	assert.True(t, fh.showFrame, "show_frame")
	assert.Equal(t, 320, fh.frameWidth, "frame_width")
	assert.Equal(t, 240, fh.frameHeight, "frame_height")
	assert.Equal(t, 1, fh.tileCols, "tile_cols")
	assert.Equal(t, 1, fh.tileRows, "tile_rows")
	assert.Equal(t, av1PrimaryRefNone, fh.primaryRefFrame, "primary_ref_frame")
	assert.False(t, fh.segEnabled, "segmentation_enabled")
	t.Logf("AV1 parse: profile=%d bd=%d cdef_bits=%d cdef_y0=%d base_q=%d lf=%v sharp=%d tileCols=%d widthSbs=%v",
		seq.seqProfile, seq.bitDepth, fh.cdefBits, fh.cdefYStrengths[0], fh.baseQIdx,
		fh.loopFilterLevel, fh.sharpness, fh.tileCols, fh.widthInSbs)
}

// TestVAAPIAV1Decode decodes an ffmpeg libaom-av1 IVF stream through the VAAPI
// AV1 VLD path and asserts real pixels come back.
func TestVAAPIAV1Decode(t *testing.T) {
	b, err := newVABackend()
	if err != nil {
		t.Skipf("vaapi unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.AV1, Decode) {
		t.Skip("AV1 decode not supported on this host")
	}

	data := ffmpegIVF(t, "libaom-av1", "-cpu-used", "8")
	frames := ivfFrames(t, data)
	require.NotEmpty(t, frames)

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.AV1)))
	require.NoError(t, err)
	defer dec.Close()

	var decoded []video.Frame
	start := time.Now()
	for i, fr := range frames {
		out, err := dec.Decode(video.Packet{Codec: video.AV1, Data: fr, Keyframe: i == 0})
		require.NoError(t, err, "decode frame %d (%d bytes)", i, len(fr))
		decoded = append(decoded, out...)
	}
	elapsed := time.Since(start)
	require.NotEmpty(t, decoded, "decoder produced no frames")

	f := decoded[0]
	assert.Equal(t, 320, f.Width)
	assert.Equal(t, 240, f.Height)
	assert.Equal(t, video.NV12, f.PixelFormat)
	assertRealPicture(t, f)
	t.Logf("av1 decode: %d frames -> %d decoded %dx%d, %v (%.0f fps)",
		len(frames), len(decoded), f.Width, f.Height, elapsed,
		float64(len(decoded))/elapsed.Seconds())
}
