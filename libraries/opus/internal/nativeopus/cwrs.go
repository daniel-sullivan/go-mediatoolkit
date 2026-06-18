package nativeopus

// Port of libopus/celt/cwrs.h + cwrs.c — the PVQ codebook size /
// index (V, U) tables and the encode_pulses / decode_pulses core.
//
// Our build does not define CUSTOM_MODES, ENABLE_QEXT, or
// SMALL_FOOTPRINT. We therefore take the table-backed path (the
// larger, ~1272-entry CELT_PVQ_U_DATA without the EXTRA_ROWS suffix)
// and omit the log2_frac / get_required_bits helpers that are only
// used by CUSTOM_MODES. The SMALL_FOOTPRINT recurrence-based path
// (ncwrs_urow, unext, uprev, the smaller icwrs / cwrsi) is likewise
// not compiled.
//
// All arithmetic is integer; there are no FMA concerns.

// CELT_PVQ_U — U(N,K) lookup, indexed by min(N,K) then max(N,K) via
// CELT_PVQ_U_ROW. The macro CELT_PVQ_U(n,k) becomes the helper below.
func CELT_PVQ_U(n, k opus_int) opus_uint32 {
	return CELT_PVQ_U_ROW[IMIN(n, k)][IMAX(n, k)]
}

// CELT_PVQ_V(n,k) = U(n,k) + U(n,k+1) — the number of PVQ codewords
// for a band of size n with k pulses.
func CELT_PVQ_V(n, k opus_int) opus_uint32 {
	return CELT_PVQ_U(n, k) + CELT_PVQ_U(n, k+1)
}

// icwrs — compute the PVQ codebook index for the given pulse vector.
// C: cwrs.c:444-460.
func icwrs(_n opus_int, _y []opus_int) opus_uint32 {
	celt_assert(_n >= 2)
	j := _n - 1
	var i opus_uint32
	if _y[j] < 0 {
		i = 1
	}
	k := _y[j]
	if k < 0 {
		k = -k
	}
	for {
		j--
		i += CELT_PVQ_U(_n-j, k)
		aj := _y[j]
		if aj < 0 {
			aj = -aj
		}
		k += aj
		if _y[j] < 0 {
			i += CELT_PVQ_U(_n-j, k+1)
		}
		if j <= 0 {
			break
		}
	}
	return i
}

// encode_pulses — range-encode the PVQ codebook index of the given
// pulse vector. C: cwrs.c:462-465.
func encode_pulses(_y []opus_int, _n, _k opus_int, _enc *ec_enc) {
	celt_assert(_k > 0)
	ec_enc_uint(_enc, icwrs(_n, _y), CELT_PVQ_V(_n, _k))
}

// cwrsi — recover the _i-th pulse vector. Destructively advances
// through the CELT_PVQ_U table. Returns the energy (sum of squares)
// of the recovered vector.
// C: cwrs.c:467-541.
func cwrsi(_n, _k opus_int, _i opus_uint32, _y []opus_int) opus_val32 {
	var p opus_uint32
	var s opus_int
	var k0 opus_int
	var val opus_int16
	var yy opus_val32 = 0
	celt_assert(_k > 0)
	celt_assert(_n > 1)
	yi := 0
	for _n > 2 {
		var q opus_uint32
		// Lots of pulses case:
		if _k >= _n {
			row := CELT_PVQ_U_ROW[_n]
			// Are the pulses in this dimension negative?
			p = row[_k+1]
			if _i >= p {
				s = -1
			} else {
				s = 0
			}
			_i -= p & opus_uint32(s)
			// Count how many pulses were placed in this dimension.
			k0 = _k
			q = row[_n]
			if q > _i {
				celt_sig_assert(p > q)
				_k = _n
				for {
					_k--
					p = CELT_PVQ_U_ROW[_k][_n]
					if p <= _i {
						break
					}
				}
			} else {
				for p = row[_k]; p > _i; p = row[_k] {
					_k--
				}
			}
			_i -= p
			val = opus_int16((k0 - _k + s) ^ s)
			_y[yi] = opus_int(val)
			yi++
			yy = MAC16_16(yy, opus_val16(val), opus_val16(val))
		} else {
			// Lots of dimensions case. Are there any pulses in this
			// dimension at all?
			p = CELT_PVQ_U_ROW[_k][_n]
			q = CELT_PVQ_U_ROW[_k+1][_n]
			if p <= _i && _i < q {
				_i -= p
				_y[yi] = 0
				yi++
			} else {
				// Are the pulses negative?
				if _i >= q {
					s = -1
				} else {
					s = 0
				}
				_i -= q & opus_uint32(s)
				k0 = _k
				for {
					_k--
					p = CELT_PVQ_U_ROW[_k][_n]
					if p <= _i {
						break
					}
				}
				_i -= p
				val = opus_int16((k0 - _k + s) ^ s)
				_y[yi] = opus_int(val)
				yi++
				yy = MAC16_16(yy, opus_val16(val), opus_val16(val))
			}
		}
		_n--
	}
	// _n == 2
	p = opus_uint32(2*_k + 1)
	if _i >= p {
		s = -1
	} else {
		s = 0
	}
	_i -= p & opus_uint32(s)
	k0 = _k
	_k = opus_int((_i + 1) >> 1)
	if _k != 0 {
		_i -= opus_uint32(2*_k - 1)
	}
	val = opus_int16((k0 - _k + s) ^ s)
	_y[yi] = opus_int(val)
	yi++
	yy = MAC16_16(yy, opus_val16(val), opus_val16(val))
	// _n == 1
	s = -opus_int(_i)
	val = opus_int16((_k + s) ^ s)
	_y[yi] = opus_int(val)
	yy = MAC16_16(yy, opus_val16(val), opus_val16(val))
	return yy
}

// decode_pulses — public decode entry point. Reads the index from the
// range coder and fills _y with the recovered pulse vector.
// C: cwrs.c:543-545.
func decode_pulses(_y []opus_int, _n, _k opus_int, _dec *ec_dec) opus_val32 {
	return cwrsi(_n, _k, ec_dec_uint(_dec, CELT_PVQ_V(_n, _k)), _y)
}
