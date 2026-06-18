// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Layer III quantizer front-end — the floating-point half of the vendored LAME
// 3.100 encoder's libmp3lame/takehiro.c that was deferred by the integer
// bit-counting slice (takehiro.go). This is a 1:1 translation of the
// quantize_lines_xrpow / quantize_lines_xrpow_01 / quantize_xrpow / count_bits
// functions: given a granule's coloured magnitudes xr^(3/4) (the xrpow array)
// and the current global_gain, it quantizes each coefficient to its integer
// l3_enc value and returns the part2_3 bit cost via noquant_count_bits
// (takehiro.go).
//
// # Scope of this slice ("takehiro-quantize")
//
// quantize_lines_xrpow_01 (takehiro.c:114), quantize_lines_xrpow (the
// non-TAKEHIRO_IEEE754_HACK branch, takehiro.c:223), quantize_xrpow
// (takehiro.c:282) and count_bits (takehiro.c:768). The vendored config.h does
// NOT define TAKEHIRO_IEEE754_HACK, so the float-table XRPOW_FTOI = (int)(src)
// truncation path is ported (the IEEE754 magic-float branch is the alternate C
// path and is intentionally not translated). The integer bit counters
// (noquant_count_bits, choose_table, ix_max, …) live in takehiro.go.
//
// # Floating-point parity
//
// LAME's FLOAT is float32 (machine.h). quantize_lines_xrpow multiplies each
// coefficient by istep and adds adj43[rx] in float32; both go through the
// //go:noinline tqMul / tqAdd helpers (takehiro_quantize_fp_strict.go) so the
// mp3_strict build separately-rounds, matching the -ffp-contract=off oracle.
// XRPOW_FTOI(x) = (int)(x) truncates toward zero — Go's int(float32) does the
// same. The two double-precision narrowings (count_bits's IXMAX_VAL/IPOW20 and
// quantize_xrpow's substep roundfac in count_bits) reproduce the C double
// promotion with float32(... float64 ...) directly. Every ported function names
// its takehiro.c counterpart as file:line.

// ipow20Idx is LAME's IPOW20(x) macro (machine.h:92): ipow20[x]. The C asserts
// 0 <= x < Q_MAX; the port relies on the same caller invariants (global_gain is
// clamped to [0,255] by bin_search_StepSize / outer_loop, and Q_MAX = 257).
func ipow20Idx(x int) float32 { return ipow20[x] }

// quantizeLinesXrpow01 quantizes l coefficients to 0/1 against the threshold
// compareval0 = (1 - 0.4054)/istep (quantize_lines_xrpow_01, takehiro.c:114).
// It is used for the count1 / accumulate01 region where every line is known to
// be <= 1. compareval0 is a float32 reciprocal-scaled constant; the comparison
// is exact (no rounding in the >). l is even and > 0 by the caller contract.
func quantizeLinesXrpow01(l int, istep float32, xr []float32, ix []int) {
	// compareval0 = (1.0f - 0.4054f) / istep — float32 throughout.
	compareval0 := tqDiv01(istep)
	for i := 0; i < l; i += 2 {
		xr0 := xr[i+0]
		xr1 := xr[i+1]
		if compareval0 > xr0 {
			ix[i+0] = 0
		} else {
			ix[i+0] = 1
		}
		if compareval0 > xr1 {
			ix[i+1] = 0
		} else {
			ix[i+1] = 1
		}
	}
}

