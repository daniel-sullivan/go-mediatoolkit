// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

// Frame-encode dispatcher: a 1:1 Go translation of LAME 3.100's per-frame
// encode driver. The C reference is
// libraries/mp3/liblame/libmp3lame/encoder.c — specifically
// lame_encode_mp3_frame (encoder.c:305) and its one-time prime-the-filterbank
// helper lame_encode_frame_init (encoder.c:189).
//
// The work-list names the C source as lame.c; the function that actually
// dispatches one Layer III frame through the five encode stages (psy model,
// MDCT, M/S-vs-L/R decision, quantization loop, bitstream formatting) lives in
// encoder.c. lame.c's lame_encode_buffer_sample_t (lame.c:1704) only chops the
// PCM ring buffer into 576*mode_gr-sample frames and calls this function once
// per frame; the dispatch loop the work-list describes ("NewEncoder loop /
// psycho analysis loop / quantize/bitstream loop") is the body of
// lame_encode_mp3_frame. This slice translates that body.
//
// # License fence (LGPL)
//
// encoder.c is part of LAME (libmp3lame), distributed under the GNU Library
// General Public License. This translation is a derivative work of that LGPL
// source, so — like the rest of the LAME 1:1 encoder port (mdct_analysis.go,
// mdct_fft.go) — it is guarded by the mp3lame build tag and excluded from the
// project's default (MIT/minimp3-derived) builds. A bare `go build ./...`
// never compiles this code; only `-tags mp3lame` makes the encoder dispatcher
// visible.
//
// # Scope of this slice ("frame-encode-dispatch")
//
// This slice owns only the dispatcher's own control flow and arithmetic:
//
//   - the first-frame prime of the MDCT/polyphase filterbank
//     (lame_encode_frame_init, encoder.c:189);
//   - the per-frame padding decision (the slot_lag / frac_SpF accumulator,
//     encoder.c:348);
//   - the Stage 1 psychoacoustic-analysis loop over granules (encoder.c:360),
//     which collects per-granule masking, perceptual entropy and block types;
//   - the Stage 3 M/S-vs-L/R mode decision (encoder.c:413), including the
//     JOINT_STEREO PE-sum comparison and block-type agreement check;
//   - the Stage 4 perceptual-entropy smoothing FIR for CBR/ABR
//     (encoder.c:489) and the vbr-mode switch that selects an iteration loop;
//   - the Stage 5 bitstream-format / copy-out tail (encoder.c:544) and the
//     per-frame bookkeeping (frame_number, VBR-tag, stats).
//
// The heavy callees each stage invokes — L3psycho_anal_vbr (psymodel.c),
// adjust_ATH (psymodel.c), mdct_sub48 (newmdct.c), CBR/ABR/VBR_*_iteration_loop
// (quantize.c), format_bitstream / copy_buffer (bitstream.c), AddVbrFrame
// (VbrTag.c) and updateStats (lame.c) — are separate slices. The dispatcher
// reaches them through the EncoderStages seam below, exactly as the C
// calls out to other translation units. Wiring a concrete EncoderStages is
// a later slice's job; this file translates the orchestration faithfully and
// does not "improve" the stage ordering or the decisions.
//
// # Floating-point type
//
// LAME's `FLOAT` typedef is `float` (32-bit); see liblame/libmp3lame/machine.h.
// Every FLOAT below is therefore float32. The dispatcher's M/S energy ratio,
// PE sums and the smoothing FIR are routed through the //go:noinline helpers
// in frame_encode_fp_strict.go so the mp3_strict build cannot fuse a*b+c into
// an FMA (matching the cgo oracle compiled with -ffp-contract=off). The
// default build (frame_encode_fp_default.go) may fuse/vectorize and is not a
// bit-exact target.
package nativemp3

// Frame / filterbank geometry constants the dispatcher uses, mirrored 1:1 from
// the LAME headers.
//
//   - MDCTDELAY is the MDCT look-ahead delay in samples (encoder.h:82,
//     #define MDCTDELAY 48).
//   - FFTOFFSET is the offset of the psy-model FFT window inside the granule
//     (encoder.h:83, #define FFTOFFSET (224+MDCTDELAY)).
//   - encGranule is one Layer III granule length in samples (576); the C uses
//     the literal 576 throughout encoder.c.
//
// (SHORT_TYPE / BLKSIZE are already defined in the psymodel slice as
// SHORT_TYPE-equivalent block-type values and BLKSIZE; this slice uses
// blockTypeShort for the SHORT_TYPE filterbank prime to avoid redefining
// them.)
const (
	MDCTDELAY  = 48
	FFTOFFSET  = 224 + MDCTDELAY
	encGranule = 576

	// blockTypeShort is encoder.h:120 #define SHORT_TYPE 2, the block type the
	// filterbank prime forces on every granule.
	blockTypeShort = 2

	// primeLookahead is the 286-sample look-ahead the filterbank prime shifts
	// the first frame's PCM by (encoder.c:205, the `286 +` in the prime loop
	// bound and the primebuff sizing).
	primeLookahead = 286
)

