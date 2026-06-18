package nativeopus

// Port of libopus/celt/entenc.h + entenc.c.
//
// Range encoder — the counterpart to entdec.go. Writes range-coded
// symbols at the front of the buffer (offs growing upward) and raw
// bits at the tail (end_offs growing inward from storage). See
// entdec.go's header comment for references.

// ec_write_byte writes one byte at the front of the buffer.
// C: entenc.c:60-64.
func ec_write_byte(_this *ec_enc, _value opus_uint32) int {
	if _this.offs+_this.end_offs >= _this.storage {
		return -1
	}
	_this.buf[_this.offs] = byte(_value)
	_this.offs++
	return 0
}

// ec_write_byte_at_end writes one byte at the tail of the buffer.
// C: entenc.c:66-70.
func ec_write_byte_at_end(_this *ec_enc, _value opus_uint32) int {
	if _this.offs+_this.end_offs >= _this.storage {
		return -1
	}
	_this.end_offs++
	_this.buf[_this.storage-_this.end_offs] = byte(_value)
	return 0
}

// ec_enc_carry_out flushes the oldest symbol (with carry tracking)
// once a further carry can no longer reach it.
// C: entenc.c:82-99.
func ec_enc_carry_out(_this *ec_enc, _c int) {
	if opus_uint32(_c) != EC_SYM_MAX {
		carry := _c >> EC_SYM_BITS
		// Don't output a byte on the first write. Branch prediction
		// handles this cheaply thereafter.
		if _this.rem >= 0 {
			_this.error |= ec_write_byte(_this, opus_uint32(_this.rem+carry))
		}
		if _this.ext > 0 {
			sym := opus_uint32(EC_SYM_MAX+opus_uint32(carry)) & EC_SYM_MAX
			for {
				_this.error |= ec_write_byte(_this, sym)
				_this.ext--
				if _this.ext == 0 {
					break
				}
			}
		}
		_this.rem = _c & int(EC_SYM_MAX)
	} else {
		_this.ext++
	}
}

// ec_enc_normalize — outputs bits and rescales val/rng when the range
// is too small.
// C: entenc.c:101-110.
func ec_enc_normalize(_this *ec_enc) {
	for _this.rng <= EC_CODE_BOT {
		ec_enc_carry_out(_this, int(_this.val>>EC_CODE_SHIFT))
		_this.val = (_this.val << EC_SYM_BITS) & (EC_CODE_TOP - 1)
		_this.rng <<= EC_SYM_BITS
		_this.nbits_total += EC_SYM_BITS
	}
}

// ec_enc_init initialises the encoder.
// C: entenc.c:112-126.
func ec_enc_init(_this *ec_enc, _buf []byte, _size opus_uint32) {
	_this.buf = _buf
	_this.end_offs = 0
	_this.end_window = 0
	_this.nend_bits = 0
	// Offset from which ec_tell() will subtract partial bits.
	_this.nbits_total = EC_CODE_BITS + 1
	_this.offs = 0
	_this.rng = EC_CODE_TOP
	_this.rem = -1
	_this.val = 0
	_this.ext = 0
	_this.storage = _size
	_this.error = 0
}

// ec_encode — encodes a symbol with the given cumulative-frequency
// triple (fl, fh, ft).
// C: entenc.c:128-137.
func ec_encode(_this *ec_enc, _fl, _fh, _ft opus_uint32) {
	r := celt_udiv(_this.rng, _ft)
	if _fl > 0 {
		_this.val += _this.rng - opus_uint32(IMUL32(opus_int32(r), opus_int32(_ft-_fl)))
		_this.rng = opus_uint32(IMUL32(opus_int32(r), opus_int32(_fh-_fl)))
	} else {
		_this.rng -= opus_uint32(IMUL32(opus_int32(r), opus_int32(_ft-_fh)))
	}
	ec_enc_normalize(_this)
}