// quantizeLinesXrpow quantizes l coefficients via xr*istep + adj43[(int)(xr*istep)]
// then truncates to int (quantize_lines_xrpow, takehiro.c:223, the
// non-TAKEHIRO_IEEE754_HACK branch). The C unrolls the loop into a 4-wide body
// plus a remaining-2 tail, interleaving the float-to-int conversions to hide
// latency; the port keeps the 4/2 structure so the truncation order and the
// adj43 table indices match exactly. XRPOW_FTOI(x) = (int)(x), QUANTFAC(rx) =
// adj43[rx], ROUNDFAC unused. l is > 0 by the caller contract.
func quantizeLinesXrpow(l int, istep float32, xr []float32, ix []int) {
	// C: l = l >> 1; remaining = l % 2; l = l >> 1; (so the 4-wide body runs
	// (l>>2) times over the original count, with a 2-wide tail when (l>>1) is
	// odd).
	half := l >> 1
	remaining := half % 2
	quads := half >> 1

	xp := 0 // read cursor into xr (the C *xr++)
	ip := 0 // write cursor into ix (the C *ix++)

	for ; quads > 0; quads-- {
		x0 := tqMul(xr[xp+0], istep)
		x1 := tqMul(xr[xp+1], istep)
		rx0 := int(x0)
		x2 := tqMul(xr[xp+2], istep)
		rx1 := int(x1)
		x3 := tqMul(xr[xp+3], istep)
		rx2 := int(x2)
		x0 = tqAdd(x0, adj43[rx0])
		rx3 := int(x3)
		x1 = tqAdd(x1, adj43[rx1])
		ix[ip+0] = int(x0)
		x2 = tqAdd(x2, adj43[rx2])
		ix[ip+1] = int(x1)
		x3 = tqAdd(x3, adj43[rx3])
		ix[ip+2] = int(x2)
		ix[ip+3] = int(x3)
		xp += 4
		ip += 4
	}
	if remaining != 0 {
		x0 := tqMul(xr[xp+0], istep)
		x1 := tqMul(xr[xp+1], istep)
		rx0 := int(x0)
		rx1 := int(x1)
		x0 = tqAdd(x0, adj43[rx0])
		x1 = tqAdd(x1, adj43[rx1])
		ix[ip+0] = int(x0)
		ix[ip+1] = int(x1)
	}
}

// quantizeXrpow selects which scalefactor bands to quantize and dispatches each
// run of lines to quantize_lines_xrpow (big values) or quantize_lines_xrpow_01
// (the 0/1 region) (quantize_xrpow, takehiro.c:282). It accumulates contiguous
// runs of the same kind into one call (the C's accumulate / accumulate01 run
// lengths and acc_xp / acc_iData run starts) and reuses the prev_noise step
// cache when global_gain is unchanged. xp is the full xrpow array; pi is the
// granule's l3_enc; istep is IPOW20(global_gain).
func quantizeXrpow(xp []float32, pi []int, istep float32, codInfo *GrInfo, prevNoise *CalcNoiseData) {
	j := 0
	accumulate := 0
	accumulate01 := 0

	// acc_iData / acc_xp are run starts as offsets into pi / xp (the C carries
	// int*/FLOAT* run pointers). iData / xp advance by width per band.
	iDataBase := 0 // current band's l3_enc offset (C iData)
	xpBase := 0    // current band's xrpow offset (C xp)
	accIData := 0  // run start in pi (C acc_iData)
	accXp := 0     // run start in xp (C acc_xp)

	// Reusing previously computed data does not seem to work if global gain is
	// changed (takehiro.c:301-304).
	prevDataUse := prevNoise != nil && codInfo.GlobalGain == prevNoise.GlobalGain

	sfbmax := 21
	if codInfo.BlockType == ShortType {
		sfbmax = 38
	}

	for sfb := 0; sfb <= sfbmax; sfb++ {
		step := -1

		if prevDataUse || codInfo.BlockType == NormType {
			pre := 0
			if codInfo.Preflag != 0 {
				pre = pretab[sfb]
			}
			step = codInfo.GlobalGain -
				((codInfo.Scalefac[sfb] + pre) << (codInfo.ScalefacScale + 1)) -
				codInfo.SubblockGain[codInfo.Window[sfb]]*8
		}

		if prevDataUse && prevNoise.Step[sfb] == step {
			// do not recompute this part, but compute accumulated lines
			if accumulate != 0 {
				quantizeLinesXrpow(accumulate, istep, xp[accXp:], pi[accIData:])
				accumulate = 0
			}
			if accumulate01 != 0 {
				quantizeLinesXrpow01(accumulate01, istep, xp[accXp:], pi[accIData:])
				accumulate01 = 0
			}
		} else { // should compute this part
			l := codInfo.Width[sfb]

			if (j + codInfo.Width[sfb]) > codInfo.MaxNonzeroCoeff {
				// do not compute upper zero part
				usefullsize := codInfo.MaxNonzeroCoeff - j + 1
				// memset(&pi[max_nonzero_coeff], 0, ...(576 - max_nonzero_coeff))
				for k := codInfo.MaxNonzeroCoeff; k < 576; k++ {
					pi[k] = 0
				}
				l = usefullsize
				if l < 0 {
					l = 0
				}
				// no need to compute higher sfb values
				sfb = sfbmax + 1
			}

			// accumulate lines to quantize
			if accumulate == 0 && accumulate01 == 0 {
				accIData = iDataBase
				accXp = xpBase
			}
			if prevNoise != nil &&
				prevNoise.SfbCount1 > 0 &&
				sfb >= prevNoise.SfbCount1 &&
				prevNoise.Step[sfb] > 0 && step >= prevNoise.Step[sfb] {

				if accumulate != 0 {
					quantizeLinesXrpow(accumulate, istep, xp[accXp:], pi[accIData:])
					accumulate = 0
					accIData = iDataBase
					accXp = xpBase
				}
				accumulate01 += l
			} else {
				if accumulate01 != 0 {
					quantizeLinesXrpow01(accumulate01, istep, xp[accXp:], pi[accIData:])
					accumulate01 = 0
					accIData = iDataBase
					accXp = xpBase
				}
				accumulate += l
			}

			if l <= 0 {
				// rh 20040215: may happen due to "prev_data_use" optimization
				if accumulate01 != 0 {
					quantizeLinesXrpow01(accumulate01, istep, xp[accXp:], pi[accIData:])
					accumulate01 = 0
				}
				if accumulate != 0 {
					quantizeLinesXrpow(accumulate, istep, xp[accXp:], pi[accIData:])
					accumulate = 0
				}
				break // ends for-loop
			}
		}
		if sfb <= sfbmax {
			iDataBase += codInfo.Width[sfb]
			xpBase += codInfo.Width[sfb]
			j += codInfo.Width[sfb]
		}
	}
	if accumulate != 0 { // last data part
		quantizeLinesXrpow(accumulate, istep, xp[accXp:], pi[accIData:])
		accumulate = 0
	}
	if accumulate01 != 0 { // last data part
		quantizeLinesXrpow01(accumulate01, istep, xp[accXp:], pi[accIData:])
		accumulate01 = 0
	}
}

