// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Layer III VBR bit-search orchestration — a 1:1 translation of the vendored
// LAME 3.100 encoder's libmp3lame/vbrquantize.c top tier: the global-stepsize /
// distribution-flattening search that drives a granule back inside its bit
// budget, and VBR_encode_frame, the whole-frame vbr_mtrh orchestrator that runs
// block_sf + the allocator for every granule/channel, encodes 'as is', and —
// when the frame overflows — redistributes the available bits and re-runs the
// out-of-bits strategy until the frame fits.
//
// Functions ported here (each names its vbrquantize.c counterpart as file:line):
//
//	tryGlobalStepsize        (vbrquantize.c:1011) — alloc+count at sfwork+delta,
//	searchGlobalStepsizeMax  (vbrquantize.c:1040) — binary search the global gain,
//	sfDepth                  (vbrquantize.c:1074) — max (255 - sfwork[i]),
//	cutDistribution          (vbrquantize.c:1093) — clamp sfwork[] to a cut,
//	flattenDistribution      (vbrquantize.c:1104) — interpolate sfwork[] toward p,
//	tryThatOne               (vbrquantize.c:1140) — alloc+count a candidate dist,
//	outOfBitsStrategy        (vbrquantize.c:1154) — two-phase flatten search,
//	reduce_bit_usage         (vbrquantize.c:1231) — best_scalefac_store + huffman,
//	VBR_encode_frame         (vbrquantize.c:1254) — the per-frame orchestrator.
//
// This tier sits above the allocation tier (vbrquantize_sfalloc.go: block_sf,
// the constrain allocators, quantizeAndCountBits, bitcount) and the leaf kernels
// (vbrquantize.go). It does NOT re-port the shared CBR kernels — best_scalefac_-
// store / best_huffman_divide / scale_bitcount / noquant_count_bits live in
// takehiro.go / quantize_encode.go and are called here exactly where the C
// dereferences them.
//
// # Floating-point parity
//
// This tier is almost entirely integer (the global-stepsize binary search and
// the distribution flatten/cut are integer-exact). The ONE FP-sensitive region
// is VBR_encode_frame's out-of-budget bit redistribution (vbrquantize.c:1388-
// 1505), which weights each block by sqrt(sqrt(used)) / sqrt(used) and splits a
// budget proportionally. Those float multiplies route through the //go:noinline
// vqf* helpers (vbrquantize_frame_fp_strict.go) so the mp3_strict build
// separately-rounds like the -ffp-contract=off oracle; the integer clamps around
// them are bit-identical in both builds.

// tryGlobalStepsize allocates and bit-counts the granule at scalefactor work
// vector sfwork shifted by delta (clamped per band to [vbrsfmin[i], 255]),
// returning the resulting Huffman+scalefactor bit count and restoring
// cod_info->xrpow_max afterwards (tryGlobalStepsize, vbrquantize.c:1011).
func tryGlobalStepsize(that *algoT, sfwork []int, vbrsfmin []int, delta int) int {
	xrpowMax := that.CodInfo.XrpowMax
	var sftemp [SFBMAX]int
	vbrmax := 0
	for i := 0; i < SFBMAX; i++ {
		gain := sfwork[i] + delta
		if gain < vbrsfmin[i] {
			gain = vbrsfmin[i]
		}
		if gain > 255 {
			gain = 255
		}
		if vbrmax < gain {
			vbrmax = gain
		}
		sftemp[i] = gain
	}
	that.alloc(that, sftemp[:], vbrsfmin, vbrmax)
	bitcount(that)
	nbits := quantizeAndCountBits(that)
	that.CodInfo.XrpowMax = xrpowMax
	return nbits
}

// searchGlobalStepsizeMax binary-searches the global stepsize for the largest
// gain whose bit count (plus part2_length) still meets target, re-running
// tryGlobalStepsize at the best gain found (searchGlobalStepsizeMax,
// vbrquantize.c:1040). It is the fall-back of outOfBitsStrategy.
func searchGlobalStepsizeMax(that *algoT, sfwork []int, vbrsfmin []int, target int) {
	codInfo := that.CodInfo
	gain := codInfo.GlobalGain
	curr := gain
	gainOk := 1024
	nbits := largeBits
	l := gain
	r := 512

	// assert(gain >= 0)
	for l <= r {
		curr = (l + r) >> 1
		nbits = tryGlobalStepsize(that, sfwork, vbrsfmin, curr-gain)
		if nbits == 0 || (nbits+codInfo.Part2Length) < target {
			r = curr - 1
			gainOk = curr
		} else {
			l = curr + 1
			if gainOk == 1024 {
				gainOk = curr
			}
		}
	}
	if gainOk != curr {
		curr = gainOk
		nbits = tryGlobalStepsize(that, sfwork, vbrsfmin, curr-gain)
	}
	_ = nbits
}

