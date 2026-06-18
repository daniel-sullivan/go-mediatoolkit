//go:build darwin

package hwaccel

import (
	"encoding/binary"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/video"
)

// vtIVFFrames parses an IVF file into its coded frame payloads.
func vtIVFFrames(t *testing.T, data []byte) [][]byte {
	t.Helper()
	require.GreaterOrEqual(t, len(data), 32)
	require.Equal(t, "DKIF", string(data[0:4]))
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

// vtFfmpegIVF produces an IVF stream with ffmpeg for the given encoder.
func vtFfmpegIVF(t *testing.T, encoder string, extra ...string) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	tmp := t.TempDir() + "/ref.ivf"
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=30:duration=0.2",
		"-pix_fmt", "yuv420p", "-c:v", encoder, "-g", "1", "-frames:v", "3"}
	args = append(args, extra...)
	args = append(args, "-f", "ivf", tmp)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg %s failed (%v): %s", encoder, err, out)
	}
	data, err := os.ReadFile(tmp)
	require.NoError(t, err)
	return data
}

// TestVTAV1Decode decodes an ffmpeg libaom-av1 IVF stream through the
// VideoToolbox AV1 decoder (M3+ hardware) and asserts real pixels return.
func TestVTAV1Decode(t *testing.T) {
	b, err := newVTBackend()
	if err != nil {
		t.Skipf("videotoolbox unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.AV1, Decode) {
		t.Skip("AV1 hardware decode not supported on this host")
	}

	data := vtFfmpegIVF(t, "libaom-av1", "-cpu-used", "8")
	frames := vtIVFFrames(t, data)
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
	tail, err := dec.Flush()
	require.NoError(t, err)
	decoded = append(decoded, tail...)
	elapsed := time.Since(start)
	require.NotEmpty(t, decoded, "decoder produced no frames")

	f := decoded[0]
	assert.Equal(t, 320, f.Width)
	assert.Equal(t, 240, f.Height)
	require.GreaterOrEqual(t, len(f.Planes), 2)
	var nonzero int
	for _, v := range f.Planes[0] {
		if v != 0 {
			nonzero++
		}
	}
	assert.Greater(t, nonzero, len(f.Planes[0])/10, "decoded luma looks empty")
	t.Logf("vt av1 decode: %d frames -> %d decoded %dx%d fmt=%s, %v (%.0f fps)",
		len(frames), len(decoded), f.Width, f.Height, f.PixelFormat, elapsed,
		float64(len(decoded))/elapsed.Seconds())
}

// TestVTVP9Decode decodes a VP9 IVF stream when the host has a VP9 hardware
// decoder. Apple silicon has none, so this skips on M-series (the probe
// reports VP9 decode=false), exercising the graceful path.
func TestVTVP9Decode(t *testing.T) {
	b, err := newVTBackend()
	if err != nil {
		t.Skipf("videotoolbox unavailable: %v", err)
	}
	caps, err := b.Probe()
	require.NoError(t, err)
	if !caps.Supports(video.VP9, Decode) {
		t.Skip("VP9 hardware decode not supported on this host (expected on Apple silicon)")
	}

	data := vtFfmpegIVF(t, "libvpx-vp9", "-profile:v", "0")
	frames := vtIVFFrames(t, data)
	require.NotEmpty(t, frames)

	dec, err := b.NewDecoder(NewConfig(WithCodec(video.VP9)))
	require.NoError(t, err)
	defer dec.Close()

	var decoded []video.Frame
	for i, fr := range frames {
		out, err := dec.Decode(video.Packet{Codec: video.VP9, Data: fr, Keyframe: true})
		require.NoError(t, err, "decode frame %d", i)
		decoded = append(decoded, out...)
	}
	require.NotEmpty(t, decoded)
	t.Logf("vt vp9 decode: %d frames -> %d decoded", len(frames), len(decoded))
}
