//go:build linux

package hwaccel

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// TestV4L2Probe asserts the backend enumerates the host's M2M nodes and
// reports the codecs they handle. On the Pi 5 this finds the rpi-hevc-dec
// stateless HEVC decoder. Skips cleanly when no M2M node is present (a
// no-op on Mac / CI / the Arc box without an M2M codec).
func TestV4L2Probe(t *testing.T) {
	b, err := newV4L2Backend()
	if err != nil {
		t.Skipf("v4l2: no M2M video node present: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)

	for _, n := range b.nodes {
		t.Logf("node %s driver=%s decKind=%d encKind=%d decodes=%v encodes=%v",
			n.path, n.driver, n.decKind, n.encKind, n.decodes, n.encodes)
	}
	for _, c := range caps.Codecs {
		t.Logf("codec=%s decode=%v encode=%v", c.Codec, c.Decode, c.Encode)
	}

	// At least one node must classify as a decoder or encoder, otherwise the
	// backend would not have registered.
	require.NotEmpty(t, b.nodes, "no M2M nodes classified")
}

// TestV4L2StatelessHEVCDecode runs the Pi-5 stateless HEVC decode path on
// a real HEVC elementary stream (V4L2_TEST_HEVC, an Annex-B .h265 file),
// asserting frames come out at the right resolution with sane,
// correctly-de-tiled luma. Skips when no stateless HEVC node is present or
// the test stream is not supplied.
func TestV4L2StatelessHEVCDecode(t *testing.T) {
	path := os.Getenv("V4L2_TEST_HEVC")
	if path == "" {
		t.Skip("V4L2_TEST_HEVC not set (path to an Annex-B .h265 stream)")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	b, err := newV4L2Backend()
	if err != nil {
		t.Skipf("v4l2: no M2M video node present: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.H265, Decode) {
		t.Skip("v4l2: no HEVC decode node on this host")
	}
	// Require the stateless architecture for this test.
	var stateless bool
	for _, n := range b.nodes {
		if n.decodes[video.H265] && n.decKind == v4l2KindStateless {
			stateless = true
		}
	}
	if !stateless {
		t.Skip("v4l2: HEVC decode node is not stateless (Request API)")
	}

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.H265), WithFrameRate(30, 1)))
	require.NoError(t, err)
	defer dec.Close()

	// Feed the stream as access units split on AUDs / IRAP boundaries. The
	// simplest correct framing: hand each coded picture (a run of NALs up to
	// the next slice's first_slice flag) to Decode. Here we rely on the
	// decoder's per-AU NAL classification by splitting the elementary stream
	// into access units at every VPS or slice boundary heuristically; for a
	// keyframe-rich test clip, feeding the whole stream as one packet and
	// letting per-picture state advance also works because each Decode call
	// processes exactly the slices it was given. We split per access unit.
	aus := splitAccessUnits(data)
	require.NotEmpty(t, aus, "no access units parsed from %s", path)

	var frames []video.Frame
	start := time.Now()
	for i, au := range aus {
		fs, derr := dec.Decode(video.Packet{Codec: video.H265, Data: au, Keyframe: i == 0})
		require.NoError(t, derr, "decode AU %d (%d bytes)", i, len(au))
		frames = append(frames, fs...)
	}
	drained, err := dec.Flush()
	require.NoError(t, err)
	frames = append(frames, drained...)
	elapsed := time.Since(start)

	require.NotEmpty(t, frames, "decoder produced no frames")

	f := frames[0]
	assert.Equal(t, video.NV12, f.PixelFormat)
	assert.Positive(t, f.Width, "decoded width")
	assert.Positive(t, f.Height, "decoded height")
	require.GreaterOrEqual(t, len(f.Planes), 2)
	require.Len(t, f.Planes[0], f.Width*f.Height, "luma plane size")
	require.Len(t, f.Planes[1], f.Width*f.Height/2, "chroma plane size")

	// De-tiling sanity: a real picture's luma is not predominantly a single
	// value, and the testsrc clip has a bright bar so the mean is mid-range.
	var nonzero, sum int
	for _, v := range f.Planes[0] {
		if v != 0 {
			nonzero++
		}
		sum += int(v)
	}
	assert.Greater(t, nonzero, len(f.Planes[0])/10, "decoded luma looks empty (de-tile failure)")
	mean := float64(sum) / float64(len(f.Planes[0]))
	assert.Greater(t, mean, 8.0, "luma mean too low (%.1f)", mean)
	assert.Less(t, mean, 250.0, "luma mean too high (%.1f)", mean)

	// Strong de-tile validation: compare frame 0 against ffmpeg's
	// independently-decoded linear NV12 of the same picture, when ffmpeg is
	// available. A correct de-tile is near bit-exact (both are HEVC-conformant
	// decodes of the same IDR), so the MAE must be tiny.
	if ref := referenceNV12(t, path, f.Width, f.Height); ref != nil {
		mae := lumaMAEFlat(f.Planes[0], ref[:f.Width*f.Height], f.Width, f.Height)
		assert.Less(t, mae, 2.0, "de-tiled luma MAE vs ffmpeg too high (%.3f) — de-tile geometry wrong", mae)
		t.Logf("de-tile validated against ffmpeg reference: luma MAE=%.3f", mae)
	}

	fps := float64(len(frames)) / elapsed.Seconds()
	t.Logf("v4l2 stateless HEVC: %d AUs -> %d frames %dx%d, luma mean=%.1f, "+
		"decode %v (%.0f fps)", len(aus), len(frames), f.Width, f.Height, mean, elapsed, fps)
}

// referenceNV12 decodes the first frame of the HEVC stream to linear NV12
// via ffmpeg as an independent de-tile oracle, or returns nil if ffmpeg is
// unavailable.
func referenceNV12(t *testing.T, hevcPath string, w, h int) []byte {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil
	}
	out := t.TempDir() + "/ref_nv12.raw"
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-i", hevcPath, "-frames:v", "1", "-pix_fmt", "nv12", "-f", "rawvideo", out)
	if err := cmd.Run(); err != nil {
		return nil
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) < w*h*3/2 {
		return nil
	}
	return data
}

