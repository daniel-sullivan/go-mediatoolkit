//go:build cgo

package dequantize

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// requireStrict skips a parity test unless the mp3_strict build tag is set.
// The dequantize slice's gain expansion (L3_ldexp_q2's float32 multiplies) is
// floating point, so its output only matches the cgo oracle bit-for-bit when
// the strict, FMA-free Go build is paired with the scalar (-ffp-contract=off,
// -fno-vectorize, …) cgo oracle the mise `parity` task configures. A bare
// `go test` builds the default (FMA-fusing) helpers and would diverge in the
// last ULP, so the assertions are gated to the canonical
// `mise run //libraries/mp3:parity` gate. The integer L3_read_scalefactors
// slice matches in both builds but gates with the suite for uniformity. See
// the FP-parity convention in the add-audio-format skill.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("dequantize parity asserts bit-exactness; run under -tags=mp3_strict (mise run //libraries/mp3:parity)")
	}
}

// makeRandomBytes returns n deterministically seeded random bytes so the parity
// sweep is reproducible. The payload feeds the bit reader L3_read_scalefactors
// walks; any byte run unpacks deterministically because the reader returns 0
// past its limit and the scalefactor unpack reads fixed-width fields.
func makeRandomBytes(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, seed+1))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// The reassembled main-data buffer is sized as the real decoder sizes its
// scratch.maindata, so the bit reader never indexes past the slice even when
// bsPos is non-zero or the scalefactor partitions are at their widest.
const payloadBytes = nativemp3.MaxBitReservoirBytes + nativemp3.MaxL3FramePayloadBytes

// TestL3LdexpParity sweeps L3_ldexp_q2 across the full exp_q2 domain the
// granule decode reaches and asserts the Go port reproduces the C bit-for-bit.
//
// The reachable domain follows from L3_decode_scalefactors:
//   - the gain seed call is L3_ldexp_q2(1<<(MAX_SCFI/4), MAX_SCFI - gain_exp),
//     where gain_exp = global_gain - 214 (- 2 for MS stereo). global_gain runs
//     0..255, so exp_q2 = MAX_SCFI - gain_exp reaches -1 / -2 (the negative
//     `1 << 30 >> (e >> 2)` shift-by-negative-count caveat the Go port
//     reproduces with explicit mod-32 masking) at the low end and ~254 at the
//     high end;
//   - the per-band call is L3_ldexp_q2(gain, iscf[i] << scf_shift), a
//     non-negative exponent that can run into the thousands once the
//     subblock-gain / preamp adjustments push iscf high, so the do/while
//     chunking loop (e capped at 30*4) iterates multiple times.
//
// The sweep therefore spans the negative region, the single-pass region, and
// the multi-pass chunking region, plus a sweep of the y mantissa.
func TestL3LdexpParity(t *testing.T) {
	requireStrict(t)

	const maxSCFI = 40 // (255 - 214 + 3) & ~3 == MAX_SCFI

	// Exponent sweep: the negative caveat region, the single-/multi-pass
	// boundary (e capped at 120), and well past the chunking threshold.
	exps := []int{}
	for e := -4; e <= 600; e++ {
		exps = append(exps, e)
	}
	// A few exactly-representable seed mantissas: the gain seed 1<<(MAX_SCFI/4)
	// and the per-band running-gain magnitudes.
	ys := []float32{
		float32(int32(1) << (maxSCFI / 4)),
		1.0, 0.5, 2.0, 1234.5, 9.31322575e-10,
	}

	for _, y := range ys {
		for _, e := range exps {
			assert.Equalf(t, cgoL3Ldexp(y, e), nativemp3.L3Ldexp(y, e), "L3_ldexp_q2(%v, %d)", y, e)
		}
	}
}

// readSpec describes a synthetic L3_read_scalefactors input. The fields mirror
// the arguments minimp3's L3_read_scalefactors consumes; scfCount is the
// per-partition band count (a g_scf_partitions row shape), scfSize the
// per-partition field width, and scfsi the (signed) selection-info word.
type readSpec struct {
	name     string
	scfSize  [4]uint8
	scfCount [28]uint8
	scfsi    int
	seed     uint64
	bsPos    int
}