// countBits quantizes the granule's xrpow at the current global_gain and counts
// the part2_3 bits (count_bits, takehiro.c:768). It first rejects an over-large
// step via the IXMAX_VAL/IPOW20 guard, then quantize_xrpow fills l3_enc, applies
// the optional substep-shaping pseudohalf rounding, and finally
// noquant_count_bits (takehiro.go) returns the bit total.
func (gfc *LameInternalFlags) countBits(xr []float32, gi *GrInfo, prevNoise *CalcNoiseData) int {
	ix := gi.L3Enc[:]

	// since quantize_xrpow uses table lookup, we need to check this first:
	// w = (IXMAX_VAL) / IPOW20(gi->global_gain). IXMAX_VAL is an int literal,
	// IPOW20 is float32; C promotes IXMAX_VAL to FLOAT for the division (FLOAT /
	// FLOAT), so it is a float32 reciprocal-scaled value.
	w := tqDivIxmax(ipow20Idx(gi.GlobalGain))

	if gi.XrpowMax > w {
		return largeBits
	}

	quantizeXrpow(xr, ix, ipow20Idx(gi.GlobalGain), gi, prevNoise)

	if gfc.SvQnt.SubstepShaping&2 != 0 {
		j := 0
		// 0.634521682242439 = 0.5946*2**(.5*0.1875); roundfac = that / IPOW20(gain).
		// The double literal divided by the float32 IPOW20 promotes to double then
		// narrows to FLOAT roundfac.
		gain := gi.GlobalGain + gi.ScalefacScale
		roundfac := float32(0.634521682242439 / float64(ipow20Idx(gain)))
		for sfb := 0; sfb < gi.Sfbmax; sfb++ {
			width := gi.Width[sfb]
			if gfc.SvQnt.Pseudohalf[sfb] == 0 {
				j += width
			} else {
				k := j
				j += width
				for ; k < j; k++ {
					if xr[k] < roundfac {
						ix[k] = 0
					}
				}
			}
		}
	}
	return gfc.noquantCountBits(gi, prevNoise)
}