// ec_encode_bin — equivalent to ec_encode with _ft == 1 << _bits.
// C: entenc.c:139-148.
func ec_encode_bin(_this *ec_enc, _fl, _fh opus_uint32, _bits int) {
	r := _this.rng >> _bits
	if _fl > 0 {
		_this.val += _this.rng - opus_uint32(IMUL32(opus_int32(r), opus_int32((1<<_bits)-_fl)))
		_this.rng = opus_uint32(IMUL32(opus_int32(r), opus_int32(_fh-_fl)))
	} else {
		_this.rng -= opus_uint32(IMUL32(opus_int32(r), opus_int32((1<<_bits)-_fh)))
	}
	ec_enc_normalize(_this)
}

// ec_enc_bit_logp — encodes a bit with 1/(1<<_logp) probability of
// being a one.
// C: entenc.c:151-162.
func ec_enc_bit_logp(_this *ec_enc, _val, _logp int) {
	r := _this.rng
	l := _this.val
	s := r >> _logp
	r -= s
	if _val != 0 {
		_this.val = l + r
	}
	if _val != 0 {
		_this.rng = s
	} else {
		_this.rng = r
	}
	ec_enc_normalize(_this)
}

// ec_enc_icdf — encodes a symbol using a byte inverse-CDF table.
// C: entenc.c:164-173.
func ec_enc_icdf(_this *ec_enc, _s int, _icdf []byte, _ftb int) {
	r := _this.rng >> _ftb
	if _s > 0 {
		_this.val += _this.rng - opus_uint32(IMUL32(opus_int32(r), opus_int32(_icdf[_s-1])))
		_this.rng = opus_uint32(IMUL32(opus_int32(r), opus_int32(_icdf[_s-1]-_icdf[_s])))
	} else {
		_this.rng -= opus_uint32(IMUL32(opus_int32(r), opus_int32(_icdf[_s])))
	}
	ec_enc_normalize(_this)
}

// ec_enc_icdf16 — same as ec_enc_icdf but with a uint16 table.
// C: entenc.c:175-184.
func ec_enc_icdf16(_this *ec_enc, _s int, _icdf []opus_uint16, _ftb int) {
	r := _this.rng >> _ftb
	if _s > 0 {
		_this.val += _this.rng - opus_uint32(IMUL32(opus_int32(r), opus_int32(_icdf[_s-1])))
		_this.rng = opus_uint32(IMUL32(opus_int32(r), opus_int32(_icdf[_s-1]-_icdf[_s])))
	} else {
		_this.rng -= opus_uint32(IMUL32(opus_int32(r), opus_int32(_icdf[_s])))
	}
	ec_enc_normalize(_this)
}

// ec_enc_uint — encodes a raw unsigned integer (non-power-of-2 range).
// C: entenc.c:186-202.
func ec_enc_uint(_this *ec_enc, _fl, _ft opus_uint32) {
	// EC_ILOG is undefined for 0, so _ft must be at least 2.
	celt_assert(_ft > 1)
	_ft--
	ftb := ec_ilog(_ft)
	if ftb > EC_UINT_BITS {
		ftb -= EC_UINT_BITS
		ft := (_ft >> ftb) + 1
		fl := _fl >> ftb
		ec_encode(_this, fl, fl+1, ft)
		ec_enc_bits(_this, _fl&((1<<ftb)-1), ftb)
	} else {
		ec_encode(_this, _fl, _fl+1, _ft+1)
	}
}

// ec_enc_bits — encodes _bits raw bits from _fl into the tail.
// C: entenc.c:204-223.
func ec_enc_bits(_this *ec_enc, _fl opus_uint32, _bits int) {
	window := _this.end_window
	used := _this.nend_bits
	celt_assert(_bits > 0)
	if used+_bits > EC_WINDOW_SIZE {
		for {
			_this.error |= ec_write_byte_at_end(_this, opus_uint32(window)&EC_SYM_MAX)
			window >>= EC_SYM_BITS
			used -= EC_SYM_BITS
			if used < EC_SYM_BITS {
				break
			}
		}
	}
	window |= ec_window(_fl) << used
	used += _bits
	_this.end_window = window
	_this.nend_bits = used
	_this.nbits_total += _bits
}