// MPG_MD_* stereo mode_ext selector values (encoder.h:134/136). MPG_MD_LR_LR
// is independent L/R coding of the two channels; MPG_MD_MS_LR is mid/side
// coding. The dispatcher writes one of these into EncResult.ModeExt.
const (
	mpgMdLRLR = 0 // MPG_MD_LR_LR
	mpgMdMSLR = 2 // MPG_MD_MS_LR
)

// vbr_mode selector values (include/lame.h:49 enum vbr_mode_e). The dispatcher
// branches on these to pick an iteration loop and to gate the PE smoothing FIR.
const (
	vbrOff  = 0 // vbr_off
	vbrMt   = 1 // vbr_mt   (obsolete, same as vbr_mtrh)
	vbrRh   = 2 // vbr_rh
	vbrAbr  = 3 // vbr_abr
	vbrMtrh = 4 // vbr_mtrh
)

// bFalse / bTrue mirror LAME's FALSE / TRUE (util.h:41/45). The dispatcher
// stores them into the integer EncResult.Padding flag, exactly as the C does.
const (
	bFalse = 0
	bTrue  = 1
)

// errFrameAbort is the value EncodeMP3Frame returns when the psy model fails
// (encoder.c:378, `return -4`).
const errFrameAbort = -4

// The per-granule gr_info, the encoder state-var struct (EncStateVar_t), the
// per-frame result (EncResult_t), the whole lame_internal_flags context and the
// heavy-stage seam this dispatcher threads through are now defined once, in the
// unified context (context.go):
//
//   - the per-granule struct is the shared GrInfo (gfc->l3_side.tt[gr][ch],
//     reached here as gfc.L3Side.Tt[gr][ch]);
//   - the padding accumulator (frac_SpF / slot_lag) and PE FIR history
//     (pefirbuf) live on the shared EncStateVar (gfc.SvEnc);
//   - the padding flag / mode_ext / frame counter live on the shared EncResult
//     (gfc.OvEnc);
//   - the dispatcher's force_ms / vbr / write_lame_tag are cfg->* fields and
//     are read off gfc.Cfg.ForceMs / gfc.Cfg.Vbr / gfc.Cfg.WriteLameTag,
//     matching the C exactly (they were carried on the FE mirror only because
//     the per-slice SessionConfig subset did not yet hold them);
//   - the context itself is LameInternalFlags, the Go stand-in for `gfc`, and
//     the heavy-stage seam is the unified EncoderStages interface.

// lameEncodeFrameInit primes the MDCT/polyphase filterbank with a short block
// on the first frame. A 1:1 translation of lame_encode_frame_init
// (encoder.c:189).
//
// The C builds two 286+1152+576-sample prime buffers: the first `framesize`
// (= 576*mode_gr) samples are zero, then the input PCM is copied in starting at
// `framesize`, shifting the signal by the filterbank look-ahead. It forces every
// granule to SHORT_TYPE, runs mdct_sub48 to fill the filterbank history, and is
// run exactly once (latched by gfc.LameEncodeFrameInit). The C's two assertions
// on mf_size are debug-only checks and are not part of the encode output, so
// they are omitted from the port (Go has no NDEBUG-gated assert; reproducing
// them would change no emitted bytes).
func lameEncodeFrameInit(gfc *LameInternalFlags, inbuf [2][]float32) {
	cfg := &gfc.Cfg

	if gfc.LameEncodeFrameInit == 0 {
		// primebuff0 / primebuff1 are encoder.c:197-198
		// sample_t primebuff[286 + 1152 + 576].
		var primebuff0 [primeLookahead + 1152 + encGranule]float32
		var primebuff1 [primeLookahead + 1152 + encGranule]float32
		framesize := encGranule * cfg.ModeGr // encoder.c:199
		gfc.LameEncodeFrameInit = 1          // encoder.c:202

		// encoder.c:205-217: zero the first `framesize` samples, then shift the
		// input in. (primebuff is already zero-initialised in Go, mirroring the
		// memset at encoder.c:203-204.)
		for i, j := 0, 0; i < primeLookahead+encGranule*(1+cfg.ModeGr); i++ {
			if i < framesize {
				primebuff0[i] = 0
				if cfg.ChannelsOut == 2 {
					primebuff1[i] = 0
				}
			} else {
				primebuff0[i] = inbuf[0][j]
				if cfg.ChannelsOut == 2 {
					primebuff1[i] = inbuf[1][j]
				}
				j++
			}
		}

		// encoder.c:219-223: force SHORT_TYPE on every granule before priming.
		for gr := 0; gr < cfg.ModeGr; gr++ {
			for ch := 0; ch < cfg.ChannelsOut; ch++ {
				gfc.L3Side.Tt[gr][ch].BlockType = blockTypeShort
			}
		}

		// encoder.c:224: polyphase filtering / mdct prime.
		gfc.Stages.MdctSub48(gfc, primebuff0[:], primebuff1[:])
	}
}

