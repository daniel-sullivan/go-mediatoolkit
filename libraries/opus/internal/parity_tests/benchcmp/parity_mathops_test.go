//go:build cgo && opus_strict

package benchcmp

import (
	"fmt"
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// bitExactF32 checks that two float32s have identical IEEE 754 bit
// patterns — the only meaningful equality for a bit-exact port. NaN
// payloads and +0 vs -0 are both respected.
func bitExactF32(a, b float32) bool {
	return math.Float32bits(a) == math.Float32bits(b)
}

// reportMismatch turns a failure into a detailed log line that shows
// both values, their bit patterns, and the ULP distance.
func reportMismatch(t *testing.T, name string, args string, want, got float32) {
	t.Helper()
	wantBits := math.Float32bits(want)
	gotBits := math.Float32bits(got)
	ulp := int64(gotBits) - int64(wantBits)
	t.Errorf("%s(%s): want %g (0x%08x), got %g (0x%08x), ULP diff %d",
		name, args, want, wantBits, got, gotBits, ulp)
}

// TestParity_CeltLog2 — the FLOAT_APPROX polynomial log2 with bit
// manipulation is the most error-prone mathops port.
func TestParity_CeltLog2(t *testing.T) {
	// Representative sweep: small to large, spanning the whole float32
	// positive range that celt_log2 is ever called with.
	inputs := []float32{
		1e-20, 1e-10, 1e-5, 1e-3, 1e-2, 0.1, 0.25, 0.5, 0.75,
		1.0, 1.5, 2.0, 3.0, 4.0, 7.5, 10.0, 100.0, 1e4, 1e6, 1e10, 1e20,
	}
	// Dense sweep across each decade.
	for d := -20; d <= 20; d++ {
		base := float32(math.Pow(10, float64(d)))
		for frac := float32(1.0); frac < 10.0; frac += 0.37 {
			inputs = append(inputs, base*frac)
		}
	}
	for _, x := range inputs {
		want := cCeltLog2(x)
		got := nativeopus.ExportTestCeltLog2(x)
		if !bitExactF32(want, got) {
			reportMismatch(t, "celt_log2", formatF(x), want, got)
		}
	}
}

// TestParity_CeltExp2 — bit-twiddled polynomial exp2.
func TestParity_CeltExp2(t *testing.T) {
	inputs := []float32{}
	for x := float32(-50); x <= 20; x += 0.37 {
		inputs = append(inputs, x)
	}
	// Exact boundaries the C code branches on.
	inputs = append(inputs, -50, -49.999, -49.5, 0, 1, 14, 14.999, 20)
	for _, x := range inputs {
		want := cCeltExp2(x)
		got := nativeopus.ExportTestCeltExp2(x)
		if !bitExactF32(want, got) {
			reportMismatch(t, "celt_exp2", formatF(x), want, got)
		}
	}
}

// TestParity_CeltCosNorm2 — polynomial cosine for (PI/2 * x).
func TestParity_CeltCosNorm2(t *testing.T) {
	for x := float32(-5); x <= 5; x += 0.01 {
		want := cCeltCosNorm2(x)
		got := nativeopus.ExportTestCeltCosNorm2(x)
		if !bitExactF32(want, got) {
			reportMismatch(t, "celt_cos_norm2", formatF(x), want, got)
		}
	}
}

// TestParity_CeltAtanNorm — 15th-order Remez polynomial.
func TestParity_CeltAtanNorm(t *testing.T) {
	for x := float32(0); x <= 1; x += 0.001 {
		want := cCeltAtanNorm(x)
		got := nativeopus.ExportTestCeltAtanNorm(x)
		if !bitExactF32(want, got) {
			reportMismatch(t, "celt_atan_norm", formatF(x), want, got)
		}
	}
}

// TestParity_CeltAtan2pNorm — atan2 over the non-negative quadrant.
func TestParity_CeltAtan2pNorm(t *testing.T) {
	points := []struct{ y, x float32 }{
		{0, 0.1}, {0.1, 0}, {1, 1}, {0.5, 0.7}, {0.7, 0.5}, {1e-10, 1e-10},
	}
	for y := float32(0); y <= 2; y += 0.13 {
		for x := float32(0); x <= 2; x += 0.13 {
			points = append(points, struct{ y, x float32 }{y, x})
		}
	}
	for _, p := range points {
		if p.x == 0 && p.y == 0 {
			continue // undefined
		}
		want := cCeltAtan2pNorm(p.y, p.x)
		got := nativeopus.ExportTestCeltAtan2pNorm(p.y, p.x)
		if !bitExactF32(want, got) {
			reportMismatch(t, "celt_atan2p_norm", formatF2(p.y, p.x), want, got)
		}
	}
}

// TestParity_FastAtan2f — the general-quadrant float atan2.
func TestParity_FastAtan2f(t *testing.T) {
	for y := float32(-2); y <= 2; y += 0.17 {
		for x := float32(-2); x <= 2; x += 0.17 {
			want := cFastAtan2f(y, x)
			got := nativeopus.ExportTestFastAtan2f(y, x)
			if !bitExactF32(want, got) {
				reportMismatch(t, "fast_atan2f", formatF2(y, x), want, got)
			}
		}
	}
}

// TestParity_Float2Int16 — scalar scale+clip+round.
func TestParity_Float2Int16(t *testing.T) {
	// Sweep the range the decoder emits, plus edges past the clip.
	for x := float32(-2.5); x <= 2.5; x += 0.0013 {
		want := cFloat2Int16(x)
		got := nativeopus.ExportTestFloat2Int16(x)
		if want != got {
			t.Errorf("FLOAT2INT16(%g): want %d, got %d", x, want, got)
		}
	}
	// Edge values where rounding matters.
	edges := []float32{
		0.0, -0.0, 1.0 / 65536, -1.0 / 65536,
		32767.0 / 32768, -32768.0 / 32768,
		1.0, -1.0, 1.5, -1.5,
		float32(math.Nextafter32(1.0, 2.0)),
		float32(math.Nextafter32(1.0, 0.0)),
	}
	for _, x := range edges {
		want := cFloat2Int16(x)
		got := nativeopus.ExportTestFloat2Int16(x)
		if want != got {
			t.Errorf("FLOAT2INT16(%g edge): want %d, got %d", x, want, got)
		}
	}
}

// TestParity_Float2Int — round-to-nearest-even conversion.
func TestParity_Float2Int(t *testing.T) {
	// Include exact half-integer ties where ties-to-even matters.
	ties := []float32{-2.5, -1.5, -0.5, 0.5, 1.5, 2.5, 3.5, 4.5}
	for _, x := range ties {
		want := cFloat2Int(x)
		got := nativeopus.ExportTestFloat2Int(x)
		if want != got {
			t.Errorf("float2int(%g tie): want %d, got %d", x, want, got)
		}
	}
	for x := float32(-1000); x <= 1000; x += 0.33 {
		want := cFloat2Int(x)
		got := nativeopus.ExportTestFloat2Int(x)
		if want != got {
			t.Errorf("float2int(%g): want %d, got %d", x, want, got)
		}
	}
}

// TestParity_Isqrt32 — integer floor-sqrt.
func TestParity_Isqrt32(t *testing.T) {
	vs := []uint32{
		1, 2, 3, 4, 5, 10, 100, 1000, 10000,
		65535, 65536, 65537, 1 << 20, 1 << 24, 1 << 30, 1<<31 - 1,
		0xDEADBEEF, 0x12345678, 0xFFFFFFFF, 0x80000000,
	}
	// Also a stride across the 32-bit range.
	for v := uint32(1); v < 0xFFFFFFFF/64; v += 0xFFFFFFFF / 4096 {
		vs = append(vs, v)
	}
	for _, v := range vs {
		want := cIsqrt32(v)
		got := uint32(nativeopus.ExportTestIsqrt32(v))
		if want != got {
			t.Errorf("isqrt32(0x%x): want %d, got %d", v, want, got)
		}
	}
}

// TestParity_CeltFloat2Int16C — vectorised scalar conversion.
func TestParity_CeltFloat2Int16C(t *testing.T) {
	n := 960
	in := make([]float32, n)
	for i := range in {
		in[i] = float32(math.Sin(float64(i)*0.01)) * 0.5
	}
	// Push a few samples past the clip threshold.
	in[10] = 2.5
	in[20] = -2.5
	in[30] = 0.999999
	wantOut := make([]int16, n)
	gotOut := make([]int16, n)
	cCeltFloat2int16C(in, wantOut)
	nativeopus.ExportTestCeltFloat2Int16C(in, gotOut, n)
	for i := range wantOut {
		if wantOut[i] != gotOut[i] {
			t.Errorf("celt_float2int16_c[%d]: want %d, got %d (in=%g)",
				i, wantOut[i], gotOut[i], in[i])
			break
		}
	}
}

// TestParity_OpusLimit2 — in-place clip plus hint-return.
func TestParity_OpusLimit2(t *testing.T) {
	cases := [][]float32{
		nil,
		{},
		{0.5, -0.5, 0.1, 0.2},
		{1.5, -1.5, 0.9, -0.9},
		{3.0, -3.0, 0.5, -0.5}, // pushes past 2 for clipping
	}
	for idx, base := range cases {
		var bufGo, bufC []float32
		if base != nil {
			bufGo = append([]float32(nil), base...)
			bufC = append([]float32(nil), base...)
		}
		wantN := cOpusLimit2CheckWithin1C(bufC)
		gotN := nativeopus.ExportTestOpusLimit2CheckWithin1C(bufGo, len(bufGo))
		if wantN != gotN {
			t.Errorf("case %d: return want=%d, got=%d", idx, wantN, gotN)
		}
		if len(bufGo) != len(bufC) {
			continue
		}
		for i := range bufGo {
			if !bitExactF32(bufGo[i], bufC[i]) {
				t.Errorf("case %d [%d]: want %g, got %g", idx, i, bufC[i], bufGo[i])
				break
			}
		}
	}
}

// ── test helpers ────────────────────────────────────────────────────

func formatF(x float32) string {
	return fmtF(x)
}
func formatF2(y, x float32) string {
	return fmtF(y) + ", " + fmtF(x)
}

// fmtF prints the float plus its bit pattern so mismatches are
// reproducible without guessing at rounding.
func fmtF(x float32) string {
	return fmt.Sprintf("%g (0x%08x)", x, math.Float32bits(x))
}
