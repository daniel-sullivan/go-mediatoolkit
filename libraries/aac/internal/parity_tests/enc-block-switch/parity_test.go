// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encblockswitch

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// granuleLength is the AAC-LC frame length in time samples (nTimeSamples ==
// 1024); psy_main.cpp:486 passes this to FDKaacEnc_BlockSwitching.
const granuleLength = 1024

// goSnapshot projects the pure-Go BlockSwitchingControl into the same flat shape
// as the C bs_snapshot so the two can be compared field-for-field.
func goSnapshot(b *nativeaac.BlockSwitchingControl) cSnapshot {
	var o cSnapshot
	o.lastWindowSequence = int32(b.LastWindowSequence)
	o.windowShape = int32(b.WindowShape)
	o.lastWindowShape = int32(b.LastWindowShape)
	o.nBlockSwitchWindows = uint32(b.NBlockSwitchWindows)
	o.attack = int32(b.Attack)
	o.lastattack = int32(b.Lastattack)
	o.attackIndex = int32(b.AttackIndex)
	o.lastAttackIndex = int32(b.LastAttackIndex)
	o.allowShortFrames = int32(b.AllowShortFrames)
	o.allowLookAhead = int32(b.AllowLookAhead)
	o.noOfGroups = int32(b.NoOfGroups)
	for i := 0; i < 4; i++ {
		o.groupLen[i] = int32(b.GroupLen[i])
	}
	o.maxWindowNrg = b.MaxWindowNrg
	for j := 0; j < 2; j++ {
		for i := 0; i < 8; i++ {
			o.windowNrg[j][i] = b.WindowNrg[j][i]
			o.windowNrgF[j][i] = b.WindowNrgF[j][i]
		}
	}
	o.accWindowNrg = b.AccWindowNrg
	for i := 0; i < 2; i++ {
		o.iirStates[i] = b.IIRStates[i]
	}
	return o
}

// requireSnapEqual asserts every BLOCK_SWITCHING_CONTROL state field matches
// bit-for-bit between the C oracle and the Go port.
func requireSnapEqual(t *testing.T, want, got cSnapshot, ctx string) {
	t.Helper()
	require.Equal(t, want.lastWindowSequence, got.lastWindowSequence, "%s: lastWindowSequence", ctx)
	require.Equal(t, want.windowShape, got.windowShape, "%s: windowShape", ctx)
	require.Equal(t, want.lastWindowShape, got.lastWindowShape, "%s: lastWindowShape", ctx)
	require.Equal(t, want.nBlockSwitchWindows, got.nBlockSwitchWindows, "%s: nBlockSwitchWindows", ctx)
	require.Equal(t, want.attack, got.attack, "%s: attack", ctx)
	require.Equal(t, want.lastattack, got.lastattack, "%s: lastattack", ctx)
	require.Equal(t, want.attackIndex, got.attackIndex, "%s: attackIndex", ctx)
	require.Equal(t, want.lastAttackIndex, got.lastAttackIndex, "%s: lastAttackIndex", ctx)
	require.Equal(t, want.allowShortFrames, got.allowShortFrames, "%s: allowShortFrames", ctx)
	require.Equal(t, want.allowLookAhead, got.allowLookAhead, "%s: allowLookAhead", ctx)
	require.Equal(t, want.noOfGroups, got.noOfGroups, "%s: noOfGroups", ctx)
	require.Equal(t, want.groupLen, got.groupLen, "%s: groupLen", ctx)
	require.Equal(t, want.maxWindowNrg, got.maxWindowNrg, "%s: maxWindowNrg", ctx)
	require.Equal(t, want.windowNrg, got.windowNrg, "%s: windowNrg", ctx)
	require.Equal(t, want.windowNrgF, got.windowNrgF, "%s: windowNrgF", ctx)
	require.Equal(t, want.accWindowNrg, got.accWindowNrg, "%s: accWindowNrg", ctx)
	require.Equal(t, want.iirStates, got.iirStates, "%s: iirStates", ctx)
}

// TestConstants cross-checks the FL2FXCONST compile-time constants embedded in
// the Go port against the genuine C macro output, and asserts SINETABLE_16BIT is
// ON on the build platform (aarch64 → FDK_archdef.h promotes __aarch64__ to
// __arm__ + __ARM_ARCH_8__): hiPassCoeff and the two attack thresholds are
// FIXP_SGL (int16) and the arm fix-mul overrides apply — the port's documented
// build-config assumption.
func TestConstants(t *testing.T) {
	require.Equal(t, 1, cSineTable16Bit(),
		"SINETABLE_16BIT must be ON on this platform (the port assumes FIXP_SGL coefficients)")

	c := cConsts()
	// These are the literals embedded in block_switch.go (FIXP_SGL for the
	// coefficient/threshold, FIXP_DBL for accWindowNrgFac/minAttackNrg/tenConst).
	require.Equal(t, int32(-16695), c[0], "hiPassCoeff[0]")
	require.Equal(t, int32(24733), c[1], "hiPassCoeff[1]")
	require.Equal(t, int32(644245120), c[2], "accWindowNrgFac")
	require.Equal(t, int32(22938), c[3], "oneMinusAccWindowNrgFac")
	require.Equal(t, int32(3277), c[4], "invAttackRatio")
	require.Equal(t, int32(15625), c[5], "minAttackNrg")
	require.Equal(t, int32(10<<(32-1-4)), c[6], "tenConst")
}

