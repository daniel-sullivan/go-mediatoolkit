//go:build cgo

package bitallocation

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// requireStrict skips a parity test unless the mp3_strict build tag is set.
// The scalefactor gain expansion (L3_ldexp_q2) is floating-point, so the scf[]
// comparison is only a bit-exact target under the FMA-free strict build against
// the -ffp-contract=off cgo oracle. A bare `go test` therefore stays clean;
// `mise run //libraries/mp3:parity` (which sets -tags=mp3_strict + the scalar
// CGO flags) is the single bit-exact gate. See the FP-parity convention in
// CONTRIBUTING.md.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-allocation parity asserts FP bit-exactness; run under -tags=mp3_strict (mise run //libraries/mp3:parity)")
	}
}

// hexCtx formats a compact label for a failing scalefactor case.
func hexCtx(tag string, hdr []byte, gi, ch int) string {
	return fmt.Sprintf("%s hdr=% x gi=%d ch=%d", tag, hdr, gi, ch)
}

// grCountFor returns the number of granules L3_read_side_info populates for a
// header: MPEG-1 stereo -> 4, MPEG-1 mono / MPEG-2 stereo -> 2, MPEG-2 mono
// -> 1 (gr_count = (mono?1:2) * (mpeg1?2:1), minimp3.h:520-524).
func grCountFor(hdr []byte) int {
	n := 2
	if (hdr[3] & 0xC0) == 0xC0 { // HDR_IS_MONO
		n = 1
	}
	if hdr[1]&0x8 != 0 { // HDR_TEST_MPEG1
		n *= 2
	}
	return n
}

// channelsFor returns the number of channels (1 mono, else 2) for a header.
func channelsFor(hdr []byte) int {
	if (hdr[3] & 0xC0) == 0xC0 {
		return 1
	}
	return 2
}

// headerCorpus spans MPEG-1/2/2.5, mono/stereo, MS-stereo and intensity-stereo
// mode bits, and the three sample-rate indices, so the scalefactor decode
// exercises the MPEG-1 (g_scfc_decode) and MPEG-2/2.5 (g_mod mixed-radix)
// branches plus the MS-stereo gain bias and I-stereo channel-1 path.
var headerCorpus = [][]byte{
	{0xFF, 0xFB, 0x90, 0x04}, // MPEG-1 L3 44100 stereo, no mode ext
	{0xFF, 0xFB, 0x90, 0x64}, // MPEG-1 L3 stereo, MS-stereo (mode ext 0x60)
	{0xFF, 0xFB, 0x90, 0x54}, // MPEG-1 L3 stereo, I-stereo + MS (0x50)
	{0xFF, 0xFB, 0x90, 0x14}, // MPEG-1 L3 stereo, I-stereo only (0x10)
	{0xFF, 0xFB, 0x90, 0xC4}, // MPEG-1 L3 mono
	{0xFF, 0xFB, 0x94, 0x04}, // MPEG-1 L3 48000 stereo
	{0xFF, 0xFB, 0x98, 0x04}, // MPEG-1 L3 32000 stereo
	{0xFF, 0xF3, 0x90, 0x04}, // MPEG-2 L3 stereo
	{0xFF, 0xF3, 0x90, 0x64}, // MPEG-2 L3 stereo, MS-stereo
	{0xFF, 0xF3, 0x90, 0x14}, // MPEG-2 L3 stereo, I-stereo
	{0xFF, 0xF3, 0x90, 0xC4}, // MPEG-2 L3 mono
	{0xFF, 0xE3, 0x90, 0x04}, // MPEG-2.5 L3 stereo
	{0xFF, 0xE3, 0x90, 0xC4}, // MPEG-2.5 L3 mono
}

// f32bitsEqual asserts two float32 slices are bit-identical via their raw
// IEEE-754 bit patterns (NaN-safe), the bit-exactness contract for the FP
// gains. ctx is a short label identifying the failing case.
func f32bitsEqual(t *testing.T, want, got []float32, ctx string) {
	t.Helper()
	require.Equal(t, len(want), len(got))
	for i := range want {
		wb, gb := math.Float32bits(want[i]), math.Float32bits(got[i])
		if wb != gb {
			assert.Failf(t, "scf bit mismatch",
				"%s: scf[%d] c=%v(0x%08x) go=%v(0x%08x)", ctx, i, want[i], wb, got[i], gb)
			return
		}
	}
}