// EncodeMP3Frame encodes one Layer III frame, returning the number of mp3 bytes
// written into mp3buf (or a negative error code). A 1:1 translation of
// lame_encode_mp3_frame (encoder.c:305). errFrameAbort below (-4) mirrors the
// C's `return -4` when the psy model fails.
//
// The dispatcher runs the five encode stages in order: (1) the psychoacoustic
// model per granule, (2) MDCT, (3) the M/S-vs-L/R stereo decision, (4) the
// quantization iteration loop selected by cfg.vbr, and (5) bitstream
// formatting + copy-out. inbufL / inbufR are the frame's left/right PCM (the
// dispatcher reads them with the encoder.c:372 look-ahead offset), and mp3buf
// receives the emitted frame.
func EncodeMP3Frame(gfc *LameInternalFlags, inbufL, inbufR []float32, mp3buf []byte, mp3bufSize int) int {
	cfg := &gfc.Cfg
	var mp3count int

	var maskingLR [2][2]III_psy_ratio // encoder.c:315 III_psy_ratio masking_LR[2][2]
	var maskingMS [2][2]III_psy_ratio // encoder.c:316 III_psy_ratio masking_MS[2][2]
	var masking *[2][2]III_psy_ratio  // encoder.c:317 pointer to selected maskings

	inbuf := [2][]float32{inbufL, inbufR} // encoder.c:318/329-330

	var totEner [2][4]float32 // encoder.c:320 FLOAT tot_ener[2][4]
	// encoder.c:321 FLOAT ms_ener_ratio[2] = { .5, .5 }.
	msEnerRatio := [2]float32{0.5, 0.5}
	// encoder.c:322-324 FLOAT pe[2][2] = {{0,0},{0,0}}, pe_MS[2][2] = {{0,0},{0,0}}.
	var pe [2][2]float32
	var peMS [2][2]float32
	var peUse *[2][2]float32 // encoder.c:325 FLOAT (*pe_use)[2]

	var ch, gr int

	// encoder.c:332-336: first run primes the filterbank.
	if gfc.LameEncodeFrameInit == 0 {
		lameEncodeFrameInit(gfc, inbuf)
	}

	// ----- padding (encoder.c:339-352) -----
	// "MPEG-Layer3 / Bitstream Syntax and Decoding" padding accumulator. No
	// padding for the very first frame. (slot_lag/frac_SpF are integers — this
	// arithmetic is not FP and never routes through the fe* helpers.)
	gfc.OvEnc.Padding = bFalse
	gfc.SvEnc.SlotLag -= gfc.SvEnc.FracSpF
	if gfc.SvEnc.SlotLag < 0 {
		gfc.SvEnc.SlotLag += cfg.SamplerateOut
		gfc.OvEnc.Padding = bTrue
	}

	// ----- Stage 1: psychoacoustic model (encoder.c:356-393) -----
	// The psy model has a 1-granule (576) delay that is compensated by reading
	// the next granule's window (the +576 in bufp).
	{
		var bufp [2][]float32 // encoder.c:366 const sample_t *bufp[2]
		var blocktype [2]int  // encoder.c:367 int blocktype[2]

		for gr = 0; gr < cfg.ModeGr; gr++ {
			for ch = 0; ch < cfg.ChannelsOut; ch++ {
				// encoder.c:372 bufp[ch] = &inbuf[ch][576 + gr*576 - FFTOFFSET].
				bufp[ch] = inbuf[ch][encGranule+gr*encGranule-FFTOFFSET:]
			}
			// encoder.c:374-376.
			ret := gfc.Stages.L3PsychoAnalVbr(gfc, bufp, gr,
				&maskingLR, &maskingMS,
				&pe[gr], &peMS[gr], &totEner[gr], &blocktype)
			if ret != 0 {
				return errFrameAbort // encoder.c:378 return -4
			}

			if cfg.Mode == modeJointStereo { // encoder.c:380
				// encoder.c:381 ms_ener_ratio[gr] = tot_ener[gr][2] + tot_ener[gr][3].
				msEnerRatio[gr] = feAdd(totEner[gr][2], totEner[gr][3])
				if msEnerRatio[gr] > 0 { // encoder.c:382
					// encoder.c:383 ms_ener_ratio[gr] = tot_ener[gr][3] / ms_ener_ratio[gr].
					msEnerRatio[gr] = feDiv(totEner[gr][3], msEnerRatio[gr])
				}
			}

			// encoder.c:386-391: block-type flags.
			for ch = 0; ch < cfg.ChannelsOut; ch++ {
				codInfo := &gfc.L3Side.Tt[gr][ch]
				codInfo.BlockType = blocktype[ch]
				codInfo.MixedBlockFlag = 0
			}
		}
	}

	// encoder.c:397: auto-adjust of ATH, useful for low volume.
	gfc.Stages.AdjustATH(gfc)

	// ----- Stage 2: MDCT (encoder.c:400-405) -----
	// polyphase filtering / mdct over both channels' PCM.
	gfc.Stages.MdctSub48(gfc, inbuf[0], inbuf[1])

	// ----- Stage 3: MS/LR decision (encoder.c:408-461) -----
	gfc.OvEnc.ModeExt = mpgMdLRLR // encoder.c:413

	if gfc.Cfg.ForceMs != 0 { // encoder.c:415 cfg->force_ms
		gfc.OvEnc.ModeExt = mpgMdMSLR // encoder.c:416
	} else if cfg.Mode == modeJointStereo { // encoder.c:418
		// ms_ratio is scaled (historical) to look like side/total. [0] and [1]
		// are the two MPEG-1 granules; in MPEG-2 it's a faked average.
		var sumPeMS float32 // encoder.c:431 FLOAT sum_pe_MS = 0
		var sumPeLR float32 // encoder.c:432 FLOAT sum_pe_LR = 0
		for gr = 0; gr < cfg.ModeGr; gr++ {
			for ch = 0; ch < cfg.ChannelsOut; ch++ {
				sumPeMS = feAdd(sumPeMS, peMS[gr][ch]) // encoder.c:435
				sumPeLR = feAdd(sumPeLR, pe[gr][ch])   // encoder.c:436
			}
		}

		// encoder.c:441: M/S would not use much more bits than L/R. The C compares
		// `sum_pe_MS <= 1.00 * sum_pe_LR`; 1.00 is exact and `1.0 * x == x` rounds
		// to x for any finite float, so routing it through feMul changes no bit
		// (the multiply is identity), and the comparison is exact either way.
		if sumPeMS <= feMul(1.00, sumPeLR) {
			gi0 := &gfc.L3Side.Tt[0]            // encoder.c:443 &tt[0][0]
			gi1 := &gfc.L3Side.Tt[cfg.ModeGr-1] // encoder.c:444 &tt[mode_gr-1][0]
			// encoder.c:446: both granules' two channels must agree on block type.
			if gi0[0].BlockType == gi0[1].BlockType && gi1[0].BlockType == gi1[1].BlockType {
				gfc.OvEnc.ModeExt = mpgMdMSLR // encoder.c:448
			}
		}
	}

	// encoder.c:453-461: bit and noise allocation — select masking + pe set.
	if gfc.OvEnc.ModeExt == mpgMdMSLR {
		masking = &maskingMS // encoder.c:455 use MS masking
		peUse = &peMS        // encoder.c:456
	} else {
		masking = &maskingLR // encoder.c:459 use LR masking
		peUse = &pe          // encoder.c:460
	}

	// encoder.c:464-482: copy data for the MP3 frame analyzer. cfg.analysis is
	// the GTK analysis front end (pinfo), which this port does not carry, so the
	// whole `if (cfg->analysis && gfc->pinfo != NULL)` block is a no-op here. It
	// emits no frame bytes; it only populates the analyzer GUI's scratch.

	// ----- Stage 4: quantization loop (encoder.c:485-535) -----
	if gfc.Cfg.Vbr == vbrOff || gfc.Cfg.Vbr == vbrAbr { // encoder.c:489 cfg->vbr
		// encoder.c:490-494: PE smoothing FIR coefficients. In the C these are a
		// `static FLOAT const fircoef[9]` whose `* 5` initializers are folded by
		// the compiler in double precision and narrowed to float (one round). Go's
		// untyped float32 constant expressions are likewise evaluated in arbitrary
		// precision and rounded once to float32, so writing them as constant
		// products (not runtime feMul calls, which would double-round the float32
		// operands) matches the C's constant table bit-for-bit.
		fircoef := [9]float32{
			-0.0207887 * 5, -0.0378413 * 5, -0.0432472 * 5, -0.031183 * 5,
			7.79609e-18 * 5, 0.0467745 * 5, 0.10091 * 5, 0.151365 * 5,
			0.187098 * 5,
		}

		var f float32

		// encoder.c:499-500: shift the PE history down one tap.
		for i := 0; i < 18; i++ {
			gfc.SvEnc.PefirBuf[i] = gfc.SvEnc.PefirBuf[i+1]
		}

		// encoder.c:502-506: push this frame's summed PE into the newest tap.
		f = 0.0
		for gr = 0; gr < cfg.ModeGr; gr++ {
			for ch = 0; ch < cfg.ChannelsOut; ch++ {
				f = feAdd(f, peUse[gr][ch])
			}
		}
		gfc.SvEnc.PefirBuf[18] = f

		// encoder.c:508-510: symmetric FIR convolution about the centre tap.
		f = gfc.SvEnc.PefirBuf[9]
		for i := 0; i < 9; i++ {
			f = feFma(f, feAdd(gfc.SvEnc.PefirBuf[i], gfc.SvEnc.PefirBuf[18-i]), fircoef[i])
		}

		// encoder.c:512: f = (670 * 5 * cfg->mode_gr * cfg->channels_out) / f.
		// The numerator is an all-int product in the C (670*5*mode_gr*channels_out),
		// converted to FLOAT only for the division (C's `int / float` promotes the
		// int operand). Compute it as a Go int first, then narrow to float32, so the
		// numerator is a single round of the exact integer (not a chain of float32
		// multiplies); the divide then routes through feDiv.
		f = feDiv(float32(670*5*cfg.ModeGr*cfg.ChannelsOut), f)
		// encoder.c:513-517: scale every PE by the smoothing factor.
		for gr = 0; gr < cfg.ModeGr; gr++ {
			for ch = 0; ch < cfg.ChannelsOut; ch++ {
				peUse[gr][ch] = feMul(peUse[gr][ch], f)
			}
		}
	}

	// encoder.c:519-535: dispatch to the iteration loop selected by cfg.vbr.
	switch gfc.Cfg.Vbr { // encoder.c:519 cfg->vbr
	default:
		fallthrough
	case vbrOff: // encoder.c:521-524
		gfc.Stages.CBRIterationLoop(gfc, peUse, &msEnerRatio, masking)
	case vbrAbr: // encoder.c:525-527
		gfc.Stages.ABRIterationLoop(gfc, peUse, &msEnerRatio, masking)
	case vbrRh: // encoder.c:528-530
		gfc.Stages.VBROldIterationLoop(gfc, peUse, &msEnerRatio, masking)
	case vbrMt, vbrMtrh: // encoder.c:531-534
		gfc.Stages.VBRNewIterationLoop(gfc, peUse, &msEnerRatio, masking)
	}

	// ----- Stage 5: bitstream formatting (encoder.c:538-547) -----
	gfc.Stages.FormatBitstream(gfc)                              // encoder.c:544 write the frame
	mp3count = gfc.Stages.CopyBuffer(gfc, mp3buf, mp3bufSize, 1) // encoder.c:547 copy into array

	if gfc.Cfg.WriteLameTag != 0 { // encoder.c:550 cfg->write_lame_tag
		gfc.Stages.AddVbrFrame(gfc) // encoder.c:551
	}

	// encoder.c:554-567: analysis (pinfo) copy-out — omitted, as above.

	gfc.OvEnc.FrameNumber++ // encoder.c:569

	gfc.Stages.UpdateStats(gfc) // encoder.c:571

	return mp3count // encoder.c:573
}
