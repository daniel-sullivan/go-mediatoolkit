//go:build linux

package hwaccel

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/video"
)

// vaTestWidth / vaTestHeight / vaTestFrames size the synthetic clip.
const (
	vaTestWidth  = 640
	vaTestHeight = 480
	vaTestFrames = 8
)

// makeVANV12 synthesises a deterministic NV12 frame with a moving gradient,
// so successive frames differ (exercising real encode/decode rather than a
// flat field). Mirrors the VideoToolbox test's makeNV12.
func makeVANV12(w, h, idx int) video.Frame {
	y := make([]byte, w*h)
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			y[j*w+i] = byte((i + j + idx*4) & 0xff)
		}
	}
	c := make([]byte, w*h/2)
	for j := 0; j < h/2; j++ {
		for i := 0; i < w; i++ {
			c[j*w+i] = byte((i*2 + j + idx) & 0xff)
		}
	}
	return video.Frame{
		PixelFormat: video.NV12,
		Width:       w, Height: h,
		Planes:  [][]byte{y, c},
		Strides: []int{w, w},
		PTS:     time.Duration(idx) * time.Second / 30,
	}
}

// vaLumaMAE is the mean absolute error between two luma planes honouring
// each plane's stride over a w×h visible region.
func vaLumaMAE(a []byte, aStride int, b []byte, bStride, w, h int) float64 {
	var sum float64
	var n int
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			d := int(a[j*aStride+i]) - int(b[j*bStride+i])
			if d < 0 {
				d = -d
			}
			sum += float64(d)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// TestVAAPIProbe asserts the backend reports the codecs the Arc box
// exposes: H.264 and H.265 each decode+encode (the iHD low-power HEVC encode
// path is now driven correctly — see vaapi_encode_hevc_linux.go).
func TestVAAPIProbe(t *testing.T) {
	b, err := newVABackend()
	if err != nil {
		t.Skipf("vaapi unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)

	assert.True(t, caps.Supports(video.H264, Decode), "H.264 decode")
	assert.True(t, caps.Supports(video.H264, Encode), "H.264 encode")
	assert.True(t, caps.Supports(video.H265, Decode), "H.265 decode")
	assert.True(t, caps.Supports(video.H265, Encode), "H.265 encode")
	for _, c := range caps.Codecs {
		t.Logf("codec=%s decode=%v encode=%v profiles=%v", c.Codec, c.Decode, c.Encode, c.Profiles)
	}
}

// TestVAAPIH264RoundTrip proves a full hardware transcode round-trip:
// synthesise NV12 frames -> VAAPI encode -> VAAPI decode -> assert the frame
// count and that the keyframe luma is recovered near-losslessly (small MAE).
// Skips gracefully when VAAPI is unavailable (so it is a no-op on the Mac/CI).
func TestVAAPIH264RoundTrip(t *testing.T) {
	b, err := newVABackend()
	if err != nil {
		t.Skipf("vaapi unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.H264, Encode) || !caps.Supports(video.H264, Decode) {
		t.Skip("H.264 encode+decode not supported on this host")
	}

	enc, err := b.NewEncoder(NewConfig(
		WithCodec(video.H264),
		WithResolution(vaTestWidth, vaTestHeight),
		WithFrameRate(30, 1),
	))
	require.NoError(t, err)

	sources := make([]video.Frame, vaTestFrames)
	var packets []video.Packet
	for i := 0; i < vaTestFrames; i++ {
		f := makeVANV12(vaTestWidth, vaTestHeight, i)
		sources[i] = f
		pkts, err := enc.Encode(f)
		require.NoError(t, err, "encode frame %d", i)
		packets = append(packets, pkts...)
	}
	tail, err := enc.Flush()
	require.NoError(t, err)
	packets = append(packets, tail...)
	require.NoError(t, enc.Close())
	require.NotEmpty(t, packets, "encoder produced no packets")

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.H264)))
	require.NoError(t, err)
	defer dec.Close()

	var decoded []video.Frame
	start := time.Now()
	for i, p := range packets {
		frames, err := dec.Decode(p)
		require.NoError(t, err, "decode packet %d (keyframe=%v, %d bytes)", i, p.Keyframe, len(p.Data))
		decoded = append(decoded, frames...)
	}
	drained, err := dec.Flush()
	require.NoError(t, err)
	decoded = append(decoded, drained...)
	elapsed := time.Since(start)

	require.NotEmpty(t, decoded, "decoder produced no frames")
	assert.GreaterOrEqual(t, len(decoded), vaTestFrames-1,
		"expected ~%d decoded frames, got %d", vaTestFrames, len(decoded))

	for i, f := range decoded {
		assert.Equal(t, vaTestWidth, f.Width, "frame %d width", i)
		assert.Equal(t, vaTestHeight, f.Height, "frame %d height", i)
		assert.Equal(t, video.NV12, f.PixelFormat, "frame %d format", i)
		require.GreaterOrEqual(t, len(f.Planes), 2, "frame %d planes", i)
		require.NotEmpty(t, f.Planes[0], "frame %d luma", i)
		require.GreaterOrEqual(t, f.Strides[0], vaTestWidth, "frame %d luma stride", i)
	}

	// Keyframe[0] fidelity: an all-intra QP-22 encode is near lossless.
	mae0 := vaLumaMAE(sources[0].Planes[0], sources[0].Strides[0],
		decoded[0].Planes[0], decoded[0].Strides[0], vaTestWidth, vaTestHeight)
	assert.Less(t, mae0, 4.0, "keyframe luma MAE too high (%.2f)", mae0)

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
	assert.Less(t, avgMAE, 6.0, "average luma MAE too high (%.2f)", avgMAE)

	fps := float64(len(decoded)) / elapsed.Seconds()
	t.Logf("h264 round-trip: %d packets -> %d decoded frames %dx%d, "+
		"keyframe luma MAE=%.2f, avg luma MAE=%.2f, decode %v (%.0f fps)",
		len(packets), len(decoded), vaTestWidth, vaTestHeight, mae0, avgMAE, elapsed, fps)
}

// TestVAAPIH265Decode proves the hardware H.265 decode path against a
// known-valid HEVC elementary stream produced by ffmpeg's hevc_vaapi
// encoder — an independent oracle for the decoder, complementing the
// self-encode round-trip in TestVAAPIH265RoundTrip.
// Skips gracefully when VAAPI or ffmpeg is unavailable.
func TestVAAPIH265Decode(t *testing.T) {
	b, err := newVABackend()
	if err != nil {
		t.Skipf("vaapi unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.H265, Decode) {
		t.Skip("H.265 decode not supported on this host")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed (needed to produce a reference H.265 stream)")
	}

	tmp := t.TempDir() + "/ref.h265"
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=640x480:rate=30:duration=0.2",
		"-vf", "format=nv12,hwupload", "-vaapi_device", "/dev/dri/renderD128",
		"-c:v", "hevc_vaapi", "-g", "1", "-frames:v", "3", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg hevc_vaapi reference encode failed (%v): %s", err, out)
	}
	data, err := os.ReadFile(tmp)
	require.NoError(t, err)

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.H265)))
	require.NoError(t, err)
	defer dec.Close()

	start := time.Now()
	frames, err := dec.Decode(video.Packet{Codec: video.H265, Data: data, Keyframe: true})
	require.NoError(t, err)
	elapsed := time.Since(start)
	require.NotEmpty(t, frames, "decoder produced no frames")

	f := frames[0]
	assert.Equal(t, 640, f.Width)
	assert.Equal(t, 480, f.Height)
	assert.Equal(t, video.NV12, f.PixelFormat)
	require.GreaterOrEqual(t, len(f.Planes), 2)

	// A real picture: luma is not predominantly zero.
	var nonzero int
	for _, v := range f.Planes[0] {
		if v != 0 {
			nonzero++
		}
	}
	assert.Greater(t, nonzero, len(f.Planes[0])/10, "decoded luma looks empty")

	fps := float64(len(frames)) / elapsed.Seconds()
	t.Logf("h265 decode: %d bytes -> %d frames %dx%d, decode %v (%.0f fps)",
		len(data), len(frames), f.Width, f.Height, elapsed, fps)
}

