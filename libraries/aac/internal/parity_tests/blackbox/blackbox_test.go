//go:build aac_blackbox

// Black-box parity: the pure-Go nativeaac AAC-LC port vs an UNMODIFIED
// upstream mstorsjo/fdk-aac v2.0.3 build. The C side is driven exclusively
// through two tiny CLIs invoked out-of-process — `aac-rawenc` and `aac-dec` —
// that run.sh compiles against the freshly-built upstream static lib. There is
// no cgo here and no linkage to any FDK symbol from this package: the script
// builds the upstream binaries, this test exec's them with temp-file WAV / raw
// access-unit streams, and compares against what the Go port produces from
// identical input. (The shipped `aac-enc` is not used — it hardcodes ADTS
// transport, which is not byte-comparable; see README.md.)
//
// FDK-AAC is FIXED-POINT, so the parity target is the strongest possible:
//   - encode: BYTE-IDENTICAL raw access units (raw TRANSMUX, no framing)
//   - decode: EXACT integer-equal int16 PCM
//
// There is no FP/FMA/ULP tolerance here by design. Any divergence is a real
// bug (or a config mismatch documented in README.md), never "rounding".
//
// Run via ./run.sh which sets AAC_ENC_BIN + AAC_DEC_BIN.
package blackbox

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

const (
	bbFrameLen = 1024 // AAC-LC frame length (samples/channel)
	bbNFrames  = 12
)

const pi = math.Pi

func sin(x float64) float64 { return math.Sin(x) }

// durationFor returns the time.Duration that yields exactly `samples` mono
// samples at `rate` (used to size the generators.* helpers).
func durationFor(samples, rate int) time.Duration {
	return time.Duration(samples) * time.Second / time.Duration(rate)
}

// ---- CLI locators --------------------------------------------------------

func aacEncPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("AAC_ENC_BIN")
	if p == "" {
		t.Skip("AAC_ENC_BIN not set — run via ./run.sh")
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("AAC_ENC_BIN=%s not accessible: %v", p, err)
	}
	return p
}

func aacDecPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("AAC_DEC_BIN")
	if p == "" {
		t.Skip("AAC_DEC_BIN not set — run via ./run.sh")
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("AAC_DEC_BIN=%s not accessible: %v", p, err)
	}
	return p
}

// ---- Synthetic PCM -------------------------------------------------------

// buildPCM builds a deterministic multi-tone interleaved int16 signal so the
// encoder exercises real M/S, sectioning and TNS rather than a trivial tone.
// This is the SAME waveform the encode-e2e / decode-e2e cgo parity slices use,
// so the black-box result is directly comparable to the in-tree oracle.
func buildPCM(nFrames, frameLen, channels, rate int) []int16 {
	pcm := make([]int16, nFrames*frameLen*channels)
	for n := 0; n < nFrames*frameLen; n++ {
		t0 := float64(n) / float64(rate)
		s := 0.5*sin(2*pi*440*t0) +
			0.25*sin(2*pi*1500*t0) +
			0.15*sin(2*pi*5000*t0)
		l := int16(s * 26000)
		for c := 0; c < channels; c++ {
			v := l
			if c == 1 {
				v = int16((s*0.8 + 0.1*sin(2*pi*880*t0)) * 26000)
			}
			pcm[n*channels+c] = v
		}
	}
	return pcm
}

// pinkPCM builds an interleaved int16 pink-noise signal from the shared
// generators package (generators.* return mutations.Audio; .Data is []float64).
// Demonstrates the generators wiring requested by the task; it stresses the
// rate-control loop with a broadband spectrum.
func pinkPCM(nFrames, frameLen, channels, rate int, seed int64) []int16 {
	total := nFrames * frameLen
	mono := generators.PinkNoise(durationFor(total, rate), rate, seed).Data
	if len(mono) < total {
		// PinkNoise rounds to the nearest sample; pad with zeros if short.
		mono = append(mono, make([]float64, total-len(mono))...)
	}
	pcm := make([]int16, total*channels)
	for n := 0; n < total; n++ {
		v := mono[n]
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		s := int16(v * 0.9 * 32767)
		for c := 0; c < channels; c++ {
			pcm[n*channels+c] = s
		}
	}
	return pcm
}