// TestL3ReadScalefactorsParity drives the Go L3ReadScalefactors and the C
// L3_read_scalefactors over identical inputs and asserts the written scf and
// ist_pos arrays plus the final bit position match exactly. The corpus spans:
//   - the bits==0 partition (memset of scf + ist_pos);
//   - the scfsi&8 copy partition (scf := ist_pos memcpy);
//   - the scfsi<0 max_scf sentinel branch (ist_pos == -1 -> 0xFF wrap) vs the
//     scfsi>=0 branch (max_scf == -1, no sentinel);
//   - non-byte-aligned start positions; and
//   - the four-partition walk with the trailing three guard zeros.
//
// L3_read_scalefactors is pure-integer and matches in both build modes, but the
// suite gates uniformly so a bare `go test` stays clean.
func TestL3ReadScalefactorsParity(t *testing.T) {
	requireStrict(t)

	specs := []readSpec{
		{
			name:     "mpeg1-typical",
			scfSize:  [4]uint8{4, 4, 3, 3},
			scfCount: [28]uint8{6, 5, 5, 5, 6, 5, 5, 5, 6, 5, 7, 3, 11, 10, 0},
			scfsi:    0,
			seed:     1,
		},
		{
			name:     "scfsi-negative-sentinel", // scfsi<0 -> max_scf active, -1 wraps to 0xFF
			scfSize:  [4]uint8{3, 2, 1, 4},
			scfCount: [28]uint8{6, 6, 6, 3, 8, 8, 5, 0},
			scfsi:    -16,
			seed:     2,
		},
		{
			name:     "scfsi-copy-bit", // scfsi&8 on the first partition -> memcpy from ist_pos
			scfSize:  [4]uint8{4, 4, 3, 3},
			scfCount: [28]uint8{6, 5, 5, 5, 6, 5, 5, 5, 6, 5, 7, 3, 11, 10, 0},
			scfsi:    8,
			seed:     3,
		},
		{
			name:     "bits-zero-partition", // scf_size has a zero -> memset path
			scfSize:  [4]uint8{0, 4, 0, 3},
			scfCount: [28]uint8{6, 5, 5, 5, 6, 5, 5, 5, 6, 5, 7, 3, 11, 10, 0},
			scfsi:    0,
			seed:     4,
		},
		{
			name:     "bspos-unaligned",
			scfSize:  [4]uint8{4, 4, 3, 3},
			scfCount: [28]uint8{6, 5, 5, 5, 6, 5, 5, 5, 6, 5, 7, 3, 11, 10, 0},
			scfsi:    0,
			seed:     5,
			bsPos:    5,
		},
		{
			name:     "single-partition", // scf_count[1]==0 terminates the walk early
			scfSize:  [4]uint8{4, 0, 0, 0},
			scfCount: [28]uint8{8, 0},
			scfsi:    0,
			seed:     6,
		},
		{
			name:     "wide-mpeg2-partition",
			scfSize:  [4]uint8{5, 5, 4, 4},
			scfCount: [28]uint8{8, 9, 6, 12, 6, 9, 9, 9, 6, 9, 12, 6, 15, 18, 0},
			scfsi:    -16,
			seed:     7,
		},
	}

	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			payload := makeRandomBytes(s.seed, payloadBytes)

			// The decoder seeds ist_pos with distinct values so the scfsi&8
			// memcpy partition surfaces a copy (vs a fresh unpack) and a
			// misaligned walk would surface as a wrong byte.
			istInit := make([]byte, 39)
			for i := range istInit {
				istInit[i] = byte(0x40 + i)
			}

			// C side over its own buffers.
			cScf := make([]byte, 40)
			cIst := append([]byte(nil), istInit...)
			cPayload := append([]byte(nil), payload...)
			cPos := cgoL3ReadScalefactors(cScf, cIst, s.scfSize, s.scfCount, cPayload, s.bsPos, s.scfsi)

			// Go side over independent buffers.
			goScf := make([]byte, 40)
			goIst := append([]byte(nil), istInit...)
			goPayload := append([]byte(nil), payload...)
			var bs nativemp3.BitStream
			nativemp3.BsInit(&bs, goPayload, len(goPayload))
			bs.Pos = s.bsPos
			nativemp3.L3ReadScalefactors(goScf, goIst, s.scfSize[:], s.scfCount[:], &bs, s.scfsi)

			assert.Equalf(t, cPos, bs.Pos, "final bs.pos")
			assert.Equalf(t, cScf, goScf, "scf[]")
			assert.Equalf(t, cIst, goIst, "ist_pos[]")
		})
	}
}

