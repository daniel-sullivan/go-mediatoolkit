package ogg

// nativeDecoder implements the Decoder interface in pure Go.
// Port of ogg_stream_state (decode path) from libogg/src/framing.c.
type nativeDecoder struct {
	bodyData     []byte
	bodyFill     int
	bodyReturned int

	lacingVals     []int
	granuleVals    []int64
	lacingFill     int
	lacingPacket   int
	lacingReturned int

	serialNo   int32
	pageno     int32
	packetno   int64
	granulepos int64
	eos        bool
	bos        bool
}

func newNativeDecoder(serialNo int32) (Decoder, error) {
	d := &nativeDecoder{
		bodyData:    make([]byte, 16*1024),
		lacingVals:  make([]int, 1024),
		granuleVals: make([]int64, 1024),
		serialNo:    serialNo,
	}
	return d, nil
}

func (d *nativeDecoder) PageIn(page *Page) error {
	header := page.Header
	body := page.Body
	bodySize := len(body)
	segptr := 0

	if len(header) < 27 {
		return ErrBadArg
	}

	version := int(header[4])
	continued := header[5]&0x01 != 0
	bos := header[5]&0x02 != 0
	eos := header[5]&0x04 != 0
	granulepos := page.GranulePos()
	serialno := page.SerialNo()
	pageno := page.PageNo()
	segments := int(header[26])

	if serialno != d.serialNo {
		return ErrStreamMismatch
	}
	if version > 0 {
		return ErrBadVersion
	}

	// Clean up returned data.
	if d.bodyReturned > 0 {
		d.bodyFill -= d.bodyReturned
		if d.bodyFill > 0 {
			copy(d.bodyData, d.bodyData[d.bodyReturned:d.bodyReturned+d.bodyFill])
		}
		d.bodyReturned = 0
	}

	if d.lacingReturned > 0 {
		lr := d.lacingReturned
		if d.lacingFill-lr > 0 {
			copy(d.lacingVals, d.lacingVals[lr:d.lacingFill])
			copy(d.granuleVals, d.granuleVals[lr:d.lacingFill])
		}
		d.lacingFill -= lr
		d.lacingPacket -= lr
		d.lacingReturned = 0
	}

	// Grow lacing storage if needed.
	d.lacingExpand(segments + 1)

	// Check page sequence.
	if pageno != d.pageno {
		// Unroll previous partial packet.
		for i := d.lacingPacket; i < d.lacingFill; i++ {
			d.bodyFill -= d.lacingVals[i] & 0xff
		}
		d.lacingFill = d.lacingPacket

		// Mark dropped data.
		if d.pageno != -1 {
			d.lacingVals[d.lacingFill] = 0x400
			d.lacingFill++
			d.lacingPacket++
		}
	}

	// Handle continued packet page.
	if continued {
		if d.lacingFill < 1 ||
			(d.lacingVals[d.lacingFill-1]&0xff) < 255 ||
			d.lacingVals[d.lacingFill-1] == 0x400 {
			bos = false
			for segptr < segments {
				val := int(header[27+segptr])
				body = body[val:]
				bodySize -= val
				if val < 255 {
					segptr++
					break
				}
				segptr++
			}
		}
	}

	if bodySize > 0 {
		d.bodyExpand(bodySize)
		copy(d.bodyData[d.bodyFill:], body[:bodySize])
		d.bodyFill += bodySize
	}

	{
		saved := -1
		for segptr < segments {
			val := int(header[27+segptr])
			d.lacingVals[d.lacingFill] = val
			d.granuleVals[d.lacingFill] = -1

			if bos {
				d.lacingVals[d.lacingFill] |= 0x100
				bos = false
			}

			if val < 255 {
				saved = d.lacingFill
			}

			d.lacingFill++
			segptr++

			if val < 255 {
				d.lacingPacket = d.lacingFill
			}
		}

		// Set granule position on last complete packet.
		if saved != -1 {
			d.granuleVals[saved] = granulepos
		}
	}

	if eos {
		d.eos = true
		if d.lacingFill > 0 {
			d.lacingVals[d.lacingFill-1] |= 0x200
		}
	}

	d.pageno = pageno + 1

	return nil
}

func (d *nativeDecoder) packetOut(advance bool) (Packet, int) {
	ptr := d.lacingReturned
	if d.lacingPacket <= ptr {
		return Packet{}, 0
	}

	if d.lacingVals[ptr]&0x400 != 0 {
		// Gap marker — report hole.
		if advance {
			d.lacingReturned++
			d.packetno++
		}
		return Packet{}, -1
	}

	// Gather the whole packet.
	size := d.lacingVals[ptr] & 0xff
	pktBytes := size
	eosFlag := d.lacingVals[ptr] & 0x200
	bosFlag := d.lacingVals[ptr] & 0x100

	p := ptr
	for size == 255 {
		p++
		val := d.lacingVals[p]
		size = val & 0xff
		if val&0x200 != 0 {
			eosFlag = 0x200
		}
		pktBytes += size
	}

	data := make([]byte, pktBytes)
	copy(data, d.bodyData[d.bodyReturned:d.bodyReturned+pktBytes])

	pkt := Packet{
		Data:       data,
		BOS:        bosFlag != 0,
		EOS:        eosFlag != 0,
		GranulePos: d.granuleVals[p],
		PacketNo:   d.packetno,
	}

	if advance {
		d.bodyReturned += pktBytes
		d.lacingReturned = p + 1
		d.packetno++
	}

	return pkt, 1
}

func (d *nativeDecoder) PacketOut() (Packet, int, error) {
	pkt, ret := d.packetOut(true)
	return pkt, ret, nil
}

func (d *nativeDecoder) PacketPeek() (Packet, int, error) {
	pkt, ret := d.packetOut(false)
	return pkt, ret, nil
}

func (d *nativeDecoder) SerialNo() int32 { return d.serialNo }

func (d *nativeDecoder) EOS() bool { return d.eos }

func (d *nativeDecoder) Reset() {
	d.bodyFill = 0
	d.bodyReturned = 0
	d.lacingFill = 0
	d.lacingPacket = 0
	d.lacingReturned = 0
	d.eos = false
	d.bos = false
	d.pageno = -1
	d.packetno = 0
	d.granulepos = 0
}

func (d *nativeDecoder) bodyExpand(needed int) {
	if len(d.bodyData)-needed <= d.bodyFill {
		newSize := 2 * (d.bodyFill + needed)
		buf := make([]byte, newSize)
		copy(buf, d.bodyData[:d.bodyFill])
		d.bodyData = buf
	}
}

func (d *nativeDecoder) lacingExpand(needed int) {
	if len(d.lacingVals)-needed <= d.lacingFill {
		newSize := 2 * (d.lacingFill + needed)
		lv := make([]int, newSize)
		copy(lv, d.lacingVals[:d.lacingFill])
		d.lacingVals = lv
		gv := make([]int64, newSize)
		copy(gv, d.granuleVals[:d.lacingFill])
		d.granuleVals = gv
	}
}
