//go:build cgo

package imdctsynthesisfilterbank

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// requireStrict skips a parity test unless the mp3_strict build tag is set.
// The IMDCT + synthesis slice is floating point throughout (the inverse-MDCT
// windowing, the 32-point DCT-II, and the synthesis overlap-add), so its
// output only matches the cgo oracle bit-for-bit when the strict, FMA-free Go
// build is paired with the scalar (-ffp-contract=off, -fno-vectorize, …) cgo
// oracle the mise `parity` task configures. A bare `go test` builds the
// default (FMA-fusing) helpers and would diverge in the last ULP, so the
// assertions are gated to the canonical `mise run //libraries/mp3:parity`
// gate. See the FP-parity convention in the add-audio-format skill.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("imdct-synthesis-filterbank parity asserts bit-exactness; run under -tags=mp3_strict (mise run //libraries/mp3:parity)")
	}
}

// randCoeffs fills n deterministically seeded float32 coefficients in roughly
// the magnitude range a dequantized Layer III granule produces. The IMDCT and
// synthesis kernels are numerically well-conditioned over this range, so any
// per-ULP divergence between the Go port and the C oracle is a real FMA/order
// mismatch rather than a contrived overflow.
func randCoeffs(seed uint64, n int) []float32 {
	r := rand.New(rand.NewPCG(seed, seed+0x9e3779b9))
	out := make([]float32, n)
	for i := range out {
		// Symmetric, exactly-representable-ish spread around 0; the factor
		// keeps values O(1e2) like real dequantized lines.
		out[i] = (r.Float32()*2 - 1) * 250
	}
	return out
}

// blockSpec describes one IMDCT configuration: the block_type fed to
// L3_imdct_gr and the number of leading long bands. These span minimp3's
// branches: a pure long block, a stop block (block_type 3, the start/stop
// window), a short block (block_type 2) with the mixed-block long-band prefix,
// and a fully-short block (n_long_bands == 0).
type blockSpec struct {
	name       string
	blockType  uint8
	nLongBands uint
}

// TestL3IMDCTGrParity drives the Go L3IMDCTGr (folded with L3ChangeSign, as the
// decoder calls them) and the C L3_imdct_gr + L3_change_sign over identical
// granule buffers and overlap histories, asserting all 576 output lines and
// all 9*32 updated overlap floats match bit-for-bit. The corpus spans the long,
// stop, mixed-short and fully-short IMDCT branches, each over a fresh random
// granule and a fresh random overlap carry so the windowing reads live history.
func TestL3IMDCTGrParity(t *testing.T) {
	requireStrict(t)

	const longBlockType = 0
	const shortBlockType = 2
	const stopBlockType = 3

	specs := []blockSpec{
		{name: "long", blockType: longBlockType, nLongBands: 32},
		{name: "stop", blockType: stopBlockType, nLongBands: 32},
		{name: "short-all", blockType: shortBlockType, nLongBands: 0},
		{name: "short-mixed-2long", blockType: shortBlockType, nLongBands: 2},
		{name: "short-mixed-8long", blockType: shortBlockType, nLongBands: 8},
		{name: "long-partial", blockType: longBlockType, nLongBands: 10},
	}

	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			grSeed := uint64(0x1000) + uint64(s.blockType)*7 + uint64(s.nLongBands)
			ovSeed := uint64(0x2000) + uint64(s.blockType)*13 + uint64(s.nLongBands)

			var gr [576]float32
			var ov [9 * 32]float32
			copy(gr[:], randCoeffs(grSeed, 576))
			copy(ov[:], randCoeffs(ovSeed, 9*32))

			// C oracle over independent copies.
			cGr, cOv := cgoL3IMDCTGr(gr, ov, s.blockType, s.nLongBands)

			// Go port over independent copies. l3ChangeSign reslices grbuf out
			// to a 18+16*36 = 594-float cursor (the trailing reslice's implicit
			// high bound is len, so the slice must be at least that long; it
			// never reads or writes past the 576th element). The decoder backs
			// grbuf with mp3dec_scratch_t.grbuf[2][576] and the C walks the
			// channel-0 pointer through that contiguous 1152-float block, so
			// give the Go channel-0 view the same flat length.
			var goBack [2 * 576]float32
			copy(goBack[:576], gr[:])
			goGr := goBack[:]
			goOv := ov
			nativemp3.L3IMDCTGr(goGr, goOv[:], s.blockType, s.nLongBands)
			nativemp3.L3ChangeSign(goGr)

			for i := 0; i < 576; i++ {
				assert.Equalf(t, cGr[i], goGr[i], "grbuf[%d]", i)
			}
			for i := 0; i < 9*32; i++ {
				assert.Equalf(t, cOv[i], goOv[i], "overlap[%d]", i)
			}
		})
	}
}

// TestL3ChangeSignParity pins L3_change_sign in isolation: it negates every odd
// line of every other subband. This is sign-flipping only (no arithmetic), so
// it is bit-identical in either build mode, but gates with the suite for
// uniformity.
func TestL3ChangeSignParity(t *testing.T) {
	requireStrict(t)
	var gr [576]float32
	copy(gr[:], randCoeffs(0x3333, 576))

	cGr := cgoL3ChangeSign(gr)
	// l3ChangeSign reslices out to a 594-float cursor (implicit-high = len), so
	// the Go granule view must be at least that long; back it with the same
	// flat 1152-float block the decoder's grbuf[2][576] gives channel 0.
	var goBack [2 * 576]float32
	copy(goBack[:576], gr[:])
	goGr := goBack[:]
	nativemp3.L3ChangeSign(goGr)

	for i := 0; i < 576; i++ {
		assert.Equalf(t, cGr[i], goGr[i], "grbuf[%d]", i)
	}
}

