// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Bit-reservoir FRAMING — LAME's encoder-side bit reservoir bookkeeping. This
// is a 1:1 translation of the vendored LAME 3.100 encoder
// (liblame/libmp3lame/reservoir.c, copyright Mark Taylor). It is a distinct C
// reference from the minimp3 decoder reservoir reassembly in reservoir.go
// (L3RestoreReservoir / L3SaveReservoir): reservoir.go reconstructs a decoded
// frame's main data from carried-over bytes, whereas these routines plan how
// many bits the encoder may spend on the current frame and how much to carry
// over to the next, sizing the reservoir and computing the resvDrain_pre /
// resvDrain_post / main_data_begin that format_bitstream (bitstream_format.go)
// then emits. The two coexist in nativemp3 because libraries/mp3 serves a single
// pure-Go encode+decode surface.
//
// # Scope of this slice
//
// reservoir.c's four entry points, called once per frame by the quantize
// iteration loop:
//
//   - ResvFrameBegin — sizes ResvMax and returns the frame's full bit budget.
//   - ResvMaxBits     — splits the budget into a per-granule target + extra
//                       reservoir bits.
//   - ResvAdjust      — debits the reservoir by a granule's actual usage.
//   - ResvFrameEnd    — credits the frame's mean bits, byte-aligns the
//                       reservoir, and drains any overflow into ancillary data.
//
// # Strict mode
//
// reservoir.c is otherwise integer arithmetic, with two double-precision
// scalings in ResvMaxBits: `ResvMax *= 0.9` (when substep_shaping is active) and
// `targBits -= .1 * mean_bits`. In the cgo oracle reservoir.c is compiled with
// -ffp-contract=off, so the `targBits - 0.1*mean_bits` is a separately rounded
// multiply then subtract; Go's backend could fuse the multiply into an FMS.
// Both scalings are therefore routed through the //go:noinline helpers in
// reservoir_encode_fp_strict.go / reservoir_encode_fp_default.go (resvMul /
// resvScale9), so the strict build rounds the product before combining, matching
// clang. Everything else here is integer and bit-identical.

// ResvFrameBegin updates the maximum reservoir size for the current frame and
// returns the maximum number of bits available to encode it; *meanBits receives
// the target bits per granule (ResvFrameBegin, reservoir.c:82).
//
//	int
//	ResvFrameBegin(lame_internal_flags * gfc, int *mean_bits)
//	{
//	    ...
//	    frameLength = getframebits(gfc);
//	    meanBits = (frameLength - cfg->sideinfo_len * 8) / cfg->mode_gr;
//	    resvLimit = (8 * 256) * cfg->mode_gr - 8;
//	    maxmp3buf = cfg->buffer_constraint;
//	    esv->ResvMax = maxmp3buf - frameLength;
//	    if (esv->ResvMax > resvLimit) esv->ResvMax = resvLimit;
//	    if (esv->ResvMax < 0 || cfg->disable_reservoir) esv->ResvMax = 0;
//	    fullFrameBits = meanBits * cfg->mode_gr + Min(esv->ResvSize, esv->ResvMax);
//	    if (fullFrameBits > maxmp3buf) fullFrameBits = maxmp3buf;
//	    l3_side->resvDrain_pre = 0;
//	    ... (pinfo copy-out, analysis only) ...
//	    *mean_bits = meanBits;
//	    return fullFrameBits;
//	}
//
// The gfc->pinfo (analysis) copy-out runs only when pinfo != NULL; the port
// omits it as the rest of the encoder omits the analysis path. The two asserts
// (ResvMax % 8 == 0, ResvMax >= 0) are debug-only invariants.
func (gfc *LameInternalFlags) ResvFrameBegin(meanBits *int) int {
	cfg := &gfc.Cfg
	esv := &gfc.SvEnc
	l3Side := &gfc.L3Side

	frameLength := gfc.getframebits()
	mean := (frameLength - cfg.SideinfoLen*8) / cfg.ModeGr

	// main_data_begin has 9 bits in MPEG-1, 8 bits MPEG-2
	resvLimit := (8*256)*cfg.ModeGr - 8

	// maximum allowed frame size
	maxmp3buf := cfg.BufferConstraint
	esv.ResvMax = maxmp3buf - frameLength
	if esv.ResvMax > resvLimit {
		esv.ResvMax = resvLimit
	}
	if esv.ResvMax < 0 || cfg.DisableReservoir != 0 {
		esv.ResvMax = 0
	}

	fullFrameBits := mean*cfg.ModeGr + minInt(esv.ResvSize, esv.ResvMax)

	if fullFrameBits > maxmp3buf {
		fullFrameBits = maxmp3buf
	}

	l3Side.ResvDrainPre = 0

	// gfc->pinfo (analysis) copy-out omitted (pinfo == NULL on the encode path).

	*meanBits = mean
	return fullFrameBits
}