// ---- WAV writer (16-bit PCM) ---------------------------------------------

func writeWAV(t *testing.T, path string, pcm []int16, channels, rate int) {
	t.Helper()
	dataBytes := len(pcm) * 2
	byteRate := rate * channels * 2
	blockAlign := channels * 2

	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataBytes))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16)) // fmt chunk size
	binary.Write(&b, binary.LittleEndian, uint16(1))  // PCM
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(byteRate))
	binary.Write(&b, binary.LittleEndian, uint16(blockAlign))
	binary.Write(&b, binary.LittleEndian, uint16(16)) // bits per sample
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(dataBytes))
	for _, s := range pcm {
		binary.Write(&b, binary.LittleEndian, s)
	}
	require.NoError(t, os.WriteFile(path, b.Bytes(), 0o644))
}

// ---- length-prefixed AU framing ------------------------------------------

// readLengthPrefixedAUs parses the raw access-unit stream emitted by our
// out-of-tree aac-rawenc CLI: a sequence of [4-byte big-endian AU length][AU
// bytes]. This is the SAME raw transmux (TT_MP4_RAW) bitstream the in-tree cgo
// encode oracle produces and the nativeaac encoder emits per frame, so the AUs
// are directly byte-comparable (no ADTS header to strip).
func readLengthPrefixedAUs(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var aus [][]byte
	for off := 0; off+4 <= len(data); {
		l := int(binary.BigEndian.Uint32(data[off : off+4]))
		off += 4
		require.LessOrEqualf(t, off+l, len(data),
			"AU at %d claims len %d but only %d bytes remain", off-4, l, len(data)-off)
		aus = append(aus, append([]byte(nil), data[off:off+l]...))
		off += l
	}
	return aus
}

// ---- aac-rawenc / aac-dec drivers ----------------------------------------

// runAacEnc invokes: aac-rawenc <bitrate> in.wav out.aus out.asc
//
// aac-rawenc is our out-of-tree CLI that drives the pristine upstream
// libfdk-aac with the EXACT oracle config: AOT 2 (AAC-LC), BITRATEMODE 0 (CBR),
// TRANSMUX 0 (raw AUs, no ADTS framing), afterburner default 0, no
// CHANNELORDER override. It writes a length-prefixed AU stream + the ASC
// sidecar. (The shipped `aac-enc` cannot be used: it hardcodes ADTS, which
// shifts the CBR bit budget by the 7-byte header and yields a different
// bitstream by construction — see README.)
func runAacEnc(t *testing.T, enc string, bitrate int, inWAV, outAUs, outASC string) {
	t.Helper()
	cmd := exec.Command(enc, fmt.Sprintf("%d", bitrate), inWAV, outAUs, outASC)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoErrorf(t, cmd.Run(), "aac-rawenc failed:\n%s", stderr.String())
}

// runAacDec invokes our out-of-tree: aac-dec in.asc in.aus out.pcm
// (TT_MP4_RAW + ConfigRaw, PCM limiter disabled, int16 LE PCM out — mirrors the
// cgo decode oracle).
func runAacDec(t *testing.T, dec, inASC, inAUs, outPCM string) {
	t.Helper()
	cmd := exec.Command(dec, inASC, inAUs, outPCM)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoErrorf(t, cmd.Run(), "aac-dec failed:\n%s", stderr.String())
}

func readInt16PCM(t *testing.T, path string) []int16 {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Zero(t, len(data)%2, "%s has odd length", path)
	out := make([]int16, len(data)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
	}
	return out
}

// ---- Go-side helpers -----------------------------------------------------

func nativeEncodeAUs(t *testing.T, rate, channels, bitrate int, pcm []int16) [][]byte {
	t.Helper()
	enc, encErr := nativeaac.NewEncoder(rate, channels, bitrate)
	require.Equal(t, nativeaac.AacEncOK, encErr, "nativeaac.NewEncoder failed")
	require.Equal(t, bbFrameLen, enc.FrameLength(), "unexpected native frame length")
	per := bbFrameLen * channels
	framesIn := len(pcm) / per
	aus := make([][]byte, 0, framesIn)
	for f := 0; f < framesIn; f++ {
		au, e := enc.EncodeOneFrame(pcm[f*per : (f+1)*per])
		require.Equalf(t, nativeaac.AacEncOK, e, "native EncodeOneFrame frame %d failed", f)
		aus = append(aus, au)
	}
	return aus
}