// frameKind enumerates the test-signal shapes that drive the energy/attack
// detector into different window decisions.
type frameKind int

const (
	kindSilence    frameKind = iota // near-zero energy → below minAttackNrg
	kindSteady                      // sustained sine → no attack
	kindAttack                      // sharp onset mid-frame → attack
	kindLateAttack                  // onset in the last window → spreads over border
	kindNoise                       // random → exercises the IIR + accumulators
)

// makeFrame deterministically synthesises a granuleLength-long int16 frame for
// the given kind, seeded by frame index so the sequence is reproducible.
func makeFrame(kind frameKind, idx int) []int16 {
	out := make([]int16, granuleLength)
	rng := rand.New(rand.NewPCG(uint64(idx)+1, 0xA5A5))
	switch kind {
	case kindSilence:
		for i := range out {
			out[i] = int16(rng.IntN(3) - 1) // -1..1
		}
	case kindSteady:
		for i := range out {
			// sustained ramp-y waveform, modest amplitude
			out[i] = int16(((i*97 + idx*131) % 4001) - 2000)
		}
	case kindAttack:
		for i := range out {
			if i < granuleLength/2 {
				out[i] = int16(rng.IntN(5) - 2)
			} else {
				// loud onset second half
				out[i] = int16(((i * 211) % 20001) - 10000)
			}
		}
	case kindLateAttack:
		for i := range out {
			if i < granuleLength*7/8 {
				out[i] = int16(rng.IntN(5) - 2)
			} else {
				out[i] = int16(((i * 307) % 24001) - 12000)
			}
		}
	case kindNoise:
		for i := range out {
			out[i] = int16(rng.IntN(0x10000) - 0x8000)
		}
	}
	return out
}

// monoSequence exercises init → a stateful run of frames that walks every block
// decision path: startup long, attack (long→start, then short), sustained,
// late-attack (frame-border spread), back to silence (short→stop→long).
var monoSequence = []frameKind{
	kindSilence,
	kindSteady,
	kindAttack,
	kindAttack,
	kindSteady,
	kindLateAttack,
	kindSilence,
	kindSteady,
	kindNoise,
	kindNoise,
	kindSilence,
}

// TestBlockSwitchingMono drives the genuine C kernel and the Go port through the
// same stateful frame sequence (LC config, isLowDelay=0) and asserts the full
// control state matches bit-for-bit after init and after every frame.
func TestBlockSwitchingMono(t *testing.T) {
	cs := cNewState()
	defer cs.free()
	var gb nativeaac.BlockSwitchingControl

	requireSnapEqual(t, cs.cInit(0), func() cSnapshot {
		nativeaac.InitBlockSwitching(&gb, 0)
		return goSnapshot(&gb)
	}(), "init")

	for n, kind := range monoSequence {
		frame := makeFrame(kind, n)
		cRC, cSnap := cs.cBlockSwitch(granuleLength, 0, frame)
		gRC := nativeaac.BlockSwitching(&gb, granuleLength, 0, frame)
		require.Equal(t, cRC, gRC, "frame %d rc", n)
		requireSnapEqual(t, cSnap, goSnapshot(&gb), "frame "+kindName(kind)+itoa(n))
	}
}

// TestBlockSwitchingLFE verifies the LFE shortcut (always LONG/SINE, one group).
func TestBlockSwitchingLFE(t *testing.T) {
	cs := cNewState()
	defer cs.free()
	var gb nativeaac.BlockSwitchingControl
	cs.cInit(0)
	nativeaac.InitBlockSwitching(&gb, 0)

	for n := 0; n < 4; n++ {
		frame := makeFrame(kindAttack, n)
		cRC, cSnap := cs.cBlockSwitch(granuleLength, 1 /*isLFE*/, frame)
		gRC := nativeaac.BlockSwitching(&gb, granuleLength, 1, frame)
		require.Equal(t, cRC, gRC, "lfe frame %d rc", n)
		requireSnapEqual(t, cSnap, goSnapshot(&gb), "lfe frame "+itoa(n))
	}
}

