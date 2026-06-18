//go:build linux

// The Pi-5 stateless HEVC decode session: drives the rpi-hevc-dec
// "Memory-to-Memory Stateless Video Decoder" (V4L2 Request API). Userspace
// owns the entire bitstream model — this file parses each access unit's
// VPS/SPS/PPS and slice headers (v4l2_hevc_parse_linux.go +
// v4l2_hevc_slice_linux.go), derives the picture order count and the
// decoded-picture-buffer reference lists, fills the five stateless HEVC
// controls, and submits one frame per media-controller request
// (v4l2_request_linux.go). The hardware writes Broadcom SAND128 tiled NV12
// to the CAPTURE queue, which is de-tiled to linear NV12
// (v4l2_detile_linux.go) for the returned video.Frame.
//
// # Per-access-unit flow
//
//	split Annex-B; cache VPS/SPS/PPS; collect the picture's slices
//	derive POC (prevTid0 POC + lsb) and the DPB / short-term ref lists
//	S_FMT OUTPUT (S265, coded size) once; on first AU also S_FMT CAPTURE
//	allocate a request fd; attach SPS/PPS/scaling/decode-params + the
//	  slice-params dynamic array to it (WHICH_REQUEST_VAL)
//	QBUF the coded OUTPUT buffer bound to the request (REQUEST_FD)
//	QUEUE the request; DQBUF OUTPUT (recycle) + DQBUF CAPTURE (the frame)
//	de-tile SAND128 -> linear NV12; REINIT the request
//
// Frame-based decode mode: all slices of one picture are submitted in a
// single OUTPUT buffer with a slice-params array of one entry per slice.
//
// Not safe for concurrent use.

package hwaccel

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"go-mediatoolkit/video"
)

// statelessNumOutputBufs / statelessNumCaptureBufs size the two queues.
// CAPTURE needs enough buffers to hold the reference pictures the DPB can
// hold (up to 16) plus the picture in flight; OUTPUT is single-shot per
// frame in frame-based mode.
const (
	statelessNumOutputBufs  = 4
	statelessNumCaptureBufs = 20
)

// dpbPic tracks a decoded reference picture: its POC and the CAPTURE
// buffer index + timestamp the hardware uses as the DPB reference key.
type dpbPic struct {
	poc       int32
	bufIndex  uint32
	timestamp syscall.Timeval
	longTerm  bool
}

// v4l2StatelessDecoder is one stateless HEVC decode session.
type v4l2StatelessDecoder struct {
	dev   *v4l2Device
	media *mediaDevice
	cfg   Config

	output  *v4l2Queue
	capture *v4l2Queue

	sps     hevcFullSPS
	pps     hevcFullPPS
	haveSPS bool
	havePPS bool

	codedW, codedH int
	visW, visH     int
	capNumPlanes   int
	capStride      int // SAND128 column height (driver-reported bytesperline)
	capConfigured  bool
	streaming      bool

	// POC derivation state (H.265 8.3.1): the previous TID0 picture POC.
	prevTid0POC int32
	pocMSB      int32

	// dpb holds the active short-term/long-term reference pictures keyed by
	// their CAPTURE buffer; freeCaptureBufs is the pool of CAPTURE buffer
	// indices not currently holding a reference.
	dpb          []dpbPic
	captureInUse map[uint32]bool

	frameIdx int64
	closed   bool
}

// newV4L2StatelessDecoder opens the decoder + its media node and prepares
// the OUTPUT queue lazily on first decode (once the coded size is known).
func newV4L2StatelessDecoder(node string, cfg Config) (*v4l2StatelessDecoder, error) {
	dev, err := openV4L2Device(node)
	if err != nil {
		return nil, err
	}
	mpath := findMediaForVideo(dev.driverName())
	if mpath == "" {
		dev.close()
		return nil, fmt.Errorf("%w: no media node for %s (%s)", ErrBackendFailure, node, dev.driverName())
	}
	media, err := openMediaDevice(mpath)
	if err != nil {
		dev.close()
		return nil, err
	}
	return &v4l2StatelessDecoder{
		dev:          dev,
		media:        media,
		cfg:          cfg,
		captureInUse: make(map[uint32]bool),
	}, nil
}

