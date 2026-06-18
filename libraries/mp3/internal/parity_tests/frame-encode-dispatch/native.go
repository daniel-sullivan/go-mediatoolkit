// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package frameencodedispatch

import "go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 dispatcher (EncodeMP3Frame) the same
// way oracle.c drives the vendored C: a stub FrameEncodeStages stands in for the
// heavy per-stage callees, feeding the dispatcher the fabricated psy-model
// outputs it reads back and recording the primebuff the filterbank prime shifts.
// These import only internal/nativemp3 (never libraries/mp3).
//
// stubStages mirrors oracle.c's inert stub callees one-to-one: L3PsychoAnalVbr
// writes the test-supplied pe / peMS / totEner / blocktype; MdctSub48 captures
// the first (prime) call's buffers; the iteration loops, FormatBitstream,
// AddVbrFrame and UpdateStats are no-ops; CopyBuffer returns a fixed byte count.

// stubStages is the Go counterpart of the oracle's stage stubs.
type stubStages struct {
	// psy[gr] is the fabricated L3psycho_anal_vbr output for granule gr.
	psy [2]psyOut
	// psyRet is the dispatcher-abort return (non-zero aborts the frame).
	psyRet int

	// mdctCalls counts MdctSub48 invocations; when armed, prime0/prime1 capture
	// the first (filterbank-prime) call's buffers. Disarmed by default so the
	// Stage-2 frame mdct (whose inbuf may be nil when frameInit is pre-latched)
	// is never recorded — matching the C stub's g_capture_arm gate.
	captureArm bool
	mdctCalls  int
	prime0     []float32
	prime1     []float32
}

// psyOut is one granule's fabricated psy-model output.
type psyOut struct {
	pe        [2]float32
	peMS      [2]float32
	totEner   [4]float32
	blocktype [2]int
}

func (s *stubStages) MdctSub48(gfc *nativemp3.LameInternalFlags, w0, w1 []float32) {
	if s.captureArm && s.mdctCalls == 0 {
		s.prime0 = append([]float32(nil), w0...)
		s.prime1 = append([]float32(nil), w1...)
	}
	s.mdctCalls++
}

func (s *stubStages) L3PsychoAnalVbr(gfc *nativemp3.LameInternalFlags, bufp [2][]float32, gr int,
	maskingLR, maskingMS *[2][2]nativemp3.III_psy_ratio,
	pe, peMS *[2]float32, totEner *[4]float32, blocktype *[2]int) int {
	o := &s.psy[gr]
	*pe = o.pe
	*peMS = o.peMS
	*totEner = o.totEner
	*blocktype = o.blocktype
	return s.psyRet
}

func (s *stubStages) AdjustATH(gfc *nativemp3.LameInternalFlags) {}

func (s *stubStages) CBRIterationLoop(gfc *nativemp3.LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]nativemp3.III_psy_ratio) {
}
func (s *stubStages) ABRIterationLoop(gfc *nativemp3.LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]nativemp3.III_psy_ratio) {
}
func (s *stubStages) VBROldIterationLoop(gfc *nativemp3.LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]nativemp3.III_psy_ratio) {
}
func (s *stubStages) VBRNewIterationLoop(gfc *nativemp3.LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]nativemp3.III_psy_ratio) {
}

func (s *stubStages) FormatBitstream(gfc *nativemp3.LameInternalFlags) {}

func (s *stubStages) CopyBuffer(gfc *nativemp3.LameInternalFlags, mp3buf []byte, mp3bufSize, mt int) int {
	return 0
}

func (s *stubStages) AddVbrFrame(gfc *nativemp3.LameInternalFlags) {}
func (s *stubStages) UpdateStats(gfc *nativemp3.LameInternalFlags) {}