// lumaMAEFlat is the mean absolute error between two tightly-packed luma
// planes of w*h bytes.
func lumaMAEFlat(a, b []byte, w, h int) float64 {
	n := w * h
	if len(a) < n || len(b) < n {
		return 1e9
	}
	var sum float64
	for i := 0; i < n; i++ {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		sum += float64(d)
	}
	return sum / float64(n)
}

// TestV4L2UnsupportedCodecFallback asserts that requesting a codec no V4L2
// node handles surfaces ErrUnsupportedCodec from the backend (which the
// policy walk turns into a loud HardwareFallbackEvent). Skips when no M2M
// node is present.
func TestV4L2UnsupportedCodecFallback(t *testing.T) {
	b, err := newV4L2Backend()
	if err != nil {
		t.Skipf("v4l2: no M2M video node present: %v", err)
	}
	if _, err := b.Probe(); err != nil {
		t.Fatalf("probe: %v", err)
	}
	// CodecUnknown is never supported by any node.
	_, derr := b.NewDecoder(NewConfig(WithCodec(video.CodecUnknown)))
	assert.Error(t, derr, "expected an error for an unsupported codec")

	// The policy walk publishes a HardwareFallbackEvent under PreferHardware
	// when the only backend cannot satisfy the codec. Drive it through the
	// registry with just this backend registered.
	reg := NewRegistry()
	reg.Register(b)
	bus := NewFallbackBus()
	var got []HardwareFallbackEvent
	bus.Subscribe(func(e HardwareFallbackEvent) { got = append(got, e) })
	p := Policy{Mode: PreferHardware, Bus: bus}
	// H264 may or may not be supported on this host; force an unsupported
	// codec so the walk always falls back.
	_, _ = p.OpenDecoder(reg, NewConfig(WithCodec(video.H264)))
	if !b.probeCaps.Supports(video.H264, Decode) {
		require.NotEmpty(t, got, "expected a HardwareFallbackEvent when H264 decode is unsupported")
		assert.Contains(t, got[0].Attempted, "v4l2")
		t.Logf("fallback event: attempted=%v reasons=%v fellBackTo=%q",
			got[0].Attempted, got[0].Reasons, got[0].FellBackTo)
	}
}

// splitAccessUnits splits an Annex-B HEVC elementary stream into access
// units: a new AU starts at the first VCL slice whose
// first_slice_segment_in_pic_flag is set, carrying along any preceding
// non-VCL NALs (VPS/SPS/PPS/SEI/AUD). This is the framing the per-AU
// stateless Decode expects (one coded picture per call).
func splitAccessUnits(data []byte) [][]byte {
	nals := splitAnnexBNALs(data)
	var aus [][]byte
	var cur []byte
	seenSliceInAU := false
	appendNAL := func(nal []byte) {
		// Re-prefix a 4-byte start code so the AU is a valid Annex-B stream
		// (the stateless decoder re-splits and strips start codes itself).
		cur = append(cur, 0, 0, 0, 1)
		cur = append(cur, nal...)
	}
	for _, nal := range nals {
		nt := nalUnitType(nal)
		isVCL := nt >= 0 && nt <= hevcNalCRANUT
		if isVCL {
			firstSlice := len(nal) >= 3 && nal[2]&0x80 != 0
			if firstSlice && seenSliceInAU {
				aus = append(aus, cur)
				cur = nil
				seenSliceInAU = false
			}
			seenSliceInAU = true
		} else if seenSliceInAU {
			// A non-VCL NAL after slices begins the next AU.
			aus = append(aus, cur)
			cur = nil
			seenSliceInAU = false
		}
		appendNAL(nal)
	}
	if len(cur) > 0 {
		aus = append(aus, cur)
	}
	return aus
}
