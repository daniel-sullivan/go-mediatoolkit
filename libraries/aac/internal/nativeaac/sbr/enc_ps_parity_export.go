// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parity-export shims for the parametric-stereo ENCODER. These expose the
// unexported PS encode kernels (enc_ps_encode.go / enc_ps_bitenc.go /
// enc_ps_main.go) to the external parity_tests packages so the byte-exact cgo
// oracles can drive identical inputs through the Go port and the vendored C.
//
// FDK-AAC-derived; see libfdk/COPYING. Fenced behind aacfdk.
package sbr

// PSOutHandle wraps a *psOut so the SBR-extension writer
// (WriteEnvSingleChannelElementPS) can carry the parametric-stereo output of a
// frame without exposing the unexported struct.
type PSOutHandle struct{ p *psOut }

// PSOutHandleFromParity builds a PSOutHandle from a flat PSOutParity (the same
// fields the bitwrite oracle uses), so the SBR-extension parity slice can drive
// a known ps_data() through the extension wrapper.
func PSOutHandleFromParity(p PSOutParity) *PSOutHandle {
	out := &psOut{
		enablePSHeader: p.EnablePSHeader, enableIID: p.EnableIID, iidMode: p.IidMode,
		enableICC: p.EnableICC, iccMode: p.IccMode, enableIpdOpd: p.EnableIpdOpd,
		frameClass: p.FrameClass, nEnvelopes: p.NEnvelopes,
	}
	out.frameBorder = p.FrameBorder
	out.deltaIID = p.DeltaIID
	out.deltaICC = p.DeltaICC
	out.iidLast = p.IidLast
	out.iccLast = p.IccLast
	for e := 0; e < 4; e++ {
		out.iid[e] = p.Iid[e]
		out.icc[e] = p.Icc[e]
	}
	return &PSOutHandle{p: out}
}

// PSOutParity is a flat mirror of T_PS_OUT for the bitwrite oracle.
type PSOutParity struct {
	EnablePSHeader int
	EnableIID      int
	IidMode        int
	EnableICC      int
	IccMode        int
	EnableIpdOpd   int

	FrameClass  int
	NEnvelopes  int
	FrameBorder [4]int

	DeltaIID [4]int
	Iid      [4][20]int
	IidLast  [20]int

	DeltaICC [4]int
	Icc      [4][20]int
	IccLast  [20]int
}

// WritePSBitstreamParity builds a psOut from the flat scenario and runs the
// 1:1-ported ps_data() writer, returning the emitted bytes + bit count.
func WritePSBitstreamParity(p PSOutParity) (payload []byte, nBits int) {
	out := &psOut{
		enablePSHeader: p.EnablePSHeader,
		enableIID:      p.EnableIID,
		iidMode:        p.IidMode,
		enableICC:      p.EnableICC,
		iccMode:        p.IccMode,
		enableIpdOpd:   p.EnableIpdOpd,
		frameClass:     p.FrameClass,
		nEnvelopes:     p.NEnvelopes,
	}
	out.frameBorder = p.FrameBorder
	out.deltaIID = p.DeltaIID
	out.deltaICC = p.DeltaICC
	out.iidLast = p.IidLast
	out.iccLast = p.IccLast
	for e := 0; e < 4; e++ {
		out.iid[e] = p.Iid[e]
		out.icc[e] = p.Icc[e]
	}

	mem := make([]byte, 512)
	bs := NewFdkWriteBitStream(mem)
	nBits = writePSBitstream(out, bs)
	_ = nBits
	bits := int(bs.GetValidBits())
	nBytes := (bits + 7) >> 3
	return bs.Bytes()[:nBytes], bits
}

// CountPSBitstreamParity returns just the bit count (nil-bitbuffer path).
func CountPSBitstreamParity(p PSOutParity) int {
	out := &psOut{
		enablePSHeader: p.EnablePSHeader, enableIID: p.EnableIID, iidMode: p.IidMode,
		enableICC: p.EnableICC, iccMode: p.IccMode, enableIpdOpd: p.EnableIpdOpd,
		frameClass: p.FrameClass, nEnvelopes: p.NEnvelopes,
	}
	out.frameBorder = p.FrameBorder
	out.deltaIID = p.DeltaIID
	out.deltaICC = p.DeltaICC
	out.iidLast = p.IidLast
	out.iccLast = p.IccLast
	for e := 0; e < 4; e++ {
		out.iid[e] = p.Iid[e]
		out.icc[e] = p.Icc[e]
	}
	return writePSBitstream(out, nil)
}

// --- PS parameter EXTRACTION parity export ---------------------------------

// PSEncodeParity drives FDKsbrEnc_PSEncode (psEncodeRun) end-to-end for the
// ps-enc-extract oracle. hybridFlat is [HYBRID_FRAMESIZE][2 ch][2 reim][71]
// laid out row-major (col-major over col, then ch, then reim, then 71 bands).
// It returns the resulting PS_OUT fields that the bitstream writer consumes.
type PSEncodeResult struct {
	EnablePSHeader int
	EnableIID      int
	IidMode        int
	EnableICC      int
	IccMode        int
	FrameClass     int
	NEnvelopes     int
	FrameBorder    [4]int
	DeltaIID       [4]int
	DeltaICC       [4]int
	Iid            [4][20]int
	Icc            [4][20]int
	IidLast        [20]int
	IccLast        [20]int
}

// NewPSEncodeParity creates + inits a psEncode for psEncMode (10 or 20) with the
// given iidQuantErrorThreshold.
func NewPSEncodeParity(psEncMode int, iidQuantErrorThreshold int32) *PSEncodeState {
	h := createPSEncode()
	initPSEncode(h, psEncMode, iidQuantErrorThreshold)
	return &PSEncodeState{h: h}
}

// PSEncodeState wraps a stateful psEncode handle so multi-frame parity (the
// delta-time / header counters) can be exercised across frames.
type PSEncodeState struct{ h *psEncode }

// DynBandScale exposes the per-band dynamic scale array the caller must supply
// (computed by psFindBestScaling in the full path; the oracle supplies it).
func (s *PSEncodeState) Encode(hybridFlat []int32, dynBandScale []uint8, maxEnvelopes, frameSize, sendHeader int) PSEncodeResult {
	const nb = 71
	var hyb [hybridFramesize][maxPsChannels][2][]int32
	for col := 0; col < hybridFramesize; col++ {
		for ch := 0; ch < maxPsChannels; ch++ {
			for ri := 0; ri < 2; ri++ {
				base := ((col*maxPsChannels+ch)*2 + ri) * nb
				hyb[col][ch][ri] = hybridFlat[base : base+nb]
			}
		}
	}
	var ds [psMaxBandsE]uint8
	copy(ds[:], dynBandScale)

	out := new(psOut)
	psEncodeRun(s.h, out, ds[:], uint(maxEnvelopes), hyb, frameSize, sendHeader)

	r := PSEncodeResult{
		EnablePSHeader: out.enablePSHeader,
		EnableIID:      out.enableIID,
		IidMode:        out.iidMode,
		EnableICC:      out.enableICC,
		IccMode:        out.iccMode,
		FrameClass:     out.frameClass,
		NEnvelopes:     out.nEnvelopes,
		FrameBorder:    out.frameBorder,
		DeltaIID:       out.deltaIID,
		DeltaICC:       out.deltaICC,
		IidLast:        out.iidLast,
		IccLast:        out.iccLast,
	}
	for e := 0; e < 4; e++ {
		r.Iid[e] = out.iid[e]
		r.Icc[e] = out.icc[e]
	}
	return r
}
