// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package frameencodedispatch

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 frame-encode dispatcher
// (frame_encode.go: EncodeMP3Frame / lameEncodeFrameInit, ported 1:1 from LAME
// 3.100 encoder.c lame_encode_mp3_frame / lame_encode_frame_init) against the
// vendored LAME C reference (oracle.c, which #includes encoder.c). Both sides
// run over identical fabricated session config, padding state, PE-smoothing
// history and per-granule psy-model outputs (fed through the stub stage seam),
// and the dispatcher's full observable state — the padding flag and slot_lag,
// the chosen mode_ext, the rolled PE-firbuf, the per-granule block-type flags,
// the frame counter, the latched frame-init flag, and the primebuff the
// filterbank prime shifts — must be bit-for-bit equal.
//
// The dispatcher's M/S energy ratio, M/S-vs-L/R PE sums and CBR/ABR smoothing
// FIR are single-precision FP whose last ULP depends on FMA fusing, so the
// bit-exact assertions are gated behind nativemp3.StrictMode per the FP-parity
// convention: a bare `go test` is clean and the strict run (mp3_strict + mp3lame
// + the FP CGO env, via mise //libraries/mp3:parity) is the authoritative gate.

// MPEG_mode / vbr_mode / MPG_MD_* / force/tag flag values mirrored from the LAME
// headers (include/lame.h enum MPEG_mode_e / vbr_mode_e, encoder.h). The
// nativemp3 equivalents are unexported, so the test names the integer values
// directly, exactly as the C enum resolves them.
const (
	modeStereo      = 0 // STEREO
	modeJointStereo = 1 // JOINT_STEREO
	modeMono        = 3 // MONO

	vbrOff  = 0 // vbr_off
	vbrMt   = 1 // vbr_mt
	vbrRh   = 2 // vbr_rh
	vbrAbr  = 3 // vbr_abr
	vbrMtrh = 4 // vbr_mtrh

	mpgMdLRLR = 0 // MPG_MD_LR_LR
	mpgMdMSLR = 2 // MPG_MD_MS_LR

	bFalse = 0
	bTrue  = 1
)

func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags='mp3lame mp3_strict' (FP env via mise //libraries/mp3:parity)")
	}
}

// scenario is one fully-specified frame-encode input: the session config, the
// padding accumulator state, the PE-smoothing history, and the per-granule
// fabricated psy-model outputs.
type scenario struct {
	samplerateOut int
	channelsOut   int
	modeGr        int
	mode          int
	forceMS       int
	vbr           int
	writeLameTag  int
	frameInit     int // 1 = skip the filterbank prime (no input PCM needed)

	fracSpF int
	slotLag int

	pefirbuf [19]float32
	psy      [2]psyOut

	// inLen is the per-channel input PCM length when frameInit==0 (the prime
	// shifts the input). Zero when the prime is pre-latched.
	inLen  int
	inputL []float32
	inputR []float32

	// armCapture records the filterbank prime's shifted primebuff (only the
	// prime test sets this; otherwise the Stage-2 frame mdct, whose inbuf may be
	// nil, must not be copied).
	armCapture bool
}

// setZeroInput gives both drivers a zero PCM buffer long enough that the
// dispatcher's inbuf[ch][576 + gr*576 - FFTOFFSET:] slice and the Stage-2 mdct's
// full-inbuf pass stay in range (the stubs ignore the contents). Used by the
// pre-latched-prime tests that drive the drivers directly.
func setZeroInput(c *cgoFE, n *nativeFE, modeGr int) {
	need := 576*(1+modeGr) + 4096
	buf := make([]float32, need)
	c.allocInput(need)
	c.setInput(0, buf)
	c.setInput(1, buf)
	n.setInput(0, buf)
	n.setInput(1, buf)
}

