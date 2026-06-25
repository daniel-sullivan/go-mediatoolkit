// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package bitstreamformat

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"

// enc_native.go drives the pure-Go nativemp3 LAME-encoder bitstream-format port
// (bitstream_format.go / reservoir_encode.go) through helpers shaped like the
// cgoEnc oracle wrappers, so enc_parity_test.go can compare the two sides over a
// uniform surface. These import only internal/nativemp3 (never libraries/mp3).
//
// The Go context (LameInternalFlags) exposes Cfg / OvEnc / SvEnc / L3Side / Bs
// as public fields, so the driver primes and reads them directly; the
// bitstream.c methods are reached through the exported parity hooks
// (parityhooks_bitstream_format.go) and the reservoir.c methods through their
// already-exported names.

// encCfg / encGr mirror the C setter argument bundles so the test fabricates one
// shared description and applies it to both sides identically.
type encCfg struct {
	version          int
	samplerateOut    int
	samplerateIndex  int
	sideinfoLen      int
	channelsOut      int
	modeGr           int
	mode             int
	errorProtection  int
	extension        int
	copyright        int
	original         int
	emphasis         int
	disableReservoir int
	avgBitrate       int
	bufferConstraint int
}

type encGr struct {
	part23Length      int
	part2Length       int
	bigValues         int
	count1            int
	globalGain        int
	scalefacCompress  int
	blockType         int
	mixedBlockFlag    int
	region0Count      int
	region1Count      int
	preflag           int
	scalefacScale     int
	count1tableSelect int
	sfbdivide         int
	sfbmax            int
}

// nativeEnc wraps a fresh nativemp3 context with a sized output buffer.
type nativeEnc struct {
	gfc     nativemp3.LameInternalFlags
	bufSize int
}

func newNativeEnc(bufSize int) *nativeEnc {
	n := &nativeEnc{bufSize: bufSize}
	n.gfc.Bs.Buf = make([]byte, bufSize)
	n.gfc.Bs.BufSize = bufSize
	n.gfc.Bs.Totbit = 0
	n.gfc.Bs.BufByteIdx = 0
	n.gfc.Bs.BufBitIdx = 0
	return n
}

func (n *nativeEnc) free() {}

// ---- config / state setters ----

func (n *nativeEnc) setCfg(cf encCfg) {
	c := &n.gfc.Cfg
	c.Version = cf.version
	c.SamplerateOut = cf.samplerateOut
	c.SamplerateIndex = cf.samplerateIndex
	c.SideinfoLen = cf.sideinfoLen
	c.ChannelsOut = cf.channelsOut
	c.ModeGr = cf.modeGr
	c.Mode = cf.mode
	c.ErrorProtection = cf.errorProtection
	c.Extension = cf.extension
	c.Copyright = cf.copyright
	c.Original = cf.original
	c.Emphasis = cf.emphasis
	c.DisableReservoir = cf.disableReservoir
	c.AvgBitrate = cf.avgBitrate
	c.BufferConstraint = cf.bufferConstraint
}

func (n *nativeEnc) setOv(bitrateIndex, padding, modeExt int) {
	n.gfc.OvEnc.BitrateIndex = bitrateIndex
	n.gfc.OvEnc.Padding = padding
	n.gfc.OvEnc.ModeExt = modeExt
}

func (n *nativeEnc) setSv(hPtr, wPtr, ancillaryFlag, resvSize, resvMax int) {
	s := &n.gfc.SvEnc
	s.HPtr = hPtr
	s.WPtr = wPtr
	s.AncillaryFlag = ancillaryFlag
	s.ResvSize = resvSize
	s.ResvMax = resvMax
}

func (n *nativeEnc) setSubstepShaping(v int) { n.gfc.SvQnt.SubstepShaping = v }