// TestScalefactorDecodeParity is the core bit-allocation parity assertion. For
// each header it fabricates many random side-info buffers, parses each with
// BOTH the vendored C L3_read_side_info and the Go L3ReadSideInfo (the
// library's own parser is the input fabricator), and — when both accept the
// buffer — runs L3_decode_scalefactors / L3DecodeScalefactors on every granule
// of every channel over a shared random main-data buffer, asserting the
// resulting float32 gains, ist_pos bytes and final bit position match exactly.
//
// With the Go port's scfsi mask corrected to the canonical & 15, the two
// side-info parsers agree on scfsi for every granule, so the scalefactor decode
// is asserted unconditionally here. TestScfsiMaskParity separately pins that the
// scfsi masks themselves match. This test proves the
// L3_decode_scalefactors / L3_read_scalefactors / L3_ldexp_q2 chain (including
// the float32 gain expansion) is bit-identical.
func TestScalefactorDecodeParity(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewSource(0x5cf5cf))
	const sideBytes = 32 // ample for the largest side info (MPEG-1 stereo)
	const mainBytes = 96 // scalefactor bit budget; never overrun within a granule

	asserted := 0
	for _, hdr := range headerCorpus {
		gc := grCountFor(hdr)
		nch := channelsFor(hdr)
		for iter := 0; iter < 4000; iter++ {
			side := make([]byte, sideBytes)
			rng.Read(side)
			main := make([]byte, mainBytes)
			rng.Read(main)

			cg, cok := cgoReadSideInfo(side, hdr)
			gg, gok := goReadSideInfo(side, hdr)
			require.Equalf(t, cok, gok, "side-info accept disagreement hdr=% x side=% x", hdr, side)
			if !cok {
				continue
			}

			// The ist_pos[ch] scratch carries across granules in the real
			// decode loop; seed it deterministically and identically per
			// channel so both sides start from the same state.
			for ch := 0; ch < nch; ch++ {
				seed := make([]uint8, 39)
				for i := range seed {
					seed[i] = byte(rng.Intn(256))
				}
				for gi := ch; gi < gc; gi += nch {
					// The Go port now stores gr->scfsi with the canonical & 15
					// mask (matching the vendored minimp3), so the scalefactor
					// decode is asserted unconditionally — there are no longer
					// any scfsi-mask-gated cases to skip. TestScfsiMaskParity
					// pins that the masks themselves agree.
					cScf, cIst, cPos := cgoDecodeScalefactors(cg, gi, hdr, main, ch, seed)
					gScf, gIst, gPos := goDecodeScalefactors(gg, gi, hdr, main, ch, seed)

					f32bitsEqual(t, cScf, gScf, hexCtx("scf", hdr, gi, ch))
					assert.Equalf(t, cIst, gIst, "ist_pos hdr=% x gi=%d ch=%d side=% x", hdr, gi, ch, side)
					assert.Equalf(t, cPos, gPos, "bs.pos hdr=% x gi=%d ch=%d", hdr, gi, ch)
					asserted++
				}
			}
		}
	}
	// Guard against a corpus that silently never exercises the decode path.
	require.Greater(t, asserted, 200, "too few asserted granules — corpus is not exercising the scalefactor decode")
}

// TestScfsiMaskParity pins that nativemp3.L3ReadSideInfo stores gr->scfsi with
// the canonical & 15 mask, exactly matching the vendored libminimp3/minimp3.h
// (and lieff/minimp3, which use (scfsi >> 12) & 15). A prior version of the Go
// port masked with & 3, dropping the high two bits of the scfsi nibble; those
// bits select intensity-stereo position reuse (scfsi & 8) and band-group reuse,
// so the truncation silently corrupted scalefactor decode whenever they were
// set. This test exercises a large random side-info corpus — including cases
// where the high bits are set — and asserts the Go and C masks agree on every
// granule, so a regression back to a narrower mask is caught immediately.
func TestScfsiMaskParity(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewSource(0x5cf5cf))
	asserted := 0
	sawHighBits := false
	for _, hdr := range headerCorpus {
		gc := grCountFor(hdr)
		for iter := 0; iter < 4000; iter++ {
			side := make([]byte, 32)
			rng.Read(side)
			cg, cok := cgoReadSideInfo(side, hdr)
			gg, gok := goReadSideInfo(side, hdr)
			if cok != gok || !cok {
				continue
			}
			for gi := 0; gi < gc; gi++ {
				c, g := cg.scfsi(gi), gg.scfsi(gi)
				assert.Equalf(t, c, g, "scfsi mismatch hdr=% x gi=%d side=% x", hdr, gi, side)
				if c&0xC != 0 {
					sawHighBits = true
				}
				asserted++
			}
		}
	}
	require.Greater(t, asserted, 200, "too few asserted granules — corpus is not exercising side-info decode")
	require.True(t, sawHighBits, "expected the corpus to exercise scfsi values with the high two bits set")
}
