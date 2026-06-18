package nativeopus

// Port of libopus/celt/entdec.h + entdec.c.
//
// Range decoder based on Martin 1979 / Pasco 1976 FIFO arithmetic
// coding. Implementation notes are preserved from the C commentary;
// the algorithmic structure is kept verbatim so that the encoder and
// decoder remain in lock-step with the oracle.
//
// Cross-file dependencies: ec_ctx / ec_ilog / ec_tell in entcode.go;
// EC_SYM_BITS / EC_CODE_BITS / EC_CODE_TOP / EC_CODE_BOT / EC_CODE_EXTRA
// / EC_SYM_MAX / EC_MINI in mfrngcod.go; IMUL32 in arch.go; celt_assert
// and celt_udiv also from their respective files.

// ec_read_byte reads the next input byte, or 0 past the end.
// C: entdec.c:91-93.
func ec_read_byte(_this *ec_dec) int {
	if _this.offs < _this.storage {
		b := int(_this.buf[_this.offs])
		_this.offs++
		return b
	}
	return 0
}

// ec_read_byte_from_end — reads the next byte counting from the end
// of the buffer (used for the raw-bits tail).
// C: entdec.c:95-98.
func ec_read_byte_from_end(_this *ec_dec) int {
	if _this.end_offs < _this.storage {
		_this.end_offs++
		return int(_this.buf[_this.storage-_this.end_offs])
	}
	return 0
}

// ec_dec_normalize — shifts val/rng so that rng lies entirely in the
// high-order symbol. Reads input bytes as needed.
// C: entdec.c:102-117.
func ec_dec_normalize(_this *ec_dec) {
	for _this.rng <= EC_CODE_BOT {
		var sym int
		_this.nbits_total += EC_SYM_BITS
		_this.rng <<= EC_SYM_BITS
		// Use up the remaining bits from our last symbol.
		sym = _this.rem
		// Read the next value from the input.
		_this.rem = ec_read_byte(_this)
		// Take the rest of the bits we need from this new symbol.
		sym = (sym<<EC_SYM_BITS | _this.rem) >> (EC_SYM_BITS - EC_CODE_EXTRA)
		// Subtract them from val, capped to less than EC_CODE_TOP.
		_this.val = ((_this.val << EC_SYM_BITS) + (EC_SYM_MAX & ^opus_uint32(sym))) & (EC_CODE_TOP - 1)
	}
}

// ec_dec_init initialises the decoder.
// C: entdec.c:119-137.
func ec_dec_init(_this *ec_dec, _buf []byte, _storage opus_uint32) {
	_this.buf = _buf
	_this.storage = _storage
	_this.end_offs = 0
	_this.end_window = 0
	_this.nend_bits = 0
	// Offset from which ec_tell() subtracts partial bits. The final
	// value after ec_dec_normalize() matches the encoder's, compensating
	// for the bits added by the normalisation loop itself.
	_this.nbits_total = EC_CODE_BITS + 1 -
		((EC_CODE_BITS-EC_CODE_EXTRA)/EC_SYM_BITS)*EC_SYM_BITS
	_this.offs = 0
	_this.rng = 1 << EC_CODE_EXTRA
	_this.rem = ec_read_byte(_this)
	_this.val = _this.rng - 1 - opus_uint32(_this.rem>>(EC_SYM_BITS-EC_CODE_EXTRA))
	_this.error = 0
	// Normalize the interval.
	ec_dec_normalize(_this)
}

// ec_decode — cumulative-frequency decode step.
// C: entdec.c:139-144.
func ec_decode(_this *ec_dec, _ft opus_uint32) opus_uint32 {
	_this.ext = celt_udiv(_this.rng, _ft)
	s := _this.val / _this.ext
	return _ft - EC_MINI(s+1, _ft)
}

// ec_decode_bin — equivalent to ec_decode with _ft == 1 << _bits.
// C: entdec.c:146-151.
func ec_decode_bin(_this *ec_dec, _bits int) opus_uint32 {
	_this.ext = _this.rng >> _bits
	s := _this.val / _this.ext
	return (1 << _bits) - EC_MINI(s+1, 1<<_bits)
}