func (n *nativeEnc) primeHeader(slot, writeTiming, ptr int, buf []byte) {
	h := &n.gfc.SvEnc.Header[slot]
	h.WriteTiming = writeTiming
	h.Ptr = ptr
	for i := range h.Buf {
		h.Buf[i] = 0
	}
	copy(h.Buf[:], buf)
}

func (n *nativeEnc) disarmHeaders(sentinel int) {
	for i := range n.gfc.SvEnc.Header {
		n.gfc.SvEnc.Header[i].WriteTiming = sentinel
	}
}

func (n *nativeEnc) setBs(totbit, bufByteIdx, bufBitIdx int) {
	n.gfc.Bs.Totbit = totbit
	n.gfc.Bs.BufByteIdx = bufByteIdx
	n.gfc.Bs.BufBitIdx = bufBitIdx
}

func (n *nativeEnc) setSide(mainDataBegin, privateBits, resvDrainPre, resvDrainPost int) {
	s := &n.gfc.L3Side
	s.MainDataBegin = mainDataBegin
	s.PrivateBits = privateBits
	s.ResvDrainPre = resvDrainPre
	s.ResvDrainPost = resvDrainPost
}

func (n *nativeEnc) setScfsi(ch, band, v int) { n.gfc.L3Side.Scfsi[ch][band] = v }
func (n *nativeEnc) setSfbL(i, v int)         { n.gfc.ScalefacBand.L[i] = v }
func (n *nativeEnc) setSfbS(i, v int)         { n.gfc.ScalefacBand.S[i] = v }

func (n *nativeEnc) setGr(gr, ch int, g encGr) {
	gi := &n.gfc.L3Side.Tt[gr][ch]
	gi.Part23Length = g.part23Length
	gi.Part2Length = g.part2Length
	gi.BigValues = g.bigValues
	gi.Count1 = g.count1
	gi.GlobalGain = g.globalGain
	gi.ScalefacCompress = g.scalefacCompress
	gi.BlockType = g.blockType
	gi.MixedBlockFlag = g.mixedBlockFlag
	gi.Region0Count = g.region0Count
	gi.Region1Count = g.region1Count
	gi.Preflag = g.preflag
	gi.ScalefacScale = g.scalefacScale
	gi.Count1tableSelect = g.count1tableSelect
	gi.Sfbdivide = g.sfbdivide
	gi.Sfbmax = g.sfbmax
}

func (n *nativeEnc) setGrTableSelect(gr, ch, idx, v int) {
	n.gfc.L3Side.Tt[gr][ch].TableSelect[idx] = v
}
func (n *nativeEnc) setGrSubblockGain(gr, ch, idx, v int) {
	n.gfc.L3Side.Tt[gr][ch].SubblockGain[idx] = v
}
func (n *nativeEnc) setGrScalefac(gr, ch, sfb, v int) {
	n.gfc.L3Side.Tt[gr][ch].Scalefac[sfb] = v
}
func (n *nativeEnc) setGrPartition(gr, ch int, part4, slen4 [4]int) {
	gi := &n.gfc.L3Side.Tt[gr][ch]
	gi.SfbPartitionTable = append([]int(nil), part4[:]...)
	gi.Slen = slen4
}
func (n *nativeEnc) grTableSelect(gr, ch, idx int) int {
	return n.gfc.L3Side.Tt[gr][ch].TableSelect[idx]
}

// ---- trampolines ----