// driveBoth configures both the cgo oracle and the native port from a scenario,
// runs one frame through each, and returns the two drivers for comparison.
func driveBoth(t *testing.T, s *scenario) (*cgoFE, *nativeFE, int, int) {
	t.Helper()

	c := newCgoFE()
	c.resetCapture()
	c.setCfg(s.samplerateOut, s.channelsOut, s.modeGr, s.mode, s.forceMS, s.vbr, s.writeLameTag)
	c.setPad(s.fracSpF, s.slotLag)
	c.setPefirbuf(s.pefirbuf)
	c.setFrameInit(s.frameInit)

	n := newNativeFE()
	n.setCfg(s.samplerateOut, s.channelsOut, s.modeGr, s.mode, s.forceMS, s.vbr, s.writeLameTag)
	n.setPad(s.fracSpF, s.slotLag)
	n.setPefirbuf(s.pefirbuf)
	n.setFrameInit(s.frameInit)

	for gr := 0; gr < s.modeGr; gr++ {
		c.setPsy(gr, s.psy[gr].pe, s.psy[gr].peMS, s.psy[gr].totEner, s.psy[gr].blocktype)
		n.setPsy(gr, s.psy[gr].pe, s.psy[gr].peMS, s.psy[gr].totEner, s.psy[gr].blocktype)
	}

	// The dispatcher slices inbuf[ch][576 + gr*576 - FFTOFFSET:] for the psy
	// window and passes the full inbuf to the Stage-2 mdct. The stubs ignore the
	// PCM contents, but the slice expression itself must be in-range, so both
	// sides always get a buffer long enough for the deepest window start. When a
	// test supplies no input (frameInit pre-latched), synthesize a zero buffer.
	inputL, inputR := s.inputL, s.inputR
	if len(inputL) == 0 {
		// 576 + (mode_gr-1)*576 - FFTOFFSET is the deepest psy-window start; pad
		// generously past it so inbuf[ch][start:] is always valid.
		need := 576*(1+s.modeGr) + 4096
		inputL = make([]float32, need)
		inputR = make([]float32, need)
	}

	if s.frameInit == 0 {
		c.allocInput(s.inLen)
		c.setInput(0, s.inputL)
		c.setInput(1, s.inputR)
	} else {
		// Pre-latched prime: still give C a real (zero) buffer so its inbuf
		// pointer arithmetic and the Go slice expression stay symmetric.
		c.allocInput(len(inputL))
		c.setInput(0, inputL)
		c.setInput(1, inputR)
	}
	n.setInput(0, inputL)
	n.setInput(1, inputR)

	if s.armCapture {
		c.armCapture()
		n.armCapture()
	}

	rc := c.encode()
	rn := n.encode()
	return c, n, rc, rn
}

// assertState compares every observable field of the two dispatchers.
func assertState(t *testing.T, s *scenario, c *cgoFE, n *nativeFE, rc, rn int) {
	t.Helper()
	assert.Equal(t, rc, rn, "encode return value")
	assert.Equal(t, c.padding(), n.padding(), "padding")
	assert.Equal(t, c.slotLag(), n.slotLag(), "slot_lag")
	assert.Equal(t, c.modeExt(), n.modeExt(), "mode_ext")
	assert.Equal(t, c.frameNumber(), n.frameNumber(), "frame_number")
	assert.Equal(t, c.frameInit(), n.frameInit(), "lame_encode_frame_init")

	for i := 0; i < 19; i++ {
		assert.Equal(t, math.Float32bits(c.pefirbuf(i)), math.Float32bits(n.pefirbuf(i)),
			"pefirbuf[%d]", i)
	}
	for gr := 0; gr < s.modeGr; gr++ {
		for ch := 0; ch < s.channelsOut; ch++ {
			assert.Equal(t, c.blockType(gr, ch), n.blockType(gr, ch), "block_type[%d][%d]", gr, ch)
			assert.Equal(t, c.mixedBlockFlag(gr, ch), n.mixedBlockFlag(gr, ch),
				"mixed_block_flag[%d][%d]", gr, ch)
		}
	}
}

// randPsy fabricates one granule's psy-model output. PE values span the
// magnitudes the real model produces (hundreds to thousands) so the M/S vs L/R
// comparison and the smoothing FIR exercise realistic float32 rounding.
func randPsy(rng *rand.Rand) psyOut {
	var o psyOut
	for ch := 0; ch < 2; ch++ {
		o.pe[ch] = float32(rng.Float64() * 4000)
		o.peMS[ch] = float32(rng.Float64() * 4000)
		o.blocktype[ch] = []int{0, 1, 2, 3}[rng.IntN(4)] // NORM/START/SHORT/STOP
	}
	for k := 0; k < 4; k++ {
		o.totEner[k] = float32(rng.Float64() * 1e6)
	}
	return o
}