// decodeSpec describes a synthetic-but-valid L3_decode_scalefactors input. The
// fields mirror the L3_gr_info_t members the function consumes plus the 4-byte
// header and channel index that gate the MPEG-1 / I-stereo / MS-stereo
// branches.
type decodeSpec struct {
	name             string
	hdr              [4]byte
	scalefacCompress uint16
	globalGain       uint8
	scalefacScale    uint8
	nLongSfb         uint8
	nShortSfb        uint8
	subblockGain     [3]uint8
	preflag          uint8
	scfsi            uint8
	ch               int
	seed             uint64
	bsPos            int
}

// mpeg1Hdr returns a 4-byte header with the MPEG-1 version bit set
// (HDR_TEST_MPEG1: h[1] & 0x08), no MS/I-stereo unless overridden via the
// channel-mode bits in h[3]. The sync word in h[0]/h[1] is irrelevant to
// L3_decode_scalefactors (it only tests h[1]&0x08 and h[3]'s mode bits).
func mpeg1Hdr(modeExt byte) [4]byte { return [4]byte{0xFF, 0xFB, 0x00, modeExt} }

// mpeg2Hdr returns a 4-byte header with the MPEG-1 bit clear (MPEG-2/2.5),
// selecting the mixed-radix g_mod scalefactor-size branch.
func mpeg2Hdr(modeExt byte) [4]byte { return [4]byte{0xFF, 0xF3, 0x00, modeExt} }

