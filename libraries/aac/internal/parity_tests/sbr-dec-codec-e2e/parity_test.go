// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrdeccodece2e

import (
	"math"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/heaac"

	"github.com/stretchr/testify/require"
)

// TestSbrDecodeCodecInt16Parity is the codec-level HE-AAC v1 decode parity gate.
// It drives the genuine vendored fdk FULL decoder (aacDecoder_Open / ConfigRaw /
// Fill / DecodeFrame, PCM limiter disabled — the deterministic fixed-point decode
// chain) and the pure-Go heaac FULL decoder (AAC-LC core -> SBR upsample ->
// int16 narrowing) over the SAME HE-AAC AOT_SBR access-unit stream, and asserts
// the int16 PCM matches after removing fdk's whole-frame output delay.
//
// WHY THIS GATE EXISTS (residual chased to ground on the main tree).
//
// The companion sbr-dec-e2e slice already proves the SBR decode math is EXACTLY
// bit-exact int32 vs fdk when both are fed the IDENTICAL native AAC-LC core frame
// for frame. This slice closes the remaining gap: the CODEC-LEVEL int16 output of
// the whole pipeline — core decode + SBR upsample + int16 narrowing + output-delay
// alignment — vs the genuine fdk full decoder.
//
// Two things separate this full-decode comparison from the bit-exact int32 one:
//
//	(1) DECODER-DELAY MISALIGNMENT [now aligned]. fdk's full HE-AAC decoder emits
//	    its output delayed by EXACTLY ONE 2048-sample output frame relative to the
//	    native heaac decoder (fdk buffers one core frame inside the full lib before
//	    the SBR-coupled output; the GA dual-rate SBR pipeline additionally carries
//	    a reported sbrDecoder_GetDelay() == 962-sample output delay,
//	    libSBRdec/src/sbrdecoder.cpp:2006). A fine integer-sample cross-alignment
//	    over a NON-periodic chirp finds the global L1 minimum SHARPLY and
//	    unambiguously at S == 2048 (== one frame) for both mono and stereo, every
//	    rate (avgL1 ~ 2 LSB at S==2048 vs thousands at every other shift). The
//	    native decoder emits frame-immediate, so it is compared frame f against fdk
//	    frame f+1 below.
//
//	(2) An intrinsic startup STATE-SEEDING difference. Having buffered one extra
//	    core frame, fdk seeds its SBR envelope adjuster + QMF transposer with a
//	    one-frame-different history during warmup, which shows up only as a short
//	    transient at the very start of the stream (aligned frames < warmupFrames).
//
// SCOPE OF THIS GATE — content within the AAC-LC CORE BAND (below the SBR
// crossover). For such content the WHOLE pipeline is verified tight and robustly
// across every config (mono/stereo x 44100/48000/32000): aligned warmup frames
// <= warmupTol, aligned steady frames <= steadyTol (measured <= 2 LSB mono,
// <= 7 LSB stereo). The chirp here sweeps ~200 Hz upward but stays well below the
// SBR start frequency, so the SBR-reconstructed high band carries little energy
// and the int16 output is dominated by the core band.
//
// HF-NEAR-CROSSOVER content — RESOLVED, no native core bug. A prior
// investigation reported that for input with significant energy NEAR/ABOVE the
// SBR crossover (e.g. an 8 kHz tone at a 22050 Hz core rate) the native and fdk
// full int16 decodes diverge by thousands of LSB, and hypothesised a native
// AAC-LC core bug in the top half-rate scalefactor bands. That hypothesis is
// DISPROVEN. TestSbrDecodeCoreBitExactHF below pins it: it taps the GENUINE fdk
// pre-SBR AAC-LC core (the buffer handed to sbrDecoder_Apply, via the
// parity-local core tap in _fdk_local_aacdecoder_lib_tapped.cpp +
// core_tap_bridge.cpp) and asserts the native planar core is EXACTLY equal to
// it — and it IS, bit-for-bit, for HF tones at 8 kHz / 10 kHz, mono and stereo,
// once fdk's one-frame core-buffering delay is removed.
//
// The residual the prior investigation saw is therefore NOT in the core; it is
// entirely in the SBR-integration layer of fdk's FULL decoder, which (a) buffers
// the SBR bitstream one frame behind the core (bsDelay == 1, because the default
// concealment method is ConcealMethodInter — conceal.cpp:270; CConcealment_GetDelay
// returns 1 conceal.cpp:1651; wired to SBR_SYSTEM_BITSTREAM_DELAY at
// aacdecoder_lib.cpp:613/619/1466) and (b) seeds its QMF transposer / envelope
// adjuster from that one-frame-different history. The sbr-dec-e2e int32 gate
// already proves native SBR == fdk sbrDecoder_Apply bit-for-bit on identical core
// (8 kHz / 10.5 kHz tones included), so the HF residual is an intrinsic
// fdk-full-decoder state difference, not a native decode error — exactly the
// regime below-crossover content hides (small HF energy => the misalignment is
// sub-LSB). It cannot be bounded as an int16 tolerance here; the tight, provable
// claim is the bit-exact CORE equality TestSbrDecodeCoreBitExactHF asserts.
//
// This slice compiles its OWN copy of the needed fdk C TUs (fdk_tu_*.cpp here); it
// never imports libraries/aac. It MAY import internal/nativeaac and the heaac glue.
// The aacdecoder_lib TU is a PARITY-LOCAL tapped copy (the shared vendored libfdk
// is never modified) so the pre-SBR core can be observed.
func TestSbrDecodeCodecInt16Parity(t *testing.T) {
	const (
		warmupFrames = 4   // aligned frames < warmupFrames carry the SBR warmup transient
		warmupTol    = 256 // proven warmup-transient bound (below-crossover content)
		steadyTol    = 8   // proven steady bound (<= 2 mono, <= 7 stereo); 8 = margin
		frameDelay   = 1   // fdk full-decode output delay == 1 frame (== 2048 samples)
	)

	cases := []struct {
		name     string
		channels int
		outRate  int
	}{
		{"mono-out44100", 1, 44100},
		{"mono-out48000", 1, 48000},
		{"mono-out32000", 1, 32000},
		{"stereo-out44100", 2, 44100},
		{"stereo-out48000", 2, 48000},
		{"stereo-out32000", 2, 32000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const coreFrameLen = 1024
			coreRate := tc.outRate / 2
			nf := 48
			fs := tc.channels * 2 * coreFrameLen

			// A slow non-periodic chirp confined BELOW the SBR crossover, so the
			// codec-level int16 output is dominated by the (verified) AAC-LC core
			// band. Non-periodic so the 1-frame alignment below is unambiguous (a
			// periodic multitone admits alignment aliases).
			pcm := make([]int16, nf*coreFrameLen*tc.channels)
			for n := 0; n < nf*coreFrameLen; n++ {
				t0 := float64(n) / float64(tc.outRate)
				f0 := 200.0 + 30.0*t0
				s := 0.5*math.Sin(2*math.Pi*f0*t0*(1+0.3*t0)) + 0.15*math.Sin(2*math.Pi*2500*t0)
				for c := 0; c < tc.channels; c++ {
					v := s
					if c == 1 {
						v = s*0.6 + 0.1*math.Sin(2*math.Pi*800*t0)
					}
					pcm[n*tc.channels+c] = int16(v * 22000)
				}
			}

			aus, asc, _, ok := cEncodeHEAAC(tc.outRate, tc.channels, 48000, coreFrameLen, pcm)
			require.True(t, ok, "fdk HE-AAC encode failed")
			require.NotEmpty(t, asc, "encoder produced no ASC")
			if len(aus) < nf {
				nf = len(aus)
			}
			require.Greater(t, nf, warmupFrames+frameDelay+8, "too few access units")

			dec, err := heaac.NewDecoder(coreFrameLen, coreRate, tc.channels, 0)
			require.NoError(t, err)
			require.Equal(t, 2*coreFrameLen, dec.FrameSamples())
			require.Equal(t, tc.outRate, dec.SampleRate())

			// Native FULL int16 decode, per frame.
			goOut := make([][]int16, nf)
			for f := 0; f < nf; f++ {
				out := make([]int16, fs)
				_, derr := dec.DecodeAccessUnit(aus[f], out)
				require.NoErrorf(t, derr, "go HE-AAC decode of AU %d failed", f)
				goOut[f] = out
			}

			// Genuine fdk FULL int16 decode (limiter disabled).
			fdkPCM, sp, ch, sr, nFrames, ok := cDecodeHEAAC(asc, aus, tc.channels, 2*coreFrameLen)
			require.True(t, ok, "fdk full HE-AAC decode failed")
			require.Equal(t, 2*coreFrameLen, sp, "fdk steady frame size")
			require.Equal(t, tc.channels, ch, "fdk channel count")
			require.Equal(t, tc.outRate, sr, "fdk output sample rate")
			fdkOut := make([][]int16, nFrames)
			for f := 0; f < nFrames; f++ {
				fdkOut[f] = fdkPCM[f*fs : (f+1)*fs]
			}

			// Compare native frame f against fdk frame f+frameDelay, removing fdk's
			// one-frame full-decode output delay. The aligned warmup window carries
			// the documented SBR start-up transient; the steady window must hold the
			// tight core-band bound.
			lim := nf
			if nFrames-frameDelay < lim {
				lim = nFrames - frameDelay
			}
			require.Greater(t, lim, warmupFrames+8, "too few aligned frames")

			steadyWorst := 0
			for f := 0; f < lim; f++ {
				worst, worstIdx := 0, -1
				for i := 0; i < fs; i++ {
					d := int(goOut[f][i]) - int(fdkOut[f+frameDelay][i])
					if d < 0 {
						d = -d
					}
					if d > worst {
						worst, worstIdx = d, i
					}
				}
				if f < warmupFrames {
					require.LessOrEqualf(t, worst, warmupTol,
						"aligned warmup frame %d residual %d exceeds proven bound %d (sample %d)",
						f, worst, warmupTol, worstIdx)
				} else {
					require.LessOrEqualf(t, worst, steadyTol,
						"aligned steady frame %d residual %d exceeds proven core-band bound %d (sample %d)",
						f, worst, steadyTol, worstIdx)
					if worst > steadyWorst {
						steadyWorst = worst
					}
				}
			}
			t.Logf("%s: codec-level int16 parity OK — aligned steady residual <= %d LSB (bound %d)",
				tc.name, steadyWorst, steadyTol)
		})
	}
}

