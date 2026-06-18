package nativeopus

// Port of libopus/celt/laplace.h + laplace.c.
//
// Laplace-distribution entropy helpers used by quant_bands to code
// fine-energy deltas. Pure integer math, all bit-exact with the C
// oracle.

const (
	// LAPLACE_LOG_MINP — log2 of the minimum probability of an energy
	// delta (out of 32768).
	LAPLACE_LOG_MINP = 0
	// LAPLACE_MINP — minimum probability of an energy delta.
	LAPLACE_MINP = 1 << LAPLACE_LOG_MINP
	// LAPLACE_NMIN — minimum number of guaranteed representable energy
	// deltas (in one direction).
	LAPLACE_NMIN = 16
)

// ec_laplace_get_freq1 — compute the PDF frequency for |value| == 1
// given the zero-probability mass `fs0` and the decay factor. When
// called, decay is positive and at most 11456.
// C: laplace.c:44-49.
func ec_laplace_get_freq1(fs0 opus_uint32, decay int) opus_uint32 {
	ft := opus_uint32(32768-LAPLACE_MINP*(2*LAPLACE_NMIN)) - fs0
	return opus_uint32(opus_int32(ft) * opus_int32(16384-decay) >> 15)
}

// ec_laplace_encode — encode a Laplace-distributed int. The pointer
// form mirrors the C signature: on overflow (value reaches the
// tail-probability floor), *value is updated to the actual encoded
// magnitude so the caller knows what was emitted.
// C: laplace.c:51-92.
func ec_laplace_encode(enc *ec_enc, value *int, fs opus_uint32, decay int) {
	var fl opus_uint32
	val := *value
	fl = 0
	if val != 0 {
		var s int
		var i int
		if val < 0 {
			s = -1
		} else {
			s = 0
		}
		val = (val + s) ^ s
		fl = fs
		fs = ec_laplace_get_freq1(fs, decay)
		// Search the decaying part of the PDF.
		for i = 1; fs > 0 && i < val; i++ {
			fs *= 2
			fl += fs + 2*LAPLACE_MINP
			fs = opus_uint32(opus_int32(fs) * opus_int32(decay) >> 15)
		}
		// Everything beyond that has probability LAPLACE_MINP.
		if fs == 0 {
			var di int
			var ndi_max int
			ndi_max = int(32768-fl+LAPLACE_MINP-1) >> LAPLACE_LOG_MINP
			ndi_max = (ndi_max - s) >> 1
			di = IMIN(opus_int(val-i), opus_int(ndi_max-1))
			fl += opus_uint32((2*di + 1 + s) * LAPLACE_MINP)
			fs = opus_uint32(IMIN(opus_int(LAPLACE_MINP), opus_int(32768-fl)))
			*value = (i + di + s) ^ s
		} else {
			fs += LAPLACE_MINP
			fl += fs & opus_uint32(^s)
		}
		celt_assert(fl+fs <= 32768)
		celt_assert(fs > 0)
	}
	ec_encode_bin(enc, fl, fl+fs, 15)
}

// ec_laplace_decode — inverse of ec_laplace_encode.
// C: laplace.c:94-134.
func ec_laplace_decode(dec *ec_dec, fs opus_uint32, decay int) int {
	val := 0
	var fl opus_uint32
	fm := ec_decode_bin(dec, 15)
	fl = 0
	if fm >= fs {
		val++
		fl = fs
		fs = ec_laplace_get_freq1(fs, decay) + LAPLACE_MINP
		// Search the decaying part of the PDF.
		for fs > LAPLACE_MINP && fm >= fl+2*fs {
			fs *= 2
			fl += fs
			fs = opus_uint32(opus_int32(fs-2*LAPLACE_MINP) * opus_int32(decay) >> 15)
			fs += LAPLACE_MINP
			val++
		}
		// Everything beyond that has probability LAPLACE_MINP.
		if fs <= LAPLACE_MINP {
			di := int(fm-fl) >> (LAPLACE_LOG_MINP + 1)
			val += di
			fl += opus_uint32(2 * di * LAPLACE_MINP)
		}
		if fm < fl+fs {
			val = -val
		} else {
			fl += fs
		}
	}
	celt_assert(fl < 32768)
	celt_assert(fs > 0)
	celt_assert(fl <= fm)
	celt_assert(fm < opus_uint32(IMIN(opus_int(fl+fs), 32768)))
	ec_dec_update(dec, fl, opus_uint32(IMIN(opus_int(fl+fs), 32768)), 32768)
	return val
}

// ec_laplace_encode_p0 — alternative Laplace encoder keyed on a
// bit-0 probability `p0`. Used by the QEXT extension; not in our
// decode path, but ported for completeness.
// C: laplace.c:136-162.
func ec_laplace_encode_p0(enc *ec_enc, value int, p0, decay opus_uint16) {
	var s int
	var sign_icdf [3]opus_uint16
	sign_icdf[0] = 32768 - p0
	sign_icdf[1] = sign_icdf[0] / 2
	sign_icdf[2] = 0
	switch {
	case value == 0:
		s = 0
	case value > 0:
		s = 1
	default:
		s = 2
	}
	ec_enc_icdf16(enc, s, sign_icdf[:], 15)
	if value < 0 {
		value = -value
	}
	if value != 0 {
		var i int
		var icdf [8]opus_uint16
		icdf[0] = opus_uint16(IMAX(7, opus_int(decay)))
		for i = 1; i < 7; i++ {
			icdf[i] = opus_uint16(IMAX(opus_int(7-i),
				opus_int(opus_int32(icdf[i-1])*opus_int32(decay)>>15)))
		}
		icdf[7] = 0
		value--
		for {
			ec_enc_icdf16(enc, IMIN(value, 7), icdf[:], 15)
			value -= 7
			if value < 0 {
				break
			}
		}
	}
}

// ec_laplace_decode_p0 — inverse of ec_laplace_encode_p0.
// C: laplace.c:164-192.
func ec_laplace_decode_p0(dec *ec_dec, p0, decay opus_uint16) int {
	var s int
	var value int
	var sign_icdf [3]opus_uint16
	sign_icdf[0] = 32768 - p0
	sign_icdf[1] = sign_icdf[0] / 2
	sign_icdf[2] = 0
	s = ec_dec_icdf16(dec, sign_icdf[:], 15)
	if s == 2 {
		s = -1
	}
	if s != 0 {
		var i int
		var v int
		var icdf [8]opus_uint16
		icdf[0] = opus_uint16(IMAX(7, opus_int(decay)))
		for i = 1; i < 7; i++ {
			icdf[i] = opus_uint16(IMAX(opus_int(7-i),
				opus_int(opus_int32(icdf[i-1])*opus_int32(decay)>>15)))
		}
		icdf[7] = 0
		value = 1
		for {
			v = ec_dec_icdf16(dec, icdf[:], 15)
			value += v
			if v != 7 {
				break
			}
		}
		return s * value
	}
	return 0
}