// ResvMaxBits returns, via *targBits, the target number of bits to use for one
// granule and, via *extraBits, the amount extra available from the reservoir
// (ResvMaxBits, reservoir.c:174). cbr is non-zero in constant-bitrate mode.
//
//	void
//	ResvMaxBits(lame_internal_flags * gfc, int mean_bits, int *targ_bits, int *extra_bits, int cbr)
//	{
//	    ...
//	    int     ResvSize = esv->ResvSize, ResvMax = esv->ResvMax;
//	    if (cbr) ResvSize += mean_bits;
//	    if (gfc->sv_qnt.substep_shaping & 1) ResvMax *= 0.9;
//	    targBits = mean_bits;
//	    if (ResvSize * 10 > ResvMax * 9) {
//	        add_bits = ResvSize - (ResvMax * 9) / 10;
//	        targBits += add_bits;
//	        gfc->sv_qnt.substep_shaping |= 0x80;
//	    } else {
//	        add_bits = 0;
//	        gfc->sv_qnt.substep_shaping &= 0x7f;
//	        if (!cfg->disable_reservoir && !(gfc->sv_qnt.substep_shaping & 1))
//	            targBits -= .1 * mean_bits;
//	    }
//	    extraBits = (ResvSize < (esv->ResvMax * 6) / 10 ? ResvSize : (esv->ResvMax * 6) / 10);
//	    extraBits -= add_bits;
//	    if (extraBits < 0) extraBits = 0;
//	    *targ_bits = targBits;
//	    *extra_bits = extraBits;
//	}
//
// `ResvMax *= 0.9` and `targBits -= .1 * mean_bits` are the slice's only float
// arithmetic; both go through the resvScale9 / resvMul helpers so the truncation
// to int matches the -ffp-contract=off oracle (see this file's strict-mode note).
// Note ResvMax is the local copy (possibly *0.9), while the extraBits clamp uses
// esv->ResvMax (the unscaled field) — the port preserves that distinction.
func (gfc *LameInternalFlags) ResvMaxBits(meanBits int, targBits, extraBits *int, cbr int) {
	cfg := &gfc.Cfg
	esv := &gfc.SvEnc

	resvSize := esv.ResvSize
	resvMax := esv.ResvMax

	// compensate the saved bits used in the 1st granule
	if cbr != 0 {
		resvSize += meanBits
	}

	if gfc.SvQnt.SubstepShaping&1 != 0 {
		resvMax = resvScale9(resvMax) // ResvMax *= 0.9
	}

	targ := meanBits
	var addBits int

	// extra bits if the reservoir is almost full
	if resvSize*10 > resvMax*9 {
		addBits = resvSize - (resvMax*9)/10
		targ += addBits
		gfc.SvQnt.SubstepShaping |= 0x80
	} else {
		addBits = 0
		gfc.SvQnt.SubstepShaping &= 0x7f
		// build up reservoir (slower than FhG; rigged to give 100 at 128kbs)
		if cfg.DisableReservoir == 0 && (gfc.SvQnt.SubstepShaping&1) == 0 {
			// C: targBits -= .1 * mean_bits, i.e. the whole RHS
			// ((double)targBits - 0.1*mean_bits) is computed in double then
			// truncated back to int. resvSubTenth performs exactly that.
			targ = resvSubTenth(targ, meanBits)
		}
	}

	// amount from the reservoir we are allowed to use. ISO says 6/10
	extra := (esv.ResvMax * 6) / 10
	if resvSize < extra {
		extra = resvSize
	}
	extra -= addBits

	if extra < 0 {
		extra = 0
	}

	*targBits = targ
	*extraBits = extra
}