// ec_dec_update — advances past the symbol whose range was fl..fh of
// total _ft.
// C: entdec.c:153-159.
func ec_dec_update(_this *ec_dec, _fl, _fh, _ft opus_uint32) {
	s := opus_uint32(IMUL32(opus_int32(_this.ext), opus_int32(_ft-_fh)))
	_this.val -= s
	if _fl > 0 {
		_this.rng = opus_uint32(IMUL32(opus_int32(_this.ext), opus_int32(_fh-_fl)))
	} else {
		_this.rng -= s
	}
	ec_dec_normalize(_this)
}

// ec_dec_bit_logp — decodes a bit that has 1/(1<<_logp) probability of
// being a one.
// C: entdec.c:162-175.
func ec_dec_bit_logp(_this *ec_dec, _logp int) int {
	r := _this.rng
	d := _this.val
	s := r >> _logp
	var ret int
	if d < s {
		ret = 1
	} else {
		ret = 0
		_this.val = d - s
	}
	if ret != 0 {
		_this.rng = s
	} else {
		_this.rng = r - s
	}
	ec_dec_normalize(_this)
	return ret
}

// ec_dec_icdf — decodes a symbol using an "inverse" CDF table (bytes).
// C: entdec.c:177-196.
func ec_dec_icdf(_this *ec_dec, _icdf []byte, _ftb int) int {
	s := _this.rng
	d := _this.val
	r := s >> _ftb
	ret := -1
	var t opus_uint32
	for ok := true; ok; ok = d < s {
		t = s
		ret++
		s = opus_uint32(IMUL32(opus_int32(r), opus_int32(_icdf[ret])))
	}
	_this.val = d - s
	_this.rng = t - s
	ec_dec_normalize(_this)
	return ret
}

// ec_dec_icdf16 — same as ec_dec_icdf but with a uint16 table.
// C: entdec.c:198-217.
func ec_dec_icdf16(_this *ec_dec, _icdf []opus_uint16, _ftb int) int {
	s := _this.rng
	d := _this.val
	r := s >> _ftb
	ret := -1
	var t opus_uint32
	for ok := true; ok; ok = d < s {
		t = s
		ret++
		s = opus_uint32(IMUL32(opus_int32(r), opus_int32(_icdf[ret])))
	}
	_this.val = d - s
	_this.rng = t - s
	ec_dec_normalize(_this)
	return ret
}

// ec_dec_uint — extracts a raw unsigned integer with a non-power-of-2
// range from the stream.
// C: entdec.c:219-244.
func ec_dec_uint(_this *ec_dec, _ft opus_uint32) opus_uint32 {
	// EC_ILOG is undefined for 0, so _ft must be at least 2.
	celt_assert(_ft > 1)
	_ft--
	ftb := ec_ilog(_ft)
	if ftb > EC_UINT_BITS {
		ftb -= EC_UINT_BITS
		ft := (_ft >> ftb) + 1
		s := ec_decode(_this, ft)
		ec_dec_update(_this, s, s+1, ft)
		t := s<<ftb | ec_dec_bits(_this, ftb)
		if t <= _ft {
			return t
		}
		_this.error = 1
		return _ft
	}
	_ft++
	s := ec_decode(_this, _ft)
	ec_dec_update(_this, s, s+1, _ft)
	return s
}

// ec_dec_bits — extracts a sequence of raw bits from the stream.
// C: entdec.c:246-266.
func ec_dec_bits(_this *ec_dec, _bits int) opus_uint32 {
	window := _this.end_window
	available := _this.nend_bits
	if available < _bits {
		for {
			window |= ec_window(ec_read_byte_from_end(_this)) << available
			available += EC_SYM_BITS
			if available > EC_WINDOW_SIZE-EC_SYM_BITS {
				break
			}
		}
	}
	ret := opus_uint32(window) & ((1 << _bits) - 1)
	window >>= _bits
	available -= _bits
	_this.end_window = window
	_this.nend_bits = available
	_this.nbits_total += _bits
	return ret
}
