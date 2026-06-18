package opus

import (
	"encoding/binary"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readTestVector reads a .pkt file (length-prefixed packets) and returns the packets.
func readTestVector(t *testing.T, pktPath string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(pktPath)
	if err != nil {
		t.Skipf("test vector not found: %s (run: cd testdata && go run gen_vectors.go)", pktPath)
		return nil
	}
	var packets [][]byte
	for len(data) >= 4 {
		pktLen := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		if pktLen > len(data) {
			break
		}
		packets = append(packets, data[:pktLen])
		data = data[pktLen:]
	}
	return packets
}

// readReferencePCM reads a .pcm file (float32 LE samples) and returns float64 slices per frame.
func readReferencePCM(t *testing.T, pcmPath string, samplesPerFrame int) [][]float64 {
	t.Helper()
	data, err := os.ReadFile(pcmPath)
	if err != nil {
		t.Skipf("reference PCM not found: %s", pcmPath)
		return nil
	}
	nSamples := len(data) / 4
	var frames [][]float64
	for off := 0; off+samplesPerFrame*4 <= len(data); off += samplesPerFrame * 4 {
		frame := make([]float64, samplesPerFrame)
		for i := 0; i < samplesPerFrame; i++ {
			bits := binary.LittleEndian.Uint32(data[off+i*4:])
			frame[i] = float64(math.Float32frombits(bits))
		}
		frames = append(frames, frame)
	}
	_ = nSamples
	return frames
}

func TestDecodeVectorCELT20ms(t *testing.T) {
	packets := readTestVector(t, "testdata/celt_mono_48k_20ms.pkt")
	refFrames := readReferencePCM(t, "testdata/celt_mono_48k_20ms.pcm", 960)
	require.NotEmpty(t, packets)
	require.Equal(t, len(packets), len(refFrames))

	dec, err := NewDecoder(48000, 1)
	require.NoError(t, err)

	for f, pkt := range packets {
		pcm := make([]float64, MaxFrameSize(48000))
		n, err := dec.Decode(pkt, pcm)
		if err != nil {
			t.Logf("frame %d decode error: %v", f, err)
			continue
		}
		assert.Equal(t, 960, n, "frame %d: wrong sample count", f)

		// Compare against reference (skip first frame which may differ due to state).
		if f > 0 && n == 960 {
			maxErr := 0.0
			for i := 0; i < n; i++ {
				diff := math.Abs(pcm[i] - refFrames[f][i])
				if diff > maxErr {
					maxErr = diff
				}
			}
			t.Logf("frame %d: max error = %e", f, maxErr)
		}
	}
}

func TestDecodeVectorCELT10ms(t *testing.T) {
	packets := readTestVector(t, "testdata/celt_mono_48k_10ms.pkt")
	if len(packets) == 0 {
		return
	}

	dec, err := NewDecoder(48000, 1)
	require.NoError(t, err)

	for f, pkt := range packets {
		pcm := make([]float64, MaxFrameSize(48000))
		n, err := dec.Decode(pkt, pcm)
		if err != nil {
			t.Logf("frame %d decode error: %v", f, err)
			continue
		}
		assert.Equal(t, 480, n, "frame %d: wrong sample count", f)
	}
}

func TestDecodeVectorSILK20ms(t *testing.T) {
	packets := readTestVector(t, "testdata/silk_mono_48k_20ms.pkt")
	if len(packets) == 0 {
		return
	}

	dec, err := NewDecoder(48000, 1)
	require.NoError(t, err)

	for f, pkt := range packets {
		pcm := make([]float64, MaxFrameSize(48000))
		n, err := dec.Decode(pkt, pcm)
		if err != nil {
			t.Logf("frame %d decode error: %v (n=%d)", f, err, n)
			continue
		}
		var maxAmp float64
		for i := 0; i < n; i++ {
			if math.Abs(pcm[i]) > maxAmp {
				maxAmp = math.Abs(pcm[i])
			}
		}
		t.Logf("frame %d: decoded %d samples, max amplitude=%.6f", f, n, maxAmp)
	}
}

func TestSILKRawDecode(t *testing.T) {
	packets := readTestVector(t, "testdata/silk_mono_48k_20ms.pkt")
	if len(packets) == 0 {
		return
	}
	// Decode at 16kHz (internal rate = API rate, no resampling).
	dec, _ := NewDecoder(16000, 1)
	for f, pkt := range packets {
		pcm := make([]float64, MaxFrameSize(16000))
		n, err := dec.Decode(pkt, pcm)
		if err != nil {
			t.Logf("frame %d: error %v", f, err)
			continue
		}
		var maxAmp float64
		for i := 0; i < n; i++ {
			if math.Abs(pcm[i]) > maxAmp {
				maxAmp = math.Abs(pcm[i])
			}
		}
		t.Logf("frame %d: %d samples at 16kHz, max amp=%.6f", f, n, maxAmp)
	}
}

func TestParseVectorPackets(t *testing.T) {
	// Verify we can parse the generated packets correctly.
	for _, name := range []string{
		"testdata/celt_mono_48k_20ms.pkt",
		"testdata/celt_mono_48k_10ms.pkt",
		"testdata/silk_mono_48k_20ms.pkt",
	} {
		packets := readTestVector(t, name)
		if len(packets) == 0 {
			continue
		}
		t.Run(name, func(t *testing.T) {
			for f, pkt := range packets {
				info, err := ParsePacket(pkt)
				require.NoError(t, err, "frame %d", f)
				t.Logf("frame %d: mode=%s bw=%s dur=%.1fms stereo=%v frames=%d",
					f, info.Mode, info.Bandwidth, info.FrameDuration, info.Stereo, info.FrameCount)
			}
		})
	}
}
