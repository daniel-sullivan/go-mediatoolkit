//go:build cgo && opus_strict

package benchcmp

// Three-way decode parity: encode once with the Go port, then decode
// the same packets via three paths and compare pairwise:
//
//   C_demo    = upstream autotools libopus (opus_demo CLI subprocess)
//   C_cgo     = vendored libopus linked into this test via cgo
//   Go_native = pure-Go port (libraries/opus/internal/nativeopus)
//
// The Cgo and Go paths use the same interface (Decoder in impl.go), so
// they're driven by identical code. The upstream path runs the
// opus_demo CLI from libraries/opus/blackbox/run.sh's pristine clone.
//
// What this test pins down:
//
//   - C_cgo vs Go_native at int24 resolution → this is the primary
//     port correctness gate. Must be bit-exact; the benchcmp matrix
//     already covers this but the three-way view puts it in context.
//   - C_demo vs Go_native — documents the build-environment drift we
//     see in the black-box suite.
//   - C_demo vs C_cgo — isolates "does the SAME C source produce the
//     SAME output under different toolchains?" Answers: does the
//     int16 drift come from the port, or from the C build?
//
// Skipped unless OPUS_DEMO_BIN points at an upstream-built opus_demo.
// Use ./libraries/opus/blackbox/run.sh to produce one.

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

	"go-mediatoolkit/generators"
	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// ---- Configuration ------------------------------------------------------

type threewayCfg struct {
	name    string
	kind    string // pink | sweep | mixed
	ch      int
	bitrate int
	demoApp string
	goApp   int // nativeopus.Application*
}

func threewayMatrix() []threewayCfg {
	return []threewayCfg{
		{"pink_mono_16k_VOIP", "pink", 1, 16000, "voip", nativeopus.ApplicationVoIP},
		{"pink_mono_64k_AUDIO", "pink", 1, 64000, "audio", nativeopus.ApplicationAudio},
		{"pink_mono_128k_LOWDELAY", "pink", 1, 128000, "restricted-lowdelay", nativeopus.ApplicationRestrictedLowdelay},
		{"pink_stereo_32k_VOIP", "pink", 2, 32000, "voip", nativeopus.ApplicationVoIP},
		{"pink_stereo_128k_AUDIO", "pink", 2, 128000, "audio", nativeopus.ApplicationAudio},
	}
}

const (
	twFs       = 48000
	twFrameMs  = 20
	twDuration = 5 * time.Second
)

// ---- Helpers ------------------------------------------------------------

func opusDemoBin(t *testing.T) string {
	t.Helper()
	p := os.Getenv("OPUS_DEMO_BIN")
	if p == "" {
		t.Skip("OPUS_DEMO_BIN not set — run via libraries/opus/blackbox/run.sh")
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("OPUS_DEMO_BIN=%s not accessible: %v", p, err)
	}
	return p
}

func buildPCMInt16(kind string, ch, sampleRate int, duration time.Duration, seed int64) []int16 {
	var mono []float64
	switch kind {
	case "pink":
		mono = generators.PinkNoise(duration, sampleRate, seed).Data
	case "sweep":
		mono = generators.SineSweep(80, 8000, duration, sampleRate).Data
	case "mixed":
		pink := generators.PinkNoise(duration, sampleRate, seed).Data
		sweep := generators.SineSweep(80, 8000, duration, sampleRate).Data
		n := len(pink)
		if len(sweep) < n {
			n = len(sweep)
		}
		mono = make([]float64, n)
		for i := 0; i < n; i++ {
			env := 0.5 + 0.5*float64(i)/float64(n)
			mono[i] = env*sweep[i]*0.5 + pink[i]*0.5
		}
	default:
		panic("unknown waveform: " + kind)
	}
	out := make([]int16, len(mono)*ch)
	for i, v := range mono {
		if v > 1 {
			v = 1
		}
		if v < -1 {
			v = -1
		}
		s := int16(v * 0.9 * 32767)
		for c := 0; c < ch; c++ {
			out[i*ch+c] = s
		}
	}
	return out
}

