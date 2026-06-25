// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package mdctanalysis

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 mdct-analysis port (the LAME
// encoder analysis filterbank window_subband and the long/short MDCTs) against
// the vendored C LAME reference (oracle.c). Each kernel is driven on both sides
// over identical fabricated float input and the in-place / output buffers must
// be bit-for-bit equal.
//
// The slice is floating-point-bearing — every term is a separately rounded
// product/sum — so the bit-exact assertions are gated behind nativemp3.StrictMode
// per the FP-parity convention: a bare `go test` is clean and the strict run
// (mp3_strict + the FP CGO env, plus mp3lame for the LGPL-fenced encoder
// front end) is the authoritative bit-exact gate.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags=mp3_strict,mp3lame (FP env via mise //libraries/mp3:parity-lame)")
	}
}

// requireBitEqual asserts two float32 slices are bit-for-bit identical (NaN
// payloads compared by bit pattern), which is the bit-exact contract — not an
// epsilon tolerance.
func requireBitEqual(t *testing.T, want, got []float32, msgAndArgs ...interface{}) {
	t.Helper()
	require.Equal(t, len(want), len(got), msgAndArgs...)
	for i := range want {
		require.Equalf(t, math.Float32bits(want[i]), math.Float32bits(got[i]),
			"index %d: want %v got %v (%v)", i, want[i], got[i], msgAndArgs)
	}
}

// randFloats returns n deterministically-seeded float32s in [-scale, scale).
func randFloats(seed uint64, n int, scale float32) []float32 {
	r := rand.New(rand.NewPCG(seed, seed+1))
	out := make([]float32, n)
	for i := range out {
		out[i] = (r.Float32()*2 - 1) * scale
	}
	return out
}

// windowBufLen / windowBase size the PCM window buffer window_subband reads
// over. The kernel reads x1[base-286 .. base+256] (the look-behind history plus
// the 32 fresh samples and their windowed reach), so base must be >= 286 and
// the buffer must extend at least base+257. base=320 / len=640 covers it with
// margin on both ends.
const (
	windowBase   = 320
	windowBufLen = 640
)

// TestParityWindowSubband sweeps the polyphase analysis filterbank +
// Takehiro IDCT over random PCM windows at several amplitude scales (LAME feeds
// it 16-bit-derived sample_t values, so scales spanning small to ~full-scale
// exercise the windowed sums across magnitudes). The 32 subband samples each
// side writes must be bit-identical.
func TestParityWindowSubband(t *testing.T) {
	requireStrict(t)
	for _, scale := range []float32{1, 100, 32768, 1e6} {
		for seed := uint64(1); seed <= 24; seed++ {
			x1 := randFloats(seed*101+uint64(scale), windowBufLen, scale)
			c := cgoWindowSubband(x1, windowBase)
			g := goWindowSubband(x1, windowBase)
			requireBitEqual(t, c, g, "window_subband scale=%v seed=%d", scale, seed)
		}
	}
}

// TestParityMdctShort sweeps the three short-block 6-line MDCTs over random
// 18-line buffers. The in-place result must be bit-identical.
func TestParityMdctShort(t *testing.T) {
	requireStrict(t)
	for _, scale := range []float32{1, 1000, 1e6} {
		for seed := uint64(1); seed <= 64; seed++ {
			in := randFloats(seed*211+uint64(scale)*7, 18, scale)
			c := cgoMdctShort(in)
			g := goMdctShort(in)
			requireBitEqual(t, c, g, "mdct_short scale=%v seed=%d", scale, seed)
		}
	}
}

// TestParityMdctLong sweeps the long-block 18-line MDCT over random 18-line
// windowed inputs. The 18 output lines must be bit-identical.
func TestParityMdctLong(t *testing.T) {
	requireStrict(t)
	for _, scale := range []float32{1, 1000, 1e6} {
		for seed := uint64(1); seed <= 64; seed++ {
			in := randFloats(seed*307+uint64(scale)*3, 18, scale)
			c := cgoMdctLong(in)
			g := goMdctLong(in)
			requireBitEqual(t, c, g, "mdct_long scale=%v seed=%d", scale, seed)
		}
	}
}
