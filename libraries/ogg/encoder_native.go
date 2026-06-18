package ogg

// nativeEncoder implements the Encoder interface in pure Go.
// Port of ogg_stream_state (encode path) from libogg/src/framing.c.
type nativeEncoder struct {
	bodyData     []byte
	bodyFill     int
	bodyReturned int

	lacingVals  []int
	granuleVals []int64
	lacingFill  int

	header     [282]byte
	headerFill int

	serialNo   int32
	pageno     int32
	packetno   int64
	granulepos int64
	eos        bool
	bos        bool
}

func newNativeEncoder(serialNo int32) (Encoder, error) {
	e := &nativeEncoder{
		bodyData:    make([]byte, 16*1024),
		lacingVals:  make([]int, 1024),
		granuleVals: make([]int64, 1024),
		serialNo:    serialNo,
	}
	return e, nil
}

func (e *nativeEncoder) PacketIn(pkt *Packet) error {
	data := pkt.Data
	pktBytes := len(data)
	lacingVals := pktBytes/255 + 1

	// Compact returned body data.
	if e.bodyReturned > 0 {
		e.bodyFill -= e.bodyReturned
		if e.bodyFill > 0 {
			copy(e.bodyData, e.bodyData[e.bodyReturned:e.bodyReturned+e.bodyFill])
		}
		e.bodyReturned = 0
	}

	// Grow storage if needed.
	e.bodyExpand(pktBytes)
	e.lacingExpand(lacingVals)

	// Copy packet body.
	copy(e.bodyData[e.bodyFill:], data)
	e.bodyFill += pktBytes

	// Store lacing values.
	for i := 0; i < lacingVals-1; i++ {
		e.lacingVals[e.lacingFill+i] = 255
		e.granuleVals[e.lacingFill+i] = e.granulepos
	}
	e.lacingVals[e.lacingFill+lacingVals-1] = pktBytes % 255
	e.granulepos = pkt.GranulePos
	e.granuleVals[e.lacingFill+lacingVals-1] = pkt.GranulePos

	// Flag first segment as beginning of packet.
	e.lacingVals[e.lacingFill] |= 0x100

	e.lacingFill += lacingVals
	e.packetno++

	if pkt.EOS {
		e.eos = true
	}

	return nil
}