func nativeDecodePCM(t *testing.T, rate, channels int, aus [][]byte) []int16 {
	t.Helper()
	dec, err := nativeaac.NewDecoder(bbFrameLen, uint32(rate), channels)
	require.NoError(t, err)
	out := make([]int16, 0, len(aus)*bbFrameLen*channels)
	for i, au := range aus {
		buf := make([]int16, bbFrameLen*channels)
		n, err := dec.DecodeAccessUnit(au, buf)
		require.NoErrorf(t, err, "native decode of AU %d failed", i)
		require.Equal(t, bbFrameLen, n)
		out = append(out, buf...)
	}
	return out
}

// ---- Config matrix -------------------------------------------------------

type cfg struct {
	name     string
	kind     string // "tone" | "pink"
	channels int
	rate     int
	bitrate  int
}

func matrix() []cfg {
	return []cfg{
		{"tone_mono_44100_128k", "tone", 1, 44100, 128000},
		{"tone_stereo_44100_128k", "tone", 2, 44100, 128000},
		{"tone_stereo_48000_128k", "tone", 2, 48000, 128000},
		{"tone_mono_48000_96k", "tone", 1, 48000, 96000},
		{"pink_mono_44100_128k", "pink", 1, 44100, 128000},
		{"pink_stereo_48000_128k", "pink", 2, 48000, 128000},
	}
}

func (c cfg) pcm() []int16 {
	if c.kind == "pink" {
		return pinkPCM(bbNFrames, bbFrameLen, c.channels, c.rate, 20260618)
	}
	return buildPCM(bbNFrames, bbFrameLen, c.channels, c.rate)
}

// ---- Tests ---------------------------------------------------------------

// TestBlackBox_EncodeParity is the strongest gate: encode identical PCM via the
// out-of-tree aac-rawenc CLI (which drives the pristine upstream libfdk-aac at
// the exact oracle config: AOT 2, CBR, TRANSMUX 0 raw AUs, afterburner default)
// and via the pure-Go nativeaac encoder, and assert the raw access units are
// BYTE-IDENTICAL AU-for-AU. fdk-aac encode is fixed-point, so a matched CBR
// config reproduces the bitstream exactly — no tolerance.
//
// There is NO leading priming offset: with raw transmux fed frame-by-frame the
// FDK encoder emits AU 0 from cold state, just like the native encoder. The CLI
// does emit ONE extra trailing AU — the final aacEncEncode(numInSamples=-1)
// EOF flush — which the per-frame native driver never produces, so we compare
// only the common-prefix AUs (the native count).
func TestBlackBox_EncodeParity(t *testing.T) {
	enc := aacEncPath(t)
	for _, c := range matrix() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pcm := c.pcm()
			dir := t.TempDir()
			inWAV := filepath.Join(dir, "in.wav")
			outAUs := filepath.Join(dir, "c.aus")
			outASC := filepath.Join(dir, "c.asc")
			writeWAV(t, inWAV, pcm, c.channels, c.rate)

			runAacEnc(t, enc, c.bitrate, inWAV, outAUs, outASC)
			raw, err := os.ReadFile(outAUs)
			require.NoError(t, err)
			cAUs := readLengthPrefixedAUs(t, raw)
			require.NotEmpty(t, cAUs, "aac-rawenc produced no access units")

			goAUs := nativeEncodeAUs(t, c.rate, c.channels, c.bitrate, pcm)
			require.NotEmpty(t, goAUs, "native encoder produced no access units")

			// Compare the common-prefix AUs (native emits one AU per frame, the
			// CLI emits those plus one trailing EOF-flush AU).
			n := len(goAUs)
			if len(cAUs) < n {
				n = len(cAUs)
			}
			require.Greater(t, n, 0, "no overlapping AUs to compare")
			for i := 0; i < n; i++ {
				require.Equalf(t, cAUs[i], goAUs[i],
					"AU %d not byte-identical (aac-rawenc len=%d vs native len=%d): "+
						"the fixed-point CBR bitstream must match exactly",
					i, len(cAUs[i]), len(goAUs[i]))
			}
			t.Logf("%d AUs byte-exact vs aac-rawenc (CLI emitted %d incl. EOF flush)", n, len(cAUs))
		})
	}
}

