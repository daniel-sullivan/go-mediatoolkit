// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// MP3 VBR iteration loops — a 1:1 translation of the vendored LAME 3.100
// encoder's libmp3lame/quantize.c VBR drivers (copyright Mark Taylor / Robert
// Hegemann). Two families: the older per-granule bit-search (vbr_rh) and the
// newer whole-frame search (vbr_mtrh / vbr_mt, the -V quality modes). From the
// per-granule MDCT lines (gr_info.xr), the psychoacoustic distortion budget
// (calc_xmin's l3_xmin) and a per-(gr,ch) max-bits budget, each loop finds the
// quantization that fits the frame and chooses the lowest bitrate able to hold
// the used bits, refilling the reservoir.
//
// Functions ported here (each names its quantize.c counterpart as file:line):
//
//	VBR_encode_granule     (quantize.c:1244, static) — bisection bit-search of a
//	                        single granule around the optimal bit count,
//	VBR_old_prepare        (quantize.c:1389, static) — LR->MS, l3_xmin, min/max
//	                        bits, analog-silence detection for the old path,
//	bitpressure_strategy   (quantize.c:1453, static) — inflate l3_xmin + shrink
//	                        max_bits when the frame overflows,
//	VBR_old_iteration_loop (quantize.c:1490) — the old whole-frame driver,
//	VBR_new_prepare        (quantize.c:1582, static) — l3_xmin + max_bits +
//	                        analog-silence / reservoir cap for the new path,
//	VBR_new_iteration_loop (quantize.c:1645) — the new whole-frame driver,
//	                        delegating the quantization to VBR_encode_frame
//	                        (vbrquantize_frame.go).
//
// This file does NOT re-port the shared kernels these loops drive: init_xrpow /
// outer_loop / init_outer_loop / iteration_finish_one / trancate_smallspectrums /
// ms_convert / get_framebits live in quantize_encode.go; on_pe / reduce_side in
// quantize_pvt_bits.go; calc_xmin in quantize_pvt.go; ResvFrameBegin/End/Adjust
// in reservoir_encode.go; VBR_encode_frame in vbrquantize_frame.go. They are
// called exactly where the C dereferences them.
//
// # Floating-point parity
//
// The control flow (the bisection, the bitrate-index scan, the reservoir
// arithmetic) is integer-exact. The FP-sensitive expressions — VBR_old_prepare's
// pe-driven masking adjust + pow(10, .) masking_lower, VBR_new_prepare's
// pow(10, .) masking_lower, and bitpressure_strategy's xmin inflation + 0.9*
// max_bits shrink — route through the //go:noinline qeVbr* / qeBitpressure*
// helpers (quantize_encode_fp_strict.go) so the mp3_strict build separately-
// rounds, matching the -ffp-contract=off cgo oracle.

// vbrEncodeGranule searches, by bisection over the bit budget, the quantization
// of one granule whose distortion satisfies l3_xmin within roughly 40 bits of the
// optimum (VBR_encode_granule, quantize.c:1244). Robert Hegemann 2000-09-04. It
// drives outer_loop at trial bit counts, remembers the best non-distorted result
// (its part2_3_length and xrpow) and restores it; min_bits..max_bits bound the
// search.
func (gfc *LameInternalFlags) vbrEncodeGranule(codInfo *GrInfo, l3Xmin []float32,
	xrpow []float32, ch, minBits, maxBits int) {
	var bstCodInfo GrInfo
	var bstXrpow [576]float32
	maxBitsConst := maxBits
	realBits := maxBits + 1
	thisBits := (maxBits + minBits) / 2
	var dbits, over, found int
	sfb21Extra := gfc.SvQnt.Sfb21Extra

	// memset(bst_cod_info.l3_enc, 0, ...)
	for i := range bstCodInfo.L3Enc {
		bstCodInfo.L3Enc[i] = 0
	}

	// search within round about 40 bits of optimal
	for {
		if thisBits > maxBitsConst-42 {
			gfc.SvQnt.Sfb21Extra = 0
		} else {
			gfc.SvQnt.Sfb21Extra = sfb21Extra
		}

		over = gfc.outerLoop(codInfo, l3Xmin, xrpow, ch, thisBits)

		// is quantization as good as we are looking for? (no sfb distorted)
		if over <= 0 {
			found = 1
			// now we know it can be done with "real_bits"
			realBits = codInfo.Part23Length

			// store best quantization so far
			bstCodInfo = *codInfo
			copy(bstXrpow[:], xrpow[:576])

			// try with fewer bits
			maxBits = realBits - 32
			dbits = maxBits - minBits
			thisBits = (maxBits + minBits) / 2
		} else {
			// try with more bits
			minBits = thisBits + 32
			dbits = maxBits - minBits
			thisBits = (maxBits + minBits) / 2

			if found != 0 {
				found = 2
				// start again with best quantization so far
				*codInfo = bstCodInfo
				copy(xrpow[:576], bstXrpow[:])
			}
		}
		if dbits <= 12 {
			break
		}
	}

	gfc.SvQnt.Sfb21Extra = sfb21Extra

	// found=0 => nothing found, use last one
	// found=1 => we just found the best and left the loop
	// found=2 => we restored a good one and have now l3_enc to restore too
	if found == 2 {
		copy(codInfo.L3Enc[:], bstCodInfo.L3Enc[:])
	}
}