// TestMp3dDCTIIParity drives the Go Mp3dDCTII and the C mp3d_DCT_II over
// identical column blocks for the two granule widths the decoder uses (n == 18
// for the long/normal granule, n == 12 for the lower-band reduced granule at
// minimp3.h:1793) plus a couple of intermediate widths, asserting the full 576
// floats match. The transform is in-place and reads a stride-18 column layout,
// so the whole buffer is seeded even though only the first n columns transform.
func TestMp3dDCTIIParity(t *testing.T) {
	requireStrict(t)

	for _, n := range []int{1, 12, 16, 18} {
		t.Run(map[int]string{1: "n1", 12: "n12", 16: "n16", 18: "n18"}[n], func(t *testing.T) {
			var gr [576]float32
			copy(gr[:], randCoeffs(0x4000+uint64(n), 576))

			cGr := cgoMp3dDCTII(gr, n)
			goGr := gr
			nativemp3.Mp3dDCTII(goGr[:], n)

			for i := 0; i < 576; i++ {
				assert.Equalf(t, cGr[i], goGr[i], "grbuf[%d] (n=%d)", i, n)
			}
		})
	}
}

// TestMp3dSynthGranuleParity drives the full per-granule synthesis filterbank
// end-to-end through both implementations. The grbuf is the genuine post-IMDCT
// product (built by running L3_imdct_gr + L3_change_sign on random coefficients
// per channel) so the synthesis sees realistic subband data; the qmf_state
// carry is seeded random so the windowed history is exercised, not zeroed. The
// test asserts both the emitted interleaved int16 PCM block (32*nbands*nch
// samples) and the updated qmf_state (15*2*32 floats) match bit-for-bit. It
// covers mono (the stride-2 partial qmf save) and stereo, at both decoder
// granule widths (nbands 18 and 12).
//
// The ported mp3dSynth now addresses the synthesis line buffer through a
// constant base offset (lins[15*64 + 4*i - k*64]) instead of reslicing a
// zlin := lins[15*64:] view, so the C reference's positive-and-negative zlin
// subscripts no longer underflow a Go slice. This end-to-end oracle therefore
// runs and asserts the full per-granule synthesis filterbank is bit-exact.
func TestMp3dSynthGranuleParity(t *testing.T) {
	requireStrict(t)

	cases := []struct {
		name   string
		nch    int
		nbands int
	}{
		{name: "mono-18", nch: 1, nbands: 18},
		{name: "stereo-18", nch: 2, nbands: 18},
		{name: "mono-12", nch: 1, nbands: 12},
		{name: "stereo-12", nch: 2, nbands: 12},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Build a realistic grbuf: run the IMDCT slice per channel over
			// random coefficients (long block, all 32 long bands), exactly as
			// the decoder feeds mp3d_synth_granule.
			grbuf := make([]float32, 576*c.nch)
			for ch := 0; ch < c.nch; ch++ {
				// l3ChangeSign reslices out to a 594-float cursor (implicit-high
				// = len), so build each channel in a flat 1152-float buffer
				// (matching grbuf[2][576]) before copying the 576 result into the
				// shared grbuf.
				var chBack [2 * 576]float32
				var ov [9 * 32]float32
				copy(chBack[:576], randCoeffs(0x5000+uint64(ch)*101+uint64(c.nbands), 576))
				copy(ov[:], randCoeffs(0x6000+uint64(ch)*101+uint64(c.nbands), 9*32))
				nativemp3.L3IMDCTGr(chBack[:], ov[:], 0, 32)
				nativemp3.L3ChangeSign(chBack[:])
				copy(grbuf[576*ch:], chBack[:576])
			}

			var qmf [15 * 2 * 32]float32
			copy(qmf[:], randCoeffs(0x7000+uint64(c.nch)*31+uint64(c.nbands), 15*2*32))

			// C oracle.
			cPCM, cQmf := cgoMp3dSynthGranule(qmf, grbuf, c.nbands, c.nch)

			// Go port over independent copies (mp3d_synth_granule mutates
			// grbuf in place via the per-channel DCT-II, so each side gets its
			// own copy).
			goGrbuf := append([]float32(nil), grbuf...)
			goQmf := qmf
			goPCM := make([]int16, 32*c.nbands*c.nch)
			lins := make([]float32, (18+15)*64)
			nativemp3.Mp3dSynthGranule(goQmf[:], goGrbuf, c.nbands, c.nch, goPCM, lins)

			require.Equal(t, len(cPCM), len(goPCM))
			for i := 0; i < len(cPCM); i++ {
				assert.Equalf(t, cPCM[i], goPCM[i], "pcm[%d]", i)
			}
			for i := 0; i < 15*2*32; i++ {
				assert.Equalf(t, cQmf[i], goQmf[i], "qmf_state[%d]", i)
			}
		})
	}
}