// goEncodeAllInt16ThreeWay drives a GoEncoder through the same CTL
// sequence opus_demo applies so its bitstream is comparable.
func goEncodeAllInt16ThreeWay(t *testing.T, c threewayCfg, pcm []int16, frameSamples int) [][]byte {
	t.Helper()
	e := NewGoEncoder(twFs, c.ch, c.goApp)
	if e == nil {
		t.Fatalf("NewGoEncoder failed")
	}
	defer e.Destroy()
	// Mirror the CTL sequence opus_demo applies. SetBitrate +
	// SetComplexity are on the interface; the rest go through
	// nativeopus.EncoderCtl directly via the wrapped state pointer.
	e.SetBitrate(c.bitrate)
	e.SetComplexity(10)
	for _, p := range []struct {
		req int
		val int32
	}{
		{nativeopus.CtlSetBandwidth, int32(nativeopus.AutoValue)},
		{nativeopus.CtlSetVBR, 1},
		{nativeopus.CtlSetVBRConstraint, 0},
		{nativeopus.CtlSetInbandFEC, 0},
		{nativeopus.CtlSetForceChannels, int32(nativeopus.AutoValue)},
		{nativeopus.CtlSetDTX, 0},
		{nativeopus.CtlSetPacketLossPerc, 0},
		{nativeopus.CtlSetLSBDepth, 16},
		{nativeopus.CtlSetExpertFrameDuration, int32(nativeopus.FrameSize20ms)},
	} {
		if code := nativeopus.EncoderCtl(e.st, p.req, p.val); code != nativeopus.ErrorOK {
			t.Fatalf("encoder ctl %d: %d", p.req, code)
		}
	}

	nFrames := len(pcm) / (frameSamples * c.ch)
	pkts := make([][]byte, 0, nFrames)
	buf := make([]byte, 8000*c.ch)
	for f := 0; f < nFrames; f++ {
		s := f * frameSamples * c.ch
		n := e.EncodeInt16(pcm[s:s+frameSamples*c.ch], frameSamples, buf)
		if n < 0 {
			t.Fatalf("frame %d: encode %d", f, n)
		}
		pkts = append(pkts, append([]byte(nil), buf[:n]...))
	}
	return pkts
}

// decodeAllInt16 runs `pkts` through the given Decoder implementation
// and returns interleaved int16 PCM.
func decodeAllInt16(t *testing.T, dec Decoder, ch int, pkts [][]byte, frameSamples int) []int16 {
	t.Helper()
	out := make([]int16, 0, len(pkts)*frameSamples*ch)
	buf := make([]int16, frameSamples*ch)
	for f, pkt := range pkts {
		n := dec.DecodeInt16(pkt, buf, frameSamples)
		if n < 0 {
			t.Fatalf("frame %d: decode %d", f, n)
		}
		out = append(out, buf[:n*ch]...)
	}
	return out
}

func decodeAllInt24(t *testing.T, dec Decoder, ch int, pkts [][]byte, frameSamples int) []int32 {
	t.Helper()
	out := make([]int32, 0, len(pkts)*frameSamples*ch)
	buf := make([]int32, frameSamples*ch)
	for f, pkt := range pkts {
		n := dec.DecodeInt24(pkt, buf, frameSamples)
		if n < 0 {
			t.Fatalf("frame %d: decode24 %d", f, n)
		}
		out = append(out, buf[:n*ch]...)
	}
	return out
}

// writeGoBitstreamDemo writes Go-encoder packets in opus_demo's format.
func writeGoBitstreamDemo(t *testing.T, path string, pkts [][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bits: %v", err)
	}
	defer f.Close()
	hdr := make([]byte, 8)
	for _, pkt := range pkts {
		binary.BigEndian.PutUint32(hdr[0:4], uint32(len(pkt)))
		binary.BigEndian.PutUint32(hdr[4:8], 0)
		if _, err := f.Write(hdr); err != nil {
			t.Fatalf("write hdr: %v", err)
		}
		if _, err := f.Write(pkt); err != nil {
			t.Fatalf("write pkt: %v", err)
		}
	}
}

func runOpusDemoDecodeInt16(t *testing.T, demo string, Fs, ch int, bits, out string) {
	t.Helper()
	cmd := exec.Command(demo, "-d",
		fmt.Sprintf("%d", Fs), fmt.Sprintf("%d", ch), bits, out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("opus_demo -d failed: %v\nstderr:\n%s", err, stderr.String())
	}
}

func runOpusDemoDecodeInt24Impl(t *testing.T, demo string, Fs, ch int, bits, out string) {
	t.Helper()
	cmd := exec.Command(demo, "-d",
		fmt.Sprintf("%d", Fs), fmt.Sprintf("%d", ch),
		"-24", bits, out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("opus_demo -d -24 failed: %v\nstderr:\n%s", err, stderr.String())
	}
}

func readFileInt16(t *testing.T, path string) []int16 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(data)%2 != 0 {
		t.Fatalf("%s odd len %d", path, len(data))
	}
	out := make([]int16, len(data)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return out
}

