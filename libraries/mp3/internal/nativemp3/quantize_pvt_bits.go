// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Per-channel bit allocation — the two LAME quantize_pvt.c routines that split
// a granule's bit budget across channels. This is a 1:1 translation of on_pe
// (quantize_pvt.c:430) and reduce_side (quantize_pvt.c:494) from the vendored
// LAME 3.100 encoder. They sit in the quantize_pvt translation unit but were
// deferred by the calc_xmin / calc_noise slice (quantize_pvt.go) because they
// only become reachable once the iteration loop (quantize_encode.go) lands.
//
// on_pe distributes the reservoir-derived target bits between the channels in
// proportion to their perceptual entropy; reduce_side moves bits from the side
// to the mid channel for joint-stereo frames. The CBR/ABR/VBR iteration loops
// call them once per granule.
//
// # Floating-point parity
//
// Both routines compute a few intermediate quantities in C `double` (the 700.0
// / .5 / .33 literals promote the int / FLOAT operands to double) and truncate
// the result back to int. The cgo oracle compiles quantize_pvt.c with
// -ffp-contract=off, so each multiply/divide rounds separately; Go's backend
// would otherwise fuse a product into an FMA. The double expressions are routed
// through the //go:noinline opAddBits / opReduceFac / opMoveBits helpers
// (quantize_pvt_bits_fp_strict.go) so the strict build separately-rounds,
// matching clang. int(float64) truncates toward zero in both C and Go.

// onPe allocates targBits[ch] for a granule based on each channel's perceptual
// entropy, returning the maximum allowed bits for the granule (on_pe,
// quantize_pvt.c:430). pe is the [2][2] perceptual-entropy array; cbr is
// non-zero in constant-bitrate mode (forwarded to ResvMaxBits).
//
// bugfixes rh 8/01: often allocated more than the allowed 4095 bits.
func (gfc *LameInternalFlags) onPe(pe *[2][2]float32, targBits *[2]int, meanBits, gr, cbr int) int {
	cfg := &gfc.Cfg
	extraBits := 0
	var tbits int
	var addBits [2]int

	// allocate targ_bits for granule
	gfc.ResvMaxBits(meanBits, &tbits, &extraBits, cbr)
	maxBits := tbits + extraBits
	if maxBits > maxBitsPerGranule { // hard limit per granule
		maxBits = maxBitsPerGranule
	}

	bits := 0
	for ch := 0; ch < cfg.ChannelsOut; ch++ {
		// allocate bits for each channel
		targBits[ch] = iMin(maxBitsPerChannel, tbits/cfg.ChannelsOut)

		// add_bits[ch] = targ_bits[ch] * pe[gr][ch] / 700.0 - targ_bits[ch]
		// (computed in double, truncated to int).
		addBits[ch] = opAddBits(targBits[ch], pe[gr][ch])

		// at most increase bits by 1.5*average
		if addBits[ch] > meanBits*3/4 {
			addBits[ch] = meanBits * 3 / 4
		}
		if addBits[ch] < 0 {
			addBits[ch] = 0
		}

		if addBits[ch]+targBits[ch] > maxBitsPerChannel {
			addBits[ch] = maxInt(0, maxBitsPerChannel-targBits[ch])
		}

		bits += addBits[ch]
	}
	if bits > extraBits && bits > 0 {
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			addBits[ch] = extraBits * addBits[ch] / bits
		}
	}

	for ch := 0; ch < cfg.ChannelsOut; ch++ {
		targBits[ch] += addBits[ch]
		extraBits -= addBits[ch]
	}

	bits = 0
	for ch := 0; ch < cfg.ChannelsOut; ch++ {
		bits += targBits[ch]
	}
	if bits > maxBitsPerGranule {
		for ch := 0; ch < cfg.ChannelsOut; ch++ {
			targBits[ch] *= maxBitsPerGranule
			targBits[ch] /= bits
		}
	}

	return maxBits
}

// reduceSide moves bits from the side channel to the mid channel for a
// joint-stereo granule, in proportion to the mid/side energy ratio
// (reduce_side, quantize_pvt.c:494). targBits[0] is mid, targBits[1] is side;
// maxBits caps their sum.
func reduceSide(targBits *[2]int, msEnerRatio float32, meanBits, maxBits int) {
	// ms_ener_ratio = 0: allocate 66/33 mid/side (fac=.33); =.5: 50/50 (fac=0).
	// fac = .33 * (.5 - ms_ener_ratio) / .5 — computed in double, narrowed.
	fac := opReduceFac(msEnerRatio)
	if fac < 0 {
		fac = 0
	}
	if fac > 0.5 {
		fac = 0.5
	}

	// number of bits to move from side channel to mid channel:
	// move_bits = fac * .5 * (targ_bits[0] + targ_bits[1]) — double, truncated.
	moveBits := opMoveBits(fac, targBits[0]+targBits[1])

	if moveBits > maxBitsPerChannel-targBits[0] {
		moveBits = maxBitsPerChannel - targBits[0]
	}
	if moveBits < 0 {
		moveBits = 0
	}

	if targBits[1] >= 125 {
		// dont reduce side channel below 125 bits
		if targBits[1]-moveBits > 125 {
			// if mid channel already has 2x more than average, dont bother.
			// mean_bits = bits per granule (for both channels)
			if targBits[0] < meanBits {
				targBits[0] += moveBits
			}
			targBits[1] -= moveBits
		} else {
			targBits[0] += targBits[1] - 125
			targBits[1] = 125
		}
	}

	moveBits = targBits[0] + targBits[1]
	if moveBits > maxBits {
		targBits[0] = (maxBits * targBits[0]) / moveBits
		targBits[1] = (maxBits * targBits[1]) / moveBits
	}
}