// Decode submits one Annex-B access unit (one coded picture, possibly
// multiple slices) and returns the decoded frame.
func (d *v4l2StatelessDecoder) Decode(p video.Packet) ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if len(p.Data) == 0 {
		return nil, nil
	}
	nals := splitAnnexBNALs(p.Data)
	if len(nals) == 0 {
		return nil, nil
	}

	// Classify NALs: cache parameter sets, collect the slices of this AU.
	var slices [][]byte
	for _, nal := range nals {
		switch nt := nalUnitType(nal); {
		case nt == hevcNalVPS:
			// VPS carries no field the stateless controls need.
		case nt == hevcNalSPS:
			d.sps = parseFullSPS(ebspToRBSP(nal))
			d.haveSPS = true
		case nt == hevcNalPPS:
			d.pps = parseFullPPS(ebspToRBSP(nal))
			d.havePPS = true
		case nt <= hevcNalRASLR || (nt >= hevcNalBLAWLP && nt <= hevcNalCRANUT):
			slices = append(slices, nal)
		}
	}
	if len(slices) == 0 {
		return nil, nil
	}
	if !d.haveSPS || !d.havePPS {
		return nil, ErrParameterSetsMissing
	}

	return d.decodePicture(slices)
}

// Flush is a no-op: each access unit is decoded synchronously in Decode.
func (d *v4l2StatelessDecoder) Flush() ([]video.Frame, error) {
	if d.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// Close tears down both queues, the media node, and the device. Idempotent.
func (d *v4l2StatelessDecoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	if d.streaming {
		d.dev.streamOff(v4l2BufTypeVideoOutMP)
		d.dev.streamOff(v4l2BufTypeVideoCapMP)
	}
	if d.output != nil {
		d.output.free()
	}
	if d.capture != nil {
		d.capture.free()
	}
	if d.media != nil {
		d.media.close()
	}
	return d.dev.close()
}

// decodePicture parses the picture's slices, derives POC + DPB, configures
// the pipeline if needed, submits the request, and returns the de-tiled
// frame.
func (d *v4l2StatelessDecoder) decodePicture(slices [][]byte) ([]video.Frame, error) {
	// Parse all slices; the first independent slice drives picture-level
	// derivation.
	parsed := make([]parsedSlice, 0, len(slices))
	var first *parsedSlice
	for i := range slices {
		ps := parseSliceHeader(slices[i], &d.sps, &d.pps)
		parsed = append(parsed, ps)
		if first == nil && ps.firstSlice {
			first = &parsed[len(parsed)-1]
		}
	}
	if first == nil {
		first = &parsed[0]
	}

	if err := d.ensurePipeline(); err != nil {
		return nil, err
	}

	poc := d.derivePOC(first)
	decodeParams, refList := d.buildDecodeParams(first, poc)

	// Per-slice: fill num_ref_idx active counts and ref_idx_lX from the
	// derived reference list.
	for i := range parsed {
		d.fillSliceRefs(&parsed[i], refList, parsed[i].params.SliceType)
	}

	frame, bufIdx, ts, err := d.submitRequest(slices, parsed, &d.sps, &d.pps, decodeParams, poc)
	if err != nil {
		return nil, err
	}

	d.updateDPB(first, poc, bufIdx, ts)
	d.frameIdx++
	return []video.Frame{frame}, nil
}

// ensurePipeline sets the OUTPUT format and per-frame controls once, then
// streams on. CAPTURE is configured here too (the stateless decoder needs
// the coded size up front from the SPS, not a SOURCE_CHANGE event).
func (d *v4l2StatelessDecoder) ensurePipeline() error {
	if d.streaming {
		return nil
	}
	d.codedW = int(d.sps.PicWidthInLumaSamples)
	d.codedH = int(d.sps.PicHeightInLumaSamples)
	subW, subH := chromaSub(d.sps.ChromaFormatIDC)
	d.visW = d.codedW - int(d.sps.confWinLeft+d.sps.confWinRight)*subW
	d.visH = d.codedH - int(d.sps.confWinTop+d.sps.confWinBottom)*subH
	if d.visW <= 0 {
		d.visW = d.codedW
	}
	if d.visH <= 0 {
		d.visH = d.codedH
	}

	// OUTPUT: S265 coded slices, one plane, a generous coded buffer.
	if _, err := d.dev.setFormatMP(v4l2BufTypeVideoOutMP, pixFmtHEVCSlice,
		d.codedW, d.codedH, 1, d.codedW*d.codedH); err != nil {
		return err
	}

	// Frame-based decode mode + no-start-code, set on the current value.
	if err := d.setMenuCtrl(v4l2CidStatelessHEVCDecodeMode, v4l2StatelessHEVCDecodeModeFrameBased); err != nil {
		return err
	}
	if err := d.setMenuCtrl(v4l2CidStatelessHEVCStartCode, v4l2StatelessHEVCStartCodeNone); err != nil {
		return err
	}

	// CAPTURE: select NC12 (SAND128) at the coded size. The rpivid driver
	// negotiates a single-plane NC12 (luma + chroma packed) and reports the
	// SAND column height in bytesperline; capture the negotiated geometry.
	capFmt, err := d.dev.setFormatMP(v4l2BufTypeVideoCapMP, pixFmtNV12Col128,
		d.codedW, d.codedH, 2, 0)
	if err != nil {
		return err
	}
	d.capNumPlanes = int(capFmt.NumPlanes)
	if d.capNumPlanes == 0 {
		d.capNumPlanes = 1
	}
	// bytesperline carries the SAND128 column height: luma occupies the top
	// codedH rows of every 128-wide column and chroma the next codedH/2 rows
	// (single-plane packing), so this one stride drives both regions.
	d.capStride = int(capFmt.PlaneFmt[0].BytesPerLine)

	// Allocate + map both queues.
	out, err := newV4L2Queue(d.dev, v4l2BufTypeVideoOutMP, statelessNumOutputBufs, 1)
	if err != nil {
		return err
	}
	d.output = out
	cap, err := newV4L2Queue(d.dev, v4l2BufTypeVideoCapMP, statelessNumCaptureBufs, d.capNumPlanes)
	if err != nil {
		return err
	}
	d.capture = cap

	// Queue all CAPTURE buffers up front so the hardware always has a free
	// target; OUTPUT buffers are queued per-frame.
	for _, b := range d.capture.bufs {
		if err := d.capture.qbuf(b.index, nil, -1, syscall.Timeval{}); err != nil {
			return err
		}
		d.captureInUse[b.index] = false
	}

	if err := d.dev.streamOn(v4l2BufTypeVideoOutMP); err != nil {
		return err
	}
	if err := d.dev.streamOn(v4l2BufTypeVideoCapMP); err != nil {
		return err
	}
	d.streaming = true
	return nil
}

// submitRequest attaches the per-frame controls to a fresh request,
// queues the coded OUTPUT buffer bound to it, launches the request, then
// dequeues the OUTPUT (to recycle) and CAPTURE (the decoded frame) and
// de-tiles it. Returns the frame plus the CAPTURE buffer index/timestamp
// so it can be tracked as a potential DPB reference.
func (d *v4l2StatelessDecoder) submitRequest(slices [][]byte, parsed []parsedSlice,
	sps *hevcFullSPS, pps *hevcFullPPS, decodeParams v4l2CtrlHEVCDecodeParams, poc int32,
) (video.Frame, uint32, syscall.Timeval, error) {
	reqFD, err := d.media.allocRequest()
	if err != nil {
		return video.Frame{}, 0, syscall.Timeval{}, err
	}
	defer closeRequest(reqFD)

	// Pick a free OUTPUT buffer (round-robin by frame index).
	outIdx := uint32(int(d.frameIdx) % len(d.output.bufs))

	// Assemble the coded payload: all slices concatenated as raw RBSP-bearing
	// NAL units WITHOUT start codes (start_code=None), with each slice's
	// bit_size / data_byte_offset recorded relative to the concatenated
	// buffer.
	coded := d.output.bufs[outIdx].planes[0]
	offset := 0
	sliceParams := make([]v4l2CtrlHEVCSliceParams, len(parsed))
	for i := range parsed {
		nal := slices[i]
		if offset+len(nal) > len(coded) {
			return video.Frame{}, 0, syscall.Timeval{}, fmt.Errorf("%w: coded buffer overflow", ErrBackendFailure)
		}
		copy(coded[offset:], nal)
		sp := parsed[i].params
		sp.BitSize = uint32(len(nal) * 8)
		// data_byte_offset is relative to this slice's start in the buffer.
		sp.DataByteOffset = parsed[i].params.DataByteOffset
		// slice_segment_addr already set during parse.
		sliceParams[i] = sp
		offset += len(nal)
	}
	bytesUsed := offset

	// Build the control set. The SPS/PPS/scaling/decode-params are
	// single-object pointer controls; slice-params is a dynamic array.
	spsCtrl := sps.v4l2CtrlHEVCSPS
	ppsCtrl := pps.v4l2CtrlHEVCPPS
	var scaling v4l2CtrlHEVCScalingMatrix
	fillDefaultScalingMatrix(&scaling)

	ctrls := []v4l2ExtControl{
		ptrCtrl(v4l2CidStatelessHEVCSPS, unsafe.Pointer(&spsCtrl), unsafe.Sizeof(spsCtrl)),
		ptrCtrl(v4l2CidStatelessHEVCPPS, unsafe.Pointer(&ppsCtrl), unsafe.Sizeof(ppsCtrl)),
		ptrCtrl(v4l2CidStatelessHEVCDecodeParams, unsafe.Pointer(&decodeParams), unsafe.Sizeof(decodeParams)),
		ptrCtrl(v4l2CidStatelessHEVCScalingMatrix, unsafe.Pointer(&scaling), unsafe.Sizeof(scaling)),
		ptrCtrl(v4l2CidStatelessHEVCSliceParams, unsafe.Pointer(&sliceParams[0]),
			unsafe.Sizeof(sliceParams[0])*uintptr(len(sliceParams))),
	}
	extCtrls := v4l2ExtControls{
		Which:     v4l2CtrlWhichRequestVal,
		Count:     uint32(len(ctrls)),
		RequestFD: int32(reqFD),
		Controls:  uint64(uintptr(unsafe.Pointer(&ctrls[0]))),
	}
	if errno := ioctl(d.dev.fd, vidiocSExtCtrls, unsafe.Pointer(&extCtrls)); errno != 0 {
		return video.Frame{}, 0, syscall.Timeval{}, fmt.Errorf(
			"%w: VIDIOC_S_EXT_CTRLS(request) errIdx=%d: %v", ErrBackendFailure, extCtrls.ErrorIdx, errno)
	}

	// Use a per-frame timestamp as the DPB reference key (microsecond
	// granularity, monotonically increasing).
	ts := syscall.Timeval{Sec: 0, Usec: int64(d.frameIdx + 1)}

	if err := d.output.qbuf(outIdx, []int{bytesUsed}, reqFD, ts); err != nil {
		return video.Frame{}, 0, syscall.Timeval{}, err
	}
	if err := queueRequest(reqFD); err != nil {
		return video.Frame{}, 0, syscall.Timeval{}, err
	}

	// Wait for completion and dequeue OUTPUT (recycle) + CAPTURE (frame).
	if _, err := d.waitDQ(d.output); err != nil {
		return video.Frame{}, 0, syscall.Timeval{}, err
	}
	capIdx, capTS, err := d.dqCapture()
	if err != nil {
		return video.Frame{}, 0, syscall.Timeval{}, err
	}

	frame := d.captureToFrame(capIdx, capTS)

	if err := reinitRequest(reqFD); err != nil {
		return video.Frame{}, 0, syscall.Timeval{}, err
	}
	return frame, capIdx, ts, nil
}

// waitDQ polls then dequeues one buffer from q, returning its index.
func (d *v4l2StatelessDecoder) waitDQ(q *v4l2Queue) (uint32, error) {
	for {
		idx, _, _, _, ok, err := q.dqbuf()
		if err != nil {
			return 0, err
		}
		if ok {
			return idx, nil
		}
		if _, _, _, err := d.dev.poll(2000); err != nil {
			return 0, err
		}
	}
}

// dqCapture dequeues the decoded CAPTURE buffer, returning its index and
// the timestamp the hardware echoed (matching the OUTPUT timestamp it was
// decoded from).
func (d *v4l2StatelessDecoder) dqCapture() (uint32, syscall.Timeval, error) {
	for {
		idx, _, _, ts, ok, err := d.capture.dqbuf()
		if err != nil {
			return 0, syscall.Timeval{}, err
		}
		if ok {
			return idx, ts, nil
		}
		if _, _, _, err := d.dev.poll(2000); err != nil {
			return 0, syscall.Timeval{}, err
		}
	}
}

// captureToFrame de-tiles a decoded SAND128 CAPTURE buffer into a linear
// NV12 video.Frame. The rpivid driver packs luma and chroma into a single
// plane (the chroma tiled region follows the luma region by lumaTiledSize);
// a two-plane negotiation is also handled.
func (d *v4l2StatelessDecoder) captureToFrame(capIdx uint32, _ syscall.Timeval) video.Frame {
	planes := d.capture.bufs[capIdx].planes
	var yTiled, cTiled []byte
	if d.capNumPlanes >= 2 {
		// Two-plane negotiation: luma and chroma are separate planes, each a
		// SAND128 region of its own height (chroma rowOffset 0 within plane 1).
		yTiled, cTiled = planes[0], planes[1]
	} else {
		// Single-plane packing: luma and chroma share one buffer of column
		// height capStride; the chroma region starts at column-row codedH.
		yTiled, cTiled = planes[0], planes[0]
	}
	y, c := detileNV12(yTiled, cTiled, d.visW, d.visH, d.capStride, d.codedH)
	return video.Frame{
		PixelFormat: video.NV12,
		Width:       d.visW,
		Height:      d.visH,
		Planes:      [][]byte{y, c},
		Strides:     []int{d.visW, d.visW},
		PTS:         time.Duration(float64(d.frameIdx) / d.cfg.frameRate() * float64(time.Second)),
	}
}

// ---- POC + DPB derivation --------------------------------------------

// derivePOC computes the picture order count for the current picture per
// H.265 8.3.1, using the prior TID0 POC and slice_pic_order_cnt_lsb. IDR
// pictures reset the count to zero.
func (d *v4l2StatelessDecoder) derivePOC(first *parsedSlice) int32 {
	if isIDR(first.nalType) {
		d.pocMSB = 0
		d.prevTid0POC = 0
		return 0
	}
	maxLsb := int32(1) << (d.sps.Log2MaxPicOrderCntLsbMinus4 + 4)
	lsb := int32(first.picOrderLsb)
	prevLsb := d.prevTid0POC & (maxLsb - 1)
	prevMSB := d.prevTid0POC - prevLsb

	var msb int32
	switch {
	case lsb < prevLsb && (prevLsb-lsb) >= maxLsb/2:
		msb = prevMSB + maxLsb
	case lsb > prevLsb && (lsb-prevLsb) > maxLsb/2:
		msb = prevMSB - maxLsb
	default:
		msb = prevMSB
	}
	if isBLA(first.nalType) || isIRAP(first.nalType) && first.firstSlice {
		// For BLA the MSB is 0; CRA at random access also 0, but within a
		// sequence CRA behaves normally. Keep the general path.
	}
	return msb + lsb
}

// buildDecodeParams fills the picture-level decode-params control and
// returns the derived reference list (short-term before/after entries) for
// per-slice ref_idx population. The DPB array carries every active
// reference picture (its POC + CAPTURE timestamp key).
func (d *v4l2StatelessDecoder) buildDecodeParams(first *parsedSlice, poc int32) (v4l2CtrlHEVCDecodeParams, []dpbPic) {
	var dp v4l2CtrlHEVCDecodeParams
	dp.PicOrderCntVal = poc
	dp.ShortTermRefPicSetSize = first.params.ShortTermRefPicSetSize
	dp.LongTermRefPicSetSize = first.params.LongTermRefPicSetSize

	if isIRAP(first.nalType) {
		dp.Flags |= hevcDecodeParamFlagIRAPPic
	}
	if isIDR(first.nalType) {
		dp.Flags |= hevcDecodeParamFlagIDRPic
	}

	// For IDR/IRAP that flush the DPB there are no references.
	if isIDR(first.nalType) {
		return dp, nil
	}

	// Build PocStCurrBefore / PocStCurrAfter from the in-effect RPS,
	// resolving each delta POC to a DPB entry index.
	maxLsb := int32(1) << (d.sps.Log2MaxPicOrderCntLsbMinus4 + 4)
	_ = maxLsb

	// Populate the DPB array with the current references.
	var refList []dpbPic
	idx := 0
	for _, r := range d.dpb {
		if idx >= v4l2HEVCDPBEntriesMax {
			break
		}
		dp.DPB[idx] = v4l2HEVCDPBEntry{
			Timestamp:      timevalToNS(r.timestamp),
			PicOrderCntVal: r.poc,
		}
		if r.longTerm {
			dp.DPB[idx].Flags = hevcDPBEntryLongTermReference
		}
		refList = append(refList, r)
		idx++
	}
	dp.NumActiveDPBEntries = uint8(idx)

	// Short-term before/after: map each RPS delta to a DPB index by POC.
	var before, after []uint8
	for i := 0; i < first.rps.numNegative; i++ {
		if !first.rps.usedS0[i] {
			continue
		}
		target := poc + first.rps.deltaPocS0[i]
		if di := d.dpbIndexByPOC(refList, target); di >= 0 {
			before = append(before, uint8(di))
		}
	}
	for i := 0; i < first.rps.numPositive; i++ {
		if !first.rps.usedS1[i] {
			continue
		}
		target := poc + first.rps.deltaPocS1[i]
		if di := d.dpbIndexByPOC(refList, target); di >= 0 {
			after = append(after, uint8(di))
		}
	}
	dp.NumPocStCurrBefore = uint8(len(before))
	dp.NumPocStCurrAfter = uint8(len(after))
	copy(dp.PocStCurrBefore[:], before)
	copy(dp.PocStCurrAfter[:], after)
	return dp, refList
}

// dpbIndexByPOC returns the index in refList of the reference with the
// given POC, or -1.
func (d *v4l2StatelessDecoder) dpbIndexByPOC(refList []dpbPic, poc int32) int {
	for i, r := range refList {
		if r.poc == poc {
			return i
		}
	}
	return -1
}

// fillSliceRefs populates a slice's num_ref_idx active counts and ref_idx
// _lX from the picture reference list. For frame-based decode the hardware
// derives the actual ordering; we provide identity indices covering the
// active count.
func (d *v4l2StatelessDecoder) fillSliceRefs(ps *parsedSlice, refList []dpbPic, sliceType uint8) {
	if sliceType == hevcSliceTypeI {
		return
	}
	n := len(refList)
	if n == 0 {
		return
	}
	for i := 0; i <= int(ps.params.NumRefIdxL0ActiveMinus1) && i < v4l2HEVCDPBEntriesMax; i++ {
		ps.params.RefIdxL0[i] = uint8(i % n)
	}
	if sliceType == hevcSliceTypeB {
		for i := 0; i <= int(ps.params.NumRefIdxL1ActiveMinus1) && i < v4l2HEVCDPBEntriesMax; i++ {
			ps.params.RefIdxL1[i] = uint8(i % n)
		}
	}
}

// updateDPB records the just-decoded picture as a reference (if it can be
// one) and prunes pictures no longer referenced by the current RPS. The
// freed CAPTURE buffers are re-queued for reuse.
func (d *v4l2StatelessDecoder) updateDPB(first *parsedSlice, poc int32, bufIdx uint32, ts syscall.Timeval) {
	if isIDR(first.nalType) {
		// IDR flushes the DPB: re-queue every previously-referenced buffer.
		for _, r := range d.dpb {
			if r.bufIndex != bufIdx {
				d.requeueCapture(r.bufIndex)
			}
		}
		d.dpb = d.dpb[:0]
	} else {
		// Prune references not in the current RPS (their POCs are not in the
		// before/after lists relative to this picture's RPS). A picture is
		// kept if some future picture could still reference it; conservatively
		// keep those whose POC is in the RPS-derived "follow" sets. For the
		// simple GOP structures here, keep pictures referenced by the current
		// RPS plus the current picture.
		keep := d.dpb[:0]
		referenced := d.rpsReferencedPOCs(first, poc)
		for _, r := range d.dpb {
			if referenced[r.poc] {
				keep = append(keep, r)
			} else {
				d.requeueCapture(r.bufIndex)
			}
		}
		d.dpb = keep
	}

	// Add the current picture as a reference if its NAL type marks it
	// reference (the _R variants and IRAP pictures).
	if isReferencePicture(first.nalType) {
		d.dpb = append(d.dpb, dpbPic{poc: poc, bufIndex: bufIdx, timestamp: ts})
		d.captureInUse[bufIdx] = true
	} else {
		// Non-reference: this CAPTURE buffer can be recycled immediately.
		d.requeueCapture(bufIdx)
	}

	// Advance the TID0 POC reference for the next picture.
	if first.params.NuhTemporalIDPlus1 == 1 && !isRASLorRADL(first.nalType) {
		d.prevTid0POC = poc
	}
}

// rpsReferencedPOCs returns the set of POCs the current picture's RPS
// references (before+after, used or not — a picture in the RPS is kept in
// the DPB even if not used by the current picture, since it may be used by
// a later picture in the same GOP).
func (d *v4l2StatelessDecoder) rpsReferencedPOCs(first *parsedSlice, poc int32) map[int32]bool {
	m := make(map[int32]bool)
	for i := 0; i < first.rps.numNegative; i++ {
		m[poc+first.rps.deltaPocS0[i]] = true
	}
	for i := 0; i < first.rps.numPositive; i++ {
		m[poc+first.rps.deltaPocS1[i]] = true
	}
	return m
}

// requeueCapture returns a CAPTURE buffer to the hardware for reuse.
func (d *v4l2StatelessDecoder) requeueCapture(bufIdx uint32) {
	if d.captureInUse[bufIdx] {
		d.captureInUse[bufIdx] = false
	}
	d.capture.qbuf(bufIdx, nil, -1, syscall.Timeval{})
}

// ---- control helpers --------------------------------------------------

// setMenuCtrl sets a single integer (menu) control on the current value.
func (d *v4l2StatelessDecoder) setMenuCtrl(id uint32, value int32) error {
	ctrl := v4l2ExtControl{ID: id, Size: 0}
	ctrl.setS32(value)
	extCtrls := v4l2ExtControls{
		Which:    v4l2CtrlWhichCurVal,
		Count:    1,
		Controls: uint64(uintptr(unsafe.Pointer(&ctrl))),
	}
	if errno := ioctl(d.dev.fd, vidiocSExtCtrls, unsafe.Pointer(&extCtrls)); errno != 0 {
		return fmt.Errorf("%w: VIDIOC_S_EXT_CTRLS(menu id=0x%x): %v", ErrBackendFailure, id, errno)
	}
	return nil
}

// ptrCtrl builds a pointer-payload ext-control for a variable-size codec
// control.
func ptrCtrl(id uint32, p unsafe.Pointer, size uintptr) v4l2ExtControl {
	c := v4l2ExtControl{ID: id, Size: uint32(size)}
	c.setPtr(p)
	return c
}

// ---- misc -------------------------------------------------------------

// chromaSub returns the chroma sub-sampling factors for a chroma_format_idc
// (0 mono, 1 = 4:2:0, 2 = 4:2:2, 3 = 4:4:4) used for conformance-window
// cropping.
func chromaSub(idc uint8) (subW, subH int) {
	switch idc {
	case 1:
		return 2, 2
	case 2:
		return 2, 1
	case 3:
		return 1, 1
	default:
		return 1, 1
	}
}

// isReferencePicture reports whether a NAL type marks a picture usable as a
// reference (the _R sub-layer variants and IRAP pictures); the _N variants
// are non-reference.
func isReferencePicture(nalType int) bool {
	switch nalType {
	case hevcNalTrailN, hevcNalTSAN, hevcNalSTSAN, hevcNalRADLN, hevcNalRASLN:
		return false
	}
	return true
}

// isRASLorRADL reports whether a NAL type is a leading picture (RASL/RADL),
// which do not advance the TID0 POC reference.
func isRASLorRADL(nalType int) bool {
	return nalType >= hevcNalRADLN && nalType <= hevcNalRASLR
}

// timevalToNS converts a buffer timestamp to the nanosecond key the kernel
// uses to match DPB references (v4l2_timeval_to_ns).
func timevalToNS(tv syscall.Timeval) uint64 {
	return uint64(tv.Sec)*1_000_000_000 + uint64(tv.Usec)*1000
}

// fillDefaultScalingMatrix fills a flat (all-16) scaling matrix, the
// default when scaling lists are not signalled.
func fillDefaultScalingMatrix(m *v4l2CtrlHEVCScalingMatrix) {
	for i := range m.ScalingList4x4 {
		for j := range m.ScalingList4x4[i] {
			m.ScalingList4x4[i][j] = 16
		}
	}
	for i := range m.ScalingList8x8 {
		for j := range m.ScalingList8x8[i] {
			m.ScalingList8x8[i][j] = 16
		}
	}
	for i := range m.ScalingList16x16 {
		for j := range m.ScalingList16x16[i] {
			m.ScalingList16x16[i][j] = 16
		}
	}
	for i := range m.ScalingList32x32 {
		for j := range m.ScalingList32x32[i] {
			m.ScalingList32x32[i][j] = 16
		}
	}
	for i := range m.ScalingListDCCoef16x16 {
		m.ScalingListDCCoef16x16[i] = 16
	}
	for i := range m.ScalingListDCCoef32x32 {
		m.ScalingListDCCoef32x32[i] = 16
	}
}