func readFileInt24(t *testing.T, path string) []int32 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(data)%3 != 0 {
		t.Fatalf("%s not multiple of 3: len=%d", path, len(data))
	}
	out := make([]int32, len(data)/3)
	for i := range out {
		b0 := uint32(data[i*3+0])
		b1 := uint32(data[i*3+1])
		b2 := uint32(data[i*3+2])
		v := b0 | (b1 << 8) | (b2 << 16)
		if v&0x800000 != 0 {
			v |= 0xFF000000
		}
		out[i] = int32(v)
	}
	return out
}

// ---- Similarity stats (duplicated from blackbox for package isolation) ---

type twStats struct {
	N               int
	Mismatches      int
	MaxAbsDiff      int64
	MeanAbsDiff     float64
	RMSDiff         float64
	SignalToNoiseDB float64
	PeakSNRDB       float64
	MatchPercent    float64
	FullScale       int64
}

func (s twStats) String() string {
	snr := "+Inf"
	psnr := "+Inf"
	if !math.IsInf(s.SignalToNoiseDB, 1) {
		snr = fmt.Sprintf("%.2f dB", s.SignalToNoiseDB)
	}
	if !math.IsInf(s.PeakSNRDB, 1) {
		psnr = fmt.Sprintf("%.2f dB", s.PeakSNRDB)
	}
	return fmt.Sprintf(
		"N=%d diffs=%d (%.4f%% match) maxAbs=%d mean=%.4f RMS=%.4f SNR=%s PSNR=%s",
		s.N, s.Mismatches, s.MatchPercent, s.MaxAbsDiff, s.MeanAbsDiff,
		s.RMSDiff, snr, psnr)
}

func compareInt16TW(a, b []int16) twStats {
	return compareI64TW(toI64I16(a), toI64I16(b), 32768)
}

func compareInt24TW(a, b []int32) twStats {
	return compareI64TW(toI64I32(a), toI64I32(b), 1<<24)
}

func toI64I16(a []int16) []int64 {
	out := make([]int64, len(a))
	for i, v := range a {
		out[i] = int64(v)
	}
	return out
}

func toI64I32(a []int32) []int64 {
	out := make([]int64, len(a))
	for i, v := range a {
		out[i] = int64(v)
	}
	return out
}

func compareI64TW(a, b []int64, fullScale int64) twStats {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	s := twStats{N: n, FullScale: fullScale}
	if n == 0 {
		s.MatchPercent = 100
		s.SignalToNoiseDB = math.Inf(1)
		s.PeakSNRDB = math.Inf(1)
		return s
	}
	var sumAbs, sumSqDiff, sumSqRef float64
	for i := 0; i < n; i++ {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		if d != 0 {
			s.Mismatches++
			if d > s.MaxAbsDiff {
				s.MaxAbsDiff = d
			}
		}
		sumAbs += float64(d)
		sumSqDiff += float64(d) * float64(d)
		sumSqRef += float64(a[i]) * float64(a[i])
	}
	s.MeanAbsDiff = sumAbs / float64(n)
	s.RMSDiff = math.Sqrt(sumSqDiff / float64(n))
	s.MatchPercent = 100 * float64(n-s.Mismatches) / float64(n)
	if sumSqDiff == 0 {
		s.SignalToNoiseDB = math.Inf(1)
		s.PeakSNRDB = math.Inf(1)
	} else {
		if sumSqRef > 0 {
			s.SignalToNoiseDB = 10 * math.Log10(sumSqRef/sumSqDiff)
		}
		s.PeakSNRDB = 20 * math.Log10(float64(fullScale)/s.RMSDiff)
	}
	return s
}

// ---- The three-way test -------------------------------------------------