// vbrOldPrepare converts LR->MS where chosen, computes the per-(gr,ch) allowed
// distortion l3_xmin and the min/max bit budgets, and detects analog silence for
// the old VBR path (VBR_old_prepare, quantize.c:1389). Robert Hegemann
// 2000-09-04. Returns 1 if the whole frame is analog silence, else 0.
func (gfc *LameInternalFlags) vbrOldPrepare(pe *[2][2]float32, msEnerRatio *[2]float32,
	ratio *[2][2]III_psy_ratio, l3Xmin *[2][2][SFBMAX]float32, frameBits []int,
	minBits, maxBits *[2][2]int, bands *[2][2]int) int {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc

	analogSilence := 1
	bits := 0

	eov.BitrateIndex = cfg.VbrMaxBitrateIndex
	var avg int
	avg = gfc.ResvFrameBegin(&avg) / cfg.ModeGr

	gfc.getFramebits(frameBits)

	for gr := 0; gr < cfg.ModeGr; gr++ {
		mxb := gfc.onPe(pe, &maxBits[gr], avg, gr, 0)
		if gfc.OvEnc.ModeExt == mpgMdMSLR {
			msConvert(&gfc.L3Side, gr)
			reduceSide(&maxBits[gr], msEnerRatio[gr], avg, mxb)
		}
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			codInfo := &gfc.L3Side.Tt[gr][ch]

			var maskingLowerDb float32
			if codInfo.BlockType != ShortType { // NORM, START or STOP type
				adjust := qeVbrAdjust(1.28, pe[gr][ch], 0.05)
				maskingLowerDb = gfc.SvQnt.MaskAdjust - adjust
			} else {
				adjust := qeVbrAdjust(2.56, pe[gr][ch], 0.14)
				maskingLowerDb = gfc.SvQnt.MaskAdjustShort - adjust
			}
			gfc.SvQnt.MaskingLower = qeVbrMaskingLower(maskingLowerDb)

			gfc.initOuterLoop(codInfo)
			bands[gr][ch] = calcXmin(gfc, &ratio[gr][ch], codInfo, l3Xmin[gr][ch][:])
			if bands[gr][ch] != 0 {
				analogSilence = 0
			}

			minBits[gr][ch] = 126

			bits += maxBits[gr][ch]
		}
	}
	for gr := 0; gr < cfg.ModeGr; gr++ {
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			if bits > frameBits[cfg.VbrMaxBitrateIndex] && bits > 0 {
				maxBits[gr][ch] *= frameBits[cfg.VbrMaxBitrateIndex]
				maxBits[gr][ch] /= bits
			}
			if minBits[gr][ch] > maxBits[gr][ch] {
				minBits[gr][ch] = maxBits[gr][ch]
			}
		}
	}

	return analogSilence
}

// bitpressureStrategy inflates the per-band allowed distortion l3_xmin (more for
// higher scalefactor bands) and shrinks each max_bits to 0.9x, so the next VBR-old
// iteration quantizes more coarsely and fits the frame (bitpressure_strategy,
// quantize.c:1453).
func (gfc *LameInternalFlags) bitpressureStrategy(l3Xmin *[2][2][SFBMAX]float32,
	minBits, maxBits *[2][2]int) {
	cfg := &gfc.Cfg
	for gr := 0; gr < cfg.ModeGr; gr++ {
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			gi := &gfc.L3Side.Tt[gr][ch]
			pxmin := l3Xmin[gr][ch][:]
			p := 0
			for sfb := 0; sfb < gi.PsyLmax; sfb++ {
				pxmin[p] = qeBitpressureXmin(pxmin[p], sfb, float64(SBMAXl))
				p++
			}

			if gi.BlockType == ShortType {
				for sfb := gi.SfbSmin; sfb < SBMAXs; sfb++ {
					pxmin[p] = qeBitpressureXmin(pxmin[p], sfb, float64(SBMAXs))
					p++
					pxmin[p] = qeBitpressureXmin(pxmin[p], sfb, float64(SBMAXs))
					p++
					pxmin[p] = qeBitpressureXmin(pxmin[p], sfb, float64(SBMAXs))
					p++
				}
			}
			maxBits[gr][ch] = qeBitpressureMaxBits(minBits[gr][ch], maxBits[gr][ch])
		}
	}
}