func randPefirbuf(rng *rand.Rand) [19]float32 {
	var b [19]float32
	for i := range b {
		b[i] = float32(rng.Float64() * 6000)
	}
	return b
}

// TestDispatchParity sweeps the dispatcher's full decision space with the
// filterbank prime pre-latched (frameInit=1), so the focus is the padding
// accumulator, the M/S decision and the PE-smoothing FIR. It covers MPEG-1
// (mode_gr=2) and MPEG-2 (mode_gr=1), mono / stereo / joint-stereo, force_ms,
// and every vbr mode (vbr_off / vbr_abr run the FIR; vbr_rh / vbr_mtrh skip it).
func TestDispatchParity(t *testing.T) {
	requireStrict(t)

	type cfgCase struct {
		channelsOut int
		modeGr      int
		mode        int
		forceMS     int
		vbr         int
	}
	cfgs := []cfgCase{
		{2, 2, modeJointStereo, 0, vbrOff},
		{2, 2, modeJointStereo, 0, vbrAbr},
		{2, 2, modeJointStereo, 0, vbrRh},
		{2, 2, modeJointStereo, 0, vbrMtrh},
		{2, 2, modeJointStereo, 0, vbrMt},
		{2, 2, modeJointStereo, 1, vbrOff}, // force_ms
		{2, 2, modeStereo, 0, vbrOff},
		{2, 2, modeStereo, 0, vbrAbr},
		{1, 2, modeMono, 0, vbrOff},
		{1, 2, modeMono, 0, vbrAbr},
		{2, 1, modeJointStereo, 0, vbrOff}, // MPEG-2 (mode_gr=1)
		{2, 1, modeJointStereo, 0, vbrAbr},
		{1, 1, modeMono, 0, vbrOff},
	}

	for ci, cf := range cfgs {
		cf := cf
		for seed := uint64(0); seed < 48; seed++ {
			rng := rand.New(rand.NewPCG(uint64(ci)<<32|seed, 0x9e3779b97f4a7c15))
			s := &scenario{
				samplerateOut: 44100,
				channelsOut:   cf.channelsOut,
				modeGr:        cf.modeGr,
				mode:          cf.mode,
				forceMS:       cf.forceMS,
				vbr:           cf.vbr,
				writeLameTag:  rng.IntN(2),
				frameInit:     1,
				fracSpF:       rng.IntN(1000),
				slotLag:       rng.IntN(2000) - 500, // straddle the < 0 padding boundary
				pefirbuf:      randPefirbuf(rng),
			}
			for gr := 0; gr < cf.modeGr; gr++ {
				s.psy[gr] = randPsy(rng)
			}
			c, n, rc, rn := driveBoth(t, s)
			assertState(t, s, c, n, rc, rn)
			c.free()
		}
	}
}

// TestMSDecisionTieBreak forces the JOINT_STEREO sum_pe_MS <= sum_pe_LR
// comparison to land near equality (and exercises the block-type agreement
// check) so the M/S vs L/R selection boundary is pinned, not just random
// interiors.
func TestMSDecisionTieBreak(t *testing.T) {
	requireStrict(t)

	for seed := uint64(0); seed < 64; seed++ {
		rng := rand.New(rand.NewPCG(seed, 0xc2b2ae3d27d4eb4f))
		s := &scenario{
			samplerateOut: 48000,
			channelsOut:   2,
			modeGr:        2,
			mode:          modeJointStereo,
			vbr:           vbrOff,
			frameInit:     1,
			fracSpF:       rng.IntN(500),
			slotLag:       rng.IntN(1000),
			pefirbuf:      randPefirbuf(rng),
		}
		// Make the MS and LR PE sums nearly equal so the <= comparison is
		// sensitive to the last ULP of each float32 accumulation.
		base := float32(rng.Float64() * 1000)
		for gr := 0; gr < 2; gr++ {
			var o psyOut
			for ch := 0; ch < 2; ch++ {
				o.pe[ch] = base + float32(rng.Float64()-0.5)
				o.peMS[ch] = base + float32(rng.Float64()-0.5)
				// Bias block types toward agreement so the splice into MS depends
				// on the PE comparison rather than always failing the type check.
				bt := rng.IntN(4)
				o.blocktype[ch] = bt
			}
			for k := 0; k < 4; k++ {
				o.totEner[k] = float32(rng.Float64() * 1e6)
			}
			s.psy[gr] = o
		}
		c, n, rc, rn := driveBoth(t, s)
		assertState(t, s, c, n, rc, rn)
		c.free()
	}
}