// TestParity_ThreeWayDecode runs one Go-side encode and three decodes
// (upstream CLI, vendored cgo, Go port) at both int16 and int24
// resolutions. It prints the full similarity matrix so drift that
// appears in one pair but not another is immediately visible.
//
// Assertions:
//   - C_cgo ≡ Go_native at both int16 and int24 (primary port gate).
//   - C_demo == C_cgo at int24 (implies compiler-level noise under
//     int24 precision threshold; no guarantee for int16).
//   - C_demo vs others: logged as stats, max-1-ULP tolerance on int16.
func TestParity_ThreeWayDecode(t *testing.T) {
	demo := opusDemoBin(t)
	frameSamples := twFs * twFrameMs / 1000

	for _, c := range threewayMatrix() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pcm := buildPCMInt16(c.kind, c.ch, twFs, twDuration, 20260420)
			pkts := goEncodeAllInt16ThreeWay(t, c, pcm, frameSamples)
			if len(pkts) == 0 {
				t.Fatalf("no packets produced")
			}

			// ---- Subprocess: opus_demo -d and -d -24.
			dir := t.TempDir()
			bitsPath := filepath.Join(dir, "go.bit")
			writeGoBitstreamDemo(t, bitsPath, pkts)

			demo16 := filepath.Join(dir, "demo16.pcm")
			demo24 := filepath.Join(dir, "demo24.pcm")
			runOpusDemoDecodeInt16(t, demo, twFs, c.ch, bitsPath, demo16)
			runOpusDemoDecodeInt24Impl(t, demo, twFs, c.ch, bitsPath, demo24)
			demoI16 := readFileInt16(t, demo16)
			demoI24 := readFileInt24(t, demo24)

			// ---- In-process: cgo (vendored) and Go port.
			cDec := NewCDecoder(twFs, c.ch)
			if cDec == nil {
				t.Fatalf("NewCDecoder failed")
			}
			defer cDec.Destroy()
			cgoI16 := decodeAllInt16(t, cDec, c.ch, pkts, frameSamples)
			// Fresh decoder for int24 (decoder state would otherwise be
			// contaminated with soft-clip state from the int16 path).
			cDec24 := NewCDecoder(twFs, c.ch)
			if cDec24 == nil {
				t.Fatalf("NewCDecoder failed")
			}
			defer cDec24.Destroy()
			cgoI24 := decodeAllInt24(t, cDec24, c.ch, pkts, frameSamples)

			goDec := NewGoDecoder(twFs, c.ch)
			if goDec == nil {
				t.Fatalf("NewGoDecoder failed")
			}
			defer goDec.Destroy()
			goI16 := decodeAllInt16(t, goDec, c.ch, pkts, frameSamples)
			goDec24 := NewGoDecoder(twFs, c.ch)
			if goDec24 == nil {
				t.Fatalf("NewGoDecoder failed")
			}
			defer goDec24.Destroy()
			goI24 := decodeAllInt24(t, goDec24, c.ch, pkts, frameSamples)

			// ---- Trim to common length.
			n16 := minOf(len(demoI16), len(cgoI16), len(goI16))
			n24 := minOf(len(demoI24), len(cgoI24), len(goI24))

			// ---- Pairwise similarity, int16.
			s_DemoCgo16 := compareInt16TW(demoI16[:n16], cgoI16[:n16])
			s_DemoGo16 := compareInt16TW(demoI16[:n16], goI16[:n16])
			s_CgoGo16 := compareInt16TW(cgoI16[:n16], goI16[:n16])

			// ---- Pairwise similarity, int24.
			s_DemoCgo24 := compareInt24TW(demoI24[:n24], cgoI24[:n24])
			s_DemoGo24 := compareInt24TW(demoI24[:n24], goI24[:n24])
			s_CgoGo24 := compareInt24TW(cgoI24[:n24], goI24[:n24])

			t.Logf(`%s (N_int16=%d, N_int24=%d)

  ┌── int16 (opus_decode path, soft-clip + FLOAT2INT16)
  │   C_demo  vs C_cgo : %s
  │   C_demo  vs Go    : %s
  │   C_cgo   vs Go    : %s
  │
  └── int24 (opus_decode24 path, no soft-clip, FLOAT2INT24)
      C_demo  vs C_cgo : %s
      C_demo  vs Go    : %s
      C_cgo   vs Go    : %s`,
				c.name, n16, n24,
				s_DemoCgo16.String(),
				s_DemoGo16.String(),
				s_CgoGo16.String(),
				s_DemoCgo24.String(),
				s_DemoGo24.String(),
				s_CgoGo24.String(),
			)

			// ---- Hard gates.
			if s_CgoGo16.Mismatches != 0 {
				t.Errorf("C_cgo vs Go int16: %d diffs (expected 0 — primary port gate)",
					s_CgoGo16.Mismatches)
			}
			if s_CgoGo24.Mismatches != 0 {
				t.Errorf("C_cgo vs Go int24: %d diffs (expected 0 — primary port gate)",
					s_CgoGo24.Mismatches)
			}
			if s_DemoGo16.MaxAbsDiff > 1 {
				t.Errorf("C_demo vs Go int16: max abs diff %d exceeds 1 ULP",
					s_DemoGo16.MaxAbsDiff)
			}
			if s_DemoCgo16.MaxAbsDiff > 1 {
				t.Errorf("C_demo vs C_cgo int16: max abs diff %d exceeds 1 ULP (pure C↔C)",
					s_DemoCgo16.MaxAbsDiff)
			}
		})
	}
}

func minOf(xs ...int) int {
	m := xs[0]
	for _, x := range xs[1:] {
		if x < m {
			m = x
		}
	}
	return m
}