func (n *nativeEnc) calcFrameLength(kbps, pad int) int { return n.gfc.CalcFrameLength(kbps, pad) }
func (n *nativeEnc) getFrameBits() int                 { return n.gfc.GetFrameBits() }
func (n *nativeEnc) getMaxFrameBufferSizeByConstraint(constraint int) int {
	return n.gfc.GetMaxFrameBufferSizeByConstraint(constraint)
}
func (n *nativeEnc) writeHeader(val, j int)       { n.gfc.WriteHeader(val, j) }
func nativeCRCUpdate(value, crc int) int          { return nativemp3.CRCUpdate(value, crc) }
func (n *nativeEnc) crcWriteHeader(header []byte) { n.gfc.CRCWriteHeader(header) }
func (n *nativeEnc) drainIntoAncillary(remainingBits int) {
	n.gfc.DrainIntoAncillary(remainingBits)
}
func (n *nativeEnc) encodeSideInfo2(bitsPerFrame int) { n.gfc.EncodeSideInfo2(bitsPerFrame) }
func (n *nativeEnc) writeMainData() int               { return n.gfc.WriteMainData() }
func (n *nativeEnc) computeFlushbits() (flushbits, totalBytesOutput int) {
	return n.gfc.ComputeFlushbits()
}
func (n *nativeEnc) flushBitstream()               { n.gfc.FlushBitstream() }
func (n *nativeEnc) addDummyByte(val byte, c uint) { n.gfc.AddDummyByte(val, c) }
func (n *nativeEnc) doCopyBuffer(buf []byte, size int) int {
	return n.gfc.DoCopyBuffer(buf, size)
}
func (n *nativeEnc) copyBuffer(buf []byte, size, mp3data int) int {
	return n.gfc.CopyBuffer(buf, size, mp3data)
}
func (n *nativeEnc) formatBitstream() int { return n.gfc.FormatBitstream() }

func (n *nativeEnc) resvFrameBegin() (fullFrameBits, meanBits int) {
	var mb int
	fullFrameBits = n.gfc.ResvFrameBegin(&mb)
	return fullFrameBits, mb
}
func (n *nativeEnc) resvMaxBits(meanBits, cbr int) (targBits, extraBits int) {
	var tb, eb int
	n.gfc.ResvMaxBits(meanBits, &tb, &eb, cbr)
	return tb, eb
}
func (n *nativeEnc) resvAdjust(gr, ch int)     { n.gfc.ResvAdjust(&n.gfc.L3Side.Tt[gr][ch]) }
func (n *nativeEnc) resvFrameEnd(meanBits int) { n.gfc.ResvFrameEnd(meanBits) }

// ---- read-back ----

func (n *nativeEnc) bsTotbit() int     { return n.gfc.Bs.Totbit }
func (n *nativeEnc) bsBufByteIdx() int { return n.gfc.Bs.BufByteIdx }
func (n *nativeEnc) bsBufBitIdx() int  { return n.gfc.Bs.BufBitIdx }
func (n *nativeEnc) bsBuf(c int) []byte {
	return append([]byte(nil), n.gfc.Bs.Buf[:c]...)
}
func (n *nativeEnc) mainDataBegin() int  { return n.gfc.L3Side.MainDataBegin }
func (n *nativeEnc) resvDrainPre() int   { return n.gfc.L3Side.ResvDrainPre }
func (n *nativeEnc) resvDrainPost() int  { return n.gfc.L3Side.ResvDrainPost }
func (n *nativeEnc) hPtr() int           { return n.gfc.SvEnc.HPtr }
func (n *nativeEnc) wPtr() int           { return n.gfc.SvEnc.WPtr }
func (n *nativeEnc) ancillaryFlag() int  { return n.gfc.SvEnc.AncillaryFlag }
func (n *nativeEnc) resvSize() int       { return n.gfc.SvEnc.ResvSize }
func (n *nativeEnc) resvMax() int        { return n.gfc.SvEnc.ResvMax }
func (n *nativeEnc) substepShaping() int { return n.gfc.SvQnt.SubstepShaping }
func (n *nativeEnc) headerWriteTiming(slot int) int {
	return n.gfc.SvEnc.Header[slot].WriteTiming
}
func (n *nativeEnc) headerPtr(slot int) int { return n.gfc.SvEnc.Header[slot].Ptr }
func (n *nativeEnc) headerBuf(slot, c int) []byte {
	return append([]byte(nil), n.gfc.SvEnc.Header[slot].Buf[:c]...)
}