// ResvAdjust debits the reservoir by the bits a just-allocated granule consumed
// (ResvAdjust, reservoir.c:225).
//
//	void
//	ResvAdjust(lame_internal_flags * gfc, gr_info const *gi)
//	{
//	    gfc->sv_enc.ResvSize -= gi->part2_3_length + gi->part2_length;
//	}
func (gfc *LameInternalFlags) ResvAdjust(gi *GrInfo) {
	gfc.SvEnc.ResvSize -= gi.Part23Length + gi.Part2Length
}

// ResvFrameEnd credits the frame's mean bits to the reservoir, byte-aligns it,
// and drains any overflow first into the previous frame's ancillary data (via
// main_data_begin) and then into this frame's, computing resvDrain_pre /
// resvDrain_post (ResvFrameEnd, reservoir.c:238).
//
//	void
//	ResvFrameEnd(lame_internal_flags * gfc, int mean_bits)
//	{
//	    ...
//	    esv->ResvSize += mean_bits * cfg->mode_gr;
//	    stuffingBits = 0;
//	    l3_side->resvDrain_post = 0;
//	    l3_side->resvDrain_pre = 0;
//	    if ((over_bits = esv->ResvSize % 8) != 0) stuffingBits += over_bits;
//	    over_bits = (esv->ResvSize - stuffingBits) - esv->ResvMax;
//	    if (over_bits > 0) stuffingBits += over_bits;
//	    {
//	        int mdb_bytes = Min(l3_side->main_data_begin * 8, stuffingBits) / 8;
//	        l3_side->resvDrain_pre += 8 * mdb_bytes;
//	        stuffingBits -= 8 * mdb_bytes;
//	        esv->ResvSize -= 8 * mdb_bytes;
//	        l3_side->main_data_begin -= mdb_bytes;
//	    }
//	    l3_side->resvDrain_post += stuffingBits;
//	    esv->ResvSize -= stuffingBits;
//	}
//
// The two asserts (over_bits % 8 == 0, over_bits >= 0) are debug-only.
func (gfc *LameInternalFlags) ResvFrameEnd(meanBits int) {
	cfg := &gfc.Cfg
	esv := &gfc.SvEnc
	l3Side := &gfc.L3Side

	esv.ResvSize += meanBits * cfg.ModeGr
	stuffingBits := 0
	l3Side.ResvDrainPost = 0
	l3Side.ResvDrainPre = 0

	// we must be byte aligned
	if overBits := esv.ResvSize % 8; overBits != 0 {
		stuffingBits += overBits
	}

	overBits := (esv.ResvSize - stuffingBits) - esv.ResvMax
	if overBits > 0 {
		stuffingBits += overBits
	}

	// drain as many bits as possible into previous frame ancillary data
	{
		mdbBytes := minInt(l3Side.MainDataBegin*8, stuffingBits) / 8
		l3Side.ResvDrainPre += 8 * mdbBytes
		stuffingBits -= 8 * mdbBytes
		esv.ResvSize -= 8 * mdbBytes
		l3Side.MainDataBegin -= mdbBytes
	}
	// drain the rest into this frame's ancillary data
	l3Side.ResvDrainPost += stuffingBits
	esv.ResvSize -= stuffingBits
}