// TestBlockSwitchingLowDelay drives the LD config (isLowDelay=1: 4 windows, no
// short frames, no look-ahead → the chgWndSq state machine).
func TestBlockSwitchingLowDelay(t *testing.T) {
	cs := cNewState()
	defer cs.free()
	var gb nativeaac.BlockSwitchingControl

	requireSnapEqual(t, cs.cInit(1), func() cSnapshot {
		nativeaac.InitBlockSwitching(&gb, 1)
		return goSnapshot(&gb)
	}(), "ld-init")

	for n, kind := range monoSequence {
		frame := makeFrame(kind, n+100)
		cRC, cSnap := cs.cBlockSwitch(granuleLength, 0, frame)
		gRC := nativeaac.BlockSwitching(&gb, granuleLength, 0, frame)
		require.Equal(t, cRC, gRC, "ld frame %d rc", n)
		requireSnapEqual(t, cSnap, goSnapshot(&gb), "ld frame "+itoa(n))
	}
}

// stereoFrameKinds pairs differing left/right kinds per frame so the sync logic
// (common-window block-type fold + grouping selection) is exercised across
// long/short/start/stop combinations.
var stereoFrameKinds = [][2]frameKind{
	{kindSilence, kindSilence},
	{kindAttack, kindSteady},
	{kindAttack, kindAttack},
	{kindSteady, kindAttack},
	{kindLateAttack, kindSteady},
	{kindSilence, kindLateAttack},
	{kindNoise, kindSteady},
	{kindSilence, kindSilence},
}

// TestSyncBlockSwitchingStereo drives a stereo pair through BlockSwitching on
// each channel then SyncBlockSwitching (common-window and non-common-window),
// asserting both controls match bit-for-bit and the rc agrees.
func TestSyncBlockSwitchingStereo(t *testing.T) {
	for _, commonWindow := range []int{1, 0} {
		commonWindow := commonWindow
		t.Run(map[int]string{1: "common", 0: "indep"}[commonWindow], func(t *testing.T) {
			cl, cr := cNewState(), cNewState()
			defer cl.free()
			defer cr.free()
			var gl, gr nativeaac.BlockSwitchingControl

			cl.cInit(0)
			cr.cInit(0)
			nativeaac.InitBlockSwitching(&gl, 0)
			nativeaac.InitBlockSwitching(&gr, 0)

			for n, kinds := range stereoFrameKinds {
				lf := makeFrame(kinds[0], n*2)
				rf := makeFrame(kinds[1], n*2+1)

				crcL, _ := cl.cBlockSwitch(granuleLength, 0, lf)
				crcR, _ := cr.cBlockSwitch(granuleLength, 0, rf)
				grcL := nativeaac.BlockSwitching(&gl, granuleLength, 0, lf)
				grcR := nativeaac.BlockSwitching(&gr, granuleLength, 0, rf)
				require.Equal(t, crcL, grcL, "stereo frame %d L rc", n)
				require.Equal(t, crcR, grcR, "stereo frame %d R rc", n)

				cRC, cSnapL, cSnapR := cSync(cl, cr, 2, commonWindow)
				gRC := nativeaac.SyncBlockSwitching(&gl, &gr, 2, commonWindow)
				require.Equal(t, cRC, gRC, "stereo frame %d sync rc", n)
				requireSnapEqual(t, cSnapL, goSnapshot(&gl), "stereo frame "+itoa(n)+" L")
				requireSnapEqual(t, cSnapR, goSnapshot(&gr), "stereo frame "+itoa(n)+" R")
			}
		})
	}
}

// TestSyncBlockSwitchingMono verifies the mono sync path normalises grouping.
func TestSyncBlockSwitchingMono(t *testing.T) {
	cs := cNewState()
	defer cs.free()
	var gb nativeaac.BlockSwitchingControl
	cs.cInit(0)
	nativeaac.InitBlockSwitching(&gb, 0)

	for n, kind := range monoSequence {
		frame := makeFrame(kind, n+200)
		cs.cBlockSwitch(granuleLength, 0, frame)
		nativeaac.BlockSwitching(&gb, granuleLength, 0, frame)

		cRC, cSnap, _ := cSync(cs, nil, 1, 0)
		gRC := nativeaac.SyncBlockSwitching(&gb, nil, 1, 0)
		require.Equal(t, cRC, gRC, "mono sync frame %d rc", n)
		requireSnapEqual(t, cSnap, goSnapshot(&gb), "mono sync frame "+itoa(n))
	}
}

// kindName labels a frameKind for failure messages.
func kindName(k frameKind) string {
	switch k {
	case kindSilence:
		return "silence#"
	case kindSteady:
		return "steady#"
	case kindAttack:
		return "attack#"
	case kindLateAttack:
		return "lateAttack#"
	case kindNoise:
		return "noise#"
	}
	return "?#"
}

// itoa is a tiny non-allocating-ish int formatter for test context strings.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