// sfDepth returns the maximum (255 - sfwork[i]) over the SFBMAX bands — the
// headroom available to flatten the distribution (sfDepth, vbrquantize.c:1074).
func sfDepth(sfwork []int) int {
	m := 0
	for i := 0; i < SFBMAX; i++ {
		di := 255 - sfwork[i]
		if m < di {
			m = di
		}
	}
	return m
}

// cutDistribution clamps each sfwork[i] to at most cut, writing into sfOut
// (cutDistribution, vbrquantize.c:1093). sfOut may alias sfwork (the C call site
// passes sfwork for both).
func cutDistribution(sfwork []int, sfOut []int, cut int) {
	for i := 0; i < SFBMAX; i++ {
		x := sfwork[i]
		if x < cut {
			sfOut[i] = x
		} else {
			sfOut[i] = cut
		}
	}
}

// flattenDistribution interpolates each sfwork[i] a fraction k/dm of the way
// toward p (clamping to [0,255]), writing sfOut and returning the resulting
// maximum (flattenDistribution, vbrquantize.c:1104). When dm <= 0 it copies
// sfwork unchanged and returns its max.
func flattenDistribution(sfwork []int, sfOut []int, dm, k, p int) int {
	sfmax := 0
	if dm > 0 {
		for i := 0; i < SFBMAX; i++ {
			di := p - sfwork[i]
			x := sfwork[i] + (k*di)/dm
			if x < 0 {
				x = 0
			} else if x > 255 {
				x = 255
			}
			sfOut[i] = x
			if sfmax < x {
				sfmax = x
			}
		}
	} else {
		for i := 0; i < SFBMAX; i++ {
			x := sfwork[i]
			sfOut[i] = x
			if sfmax < x {
				sfmax = x
			}
		}
	}
	return sfmax
}

// tryThatOne allocates and bit-counts the granule for the candidate distribution
// sftemp (with maximum vbrmax), returning nbits+part2_length and restoring
// cod_info->xrpow_max (tryThatOne, vbrquantize.c:1140).
func tryThatOne(that *algoT, sftemp []int, vbrsfmin []int, vbrmax int) int {
	xrpowMax := that.CodInfo.XrpowMax
	that.alloc(that, sftemp, vbrsfmin, vbrmax)
	bitcount(that)
	nbits := quantizeAndCountBits(that)
	nbits += that.CodInfo.Part2Length
	that.CodInfo.XrpowMax = xrpowMax
	return nbits
}

// outOfBitsStrategy drives the granule back under target bits in two phases: a
// binary search over the flatten amount (PART 1) and, failing that, over the
// flatten target gain (PART 2), falling back to searchGlobalStepsizeMax if
// neither finds a fit (outOfBitsStrategy, vbrquantize.c:1154). wrk is the scratch
// distribution buffer (length SFBMAX).
func outOfBitsStrategy(that *algoT, sfwork []int, vbrsfmin []int, target int) {
	var wrk [SFBMAX]int
	dm := sfDepth(sfwork)
	p := that.CodInfo.GlobalGain
	var nbits int

	// PART 1
	{
		bi := dm / 2
		biOk := -1
		bu := 0
		bo := dm
		for {
			sfmax := flattenDistribution(sfwork, wrk[:], dm, bi, p)
			nbits = tryThatOne(that, wrk[:], vbrsfmin, sfmax)
			if nbits <= target {
				biOk = bi
				bo = bi - 1
			} else {
				bu = bi + 1
			}
			if bu <= bo {
				bi = (bu + bo) / 2
			} else {
				break
			}
		}
		if biOk >= 0 {
			if bi != biOk {
				sfmax := flattenDistribution(sfwork, wrk[:], dm, biOk, p)
				nbits = tryThatOne(that, wrk[:], vbrsfmin, sfmax)
			}
			return
		}
	}

	// PART 2:
	{
		bi := (255 + p) / 2
		biOk := -1
		bu := p
		bo := 255
		for {
			sfmax := flattenDistribution(sfwork, wrk[:], dm, dm, bi)
			nbits = tryThatOne(that, wrk[:], vbrsfmin, sfmax)
			if nbits <= target {
				biOk = bi
				bo = bi - 1
			} else {
				bu = bi + 1
			}
			if bu <= bo {
				bi = (bu + bo) / 2
			} else {
				break
			}
		}
		if biOk >= 0 {
			if bi != biOk {
				sfmax := flattenDistribution(sfwork, wrk[:], dm, dm, biOk)
				nbits = tryThatOne(that, wrk[:], vbrsfmin, sfmax)
			}
			return
		}
	}

	// fall back to old code, likely to be never called
	searchGlobalStepsizeMax(that, wrk[:], vbrsfmin, target)
	_ = nbits
}