// TestSbrDecodeCoreBitExactHF is the HF-near-crossover CORE parity gate. It
// compares the native AAC-LC half-rate core against the GENUINE fdk pre-SBR core
// for content with significant energy NEAR/ABOVE the SBR crossover — the regime
// the prior investigation flagged as a suspected native core bug.
//
// RESULT: the suspected "thousands of LSB" native core bug DOES NOT EXIST, and
// the native half-rate AAC-LC core is now BIT-FOR-BIT equal to the fdk pre-SBR
// core (worst 0) for BOTH mono and stereo at 8/10/11 kHz. A prior pass had a
// small, rare CPE-SECOND-channel residual (≤64 LSB on 1-2 frames of ~19); it
// was root-caused to ApplyIS using the generic fMultDD instead of fMult for the
// intensity-stereo upmix (see the coreTol comment below) and FIXED, so the gate
// now pins exact equality (coreTol == 0) for every channel count. The reported
// thousands-of-LSB int16 divergence is entirely the SBR-integration-layer effect
// documented atop TestSbrDecodeCodecInt16Parity (fdk's one-frame SBR bitstream
// delay + QMF warmup state), not a core error.
//
// The fdk core oracle is the buffer fdk hands to sbrDecoder_Apply, captured by
// the parity-local pre-SBR core tap (fdk_core_tap, in core_tap_bridge.cpp, fired
// from the PARITY-LOCAL tapped copy of aacDecoder_DecodeFrame in
// _fdk_local_aacdecoder_lib_tapped.cpp — the shared vendored libfdk is never
// modified). It is planar int32 PCM_AAC (PCM_AAC == LONG == INT == 32-bit) at
// aacOutDataHeadroom, the SAME layout/domain as the native planar core from
// heaac.DecodeAccessUnitInt32. fdk's full decoder buffers one core frame inside
// the lib, so native core frame f equals fdk core frame f+coreDelay.
func TestSbrDecodeCoreBitExactHF(t *testing.T) {
	const (
		coreFrameLen = 1024
		coreDelay    = 1 // fdk full-decoder one-core-frame buffering
		warmupFrames = 4 // skip the QMF/core warmup transient at stream start
	)

	cases := []struct {
		name     string
		channels int
		outRate  int
		toneHz   float64
	}{
		{"mono-out44100-tone8k", 1, 44100, 8000},
		{"mono-out48000-tone10k", 1, 48000, 10000},
		{"mono-out48000-tone11k", 1, 48000, 11000},
		{"stereo-out44100-tone8k", 2, 44100, 8000},
		{"stereo-out48000-tone10k", 2, 48000, 10000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			coreRate := tc.outRate / 2
			nf := 48

			// A strong tone NEAR/ABOVE the SBR crossover plus a little LF, so the
			// top half-rate core bands carry real energy — the exact regime the
			// prior investigation suspected of a native core bug.
			pcm := make([]int16, nf*coreFrameLen*tc.channels)
			for n := 0; n < nf*coreFrameLen; n++ {
				t0 := float64(n) / float64(tc.outRate)
				s := 0.6*math.Sin(2*math.Pi*tc.toneHz*t0) + 0.2*math.Sin(2*math.Pi*1200*t0)
				for c := 0; c < tc.channels; c++ {
					v := s
					if c == 1 {
						v = s*0.7 + 0.15*math.Sin(2*math.Pi*900*t0)
					}
					pcm[n*tc.channels+c] = int16(v * 22000)
				}
			}

			aus, asc, _, ok := cEncodeHEAAC(tc.outRate, tc.channels, 48000, coreFrameLen, pcm)
			require.True(t, ok, "fdk HE-AAC encode failed")
			require.NotEmpty(t, asc, "encoder produced no ASC")
			if len(aus) < nf {
				nf = len(aus)
			}
			require.Greater(t, nf, warmupFrames+coreDelay+8, "too few access units")

			dec, err := heaac.NewDecoder(coreFrameLen, coreRate, tc.channels, 0)
			require.NoError(t, err)

			// Native planar int32 core per frame.
			per := tc.channels * coreFrameLen
			nativeCore := make([][]int32, nf)
			for f := 0; f < nf; f++ {
				_, ci, _, _, _, _, derr := dec.DecodeAccessUnitInt32(aus[f])
				require.NoErrorf(t, derr, "go core decode of AU %d failed", f)
				require.Len(t, ci, per)
				nativeCore[f] = ci
			}

			// Genuine fdk pre-SBR core per frame, via the parity-local tap.
			fdkCore, nFdk, coreCh, ok := cDecodeHEAACCore(asc, aus, tc.channels, coreFrameLen)
			require.True(t, ok, "fdk pre-SBR core tap failed")
			require.Equal(t, tc.channels, coreCh, "fdk core channel count")
			require.GreaterOrEqual(t, nFdk, nf, "fdk produced fewer core frames than AUs")

			// Compare native frame f against fdk frame f+coreDelay, removing fdk's
			// one-core-frame buffering delay. BIT-EXACT after the warmup transient.
			lim := nf
			if nFdk-coreDelay < lim {
				lim = nFdk - coreDelay
			}
			require.Greater(t, lim, warmupFrames+8, "too few aligned core frames")

			// Tolerance: BIT-EXACT for BOTH mono and stereo (worst 0). The CPE
			// second-channel residual a prior pass saw (≤64 LSB on 1-2 frames) was
			// root-caused to a single mis-mapped fixed-point multiply in the
			// intensity-stereo upmix: CJointStereo_ApplyIS (stereo.cpp:1232) does
			// rightSpectrum[i] = fMult(left, scale), and fMult(FIXP_DBL,FIXP_DBL)
			// == fixmul_DD takes the ARMv8 `smull; asr #31` override on this target
			// (fixmul_arm.h:156-191), keeping bit 31; the native ApplyIS had used
			// the generic fMultDD ((a*b)>>32)<<1, which drops bit 31 — a 1-LSB
			// spectral error on the SECOND channel only, on the rare intensity
			// bands, amplified by the IMDCT to ≤64 LSB in the time domain. With
			// fMultDD swapped for fMult (nativeaac/stereo_apply.go ApplyIS) the
			// whole half-rate core is now bit-for-bit equal to fdk for both mono
			// and stereo, so the bound is 0 LSB for every channel count.
			coreTol := 0
			worst, worstIdx := 0, -1
			for f := warmupFrames; f < lim; f++ {
				for i := 0; i < per; i++ {
					d := int(nativeCore[f][i]) - int(fdkCore[f+coreDelay][i])
					if d < 0 {
						d = -d
					}
					if d > worst {
						worst, worstIdx = d, i
					}
				}
			}
			require.LessOrEqualf(t, worst, coreTol,
				"native AAC-LC half-rate core vs genuine fdk pre-SBR core: worst %d LSB exceeds bound %d (planar idx %d, ch %d) — HF-near-crossover",
				worst, coreTol, worstIdx, worstIdx/coreFrameLen)
			t.Logf("%s: native AAC-LC half-rate core vs genuine fdk pre-SBR core over %d HF frames — worst %d LSB (bound %d)",
				tc.name, lim-warmupFrames, worst, coreTol)
		})
	}
}