// TestMSEnerRatioZero pins the JOINT_STEREO ms_ener_ratio branch where
// tot_ener[2]+tot_ener[3] == 0 (the `> 0` guard skips the divide), and the
// non-zero divide path, so both legs of encoder.c:381-384 are covered.
func TestMSEnerRatioZero(t *testing.T) {
	requireStrict(t)

	for _, zero := range []bool{true, false} {
		s := &scenario{
			samplerateOut: 44100,
			channelsOut:   2,
			modeGr:        2,
			mode:          modeJointStereo,
			vbr:           vbrAbr,
			frameInit:     1,
			fracSpF:       320,
			slotLag:       10,
		}
		for gr := 0; gr < 2; gr++ {
			o := psyOut{
				pe:        [2]float32{1500, 1600},
				peMS:      [2]float32{1400, 1550},
				blocktype: [2]int{0, 0},
			}
			if !zero {
				o.totEner = [4]float32{0, 0, 1234.5, 6789.0}
			}
			s.psy[gr] = o
		}
		c, n, rc, rn := driveBoth(t, s)
		assertState(t, s, c, n, rc, rn)
		c.free()
	}
}

// TestFrameInitPrimeParity drives the first frame (frameInit=0) so the
// filterbank prime (lame_encode_frame_init) runs: it verifies the latch flips,
// the per-granule SHORT_TYPE force, and the shifted primebuff fed to mdct_sub48
// match bit-for-bit between the two sides, plus the dispatcher state after.
func TestFrameInitPrimeParity(t *testing.T) {
	requireStrict(t)

	for _, cf := range []struct {
		channelsOut int
		modeGr      int
		mode        int
	}{
		{2, 2, modeJointStereo},
		{2, 1, modeJointStereo}, // MPEG-2
		{1, 2, modeMono},
		{1, 1, modeMono},
	} {
		rng := rand.New(rand.NewPCG(uint64(cf.channelsOut*10+cf.modeGr), 0xdeadbeef))

		// The prime reads inbuf[ch][0 .. 286 + 576*(1+mode_gr) - 1 - framesize]
		// and the dispatcher then reads the psy window + mdct over the frame, so
		// supply a generous input. framesize = 576*mode_gr; the prime loop bound
		// is 286 + 576*(1+mode_gr). The psy window reads up to
		// 576 + (mode_gr-1)*576 + 576 - FFTOFFSET + BLKSIZE; provide ample slack.
		const inLen = 286 + 1152 + 576 + 4096
		s := &scenario{
			samplerateOut: 44100,
			channelsOut:   cf.channelsOut,
			modeGr:        cf.modeGr,
			mode:          cf.mode,
			vbr:           vbrOff,
			frameInit:     0,
			fracSpF:       417,
			slotLag:       0,
			inLen:         inLen,
			armCapture:    true,
		}
		s.inputL = make([]float32, inLen)
		s.inputR = make([]float32, inLen)
		for i := 0; i < inLen; i++ {
			s.inputL[i] = float32(rng.Float64()*2 - 1)
			s.inputR[i] = float32(rng.Float64()*2 - 1)
		}
		for gr := 0; gr < cf.modeGr; gr++ {
			s.psy[gr] = randPsy(rng)
		}

		c, n, rc, rn := driveBoth(t, s)

		// Latch flipped on both sides.
		require.Equal(t, 1, c.frameInit(), "C frame_init latched")
		require.Equal(t, 1, n.frameInit(), "Go frame_init latched")
		// The prime calls mdct_sub48 once; the frame's Stage-2 mdct calls it
		// again, so both sides see exactly two mdct calls.
		assert.Equal(t, c.mdctCalls(), n.mdctCalls(), "mdct_sub48 call count")

		// The shifted primebuff (the first mdct_sub48 argument) must match
		// bit-for-bit across the full 286+1152+576 span.
		const primeLen = 286 + 1152 + 576
		for i := 0; i < primeLen; i++ {
			assert.Equal(t, math.Float32bits(c.prime0(i)), math.Float32bits(n.prime0(i)),
				"primebuff0[%d]", i)
			if cf.channelsOut == 2 {
				assert.Equal(t, math.Float32bits(c.prime1(i)), math.Float32bits(n.prime1(i)),
					"primebuff1[%d]", i)
			}
		}

		assertState(t, s, c, n, rc, rn)
		c.free()
	}
}

