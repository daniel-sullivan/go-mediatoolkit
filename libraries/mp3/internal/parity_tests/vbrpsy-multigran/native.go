// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrpsymg

import "go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3.L3psychoAnalVbr the same way the C
// oracle drives the genuine static L3psycho_anal_vbr: build a -V2 EncoderContext
// (so the gfc has the freshly-initialized PsyStateVar / PsyConst / ATH that
// lame_init_params produced), load the C-provided shared mfbuf into the context's
// Mfbuf, then run the psy driver for gr=0 and gr=1 with the same encoder.c:372
// bufp window slice, capturing the per-granule outputs for the test to compare.

// goResult mirrors the C handle's per-granule capture.
type goResult struct {
	nChnPsy int
	energy  [2][4]float32
	pe      [2][2]float32
	peMS    [2][2]float32
	enL     [2][2][2][22]float32 // [which][gr][ch][sb]
	thmL    [2][2][2][22]float32
	enS     [2][2][2][13 * 3]float32
	thmS    [2][2][2][13 * 3]float32
}

// newV2Gfp builds the -V2 user flags identical to the vbr-encode-e2e slice's
// native driver (the byte-identical SessionConfig target).
func newV2Gfp(samplerate, channels int) *nativemp3.LameGlobalFlags {
	gfp := &nativemp3.LameGlobalFlags{
		StrictISO:      2, // MDB_MAXIMUM (lame.h:141)
		Original:       1,
		WriteLameTag:   1,
		ShortBlocks:    -1,
		SubblockGain:   -1,
		LowpassWidth:   -1,
		HighpassWidth:  -1,
		VBRq:           4,
		VBRMeanBitrate: 128,
		QuantComp:      -1,
		QuantCompShort: -1,
		Msfix:          -1,
		Attackthre:     -1,
		AttackthreS:    -1,
		Scale:          1,
		ScaleLeft:      1,
		ScaleRight:     1,
		ATHcurve:       -1,
		ATHtype:        -1,
		AthaaType:      -1,
		UseTemporal:    -1,
		InterChRatio:   -1,
	}
	// Mirror oracle.c: set ONLY in_samplerate; lame_init_params' q-map derives the
	// output rate and remaps VBR_q at <44kHz (identity at 44.1k/48k).
	gfp.SamplerateIn = samplerate
	gfp.NumChannels = channels
	gfp.Quality = 5
	if channels == 1 {
		gfp.Mode = 3 // MONO
	} else {
		gfp.Mode = 1 // JOINT_STEREO
	}
	gfp.VBR = 4 // vbr_mtrh (vbr_default)
	gfp.VBRq = 2
	return gfp
}

// nativeRun loads the shared mfbuf and runs the pure-Go vbrpsy per granule.
// Returns nil on a lame_init_params failure.
func nativeRun(samplerate, channels int, mfbuf0, mfbuf1 []float32) *goResult {
	gfp := newV2Gfp(samplerate, channels)
	ec, ret := nativemp3.NewEncoderContext(gfp)
	if ret != 0 {
		return nil
	}
	gfc := ec.Gfc
	cfg := &gfc.Cfg

	// Load the byte-identical mfbuf the oracle built.
	for i := range mfbuf0 {
		gfc.SvEnc.Mfbuf[0][i] = mfbuf0[i]
	}
	if cfg.ChannelsOut == 2 {
		for i := range mfbuf1 {
			gfc.SvEnc.Mfbuf[1][i] = mfbuf1[i]
		}
	}

	res := &goResult{}
	if cfg.Mode == 1 /* JOINT_STEREO */ {
		res.nChnPsy = 4
	} else {
		res.nChnPsy = cfg.ChannelsOut
	}

	var maskingLR, maskingMS [2][2]nativemp3.III_psy_ratio
	for gr := 0; gr < cfg.ModeGr; gr++ {
		var bufp [2][]float32
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			// encoder.c:372 bufp[ch] = &inbuf[ch][576 + gr*576 - FFTOFFSET].
			off := 576 + gr*576 - nativemp3.FFTOFFSET
			bufp[ch] = gfc.SvEnc.Mfbuf[ch][off:]
		}
		var pe, peMS [2]float32
		var totEner [4]float32
		var blocktype [2]int
		gfc.L3psychoAnalVbr(bufp, gr, &maskingLR, &maskingMS, pe[:], peMS[:], totEner[:], blocktype[:])

		res.energy[gr] = totEner
		res.pe[gr] = pe
		res.peMS[gr] = peMS
		for ch := 0; ch < 2; ch++ {
			for sb := 0; sb < 22; sb++ {
				res.enL[0][gr][ch][sb] = maskingLR[gr][ch].En.L[sb]
				res.thmL[0][gr][ch][sb] = maskingLR[gr][ch].Thm.L[sb]
				res.enL[1][gr][ch][sb] = maskingMS[gr][ch].En.L[sb]
				res.thmL[1][gr][ch][sb] = maskingMS[gr][ch].Thm.L[sb]
			}
			for sb := 0; sb < 13; sb++ {
				for sub := 0; sub < 3; sub++ {
					k := sb*3 + sub
					res.enS[0][gr][ch][k] = maskingLR[gr][ch].En.S[sb][sub]
					res.thmS[0][gr][ch][k] = maskingLR[gr][ch].Thm.S[sb][sub]
					res.enS[1][gr][ch][k] = maskingMS[gr][ch].En.S[sb][sub]
					res.thmS[1][gr][ch][k] = maskingMS[gr][ch].Thm.S[sb][sub]
				}
			}
		}
	}
	return res
}
