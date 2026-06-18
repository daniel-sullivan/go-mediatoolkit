//go:build darwin

package hwaccel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/video"
)

const (
	testWidth  = 640
	testHeight = 480
	testFrames = 30
)

// makeNV12 synthesises a single NV12 frame with a moving diagonal
// gradient so successive frames differ (giving the encoder real motion
// to compress rather than a constant image).
func makeNV12(w, h, t int) video.Frame {
	y := make([]byte, w*h)
	cbcr := make([]byte, w*(h/2)) // interleaved Cb/Cr: w bytes per row, h/2 rows
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			y[j*w+i] = byte((i + j + t*4) & 0xff)
		}
	}
	for j := 0; j < h/2; j++ {
		for i := 0; i < w; i++ {
			cbcr[j*w+i] = byte((i*2 + t*3) & 0xff)
		}
	}
	return video.Frame{
		PixelFormat: video.NV12,
		Width:       w,
		Height:      h,
		Planes:      [][]byte{y, cbcr},
		Strides:     []int{w, w},
		PTS:         time.Duration(t) * time.Second / 30,
	}
}

// makeI420 synthesises a single I420 (3-plane) frame.
func makeI420(w, h, t int) video.Frame {
	y := make([]byte, w*h)
	cb := make([]byte, (w/2)*(h/2))
	cr := make([]byte, (w/2)*(h/2))
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			y[j*w+i] = byte((i ^ j ^ (t * 5)) & 0xff)
		}
	}
	for j := 0; j < h/2; j++ {
		for i := 0; i < w/2; i++ {
			cb[j*(w/2)+i] = byte((i + t) & 0xff)
			cr[j*(w/2)+i] = byte((j + t) & 0xff)
		}
	}
	return video.Frame{
		PixelFormat: video.I420,
		Width:       w,
		Height:      h,
		Planes:      [][]byte{y, cb, cr},
		Strides:     []int{w, w / 2, w / 2},
		PTS:         time.Duration(t) * time.Second / 30,
	}
}

// annexBNALTypes splits an Annex-B elementary stream on 3- and 4-byte
// start codes and returns the codec-specific NAL type of each unit.
// For H.264 the type is nal[0]&0x1f; for H.265 it is (nal[0]>>1)&0x3f.
func annexBNALTypes(t *testing.T, data []byte, codec video.Codec) []int {
	t.Helper()
	var types []int
	i := 0
	for i < len(data) {
		// find a start code at i
		sc := 0
		if i+4 <= len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			sc = 4
		} else if i+3 <= len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			sc = 3
		} else {
			i++
			continue
		}
		nalStart := i + sc
		// find next start code
		j := nalStart
		for j < len(data) {
			if j+3 <= len(data) && data[j] == 0 && data[j+1] == 0 && (data[j+2] == 1 ||
				(j+4 <= len(data) && data[j+2] == 0 && data[j+3] == 1)) {
				break
			}
			j++
		}
		if nalStart < len(data) {
			b0 := data[nalStart]
			if codec == video.H265 {
				types = append(types, int((b0>>1)&0x3f))
			} else {
				types = append(types, int(b0&0x1f))
			}
		}
		i = j
	}
	return types
}

func containsAny(types []int, want ...int) bool {
	set := map[int]bool{}
	for _, ty := range types {
		set[ty] = true
	}
	for _, w := range want {
		if set[w] {
			return true
		}
	}
	return false
}

// TestVideoToolboxProbe reports the hardware capabilities of this
// machine. Informational — it never fails (a Mac without an encoder is
// still a valid result), but it gates the encode subtests via the
// returned capabilities.
func TestVideoToolboxProbe(t *testing.T) {
	b, err := newVTBackend()
	require.NoError(t, err, "VideoToolbox frameworks must dlopen on macOS")
	require.True(t, b.Available())

	caps, err := b.Probe()
	require.NoError(t, err)
	t.Logf("videotoolbox capabilities on this machine:")
	for _, c := range caps.Codecs {
		t.Logf("  %-5s encode=%-5v decode=%-5v profiles=%v", c.Codec, c.Encode, c.Decode, c.Profiles)
	}
	assert.True(t, caps.Supports(video.H264, Encode), "H.264 hardware encode expected on Apple Silicon")
}