// ec_enc_patch_initial_bits — overwrites the first _nbits bits of the
// stream with _val. Requires _nbits <= 8.
// C: entenc.c:225-246.
func ec_enc_patch_initial_bits(_this *ec_enc, _val opus_uint32, _nbits int) {
	celt_assert(_nbits <= EC_SYM_BITS)
	shift := EC_SYM_BITS - _nbits
	mask := opus_uint32((1<<_nbits)-1) << shift
	switch {
	case _this.offs > 0:
		// First byte already finalised.
		_this.buf[0] = byte((opus_uint32(_this.buf[0]) & ^mask) | _val<<shift)
	case _this.rem >= 0:
		// First byte still awaiting carry propagation.
		_this.rem = int(opus_uint32(_this.rem)&^mask) | int(_val<<shift)
	case _this.rng <= EC_CODE_TOP>>_nbits:
		// Renormalisation loop hasn't run yet.
		_this.val = (_this.val & ^(mask << EC_CODE_SHIFT)) |
			_val<<(EC_CODE_SHIFT+shift)
	default:
		// Encoder hasn't encoded _nbits of data yet.
		_this.error = -1
	}
}

// ec_enc_shrink — compacts the buffer down to _size bytes, preserving
// the raw-bits tail.
// C: entenc.c:248-253.
func ec_enc_shrink(_this *ec_enc, _size opus_uint32) {
	celt_assert(_this.offs+_this.end_offs <= _size)
	// C: OPUS_MOVE(buf + size - end_offs, buf + storage - end_offs, end_offs)
	copy(_this.buf[_size-_this.end_offs:_size],
		_this.buf[_this.storage-_this.end_offs:_this.storage])
	_this.storage = _size
}

// ec_enc_done — finalises the stream: outputs the minimal number of
// bits needed to disambiguate the encoded symbols, flushes any
// buffered bits, and fills the remaining space with zeros.
// C: entenc.c:255-305.
func ec_enc_done(_this *ec_enc) {
	// Output the minimum number of bits that guarantees the symbols
	// encoded thus far decode correctly regardless of subsequent bits.
	l := EC_CODE_BITS - ec_ilog(_this.rng)
	msk := (EC_CODE_TOP - 1) >> l
	end := (_this.val + msk) & ^msk
	if end|msk >= _this.val+_this.rng {
		l++
		msk >>= 1
		end = (_this.val + msk) & ^msk
	}
	for l > 0 {
		ec_enc_carry_out(_this, int(end>>EC_CODE_SHIFT))
		end = (end << EC_SYM_BITS) & (EC_CODE_TOP - 1)
		l -= EC_SYM_BITS
	}
	// Flush any buffered byte.
	if _this.rem >= 0 || _this.ext > 0 {
		ec_enc_carry_out(_this, 0)
	}
	// Flush buffered extra bits.
	window := _this.end_window
	used := _this.nend_bits
	for used >= EC_SYM_BITS {
		_this.error |= ec_write_byte_at_end(_this, opus_uint32(window)&EC_SYM_MAX)
		window >>= EC_SYM_BITS
		used -= EC_SYM_BITS
	}
	// Clear any excess space and add remaining bits to the last byte.
	if _this.error == 0 {
		if _this.buf != nil {
			clear(_this.buf[_this.offs : _this.storage-_this.end_offs])
		}
		if used > 0 {
			// No range-coder data at all → give up.
			if _this.end_offs >= _this.storage {
				_this.error = -1
			} else {
				l = -l
				// If we've busted, don't add too many extra bits to
				// the last byte — preserving the range-coder data is
				// more important.
				if _this.offs+_this.end_offs >= _this.storage && l < used {
					window &= (1 << l) - 1
					_this.error = -1
				}
				_this.buf[_this.storage-_this.end_offs-1] |= byte(window)
			}
		}
	}
}