func (e *nativeEncoder) flushI(force bool, nfill int) (Page, bool) {
	if e.lacingFill == 0 {
		return Page{}, false
	}

	maxVals := e.lacingFill
	if maxVals > 255 {
		maxVals = 255
	}

	vals := 0
	bytes := 0
	granulePos := int64(-1)

	if !e.bos {
		// Initial header page: include only the first complete packet.
		granulePos = 0
		for vals = 0; vals < maxVals; vals++ {
			if e.lacingVals[vals]&0xff < 255 {
				vals++
				break
			}
		}
	} else {
		// Normal page: accumulate segments.
		packetsDone := 0
		packetJustDone := 0
		acc := 0
		for vals = 0; vals < maxVals; vals++ {
			if acc > nfill && packetJustDone >= 4 {
				force = true
				break
			}
			acc += e.lacingVals[vals] & 0xff
			if e.lacingVals[vals]&0xff < 255 {
				granulePos = e.granuleVals[vals]
				packetsDone++
				packetJustDone = packetsDone
			} else {
				packetJustDone = 0
			}
		}
		if vals == 255 {
			force = true
		}
	}

	if !force {
		return Page{}, false
	}

	// Build the header.
	e.header[0] = 'O'
	e.header[1] = 'g'
	e.header[2] = 'g'
	e.header[3] = 'S'

	// Version.
	e.header[4] = 0

	// Flags.
	e.header[5] = 0
	if e.lacingVals[0]&0x100 == 0 {
		e.header[5] |= 0x01 // continued packet
	}
	if !e.bos {
		e.header[5] |= 0x02 // BOS
	}
	if e.eos && e.lacingFill == vals {
		e.header[5] |= 0x04 // EOS
	}
	e.bos = true

	// Granule position (64-bit LE).
	gp := granulePos
	for i := 6; i < 14; i++ {
		e.header[i] = byte(gp)
		gp >>= 8
	}

	// Serial number (32-bit LE).
	sn := e.serialNo
	for i := 14; i < 18; i++ {
		e.header[i] = byte(sn)
		sn >>= 8
	}

	// Page number (32-bit LE).
	if e.pageno == -1 {
		e.pageno = 0
	}
	pn := e.pageno
	e.pageno++
	for i := 18; i < 22; i++ {
		e.header[i] = byte(pn)
		pn >>= 8
	}

	// CRC placeholder (computed below).
	e.header[22] = 0
	e.header[23] = 0
	e.header[24] = 0
	e.header[25] = 0

	// Segment table.
	e.header[26] = byte(vals)
	for i := 0; i < vals; i++ {
		seg := byte(e.lacingVals[i] & 0xff)
		e.header[27+i] = seg
		bytes += int(seg)
	}
	headerLen := vals + 27

	// Build the page — single allocation for header+body.
	raw := make([]byte, headerLen+bytes)
	hdr := raw[:headerLen]
	copy(hdr, e.header[:headerLen])
	body := raw[headerLen:]
	copy(body, e.bodyData[e.bodyReturned:e.bodyReturned+bytes])

	// Advance lacing and body pointers.
	e.lacingFill -= vals
	if e.lacingFill > 0 {
		copy(e.lacingVals, e.lacingVals[vals:vals+e.lacingFill])
		copy(e.granuleVals, e.granuleVals[vals:vals+e.lacingFill])
	}
	e.bodyReturned += bytes

	// Compute and set CRC-32.
	crc := oggCRC32(0, hdr)
	crc = oggCRC32(crc, body)
	hdr[22] = byte(crc)
	hdr[23] = byte(crc >> 8)
	hdr[24] = byte(crc >> 16)
	hdr[25] = byte(crc >> 24)

	return Page{Header: hdr, Body: body}, true
}

func (e *nativeEncoder) PageOut() (Page, bool) {
	force := false
	if (e.eos && e.lacingFill > 0) || (e.lacingFill > 0 && !e.bos) {
		force = true
	}
	return e.flushI(force, 4096)
}

func (e *nativeEncoder) Flush() (Page, bool) {
	return e.flushI(true, 4096)
}

func (e *nativeEncoder) SerialNo() int32 { return e.serialNo }

func (e *nativeEncoder) EOS() bool { return e.eos }

func (e *nativeEncoder) Reset() {
	e.bodyFill = 0
	e.bodyReturned = 0
	e.lacingFill = 0
	e.headerFill = 0
	e.eos = false
	e.bos = false
	e.pageno = -1
	e.packetno = 0
	e.granulepos = 0
}

func (e *nativeEncoder) ResetSerialNo(serialNo int32) {
	e.Reset()
	e.serialNo = serialNo
}

func (e *nativeEncoder) GranulePos() int64 {
	return e.granulepos
}

func (e *nativeEncoder) bodyExpand(needed int) {
	if len(e.bodyData)-needed <= e.bodyFill {
		newSize := 2 * (e.bodyFill + needed)
		buf := make([]byte, newSize)
		copy(buf, e.bodyData[:e.bodyFill])
		e.bodyData = buf
	}
}

func (e *nativeEncoder) lacingExpand(needed int) {
	if len(e.lacingVals)-needed <= e.lacingFill {
		newSize := 2 * (e.lacingFill + needed)
		lv := make([]int, newSize)
		copy(lv, e.lacingVals[:e.lacingFill])
		e.lacingVals = lv
		gv := make([]int64, newSize)
		copy(gv, e.granuleVals[:e.lacingFill])
		e.granuleVals = gv
	}
}