// TestBlackBox_DecodeParity encodes PCM via the out-of-tree aac-rawenc CLI,
// then decodes the resulting raw-AU stream via BOTH the out-of-tree aac-dec CLI
// (TT_MP4_RAW + ConfigRaw, PCM limiter disabled) and the pure-Go nativeaac
// decoder (fed the same raw AUs), and asserts the int16 PCM streams are EXACTLY
// equal. fdk-aac decode is fixed-point, so this is exact integer equality, no
// tolerance.
//
// The FDK decoder carries a one-frame output priming delay (its first emitted
// frame is the priming frame; decode(AU[k]) surfaces as the reference's frame
// k+1), whereas DecodeAccessUnit returns decode(AU[k]) directly — the exact
// relationship the in-tree decode-e2e oracle documents (refDelay=1). We
// discover the lead offset by matching (≤ 3 frames) and compare the aligned
// overlap with strict equality.
func TestBlackBox_DecodeParity(t *testing.T) {
	enc := aacEncPath(t)
	dec := aacDecPath(t)
	for _, c := range matrix() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pcm := c.pcm()
			dir := t.TempDir()
			inWAV := filepath.Join(dir, "in.wav")
			outAUs := filepath.Join(dir, "c.aus")
			outASC := filepath.Join(dir, "c.asc")
			outPCM := filepath.Join(dir, "c.pcm")
			writeWAV(t, inWAV, pcm, c.channels, c.rate)

			runAacEnc(t, enc, c.bitrate, inWAV, outAUs, outASC)
			runAacDec(t, dec, outASC, outAUs, outPCM)
			cPCM := readInt16PCM(t, outPCM)
			require.NotEmpty(t, cPCM, "aac-dec produced no PCM")

			raw, err := os.ReadFile(outAUs)
			require.NoError(t, err)
			aus := readLengthPrefixedAUs(t, raw)
			goPCM := nativeDecodePCM(t, c.rate, c.channels, aus)
			require.NotEmpty(t, goPCM, "native decoder produced no PCM")

			frameSamples := bbFrameLen * c.channels
			off := alignPCM(cPCM, goPCM, frameSamples)
			require.GreaterOrEqualf(t, off, 0,
				"no integer-exact frame alignment found between aac-dec (%d samples) and native (%d samples)",
				len(cPCM), len(goPCM))

			cAligned := cPCM[off*frameSamples:]
			n := len(cAligned)
			if len(goPCM) < n {
				n = len(goPCM)
			}
			require.Greater(t, n, 0, "no overlapping PCM to compare")
			require.Equalf(t, cAligned[:n], goPCM[:n],
				"decoded int16 PCM not exactly equal (CLI frame offset=%d): "+
					"fixed-point decode must be integer-identical", off)
			t.Logf("%d samples integer-exact vs aac-dec (CLI frame offset=%d)", n, off)
		})
	}
}

// alignPCM finds the smallest leading-frame offset k in [0,3] such that
// c[k*frameSamples:] is integer-equal to g over the common length. Returns -1
// if none. This absorbs the FDK decoder's priming-delay frame(s) without
// weakening the equality assertion.
func alignPCM(c, g []int16, frameSamples int) int {
	maxOff := 3
	for k := 0; k <= maxOff; k++ {
		base := k * frameSamples
		if base >= len(c) {
			break
		}
		ca := c[base:]
		n := len(ca)
		if len(g) < n {
			n = len(g)
		}
		if n < frameSamples {
			continue
		}
		if bytes16Equal(ca[:n], g[:n]) {
			return k
		}
	}
	return -1
}

func bytes16Equal(a, b []int16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