// reduceBitUsage tries better scalefactor storage (best_scalefac_store) and,
// when enabled, the best Huffman region split (best_huffman_divide), then returns
// the granule's resolved part2_3_length + part2_length (reduce_bit_usage,
// vbrquantize.c:1231).
func reduceBitUsage(gfc *LameInternalFlags, gr, ch int) int {
	cfg := &gfc.Cfg
	codInfo := &gfc.L3Side.Tt[gr][ch]
	// try some better scalefac storage
	gfc.bestScalefacStore(gr, ch, &gfc.L3Side)
	// best huffman_divide may save some bits too
	if cfg.UseBestHuffman == 1 {
		gfc.bestHuffmanDivide(codInfo)
	}
	return codInfo.Part23Length + codInfo.Part2Length
}

// VBRencodeFrame is LAME's VBR_encode_frame (vbrquantize.c:1254): the whole-frame
// vbr_mtrh orchestrator. For each granule/channel it surveys scalefactors
// (block_sf), allocates side info, encodes 'as is' and reduces bit usage; if the
// frame overflows max_bits it redistributes the available bits proportionally and
// re-runs outOfBitsStrategy per channel. Returns the total bits used by the
// frame. Panics (the C ERRORF + exit(-1)) if the result still overflows, which
// "should always be ok, iff there are no bugs".
//
// xr34orig is the per-granule/channel |xr|^(3/4) line array; l3Xmin the per-band
// allowed distortion; maxBits the per-granule/channel bit budget from the rate
// controller. The granule side info is written through gfc->l3_side.tt[gr][ch].
func VBRencodeFrame(gfc *LameInternalFlags, xr34orig *[2][2][576]float32,
	l3Xmin *[2][2][SFBMAX]float32, maxBits *[2][2]int) int {
	cfg := &gfc.Cfg
	var sfwork [2][2][SFBMAX]int
	var vbrsfmin [2][2][SFBMAX]int
	var that [2][2]algoT
	ngr := cfg.ModeGr
	nch := cfg.ChannelsOut

	var maxNbitsCh [2][2]int
	var maxNbitsGr [2]int
	maxNbitsFr := 0
	var useNbitsCh [2][2]int
	useNbitsCh[0][0] = maxBitsPerChannel + 1
	useNbitsCh[0][1] = maxBitsPerChannel + 1
	useNbitsCh[1][0] = maxBitsPerChannel + 1
	useNbitsCh[1][1] = maxBitsPerChannel + 1
	useNbitsGr := [2]int{maxBitsPerGranule + 1, maxBitsPerGranule + 1}
	useNbitsFr := maxBitsPerGranule + maxBitsPerGranule
	var gr, ch int
	var ok, sumFr int

	// set up some encoding parameters
	for gr = 0; gr < ngr; gr++ {
		maxNbitsGr[gr] = 0
		for ch = 0; ch < nch; ch++ {
			maxNbitsCh[gr][ch] = maxBits[gr][ch]
			useNbitsCh[gr][ch] = 0
			maxNbitsGr[gr] += maxBits[gr][ch]
			maxNbitsFr += maxBits[gr][ch]
			th := &that[gr][ch]
			if cfg.FullOuterLoop < 0 {
				th.find = guessScalefacX34
			} else {
				th.find = findScalefacX34
			}
			th.Gfc = gfc
			th.CodInfo = &gfc.L3Side.Tt[gr][ch]
			th.Xr34orig = xr34orig[gr][ch][:]
			if th.CodInfo.BlockType == ShortType {
				th.alloc = shortBlockConstrain
			} else {
				th.alloc = longBlockConstrain
			}
		}
	}
	// searches scalefactors
	for gr = 0; gr < ngr; gr++ {
		for ch = 0; ch < nch; ch++ {
			if maxBits[gr][ch] > 0 {
				th := &that[gr][ch]
				sf := sfwork[gr][ch][:]
				vm := vbrsfmin[gr][ch][:]
				vbrmax := blockSf(th, l3Xmin[gr][ch][:], sf, vm)
				th.alloc(th, sf, vm, vbrmax)
				bitcount(th)
			}
		}
	}
	// encode 'as is'
	useNbitsFr = 0
	for gr = 0; gr < ngr; gr++ {
		useNbitsGr[gr] = 0
		for ch = 0; ch < nch; ch++ {
			th := &that[gr][ch]
			if maxBits[gr][ch] > 0 {
				l3 := &th.CodInfo.L3Enc
				for i := range l3 {
					l3[i] = 0
				}
				quantizeAndCountBits(th)
			}
			useNbitsCh[gr][ch] = reduceBitUsage(gfc, gr, ch)
			useNbitsGr[gr] += useNbitsCh[gr][ch]
		}
		useNbitsFr += useNbitsGr[gr]
	}

	// check bit constrains
	if useNbitsFr <= maxNbitsFr {
		ok = 1
		for gr = 0; gr < ngr; gr++ {
			if useNbitsGr[gr] > maxBitsPerGranule {
				ok = 0
			}
			for ch = 0; ch < nch; ch++ {
				if useNbitsCh[gr][ch] > maxBitsPerChannel {
					ok = 0
				}
			}
		}
		if ok != 0 {
			return useNbitsFr
		}
	}

	// OK, we are in trouble and have to define how many bits are
	// to be used for each granule
	{
		ok = 1
		sumFr = 0

		for gr = 0; gr < ngr; gr++ {
			maxNbitsGr[gr] = 0
			for ch = 0; ch < nch; ch++ {
				if useNbitsCh[gr][ch] > maxBitsPerChannel {
					maxNbitsCh[gr][ch] = maxBitsPerChannel
				} else {
					maxNbitsCh[gr][ch] = useNbitsCh[gr][ch]
				}
				maxNbitsGr[gr] += maxNbitsCh[gr][ch]
			}
			if maxNbitsGr[gr] > maxBitsPerGranule {
				var f [2]float32
				var s float32 = 0
				for ch = 0; ch < nch; ch++ {
					if maxNbitsCh[gr][ch] > 0 {
						f[ch] = vqfQuadRoot(maxNbitsCh[gr][ch])
						s = vqfAddF(s, f[ch])
					} else {
						f[ch] = 0
					}
				}
				for ch = 0; ch < nch; ch++ {
					if s > 0 {
						maxNbitsCh[gr][ch] = vqfDistribute(maxBitsPerGranule, f[ch], s)
					} else {
						maxNbitsCh[gr][ch] = 0
					}
				}
				if nch > 1 {
					if maxNbitsCh[gr][0] > useNbitsCh[gr][0]+32 {
						maxNbitsCh[gr][1] += maxNbitsCh[gr][0]
						maxNbitsCh[gr][1] -= useNbitsCh[gr][0] + 32
						maxNbitsCh[gr][0] = useNbitsCh[gr][0] + 32
					}
					if maxNbitsCh[gr][1] > useNbitsCh[gr][1]+32 {
						maxNbitsCh[gr][0] += maxNbitsCh[gr][1]
						maxNbitsCh[gr][0] -= useNbitsCh[gr][1] + 32
						maxNbitsCh[gr][1] = useNbitsCh[gr][1] + 32
					}
					if maxNbitsCh[gr][0] > maxBitsPerChannel {
						maxNbitsCh[gr][0] = maxBitsPerChannel
					}
					if maxNbitsCh[gr][1] > maxBitsPerChannel {
						maxNbitsCh[gr][1] = maxBitsPerChannel
					}
				}
				maxNbitsGr[gr] = 0
				for ch = 0; ch < nch; ch++ {
					maxNbitsGr[gr] += maxNbitsCh[gr][ch]
				}
			}
			sumFr += maxNbitsGr[gr]
		}
		if sumFr > maxNbitsFr {
			{
				var f [2]float32
				var s float32 = 0
				for gr = 0; gr < ngr; gr++ {
					if maxNbitsGr[gr] > 0 {
						f[gr] = vqfSqrt(maxNbitsGr[gr])
						s = vqfAddF(s, f[gr])
					} else {
						f[gr] = 0
					}
				}
				for gr = 0; gr < ngr; gr++ {
					if s > 0 {
						maxNbitsGr[gr] = vqfDistribute(maxNbitsFr, f[gr], s)
					} else {
						maxNbitsGr[gr] = 0
					}
				}
			}
			if ngr > 1 {
				if maxNbitsGr[0] > useNbitsGr[0]+125 {
					maxNbitsGr[1] += maxNbitsGr[0]
					maxNbitsGr[1] -= useNbitsGr[0] + 125
					maxNbitsGr[0] = useNbitsGr[0] + 125
				}
				if maxNbitsGr[1] > useNbitsGr[1]+125 {
					maxNbitsGr[0] += maxNbitsGr[1]
					maxNbitsGr[0] -= useNbitsGr[1] + 125
					maxNbitsGr[1] = useNbitsGr[1] + 125
				}
				for gr = 0; gr < ngr; gr++ {
					if maxNbitsGr[gr] > maxBitsPerGranule {
						maxNbitsGr[gr] = maxBitsPerGranule
					}
				}
			}
			for gr = 0; gr < ngr; gr++ {
				var f [2]float32
				var s float32 = 0
				for ch = 0; ch < nch; ch++ {
					if maxNbitsCh[gr][ch] > 0 {
						f[ch] = vqfSqrt(maxNbitsCh[gr][ch])
						s = vqfAddF(s, f[ch])
					} else {
						f[ch] = 0
					}
				}
				for ch = 0; ch < nch; ch++ {
					if s > 0 {
						maxNbitsCh[gr][ch] = vqfDistribute(maxNbitsGr[gr], f[ch], s)
					} else {
						maxNbitsCh[gr][ch] = 0
					}
				}
				if nch > 1 {
					if maxNbitsCh[gr][0] > useNbitsCh[gr][0]+32 {
						maxNbitsCh[gr][1] += maxNbitsCh[gr][0]
						maxNbitsCh[gr][1] -= useNbitsCh[gr][0] + 32
						maxNbitsCh[gr][0] = useNbitsCh[gr][0] + 32
					}
					if maxNbitsCh[gr][1] > useNbitsCh[gr][1]+32 {
						maxNbitsCh[gr][0] += maxNbitsCh[gr][1]
						maxNbitsCh[gr][0] -= useNbitsCh[gr][1] + 32
						maxNbitsCh[gr][1] = useNbitsCh[gr][1] + 32
					}
					for ch = 0; ch < nch; ch++ {
						if maxNbitsCh[gr][ch] > maxBitsPerChannel {
							maxNbitsCh[gr][ch] = maxBitsPerChannel
						}
					}
				}
			}
		}
		// sanity check
		sumFr = 0
		for gr = 0; gr < ngr; gr++ {
			sumGr := 0
			for ch = 0; ch < nch; ch++ {
				sumGr += maxNbitsCh[gr][ch]
				if maxNbitsCh[gr][ch] > maxBitsPerChannel {
					ok = 0
				}
			}
			sumFr += sumGr
			if sumGr > maxBitsPerGranule {
				ok = 0
			}
		}
		if sumFr > maxNbitsFr {
			ok = 0
		}
		if ok == 0 {
			// we must have done something wrong, fallback to 'on_pe' based constrain
			for gr = 0; gr < ngr; gr++ {
				for ch = 0; ch < nch; ch++ {
					maxNbitsCh[gr][ch] = maxBits[gr][ch]
				}
			}
		}
	}

	// we already called the 'best_scalefac_store' function, so we need to reset
	// some variables before we can do it again.
	for ch = 0; ch < nch; ch++ {
		gfc.L3Side.Scfsi[ch][0] = 0
		gfc.L3Side.Scfsi[ch][1] = 0
		gfc.L3Side.Scfsi[ch][2] = 0
		gfc.L3Side.Scfsi[ch][3] = 0
	}
	for gr = 0; gr < ngr; gr++ {
		for ch = 0; ch < nch; ch++ {
			gfc.L3Side.Tt[gr][ch].ScalefacCompress = 0
		}
	}

	// alter our encoded data, until it fits into the target bitrate
	useNbitsFr = 0
	for gr = 0; gr < ngr; gr++ {
		useNbitsGr[gr] = 0
		for ch = 0; ch < nch; ch++ {
			th := &that[gr][ch]
			useNbitsCh[gr][ch] = 0
			if maxBits[gr][ch] > 0 {
				sf := sfwork[gr][ch][:]
				vm := vbrsfmin[gr][ch][:]
				cutDistribution(sf, sf, th.CodInfo.GlobalGain)
				outOfBitsStrategy(th, sf, vm, maxNbitsCh[gr][ch])
			}
			useNbitsCh[gr][ch] = reduceBitUsage(gfc, gr, ch)
			// assert(use_nbits_ch[gr][ch] <= max_nbits_ch[gr][ch])
			useNbitsGr[gr] += useNbitsCh[gr][ch]
		}
		useNbitsFr += useNbitsGr[gr]
	}

	// check bit constrains, but it should always be ok, iff there are no bugs
	if useNbitsFr <= maxNbitsFr {
		return useNbitsFr
	}

	panic("mp3: internal error in VBR new code (1313): frame bit budget overflow")
}