// TestPaddingAccumulatorParity isolates the integer padding accumulator across a
// run of frames (carrying slot_lag forward), so the slot_lag -= frac_SpF /
// wrap-on-negative logic is pinned over many iterations, not just one frame.
func TestPaddingAccumulatorParity(t *testing.T) {
	requireStrict(t)

	c := newCgoFE()
	c.resetCapture() // clears the C-global psy-ret / capture state
	c.setCfg(44100, 2, 2, modeStereo, 0, vbrOff, 0)
	c.setPad(1152*2%44100, 0) // a realistic frac_SpF-ish seed
	c.setFrameInit(1)

	n := newNativeFE()
	n.setCfg(44100, 2, 2, modeStereo, 0, vbrOff, 0)
	n.setPad(1152*2%44100, 0)
	n.setFrameInit(1)

	// The dispatcher slices inbuf[ch][..:] for the psy window; give both sides a
	// zero buffer long enough that the slice expression stays in range.
	setZeroInput(c, n, 2)

	// Identical psy outputs for every frame; only the padding state evolves.
	po := psyOut{pe: [2]float32{1000, 1100}, peMS: [2]float32{900, 1050}, blocktype: [2]int{0, 0}}
	for gr := 0; gr < 2; gr++ {
		c.setPsy(gr, po.pe, po.peMS, po.totEner, po.blocktype)
		n.setPsy(gr, po.pe, po.peMS, po.totEner, po.blocktype)
	}

	for frame := 0; frame < 200; frame++ {
		rc := c.encode()
		rn := n.encode()
		assert.Equal(t, rc, rn, "frame %d return", frame)
		assert.Equal(t, c.padding(), n.padding(), "frame %d padding", frame)
		assert.Equal(t, c.slotLag(), n.slotLag(), "frame %d slot_lag", frame)
		assert.Equal(t, c.frameNumber(), n.frameNumber(), "frame %d frame_number", frame)
	}
	c.free()
}

// TestAbortParity pins the psy-model abort path: when L3psycho_anal_vbr returns
// non-zero, the dispatcher returns -4 immediately (encoder.c:378) without
// touching the later stages. The stub returns the abort code on both sides.
func TestAbortParity(t *testing.T) {
	requireStrict(t)

	c := newCgoFE()
	c.resetCapture()
	c.setCfg(44100, 2, 2, modeJointStereo, 0, vbrOff, 0)
	c.setPad(417, 100)
	c.setFrameInit(1)

	n := newNativeFE()
	n.setCfg(44100, 2, 2, modeJointStereo, 0, vbrOff, 0)
	n.setPad(417, 100)
	n.setFrameInit(1)

	setZeroInput(c, n, 2)

	po := psyOut{pe: [2]float32{1, 2}, peMS: [2]float32{3, 4}, blocktype: [2]int{0, 0}}
	for gr := 0; gr < 2; gr++ {
		c.setPsy(gr, po.pe, po.peMS, po.totEner, po.blocktype)
		n.setPsy(gr, po.pe, po.peMS, po.totEner, po.blocktype)
	}

	// Drive both psy stubs to abort.
	c.setPsyRet(1)
	n.setPsyRet(1)

	rc := c.encode()
	rn := n.encode()
	assert.Equal(t, -4, rc, "C abort returns -4")
	assert.Equal(t, rc, rn, "abort return value")
	// On abort the dispatcher still ran the padding accumulator first, so
	// padding/slot_lag are set; mode_ext / frame_number are NOT advanced.
	assert.Equal(t, c.padding(), n.padding(), "abort padding")
	assert.Equal(t, c.slotLag(), n.slotLag(), "abort slot_lag")
	assert.Equal(t, c.frameNumber(), n.frameNumber(), "abort frame_number (unchanged)")
	c.free()
}