// TestVAAPIH265RoundTrip proves a full hardware H.265 transcode round-trip on
// the iHD low-power encoder: synthesise NV12 frames -> VAAPI HEVC encode ->
// VAAPI HEVC decode -> assert the frame count and near-lossless luma recovery.
// Skips gracefully when VAAPI is unavailable (a no-op on the Mac/CI).
func TestVAAPIH265RoundTrip(t *testing.T) {
	b, err := newVABackend()
	if err != nil {
		t.Skipf("vaapi unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.H265, Encode) || !caps.Supports(video.H265, Decode) {
		t.Skip("H.265 encode+decode not supported on this host")
	}

	enc, err := b.NewEncoder(NewConfig(
		WithCodec(video.H265),
		WithResolution(vaTestWidth, vaTestHeight),
		WithFrameRate(30, 1),
	))
	require.NoError(t, err)

	sources := make([]video.Frame, vaTestFrames)
	var packets []video.Packet
	for i := 0; i < vaTestFrames; i++ {
		f := makeVANV12(vaTestWidth, vaTestHeight, i)
		sources[i] = f
		pkts, err := enc.Encode(f)
		require.NoError(t, err, "encode frame %d", i)
		packets = append(packets, pkts...)
	}
	tail, err := enc.Flush()
	require.NoError(t, err)
	packets = append(packets, tail...)
	require.NoError(t, enc.Close())
	require.NotEmpty(t, packets, "encoder produced no packets")
	for i, p := range packets {
		assert.Equal(t, video.H265, p.Codec, "packet %d codec", i)
	}

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.H265)))
	require.NoError(t, err)
	defer dec.Close()

	var decoded []video.Frame
	start := time.Now()
	for i, p := range packets {
		frames, err := dec.Decode(p)
		require.NoError(t, err, "decode packet %d (keyframe=%v, %d bytes)", i, p.Keyframe, len(p.Data))
		decoded = append(decoded, frames...)
	}
	drained, err := dec.Flush()
	require.NoError(t, err)
	decoded = append(decoded, drained...)
	elapsed := time.Since(start)

	require.NotEmpty(t, decoded, "decoder produced no frames")
	assert.GreaterOrEqual(t, len(decoded), vaTestFrames-1,
		"expected ~%d decoded frames, got %d", vaTestFrames, len(decoded))

	for i, f := range decoded {
		assert.Equal(t, vaTestWidth, f.Width, "frame %d width", i)
		assert.Equal(t, vaTestHeight, f.Height, "frame %d height", i)
		assert.Equal(t, video.NV12, f.PixelFormat, "frame %d format", i)
		require.GreaterOrEqual(t, len(f.Planes), 2, "frame %d planes", i)
		require.NotEmpty(t, f.Planes[0], "frame %d luma", i)
	}

	// Keyframe[0] fidelity: an all-intra QP-22 encode is near lossless.
	mae0 := vaLumaMAE(sources[0].Planes[0], sources[0].Strides[0],
		decoded[0].Planes[0], decoded[0].Strides[0], vaTestWidth, vaTestHeight)
	assert.Less(t, mae0, 4.0, "keyframe luma MAE too high (%.2f)", mae0)

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
	assert.Less(t, avgMAE, 6.0, "average luma MAE too high (%.2f)", avgMAE)

	fps := float64(len(decoded)) / elapsed.Seconds()
	t.Logf("h265 round-trip: %d packets -> %d decoded frames %dx%d, "+
		"keyframe luma MAE=%.2f, avg luma MAE=%.2f, decode %v (%.0f fps)",
		len(packets), len(decoded), vaTestWidth, vaTestHeight, mae0, avgMAE, elapsed, fps)
}