func TestVideoToolboxEncode(t *testing.T) {
	b, err := newVTBackend()
	require.NoError(t, err)
	caps, err := b.Probe()
	require.NoError(t, err)

	cases := []struct {
		name  string
		codec video.Codec
		frame func(w, h, t int) video.Frame
	}{
		{"h264_nv12", video.H264, makeNV12},
		{"h264_i420", video.H264, makeI420},
		{"h265_nv12", video.H265, makeNV12},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !caps.Supports(tc.codec, Encode) {
				t.Skipf("%s hardware encode not supported on this machine", tc.codec)
			}
			enc, err := b.NewEncoder(NewConfig(
				WithCodec(tc.codec),
				WithResolution(testWidth, testHeight),
				WithBitrate(2_000_000),
				WithFrameRate(30, 1),
				WithKeyframeInterval(15),
			))
			require.NoError(t, err)
			defer enc.Close()

			var all []video.Packet
			start := time.Now()
			for i := 0; i < testFrames; i++ {
				pkts, err := enc.Encode(tc.frame(testWidth, testHeight, i))
				require.NoError(t, err, "Encode frame %d", i)
				all = append(all, pkts...)
			}
			tail, err := enc.Flush()
			require.NoError(t, err)
			all = append(all, tail...)
			elapsed := time.Since(start)

			require.NotEmpty(t, all, "encoder produced no packets")

			// Every packet must carry bytes and the right codec; the
			// first packet must be a keyframe and start with a NAL
			// start code.
			var totalBytes int
			var keyframes int
			for _, p := range all {
				assert.Equal(t, tc.codec, p.Codec)
				assert.NotEmpty(t, p.Data)
				totalBytes += len(p.Data)
				if p.Keyframe {
					keyframes++
				}
			}
			require.GreaterOrEqual(t, keyframes, 1, "expected at least one keyframe")
			assert.True(t, all[0].Keyframe, "first packet should be a keyframe")
			// With a 15-frame GOP over 30 frames we expect roughly two
			// keyframes — and crucially NOT all of them (that would mean
			// the sync-sample attachment is being misread and every
			// P-frame is being flagged + bloated with parameter sets).
			assert.Less(t, keyframes, testFrames,
				"not every frame should be a keyframe (GOP=15); got %d/%d", keyframes, testFrames)
			require.GreaterOrEqual(t, len(all[0].Data), 4)
			assert.Equal(t, startCode, all[0].Data[:4], "stream must begin with a 4-byte Annex-B start code")

			// Parse NAL units across the whole stream and assert the
			// parameter sets + an IDR slice are present.
			types := annexBNALTypes(t, concatPackets(all), tc.codec)
			require.NotEmpty(t, types)
			if tc.codec == video.H264 {
				// 7=SPS, 8=PPS, 5=IDR slice.
				assert.True(t, containsAny(types, 7), "H.264 SPS (NAL 7) present; got %v", types)
				assert.True(t, containsAny(types, 8), "H.264 PPS (NAL 8) present; got %v", types)
				assert.True(t, containsAny(types, 5), "H.264 IDR slice (NAL 5) present; got %v", types)
			} else {
				// 32=VPS, 33=SPS, 34=PPS, 19/20=IDR slices.
				assert.True(t, containsAny(types, 32), "H.265 VPS (NAL 32) present; got %v", types)
				assert.True(t, containsAny(types, 33), "H.265 SPS (NAL 33) present; got %v", types)
				assert.True(t, containsAny(types, 34), "H.265 PPS (NAL 34) present; got %v", types)
				assert.True(t, containsAny(types, 19, 20), "H.265 IDR slice (NAL 19/20) present; got %v", types)
			}

			fps := float64(testFrames) / elapsed.Seconds()
			t.Logf("%s: %d frames %dx%d -> %d packets, %d bytes, %v (%.0f fps), %d keyframes",
				tc.name, testFrames, testWidth, testHeight, len(all), totalBytes, elapsed, fps, keyframes)
		})
	}
}

func concatPackets(pkts []video.Packet) []byte {
	var out []byte
	for _, p := range pkts {
		out = append(out, p.Data...)
	}
	return out
}
