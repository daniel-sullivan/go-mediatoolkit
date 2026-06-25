//go:build darwin

package hwaccel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// lumaMAE computes the mean absolute error between two luma planes of the
// same geometry, honouring each plane's stride. Both planes are treated
// as w×h visible regions; bytes beyond the visible width in a strided row
// are ignored.
func lumaMAE(t *testing.T, a []byte, aStride int, b []byte, bStride int, w, h int) float64 {
	t.Helper()
	var sum float64
	var n int
	for j := 0; j < h; j++ {
		ar := a[j*aStride : j*aStride+w]
		br := b[j*bStride : j*bStride+w]
		for i := 0; i < w; i++ {
			d := int(ar[i]) - int(br[i])
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

// TestVideoToolboxDecodeRoundTrip proves a full hardware transcode
// round-trip on this machine: synthesize raw NV12 frames -> hardware
// encode -> hardware decode -> assert the decoded frames recover the
// source geometry/format and correlate with the source luma (H.264/5 is
// lossy, so fidelity is checked via mean-absolute-error, not byte
// equality).
func TestVideoToolboxDecodeRoundTrip(t *testing.T) {
	b, err := newVTBackend()
	require.NoError(t, err)
	caps, err := b.Probe()
	require.NoError(t, err)

	cases := []struct {
		name  string
		codec video.Codec
	}{
		{"h264", video.H264},
		{"h265", video.H265},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !caps.Supports(tc.codec, Encode) {
				t.Skipf("%s hardware encode not supported on this machine", tc.codec)
			}
			if !caps.Supports(tc.codec, Decode) {
				t.Skipf("%s hardware decode not supported on this machine", tc.codec)
			}

			// --- encode source NV12 frames ---
			enc, err := b.NewEncoder(NewConfig(
				WithCodec(tc.codec),
				WithResolution(testWidth, testHeight),
				WithBitrate(8_000_000), // generous bitrate to keep MAE low
				WithFrameRate(30, 1),
				WithKeyframeInterval(15),
			))
			require.NoError(t, err)

			sources := make([]video.Frame, testFrames)
			var packets []video.Packet
			for i := 0; i < testFrames; i++ {
				f := makeNV12(testWidth, testHeight, i)
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

			// --- hardware decode the packets ---
			dec, err := b.NewDecoder(NewConfig(WithCodec(tc.codec)))
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

			// The decoder should recover (close to) every submitted frame.
			// VideoToolbox with AllowFrameReordering off and no B-frames
			// emits one decoded frame per coded picture.
			assert.GreaterOrEqual(t, len(decoded), testFrames-1,
				"expected ~%d decoded frames, got %d", testFrames, len(decoded))

			// --- geometry + format assertions ---
			for i, f := range decoded {
				assert.Equal(t, testWidth, f.Width, "frame %d width", i)
				assert.Equal(t, testHeight, f.Height, "frame %d height", i)
				assert.Equal(t, video.NV12, f.PixelFormat,
					"frame %d format (VideoToolbox decodes 4:2:0 to NV12)", i)
				require.GreaterOrEqual(t, len(f.Planes), 2, "frame %d planes", i)
				require.NotEmpty(t, f.Planes[0], "frame %d luma", i)
				require.GreaterOrEqual(t, f.Strides[0], testWidth, "frame %d luma stride", i)
			}

			// --- fidelity: decoded luma must correlate with the source ---
			// Compare the first decoded frame (an IDR) against source[0].
			// A keyframe at 8 Mbit/s on a 640x480 gradient should be near
			// lossless; assert a comfortably loose MAE bound.
			src0 := sources[0]
			dec0 := decoded[0]
			mae0 := lumaMAE(t, src0.Planes[0], src0.Strides[0], dec0.Planes[0], dec0.Strides[0], testWidth, testHeight)
			assert.Less(t, mae0, 12.0,
				"keyframe luma MAE too high (%.2f) — decode does not correlate with source", mae0)

			// Average MAE across all paired frames (decoded order matches
			// submit order with reordering disabled).
			var sumMAE float64
			pairs := len(decoded)
			if pairs > testFrames {
				pairs = testFrames
			}
			for i := 0; i < pairs; i++ {
				s := sources[i]
				dF := decoded[i]
				sumMAE += lumaMAE(t, s.Planes[0], s.Strides[0], dF.Planes[0], dF.Strides[0], testWidth, testHeight)
			}
			avgMAE := sumMAE / float64(pairs)
			assert.Less(t, avgMAE, 18.0,
				"average luma MAE too high (%.2f) across %d frames", avgMAE, pairs)

			fps := float64(len(decoded)) / elapsed.Seconds()
			t.Logf("%s round-trip: %d packets -> %d decoded frames %dx%d, "+
				"keyframe luma MAE=%.2f, avg luma MAE=%.2f, decode %v (%.0f fps)",
				tc.name, len(packets), len(decoded), testWidth, testHeight,
				mae0, avgMAE, elapsed, fps)
		})
	}
}