// The frame-encode dispatcher only invokes the per-stage methods above. The
// init-driver methods of the unified EncoderStages seam (ppflt, preset, qval,
// iteration, psymodel, buffer-constraint, bandwidth) are never reached by
// EncodeMP3Frame, so they are inert stubs here, present only to satisfy the
// interface (mirroring the C stub's unused callees).
func (s *stubStages) InitParamsPpflt(gfc *nativemp3.LameInternalFlags) {}
func (s *stubStages) ApplyPreset(gfc *nativemp3.LameInternalFlags, gfp *nativemp3.LameGlobalFlags, bitrate, cbr int) {
}
func (s *stubStages) InitQval(gfc *nativemp3.LameInternalFlags, gfp *nativemp3.LameGlobalFlags) {}
func (s *stubStages) IterationInit(gfc *nativemp3.LameInternalFlags)                            {}
func (s *stubStages) PsymodelInit(gfc *nativemp3.LameInternalFlags, gfp *nativemp3.LameGlobalFlags) int {
	return 0
}
func (s *stubStages) GetMaxFrameBufferSizeByConstraint(gfc *nativemp3.LameInternalFlags, strictISO int) int {
	return 0
}
func (s *stubStages) OptimumBandwidth(bitrate int) (lower, upper float64)    { return 0, 0 }
func (s *stubStages) OptimumSamplefreq(lowpassFreq, inputSamplefreq int) int { return 0 }

// nativeFE holds a nativemp3.LameInternalFlags plus its stub stages, and the
// per-channel input PCM the dispatcher reads.
type nativeFE struct {
	gfc    nativemp3.LameInternalFlags
	stub   *stubStages
	inbufL []float32
	inbufR []float32
}

func newNativeFE() *nativeFE {
	n := &nativeFE{stub: new(stubStages)}
	n.gfc.Stages = n.stub
	return n
}

func (n *nativeFE) setCfg(samplerateOut, channelsOut, modeGr, mode, forceMS, vbr, writeLameTag int) {
	n.gfc.Cfg.SamplerateOut = samplerateOut
	n.gfc.Cfg.ChannelsOut = channelsOut
	n.gfc.Cfg.ModeGr = modeGr
	n.gfc.Cfg.Mode = mode
	n.gfc.Cfg.ForceMs = forceMS
	n.gfc.Cfg.Vbr = vbr
	n.gfc.Cfg.WriteLameTag = writeLameTag
}

func (n *nativeFE) setPad(fracSpF, slotLag int) {
	n.gfc.SvEnc.FracSpF = fracSpF
	n.gfc.SvEnc.SlotLag = slotLag
}

func (n *nativeFE) setPefirbuf(buf [19]float32) { n.gfc.SvEnc.PefirBuf = buf }

func (n *nativeFE) setFrameInit(v int) { n.gfc.LameEncodeFrameInit = v }

func (n *nativeFE) setPsy(gr int, pe, peMS [2]float32, totEner [4]float32, blocktype [2]int) {
	n.stub.psy[gr] = psyOut{pe: pe, peMS: peMS, totEner: totEner, blocktype: blocktype}
}

func (n *nativeFE) setPsyRet(ret int) { n.stub.psyRet = ret }

func (n *nativeFE) armCapture() { n.stub.captureArm = true }

func (n *nativeFE) setInput(ch int, vals []float32) {
	cp := append([]float32(nil), vals...)
	if ch == 0 {
		n.inbufL = cp
	} else {
		n.inbufR = cp
	}
}

func (n *nativeFE) encode() int {
	const mp3bufSize = 16384
	mp3buf := make([]byte, mp3bufSize)
	return nativemp3.EncodeMP3Frame(&n.gfc, n.inbufL, n.inbufR, mp3buf, mp3bufSize)
}

func (n *nativeFE) padding() int     { return n.gfc.OvEnc.Padding }
func (n *nativeFE) slotLag() int     { return n.gfc.SvEnc.SlotLag }
func (n *nativeFE) modeExt() int     { return n.gfc.OvEnc.ModeExt }
func (n *nativeFE) frameNumber() int { return n.gfc.OvEnc.FrameNumber }
func (n *nativeFE) frameInit() int   { return n.gfc.LameEncodeFrameInit }

func (n *nativeFE) pefirbuf(i int) float32 { return n.gfc.SvEnc.PefirBuf[i] }

func (n *nativeFE) blockType(gr, ch int) int { return n.gfc.L3Side.Tt[gr][ch].BlockType }
func (n *nativeFE) mixedBlockFlag(gr, ch int) int {
	return n.gfc.L3Side.Tt[gr][ch].MixedBlockFlag
}

func (n *nativeFE) mdctCalls() int       { return n.stub.mdctCalls }
func (n *nativeFE) prime0(i int) float32 { return n.stub.prime0[i] }
func (n *nativeFE) prime1(i int) float32 { return n.stub.prime1[i] }