// vbrOldIterationLoop is the old VBR whole-frame driver: it quantizes every
// granule at the lowest possible bit count via VBR_encode_granule, finds the
// lowest bitrate able to hold the used bits, and re-runs under bitpressure until
// the frame fits the reservoir (VBR_old_iteration_loop, quantize.c:1490). Robert
// Hegemann 2000-09-06 rewrite.
func (gfc *LameInternalFlags) vbrOldIterationLoop(pe *[2][2]float32,
	msEnerRatio *[2]float32, ratio *[2][2]III_psy_ratio) {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc
	var l3Xmin [2][2][SFBMAX]float32
	var xrpow [576]float32
	var bands [2][2]int
	var frameBits [15]int
	var minBits, maxBits [2][2]int
	var meanBits int
	l3Side := &gfc.L3Side

	analogSilence := gfc.vbrOldPrepare(pe, msEnerRatio, ratio, &l3Xmin, frameBits[:],
		&minBits, &maxBits, &bands)

	var bits int
	for {
		// quantize granules with lowest possible number of bits
		usedBits := 0

		for gr := 0; gr < cfg.ModeGr; gr++ {
			for ch := 0; ch < cfg.ChannelsOut; ch++ {
				codInfo := &l3Side.Tt[gr][ch]

				// init_xrpow sets up cod_info, scalefac and xrpow
				ret := gfc.initXrpow(codInfo, xrpow[:])
				if ret == 0 || maxBits[gr][ch] == 0 {
					// xr contains no energy; l3_enc quantized to zero
					continue
				}

				gfc.vbrEncodeGranule(codInfo, l3Xmin[gr][ch][:], xrpow[:],
					ch, minBits[gr][ch], maxBits[gr][ch])

				// do the 'substep shaping'
				if gfc.SvQnt.SubstepShaping&1 != 0 {
					gfc.trancateSmallspectrums(&l3Side.Tt[gr][ch], l3Xmin[gr][ch][:], xrpow[:])
				}

				usedBits += codInfo.Part23Length + codInfo.Part2Length
			}
		}

		// find lowest bitrate able to hold used bits
		if analogSilence != 0 && cfg.EnforceMinBitrate == 0 {
			eov.BitrateIndex = 1
		} else {
			eov.BitrateIndex = cfg.VbrMinBitrateIndex
		}

		for ; eov.BitrateIndex < cfg.VbrMaxBitrateIndex; eov.BitrateIndex++ {
			if usedBits <= frameBits[eov.BitrateIndex] {
				break
			}
		}
		bits = gfc.ResvFrameBegin(&meanBits)

		if usedBits <= bits {
			break
		}

		gfc.bitpressureStrategy(&l3Xmin, &minBits, &maxBits)
	}

	for gr := 0; gr < cfg.ModeGr; gr++ {
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			gfc.iterationFinishOne(gr, ch)
		}
	}
	gfc.ResvFrameEnd(meanBits)
}