// TestVAAPIH264ToH265Transcode proves a full hardware H.264 -> H.265
// transcode round-trip: encode NV12 as H.264, decode it, re-encode the
// decoded frames as H.265, decode the H.265 back, and assert the luma is
// recovered near-losslessly end to end. Exercises both hardware codecs and
// both directions in one pipeline. Skips when VAAPI is unavailable.
func TestVAAPIH264ToH265Transcode(t *testing.T) {
	b, err := newVABackend()
	if err != nil {
		t.Skipf("vaapi unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.H264, Encode) || !caps.Supports(video.H264, Decode) ||
		!caps.Supports(video.H265, Encode) || !caps.Supports(video.H265, Decode) {
		t.Skip("H.264+H.265 encode+decode not all supported on this host")
	}

	// Stage 1: synthesise + H.264-encode.
	sources := make([]video.Frame, vaTestFrames)
	h264Enc, err := b.NewEncoder(NewConfig(
		WithCodec(video.H264), WithResolution(vaTestWidth, vaTestHeight), WithFrameRate(30, 1)))
	require.NoError(t, err)
	var h264Pkts []video.Packet
	for i := 0; i < vaTestFrames; i++ {
		f := makeVANV12(vaTestWidth, vaTestHeight, i)
		sources[i] = f
		pkts, err := h264Enc.Encode(f)
		require.NoError(t, err, "h264 encode frame %d", i)
		h264Pkts = append(h264Pkts, pkts...)
	}
	tail, err := h264Enc.Flush()
	require.NoError(t, err)
	h264Pkts = append(h264Pkts, tail...)
	require.NoError(t, h264Enc.Close())

	// Stage 2: H.264-decode -> mid frames.
	h264Dec, err := b.NewDecoder(NewConfig(WithCodec(video.H264)))
	require.NoError(t, err)
	var mid []video.Frame
	for _, p := range h264Pkts {
		frames, err := h264Dec.Decode(p)
		require.NoError(t, err)
		mid = append(mid, frames...)
	}
	drained, err := h264Dec.Flush()
	require.NoError(t, err)
	mid = append(mid, drained...)
	require.NoError(t, h264Dec.Close())
	require.NotEmpty(t, mid, "h264 decode produced no frames")

	// Stage 3: re-encode the decoded frames as H.265.
	h265Enc, err := b.NewEncoder(NewConfig(
		WithCodec(video.H265), WithResolution(vaTestWidth, vaTestHeight), WithFrameRate(30, 1)))
	require.NoError(t, err)
	var h265Pkts []video.Packet
	for i, f := range mid {
		pkts, err := h265Enc.Encode(f)
		require.NoError(t, err, "h265 re-encode frame %d", i)
		h265Pkts = append(h265Pkts, pkts...)
	}
	tail2, err := h265Enc.Flush()
	require.NoError(t, err)
	h265Pkts = append(h265Pkts, tail2...)
	require.NoError(t, h265Enc.Close())
	require.NotEmpty(t, h265Pkts, "h265 re-encode produced no packets")

	// Stage 4: H.265-decode -> out frames.
	h265Dec, err := b.NewDecoder(NewConfig(WithCodec(video.H265)))
	require.NoError(t, err)
	defer h265Dec.Close()
	var out []video.Frame
	start := time.Now()
	for _, p := range h265Pkts {
		frames, err := h265Dec.Decode(p)
		require.NoError(t, err)
		out = append(out, frames...)
	}
	drained2, err := h265Dec.Flush()
	require.NoError(t, err)
	out = append(out, drained2...)
	elapsed := time.Since(start)
	require.NotEmpty(t, out, "h265 decode produced no frames")

	// End-to-end fidelity: original NV12 luma vs the H.264->H.265 output.
	pairs := len(out)
	if pairs > len(sources) {
		pairs = len(sources)
	}
	var sumMAE float64
	for i := 0; i < pairs; i++ {
		sumMAE += vaLumaMAE(sources[i].Planes[0], sources[i].Strides[0],
			out[i].Planes[0], out[i].Strides[0], vaTestWidth, vaTestHeight)
	}
	avgMAE := sumMAE / float64(pairs)
	assert.Less(t, avgMAE, 4.0, "end-to-end transcode luma MAE too high (%.2f)", avgMAE)

	t.Logf("h264->h265 transcode: %d h264 pkts -> %d mid frames -> %d h265 pkts -> %d frames, "+
		"end-to-end luma MAE=%.2f, final decode %v", len(h264Pkts), len(mid),
		len(h265Pkts), len(out), avgMAE, elapsed)
}