// TestL3DecodeScalefactorsParity drives the Go L3DecodeScalefactors and the C
// L3_decode_scalefactors over identical inputs and asserts every expanded float
// gain plus the final bit position match bit-for-bit. The corpus spans both
// version branches and the gain-adjustment paths:
//   - MPEG-1 long-block (g_scfc_decode scalefactor sizes, no short bands);
//   - MPEG-1 short-block (n_short_sfb != 0 -> subblock-gain shift expansion);
//   - MPEG-1 preflag (n_short_sfb == 0 & preflag -> g_preamp high-freq boost);
//   - MS-stereo (gain_exp -= 2);
//   - MPEG-2/2.5 mixed-radix g_mod scalefactor-size decode (scfsi forced -16);
//   - MPEG-2 intensity-stereo on the right channel (HDR_TEST_I_STEREO && ch ->
//     scalefac_compress >> 1 and the g_mod ist offset); and
//   - a global-gain sweep so the L3_ldexp_q2 negative-shift seed exponent is
//     exercised.
func TestL3DecodeScalefactorsParity(t *testing.T) {
	requireStrict(t)

	const msMode = 0x60     // h[3] & 0xE0 == 0x60 -> HDR_IS_MS_STEREO
	const iStereoBit = 0x10 // h[3] & 0x10 -> HDR_TEST_I_STEREO

	specs := []decodeSpec{
		{
			name:             "mpeg1-long",
			hdr:              mpeg1Hdr(0x00),
			scalefacCompress: 5,
			globalGain:       180,
			scalefacScale:    0,
			nLongSfb:         22,
			seed:             10,
		},
		{
			name:             "mpeg1-long-scale1",
			hdr:              mpeg1Hdr(0x00),
			scalefacCompress: 10,
			globalGain:       210,
			scalefacScale:    1,
			nLongSfb:         22,
			seed:             11,
		},
		{
			name:             "mpeg1-short-subblockgain",
			hdr:              mpeg1Hdr(0x00),
			scalefacCompress: 7,
			globalGain:       150,
			scalefacScale:    0,
			nLongSfb:         0,
			nShortSfb:        39,
			subblockGain:     [3]uint8{2, 1, 3},
			seed:             12,
		},
		{
			name:             "mpeg1-mixed-short",
			hdr:              mpeg1Hdr(0x00),
			scalefacCompress: 9,
			globalGain:       128,
			scalefacScale:    1,
			nLongSfb:         8,
			nShortSfb:        30,
			subblockGain:     [3]uint8{1, 0, 2},
			seed:             13,
		},
		{
			name:             "mpeg1-preflag",
			hdr:              mpeg1Hdr(0x00),
			scalefacCompress: 3,
			globalGain:       200,
			scalefacScale:    0,
			nLongSfb:         21,
			preflag:          1,
			seed:             14,
		},
		{
			name:             "mpeg1-ms-stereo",
			hdr:              mpeg1Hdr(msMode),
			scalefacCompress: 5,
			globalGain:       175,
			scalefacScale:    0,
			nLongSfb:         22,
			ch:               1,
			seed:             15,
		},
		{
			name:             "mpeg1-bspos-unaligned",
			hdr:              mpeg1Hdr(0x00),
			scalefacCompress: 6,
			globalGain:       190,
			nLongSfb:         22,
			seed:             16,
			bsPos:            3,
		},
		{
			name:             "mpeg2-long-ch0",
			hdr:              mpeg2Hdr(0x00),
			scalefacCompress: 120,
			globalGain:       170,
			scalefacScale:    0,
			nLongSfb:         21,
			seed:             20,
		},
		{
			name:             "mpeg2-istereo-ch1",
			hdr:              mpeg2Hdr(iStereoBit),
			scalefacCompress: 200,
			globalGain:       160,
			scalefacScale:    0,
			nLongSfb:         21,
			ch:               1,
			seed:             21,
		},
		{
			name:             "mpeg2-short",
			hdr:              mpeg2Hdr(0x00),
			scalefacCompress: 80,
			globalGain:       140,
			scalefacScale:    1,
			nLongSfb:         0,
			nShortSfb:        39,
			subblockGain:     [3]uint8{1, 2, 0},
			seed:             22,
		},
		{
			name:             "globalgain-low", // exp_q2 seed reaches the negative-shift caveat
			hdr:              mpeg1Hdr(0x00),
			scalefacCompress: 0,
			globalGain:       255,
			nLongSfb:         22,
			seed:             30,
		},
		{
			name:             "globalgain-high",
			hdr:              mpeg1Hdr(0x00),
			scalefacCompress: 0,
			globalGain:       0,
			nLongSfb:         22,
			seed:             31,
		},
	}

	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			payload := makeRandomBytes(s.seed, payloadBytes)

			istInit := make([]byte, 39)
			for i := range istInit {
				istInit[i] = byte(0x40 + i)
			}

			hdr := s.hdr // addressable copy

			// C side.
			cHdr := hdr
			cIst := append([]byte(nil), istInit...)
			cPayload := append([]byte(nil), payload...)
			cScf, cPos := cgoL3DecodeScalefactors(cHdr[:], cIst, cPayload, s.bsPos,
				s.scalefacCompress, s.globalGain, s.scalefacScale, s.nLongSfb, s.nShortSfb,
				s.subblockGain, s.preflag, s.scfsi, s.ch)

			// Go side.
			goHdr := hdr
			goIst := append([]byte(nil), istInit...)
			goPayload := append([]byte(nil), payload...)
			var bs nativemp3.BitStream
			nativemp3.BsInit(&bs, goPayload, len(goPayload))
			bs.Pos = s.bsPos
			gr := &nativemp3.L3GrInfo{
				ScalefacCompress: s.scalefacCompress,
				GlobalGain:       s.globalGain,
				ScalefacScale:    s.scalefacScale,
				NLongSfb:         s.nLongSfb,
				NShortSfb:        s.nShortSfb,
				SubblockGain:     s.subblockGain,
				Preflag:          s.preflag,
				Scfsi:            s.scfsi,
			}
			nbands := int(s.nLongSfb) + int(s.nShortSfb)
			goScf := make([]float32, nbands)
			nativemp3.L3DecodeScalefactors(goHdr[:], goIst, &bs, gr, goScf, s.ch)

			require.Equalf(t, cPos, bs.Pos, "final bs.pos")
			assert.Equalf(t, cIst, goIst, "ist_pos[] after decode")
			for i := 0; i < nbands; i++ {
				assert.Equalf(t, cScf[i], goScf[i], "scf[%d]", i)
			}
		})
	}
}