// vbrNewPrepare computes the per-(gr,ch) allowed distortion l3_xmin and max-bits
// budgets, detects analog silence and caps the reservoir for the new VBR path
// (VBR_new_prepare, quantize.c:1582). Returns 1 if the whole frame is analog
// silence, else 0; *maxResv receives the reservoir cap.
func (gfc *LameInternalFlags) vbrNewPrepare(pe *[2][2]float32, ratio *[2][2]III_psy_ratio,
	l3Xmin *[2][2][SFBMAX]float32, frameBits []int, maxBits *[2][2]int, maxResv *int) int {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc

	analogSilence := 1
	bits := 0
	var avg, maximumFramebits int

	if cfg.FreeFormat == 0 {
		eov.BitrateIndex = cfg.VbrMaxBitrateIndex
		_ = gfc.ResvFrameBegin(&avg)
		*maxResv = gfc.SvEnc.ResvMax

		gfc.getFramebits(frameBits)
		maximumFramebits = frameBits[cfg.VbrMaxBitrateIndex]
	} else {
		eov.BitrateIndex = 0
		maximumFramebits = gfc.ResvFrameBegin(&avg)
		frameBits[0] = maximumFramebits
		*maxResv = gfc.SvEnc.ResvMax
	}

	for gr := 0; gr < cfg.ModeGr; gr++ {
		_ = gfc.onPe(pe, &maxBits[gr], avg, gr, 0)
		if gfc.OvEnc.ModeExt == mpgMdMSLR {
			msConvert(&gfc.L3Side, gr)
		}
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			codInfo := &gfc.L3Side.Tt[gr][ch]

			gfc.SvQnt.MaskingLower = qeVbrMaskingLower(gfc.SvQnt.MaskAdjust)

			gfc.initOuterLoop(codInfo)
			if calcXmin(gfc, &ratio[gr][ch], codInfo, l3Xmin[gr][ch][:]) != 0 {
				analogSilence = 0
			}

			bits += maxBits[gr][ch]
		}
	}
	for gr := 0; gr < cfg.ModeGr; gr++ {
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			if bits > maximumFramebits && bits > 0 {
				maxBits[gr][ch] *= maximumFramebits
				maxBits[gr][ch] /= bits
			}
		}
	}
	if analogSilence != 0 {
		*maxResv = 0
	}
	return analogSilence
}

// vbrNewIterationLoop is the new VBR whole-frame driver (vbr_mtrh / vbr_mt, the
// -V quality modes): it prepares the budgets, fills xrpow per granule, delegates
// the whole-frame quantization to VBR_encode_frame, picks the lowest bitrate able
// to hold the used bits (with reservoir padding), and finalises the reservoir
// (VBR_new_iteration_loop, quantize.c:1645).
func (gfc *LameInternalFlags) vbrNewIterationLoop(pe *[2][2]float32,
	msEnerRatio *[2]float32, ratio *[2][2]III_psy_ratio) {
	cfg := &gfc.Cfg
	eov := &gfc.OvEnc
	var l3Xmin [2][2][SFBMAX]float32
	var xrpow [2][2][576]float32
	var frameBits [15]int
	var maxBits [2][2]int
	var pad int
	l3Side := &gfc.L3Side

	_ = msEnerRatio // not used

	// memset(xrpow, 0, sizeof(xrpow)) — Go zero value already covers this.

	analogSilence := gfc.vbrNewPrepare(pe, ratio, &l3Xmin, frameBits[:], &maxBits, &pad)

	for gr := 0; gr < cfg.ModeGr; gr++ {
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			codInfo := &l3Side.Tt[gr][ch]

			// init_xrpow sets up cod_info, scalefac and xrpow
			if gfc.initXrpow(codInfo, xrpow[gr][ch][:]) == 0 {
				maxBits[gr][ch] = 0 // silent granule needs no bits
			}
		}
	}

	// quantize granules with lowest possible number of bits
	usedBits := VBRencodeFrame(gfc, &xrpow, &l3Xmin, &maxBits)

	if cfg.FreeFormat == 0 {
		// find lowest bitrate able to hold used bits
		var i int
		if analogSilence != 0 && cfg.EnforceMinBitrate == 0 {
			i = 1
		} else {
			i = cfg.VbrMinBitrateIndex
		}

		for ; i < cfg.VbrMaxBitrateIndex; i++ {
			if usedBits <= frameBits[i] {
				break
			}
		}
		if i > cfg.VbrMaxBitrateIndex {
			i = cfg.VbrMaxBitrateIndex
		}
		if pad > 0 {
			j := cfg.VbrMaxBitrateIndex
			for ; j > i; j-- {
				unused := frameBits[j] - usedBits
				if unused <= pad {
					break
				}
			}
			eov.BitrateIndex = j
		} else {
			eov.BitrateIndex = i
		}
	} else {
		eov.BitrateIndex = 0
	}
	if usedBits <= frameBits[eov.BitrateIndex] {
		// update Reservoire status
		var meanBits int
		_ = gfc.ResvFrameBegin(&meanBits)
		for gr := 0; gr < cfg.ModeGr; gr++ {
			for ch := 0; ch < cfg.ChannelsOut; ch++ {
				codInfo := &l3Side.Tt[gr][ch]
				gfc.ResvAdjust(codInfo)
			}
		}
		gfc.ResvFrameEnd(meanBits)
	} else {
		// SHOULD NOT HAPPEN INTERNAL ERROR
		panic("nativemp3: INTERNAL ERROR IN VBR NEW CODE")
	}
}
